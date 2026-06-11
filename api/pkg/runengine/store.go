// Package runengine — 多 stage 流水线调度 IO 层 (T2.2.4)。
package runengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// RunInfo orchestrate 需要的 run 元数据。
type RunInfo struct {
	ID          int64
	ProjectID   int64
	PipelineID  int64
	VersionID   int64
	Status      string
	Branch      string
	CommitSHA   string
	Message     string
	TriggerType string
}

// StageRow stages 表一行。
type StageRow struct {
	ID           int64
	RunID        int64
	StageID      string
	Name         string
	Status       string
	Needs        []string
	MatrixIndex  *int
	MatrixValues json.RawMessage
}

// StepRow steps 表一行。
type StepRow struct {
	ID            int64
	StageRecordID int64
	StepIndex     int
	Name          string
	Uses          string
	Status        string
}

var ErrRunNotFound = errors.New("run not found")

// LoadRun 加载 run 元数据 + project_id (经 pipelines 表)。
func LoadRun(ctx context.Context, db *sql.DB, runID int64) (*RunInfo, error) {
	var info RunInfo
	err := db.QueryRowContext(ctx, `
		SELECT r.id, p.project_id, r.pipeline_id, r.version_id, r.status,
		       COALESCE(r.branch, ''), COALESCE(r.commit_sha, ''),
		       COALESCE(r.message, ''), COALESCE(r.trigger_type, '')
		  FROM runs r
		  JOIN pipelines p ON p.id = r.pipeline_id
		 WHERE r.id = $1
	`, runID).Scan(
		&info.ID, &info.ProjectID, &info.PipelineID, &info.VersionID, &info.Status,
		&info.Branch, &info.CommitSHA, &info.Message, &info.TriggerType,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}
	return &info, nil
}

// LoadPipelineSpec 从 pipeline_versions 取 spec_raw。
func LoadPipelineSpec(ctx context.Context, db *sql.DB, versionID int64) ([]byte, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(spec_raw, '') FROM pipeline_versions WHERE id = $1
	`, versionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("pipeline version %d not found", versionID)
	}
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	return raw, nil
}

// HasPipelineStages 判断 version 是否配置了可执行的 stages (非空且非占位)。
func HasPipelineStages(ctx context.Context, db *sql.DB, versionID int64) (bool, error) {
	raw, err := LoadPipelineSpec(ctx, db, versionID)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 {
		return false, nil
	}
	p, err := parsePipeline(raw)
	if err != nil {
		return false, err
	}
	return p != nil && len(p.Stages) > 0, nil
}

// CountStages 返回 run 下已有 stage 行数 (bootstrap 幂等判断)。
func CountStages(ctx context.Context, db *sql.DB, runID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stages WHERE run_id = $1`, runID).Scan(&n)
	return n, err
}

// ListStages 列出 run 下所有 stage 行。
func ListStages(ctx context.Context, db *sql.DB, runID int64) ([]StageRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, stage_id, COALESCE(name, ''), COALESCE(status, 'pending'),
		       COALESCE(needs, '{}'), matrix_index, matrix_values
		  FROM stages WHERE run_id = $1 ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StageRow
	for rows.Next() {
		var s StageRow
		var needs pq.StringArray
		var matrixVals []byte
		if err := rows.Scan(&s.ID, &s.RunID, &s.StageID, &s.Name, &s.Status, &needs, &s.MatrixIndex, &matrixVals); err != nil {
			return nil, err
		}
		s.Needs = []string(needs)
		if len(matrixVals) > 0 {
			s.MatrixValues = matrixVals
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertStage 插入 stage 行, 返回 id。
func InsertStage(ctx context.Context, db *sql.DB, runID int64, stageID, name string, needs []string, matrixIndex *int, matrixValues json.RawMessage) (int64, error) {
	var id int64
	var matrixJSON any
	if len(matrixValues) > 0 {
		matrixJSON = matrixValues
	}
	err := db.QueryRowContext(ctx, `
		INSERT INTO stages (run_id, stage_id, name, status, needs, matrix_index, matrix_values)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6)
		RETURNING id
	`, runID, stageID, name, pq.Array(needs), matrixIndex, matrixJSON).Scan(&id)
	return id, err
}

// InsertStep 插入 step 行。
func InsertStep(ctx context.Context, db *sql.DB, stageRecordID int64, idx int, name, uses string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO steps (stage_record_id, step_index, name, uses, status)
		VALUES ($1, $2, $3, $4, 'pending')
		RETURNING id
	`, stageRecordID, idx, name, uses).Scan(&id)
	return id, err
}

// UpdateStageStatus 更新 stage 状态 + 时间戳。
func UpdateStageStatus(ctx context.Context, db *sql.DB, stageRecordID int64, status string, exitCode *int) error {
	now := time.Now().UTC()
	switch status {
	case "running", "approval":
		_, err := db.ExecContext(ctx, `
			UPDATE stages SET status = $1, started_at = COALESCE(started_at, $2) WHERE id = $3
		`, status, now, stageRecordID)
		return err
	case "success", "failed", "skipped", "canceled":
		_, err := db.ExecContext(ctx, `
			UPDATE stages SET status = $1, finished_at = $2, exit_code = $3 WHERE id = $4
		`, status, now, exitCode, stageRecordID)
		return err
	default:
		_, err := db.ExecContext(ctx, `UPDATE stages SET status = $1 WHERE id = $2`, status, stageRecordID)
		return err
	}
}

// UpdateStepStatus 更新 step 状态。
func UpdateStepStatus(ctx context.Context, db *sql.DB, stepID int64, status string, exitCode *int) error {
	now := time.Now().UTC()
	if status == "running" {
		_, err := db.ExecContext(ctx, `
			UPDATE steps SET status = $1, started_at = COALESCE(started_at, $2) WHERE id = $3
		`, status, now, stepID)
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE steps SET status = $1, finished_at = $2, exit_code = $3 WHERE id = $4
	`, status, now, exitCode, stepID)
	return err
}

// GetStageByID 按主键取 stage。
func GetStageByID(ctx context.Context, db *sql.DB, id int64) (*StageRow, error) {
	var s StageRow
	var needs pq.StringArray
	var matrixVals []byte
	err := db.QueryRowContext(ctx, `
		SELECT id, run_id, stage_id, COALESCE(name, ''), COALESCE(status, 'pending'),
		       COALESCE(needs, '{}'), matrix_index, matrix_values
		  FROM stages WHERE id = $1
	`, id).Scan(&s.ID, &s.RunID, &s.StageID, &s.Name, &s.Status, &needs, &s.MatrixIndex, &matrixVals)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("stage %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	s.Needs = []string(needs)
	if len(matrixVals) > 0 {
		s.MatrixValues = matrixVals
	}
	return &s, nil
}

// ListSteps 列出 stage 下 steps。
func ListSteps(ctx context.Context, db *sql.DB, stageRecordID int64) ([]StepRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, stage_record_id, COALESCE(step_index, 0), COALESCE(name, ''), COALESCE(uses, ''), COALESCE(status, 'pending')
		  FROM steps WHERE stage_record_id = $1 ORDER BY step_index ASC, id ASC
	`, stageRecordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StepRow
	for rows.Next() {
		var s StepRow
		if err := rows.Scan(&s.ID, &s.StageRecordID, &s.StepIndex, &s.Name, &s.Uses, &s.Status); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ApprovalRequestInfo 审批请求摘要 (orchestrate 同步用)。
type ApprovalRequestInfo struct {
	ID       int64
	StageID  string
	Status   string
}

// GetApprovalForStage 查 run+stage 的审批请求。
func GetApprovalForStage(ctx context.Context, db *sql.DB, runID int64, stageID string) (*ApprovalRequestInfo, error) {
	var a ApprovalRequestInfo
	err := db.QueryRowContext(ctx, `
		SELECT id, stage_id, status FROM approval_requests
		 WHERE run_id = $1 AND stage_id = $2
		 ORDER BY id DESC LIMIT 1
	`, runID, stageID).Scan(&a.ID, &a.StageID, &a.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
