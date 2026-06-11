package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/helios-cicd/helios/cli/internal/config"
)

// 用 httptest 起一个迷你 api server, 把 helios CLI 命令树端到端跑通.
// HELIOS_HOME 指到 t.TempDir() 隔离每个用例.

func setupCLITest(t *testing.T, handler http.HandlerFunc) (server *httptest.Server, configDir string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HELIOS_HOME", dir)
	t.Setenv("HELIOS_PROFILE", "") // 避免外部影响
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, dir
}

// 把 token 预置好, 跳过 login 流程
func presetCreds(t *testing.T, srv string, orgID int64) {
	t.Helper()
	cfg, _ := config.LoadConfig()
	cfg.DefaultProfile = "default"
	cfg.Profiles["default"] = config.Profile{Server: srv, OrgID: orgID}
	_ = config.SaveConfig(cfg)

	creds, _ := config.LoadCredentials()
	creds.Tokens["default"] = config.Token{AccessToken: "test-tok", Username: "alice"}
	_ = config.SaveCredentials(creds)
}

func runCLI(args ...string) (string, string, error) {
	cmd := New()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	// cobra 把 RunE 的 err 也写到 stderr (但我们设了 SilenceErrors), 这里照搬
	return stdout.String(), stderr.String(), err
}

// ===== whoami =====

func TestCLI_Whoami(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			t.Errorf("missing auth header: %v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{"id": 9, "username": "alice", "email": "a@b.c"},
			"orgs": []int64{1, 2},
		})
	})
	presetCreds(t, srv.URL, 0)

	out, _, err := runCLI("whoami")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alice", "id=9", "[1 2]"} {
		if !strings.Contains(out, want) {
			t.Errorf("whoami output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// ===== templates list =====

func TestCLI_TemplatesList(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/pipeline-templates") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "slug": "node-docker-k8s", "name": "Node", "category": "fullstack", "tags": []string{"node"}, "builtin": true},
			{"id": 2, "slug": "go-bin", "name": "Go bin", "category": "release", "tags": []string{"go"}, "builtin": false},
		})
	})
	presetCreds(t, srv.URL, 0)

	out, _, err := runCLI("templates", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"node-docker-k8s", "go-bin", "release", "fullstack"} {
		if !strings.Contains(out, want) {
			t.Errorf("templates list missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestCLI_TemplatesList_JSON(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 7, "slug": "x", "name": "X"},
		})
	})
	presetCreds(t, srv.URL, 0)

	out, _, err := runCLI("templates", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"slug": "x"`) {
		t.Errorf("--json output should contain raw json, got:\n%s", out)
	}
}

// ===== templates clone =====

func TestCLI_TemplatesClone(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/from-template" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["template_slug"] != "node-docker-k8s" {
			t.Errorf("template_slug not forwarded: %v", body)
		}
		if v, _ := body["project_id"].(float64); int64(v) != 42 {
			t.Errorf("project_id not forwarded: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pipeline_id":   100,
			"version_id":    200,
			"version":       1,
			"pipeline_name": "ci",
			"template_slug": "node-docker-k8s",
		})
	})
	presetCreds(t, srv.URL, 0)

	out, _, err := runCLI("templates", "clone", "node-docker-k8s",
		"--project", "42", "--name", "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "id=100") || !strings.Contains(out, "v=1") {
		t.Errorf("clone output mismatch:\n%s", out)
	}
}

// ===== projects list =====

func TestCLI_ProjectsList(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "api", "slug": "api", "repo_url": "https://github.com/x/y", "repo_type": "github", "default_branch": "main"},
		})
	})
	presetCreds(t, srv.URL, 0)

	out, _, err := runCLI("projects", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "api") || !strings.Contains(out, "github.com/x/y") {
		t.Errorf("projects list output:\n%s", out)
	}
}

// ===== runs cancel =====

func TestCLI_RunsCancel(t *testing.T) {
	srv, _ := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/runs/55/cancel" || r.Method != "POST" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(204)
	})
	presetCreds(t, srv.URL, 0)
	out, _, err := runCLI("runs", "cancel", "55")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "55") {
		t.Errorf("cancel output:\n%s", out)
	}
}

// ===== 无 token 时的友好错误 =====

func TestCLI_NoTokenReturnsFriendlyError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HELIOS_HOME", dir)
	t.Setenv("HELIOS_PROFILE", "")
	_, _, err := runCLI("projects", "list")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "helios login") {
		t.Errorf("error message should hint login: %v", err)
	}
}

// ===== logout =====

func TestCLI_Logout(t *testing.T) {
	srv, dir := setupCLITest(t, func(w http.ResponseWriter, r *http.Request) {})
	presetCreds(t, srv.URL, 0)

	_, _, err := runCLI("logout")
	if err != nil {
		t.Fatal(err)
	}
	// token 应被清空
	b, _ := os.ReadFile(filepath.Join(dir, "credentials.yaml"))
	if strings.Contains(string(b), "test-tok") {
		t.Errorf("token not removed:\n%s", string(b))
	}
}
