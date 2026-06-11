package runengine

import (
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
)

// ExecuteOpts stage 执行依赖。
type ExecuteOpts struct {
	DB           *sql.DB
	WorkspaceDir string
	ArtifactRoot string
	Timeout      time.Duration
	LogWriter    io.Writer // 可选: 追加到 stage 日志
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

		ec, stepOut, runErr := runStep(ctx, opts, run, stageID, wsDir, storage, &step)
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

func runStep(ctx context.Context, opts ExecuteOpts, run *RunInfo, stageID, wsDir string, storage artifact.Storage, step *dsl.Step) (exitCode int, outputs map[string]any, err error) {
	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	if step.Uses != "" {
		b, ok := builtin.Lookup(step.Uses)
		if !ok {
			return 1, nil, fmt.Errorf("unknown builtin %s", step.Uses)
		}
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
