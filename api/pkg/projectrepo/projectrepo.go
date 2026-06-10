// Package projectrepo 暴露 worker / 其它跨模块组件需要的 project 读写接口。
//
// 设计原则:
//   - 不依赖 GORM / api/internal (internal 跨 module 禁用)
//   - 只暴露最小必要操作: 读 project 基本字段 + 改 config JSON 子字段
//   - config 的合并语义在 service 层定义, 这里做无脑替换 + 浅合并
package projectrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound project 不存在。
var ErrNotFound = errors.New("project not found")

// Repo project 读写仓储 (基于 sql.DB)。
type Repo struct {
	db *sql.DB
}

// New 构造。
func New(db *sql.DB) *Repo { return &Repo{db: db} }

// Project worker 关心的最小字段集。
type Project struct {
	ID            int64
	OrgID         int64
	Name          string
	Slug          string
	RepoURL       string
	RepoType      string
	DefaultBranch string
	Config        map[string]any // 已反序列化的 config jsonb
}

// Get 按 id 取 project (跨 org)。
func (r *Repo) Get(ctx context.Context, id int64) (*Project, error) {
	var p Project
	var configRaw []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, org_id, name, slug,
		       COALESCE(repo_url, ''), COALESCE(repo_type, ''),
		       COALESCE(default_branch, ''), COALESCE(config, '{}'::jsonb)
		  FROM projects WHERE id = $1
	`, id).Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &p.RepoURL, &p.RepoType, &p.DefaultBranch, &configRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &p.Config)
	}
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	return &p, nil
}

// MergeConfig 浅合并 patch 到 projects.config (top-level key 覆盖),并写回。
// 用 SELECT ... FOR UPDATE + UPDATE 保证并发安全。
func (r *Repo) MergeConfig(ctx context.Context, id int64, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var configRaw []byte
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(config, '{}'::jsonb) FROM projects WHERE id = $1 FOR UPDATE
	`, id).Scan(&configRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("select for update: %w", err)
	}
	cur := map[string]any{}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &cur)
	}
	for k, v := range patch {
		cur[k] = v
	}
	merged, err := json.Marshal(cur)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE projects SET config = $1, updated_at = $2 WHERE id = $3
	`, merged, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
