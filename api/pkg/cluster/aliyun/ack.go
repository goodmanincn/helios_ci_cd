// Package aliyun 实现 Aliyun ACK 集群 Provider (M5)。
package aliyun

import (
	"context"
	"encoding/json"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cs"
	"github.com/helios-cicd/helios/api/pkg/cluster"
	"github.com/helios-cicd/helios/api/pkg/cluster/selfhosted"
)

// Provider ACK 集群提供者。
type Provider struct {
	config cluster.ClusterConfig
	cm     *CredentialManager
	inner  cluster.Provider
}

// New 创建 ACK Provider。
func New(cfg cluster.ClusterConfig) (*Provider, error) {
	return &Provider{config: cfg}, nil
}

// Connect 获取集群 kubeconfig 并构建内嵌 Provider。
func (p *Provider) Connect(ctx context.Context) error {
	if p.inner != nil {
		return nil
	}

	var creds struct {
		AccessKeyID     string `json:"access_key_id"`
		AccessKeySecret string `json:"access_key_secret"`
		RoleARN         string `json:"role_arn"`
		Region          string `json:"region"`
		ClusterID       string `json:"cluster_id"`
	}
	if err := json.Unmarshal(p.config.Kubeconfig, &creds); err != nil {
		return cluster.NewError(cluster.KindInvalidInput, "invalid ack credentials", err)
	}

	cm := NewCredentialManager(creds.AccessKeyID, creds.AccessKeySecret, creds.RoleARN, creds.Region)
	cred, err := cm.Get()
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "sts assume role", err)
	}
	p.cm = cm

	config := sdk.NewConfig()
	csCred := credentials.NewStsTokenCredential(cred.AccessKeyID, cred.AccessKeySecret, cred.SecurityToken)
	client, err := cs.NewClientWithOptions(creds.Region, config, csCred)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "cs client", err)
	}

	req := cs.CreateDescribeClusterUserKubeconfigRequest()
	req.ClusterId = creds.ClusterID
	resp, err := client.DescribeClusterUserKubeconfig(req)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "describe kubeconfig", err)
	}

	inner, err := selfhosted.New(cluster.ClusterConfig{
		Provider:   "selfhosted",
		Kubeconfig: []byte(resp.Config),
	})
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "build inner provider", err)
	}
	p.inner = inner
	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) (*cluster.HealthInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.HealthCheck(ctx)
}

func (p *Provider) ListNamespaces(ctx context.Context) ([]string, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.ListNamespaces(ctx)
}

func (p *Provider) ListWorkloads(ctx context.Context, namespace string) ([]cluster.WorkloadInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.ListWorkloads(ctx, namespace)
}

func (p *Provider) GetEvents(ctx context.Context, namespace string, limit int) ([]cluster.EventInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.GetEvents(ctx, namespace, limit)
}

func (p *Provider) Deploy(ctx context.Context, spec cluster.DeploySpec) (*cluster.DeployResult, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.Deploy(ctx, spec)
}

func (p *Provider) Rollback(ctx context.Context, namespace, deployment string, toRevision int64) error {
	if err := p.Connect(ctx); err != nil {
		return err
	}
	return p.inner.Rollback(ctx, namespace, deployment, toRevision)
}

func (p *Provider) GetDeploymentHistory(ctx context.Context, namespace, deployment string) ([]cluster.RevisionInfo, error) {
	if err := p.Connect(ctx); err != nil {
		return nil, err
	}
	return p.inner.GetDeploymentHistory(ctx, namespace, deployment)
}
