// Package cluster 定义 Helios 与 K8s 集群交互的抽象。
//
// 设计原则:
//   - Provider 是接口,自建/TKE/ACK 等不同后端分别实现
//   - 所有操作带 context,超时由 caller 控制
//   - 错误类型分类,便于 handler 映射 HTTP 状态码
//   - Connect() 是幂等的,多次调用返回同一个 clientset
package cluster

import (
	"context"
	"fmt"
	"time"
)

// Provider 集群操作接口。
type Provider interface {
	// Connect 建立连接并返回可用性。
	// 幂等: 已连接时直接返回健康状态,不重复建连。
	Connect(ctx context.Context) error

	// HealthCheck 轻量探测,返回版本/节点数/命名空间数。
	HealthCheck(ctx context.Context) (*HealthInfo, error)

	// ListNamespaces 返回所有 namespace 名称。
	ListNamespaces(ctx context.Context) ([]string, error)

	// ListWorkloads 返回指定 namespace 下的工作负载概要。
	ListWorkloads(ctx context.Context, namespace string) ([]WorkloadInfo, error)

	// GetEvents 返回最近事件(默认 100 条)。
	GetEvents(ctx context.Context, namespace string, limit int) ([]EventInfo, error)

	// Deploy 应用 manifest,支持 server-side apply。
	// manifestBytes 可含多文档(--- 分隔)。
	Deploy(ctx context.Context, spec DeploySpec) (*DeployResult, error)

	// Rollback 回滚 Deployment 到指定 revision。
	Rollback(ctx context.Context, namespace, deployment string, toRevision int64) error

	// GetDeploymentHistory 返回 Deployment 的 revision 历史。
	GetDeploymentHistory(ctx context.Context, namespace, deployment string) ([]RevisionInfo, error)
}

// ClusterConfig 连接集群所需配置。
type ClusterConfig struct {
	Name       string
	Provider   string // selfhosted / tke / ack / eks / gke / aks
	Endpoint   string // 可选,自建集群从 kubeconfig 解析
	Kubeconfig []byte // 自建集群必填
	Token      string // 云厂商场景
	Region     string
}

// HealthCheck 返回的集群健康信息。
type HealthInfo struct {
	Version        string
	NodeCount      int
	NamespaceCount int
	Healthy        bool
}

// WorkloadInfo 工作负载概要。
type WorkloadInfo struct {
	Kind      string // Deployment / StatefulSet / DaemonSet
	Name      string
	Namespace string
	Ready     int
	Desired   int
	Updated   int
	Status    string // healthy / progressing / degraded
}

// EventInfo K8s 事件。
type EventInfo struct {
	Type      string    // Normal / Warning
	Reason    string
	Message   string
	Object    string
	Timestamp time.Time
}

// DeploySpec 部署参数。
type DeploySpec struct {
	Namespace   string
	Manifest    []byte // YAML/JSON 多文档
	Image       string // 如需替换镜像
	Strategy    string // rolling / recreate
	WaitTimeout time.Duration
	Labels      map[string]string // 注入的 labels (如 helios.io/run-id)
}

// DeployResult 部署结果。
type DeployResult struct {
	DeploymentName string
	ReadyReplicas  int
	DesiredReplicas int
	Revision       int64
}

// RevisionInfo 部署 revision。
type RevisionInfo struct {
	Revision int64
	Image    string
	Status   string
	CreatedAt time.Time
}

// ---- 错误类型 ----

type ErrorKind string

const (
	KindConnectError ErrorKind = "connect"
	KindDeployError  ErrorKind = "deploy"
	KindTimeout      ErrorKind = "timeout"
	KindNotFound     ErrorKind = "not_found"
	KindInvalidInput ErrorKind = "invalid_input"
)

// Error 集群操作错误。
type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func NewError(kind ErrorKind, msg string, cause error) *Error {
	return &Error{Kind: kind, Message: msg, Cause: cause}
}
