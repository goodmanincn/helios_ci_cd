// Package handler 中 build.go 实现 helios:run:build 处理器 (T1.3.2 简化执行引擎)。
//
// 设计:
//  1. 解析 payload (run_id, project_id)
//  2. 加载 project (要求 status=running, 取 config.build_command)
//  3. 在 workspace/{run_id}/src 跑 build_command (bash -c, 5 分钟超时, 输出落日志)
//  4. 成功 → MarkSuccess; 失败 → MarkFailed
//
// 取舍:
//  - host bash 执行非沙箱, 仅 MVP 用; E1.4 接 Docker 后整个 handler 切换到容器执行
//  - 没配 build_command 视为 no-op build, 直接 MarkSuccess (用户先建项目占位再补脚本)
//  - 输出日志写 .helios/runs/{run_id}/build.log, 不入库 (步骤日志走 E1.5/M2)
package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// BuildHandler helios:run:build 处理器。
type BuildHandler struct {
	projects     *projectrepo.Repo
	machine      *runstate.Machine
	workspaceDir string
	timeout      time.Duration
}

// NewBuild 构造。workspaceDir 与 checkout handler 必须一致。
func NewBuild(projects *projectrepo.Repo, machine *runstate.Machine, workspaceDir string, timeout time.Duration) *BuildHandler {
	if workspaceDir == "" {
		workspaceDir = "/tmp/helios/runs"
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &BuildHandler{
		projects:     projects,
		machine:      machine,
		workspaceDir: workspaceDir,
		timeout:      timeout,
	}
}

// ProcessTask 实现 asynq.Handler。
func (h *BuildHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalRunBuild(t.Payload())
	if err != nil {
		log.Printf("build: bad payload: %v", err)
		return fmt.Errorf("invalid payload: %w: %w", err, asynq.SkipRetry)
	}

	// 当前状态: 必须是 running (由 checkout MarkRunning 放进来)。
	// 如果状态已经是 success/failed/canceled, 不重试 (重复投递场景)。
	status, err := h.machine.Status(ctx, p.RunID)
	if err != nil {
		if errors.Is(err, runstate.ErrNotFound) {
			log.Printf("build: run %d not found, skip", p.RunID)
			return fmt.Errorf("run gone: %w: %w", err, asynq.SkipRetry)
		}
		return fmt.Errorf("status: %w", err) // 可重试
	}
	if runstate.IsTerminal(status) {
		log.Printf("build: run %d already terminal=%s, skip", p.RunID, status)
		return nil
	}
	if status != runstate.StatusRunning {
		log.Printf("build: run %d unexpected status=%s, skip retry", p.RunID, status)
		return fmt.Errorf("unexpected status %s: %w", status, asynq.SkipRetry)
	}

	// 取 project 配置
	proj, err := h.projects.Get(ctx, p.ProjectID)
	if err != nil {
		_ = h.machine.MarkFailed(ctx, p.RunID, "load project: "+err.Error(), runstate.TransitionOpts{})
		return fmt.Errorf("get project: %w: %w", err, asynq.SkipRetry)
	}

	buildCmd, _ := proj.Config["build_command"].(string)
	buildCmd = strings.TrimSpace(buildCmd)
	wsDir := filepath.Join(h.workspaceDir, fmt.Sprintf("%d", p.RunID), "src")

	// 没配命令 → no-op success (允许用户先建项目占位)
	if buildCmd == "" {
		log.Printf("build: run_id=%d no build_command configured, mark success (no-op)", p.RunID)
		opts := runstate.TransitionOpts{
			Reason:    "no build_command configured (no-op)",
			ProjectID: &p.ProjectID,
		}
		if err := h.machine.MarkSuccess(ctx, p.RunID, opts); err != nil {
			return fmt.Errorf("mark success: %w", err)
		}
		return nil
	}

	// workspace 检查 (checkout 失败的话不会到这里, 但稳健起见还是 verify)
	if st, err := os.Stat(wsDir); err != nil || !st.IsDir() {
		reason := fmt.Sprintf("workspace missing: %s", wsDir)
		_ = h.machine.MarkFailed(ctx, p.RunID, reason, runstate.TransitionOpts{ProjectID: &p.ProjectID})
		return fmt.Errorf("%s: %w", reason, asynq.SkipRetry)
	}

	// 执行 build_command
	runCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	logPath := filepath.Join(h.workspaceDir, fmt.Sprintf("%d", p.RunID), "build.log")
	logFile, ferr := os.Create(logPath)
	if ferr != nil {
		_ = h.machine.MarkFailed(ctx, p.RunID, "open log: "+ferr.Error(),
			runstate.TransitionOpts{ProjectID: &p.ProjectID})
		return fmt.Errorf("open log: %w: %w", ferr, asynq.SkipRetry)
	}
	defer logFile.Close()

	log.Printf("build: run_id=%d cmd=%q wsDir=%s log=%s", p.RunID, buildCmd, wsDir, logPath)

	cmd := exec.CommandContext(runCtx, "bash", "-c", buildCmd)
	cmd.Dir = wsDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HELIOS_RUN_ID=%d", p.RunID),
		fmt.Sprintf("HELIOS_PROJECT_ID=%d", p.ProjectID),
		"HELIOS_WORKSPACE="+wsDir,
	)

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	// 如果是 context timeout, 单独标记
	if runCtx.Err() == context.DeadlineExceeded {
		reason := fmt.Sprintf("build timeout after %s (exit=%d)", h.timeout, exitCode)
		_ = h.machine.MarkFailed(ctx, p.RunID, reason,
			runstate.TransitionOpts{ProjectID: &p.ProjectID, Extra: map[string]any{"exit_code": exitCode, "log_path": logPath}})
		return fmt.Errorf("%s: %w", reason, asynq.SkipRetry)
	}

	opts := runstate.TransitionOpts{
		ProjectID: &p.ProjectID,
		Extra: map[string]any{
			"exit_code": exitCode,
			"log_path":  logPath,
			"command":   buildCmd,
		},
	}
	if runErr != nil {
		opts.Reason = fmt.Sprintf("build_command exited %d: %v", exitCode, runErr)
		if mErr := h.machine.MarkFailed(ctx, p.RunID, opts.Reason, opts); mErr != nil {
			return fmt.Errorf("mark failed: %w", mErr)
		}
		log.Printf("build: run_id=%d FAILED exit=%d", p.RunID, exitCode)
		return nil // 不重试 (用户命令失败不是基础设施错误)
	}

	opts.Reason = "build_command succeeded"
	if mErr := h.machine.MarkSuccess(ctx, p.RunID, opts); mErr != nil {
		return fmt.Errorf("mark success: %w", mErr)
	}
	log.Printf("build: run_id=%d SUCCESS", p.RunID)
	return nil
}
