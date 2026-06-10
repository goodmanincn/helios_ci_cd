// Package tasks 定义 Helios 后台任务的统一类型和 payload schema。
//
// 设计原则:
//   - 所有任务名以 "helios:" 开头,便于 Asynq UI 过滤
//   - payload 用 JSON 编码,字段精简 (只放足够 worker 重建上下文的 ID,具体数据 worker 自己查 DB)
//   - 版本演进:payload 字段只增不删,worker 兼容旧版
//
// 这个包同时被 api (作为 enqueuer) 和 worker (作为 handler) 引用,
// 所以放在 api/pkg 下 (internal 不能跨 module 引用)。
package tasks

import (
	"encoding/json"
	"fmt"
)

// 任务类型常量。
const (
	// TypeGitCheckout 拉代码到 workspace。
	// 触发: webhook 收到 push 后,run 落库 status=pending,立刻入队。
	// Handler: worker/internal/handler/checkout.go
	TypeGitCheckout = "helios:git:checkout"
)

// GitCheckoutPayload helios:git:checkout 的 payload。
type GitCheckoutPayload struct {
	RunID     int64  `json:"run_id"`
	ProjectID int64  `json:"project_id"`
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"` // 可选,空则用 branch HEAD
}

// Validate 基本字段校验。
func (p *GitCheckoutPayload) Validate() error {
	if p.RunID <= 0 {
		return fmt.Errorf("run_id required")
	}
	if p.ProjectID <= 0 {
		return fmt.Errorf("project_id required")
	}
	if p.RepoURL == "" {
		return fmt.Errorf("repo_url required")
	}
	if p.Branch == "" {
		return fmt.Errorf("branch required")
	}
	return nil
}

// Marshal 序列化为 JSON bytes,用于 Asynq Task。
func (p *GitCheckoutPayload) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalGitCheckout 从 Asynq Task.Payload() 反序列化。
func UnmarshalGitCheckout(data []byte) (*GitCheckoutPayload, error) {
	var p GitCheckoutPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal git_checkout: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid git_checkout payload: %w", err)
	}
	return &p, nil
}

// 队列名常量 (Asynq 支持优先级队列)。
const (
	QueueDefault  = "default"  // 普通业务任务
	QueueCritical = "critical" // 关键路径 (审批超时等)
	QueueLow      = "low"      // 后台清理 (日志归档等)
)
