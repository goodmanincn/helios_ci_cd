// Package model 定义所有 GORM 数据模型。每个 .go 文件对应一张/一组表。
package model

import (
	"time"

	"gorm.io/gorm"
)

// Base 是所有持久化模型的公共字段。
// 用 BIGSERIAL 自增主键 (与 DDL 保持一致)。
type Base struct {
	ID        int64          `gorm:"primaryKey;column:id"             json:"id"`
	CreatedAt time.Time      `gorm:"column:created_at;not null"       json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null"       json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"          json:"-"`
}

// BaseNoSoftDelete 用于纯 append-only 表 (如 audit_logs / pipeline_versions / runs)。
type BaseNoSoftDelete struct {
	ID        int64     `gorm:"primaryKey;column:id"       json:"id"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

// JSONMap 是 jsonb 列的通用映射类型 — 模型层用 datatypes.JSONMap 也行,
// 这里轻量自定义以避免再引一个包。
type JSONMap map[string]any
