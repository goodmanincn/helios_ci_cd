// Package handler 实现 Asynq 任务处理器。
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/runrepo"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	"github.com/helios-cicd/helios/worker/internal/gitrunner"
)

// CheckoutHandler 处理 helios:git:checkout 任务。
//
// 步骤:
//  1. 解析 payload
//  2. 校验 run 存在且 status == pending
//  3. mark running
//  4. 准备 workspace 目录 (清理已存在的, 防重试残留)
//  5. git clone (depth=1, --branch, 可选 checkout commit)
//  6. 失败 → mark failed; 成功 → 留 status=running, 同时入队 build task
type CheckoutHandler struct {
	repo         *runrepo.Repo
	cloner       gitrunner.Cloner
	workspaceDir string // 例如 /tmp/helios/runs
	enq          BuildEnqueuer
}

// BuildEnqueuer 仅声明 checkout 完毕后需要调用的 enqueue 方法,
// 避免依赖整个 queue 包 (减小测试 mock 面)。
type BuildEnqueuer interface {
	EnqueueRunBuild(ctx context.Context, p *tasks.RunBuildPayload) (string, error)
}

// NewCheckout 构造。workspaceDir 必填,各 run 在其下创建子目录。
// enq 用于 checkout 成功后投递 build 任务;传 nil 兼容老调用 (此时不入队 build)。
func NewCheckout(repo *runrepo.Repo, cloner gitrunner.Cloner, workspaceDir string, enq BuildEnqueuer) *CheckoutHandler {
	if workspaceDir == "" {
		workspaceDir = "/tmp/helios/runs"
	}
	return &CheckoutHandler{repo: repo, cloner: cloner, workspaceDir: workspaceDir, enq: enq}
}

// ProcessTask 实现 asynq.Handler 接口。
func (h *CheckoutHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	// 1. payload
	p, err := tasks.UnmarshalGitCheckout(t.Payload())
	if err != nil {
		// 不可重试 (payload 损坏永远不会成功)
		return fmt.Errorf("invalid payload: %w: %w", err, asynq.SkipRetry)
	}

	log.Printf("checkout: run_id=%d project_id=%d repo=%s branch=%s commit=%s",
		p.RunID, p.ProjectID, p.RepoURL, p.Branch, p.CommitSHA)

	// 2. 取 run, 校验存在
	meta, err := h.repo.GetMeta(ctx, p.RunID)
	if err != nil {
		// run 不在 (被删了?) → 跳过, 不重试
		log.Printf("checkout: run %d not found, skip", p.RunID)
		return fmt.Errorf("run lookup: %w: %w", err, asynq.SkipRetry)
	}
	// 已经在 running/终态 → 不重复处理 (重试场景常见)
	if meta.Status != runrepo.StatusPending && meta.Status != runrepo.StatusRunning {
		log.Printf("checkout: run %d in terminal status %s, skip", p.RunID, meta.Status)
		return nil
	}

	// 3. mark running (幂等, 第二次重试不会再转换)
	if _, err := h.repo.MarkRunning(ctx, p.RunID); err != nil {
		return fmt.Errorf("mark running: %w", err) // 可重试 (网络/DB 瞬态)
	}

	// 4. workspace 准备
	wsDir := filepath.Join(h.workspaceDir, fmt.Sprintf("%d", p.RunID), "src")
	if err := os.RemoveAll(wsDir); err != nil {
		_ = h.repo.MarkFailed(ctx, p.RunID, "clean workspace: "+err.Error())
		return fmt.Errorf("clean workspace: %w: %w", err, asynq.SkipRetry)
	}

	// 5. clone
	if err := h.cloner.Clone(ctx, p.RepoURL, p.Branch, p.CommitSHA, wsDir); err != nil {
		// clone 失败可能是网络瞬态, 也可能是凭据/分支不存在 (永久)
		// MVP: 让 asynq 重试 3 次, 全失败后 mark failed (asynq retried_count == max → 走 OnFailure)
		// 这里不直接 mark failed, 让 OnFailure hook 决定 (见 worker main.go)
		return fmt.Errorf("clone: %w", err)
	}

	log.Printf("checkout: run_id=%d cloned to %s", p.RunID, wsDir)

	// 成功后入队 build 任务 (T1.3.2)。入队失败不视为 checkout 失败,
	// 但会写日志 + 把 run 标记 failed 让用户知道, 避免无限挂在 running。
	if h.enq != nil {
		_, eqErr := h.enq.EnqueueRunBuild(ctx, &tasks.RunBuildPayload{
			RunID: p.RunID, ProjectID: p.ProjectID,
		})
		if eqErr != nil {
			log.Printf("checkout: run_id=%d enqueue build failed: %v", p.RunID, eqErr)
			_ = h.repo.MarkFailed(ctx, p.RunID, "enqueue build: "+eqErr.Error())
			return fmt.Errorf("enqueue build: %w: %w", eqErr, asynq.SkipRetry)
		}
		log.Printf("checkout: run_id=%d build task enqueued", p.RunID)
	}
	return nil
}

// OnRetryExhausted 由 worker server 配置, 当任务超过 max retry 后调用。
func (h *CheckoutHandler) OnRetryExhausted(ctx context.Context, t *asynq.Task, err error) {
	var p tasks.GitCheckoutPayload
	if jerr := json.Unmarshal(t.Payload(), &p); jerr != nil {
		log.Printf("OnRetryExhausted: bad payload: %v", jerr)
		return
	}
	reason := fmt.Sprintf("git checkout retried 3 times: %v", err)
	if mErr := h.repo.MarkFailed(ctx, p.RunID, reason); mErr != nil {
		log.Printf("OnRetryExhausted: mark failed run=%d err=%v", p.RunID, mErr)
	}
}
