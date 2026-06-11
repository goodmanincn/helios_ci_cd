package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// PipelineTemplate 存储 (M8 T8.2.1)。
//
// 设计:
//   - List 返回全局 (org_id IS NULL) + 当前 org 私有的并集
//   - 支持按 category / tag / q (slug+name 模糊) 过滤
//   - Get 不校验 org, 由 handler 校验 VisibleToOrg
//   - Delete 拒删 builtin, 由 handler 调用前检查

var ErrPipelineTemplateNotFound = errors.New("pipeline template not found")
var ErrPipelineTemplateBuiltin = errors.New("builtin templates cannot be modified or deleted")

type PipelineTemplateStore interface {
	Create(t *model.PipelineTemplate) error
	Get(id int64) (*model.PipelineTemplate, error)
	GetBySlug(slug string) (*model.PipelineTemplate, error)
	List(orgID int64, filter TemplateFilter) ([]model.PipelineTemplate, error)
	Update(t *model.PipelineTemplate) error
	Delete(id int64) error
}

// TemplateFilter 模板列表过滤参数。零值表示不过滤。
type TemplateFilter struct {
	Category string
	Tag      string // 单个 tag 精确匹配; 多 tag 留扩展
	Q        string // slug / name / description 模糊
}

type GormPipelineTemplateStore struct {
	db *gorm.DB
}

func NewPipelineTemplateRepository(db *gorm.DB) *GormPipelineTemplateStore {
	return &GormPipelineTemplateStore{db: db}
}

func (s *GormPipelineTemplateStore) Create(t *model.PipelineTemplate) error {
	return s.db.Create(t).Error
}

func (s *GormPipelineTemplateStore) Get(id int64) (*model.PipelineTemplate, error) {
	var t model.PipelineTemplate
	if err := s.db.First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPipelineTemplateNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *GormPipelineTemplateStore) GetBySlug(slug string) (*model.PipelineTemplate, error) {
	var t model.PipelineTemplate
	if err := s.db.Where("slug = ?", slug).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPipelineTemplateNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *GormPipelineTemplateStore) List(orgID int64, f TemplateFilter) ([]model.PipelineTemplate, error) {
	q := s.db.Model(&model.PipelineTemplate{}).
		Where("org_id IS NULL OR org_id = ?", orgID)

	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if f.Tag != "" {
		// PostgreSQL text[] 包含语义: ? = ANY(tags)
		q = q.Where("? = ANY (tags)", f.Tag)
	}
	if f.Q != "" {
		like := "%" + f.Q + "%"
		q = q.Where("slug ILIKE ? OR name ILIKE ? OR description ILIKE ?", like, like, like)
	}

	var list []model.PipelineTemplate
	err := q.Order("builtin DESC, id DESC").Find(&list).Error
	return list, err
}

func (s *GormPipelineTemplateStore) Update(t *model.PipelineTemplate) error {
	return s.db.Save(t).Error
}

func (s *GormPipelineTemplateStore) Delete(id int64) error {
	// builtin 检查在 handler 层做, 这里只走软删
	res := s.db.Delete(&model.PipelineTemplate{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPipelineTemplateNotFound
	}
	return nil
}
