// timeout_test.go — Timeouter 三策略 + 幂等 + 时钟漂移 (T2.6.3).
package approval

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/runstate"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("no HELIOS_TEST_DB_DSN / HELIOS_DB_DSN, skip approval timeout DB tests")
	}
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("db ping: %v", err)
	}
	return db
}

type fxIDs struct {
	orgID, projectID, pipelineID, versionID, runID int64
}

// seedFixture 落一套 org→project→pipeline→version→run(approval)→approval_request,
// 返回 ids + cleanup (倒序删).
func seedFixture(t *testing.T, db *sql.DB, onTimeout string, reqStatus string, timeoutAt time.Time) (fxIDs, int64) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("at-%d", time.Now().UnixNano())
	var ids fxIDs

	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES ($1, 'TimeoutTestOrg') RETURNING id`,
		suffix).Scan(&ids.orgID))

	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO projects (org_id, name, slug, repo_url, repo_type, default_branch, visibility, config, created_at, updated_at)
		VALUES ($1, 'ap', $2, '/tmp/x', 'github', 'main', 'private', '{}'::jsonb, now(), now())
		RETURNING id
	`, ids.orgID, suffix).Scan(&ids.projectID))

	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO pipelines (project_id, name, description, enabled, created_at, updated_at)
		 VALUES ($1, 'default', '', true, now(), now()) RETURNING id`,
		ids.projectID).Scan(&ids.pipelineID))

	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO pipeline_versions (pipeline_id, version, spec, spec_raw, created_at)
		 VALUES ($1, 1, '{}'::jsonb, '{}', now()) RETURNING id`,
		ids.pipelineID).Scan(&ids.versionID))

	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO runs (pipeline_id, version_id, number, status, trigger_type, started_at, created_at)
		VALUES ($1, $2, 1, 'approval', 'webhook', now(), now())
		RETURNING id
	`, ids.pipelineID, ids.versionID).Scan(&ids.runID))

	var requestID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO approval_requests
		  (run_id, stage_id, required_approvers, mode, status, on_timeout, timeout_at, created_at, updated_at)
		VALUES ($1, 'manual', ARRAY['alice'], 'any', $2, $3, $4, now(), now())
		RETURNING id
	`, ids.runID, reqStatus, onTimeout, timeoutAt).Scan(&requestID))

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM approvals WHERE request_id IN (SELECT id FROM approval_requests WHERE run_id=$1)`, ids.runID)
		_, _ = db.Exec(`DELETE FROM approval_requests WHERE run_id=$1`, ids.runID)
		_, _ = db.Exec(`DELETE FROM audit_logs WHERE (resource_type IN ('run','approval_request')) AND resource_id IN ($1, $2)`, ids.runID, requestID)
		_, _ = db.Exec(`DELETE FROM runs WHERE id=$1`, ids.runID)
		_, _ = db.Exec(`DELETE FROM pipeline_versions WHERE id=$1`, ids.versionID)
		_, _ = db.Exec(`DELETE FROM pipelines WHERE id=$1`, ids.pipelineID)
		_, _ = db.Exec(`DELETE FROM projects WHERE id=$1`, ids.projectID)
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, ids.orgID)
	})
	return ids, requestID
}

func newTimeouter(db *sql.DB) *Timeouter {
	return NewTimeouter(db, runstate.New(db))
}

// ---- reject ----

func TestTimeout_Reject_RunToTimeout(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	past := time.Now().UTC().Add(-1 * time.Minute)
	ids, reqID := seedFixture(t, db, "reject", "pending", past)
	tm := newTimeouter(db)

	res, err := tm.HandleTimeout(context.Background(), reqID)
	require.NoError(t, err)
	require.False(t, res.NoOp)
	require.Equal(t, "timeout", res.NewStatus)
	require.Equal(t, runstate.StatusTimeout, res.RunStatus)

	var runStatus, reqStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM runs WHERE id=$1`, ids.runID).Scan(&runStatus))
	require.NoError(t, db.QueryRow(`SELECT status FROM approval_requests WHERE id=$1`, reqID).Scan(&reqStatus))
	require.Equal(t, "timeout", runStatus)
	require.Equal(t, "timeout", reqStatus)
}

// ---- approve ----

func TestTimeout_Approve_RunToRunning_WritesSystemRow(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	past := time.Now().UTC().Add(-1 * time.Minute)
	ids, reqID := seedFixture(t, db, "approve", "pending", past)
	tm := newTimeouter(db)

	res, err := tm.HandleTimeout(context.Background(), reqID)
	require.NoError(t, err)
	require.False(t, res.NoOp)
	require.Equal(t, "approved", res.NewStatus)
	require.Equal(t, runstate.StatusRunning, res.RunStatus)

	var runStatus, reqStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM runs WHERE id=$1`, ids.runID).Scan(&runStatus))
	require.NoError(t, db.QueryRow(`SELECT status FROM approval_requests WHERE id=$1`, reqID).Scan(&reqStatus))
	require.Equal(t, "running", runStatus)
	require.Equal(t, "approved", reqStatus)

	// system 投票行存在
	var sysCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM approvals WHERE request_id=$1 AND username='system'`, reqID).Scan(&sysCount))
	require.Equal(t, 1, sysCount)
}

// ---- pause ----

func TestTimeout_Pause_RunStaysApproval(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	past := time.Now().UTC().Add(-1 * time.Minute)
	ids, reqID := seedFixture(t, db, "pause", "pending", past)
	tm := newTimeouter(db)

	res, err := tm.HandleTimeout(context.Background(), reqID)
	require.NoError(t, err)
	require.False(t, res.NoOp)
	require.Equal(t, "timeout", res.NewStatus)
	require.Equal(t, "", res.RunStatus, "pause 不动 run 状态")

	var runStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM runs WHERE id=$1`, ids.runID).Scan(&runStatus))
	require.Equal(t, "approval", runStatus, "pause: run 维持 approval")
}

// ---- 幂等: Approve 抢先 ----

func TestTimeout_AlreadyDecided_Noop(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	past := time.Now().UTC().Add(-1 * time.Minute)
	_, reqID := seedFixture(t, db, "reject", "approved", past) // 模拟已 approved 终态
	tm := newTimeouter(db)

	res, err := tm.HandleTimeout(context.Background(), reqID)
	require.NoError(t, err)
	require.True(t, res.NoOp)
	require.Equal(t, "approved", res.NewStatus)
}

// ---- 时钟漂移: 提早触发 ----

func TestTimeout_EarlyFire_Noop(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	future := time.Now().UTC().Add(30 * time.Second)
	_, reqID := seedFixture(t, db, "reject", "pending", future)
	tm := newTimeouter(db)

	res, err := tm.HandleTimeout(context.Background(), reqID)
	require.NoError(t, err)
	require.True(t, res.NoOp, "未到期不应处理")
}

func TestTimeout_NotFound(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	tm := newTimeouter(db)
	_, err := tm.HandleTimeout(context.Background(), 9999999)
	require.Error(t, err)
}
