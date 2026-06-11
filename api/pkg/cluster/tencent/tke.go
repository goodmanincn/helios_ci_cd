// Package tencent 实现 Tencent TKE 集群 Provider (M5)。
package tencent

import (
	"context"

	"github.com/helios-cicd/helios/api/pkg/cluster"
)

// Provider TKE 集群提供者。
type Provider struct {
	config cluster.ClusterConfig
}

// New 创建 TKE Provider (暂不实现完整功能)。
func New(cfg cluster.ClusterConfig) (*Provider, error) {
	return &Provider{config: cfg}, nil
}

func (p *Provider) Connect(ctx context.Context) error {
	return cluster.NewError(cluster.KindConnectError, "TKE not implemented yet", nil)
}

func (p *Provider) HealthCheck(ctx context.Context) (*cluster.HealthInfo, error) {
	return nil, cluster.NewError(cluster.KindConnectError, "TKE not implemented yet", nil)
}

func (p *Provider) ListNamespaces(ctx context.Context) ([]string, error) {
	return nil, cluster.NewError(cluster.KindConnectError, "TKE not implemented yet", nil)
}

func (p *Provider) ListWorkloads(ctx context.Context, namespace string) ([]cluster.WorkloadInfo, error) {
	return nil, cluster.NewError(cluster.KindConnectError, "TKE not implemented yet", nil)
}

func (p *Provider) GetEvents(ctx context.Context, namespace string, limit int) ([]cluster.EventInfo, error) {
	return nil, cluster.NewError(cluster.KindConnectError, "TKE not implemented yet", nil)
}

func (p *Provider) Deploy(ctx context.Context, spec cluster.DeploySpec) (*cluster.DeployResult, error) {
	return nil, cluster.NewError(cluster.KindDeployError, "TKE not implemented yet", nil)
}

func (p *Provider) Rollback(ctx context.Context, namespace, deployment string, toRevision int64) error {
	return cluster.NewError(cluster.KindDeployError, "TKE not implemented yet", nil)
}

func (p *Provider) GetDeploymentHistory(ctx context.Context, namespace, deployment string) ([]cluster.RevisionInfo, error) {
	return nil, cluster.NewError(cluster.KindDeployError, "TKE not implemented yet", nil)
}
