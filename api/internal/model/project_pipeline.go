package model

import "gorm.io/datatypes"

// Project 对应 projects 表
type Project struct {
	Base
	OrgID         int64          `gorm:"column:org_id;not null;uniqueIndex:uq_projects_org_slug,priority:1" json:"org_id"`
	Name          string         `gorm:"column:name;size:128;not null"                                      json:"name"`
	Slug          string         `gorm:"column:slug;size:64;not null;uniqueIndex:uq_projects_org_slug,priority:2" json:"slug"`
	Description   string         `gorm:"column:description"                                                 json:"description,omitempty"`
	RepoURL       string         `gorm:"column:repo_url;not null"                                           json:"repo_url"`
	RepoType      string         `gorm:"column:repo_type;size:32;not null"                                  json:"repo_type"`
	DefaultBranch string         `gorm:"column:default_branch;size:128;default:'main'"                      json:"default_branch"`
	Visibility    string         `gorm:"column:visibility;size:16;default:'private'"                        json:"visibility"`
	Config        datatypes.JSON `gorm:"column:config;type:jsonb;default:'{}'"                              json:"config,omitempty"`
	CreatedBy     *int64         `gorm:"column:created_by"                                                  json:"created_by,omitempty"`

	Organization *Organization `gorm:"foreignKey:OrgID" json:"-"`
}

func (Project) TableName() string { return "projects" }

// Pipeline 对应 pipelines 表
type Pipeline struct {
	Base
	ProjectID        int64  `gorm:"column:project_id;not null;index"      json:"project_id"`
	Name             string `gorm:"column:name;size:128;not null"         json:"name"`
	Description      string `gorm:"column:description"                    json:"description,omitempty"`
	CurrentVersionID *int64 `gorm:"column:current_version_id"             json:"current_version_id,omitempty"`
	Enabled          bool   `gorm:"column:enabled;default:true"           json:"enabled"`
	CreatedBy        *int64 `gorm:"column:created_by"                     json:"created_by,omitempty"`

	Project        *Project         `gorm:"foreignKey:ProjectID"        json:"-"`
	CurrentVersion *PipelineVersion `gorm:"foreignKey:CurrentVersionID" json:"-"`
}

func (Pipeline) TableName() string { return "pipelines" }

// PipelineVersion 对应 pipeline_versions 表 (append-only)
type PipelineVersion struct {
	BaseNoSoftDelete
	PipelineID int64          `gorm:"column:pipeline_id;not null;uniqueIndex:uq_pv_pipeline_version,priority:1" json:"pipeline_id"`
	Version    int            `gorm:"column:version;not null;uniqueIndex:uq_pv_pipeline_version,priority:2"     json:"version"`
	Spec       datatypes.JSON `gorm:"column:spec;type:jsonb;not null"                                           json:"spec"`
	SpecRaw    string         `gorm:"column:spec_raw;type:text;not null"                                        json:"spec_raw"`
	CreatedBy  *int64         `gorm:"column:created_by"                                                         json:"created_by,omitempty"`
	Message    string         `gorm:"column:message"                                                            json:"message,omitempty"`
}

func (PipelineVersion) TableName() string { return "pipeline_versions" }
