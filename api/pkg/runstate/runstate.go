// Package runstate 集中实现 Run 状态机 (跨 api/worker module 共用)。
//
// 状态机:
//
//	            ┌──────────────────────────────────┐
//	            ↓                                  │
//	pending ──→ running ──→ success               │
//	   │           │                              │
//	   │           └──→ failed                    │
//	   │                                          │
//	   └──→ canceled  ←──────────────────────────┘
//	   (running → canceled 也允许; 仅由用户/系统主动调用)
//
// 设计:
//   - 唯一可信的状态转移函数 Transition
//   - 不允许跳过中间态 (pending → success 直接 ❌, 必须 running → success)
//   - canceled 是吸收态: 任何非终态 → canceled 都合法 (用户取消)
//   - 终态 (success/failed/canceled) 永不再转换
//   - 每次状态变更同事务写 audit_logs 一行 (action=run.{status})
//   - 所有 DB 操作走 *sql.DB, 不依赖 GORM, 跨 module 安全
package runstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Status 合法状态枚举。
const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// 业务错误。
var (
	ErrNotFound        = errors.New("run not found")
	ErrInvalidStatus   = errors.New("invalid status")
	ErrIllegalTransition = errors.New("illegal status transition")
	ErrTerminal        = errors.New("run is in terminal state")
)

// IsTerminal 判定状态是否终态 (不再可转移)。
func IsTerminal(s string) bool {
	return s == StatusSuccess || s == StatusFailed || s == StatusCanceled
}

// IsValidStatus 是否合法状态字符串。
func IsValidStatus(s string) bool {
	switch s {
	case StatusPending, StatusRunning, StatusSuccess, StatusFailed, StatusCanceled:
		return true
	}
	return false
}

// CanTransition 纯函数: 给定 from→to 是否允许。
//
// 矩阵 (源 → 允许的目标集):
//
//	pending  → running | canceled
//	running  → success | failed  | canceled
//	success  → (none)
//	failed   → (none)
//	canceled → (none)
func CanTransition(from, to string) bool {
	if !IsValidStatus(from) || !IsValidStatus(to) {
		return false
	}
	if from == to {
		return false
	}
	if IsTerminal(from) {
		return false
	}
	switch from {
	case StatusPending:
		return to == StatusRunning || to == StatusCanceled
	case StatusRunning:
		return to == StatusSuccess || to == StatusFailed || to == StatusCanceled
	}
	return false
}

// Machine 状态机执行器, 持有 *sql.DB 和可选 actor 信息。
type Machine struct {
	db *sql.DB
}

// New 构造。
func New(db *sql.DB) *Machine { return &Machine{db: db} }

// TransitionOpts 状态转移参数 (除主键外).
type TransitionOpts struct {
	// Reason 业务说明 / 错误消息, 追加到 runs.message 并写入 audit_logs.payload。
	Reason string
	// ActorID 触发者; nil 表示系统 (worker / engine)。
	ActorID *int64
	// OrgID 项目所属组织 (用于 audit_logs.org_id, 可空)。
	OrgID *int64
	// ProjectID 资源链路 (audit_logs.payload.project_id, 可空)。
	ProjectID *int64
	// Extra 任意附加字段, 进 audit_logs.payload。
	Extra map[string]any
}

// Transition 把 run 从当前状态转换到 to。
//
// 实现策略 (一个事务):
//  1. SELECT ... FOR UPDATE 锁住 runs 行, 取当前 status
//  2. 校验 CanTransition (非法 → ErrIllegalTransition / ErrTerminal)
//  3. UPDATE runs SET status=to, started_at/finished_at 按需
//  4. INSERT audit_logs (action="run."+to)
//
// 返回最新 (status, started_at, finished_at).
func (m *Machine) Transition(ctx context.Context, runID int64, to string, opts TransitionOpts) (newStatus string, started, finished *time.Time, err error) {
	if !IsValidStatus(to) {
		return "", nil, nil, fmt.Errorf("%w: %q", ErrInvalidStatus, to)
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cur string
	var startedRow, finishedRow sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT status, started_at, finished_at FROM runs WHERE id=$1 FOR UPDATE
	`, runID).Scan(&cur, &startedRow, &finishedRow)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil, ErrNotFound
	}
	if err != nil {
		return "", nil, nil, fmt.Errorf("select run: %w", err)
	}

	if IsTerminal(cur) {
		// 幂等: 同终态直接返回 (e.g. 重试场景), 但不写 audit
		if cur == to {
			return cur, nullTimePtr(startedRow), nullTimePtr(finishedRow), nil
		}
		return "", nil, nil, fmt.Errorf("%w: %s", ErrTerminal, cur)
	}
	if cur == to {
		// 幂等: pending→pending, running→running 不算错
		return cur, nullTimePtr(startedRow), nullTimePtr(finishedRow), nil
	}
	if !CanTransition(cur, to) {
		return "", nil, nil, fmt.Errorf("%w: %s → %s", ErrIllegalTransition, cur, to)
	}

	now := time.Now().UTC()
	var (
		setStarted  = startedRow.Valid // 是否已有 started_at
		setFinished = finishedRow.Valid
	)
	// 决定要更新的时间戳
	updStarted := startedRow
	updFinished := finishedRow
	switch to {
	case StatusRunning:
		if !setStarted {
			updStarted = sql.NullTime{Time: now, Valid: true}
		}
	case StatusSuccess, StatusFailed, StatusCanceled:
		if !setFinished {
			updFinished = sql.NullTime{Time: now, Valid: true}
		}
	}

	// message 追加 (reason 不空时)
	msgSuffix := ""
	if opts.Reason != "" {
		msgSuffix = fmt.Sprintf("\n[%s] %s", to, opts.Reason)
	}

	var durationMs int64
	if updStarted.Valid && updFinished.Valid {
		durationMs = updFinished.Time.Sub(updStarted.Time).Milliseconds()
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE runs
		   SET status      = $1,
		       started_at  = $2,
		       finished_at = $3,
		       message     = COALESCE(NULLIF(message, ''), '') || $4,
		       duration_ms = CASE WHEN $5 > 0 THEN $5 ELSE duration_ms END
		 WHERE id = $6 AND status = $7
	`, to, nullableTime(updStarted), nullableTime(updFinished), msgSuffix, durationMs, runID, cur)
	if err != nil {
		return "", nil, nil, fmt.Errorf("update run: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// race: 状态已变, 提示调用方重试
		return "", nil, nil, fmt.Errorf("%w: status changed concurrently", ErrIllegalTransition)
	}

	// audit log
	payload := map[string]any{
		"from": cur, "to": to,
	}
	if opts.Reason != "" {
		payload["reason"] = opts.Reason
	}
	if opts.ProjectID != nil {
		payload["project_id"] = *opts.ProjectID
	}
	for k, v := range opts.Extra {
		payload[k] = v
	}
	pBytes, _ := json.Marshal(payload)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_id, org_id, action, resource_type, resource_id, payload, result, created_at)
		VALUES ($1, $2, $3, 'run', $4, $5::jsonb, 'success', $6)
	`, opts.ActorID, opts.OrgID, "run."+to, runID, pBytes, now)
	if err != nil {
		return "", nil, nil, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", nil, nil, fmt.Errorf("commit: %w", err)
	}
	return to, nullTimePtr(updStarted), nullTimePtr(updFinished), nil
}

// MarkRunning 便捷封装。
func (m *Machine) MarkRunning(ctx context.Context, runID int64, opts TransitionOpts) error {
	_, _, _, err := m.Transition(ctx, runID, StatusRunning, opts)
	return err
}

// MarkSuccess 便捷封装。
func (m *Machine) MarkSuccess(ctx context.Context, runID int64, opts TransitionOpts) error {
	_, _, _, err := m.Transition(ctx, runID, StatusSuccess, opts)
	return err
}

// MarkFailed 便捷封装 (reason 不会被覆盖)。
func (m *Machine) MarkFailed(ctx context.Context, runID int64, reason string, opts TransitionOpts) error {
	opts.Reason = reason
	_, _, _, err := m.Transition(ctx, runID, StatusFailed, opts)
	return err
}

// MarkCanceled 便捷封装 (一般由用户触发)。
func (m *Machine) MarkCanceled(ctx context.Context, runID int64, opts TransitionOpts) error {
	_, _, _, err := m.Transition(ctx, runID, StatusCanceled, opts)
	return err
}

// Status 当前状态 (单独读, 不锁)。
func (m *Machine) Status(ctx context.Context, runID int64) (string, error) {
	var s string
	err := m.db.QueryRowContext(ctx, "SELECT status FROM runs WHERE id=$1", runID).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s, nil
}

// === helpers ===

func nullableTime(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time
	return &tt
}
