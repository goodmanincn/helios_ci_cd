// Package plugin — runtime.go: 把 step.Uses 解析成一个可执行的 Resolved.
//
// 调用流:
//   uses string ──┐
//                 │ ParseRef →
//                 ▼
//   Resolver.Resolve(orgID, ref) → (Resolved, error)
//      - 查 plugin_installations: 当前 org 必须安装过这个插件
//      - 取 plugin_versions: 同 plugin_id 下匹配 version (latest 走 is_latest)
//      - 解析 action.yml → Action
//
// 设计:
//   - Resolver 是 interface, worker 端用 DB-backed 实现; 单测用 in-memory
//   - 本轮 ORG = 0 时退化"不限 org" (worker 调用时拿 run.org_id 传入)
package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Resolved 一个可执行的插件实例.
type Resolved struct {
	Ref     *Ref     `json:"ref"`
	Action  *Action  `json:"action"`
	Slug    string   `json:"slug"`
	Version string   `json:"version"`
}

// Resolver 解析 step.Uses 的接口.
type Resolver interface {
	Resolve(ctx context.Context, orgID int64, ref *Ref) (*Resolved, error)
}

// ErrNotInstalled — 当前 org 未安装该插件.
type ErrNotInstalled struct{ Slug string }

func (e ErrNotInstalled) Error() string {
	return fmt.Sprintf("plugin %s not installed in current org", e.Slug)
}

// ErrVersionMismatch — 安装的版本与请求的不匹配.
type ErrVersionMismatch struct {
	Slug      string
	Requested string
	Installed string
}

func (e ErrVersionMismatch) Error() string {
	return fmt.Sprintf("plugin %s: requested %s but installed %s — re-install or pin uses: to installed version",
		e.Slug, e.Requested, e.Installed)
}

// SQLResolver 用裸 *sql.DB 实现, 给 worker 端 runengine 用 (避免依赖 GORM).
type SQLResolver struct{ DB *sql.DB }

// NewSQLResolver 构造.
func NewSQLResolver(db *sql.DB) *SQLResolver { return &SQLResolver{DB: db} }

// Resolve 把 ref 解到 Resolved.
//
// 规则:
//   - ref.Local=true → 本轮不支持, 返 error (留 hook)
//   - orgID>0: 必须 plugin_installations 有这行; 校验 version 匹配
//   - 没有 installation: 若 ref.Version="latest" 且 plugin official=true 且 verified=true, 允许"隐式使用",
//     这样官方插件不需要 explicit install 也能跑 (降低首次体验阻力)
func (r *SQLResolver) Resolve(ctx context.Context, orgID int64, ref *Ref) (*Resolved, error) {
	if ref == nil {
		return nil, errors.New("plugin.Resolve: nil ref")
	}
	if ref.Local {
		return nil, errors.New("plugin.Resolve: local ./ refs not supported (MVP)")
	}
	slug := ref.Slug()

	var (
		pluginID    int64
		official    bool
		verified    bool
		latestVer   sql.NullString
	)
	err := r.DB.QueryRowContext(ctx,
		`SELECT id, official, verified, COALESCE(latest_version,'')
		 FROM plugins WHERE slug = $1 AND deleted_at IS NULL`, slug).
		Scan(&pluginID, &official, &verified, &latestVer)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("plugin %s not in registry", slug)
		}
		return nil, err
	}

	// 是否已安装 + 选定 version
	requested := ref.Version
	var (
		instVersionID sql.NullInt64
		isInstalled   bool
	)
	if orgID > 0 {
		err = r.DB.QueryRowContext(ctx,
			`SELECT version_id FROM plugin_installations
			 WHERE org_id = $1 AND plugin_id = $2`, orgID, pluginID).
			Scan(&instVersionID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		} else {
			isInstalled = true
		}
	}

	// 选 target version row
	var (
		versionID int64
		version   string
		actionYML string
	)
	switch {
	case isInstalled:
		// 用安装的版本; 若 ref 指定具体版本但与安装版本不一致, 拒绝 (避免 silent drift)
		err = r.DB.QueryRowContext(ctx,
			`SELECT id, version, action_yml FROM plugin_versions WHERE id = $1`,
			instVersionID.Int64).
			Scan(&versionID, &version, &actionYML)
		if err != nil {
			return nil, fmt.Errorf("load installed version: %w", err)
		}
		if requested != "" && requested != "latest" && requested != version {
			return nil, ErrVersionMismatch{Slug: slug, Requested: requested, Installed: version}
		}
	case official && verified:
		// 隐式使用官方 verified 插件
		row := r.DB.QueryRowContext(ctx, pickVersionSQL(requested),
			pluginID, requested)
		if err := row.Scan(&versionID, &version, &actionYML); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("plugin %s: version %s not found", slug, requested)
			}
			return nil, err
		}
	default:
		return nil, ErrNotInstalled{Slug: slug}
	}

	action, errs := ParseActionYAML([]byte(actionYML))
	if len(errs) > 0 {
		return nil, fmt.Errorf("parse action.yml for %s@%s: %v", slug, version, errs)
	}
	return &Resolved{
		Ref:     ref,
		Action:  action,
		Slug:    slug,
		Version: version,
	}, nil
}

// pickVersionSQL — 拿 (id, version, action_yml). version="latest"/"" 走 is_latest, 否则精确匹配.
func pickVersionSQL(requested string) string {
	if requested == "" || requested == "latest" {
		return `SELECT id, version, action_yml FROM plugin_versions
		        WHERE plugin_id = $1 AND ($2 = '' OR is_latest = TRUE)
		        ORDER BY id DESC LIMIT 1`
	}
	return `SELECT id, version, action_yml FROM plugin_versions
	        WHERE plugin_id = $1 AND version = $2 LIMIT 1`
}

// MarshalResolved JSON 化, 调试用.
func MarshalResolved(r *Resolved) string {
	b, _ := json.Marshal(r)
	return string(b)
}
