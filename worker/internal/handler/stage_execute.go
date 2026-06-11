package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/logstream"
	"github.com/helios-cicd/helios/api/pkg/plugin"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/runengine"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// StageExecuteHandler 处理 helios:stage:execute (T2.2.4)。
type StageExecuteHandler struct {
	db           *sql.DB
	machine      *runstate.Machine
	workspaceDir string
	artifactRoot string
	timeout      time.Duration
	enq          *queue.AsynqEnqueuer
	logs         *logstream.Writer

	// PluginResolver 解析 step.Uses 到 plugin marketplace (M9).
	// nil 时只能用 builtin step.
	pluginResolver plugin.Resolver
}

// NewStageExecute 构造。
func NewStageExecute(db *sql.DB, machine *runstate.Machine, workspaceDir, artifactRoot string, timeout time.Duration, enq *queue.AsynqEnqueuer, logs *logstream.Writer) *StageExecuteHandler {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &StageExecuteHandler{
		db: db, machine: machine, workspaceDir: workspaceDir,
		artifactRoot: artifactRoot, timeout: timeout, enq: enq, logs: logs,
	}
}

// WithPluginResolver 注入插件解析器 (M9). 链式返回自身.
func (h *StageExecuteHandler) WithPluginResolver(r plugin.Resolver) *StageExecuteHandler {
	h.pluginResolver = r
	return h
}

func (h *StageExecuteHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalStageExecute(t.Payload())
	if err != nil {
		return fmt.Errorf("invalid payload: %w: %w", err, asynq.SkipRetry)
	}

	status, err := h.machine.Status(ctx, p.RunID)
	if err != nil {
		return fmt.Errorf("run status: %w", err)
	}
	if runstate.IsTerminal(status) {
		log.Printf("stage_execute: run %d terminal, skip stage %s", p.RunID, p.StageID)
		return nil
	}

	if h.logs != nil {
		h.logs.AppendSystem(ctx, p.RunID, fmt.Sprintf("stage %s starting", p.StageID))
	}

	opts := runengine.ExecuteOpts{
		DB: h.db, WorkspaceDir: h.workspaceDir,
		ArtifactRoot: h.artifactRoot, Timeout: h.timeout,
		PluginResolver: h.pluginResolver,
		// PluginOrgID 留 0: resolver 走"官方 verified 隐式可用"路径.
		// MVP 简化, 真正 multi-tenant 隔离待引入 runs.org_id 列后传入.
	}
	ok, exitCode, execErr := runengine.ExecuteStage(ctx, opts, p.RunID, p.ProjectID, p.StageRecordID, p.StageID)
	if execErr != nil {
		log.Printf("stage_execute: run=%d stage=%s failed: %v", p.RunID, p.StageID, execErr)
	}

	advOpts := runengine.AdvanceOpts{
		DB: h.db, Machine: h.machine, WorkspaceDir: h.workspaceDir,
		StageEnq: h.enq, TimeoutEnq: h.enq,
	}
	if err := runengine.OnStageComplete(ctx, advOpts, p.RunID, p.ProjectID, p.StageID, ok, exitCode); err != nil {
		return fmt.Errorf("on stage complete: %w", err)
	}

	if h.logs != nil {
		msg := fmt.Sprintf("stage %s success", p.StageID)
		if !ok {
			msg = fmt.Sprintf("stage %s failed exit=%d", p.StageID, exitCode)
		}
		h.logs.AppendSystem(ctx, p.RunID, msg)
	}

	if !ok {
		return fmt.Errorf("stage %s failed: %w", p.StageID, execErr)
	}
	return nil
}
