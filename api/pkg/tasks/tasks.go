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

	// TypeWebhookRegister 在目标仓库上自动注册 webhook。
	// 触发: 项目创建成功后 handler 入队 (异步,不阻塞 HTTP)。
	// Handler: worker/internal/handler/registerwebhook.go
	TypeWebhookRegister = "helios:project:webhook-register"

	// TypeRunBuild T1.3.2 简化执行引擎: 在 checkout 后的 workspace 跑 project.build_command。
	// MVP 阶段直接 host bash 执行, E1.4 接 Docker 后替换为容器执行。
	TypeRunBuild = "helios:run:build"

	// TypeApprovalTimeout 审批超时 (T2.6.3)。
	// 触发: ApprovalService.Create 时若 stage.Timeout 非空入延时任务 (asynq.ProcessIn(d))。
	// Handler: worker/internal/handler/approval_timeout.go,按 on_timeout 三策略 reject/approve/pause。
	// 幂等: handler 第一行 SELECT FOR UPDATE, 非 pending 直接 no-op (Approve/Reject 抢先时)。
	TypeApprovalTimeout = "helios:approval:timeout"

	// TypeRunOrchestrate 多 stage 流水线调度 tick (T2.2.4)。
	// 触发: checkout 成功后 (pipeline 有 stages) / stage 完成 / 审批通过后。
	// Handler: worker/internal/handler/orchestrate.go — bootstrap + NextReady + 派发 stage 任务。
	TypeRunOrchestrate = "helios:run:orchestrate"

	// TypeStageExecute 执行单个 stage (T2.2.4)。
	// 触发: orchestrate 从 Scheduler.NextReady 派发。
	// Handler: worker/internal/handler/stage_execute.go — 跑 steps / builtin, 完成后 re-enqueue orchestrate。
	TypeStageExecute = "helios:stage:execute"
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

// WebhookRegisterPayload helios:project:webhook-register 的 payload。
// owner/repo 从 repo_url 提取传入,减少 worker 解析负担。
type WebhookRegisterPayload struct {
	ProjectID int64  `json:"project_id"`
	RepoURL   string `json:"repo_url"`  // 原始 repo URL,worker 可选二次校验
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
}

func (p *WebhookRegisterPayload) Validate() error {
	if p.ProjectID <= 0 {
		return fmt.Errorf("project_id required")
	}
	if p.Owner == "" || p.Repo == "" {
		return fmt.Errorf("owner and repo required")
	}
	return nil
}

func (p *WebhookRegisterPayload) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

func UnmarshalWebhookRegister(data []byte) (*WebhookRegisterPayload, error) {
	var p WebhookRegisterPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal webhook_register: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid webhook_register payload: %w", err)
	}
	return &p, nil
}

// RunBuildPayload helios:run:build 的 payload。
// 引擎在 checkout 成功后入队,build handler 读取 project.config.build_command 在 workspace 跑命令。
type RunBuildPayload struct {
	RunID     int64 `json:"run_id"`
	ProjectID int64 `json:"project_id"`
}

func (p *RunBuildPayload) Validate() error {
	if p.RunID <= 0 {
		return fmt.Errorf("run_id required")
	}
	if p.ProjectID <= 0 {
		return fmt.Errorf("project_id required")
	}
	return nil
}

func (p *RunBuildPayload) Marshal() ([]byte, error) { return json.Marshal(p) }

func UnmarshalRunBuild(data []byte) (*RunBuildPayload, error) {
	var p RunBuildPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal run_build: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid run_build payload: %w", err)
	}
	return &p, nil
}

// 队列名常量 (Asynq 支持优先级队列)。
const (
	QueueDefault  = "default"  // 普通业务任务
	QueueCritical = "critical" // 关键路径 (审批超时等)
	QueueLow      = "low"      // 后台清理 (日志归档等)
)

// ApprovalTimeoutPayload helios:approval:timeout 的 payload.
//
// 只带 RequestID; worker handler 自己查 approval_requests / runs 拿剩余信息.
// 这样 enqueue 端不依赖任何业务状态, 便于幂等触发.
type ApprovalTimeoutPayload struct {
	RequestID int64 `json:"request_id"`
}

func (p *ApprovalTimeoutPayload) Validate() error {
	if p.RequestID <= 0 {
		return fmt.Errorf("request_id required")
	}
	return nil
}

func (p *ApprovalTimeoutPayload) Marshal() ([]byte, error) { return json.Marshal(p) }

func UnmarshalApprovalTimeout(data []byte) (*ApprovalTimeoutPayload, error) {
	var p ApprovalTimeoutPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal approval_timeout: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid approval_timeout payload: %w", err)
	}
	return &p, nil
}

// RunOrchestratePayload helios:run:orchestrate 的 payload。
type RunOrchestratePayload struct {
	RunID     int64 `json:"run_id"`
	ProjectID int64 `json:"project_id"`
}

func (p *RunOrchestratePayload) Validate() error {
	if p.RunID <= 0 {
		return fmt.Errorf("run_id required")
	}
	if p.ProjectID <= 0 {
		return fmt.Errorf("project_id required")
	}
	return nil
}

func (p *RunOrchestratePayload) Marshal() ([]byte, error) { return json.Marshal(p) }

func UnmarshalRunOrchestrate(data []byte) (*RunOrchestratePayload, error) {
	var p RunOrchestratePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal run_orchestrate: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid run_orchestrate payload: %w", err)
	}
	return &p, nil
}

// StageExecutePayload helios:stage:execute 的 payload。
type StageExecutePayload struct {
	RunID         int64  `json:"run_id"`
	ProjectID     int64  `json:"project_id"`
	StageRecordID int64  `json:"stage_record_id"` // stages.id
	StageID       string `json:"stage_id"`        // DSL stage id (含 matrix 后缀)
}

func (p *StageExecutePayload) Validate() error {
	if p.RunID <= 0 {
		return fmt.Errorf("run_id required")
	}
	if p.ProjectID <= 0 {
		return fmt.Errorf("project_id required")
	}
	if p.StageRecordID <= 0 {
		return fmt.Errorf("stage_record_id required")
	}
	if p.StageID == "" {
		return fmt.Errorf("stage_id required")
	}
	return nil
}

func (p *StageExecutePayload) Marshal() ([]byte, error) { return json.Marshal(p) }

func UnmarshalStageExecute(data []byte) (*StageExecutePayload, error) {
	var p StageExecutePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal stage_execute: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid stage_execute payload: %w", err)
	}
	return &p, nil
}
