// Package tencent 实现 Tencent TKE 集群 Provider (M5)。
package tencent

import (
	"context"
	"encoding/json"

	"github.com/helios-cicd/helios/api/pkg/cluster"
	"github.com/helios-cicd/helios/api/pkg/cluster/selfhosted"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tke "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20180525"
)

// Provider TKE 集群提供者。
type Provider struct {
	config cluster.ClusterConfig
	cm     *CredentialManager
	inner  cluster.Provider
}

// New 创建 TKE Provider。
func New(cfg cluster.ClusterConfig) (*Provider, error) {
	return &Provider{config: cfg}, nil
}

// Connect 获取集群 kubeconfig 并构建内嵌 Provider。
func (p *Provider) Connect(ctx context.Context) error {
	if p.inner != nil {
		return nil
	}

	// 解析 config 中的凭据
	var creds struct {
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
		RoleARN   string `json:"role_arn"`
		Region    string `json:"region"`
		ClusterID string `json:"cluster_id"`
	}
	if err := json.Unmarshal(p.config.Kubeconfig, &creds); err != nil {
		return cluster.NewError(cluster.KindInvalidInput, "invalid tke credentials", err)
	}

	// STS 临时凭据
	cm := NewCredentialManager(creds.SecretID, creds.SecretKey, creds.RoleARN, creds.Region)
	cred, err := cm.Get()
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "sts assume role", err)
	}
	p.cm = cm

	// TKE API 获取 kubeconfig
	tkeCred := common.NewCredential(cred.SecretID, cred.SecretKey)
	tkeCred.Token = cred.SessionToken
	prof := profile.NewClientProfile()
	client, err := tke.NewClient(tkeCred, creds.Region, prof)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "tke client", err)
	}

	req := tke.NewDescribeClusterKubeconfigRequest()
	req.ClusterId = &creds.ClusterID
	resp, err := client.DescribeClusterKubeconfig(req)
	if err != nil {
		return cluster.NewError(cluster.KindConnectError, "describe kubeconfig", err)
	}

	inner, err := selfhosted.New(cluster.ClusterConfig{
		Provider:   "selfhosted",
		Kubeconfig: []byte(*resp.Response.Kubeconfig),
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
