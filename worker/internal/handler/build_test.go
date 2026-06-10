package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// seedProjectWithConfig 建一个 project 并写入指定 config (含 build_command)。
// 返回 project_id + cleanup。
func seedProjectWithConfig(t *testing.T, db *sql.DB, config map[string]any) (int64, func()) {
	t.Helper()
	ctx := context.Background()
	var orgID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&orgID); err != nil {
		t.Skipf("no org: %v", err)
	}
	cfgBytes, _ := json.Marshal(config)
	var pid int64
	slug := "build-test-" + randSuffix()
	err := db.QueryRowContext(ctx, `
		INSERT INTO projects (org_id, name, slug, repo_url, repo_type, default_branch, visibility, config, created_at, updated_at)
		VALUES ($1, $2, $3, 'https://github.com/test/dummy', 'github', 'main', 'private', $4::jsonb, now(), now())
		RETURNING id
	`, orgID, "build-test "+slug, slug, cfgBytes).Scan(&pid)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return pid, func() { _, _ = db.Exec("DELETE FROM projects WHERE id=$1", pid) }
}

func randSuffix() string {
	return time.Now().Format("150405.000000")
}

// seedRunForProject 直接关联到指定 project 的 pipeline/version, 与已有 seedRun 区别是
// build handler 需要 project 配置, 不能复用 acme/api-gateway。
func seedRunForProject(t *testing.T, db *sql.DB, projectID int64, status string) (int64, func()) {
	t.Helper()
	ctx := context.Background()
	var pipelineID, versionID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO pipelines (project_id, name, description, enabled, created_at, updated_at)
		VALUES ($1, 'build-test-pl-'||gen_random_uuid()::text, 'unit test', true, now(), now())
		RETURNING id
	`, projectID).Scan(&pipelineID)
	if err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	err = db.QueryRowContext(ctx, `
		INSERT INTO pipeline_versions (pipeline_id, version, spec, spec_raw, created_at)
		VALUES ($1, 1, '{"stages":[]}'::jsonb, 'stages: []', now())
		RETURNING id
	`, pipelineID).Scan(&versionID)
	if err != nil {
		t.Fatalf("seed pipeline_version: %v", err)
	}
	var runID int64
	runParams := []any{pipelineID, versionID}
	if status == runstate.StatusRunning {
		runParams = append(runParams, status, time.Now().UTC())
		err = db.QueryRowContext(ctx, `
			INSERT INTO runs (pipeline_id, version_id, number, status, trigger_type, started_at, created_at)
			VALUES ($1, $2, 1, $3::text, 'push', $4, now())
			RETURNING id
		`, runParams...).Scan(&runID)
	} else {
		runParams = append(runParams, status)
		err = db.QueryRowContext(ctx, `
			INSERT INTO runs (pipeline_id, version_id, number, status, trigger_type, created_at)
			VALUES ($1, $2, 1, $3::text, 'push', now())
			RETURNING id
		`, runParams...).Scan(&runID)
	}
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return runID, func() {
		_, _ = db.Exec("DELETE FROM audit_logs WHERE resource_type='run' AND resource_id=$1", runID)
		_, _ = db.Exec("DELETE FROM runs WHERE id=$1", runID)
		_, _ = db.Exec("DELETE FROM pipeline_versions WHERE id=$1", versionID)
		_, _ = db.Exec("DELETE FROM pipelines WHERE id=$1", pipelineID)
	}
}

// 构造一条 build task。
func newBuildTask(t *testing.T, runID, projectID int64) *asynq.Task {
	t.Helper()
	body, _ := (&tasks.RunBuildPayload{RunID: runID, ProjectID: projectID}).Marshal()
	return asynq.NewTask(tasks.TypeRunBuild, body)
}

// 真正在 wsDir/src 建一个目录 (模拟 checkout 后的副作用)。
func ensureWorkspace(t *testing.T, base string, runID int64) string {
	t.Helper()
	src := filepath.Join(base, runIDStr(runID), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir wsDir: %v", err)
	}
	return src
}

func runIDStr(id int64) string {
	// 与 build handler 用的 %d 保持一致
	return os.Getenv("__no__") + intStr(id)
}

func intStr(id int64) string {
	// 简单 itoa, 避开 strconv 重复导入
	if id == 0 {
		return "0"
	}
	neg := false
	if id < 0 {
		neg = true
		id = -id
	}
	var buf [32]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ==== 1) no build_command → no-op success ====
func TestBuild_NoCommand_NoOpSuccess(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()

	tmpWs := t.TempDir()
	ensureWorkspace(t, tmpWs, runID)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 30*time.Second)
	if err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid)); err != nil {
		t.Fatalf("process: %v", err)
	}
	var st string
	_ = db.QueryRow("SELECT status FROM runs WHERE id=$1", runID).Scan(&st)
	if st != runstate.StatusSuccess {
		t.Errorf("status=%s want success", st)
	}
}

// ==== 2) build_command 成功 → success ====
func TestBuild_CommandSuccess(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "echo hello-helios > out.txt && cat out.txt",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()

	tmpWs := t.TempDir()
	wsSrc := ensureWorkspace(t, tmpWs, runID)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 30*time.Second)
	if err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid)); err != nil {
		t.Fatalf("process: %v", err)
	}
	// out.txt 应在 wsSrc 里
	if _, err := os.Stat(filepath.Join(wsSrc, "out.txt")); err != nil {
		t.Errorf("out.txt missing in %s: %v", wsSrc, err)
	}
	var st string
	_ = db.QueryRow("SELECT status FROM runs WHERE id=$1", runID).Scan(&st)
	if st != runstate.StatusSuccess {
		t.Errorf("status=%s want success", st)
	}
	// build.log 应存在并含 'hello-helios'
	logBytes, err := os.ReadFile(filepath.Join(tmpWs, runIDStr(runID), "build.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !containsString(string(logBytes), "hello-helios") {
		t.Errorf("log missing marker: %s", logBytes)
	}
}

// ==== 3) build_command 失败 → failed (SkipRetry → handler 返回 nil) ====
func TestBuild_CommandFailure(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "false",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()
	tmpWs := t.TempDir()
	ensureWorkspace(t, tmpWs, runID)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 30*time.Second)
	if err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid)); err != nil {
		t.Fatalf("process should return nil on user-command failure, got %v", err)
	}
	var st, msg string
	_ = db.QueryRow("SELECT status, COALESCE(message,'') FROM runs WHERE id=$1", runID).Scan(&st, &msg)
	if st != runstate.StatusFailed {
		t.Errorf("status=%s want failed", st)
	}
	if !containsString(msg, "exited") {
		t.Errorf("message should mention exit, got %q", msg)
	}
}

// ==== 4) workspace 不存在 → failed + SkipRetry ====
func TestBuild_MissingWorkspace(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "echo x",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()
	tmpWs := t.TempDir() // 不建 workspace

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 30*time.Second)
	err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry got %v", err)
	}
	var st string
	_ = db.QueryRow("SELECT status FROM runs WHERE id=$1", runID).Scan(&st)
	if st != runstate.StatusFailed {
		t.Errorf("status=%s want failed", st)
	}
}

// ==== 5) run 已是 terminal → skip 返回 nil 不动状态 ====
func TestBuild_AlreadyTerminal(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "true",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()
	// 先 mark success
	machine := runstate.New(db)
	_ = machine.MarkSuccess(context.Background(), runID, runstate.TransitionOpts{})

	h := NewBuild(projectrepo.New(db), machine, t.TempDir(), 30*time.Second)
	if err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid)); err != nil {
		t.Errorf("should be no-op got %v", err)
	}
}

// ==== 6) run pending (不是 running) → SkipRetry ====
func TestBuild_StatusPending_SkipRetry(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "true",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusPending)
	defer clRun()
	h := NewBuild(projectrepo.New(db), runstate.New(db), t.TempDir(), 30*time.Second)
	err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry got %v", err)
	}
}

// ==== 7) payload bad → SkipRetry ====
func TestBuild_BadPayload(t *testing.T) {
	db := requireDB(t)
	defer db.Close()
	h := NewBuild(projectrepo.New(db), runstate.New(db), t.TempDir(), 30*time.Second)
	bad := asynq.NewTask(tasks.TypeRunBuild, []byte("not-json"))
	err := h.ProcessTask(context.Background(), bad)
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry got %v", err)
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOfStr(haystack, needle) >= 0)
}
func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
