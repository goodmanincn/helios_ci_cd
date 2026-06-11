package model

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

// PipelineTemplate 对应 pipeline_templates 表 (M8 T8.2.1)。
//
// 模板与 pipelines 解耦:
//   - org_id NULL → 全局模板, 所有 org 可见 (builtin 通常如此)
//   - org_id 有值 → 该 org 私有
//   - builtin=true → seed 写入, 不允许删除
//   - 克隆动作 (POST /pipelines/from-template) 把模板的 spec_raw 写入新 pipeline_version
type PipelineTemplate struct {
	Base
	Slug        string         `gorm:"column:slug;size:64;uniqueIndex;not null"   json:"slug"`
	Name        string         `gorm:"column:name;size:128;not null"              json:"name"`
	Description string         `gorm:"column:description"                         json:"description,omitempty"`
	Category    string         `gorm:"column:category;size:32;index"              json:"category,omitempty"`
	Tags        pq.StringArray `gorm:"column:tags;type:text[];default:'{}'"       json:"tags,omitempty"`
	Spec        datatypes.JSON `gorm:"column:spec;type:jsonb;not null"            json:"spec"`
	SpecRaw     string         `gorm:"column:spec_raw;type:text;not null"         json:"spec_raw"`
	Builtin     bool           `gorm:"column:builtin;default:false"               json:"builtin"`
	OrgID       *int64         `gorm:"column:org_id"                              json:"org_id,omitempty"`
	CreatedBy   *int64         `gorm:"column:created_by"                          json:"created_by,omitempty"`

	// (CreatedAt / UpdatedAt / DeletedAt 来自 Base)
}

func (PipelineTemplate) TableName() string { return "pipeline_templates" }

// VisibleToOrg 返回模板对 orgID 是否可见 (全局或归属同 org)。
func (t *PipelineTemplate) VisibleToOrg(orgID int64) bool {
	if t.OrgID == nil {
		return true
	}
	return *t.OrgID == orgID
}

// 占位变量, 避免 unused warning (time 包以后扩展时会用)
var _ = time.Time{}
