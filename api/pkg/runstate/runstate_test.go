package runstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ===== 纯函数测试 (不需要 DB) =====

func TestCanTransition_Matrix(t *testing.T) {
	type tc struct {
		from, to string
		want     bool
	}
	cases := []tc{
		// 合法
		{StatusPending, StatusRunning, true},
		{StatusPending, StatusCanceled, true},
		{StatusRunning, StatusSuccess, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusCanceled, true},
		// 非法: 自循环
		{StatusPending, StatusPending, false},
		{StatusRunning, StatusRunning, false},
		// 非法: 跳过中间态
		{StatusPending, StatusSuccess, false},
		{StatusPending, StatusFailed, false},
		// 非法: 终态出发
		{StatusSuccess, StatusRunning, false},
		{StatusSuccess, StatusFailed, false},
		{StatusFailed, StatusSuccess, false},
		{StatusFailed, StatusRunning, false},
		{StatusCanceled, StatusRunning, false},
		{StatusCanceled, StatusSuccess, false},
		// 非法: 反向
		{StatusRunning, StatusPending, false},
		// 非法: 未知状态
		{"weird", StatusRunning, false},
		{StatusRunning, "weird", false},
		{"", "", false},
	}
	for _, c := range cases {
		got := CanTransition(c.from, c.to)
		if got != c.want {
			t.Errorf("CanTransition(%q,%q)=%v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{StatusSuccess, StatusFailed, StatusCanceled} {
		if !IsTerminal(s) {
			t.Errorf("%q should be terminal", s)
		}
	}
	for _, s := range []string{StatusPending, StatusRunning, ""} {
		if IsTerminal(s) {
			t.Errorf("%q should NOT be terminal", s)
		}
	}
}

func TestIsValidStatus(t *testing.T) {
	for _, s := range []string{
		StatusPending, StatusRunning, StatusSuccess, StatusFailed, StatusCanceled,
	} {
		if !IsValidStatus(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "weird", "Pending", "RUNNING"} {
		if IsValidStatus(s) {
			t.Errorf("%q should NOT be valid", s)
		}
	}
}

// ===== DB 集成测试 (要 HELIOS_TEST_DSN) =====

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("no HELIOS_TEST_DSN / HELIOS_DB_DSN, skip DB tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("db ping fail: %v", err)
	}
	return db
}

// seedPendingRun 建一个 pipeline + version + pending run, 返回 run_id + cleanup.
func seedPendingRun(t *testing.T, db *sql.DB) (int64, func()) {
	t.Helper()
	ctx := context.Background()
	// 用第一个 organization
	var orgID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&orgID); err != nil {
		t.Skipf("no org: %v", err)
	}
	suffix := fmt.Sprintf("rs-%d", time.Now().UnixNano())

	// project
	var projectID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO projects (org_id, name, slug, repo_url, repo_type, default_branch, visibility, config, created_at, updated_at)
		VALUES ($1, $2, $3, 'https://github.com/test/dummy', 'github', 'main', 'private', '{}'::jsonb, now(), now())
		RETURNING id
	`, orgID, "rs-test "+suffix, suffix).Scan(&projectID)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	// pipeline
	var pipelineID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO pipelines (project_id, name, description, enabled, created_at, updated_at)
		VALUES ($1, 'default', 'test', true, now(), now())
		RETURNING id
	`, projectID).Scan(&pipelineID)
	if err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	// version
	var versionID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO pipeline_versions (pipeline_id, version, spec, spec_raw, message, created_at)
		VALUES ($1, 1, '{"stages":[]}'::jsonb, 'stages: []', 'test', now())
		RETURNING id
	`, pipelineID).Scan(&versionID)
	if err != nil {
		t.Fatalf("insert version: %v", err)
	}
	// run
	var runID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO runs (pipeline_id, version_id, number, status, trigger_type, created_at)
		VALUES ($1, $2, 1, 'pending', 'manual', now())
		RETURNING id
	`, pipelineID, versionID).Scan(&runID)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return runID, func() {
		_, _ = db.Exec("DELETE FROM audit_logs WHERE resource_type='run' AND resource_id=$1", runID)
		_, _ = db.Exec("DELETE FROM runs WHERE id=$1", runID)
		_, _ = db.Exec("DELETE FROM pipeline_versions WHERE id=$1", versionID)
		_, _ = db.Exec("DELETE FROM pipelines WHERE id=$1", pipelineID)
		_, _ = db.Exec("DELETE FROM projects WHERE id=$1", projectID)
	}
}

func TestMachine_HappyPath_PendingRunningSuccess(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()

	ctx := context.Background()

	// pending → running
	if err := m.MarkRunning(ctx, runID, TransitionOpts{}); err != nil {
		t.Fatalf("running: %v", err)
	}
	s, _ := m.Status(ctx, runID)
	if s != StatusRunning {
		t.Errorf("status=%q want running", s)
	}

	// running → success
	if err := m.MarkSuccess(ctx, runID, TransitionOpts{Reason: "all green"}); err != nil {
		t.Fatalf("success: %v", err)
	}
	s, _ = m.Status(ctx, runID)
	if s != StatusSuccess {
		t.Errorf("status=%q want success", s)
	}

	// 终态再走任何转移 → ErrTerminal
	err := m.MarkFailed(ctx, runID, "late", TransitionOpts{})
	if !errors.Is(err, ErrTerminal) {
		t.Errorf("want ErrTerminal got %v", err)
	}

	// 验证 audit 至少 2 行
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE resource_type='run' AND resource_id=$1", runID).Scan(&n)
	if n < 2 {
		t.Errorf("audit count=%d want >=2", n)
	}

	// 验证 started_at / finished_at / duration_ms
	var startedAt, finishedAt sql.NullTime
	var durationMs int64
	_ = db.QueryRow("SELECT started_at, finished_at, duration_ms FROM runs WHERE id=$1", runID).
		Scan(&startedAt, &finishedAt, &durationMs)
	if !startedAt.Valid || !finishedAt.Valid {
		t.Errorf("started/finished_at missing")
	}
	if durationMs < 0 {
		t.Errorf("duration_ms=%d", durationMs)
	}
}

func TestMachine_FailedPath(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()

	_ = m.MarkRunning(ctx, runID, TransitionOpts{})
	if err := m.MarkFailed(ctx, runID, "boom", TransitionOpts{}); err != nil {
		t.Fatalf("failed: %v", err)
	}
	s, _ := m.Status(ctx, runID)
	if s != StatusFailed {
		t.Errorf("status=%q want failed", s)
	}
	var msg string
	_ = db.QueryRow("SELECT message FROM runs WHERE id=$1", runID).Scan(&msg)
	if msg == "" {
		t.Errorf("message should contain reason")
	}
}

func TestMachine_CancelFromPending(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()

	if err := m.MarkCanceled(ctx, runID, TransitionOpts{Reason: "user abort"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	s, _ := m.Status(ctx, runID)
	if s != StatusCanceled {
		t.Errorf("status=%q want canceled", s)
	}
}

func TestMachine_CancelFromRunning(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()
	_ = m.MarkRunning(ctx, runID, TransitionOpts{})
	if err := m.MarkCanceled(ctx, runID, TransitionOpts{}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

func TestMachine_IllegalSkipsAreRejected(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()

	// pending → success 直接禁止
	err := m.MarkSuccess(ctx, runID, TransitionOpts{})
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("want ErrIllegalTransition got %v", err)
	}
	// pending → failed 也禁止
	err = m.MarkFailed(ctx, runID, "x", TransitionOpts{})
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("want ErrIllegalTransition got %v", err)
	}
}

func TestMachine_NotFound(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	err := m.MarkRunning(context.Background(), 99999999, TransitionOpts{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound got %v", err)
	}
}

func TestMachine_Idempotent_SamePendingTwice(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()

	// pending → pending 应当幂等 (no-op)
	if _, _, _, err := m.Transition(ctx, runID, StatusPending, TransitionOpts{}); err != nil {
		t.Errorf("pending->pending should be no-op, got %v", err)
	}

	_ = m.MarkRunning(ctx, runID, TransitionOpts{})
	// running → running no-op
	if _, _, _, err := m.Transition(ctx, runID, StatusRunning, TransitionOpts{}); err != nil {
		t.Errorf("running->running no-op, got %v", err)
	}
}

func TestMachine_InvalidStatus(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	_, _, _, err := m.Transition(context.Background(), 1, "weird-state", TransitionOpts{})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("want ErrInvalidStatus got %v", err)
	}
}

func TestMachine_AuditPayloadHasFromTo(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	ctx := context.Background()
	_ = m.MarkRunning(ctx, runID, TransitionOpts{})
	var payload []byte
	_ = db.QueryRow(`
		SELECT payload FROM audit_logs
		 WHERE resource_type='run' AND resource_id=$1 AND action='run.running'
		 ORDER BY id DESC LIMIT 1
	`, runID).Scan(&payload)
	if len(payload) == 0 {
		t.Fatalf("audit payload empty")
	}
	got := string(payload)
	if !contains(got, `"from": "pending"`) && !contains(got, `"from":"pending"`) {
		t.Errorf("payload missing from=pending: %s", got)
	}
	if !contains(got, `"to": "running"`) && !contains(got, `"to":"running"`) {
		t.Errorf("payload missing to=running: %s", got)
	}
}

func TestMachine_Status_NotFound(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	_, err := m.Status(context.Background(), 99999999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound got %v", err)
	}
}

func TestMachine_Status_OK(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	m := New(db)
	runID, cleanup := seedPendingRun(t, db)
	defer cleanup()
	s, err := m.Status(context.Background(), runID)
	if err != nil || s != StatusPending {
		t.Errorf("got (%q,%v), want (pending,nil)", s, err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
