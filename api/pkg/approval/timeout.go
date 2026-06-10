// Package approval — Timeouter: 审批超时处理 (T2.6.3, 跨 api/worker module 共用).
//
// 为什么放 pkg 而非 internal:
//   - worker 跨 module 不能 import api/internal/service
//   - approval Create/Approve/Reject 是 handler-only 逻辑, 留在 internal/service
//   - timeout 是 worker 触发 (asynq 延时任务回调), 必须跨 module 共享 → 落 pkg/approval
//
// 三策略 (on_timeout):
//   - reject (默认): request.status=timeout, run approval→timeout (终态, 与 failed 等价但区分上报)
//   - approve: 写一条 username='system' 的 approvals 行, request.status=approved, run approval→running
//   - pause: request.status=timeout, run 仍保持 approval (M2 简化: 管理员只能 cancel run; M3 加重审入口)
//
// 幂等:
//   - 第一行 SELECT FOR UPDATE 锁 request, 若 status != pending 直接 no-op (asynq 不再重试)
//   - 时钟漂移防护: 真到点前 (now < timeout_at - 1s) 视为误触发, 返 noop
package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/helios-cicd/helios/api/pkg/runstate"
)

// 业务错误.
var (
	ErrTimeoutAlreadyHandled = errors.New("approval request already in terminal state")
)

// TimeoutResult Timeout 函数返回值, 给 worker handler 上报用.
type TimeoutResult struct {
	RequestID  int64
	RunID      int64
	OnTimeout  string // reject / approve / pause
	NewStatus  string // approval_requests.status 处理后值
	RunStatus  string // 推 runstate.Machine 后的 run 状态 (pause 时空)
	NoOp       bool   // true = 幂等吸收 (Approve/Reject 抢先了)
}

// Timeouter 处理一次审批超时.
//
// 实现注意:
//   - 用独立 *sql.DB (不依赖 GORM) 跟 runstate.Machine 保持一致, 也方便 worker 无 GORM 依赖
//   - 一个事务: SELECT FOR UPDATE → 按 on_timeout 更新 → INSERT audit_log
//   - run 状态推进放事务外 (machine 走独立 sql.DB 会绕开本 tx, 与 ApprovalService 同样套路)
type Timeouter struct {
	db      *sql.DB
	machine *runstate.Machine
}

// NewTimeouter 构造. machine 必填 (timeout 必须推 run 状态).
func NewTimeouter(db *sql.DB, m *runstate.Machine) *Timeouter {
	return &Timeouter{db: db, machine: m}
}

// HandleTimeout 处理 requestID 对应的超时.
func (t *Timeouter) HandleTimeout(ctx context.Context, requestID int64) (*TimeoutResult, error) {
	if t == nil || t.db == nil {
		return nil, fmt.Errorf("timeouter not configured")
	}

	var (
		runID      int64
		stageID    string
		status     string
		mode       string
		onTimeout  string
		timeoutAt  sql.NullTime
	)

	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
		SELECT run_id, stage_id, status, mode, on_timeout, timeout_at
		  FROM approval_requests
		 WHERE id=$1
		 FOR UPDATE
	`, requestID).Scan(&runID, &stageID, &status, &mode, &onTimeout, &timeoutAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("approval request %d not found", requestID)
	}
	if err != nil {
		return nil, fmt.Errorf("select request: %w", err)
	}

	// 1. 已经被 Approve/Reject 抢先 → no-op
	if status != "pending" {
		return &TimeoutResult{
			RequestID: requestID, RunID: runID, OnTimeout: onTimeout,
			NewStatus: status, NoOp: true,
		}, nil
	}

	// 2. 防时钟漂移: 提早超过 1 秒就吸收为 noop (理论上 asynq 已可靠延时, 双保险)
	if timeoutAt.Valid {
		now := time.Now().UTC()
		if timeoutAt.Time.After(now.Add(1 * time.Second)) {
			log.Printf("[approval-timeout] early fire request=%d timeout_at=%s now=%s (skip)",
				requestID, timeoutAt.Time, now)
			return &TimeoutResult{
				RequestID: requestID, RunID: runID, OnTimeout: onTimeout,
				NewStatus: status, NoOp: true,
			}, nil
		}
	}

	// 3. 按 on_timeout 三策略写 request + (approve 路径) 落 system 投票
	if onTimeout == "" {
		onTimeout = "reject"
	}
	now := time.Now().UTC()

	var newReqStatus string
	switch onTimeout {
	case "reject":
		newReqStatus = "timeout"
	case "approve":
		newReqStatus = "approved"
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO approvals (request_id, user_id, username, decision, comment, created_at)
			VALUES ($1, NULL, 'system', 'approve', $2, $3)
		`, requestID, fmt.Sprintf("system auto-approve on timeout (mode=%s)", mode), now); err != nil {
			return nil, fmt.Errorf("insert system approval: %w", err)
		}
	case "pause":
		newReqStatus = "timeout"
	default:
		return nil, fmt.Errorf("unknown on_timeout %q", onTimeout)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		   SET status=$1, updated_at=$2
		 WHERE id=$3 AND status='pending'
	`, newReqStatus, now, requestID); err != nil {
		return nil, fmt.Errorf("update request: %w", err)
	}

	// 4. audit_logs (raw SQL 避开 INET 空值坑)
	payload, _ := json.Marshal(map[string]any{
		"request_id":  requestID,
		"stage_id":    stageID,
		"on_timeout":  onTimeout,
		"new_status":  newReqStatus,
		"mode":        mode,
		"actor":       "system",
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (action, resource_type, resource_id, payload, result, created_at)
		VALUES ('approval.timeout', 'approval_request', $1, CAST($2 AS jsonb), 'success', $3)
	`, requestID, string(payload), now); err != nil {
		return nil, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// 5. tx 外推 run 状态
	res := &TimeoutResult{
		RequestID: requestID, RunID: runID, OnTimeout: onTimeout, NewStatus: newReqStatus,
	}
	if t.machine == nil {
		return res, nil
	}
	reason := fmt.Sprintf("approval on_timeout=%s on stage=%s", onTimeout, stageID)
	switch onTimeout {
	case "reject":
		if err := t.machine.MarkTimeout(ctx, runID, reason, runstate.TransitionOpts{}); err != nil {
			log.Printf("[approval-timeout] machine.MarkTimeout run=%d err=%v", runID, err)
		} else {
			res.RunStatus = runstate.StatusTimeout
		}
	case "approve":
		if err := t.machine.MarkRunning(ctx, runID, runstate.TransitionOpts{Reason: reason}); err != nil {
			log.Printf("[approval-timeout] machine.MarkRunning run=%d err=%v", runID, err)
		} else {
			res.RunStatus = runstate.StatusRunning
		}
	case "pause":
		// 不动 run 状态; M2 简化: 管理员手动 cancel
		log.Printf("[approval-timeout] pause: run=%d request=%d held in approval state", runID, requestID)
	}
	return res, nil
}
