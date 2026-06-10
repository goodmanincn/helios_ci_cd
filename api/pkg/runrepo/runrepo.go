// Package runrepo 暴露 worker 需要的 run 状态更新接口 (可跨 module 引用)。
//
// 设计:
//   - 不复用 api/internal/model 的 GORM struct (internal 限制)
//   - 直接走 SQL,字段最小化,语义清晰
//   - worker 只关心: 改 run.status, 记录 started_at/finished_at, 写 audit
package runrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Status run 状态机的合法值 (与 spec/06 一致)。
const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// ErrNotFound run 不存在或已被删。
var ErrNotFound = errors.New("run not found")

// Repo run 写仓储 (worker 用)。
type Repo struct {
	db *sql.DB
}

// New 构造,db 是 *sql.DB (worker 走 lib/pq 即可,不引 GORM 减少依赖)。
func New(db *sql.DB) *Repo { return &Repo{db: db} }

// MarkRunning 把 run 推进到 running 状态,只在当前是 pending 时才生效 (幂等)。
// 返回是否真的发生了转换。
func (r *Repo) MarkRunning(ctx context.Context, runID int64) (bool, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE runs
		   SET status = $1, started_at = $2
		 WHERE id = $3 AND status = $4
	`, StatusRunning, now, runID, StatusPending)
	if err != nil {
		return false, fmt.Errorf("mark running: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkFailed 推进到 failed,记录原因。
// 允许从 pending/running → failed。
func (r *Repo) MarkFailed(ctx context.Context, runID int64, reason string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE runs
		   SET status = $1, finished_at = $2, message = COALESCE(NULLIF(message, ''), '') || $3
		 WHERE id = $4 AND status IN ($5, $6)
	`, StatusFailed, now, "\n[failed] "+reason, runID, StatusPending, StatusRunning)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRunMeta 拿 run 的最少元数据 (用于 worker 决策, 例如 status 校验)。
type RunMeta struct {
	ID        int64
	Status    string
	Branch    string
	CommitSHA string
}

// GetMeta 查 run 元数据。
func (r *Repo) GetMeta(ctx context.Context, runID int64) (*RunMeta, error) {
	var m RunMeta
	err := r.db.QueryRowContext(ctx, `
		SELECT id, status, COALESCE(branch, ''), COALESCE(commit_sha, '')
		  FROM runs WHERE id = $1
	`, runID).Scan(&m.ID, &m.Status, &m.Branch, &m.CommitSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return &m, nil
}
