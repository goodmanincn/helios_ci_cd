package handler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hibiken/asynq"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/helios-cicd/helios/api/pkg/runrepo"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// fakeCloner 测试用 cloner,记录调用并按需返回错误。
type fakeCloner struct {
	calls   int
	lastDir string
	lastURL string
	lastBr  string
	lastSHA string
	err     error
	create  bool // true 时真的建目录,模拟 clone 成功的副作用
}

func (f *fakeCloner) Clone(_ context.Context, url, branch, sha, dir string) error {
	f.calls++
	f.lastURL, f.lastBr, f.lastSHA, f.lastDir = url, branch, sha, dir
	if f.err != nil {
		return f.err
	}
	if f.create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644)
	}
	return nil
}

// 集成测试需要真实 PG (worker 直接 SQL),通过 HELIOS_TEST_DSN 注入,否则 skip。
func requireDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DSN")
	if dsn == "" {
		t.Skip("HELIOS_TEST_DSN not set, skipping DB integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

// 在 DB 里建一条最小可用的 run (跳过 FK 检查的最少链路),返回 run.id 与清理函数。
func seedRun(t *testing.T, db *sql.DB) (int64, func()) {
	t.Helper()
	ctx := context.Background()
	// 用已存在的 seed org+project (acme/api-gateway) 找一个 pipeline
	var pipelineID, versionID int64
	err := db.QueryRowContext(ctx, `
		WITH p AS (
		  SELECT id FROM projects WHERE slug='api-gateway' LIMIT 1
		),
		pl AS (
		  INSERT INTO pipelines (project_id, name, description, enabled, created_at, updated_at)
		  SELECT p.id, 'test-pipeline-'||gen_random_uuid()::text, 'unit test', true, now(), now()
		  FROM p RETURNING id
		),
		pv AS (
		  INSERT INTO pipeline_versions (pipeline_id, version, spec, spec_raw, created_at)
		  SELECT pl.id, 1, '{"stages":[]}'::jsonb, 'stages: []', now() FROM pl RETURNING id, pipeline_id
		)
		SELECT pipeline_id, id FROM pv
	`).Scan(&pipelineID, &versionID)
	if err != nil {
		t.Fatalf("seed pipeline/version: %v", err)
	}

	var runID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO runs (pipeline_id, version_id, number, status, trigger_type, commit_sha, branch, created_at)
		VALUES ($1, $2, 1, 'pending', 'push', 'sha-test', 'main', now())
		RETURNING id
	`, pipelineID, versionID).Scan(&runID)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}

	cleanup := func() {
		_, _ = db.Exec(`DELETE FROM runs WHERE id=$1`, runID)
		_, _ = db.Exec(`DELETE FROM pipeline_versions WHERE id=$1`, versionID)
		_, _ = db.Exec(`DELETE FROM pipelines WHERE id=$1`, pipelineID)
	}
	return runID, cleanup
}

// happy path: pending → running, cloner 被调用, workspace 目录存在
func TestCheckout_HappyPath(t *testing.T) {
	db := requireDB(t)
	defer func() { _ = db.Close() }()

	runID, cleanup := seedRun(t, db)
	defer cleanup()

	tmp := t.TempDir()
	repo := runrepo.New(db)
	cloner := &fakeCloner{create: true}
	h := NewCheckout(repo, cloner, tmp)

	p := &tasks.GitCheckoutPayload{
		RunID: runID, ProjectID: 1,
		RepoURL: "https://github.com/acme/api.git", Branch: "main", CommitSHA: "abc",
	}
	body, _ := p.Marshal()
	task := asynq.NewTask(tasks.TypeGitCheckout, body)

	if err := h.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	// run 应该是 running
	meta, err := repo.GetMeta(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != runrepo.StatusRunning {
		t.Fatalf("expect running, got %s", meta.Status)
	}

	// cloner 被调一次, 参数对
	if cloner.calls != 1 {
		t.Fatalf("expect 1 clone call, got %d", cloner.calls)
	}
	if cloner.lastBr != "main" || cloner.lastSHA != "abc" {
		t.Fatalf("clone args mismatch: %+v", cloner)
	}
	wantDir := filepath.Join(tmp, "999")
	_ = wantDir
	// workspace 目录存在
	if _, err := os.Stat(cloner.lastDir); err != nil {
		t.Fatalf("workspace dir missing: %v", err)
	}
}

// run 不存在 → SkipRetry
func TestCheckout_RunNotFound(t *testing.T) {
	db := requireDB(t)
	defer func() { _ = db.Close() }()

	h := NewCheckout(runrepo.New(db), &fakeCloner{}, t.TempDir())
	p := &tasks.GitCheckoutPayload{RunID: 99999999, ProjectID: 1, RepoURL: "x", Branch: "main"}
	body, _ := p.Marshal()
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeGitCheckout, body))
	if err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("expect SkipRetry, got %v", err)
	}
}

// payload 损坏 → SkipRetry
func TestCheckout_BadPayload(t *testing.T) {
	h := NewCheckout(nil, &fakeCloner{}, t.TempDir())
	task := asynq.NewTask(tasks.TypeGitCheckout, []byte("not json"))
	err := h.ProcessTask(context.Background(), task)
	if err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("expect SkipRetry, got %v", err)
	}
}

// 已终态的 run → 直接 return nil 不重复处理
func TestCheckout_AlreadyTerminal(t *testing.T) {
	db := requireDB(t)
	defer func() { _ = db.Close() }()

	runID, cleanup := seedRun(t, db)
	defer cleanup()
	// 推进到 success (用直 SQL,模拟另一个 worker 先做完)
	_, err := db.Exec(`UPDATE runs SET status='success', finished_at=now() WHERE id=$1`, runID)
	if err != nil {
		t.Fatal(err)
	}

	cloner := &fakeCloner{}
	h := NewCheckout(runrepo.New(db), cloner, t.TempDir())
	p := &tasks.GitCheckoutPayload{RunID: runID, ProjectID: 1, RepoURL: "x", Branch: "main"}
	body, _ := p.Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeGitCheckout, body)); err != nil {
		t.Fatalf("expect nil error, got %v", err)
	}
	if cloner.calls != 0 {
		t.Fatalf("expect cloner not called, got %d", cloner.calls)
	}
}

// clone 失败 → 返回 error (让 asynq 重试)
func TestCheckout_CloneFails(t *testing.T) {
	db := requireDB(t)
	defer func() { _ = db.Close() }()

	runID, cleanup := seedRun(t, db)
	defer cleanup()

	cloner := &fakeCloner{err: errors.New("network down")}
	h := NewCheckout(runrepo.New(db), cloner, t.TempDir())
	p := &tasks.GitCheckoutPayload{RunID: runID, ProjectID: 1, RepoURL: "x", Branch: "main"}
	body, _ := p.Marshal()
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeGitCheckout, body))
	if err == nil {
		t.Fatal("expect error")
	}
	// run 应该是 running (clone 失败但已 mark running)
	meta, _ := runrepo.New(db).GetMeta(context.Background(), runID)
	if meta.Status != runrepo.StatusRunning {
		t.Fatalf("expect running, got %s", meta.Status)
	}
}

// OnRetryExhausted → run 转 failed
func TestCheckout_OnRetryExhausted(t *testing.T) {
	db := requireDB(t)
	defer func() { _ = db.Close() }()

	runID, cleanup := seedRun(t, db)
	defer cleanup()
	// 先 mark running 模拟实际场景
	_, _ = runrepo.New(db).MarkRunning(context.Background(), runID)

	h := NewCheckout(runrepo.New(db), &fakeCloner{}, t.TempDir())
	p := &tasks.GitCheckoutPayload{RunID: runID, ProjectID: 1, RepoURL: "x", Branch: "main"}
	body, _ := p.Marshal()
	h.OnRetryExhausted(context.Background(), asynq.NewTask(tasks.TypeGitCheckout, body), errors.New("simulated"))

	meta, _ := runrepo.New(db).GetMeta(context.Background(), runID)
	if meta.Status != runrepo.StatusFailed {
		t.Fatalf("expect failed, got %s", meta.Status)
	}
}
