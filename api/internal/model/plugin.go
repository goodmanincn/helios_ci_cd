package model

import (
	"time"

	"gorm.io/datatypes"
)

// Plugin 对应 plugins 表 (M9 插件市场).
//
// 一个插件 = (namespace, name) 唯一; slug 由 DB 生成列拼成 namespace/name.
// official=true 由 seed 写入 (helios/* 三个), 不允许删除/卸下.
type Plugin struct {
	Base
	Namespace     string `gorm:"column:namespace;size:64;not null"        json:"namespace"`
	Name          string `gorm:"column:name;size:64;not null"             json:"name"`
	Slug          string `gorm:"column:slug;size:128;->"                  json:"slug"`
	Description   string `gorm:"column:description"                       json:"description,omitempty"`
	Category      string `gorm:"column:category;size:32;index"            json:"category,omitempty"`
	Publisher     string `gorm:"column:publisher;size:128"                json:"publisher,omitempty"`
	Repository    string `gorm:"column:repository"                        json:"repository,omitempty"`
	Verified      bool   `gorm:"column:verified;default:false"            json:"verified"`
	Official      bool   `gorm:"column:official;default:false"            json:"official"`
	Downloads     int64  `gorm:"column:downloads;default:0"               json:"downloads"`
	LatestVersion string `gorm:"column:latest_version;size:64"            json:"latest_version,omitempty"`
}

func (Plugin) TableName() string { return "plugins" }

// PluginVersion 一个插件的某个 version. action_yml + action_spec 双存.
type PluginVersion struct {
	ID         int64          `gorm:"primaryKey;column:id"               json:"id"`
	PluginID   int64          `gorm:"column:plugin_id;not null;index"    json:"plugin_id"`
	Version    string         `gorm:"column:version;size:64;not null"    json:"version"`
	ActionYML  string         `gorm:"column:action_yml;type:text;not null" json:"action_yml"`
	ActionSpec datatypes.JSON `gorm:"column:action_spec;type:jsonb;not null" json:"action_spec"`
	Readme     string         `gorm:"column:readme;type:text"            json:"readme,omitempty"`
	Changelog  string         `gorm:"column:changelog;type:text"         json:"changelog,omitempty"`
	IsLatest   bool           `gorm:"column:is_latest;default:false"     json:"is_latest"`
	CreatedAt  time.Time      `gorm:"column:created_at;not null"         json:"created_at"`
}

func (PluginVersion) TableName() string { return "plugin_versions" }

// PluginInstallation 一个 org 装了某 plugin 的某 version. (org_id, plugin_id) 唯一.
type PluginInstallation struct {
	ID          int64    `gorm:"primaryKey;column:id"              json:"id"`
	OrgID       int64    `gorm:"column:org_id;not null;index"      json:"org_id"`
	PluginID    int64    `gorm:"column:plugin_id;not null"         json:"plugin_id"`
	VersionID   int64    `gorm:"column:version_id;not null"        json:"version_id"`
	InstalledBy *int64    `gorm:"column:installed_by"               json:"installed_by,omitempty"`
	InstalledAt time.Time `gorm:"column:installed_at;not null"      json:"installed_at"`
}

func (PluginInstallation) TableName() string { return "plugin_installations" }
