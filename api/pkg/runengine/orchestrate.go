package runengine

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/helios-cicd/helios/api/pkg/engine"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// StageEnqueuer orchestrate 派发 stage 任务。
type StageEnqueuer interface {
	EnqueueStageExecute(ctx context.Context, p *tasks.StageExecutePayload) (taskID string, err error)
}

// AdvanceResult orchestrate 一次 tick 的结果。
type AdvanceResult struct {
	Done           bool
	Dispatched     []tasks.StageExecutePayload
	WaitingApproval []string
	RunOutcome     string // 终态时: success/failed/canceled
}

// AdvanceOpts orchestrate 依赖。
type AdvanceOpts struct {
	DB          *sql.DB
	Machine     *runstate.Machine
	WorkspaceDir string
	StageEnq    StageEnqueuer
	TimeoutEnq  TimeoutEnqueuer
}

// Advance 执行一次调度 tick: bootstrap → 同步审批 → NextReady → 派发 / 终态。
func Advance(ctx context.Context, opts AdvanceOpts, runID, projectID int64) (*AdvanceResult, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("db required")
	}

	run, err := LoadRun(ctx, opts.DB, runID)
	if err != nil {
		return nil, err
	}
	if runstate.IsTerminal(run.Status) {
		return &AdvanceResult{Done: true, RunOutcome: run.Status}, nil
	}

	if err := Bootstrap(ctx, opts.DB, runID, run.VersionID); err != nil {
		return nil, err
	}

	raw, err := LoadPipelineSpec(ctx, opts.DB, run.VersionID)
	if err != nil {
		return nil, err
	}
	p, err := parsePipeline(raw)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}
	dag, err := buildDAG(p)
	if err != nil {
		return nil, err
	}

	stageRows, err := ListStages(ctx, opts.DB, runID)
	if err != nil {
		return nil, err
	}
	statusMap := make(map[string]string, len(stageRows))
	stageByNode := make(map[string]StageRow, len(stageRows))
	for _, s := range stageRows {
		statusMap[s.StageID] = s.Status
		stageByNode[s.StageID] = s
	}

	sch := engine.NewScheduler(dag)
	sch.RestoreFromDB(statusMap)

	// 同步已完成的审批 stage
	if err := syncApprovalStages(ctx, opts, sch, runID, stageRows, stageByNode); err != nil {
		return nil, err
	}

	// 标记因依赖失败而跳过的 stage
	if err := markSkippedStages(ctx, opts.DB, sch, stageRows); err != nil {
		return nil, err
	}

	res := &AdvanceResult{}

	if sch.Done() {
		outcome := string(sch.Outcome())
		if err := finalizeRun(ctx, opts, run, outcome); err != nil {
			return nil, err
		}
		res.Done = true
		res.RunOutcome = outcome
		return res, nil
	}

	ready := sch.NextReady()
	for _, nodeID := range ready {
		row, ok := stageByNode[nodeID]
		if !ok {
			continue
		}
		node := dag.Nodes[nodeID]
		if node != nil && node.Stage != nil && node.Stage.Type == "approval" {
			if _, err := CreateApproval(ctx, opts.DB, opts.Machine, opts.TimeoutEnq, runID, node.Stage); err != nil {
				log.Printf("orchestrate: create approval run=%d stage=%s err=%v", runID, nodeID, err)
				_ = sch.Complete(nodeID, engine.StatusFailed)
				_ = UpdateStageStatus(ctx, opts.DB, row.ID, "failed", intPtr(1))
				continue
			}
			_ = UpdateStageStatus(ctx, opts.DB, row.ID, "approval", nil)
			res.WaitingApproval = append(res.WaitingApproval, nodeID)
			continue
		}

		_ = UpdateStageStatus(ctx, opts.DB, row.ID, "running", nil)
		payload := tasks.StageExecutePayload{
			RunID: runID, ProjectID: projectID,
			StageRecordID: row.ID, StageID: nodeID,
		}
		if opts.StageEnq != nil {
			if _, err := opts.StageEnq.EnqueueStageExecute(ctx, &payload); err != nil {
				return nil, fmt.Errorf("enqueue stage %s: %w", nodeID, err)
			}
		}
		res.Dispatched = append(res.Dispatched, payload)
	}

	// 无派发且无等待 → 可能 stage 仍在执行中
	if len(res.Dispatched) == 0 && len(res.WaitingApproval) == 0 {
		// 再检查是否 done (e.g. 全 skip)
		if sch.Done() {
			outcome := string(sch.Outcome())
			if err := finalizeRun(ctx, opts, run, outcome); err != nil {
				return nil, err
			}
			res.Done = true
			res.RunOutcome = outcome
		}
	}
	return res, nil
}

func syncApprovalStages(ctx context.Context, opts AdvanceOpts, sch *engine.Scheduler, runID int64, rows []StageRow, byNode map[string]StageRow) error {
	for _, row := range rows {
		if row.Status != "approval" {
			continue
		}
		req, err := GetApprovalForStage(ctx, opts.DB, runID, row.StageID)
		if err != nil || req == nil {
			continue
		}
		switch req.Status {
		case "approved":
			if err := sch.Complete(row.StageID, engine.StatusSuccess); err != nil {
				continue
			}
			_ = UpdateStageStatus(ctx, opts.DB, row.ID, "success", intPtr(0))
		case "rejected", "timeout":
			if err := sch.Complete(row.StageID, engine.StatusFailed); err != nil {
				continue
			}
			_ = UpdateStageStatus(ctx, opts.DB, row.ID, "failed", intPtr(1))
		}
	}
	return nil
}

func markSkippedStages(ctx context.Context, db *sql.DB, sch *engine.Scheduler, rows []StageRow) error {
	snap := sch.Snapshot()
	for _, row := range rows {
		st, ok := snap[row.StageID]
		if !ok {
			continue
		}
		if st == engine.StatusSkipped && row.Status == "pending" {
			_ = UpdateStageStatus(ctx, db, row.ID, "skipped", nil)
		}
	}
	return nil
}

func finalizeRun(ctx context.Context, opts AdvanceOpts, run *RunInfo, outcome string) error {
	if opts.Machine == nil {
		return nil
	}
	topts := runstate.TransitionOpts{ProjectID: &run.ProjectID, Reason: "pipeline " + outcome}
	switch outcome {
	case "success":
		return opts.Machine.MarkSuccess(ctx, run.ID, topts)
	case "failed":
		return opts.Machine.MarkFailed(ctx, run.ID, "pipeline failed", topts)
	case "canceled":
		return opts.Machine.MarkCanceled(ctx, run.ID, topts)
	default:
		return opts.Machine.MarkSuccess(ctx, run.ID, topts)
	}
}

// OnStageComplete stage 执行完成后调: 更新 scheduler + 触发下一 tick。
func OnStageComplete(ctx context.Context, opts AdvanceOpts, runID, projectID int64, stageID string, success bool, exitCode int) error {
	rows, err := ListStages(ctx, opts.DB, runID)
	if err != nil {
		return err
	}
	var row *StageRow
	for i := range rows {
		if rows[i].StageID == stageID {
			row = &rows[i]
			break
		}
	}
	if row == nil {
		return fmt.Errorf("stage %s not found for run %d", stageID, runID)
	}

	st := "failed"
	result := engine.StatusFailed
	if success {
		st = "success"
		result = engine.StatusSuccess
	}
	_ = UpdateStageStatus(ctx, opts.DB, row.ID, st, &exitCode)

	runInfo, err := LoadRun(ctx, opts.DB, runID)
	if err != nil {
		return err
	}
	raw, err := LoadPipelineSpec(ctx, opts.DB, runInfo.VersionID)
	if err != nil {
		return err
	}
	p, _ := parsePipeline(raw)
	dag, err := buildDAG(p)
	if err != nil {
		return err
	}
	statusMap := make(map[string]string, len(rows))
	for _, s := range rows {
		statusMap[s.StageID] = s.Status
	}
	statusMap[stageID] = st
	sch := engine.NewScheduler(dag)
	sch.RestoreFromDB(statusMap)
	if err := sch.Complete(stageID, result); err != nil {
		log.Printf("onStageComplete: scheduler complete %s: %v", stageID, err)
	}

	_, err = Advance(ctx, opts, runID, projectID)
	return err
}

func intPtr(v int) *int { return &v }
