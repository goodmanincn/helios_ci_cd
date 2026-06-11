// Package handler — approval_timeout.go (T2.6.3).
//
// asynq handler: 消费 helios:approval:timeout 任务, 调 pkg/approval.Timeouter 处理.
//
// 设计:
//   - 失败不重试 (asynq.MaxRetry(0) by enqueuer); 即便重试也是幂等的
//   - 不存在的 request_id 视为 noop 返 SkipRetry, 不污染 asynq dead 列表
package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/approval"
	"github.com/helios-cicd/helios/api/pkg/runengine"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// ApprovalTimeoutHandler 是 asynq.Handler 实现.
type ApprovalTimeoutHandler struct {
	t   *approval.Timeouter
	db  *sql.DB
	enq runengine.OrchestrateEnqueuer
}

// NewApprovalTimeout 构造.
func NewApprovalTimeout(t *approval.Timeouter, db *sql.DB, enq runengine.OrchestrateEnqueuer) *ApprovalTimeoutHandler {
	return &ApprovalTimeoutHandler{t: t, db: db, enq: enq}
}

// ProcessTask asynq.Handler 接口.
func (h *ApprovalTimeoutHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalApprovalTimeout(t.Payload())
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	res, err := h.t.HandleTimeout(ctx, p.RequestID)
	if err != nil {
		// 找不到的 request 视为 noop (避免任务卡在 dead 列表)
		if errors.Is(err, approval.ErrTimeoutAlreadyHandled) {
			return nil
		}
		log.Printf("[approval-timeout] request=%d err=%v", p.RequestID, err)
		// asynq.MaxRetry(0) → 直接 archive, 不重试
		return err
	}
	if res.NoOp {
		log.Printf("[approval-timeout] request=%d run=%d noop (status=%s)",
			res.RequestID, res.RunID, res.NewStatus)
		return nil
	}
	log.Printf("[approval-timeout] request=%d run=%d on_timeout=%s → request.status=%s run.status=%s",
		res.RequestID, res.RunID, res.OnTimeout, res.NewStatus, res.RunStatus)
	if res.RunStatus == runstate.StatusRunning && h.db != nil && h.enq != nil {
		if err := runengine.ResumeOrchestrate(ctx, h.db, h.enq, res.RunID); err != nil {
			log.Printf("[approval-timeout] resume orchestrate run=%d err=%v", res.RunID, err)
		}
	}
	return nil
}
