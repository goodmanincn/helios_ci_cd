package model

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

// Run 对应 runs 表 (append-only)
type Run struct {
	ID           int64          `gorm:"primaryKey"                                       json:"id"`
	PipelineID   int64          `gorm:"column:pipeline_id;not null;uniqueIndex:uq_runs_pipeline_number,priority:1" json:"pipeline_id"`
	VersionID    int64          `gorm:"column:version_id;not null"                       json:"version_id"`
	Number       int            `gorm:"column:number;not null;uniqueIndex:uq_runs_pipeline_number,priority:2" json:"number"`
	Status       string         `gorm:"column:status;size:32;not null;index"             json:"status"`
	TriggerType  string         `gorm:"column:trigger_type;size:32"                      json:"trigger_type,omitempty"`
	TriggerData  datatypes.JSON `gorm:"column:trigger_data;type:jsonb"                   json:"trigger_data,omitempty"`
	CommitSHA    string         `gorm:"column:commit_sha;size:64"                        json:"commit_sha,omitempty"`
	Branch       string         `gorm:"column:branch;size:255"                           json:"branch,omitempty"`
	Message      string         `gorm:"column:message"                                   json:"message,omitempty"`
	TriggeredBy  *int64         `gorm:"column:triggered_by"                              json:"triggered_by,omitempty"`
	StartedAt    *time.Time     `gorm:"column:started_at"                                json:"started_at,omitempty"`
	FinishedAt   *time.Time     `gorm:"column:finished_at"                               json:"finished_at,omitempty"`
	DurationMs   int            `gorm:"column:duration_ms"                               json:"duration_ms,omitempty"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null"                       json:"created_at"`
}

func (Run) TableName() string { return "runs" }

// Stage 对应 stages 表
type Stage struct {
	ID           int64          `gorm:"primaryKey"                                      json:"id"`
	RunID        int64          `gorm:"column:run_id;not null;index"                    json:"run_id"`
	StageID      string         `gorm:"column:stage_id;size:64;not null"                json:"stage_id"`
	Name         string         `gorm:"column:name;size:128"                            json:"name,omitempty"`
	Status       string         `gorm:"column:status;size:32"                           json:"status,omitempty"`
	Needs        pq.StringArray `gorm:"column:needs;type:text[]"                        json:"needs,omitempty"`
	MatrixIndex  *int           `gorm:"column:matrix_index"                             json:"matrix_index,omitempty"`
	MatrixValues datatypes.JSON `gorm:"column:matrix_values;type:jsonb"                 json:"matrix_values,omitempty"`
	StartedAt    *time.Time     `gorm:"column:started_at"                               json:"started_at,omitempty"`
	FinishedAt   *time.Time     `gorm:"column:finished_at"                              json:"finished_at,omitempty"`
	ExitCode     *int           `gorm:"column:exit_code"                                json:"exit_code,omitempty"`
}

func (Stage) TableName() string { return "stages" }

// Step 对应 steps 表
type Step struct {
	ID            int64      `gorm:"primaryKey"                                  json:"id"`
	StageRecordID int64      `gorm:"column:stage_record_id;not null;index"       json:"stage_record_id"`
	StepIndex     *int       `gorm:"column:step_index"                           json:"step_index,omitempty"`
	Name          string     `gorm:"column:name;size:255"                        json:"name,omitempty"`
	Uses          string     `gorm:"column:uses;size:255"                        json:"uses,omitempty"`
	Status        string     `gorm:"column:status;size:32"                       json:"status,omitempty"`
	ExitCode      *int       `gorm:"column:exit_code"                            json:"exit_code,omitempty"`
	LogObject     string     `gorm:"column:log_object"                           json:"log_object,omitempty"`
	LogSize       int64      `gorm:"column:log_size"                             json:"log_size,omitempty"`
	StartedAt     *time.Time `gorm:"column:started_at"                           json:"started_at,omitempty"`
	FinishedAt    *time.Time `gorm:"column:finished_at"                          json:"finished_at,omitempty"`
}

func (Step) TableName() string { return "steps" }

// AuditLog 对应 audit_logs 表 (append-only)
type AuditLog struct {
	BaseNoSoftDelete
	ActorID      *int64         `gorm:"column:actor_id"                          json:"actor_id,omitempty"`
	ActorIP      string         `gorm:"column:actor_ip;type:inet"                json:"actor_ip,omitempty"`
	OrgID        *int64         `gorm:"column:org_id"                            json:"org_id,omitempty"`
	Action       string         `gorm:"column:action;size:128;not null"          json:"action"`
	ResourceType string         `gorm:"column:resource_type;size:64"             json:"resource_type,omitempty"`
	ResourceID   *int64         `gorm:"column:resource_id"                       json:"resource_id,omitempty"`
	Payload      datatypes.JSON `gorm:"column:payload;type:jsonb"                json:"payload,omitempty"`
	Result       string         `gorm:"column:result;size:16"                    json:"result,omitempty"`
}

func (AuditLog) TableName() string { return "audit_logs" }
