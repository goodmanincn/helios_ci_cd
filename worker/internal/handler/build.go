// Package handler 中 build.go 实现 helios:run:build 处理器 (T1.3.2 简化执行引擎 + T1.4.4 Docker runtime)。
//
// 设计:
//  1. 解析 payload (run_id, project_id)
//  2. 加载 project (要求 status=running, 取 config.build_command / build_image)
//  3. 按 runtime 跑 build_command:
//     - host (默认): bash -c 在 host 跑 (E1.3 MVP 路径, 非沙箱)
//     - docker: 在容器内跑, workspace bind mount 到 /workspace
//  4. 成功 → MarkSuccess; 失败 → MarkFailed
//
// runtime 选择:
//   - NewBuild 构造时传入(由 worker main 按 HELIOS_BUILD_RUNTIME 决定)
//   - host 路径保留, 用于无 docker 环境 / 单测
//   - docker 路径用 dockerrun.Executor; image 取 project.config.build_image, 缺省 alpine:latest
//
// 取舍:
//   - 没配 build_command → no-op success (允许占位)
//   - 输出日志写 .helios/runs/{run_id}/build.log (host+docker 共用)
//   - docker 路径 PullPolicy=missing (本地有就不拉)
package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/logstream"
	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	"github.com/helios-cicd/helios/worker/internal/dockerrun"
)

// BuildRuntime 选择 build_command 的执行方式。
type BuildRuntime string

const (
	BuildRuntimeHost   BuildRuntime = "host"
	BuildRuntimeDocker BuildRuntime = "docker"
)

// DefaultBuildImage docker runtime 下 project.config 没配 build_image 时的默认值。
const DefaultBuildImage = "alpine:latest"

// BuildHandler helios:run:build 处理器。
type BuildHandler struct {
	projects     *projectrepo.Repo
	machine      *runstate.Machine
	workspaceDir string
	timeout      time.Duration

	runtime  BuildRuntime
	executor *dockerrun.Executor // runtime=docker 时必填; runtime=host 时可 nil

	logs    *logstream.Writer  // T1.5.1: 双写 Redis Stream, nil 时仅写本地 build.log
	archive *logarchive.Service // T1.5.3: 终态后归档 Redis Stream → 持久存储 + 删 Redis; nil 跳过
}

// BuildOption 配置项。
type BuildOption func(*BuildHandler)

// WithDockerRuntime 切换到 docker runtime, 需传入已建好的 executor。
func WithDockerRuntime(ex *dockerrun.Executor) BuildOption {
	return func(h *BuildHandler) {
		h.runtime = BuildRuntimeDocker
		h.executor = ex
	}
}

// WithLogStream 注入日志流写入器, build 时每行 stdout/stderr 双写 Redis Stream + build.log。
// 不传 → 只写本地 build.log (向后兼容)。
func WithLogStream(w *logstream.Writer) BuildOption {
	return func(h *BuildHandler) { h.logs = w }
}

// WithLogArchive 注入日志归档服务, run 终态后 best-effort 把 Redis Stream 序列化到持久存储并删 Redis。
// 不传 → 不归档 (Redis Stream 留着, 受 MAXLEN 限制).
func WithLogArchive(s *logarchive.Service) BuildOption {
	return func(h *BuildHandler) { h.archive = s }
}

// NewBuild 构造。workspaceDir 与 checkout handler 必须一致。
// 默认 runtime=host, 用 WithDockerRuntime 切到 docker。
func NewBuild(projects *projectrepo.Repo, machine *runstate.Machine, workspaceDir string, timeout time.Duration, opts ...BuildOption) *BuildHandler {
	if workspaceDir == "" {
		workspaceDir = "/tmp/helios/runs"
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	h := &BuildHandler{
		projects:     projects,
		machine:      machine,
		workspaceDir: workspaceDir,
		timeout:      timeout,
		runtime:      BuildRuntimeHost,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// ProcessTask 实现 asynq.Handler。
func (h *BuildHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalRunBuild(t.Payload())
	if err != nil {
		log.Printf("build: bad payload: %v", err)
		return fmt.Errorf("invalid payload: %w: %w", err, asynq.SkipRetry)
	}

	// 当前状态: 必须是 running (由 checkout MarkRunning 放进来)。
	status, err := h.machine.Status(ctx, p.RunID)
	if err != nil {
		if errors.Is(err, runstate.ErrNotFound) {
			log.Printf("build: run %d not found, skip", p.RunID)
			return fmt.Errorf("run gone: %w: %w", err, asynq.SkipRetry)
		}
		return fmt.Errorf("status: %w", err)
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

	// 没配命令 → no-op success
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

	// workspace 检查
	if st, err := os.Stat(wsDir); err != nil || !st.IsDir() {
		reason := fmt.Sprintf("workspace missing: %s", wsDir)
		_ = h.machine.MarkFailed(ctx, p.RunID, reason, runstate.TransitionOpts{ProjectID: &p.ProjectID})
		return fmt.Errorf("%s: %w", reason, asynq.SkipRetry)
	}

	// 打开日志文件
	logPath := filepath.Join(h.workspaceDir, fmt.Sprintf("%d", p.RunID), "build.log")
	logFile, ferr := os.Create(logPath)
	if ferr != nil {
		_ = h.machine.MarkFailed(ctx, p.RunID, "open log: "+ferr.Error(),
			runstate.TransitionOpts{ProjectID: &p.ProjectID})
		return fmt.Errorf("open log: %w: %w", ferr, asynq.SkipRetry)
	}
	defer logFile.Close()

	runCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	// T1.5.1 双写日志: build.log + Redis Stream (Writer 为 nil 时 sink 自动跳过).
	if h.logs != nil {
		h.logs.AppendSystem(ctx, p.RunID,
			fmt.Sprintf("build runtime=%s cmd=%q", h.runtime, buildCmd))
	}

	var exitCode int
	var timedOut bool
	var image string

	switch h.runtime {
	case BuildRuntimeDocker:
		image, _ = proj.Config["build_image"].(string)
		image = strings.TrimSpace(image)
		if image == "" {
			image = DefaultBuildImage
		}
		log.Printf("build: run_id=%d runtime=docker image=%s cmd=%q wsDir=%s log=%s",
			p.RunID, image, buildCmd, wsDir, logPath)
		if h.logs != nil {
			h.logs.AppendSystem(ctx, p.RunID, "docker image="+image)
		}
		exitCode, timedOut, err = h.runDocker(runCtx, p, buildCmd, wsDir, image, logFile)

	default: // host
		log.Printf("build: run_id=%d runtime=host cmd=%q wsDir=%s log=%s",
			p.RunID, buildCmd, wsDir, logPath)
		exitCode, timedOut, err = h.runHost(runCtx, p, buildCmd, wsDir, logFile)
	}

	// 超时 → failed (不重试)
	if timedOut {
		reason := fmt.Sprintf("build timeout after %s (exit=%d)", h.timeout, exitCode)
		extra := map[string]any{"exit_code": exitCode, "log_path": logPath, "runtime": string(h.runtime)}
		if image != "" {
			extra["image"] = image
		}
		_ = h.machine.MarkFailed(ctx, p.RunID, reason,
			runstate.TransitionOpts{ProjectID: &p.ProjectID, Extra: extra})
		return fmt.Errorf("%s: %w", reason, asynq.SkipRetry)
	}

	opts := runstate.TransitionOpts{
		ProjectID: &p.ProjectID,
		Extra: map[string]any{
			"exit_code": exitCode,
			"log_path":  logPath,
			"command":   buildCmd,
			"runtime":   string(h.runtime),
		},
	}
	if image != "" {
		opts.Extra["image"] = image
	}

	if err != nil || exitCode != 0 {
		var reason string
		if err != nil {
			reason = fmt.Sprintf("build failed exit=%d: %v", exitCode, err)
		} else {
			reason = fmt.Sprintf("build_command exited %d", exitCode)
		}
		opts.Reason = reason
		if mErr := h.machine.MarkFailed(ctx, p.RunID, reason, opts); mErr != nil {
			return fmt.Errorf("mark failed: %w", mErr)
		}
		log.Printf("build: run_id=%d FAILED exit=%d", p.RunID, exitCode)
		if h.logs != nil {
			h.logs.AppendSystem(ctx, p.RunID, "[FAILED] "+reason)
		}
		h.archiveBestEffort(ctx, p.RunID)
		return nil // 不重试: user-command 错
	}

	opts.Reason = "build_command succeeded"
	if mErr := h.machine.MarkSuccess(ctx, p.RunID, opts); mErr != nil {
		return fmt.Errorf("mark success: %w", mErr)
	}
	log.Printf("build: run_id=%d SUCCESS", p.RunID)
	if h.logs != nil {
		h.logs.AppendSystem(ctx, p.RunID, "[SUCCESS] build_command succeeded")
	}
	h.archiveBestEffort(ctx, p.RunID)
	return nil
}

// archiveBestEffort 把 Redis Stream 序列化到持久存储并删 Redis. 任何错只 log, 不阻断 run.
// 用独立 timeout context, 不受 build runCtx 取消影响.
func (h *BuildHandler) archiveBestEffort(parent context.Context, runID int64) {
	if h.archive == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stat, err := h.archive.ArchiveAndDrop(ctx, runID)
	if err != nil {
		log.Printf("logarchive: run=%d failed: %v", runID, err)
		return
	}
	if stat.Skipped {
		log.Printf("logarchive: run=%d skipped (empty stream)", runID)
		return
	}
	log.Printf("logarchive: run=%d archived %d entries → %s (%s)",
		runID, stat.Entries, stat.Key, stat.Backend)
}

// runHost 走 host bash -c (E1.3 MVP 路径)。
// 返回 (exitCode, timedOut, infraErr)。
// infraErr 非 nil 表示无法启动(如 fork 失败),会被上层 MarkFailed。
func (h *BuildHandler) runHost(ctx context.Context, p *tasks.RunBuildPayload, buildCmd, wsDir string, logFile io.Writer) (int, bool, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", buildCmd)
	cmd.Dir = wsDir
	// T1.5.1: stdout/stderr 分别走 logFile + Redis Stream (按行切).
	if h.logs != nil {
		cmd.Stdout = io.MultiWriter(logFile, newLineStreamWriter(ctx, h.logs, p.RunID, logstream.StreamStdout))
		cmd.Stderr = io.MultiWriter(logFile, newLineStreamWriter(ctx, h.logs, p.RunID, logstream.StreamStderr))
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
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
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)

	// 区分 user-command 失败 vs infra 失败:
	// - 启动失败(找不到 bash 等): runErr 不是 *exec.ExitError, exitCode=-1, 返回 err
	// - 命令退出非 0: runErr 是 *exec.ExitError, 返回 nil(让上层按 exitCode 判)
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); ok {
			return exitCode, timedOut, nil
		}
		return exitCode, timedOut, runErr
	}
	return exitCode, false, nil
}

// runDocker 在容器内跑 build_command。workspace bind mount 到 /workspace, workdir=/workspace。
// dockerrun.Executor 已经处理 stdcopy 拆 stdout/stderr, 这里把两条流都拼回 logFile + Redis。
func (h *BuildHandler) runDocker(ctx context.Context, p *tasks.RunBuildPayload, buildCmd, wsDir, image string, logFile io.Writer) (int, bool, error) {
	if h.executor == nil {
		return -1, false, errors.New("docker runtime requires executor (build with WithDockerRuntime)")
	}
	wsAbs, err := filepath.Abs(wsDir)
	if err != nil {
		return -1, false, fmt.Errorf("abs wsDir: %w", err)
	}

	sink := func(l dockerrun.LogLine) error {
		_, _ = fmt.Fprintf(logFile, "%s\n", l.Line)
		// T1.5.1: dockerrun 已按行切, stream 名直接映射 stdout|stderr.
		if h.logs != nil {
			stream := logstream.StreamStdout
			if l.Stream == dockerrun.LogStderr {
				stream = logstream.StreamStderr
			}
			h.logs.Append(ctx, p.RunID, stream, l.Line)
		}
		return nil
	}

	spec := dockerrun.RunSpec{
		Image: image,
		// 用 sh -c 而不是 bash: alpine 默认无 bash; debian/ubuntu sh→dash 也能跑 bash 兼容子集
		Cmd:     []string{"sh", "-c", buildCmd},
		WorkDir: "/workspace",
		Mounts:  map[string]string{wsAbs: "/workspace"},
		Env: []string{
			fmt.Sprintf("HELIOS_RUN_ID=%d", p.RunID),
			fmt.Sprintf("HELIOS_PROJECT_ID=%d", p.ProjectID),
			"HELIOS_WORKSPACE=/workspace",
		},
		PullPolicy:  "missing",
		NamePrefix:  fmt.Sprintf("helios-run-%d-", p.RunID),
		NetworkMode: "bridge",
	}

	res, runErr := h.executor.Run(ctx, spec, sink)
	if res == nil {
		// 启动前阶段错 (pull/create 失败)
		return -1, errors.Is(ctx.Err(), context.DeadlineExceeded), runErr
	}
	if res.TimedOut {
		return res.ExitCode, true, nil
	}
	// runErr 非 nil 且非 timeout → daemon 报错, 视为 infra 错
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
		return res.ExitCode, false, runErr
	}
	return res.ExitCode, false, nil
}

// lineStreamWriter 是 io.Writer, 把传入字节按 \n 切行后 forward 到 logstream.Writer.
// 用于 host 路径 (exec.Cmd.Stdout/Stderr) 的逐行 redis 推送.
// 未换行的尾巴留在 buffer, 下次 Write 拼上; Close 时 flush 剩余.
type lineStreamWriter struct {
	ctx    context.Context
	w      *logstream.Writer
	runID  int64
	stream string
	buf    []byte
}

func newLineStreamWriter(ctx context.Context, w *logstream.Writer, runID int64, stream string) *lineStreamWriter {
	return &lineStreamWriter{ctx: ctx, w: w, runID: runID, stream: stream, buf: make([]byte, 0, 4096)}
}

// Write 实现 io.Writer; 永远返回 len(p), nil (不阻塞 cmd 的 stdout pipe).
func (l *lineStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := l.buf[:i]
		// 去尾部 \r (Windows CRLF)
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		l.w.Append(l.ctx, l.runID, l.stream, string(line))
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}
