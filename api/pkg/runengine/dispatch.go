package runengine

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// AfterCheckoutEnqueuer checkout 完成后入队下游任务。
type AfterCheckoutEnqueuer interface {
	EnqueueRunBuild(ctx context.Context, p *tasks.RunBuildPayload) (taskID string, err error)
	EnqueueRunOrchestrate(ctx context.Context, p *tasks.RunOrchestratePayload) (taskID string, err error)
}

// DispatchAfterCheckout 根据 pipeline spec 选择多 stage 编排或 legacy build。
func DispatchAfterCheckout(ctx context.Context, db *sql.DB, enq AfterCheckoutEnqueuer, runID, projectID int64) error {
	if enq == nil {
		return nil
	}
	run, err := LoadRun(ctx, db, runID)
	if err != nil {
		return err
	}
	hasStages, err := HasPipelineStages(ctx, db, run.VersionID)
	if err != nil {
		return fmt.Errorf("check pipeline stages: %w", err)
	}
	if hasStages {
		_, err = enq.EnqueueRunOrchestrate(ctx, &tasks.RunOrchestratePayload{
			RunID: runID, ProjectID: projectID,
		})
		return err
	}
	_, err = enq.EnqueueRunBuild(ctx, &tasks.RunBuildPayload{
		RunID: runID, ProjectID: projectID,
	})
	return err
}

// OrchestrateEnqueuer 仅入队 orchestrate tick。
type OrchestrateEnqueuer interface {
	EnqueueRunOrchestrate(ctx context.Context, p *tasks.RunOrchestratePayload) (taskID string, err error)
}

// ResumeOrchestrate 审批通过后重新入队调度 tick。
func ResumeOrchestrate(ctx context.Context, db *sql.DB, enq OrchestrateEnqueuer, runID int64) error {
	if enq == nil {
		return nil
	}
	run, err := LoadRun(ctx, db, runID)
	if err != nil {
		return err
	}
	_, err = enq.EnqueueRunOrchestrate(ctx, &tasks.RunOrchestratePayload{
		RunID: runID, ProjectID: run.ProjectID,
	})
	return err
}
