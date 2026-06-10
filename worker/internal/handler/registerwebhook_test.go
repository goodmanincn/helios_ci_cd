package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/helios-cicd/helios/api/pkg/git"
	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// openTestDB 拿 HELIOS_TEST_DSN 或跳过测试。
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := getenvFirst("HELIOS_TEST_DSN", "HELIOS_DB_DSN")
	if dsn == "" {
		t.Skip("HELIOS_TEST_DSN / HELIOS_DB_DSN not set, skip")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("db ping fail (%v), skip", err)
	}
	return db
}

func getenvFirst(keys ...string) string {
	for _, k := range keys {
		if v := envGet(k); v != "" {
			return v
		}
	}
	return ""
}

// 一个测试用的 GitHub mock server, 返回固定 hook id.
func mockGitHubServer(t *testing.T, wantOwner, wantRepo string, statusCode int, returnHookID int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		expectedPath := fmt.Sprintf("/repos/%s/%s/hooks", wantOwner, wantRepo)
		if r.URL.Path != expectedPath {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if statusCode >= 400 {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(`{"message":"mock error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		hookURL, _ := req["config"].(map[string]any)["url"].(string)
		_, _ = fmt.Fprintf(w, `{"id":%d,"url":%q,"active":true,"config":{"url":%q}}`,
			returnHookID, hookURL, hookURL)
	}))
	return srv
}

// 在 DB 创建一个临时 project, 返回 id + cleanup.
func seedProject(t *testing.T, db *sql.DB, repoType string, configJSON string) (int64, func()) {
	t.Helper()
	ctx := context.Background()
	// 找一个已有 org_id (种子已有 acme), 否则随便建一个
	var orgID int64
	err := db.QueryRowContext(ctx, "SELECT id FROM organizations ORDER BY id LIMIT 1").Scan(&orgID)
	if err != nil {
		t.Skipf("no organization in db (%v), skip", err)
	}
	if configJSON == "" {
		configJSON = `{}`
	}
	suffix := fmt.Sprintf("e2etest-%d", time.Now().UnixNano())
	var id int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO projects (org_id, name, slug, repo_url, repo_type, default_branch, visibility, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'main', 'private', $6::jsonb, now(), now())
		RETURNING id
	`, orgID, "T1.2.4-test "+suffix, suffix,
		"https://github.com/test/dummy", repoType, configJSON).Scan(&id)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id, func() {
		_, _ = db.Exec("DELETE FROM projects WHERE id=$1", id)
	}
}

func TestWebhookRegister_Success_WritesConfig(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := projectrepo.New(db)
	srv := mockGitHubServer(t, "octocat", "Hello-World", 0, 42)
	defer srv.Close()

	provider := git.NewGitHubProvider(git.GitHubConfig{
		BaseURL: srv.URL,
		Token:   "fake-token",
	})

	pid, cleanup := seedProject(t, db, "github", `{}`)
	defer cleanup()

	h := NewWebhookRegister(repo, provider, "https://helios.example.com", "")
	payload := &tasks.WebhookRegisterPayload{
		ProjectID: pid,
		Owner:     "octocat",
		Repo:      "Hello-World",
		RepoURL:   "https://github.com/octocat/Hello-World",
	}
	body, _ := payload.Marshal()
	task := asynq.NewTask(tasks.TypeWebhookRegister, body)

	if err := h.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	// 验证 config 写回
	proj, err := repo.Get(context.Background(), pid)
	if err != nil {
		t.Fatalf("get back: %v", err)
	}
	if v, _ := proj.Config["webhook_id"].(float64); v != 42 {
		t.Errorf("webhook_id = %v, want 42", proj.Config["webhook_id"])
	}
	if v, _ := proj.Config["webhook_secret"].(string); v == "" {
		t.Errorf("webhook_secret empty (should be generated)")
	} else if len(v) != 64 { // hex(32) = 64 chars
		t.Errorf("webhook_secret len=%d want 64", len(v))
	}
	if v, _ := proj.Config["webhook_provider"].(string); v != "github" {
		t.Errorf("webhook_provider = %q", v)
	}
	if v, _ := proj.Config["webhook_active"].(bool); !v {
		t.Errorf("webhook_active should be true")
	}
	if v, _ := proj.Config["webhook_registered_at"].(string); v == "" {
		t.Errorf("webhook_registered_at empty")
	}
	if v, _ := proj.Config["webhook_url"].(string); !strings.Contains(v, "/webhooks/github/") {
		t.Errorf("webhook_url = %q (no callback path)", v)
	}
}

func TestWebhookRegister_ReuseExistingSecret(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := projectrepo.New(db)
	srv := mockGitHubServer(t, "octocat", "Hello-World", 0, 100)
	defer srv.Close()
	provider := git.NewGitHubProvider(git.GitHubConfig{BaseURL: srv.URL, Token: "fake"})

	pid, cleanup := seedProject(t, db, "github", `{"webhook_secret":"existing-secret-xyz"}`)
	defer cleanup()

	h := NewWebhookRegister(repo, provider, "https://helios.example.com", "")
	body, _ := (&tasks.WebhookRegisterPayload{
		ProjectID: pid, Owner: "octocat", Repo: "Hello-World", RepoURL: "https://github.com/octocat/Hello-World",
	}).Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeWebhookRegister, body)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	proj, _ := repo.Get(context.Background(), pid)
	if v, _ := proj.Config["webhook_secret"].(string); v != "existing-secret-xyz" {
		t.Errorf("secret not reused: got %q", v)
	}
}

func TestWebhookRegister_GitHub401_SkipsRetry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := projectrepo.New(db)
	srv := mockGitHubServer(t, "octocat", "Hello-World", http.StatusUnauthorized, 0)
	defer srv.Close()
	provider := git.NewGitHubProvider(git.GitHubConfig{BaseURL: srv.URL, Token: "bad"})

	pid, cleanup := seedProject(t, db, "github", `{}`)
	defer cleanup()

	h := NewWebhookRegister(repo, provider, "https://helios.example.com", "")
	body, _ := (&tasks.WebhookRegisterPayload{
		ProjectID: pid, Owner: "octocat", Repo: "Hello-World", RepoURL: "x",
	}).Marshal()
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeWebhookRegister, body))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry, got %v", err)
	}
	proj, _ := repo.Get(context.Background(), pid)
	if v, _ := proj.Config["webhook_error"].(string); v == "" {
		t.Errorf("webhook_error should be recorded")
	}
}

func TestWebhookRegister_ProjectGone_SkipsRetry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := projectrepo.New(db)
	provider := git.NewGitHubProvider(git.GitHubConfig{Token: "x"})
	h := NewWebhookRegister(repo, provider, "https://helios.example.com", "")
	body, _ := (&tasks.WebhookRegisterPayload{
		ProjectID: 99999999, Owner: "o", Repo: "r",
	}).Marshal()
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeWebhookRegister, body))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry, got %v", err)
	}
}

func TestWebhookRegister_BadPayload_SkipsRetry(t *testing.T) {
	repo := projectrepo.New(nil)
	provider := git.NewGitHubProvider(git.GitHubConfig{Token: "x"})
	h := NewWebhookRegister(repo, provider, "https://helios.example.com", "")
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeWebhookRegister, []byte(`{not json`)))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("want SkipRetry, got %v", err)
	}
}

func TestWebhookRegister_NoPublicURL_ReturnsError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := projectrepo.New(db)
	provider := git.NewGitHubProvider(git.GitHubConfig{Token: "x"})
	pid, cleanup := seedProject(t, db, "github", `{}`)
	defer cleanup()
	// publicURL 空且没有 env
	_ = envUnset("HELIOS_PUBLIC_API_BASE")
	h := NewWebhookRegister(repo, provider, "", "")
	body, _ := (&tasks.WebhookRegisterPayload{
		ProjectID: pid, Owner: "o", Repo: "r",
	}).Marshal()
	err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeWebhookRegister, body))
	if err == nil {
		t.Errorf("want error when public URL missing")
	}
}

// === os env helpers ===

func envGet(k string) string  { return os.Getenv(k) }
func envUnset(k string) error { return os.Unsetenv(k) }
