// deploy.go — server-side apply + manifest 解析 + 等待 Ready (E4.3)。
package selfhosted

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"

	"github.com/helios-cicd/helios/api/pkg/cluster"
)

// Deploy 用 server-side apply 部署 manifest。
func (p *Provider) Deploy(ctx context.Context, spec cluster.DeploySpec) (*cluster.DeployResult, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}

	// 1. 解析 manifest
	objs, err := parseManifests(spec.Manifest)
	if err != nil {
		return nil, cluster.NewError(cluster.KindInvalidInput, "parse manifest", err)
	}
	if len(objs) == 0 {
		return nil, cluster.NewError(cluster.KindInvalidInput, "no resources found in manifest", nil)
	}

	// 2. 镜像替换 + labels 注入
	for _, obj := range objs {
		injectLabels(obj, spec.Labels)
		if spec.Image != "" {
			replaceImage(obj, spec.Image)
		}
		if spec.Strategy != "" {
			injectStrategy(obj, spec.Strategy)
		}
	}

	// 3. 准备 dynamic client + rest mapper
	dyn, err := dynamic.NewForConfig(p.restConfig)
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "dynamic client", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(p.restConfig)
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "discovery client", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	// 4. SSA apply
	var deploymentName, deploymentNS string
	for _, obj := range objs {
		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, cluster.NewError(cluster.KindDeployError, fmt.Sprintf("map %s", gvk), err)
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := obj.GetNamespace()
			if ns == "" {
				ns = spec.Namespace
			}
			if ns == "" {
				ns = "default"
			}
			obj.SetNamespace(ns)
			dr = dyn.Resource(mapping.Resource).Namespace(ns)
		} else {
			dr = dyn.Resource(mapping.Resource)
		}

		_, err = dr.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
			FieldManager: "helios",
			Force:        true,
		})
		if err != nil {
			return nil, cluster.NewError(cluster.KindDeployError,
				fmt.Sprintf("apply %s/%s", obj.GetKind(), obj.GetName()), err)
		}

		if obj.GetKind() == "Deployment" {
			deploymentName = obj.GetName()
			deploymentNS = obj.GetNamespace()
		}
	}

	// 5. 等待 Ready
	if deploymentName != "" && spec.WaitTimeout > 0 {
		if err := waitDeploymentReady(ctx, p.clientset, deploymentNS, deploymentName, spec.WaitTimeout); err != nil {
			return nil, err
		}
	}

	// 6. 构造结果
	result := &cluster.DeployResult{DeploymentName: deploymentName}
	if deploymentName != "" {
		dep, err := p.clientset.AppsV1().Deployments(deploymentNS).Get(ctx, deploymentName, metav1.GetOptions{})
		if err == nil {
			result.ReadyReplicas = int(dep.Status.ReadyReplicas)
			if dep.Spec.Replicas != nil {
				result.DesiredReplicas = int(*dep.Spec.Replicas)
			}
		}
	}
	return result, nil
}

// Rollback 回滚 Deployment 到指定 revision。
func (p *Provider) Rollback(ctx context.Context, namespace, deployment string, toRevision int64) error {
	if err := p.Connect(ctx); err != nil {
		return err
	}

	// 1. 找到目标 revision 的 ReplicaSet
	list, err := p.clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "list replicasets", err)
	}
	var target *appsv1.ReplicaSet
	for i := range list.Items {
		rs := &list.Items[i]
		// 只取属于该 Deployment 的 ReplicaSet (ownerReferences)
		belongs := false
		for _, owner := range rs.OwnerReferences {
			if owner.Kind == "Deployment" && owner.Name == deployment {
				belongs = true
				break
			}
		}
		if !belongs {
			continue
		}
		rev := rs.Annotations["deployment.kubernetes.io/revision"]
		if parseInt64(rev) == toRevision {
			target = rs
			break
		}
	}
	if target == nil {
		return cluster.NewError(cluster.KindNotFound, fmt.Sprintf("revision %d not found", toRevision), nil)
	}

	// 2. Patch Deployment 的 spec.template 为目标 ReplicaSet 的 PodTemplate
	dep, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "get deployment", err)
	}
	dep.Spec.Template = target.Spec.Template
	_, err = p.clientset.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		return cluster.NewError(cluster.KindDeployError, "update deployment", err)
	}
	return nil
}

// GetDeploymentHistory 返回 Deployment 的 revision 历史。
func (p *Provider) GetDeploymentHistory(ctx context.Context, namespace, deployment string) ([]cluster.RevisionInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	// 读取所有 ReplicaSet,按 creationTimestamp 排序,提取 revision 注释。
	list, err := p.clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deployment),
	})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list replicasets", err)
	}
	var out []cluster.RevisionInfo
	for _, rs := range list.Items {
		rev := rs.Annotations["deployment.kubernetes.io/revision"]
		if rev == "" {
			continue
		}
		image := ""
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			image = rs.Spec.Template.Spec.Containers[0].Image
		}
		out = append(out, cluster.RevisionInfo{
			Revision:  parseInt64(rev),
			Image:     image,
			Status:    rsStatus(rs.Status),
			CreatedAt: rs.CreationTimestamp.Time,
		})
	}
	return out, nil
}

// ---- manifest 解析 ----

func parseManifests(data []byte) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(data)), 4096)
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: raw}
		out = append(out, obj)
	}
	return out, nil
}

func injectLabels(obj *unstructured.Unstructured, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	curr, _, _ := unstructured.NestedMap(obj.Object, "metadata", "labels")
	if curr == nil {
		curr = map[string]any{}
	}
	for k, v := range labels {
		curr[k] = v
	}
	_ = unstructured.SetNestedMap(obj.Object, curr, "metadata", "labels")
}

func replaceImage(obj *unstructured.Unstructured, image string) {
	containers, found, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}
	for i, c := range containers {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		m["image"] = image
		containers[i] = m
	}
	_ = unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
}

func injectStrategy(obj *unstructured.Unstructured, strategy string) {
	if obj.GetKind() != "Deployment" {
		return
	}
	if strategy == "recreate" {
		_ = unstructured.SetNestedMap(obj.Object, map[string]any{"type": "Recreate"}, "spec", "strategy")
	}
	// rolling 用 K8s 默认值,不覆盖
}

// ---- 等待 Ready ----

func waitDeploymentReady(ctx context.Context, cs kubernetes.Interface, ns, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		dep, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return cluster.NewError(cluster.KindDeployError, "get deployment", err)
		}
		if dep.Status.ReadyReplicas == dep.Status.Replicas {
			return nil
		}
		select {
		case <-ctx.Done():
			return cluster.NewError(cluster.KindTimeout, "wait deployment ready timeout", ctx.Err())
		case <-ticker.C:
		}
	}
}

func rsStatus(status appsv1.ReplicaSetStatus) string {
	if status.Replicas == status.ReadyReplicas {
		return "healthy"
	}
	return "progressing"
}

func parseInt64(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}
