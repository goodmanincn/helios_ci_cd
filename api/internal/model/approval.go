package model

import (
	"time"

	"github.com/lib/pq"
)

// ApprovalRequest 对应 approval_requests 表 (E2.6 人工审批).
//
// 关系:
//   - 1 个 run 在一个 stage 上至多一条 pending 请求 (DB unique 部分索引)
//   - 状态终态: approved / rejected / timeout / canceled
//   - on_timeout 决定到期处理策略, 创建时写死 (避免 stage DSL 改动影响存量请求)
type ApprovalRequest struct {
	ID                int64          `gorm:"primaryKey"                                json:"id"`
	RunID             int64          `gorm:"column:run_id;not null;index"              json:"run_id"`
	StageID           string         `gorm:"column:stage_id;size:64;not null"          json:"stage_id"`
	RequiredApprovers pq.StringArray `gorm:"column:required_approvers;type:text[];not null" json:"required_approvers"`
	Mode              string         `gorm:"column:mode;size:16;not null;default:'any'" json:"mode"`
	Status            string         `gorm:"column:status;size:16;not null;default:'pending';index" json:"status"`
	OnTimeout         string         `gorm:"column:on_timeout;size:16;not null;default:'reject'" json:"on_timeout"`
	TimeoutAt         *time.Time     `gorm:"column:timeout_at"                         json:"timeout_at,omitempty"`
	CreatedAt         time.Time      `gorm:"column:created_at;not null"                json:"created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;not null"                json:"updated_at"`
}

func (ApprovalRequest) TableName() string { return "approval_requests" }

// Approval 对应 approvals 表 (审批投票记录, append-only).
//
// 唯一约束: (request_id, username) 防止同一用户重复投票.
// username='system' 表示 on_timeout=approve 自动批准.
type Approval struct {
	ID        int64     `gorm:"primaryKey"                          json:"id"`
	RequestID int64     `gorm:"column:request_id;not null;index"    json:"request_id"`
	UserID    *int64    `gorm:"column:user_id"                      json:"user_id,omitempty"`
	Username  string    `gorm:"column:username;size:64;not null"    json:"username"`
	Decision  string    `gorm:"column:decision;size:16;not null"    json:"decision"`
	Comment   string    `gorm:"column:comment"                      json:"comment,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;not null"          json:"created_at"`
}

func (Approval) TableName() string { return "approvals" }
