package runengine

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// TimeoutEnqueuer 审批超时入队 (窄接口)。
type TimeoutEnqueuer interface {
	EnqueueApprovalTimeout(ctx context.Context, p *tasks.ApprovalTimeoutPayload, delay time.Duration) (taskID string, err error)
}

// CreateApproval 为 approval stage 创建 approval_requests 并把 run 推到 approval。
func CreateApproval(
	ctx context.Context,
	db *sql.DB,
	machine *runstate.Machine,
	timeoutEnq TimeoutEnqueuer,
	runID int64,
	stage *dsl.Stage,
) (int64, error) {
	if stage == nil || stage.Type != "approval" {
		return 0, fmt.Errorf("not an approval stage")
	}
	mode := stage.Mode
	if mode == "" {
		mode = "any"
	}
	onTimeout := stage.OnTimeout
	if onTimeout == "" {
		onTimeout = "reject"
	}
	var timeoutAt *time.Time
	var timeoutDur time.Duration
	if stage.Timeout != "" {
		d, err := time.ParseDuration(stage.Timeout)
		if err != nil {
			return 0, fmt.Errorf("parse timeout: %w", err)
		}
		timeoutDur = d
		t := time.Now().UTC().Add(d)
		timeoutAt = &t
	}

	var reqID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO approval_requests (run_id, stage_id, required_approvers, mode, status, on_timeout, timeout_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, now(), now())
		RETURNING id
	`, runID, stage.ID, pq.Array(stage.Approvers), mode, onTimeout, timeoutAt).Scan(&reqID)
	if err != nil {
		return 0, fmt.Errorf("insert approval_request: %w", err)
	}

	if machine != nil {
		if err := machine.MarkApproval(ctx, runID, runstate.TransitionOpts{
			Reason: fmt.Sprintf("waiting approval on stage %s", stage.ID),
		}); err != nil {
			return reqID, fmt.Errorf("mark approval: %w", err)
		}
	}

	if timeoutEnq != nil && timeoutDur > 0 {
		_, _ = timeoutEnq.EnqueueApprovalTimeout(ctx, &tasks.ApprovalTimeoutPayload{RequestID: reqID}, timeoutDur)
	}
	return reqID, nil
}
