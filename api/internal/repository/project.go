// Package repository 数据访问层 — 把 GORM 操作收口,方便上层注入 mock。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// ErrNotFound repository 层统一未找到错误。
var ErrNotFound = errors.New("repository: not found")

// ListProjectsFilter 项目列表筛选。
type ListProjectsFilter struct {
	OrgID  int64  // 必填: 永远按 org 隔离
	Query  string // name/slug 模糊
	Limit  int
	Offset int
}

// ProjectRepository 项目数据访问。
type ProjectRepository interface {
	Create(ctx context.Context, p *model.Project) error
	GetByID(ctx context.Context, orgID, id int64) (*model.Project, error)
	GetBySlug(ctx context.Context, orgID int64, slug string) (*model.Project, error)
	List(ctx context.Context, f ListProjectsFilter) ([]model.Project, int64, error)
	Update(ctx context.Context, p *model.Project) error
	Delete(ctx context.Context, orgID, id int64) error
}

// ===== GORM 实现 =====

type projectRepo struct {
	db *gorm.DB
}

func NewProjectRepository(db *gorm.DB) ProjectRepository { return &projectRepo{db: db} }

func (r *projectRepo) Create(ctx context.Context, p *model.Project) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *projectRepo) GetByID(ctx context.Context, orgID, id int64) (*model.Project, error) {
	var p model.Project
	err := r.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (r *projectRepo) GetBySlug(ctx context.Context, orgID int64, slug string) (*model.Project, error) {
	var p model.Project
	err := r.db.WithContext(ctx).Where("org_id = ? AND slug = ?", orgID, slug).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (r *projectRepo) List(ctx context.Context, f ListProjectsFilter) ([]model.Project, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Project{}).Where("org_id = ?", f.OrgID)
	if f.Query != "" {
		like := "%" + f.Query + "%"
		q = q.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var out []model.Project
	err := q.Order("created_at DESC").Limit(limit).Offset(f.Offset).Find(&out).Error
	return out, total, err
}

func (r *projectRepo) Update(ctx context.Context, p *model.Project) error {
	res := r.db.WithContext(ctx).
		Model(&model.Project{}).
		Where("org_id = ? AND id = ?", p.OrgID, p.ID).
		Updates(map[string]any{
			"name":           p.Name,
			"description":    p.Description,
			"default_branch": p.DefaultBranch,
			"visibility":     p.Visibility,
			"config":         p.Config,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *projectRepo) Delete(ctx context.Context, orgID, id int64) error {
	res := r.db.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgID, id).
		Delete(&model.Project{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
