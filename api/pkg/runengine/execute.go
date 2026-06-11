package runengine

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/helios-cicd/helios/api/pkg/artifact"
	"github.com/helios-cicd/helios/api/pkg/builtin"
	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/engine"
	"github.com/helios-cicd/helios/api/pkg/plugin"
)

// ExecuteOpts stage 执行依赖。
type ExecuteOpts struct {
	DB           *sql.DB
	WorkspaceDir string
	ArtifactRoot string
	Timeout      time.Duration
	LogWriter    io.Writer // 可选: 追加到 stage 日志

	// PluginResolver: 解析 step.Uses 的"<ns>/<name>@<ver>" 引用 (M9).
	// 为 nil 时插件路径走 builtin 注册表 fallback, 不命中则报错.
	PluginResolver plugin.Resolver
	// PluginOrgID: Resolve 时传入的 org 隔离 ID. <=0 时不限 org (仅查官方 verified).
	PluginOrgID int64
}

// ExecuteStage 执行单个 stage 的全部 steps。
func ExecuteStage(ctx context.Context, opts ExecuteOpts, runID, projectID, stageRecordID int64, stageID string) (success bool, exitCode int, err error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	run, err := LoadRun(ctx, opts.DB, runID)
	if err != nil {
		return false, 1, err
	}
	raw, err := LoadPipelineSpec(ctx, opts.DB, run.VersionID)
	if err != nil {
		return false, 1, err
	}
	p, err := parsePipeline(raw)
	if err != nil {
		return false, 1, err
	}
	expanded, err := engine.ExpandPipeline(p)
	if err != nil {
		return false, 1, err
	}
	var dslStage *dsl.Stage
	for i := range expanded.Stages {
		if expanded.Stages[i].ID == stageID {
			dslStage = &expanded.Stages[i]
			break
		}
	}
	if dslStage == nil {
		return false, 1, fmt.Errorf("stage %s not in pipeline spec", stageID)
	}
	if dslStage.Type == "approval" {
		return true, 0, nil
	}

	outputsFile := NewOutputsFile(opts.WorkspaceDir, runID)
	stageOutputs, _ := outputsFile.Snapshot()
	baseCtx := BuildExprContext(run, p, stageOutputs)
	rendered, renderErrs := dsl.RenderStage(dslStage, baseCtx, "stages."+stageID)
	if len(renderErrs) > 0 {
		return false, 1, fmt.Errorf("render stage: %v", renderErrs[0])
	}
	dslStage = &rendered

	wsDir := filepath.Join(opts.WorkspaceDir, fmt.Sprintf("%d", runID), "src")
	if st, err := os.Stat(wsDir); err != nil || !st.IsDir() {
		return false, 1, fmt.Errorf("workspace missing: %s", wsDir)
	}

	stepRows, err := ListSteps(ctx, opts.DB, stageRecordID)
	if err != nil {
		return false, 1, err
	}

	storage := artifact.NewLocalFS(opts.ArtifactRoot)
	stepOutputs := map[string]map[string]any{}

	steps := dslStage.Steps
	if dslStage.Uses != "" && len(steps) == 0 {
		steps = []dsl.Step{{Name: dslStage.Uses, Uses: dslStage.Uses, With: dslStage.With}}
	}

	for i, step := range steps {
		var stepRowID int64
		if i < len(stepRows) {
			stepRowID = stepRows[i].ID
		}
		_ = UpdateStepStatus(ctx, opts.DB, stepRowID, "running", nil)

		ec, stepOut, runErr := runStep(ctx, opts, run, stageID, wsDir, storage, &step, dslStage.Secrets)
		if runErr != nil {
			_ = UpdateStepStatus(ctx, opts.DB, stepRowID, "failed", &ec)
			return false, ec, runErr
		}
		_ = UpdateStepStatus(ctx, opts.DB, stepRowID, "success", &ec)
		if step.ID != "" && len(stepOut) > 0 {
			stepOutputs[step.ID] = stepOut
		} else if step.Name != "" && len(stepOut) > 0 {
			stepOutputs[step.Name] = stepOut
		}
	}

	outs, _ := engine.ResolveStageOutputs(dslStage, stepOutputs, baseCtx)
	if len(outs) > 0 {
		_ = outputsFile.Set(stageID, outs)
	}
	return true, 0, nil
}

func runStep(ctx context.Context, opts ExecuteOpts, run *RunInfo, stageID, wsDir string, storage artifact.Storage, step *dsl.Step, stageSecrets []string) (exitCode int, outputs map[string]any, err error) {
	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	if step.Uses != "" {
		if b, ok := builtin.Lookup(step.Uses); ok {
			inputs := map[string]any{}
			for k, v := range step.With {
				inputs[k] = v
			}
			var logBuf bytes.Buffer
			logW := io.Writer(&logBuf)
			if opts.LogWriter != nil {
				logW = io.MultiWriter(&logBuf, opts.LogWriter)
			}
			execCtx := builtin.ExecContext{
				Ctx: runCtx, RunID: run.ID, StageID: stageID, WorkDir: wsDir,
				Storage: storage, Log: logW,
			}
			out, err := b.Run(&execCtx, inputs)
			if err != nil {
				return 1, nil, err
			}
			return 0, out, nil
		}
		// 不在 builtin 注册表 → 走 plugin marketplace 解析
		if opts.PluginResolver == nil {
			return 1, nil, fmt.Errorf("unknown uses %q (no builtin, no plugin resolver wired)", step.Uses)
		}
		return runPlugin(runCtx, opts, run, stageID, wsDir, step, stageSecrets)
	}

	cmdStr := strings.TrimSpace(step.Run)
	if cmdStr == "" {
		return 0, nil, nil
	}

	logPath := filepath.Join(opts.WorkspaceDir, fmt.Sprintf("%d", run.ID), "stages", stageID+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if ferr != nil {
		return 1, nil, ferr
	}
	defer logFile.Close()

	cmd := exec.CommandContext(runCtx, "bash", "-c", cmdStr)
	cmd.Dir = wsDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HELIOS_RUN_ID=%d", run.ID),
		fmt.Sprintf("HELIOS_PROJECT_ID=%d", run.ProjectID),
		"HELIOS_WORKSPACE="+wsDir,
		"HELIOS_STAGE_ID="+stageID,
	)

	runErr := cmd.Run()
	exitCode = 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			return exitCode, nil, runErr
		}
	}
	if exitCode != 0 {
		return exitCode, nil, fmt.Errorf("step exited %d", exitCode)
	}
	return 0, nil, nil
}

// runPlugin 把 step.Uses 解析成 plugin.Resolved 后执行 (M9).
//
// 执行策略 (按 action.runs.using 分支):
//   - container: shell out 到 `docker run` (host 必须有 docker CLI); 把 inputs 转 INPUT_*
//                环境变量; workspace 挂到 /github/workspace; stdout 收集 ::set-output 协议
//   - composite: 本轮不支持, 返显式错 (留 E9.1.4 实现)
//   - javascript: 不支持
//
// 安全:
//   - action.NeedsSecrets 中每一项必须由 stage.secrets[] 或 step.with[] 显式提供,
//     否则拒绝执行 (T9.1.5). 这把"插件想偷偷读 env 里的 secret"挡掉了 — 必须用户主动授权.
//
// 出错路径:
//   - Resolver 报 ErrNotInstalled / 版本不匹配 → 直接返 user-friendly error
//   - docker run 退出非 0 → exit code 透出
func runPlugin(ctx context.Context, opts ExecuteOpts, run *RunInfo, stageID, wsDir string, step *dsl.Step, stageSecrets []string) (int, map[string]any, error) {
	ref, err := plugin.ParseRef(step.Uses)
	if err != nil {
		return 1, nil, err
	}
	resolved, err := opts.PluginResolver.Resolve(ctx, opts.PluginOrgID, ref)
	if err != nil {
		return 1, nil, err
	}
	action := resolved.Action

	// 强制 needs_secrets 授权 (T9.1.5)
	if missing := checkSecretsAuthorized(action.NeedsSecrets, stageSecrets, step.With); len(missing) > 0 {
		return 1, nil, fmt.Errorf("plugin %s@%s: needs_secrets not authorized — declare in stage.secrets[] or step.with[]: missing %v",
			resolved.Slug, resolved.Version, missing)
	}

	switch action.Runs.Using {
	case "container":
		return runPluginContainer(ctx, opts, run, stageID, wsDir, resolved, step)
	case "composite":
		return 1, nil, fmt.Errorf("plugin %s@%s uses=composite: composite plugins not yet supported (MVP, see E9.1.4)",
			resolved.Slug, resolved.Version)
	default:
		return 1, nil, fmt.Errorf("plugin %s@%s uses=%q not supported",
			resolved.Slug, resolved.Version, action.Runs.Using)
	}
}

// checkSecretsAuthorized 返回 action.needs_secrets 中, 没在 stageSecrets 或 stepWith 出现的项.
//
// 匹配规则:
//   - stage.secrets[] 是 NAME 列表 — 大小写敏感; 命中即视为授权
//   - step.with[] 是 key→value map — 把 KEY 拿出来跟 needs_secrets 做不区分大小写匹配
//     (用户在 with 里通常写 webhook: ${{ secrets.DINGTALK_WEBHOOK }}, key 是小写, 但 expr 引用了 secret)
func checkSecretsAuthorized(needs []string, stageSecrets []string, stepWith map[string]any) []string {
	if len(needs) == 0 {
		return nil
	}
	provided := make(map[string]bool, len(stageSecrets)+len(stepWith))
	for _, s := range stageSecrets {
		provided[s] = true
		provided[strings.ToUpper(s)] = true
	}
	for k := range stepWith {
		provided[k] = true
		provided[strings.ToUpper(k)] = true
	}
	var missing []string
	for _, n := range needs {
		if provided[n] || provided[strings.ToUpper(n)] {
			continue
		}
		missing = append(missing, n)
	}
	return missing
}

// runPluginContainer 跑容器化插件. 使用 `docker run` 二进制 (host 必须有).
//
// inputs → 环境变量:  INPUT_<UPPER_NAME>=value
// outputs 协议:       子进程 stdout 写  ::set-output name=X::value
//
// 挂载:
//   - workspace 双绑到 /github/workspace 与 wsDir (后者保 HELIOS_WORKSPACE env 一致)
//
// 这里走 CLI 而不是 Docker daemon SDK, 是为了让 runengine 不强依赖 docker SDK
// (runengine 同时被 handler 测试构建; SDK 会拖入很重的依赖链). 真实生产用 worker 端
// dockerrun.Executor 是另一码事.
func runPluginContainer(ctx context.Context, opts ExecuteOpts, run *RunInfo, stageID, wsDir string, resolved *plugin.Resolved, step *dsl.Step) (int, map[string]any, error) {
	action := resolved.Action
	if _, err := exec.LookPath("docker"); err != nil {
		return 1, nil, fmt.Errorf("plugin %s: docker CLI not found on host (required for container plugins)",
			resolved.Slug)
	}

	containerWsDir := "/github/workspace"
	dockerArgs := []string{"run", "--rm"}
	// 镜像拉取策略: action.runs.pull_policy → docker --pull
	// 支持值: Always / IfNotPresent (默认) / Never. docker --pull 接 always|missing|never.
	switch strings.ToLower(action.Runs.PullPolicy) {
	case "always":
		dockerArgs = append(dockerArgs, "--pull", "always")
	case "never":
		dockerArgs = append(dockerArgs, "--pull", "never")
	case "ifnotpresent", "":
		dockerArgs = append(dockerArgs, "--pull", "missing")
	default:
		return 1, nil, fmt.Errorf("plugin %s: invalid pull_policy %q (allowed: Always/IfNotPresent/Never)",
			resolved.Slug, action.Runs.PullPolicy)
	}
	dockerArgs = append(dockerArgs,
		"-v", wsDir+":"+containerWsDir,
		"-w", containerWsDir,
		"-e", "HELIOS_WORKSPACE="+containerWsDir,
		"-e", fmt.Sprintf("HELIOS_RUN_ID=%d", run.ID),
		"-e", "HELIOS_STAGE_ID="+stageID,
	)
	// inputs → INPUT_<UPPER>
	for k, v := range step.With {
		envKey := "INPUT_" + strings.ToUpper(k)
		dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("%s=%v", envKey, v))
	}
	// action.runs.env
	for k, v := range action.Runs.Env {
		dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// entrypoint + args
	if len(action.Runs.Entrypoint) > 0 {
		dockerArgs = append(dockerArgs, "--entrypoint", action.Runs.Entrypoint[0])
		// entrypoint 后续元素无法直接走 --entrypoint, 拼到 command 末尾
	}
	dockerArgs = append(dockerArgs, action.Runs.Image)
	if len(action.Runs.Entrypoint) > 1 {
		dockerArgs = append(dockerArgs, action.Runs.Entrypoint[1:]...)
	}
	dockerArgs = append(dockerArgs, action.Runs.Args...)

	logPath := filepath.Join(opts.WorkspaceDir, fmt.Sprintf("%d", run.ID), "stages", stageID+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if ferr != nil {
		return 1, nil, ferr
	}
	defer logFile.Close()

	var stdoutBuf bytes.Buffer
	stdoutTee := io.MultiWriter(&stdoutBuf, logFile)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	cmd.Stdout = stdoutTee
	cmd.Stderr = logFile

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return -1, nil, runErr
		}
	}
	outputs := parseSetOutputs(stdoutBuf.Bytes())
	if exitCode != 0 {
		return exitCode, outputs, fmt.Errorf("plugin %s@%s exited %d",
			resolved.Slug, resolved.Version, exitCode)
	}
	return 0, outputs, nil
}

// parseSetOutputs 扫描 stdout 抽 `::set-output name=K::V` 行 (GitHub Actions 协议).
//
// 多行重复同名以最后一次为准. value 取到行尾 (不再进一步 trim).
func parseSetOutputs(b []byte) map[string]any {
	out := map[string]any{}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	const prefix = "::set-output name="
	for scanner.Scan() {
		line := scanner.Text()
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		rest := line[i+len(prefix):]
		sep := strings.Index(rest, "::")
		if sep < 0 {
			continue
		}
		name := strings.TrimSpace(rest[:sep])
		val := rest[sep+2:]
		if name == "" {
			continue
		}
		out[name] = val
	}
	return out
}
