// Package selfhosted 实现 ClusterProvider 接口 — 自建 K8s (kubeconfig)。
package selfhosted

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/helios-cicd/helios/api/pkg/cluster"
)

// Provider 自建 K8s 集群提供者。
type Provider struct {
	config     cluster.ClusterConfig
	restConfig *rest.Config
	clientset  kubernetes.Interface
}

// New 从 kubeconfig 字节创建 Provider。
func New(cfg cluster.ClusterConfig) (*Provider, error) {
	if len(cfg.Kubeconfig) == 0 {
		return nil, cluster.NewError(cluster.KindInvalidInput, "kubeconfig is required", nil)
	}
	return &Provider{config: cfg}, nil
}

// Connect 解析 kubeconfig 并建连。
func (p *Provider) Connect(ctx context.Context) error {
	if p.clientset != nil {
		return nil
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(p.config.Kubeconfig)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "parse kubeconfig", err)
	}
	p.restConfig = restCfg
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "build clientset", err)
	}
	p.clientset = cs
	return nil
}

// HealthCheck 调 Discovery API 取版本,同时数 node/namespace。
func (p *Provider) HealthCheck(ctx context.Context) (*cluster.HealthInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	info := &cluster.HealthInfo{Healthy: true}

	// ServerVersion
	ver, err := p.clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "server version", err)
	}
	info.Version = ver.GitVersion

	// Nodes
	nodeList, err := p.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list nodes", err)
	}
	info.NodeCount = len(nodeList.Items)

	// Namespaces
	nsList, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list namespaces", err)
	}
	info.NamespaceCount = len(nsList.Items)

	return info, nil
}

// ListNamespaces 返回所有 namespace 名称。
func (p *Provider) ListNamespaces(ctx context.Context) ([]string, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	list, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list namespaces", err)
	}
	out := make([]string, 0, len(list.Items))
	for _, n := range list.Items {
		out = append(out, n.Name)
	}
	return out, nil
}

// ListWorkloads 返回 namespace 下的 Deployment/StatefulSet/DaemonSet 概要。
func (p *Provider) ListWorkloads(ctx context.Context, namespace string) ([]cluster.WorkloadInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	var out []cluster.WorkloadInfo

	// Deployments
	ds, err := p.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list deployments", err)
	}
	for _, d := range ds.Items {
		out = append(out, cluster.WorkloadInfo{
			Kind:      "Deployment",
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     int(d.Status.ReadyReplicas),
			Desired:   int(*d.Spec.Replicas),
			Updated:   int(d.Status.UpdatedReplicas),
			Status:    workloadStatus(d.Status.ReadyReplicas, *d.Spec.Replicas),
		})
	}

	// StatefulSets
	ss, err := p.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list statefulsets", err)
	}
	for _, s := range ss.Items {
		out = append(out, cluster.WorkloadInfo{
			Kind:      "StatefulSet",
			Name:      s.Name,
			Namespace: s.Namespace,
			Ready:     int(s.Status.ReadyReplicas),
			Desired:   int(*s.Spec.Replicas),
			Updated:   int(s.Status.UpdatedReplicas),
			Status:    workloadStatus(s.Status.ReadyReplicas, *s.Spec.Replicas),
		})
	}

	// DaemonSets
	dm, err := p.clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list daemonsets", err)
	}
	for _, d := range dm.Items {
		out = append(out, cluster.WorkloadInfo{
			Kind:      "DaemonSet",
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     int(d.Status.NumberReady),
			Desired:   int(d.Status.DesiredNumberScheduled),
			Updated:   int(d.Status.UpdatedNumberScheduled),
			Status:    workloadStatus(int32(d.Status.NumberReady), d.Status.DesiredNumberScheduled),
		})
	}

	return out, nil
}

// GetEvents 返回最近事件。
func (p *Provider) GetEvents(ctx context.Context, namespace string, limit int) ([]cluster.EventInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	list, err := p.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
	if err != nil {
		return nil, cluster.NewError(cluster.KindConnectError, "list events", err)
	}
	out := make([]cluster.EventInfo, 0, len(list.Items))
	for _, e := range list.Items {
		out = append(out, cluster.EventInfo{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Object:    fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
			Timestamp: e.LastTimestamp.Time,
		})
	}
	return out, nil
}

func workloadStatus(ready, desired int32) string {
	if ready == desired {
		return "healthy"
	}
	if ready > 0 {
		return "progressing"
	}
	return "degraded"
}
