package git

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: 起 mock GitHub,把 handler 绑到 baseURL 上,返回一个配置好 baseURL 的 Provider。
func newMockGitHub(t *testing.T, h http.Handler) (*GitHubProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := NewGitHubProvider(GitHubConfig{
		BaseURL: srv.URL,
		Token:   "ghp_test_token",
		Timeout: 5 * time.Second,
	})
	return p, srv
}

// 1) GetRepo: happy path + 404 + 401
func TestGitHub_GetRepo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/api", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "Bearer ghp_test_token", r.Header.Get("Authorization"))
		assert.Contains(t, r.Header.Get("Accept"), "github+json")
		_, _ = w.Write([]byte(`{
			"name": "api",
			"full_name": "acme/api",
			"default_branch": "main",
			"private": true,
			"clone_url": "https://github.com/acme/api.git",
			"ssh_url": "git@github.com:acme/api.git",
			"html_url": "https://github.com/acme/api",
			"owner": {"login": "acme"}
		}`))
	})
	mux.HandleFunc("/repos/acme/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	mux.HandleFunc("/repos/acme/secret", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	})

	p, _ := newMockGitHub(t, mux)
	ctx := context.Background()

	repo, err := p.GetRepo(ctx, "acme", "api")
	require.NoError(t, err)
	assert.Equal(t, "acme", repo.Owner)
	assert.Equal(t, "api", repo.Name)
	assert.Equal(t, "acme/api", repo.FullName)
	assert.Equal(t, "main", repo.DefaultBranch)
	assert.True(t, repo.Private)

	_, err = p.GetRepo(ctx, "acme", "missing")
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "expected NotFound, got %v", err)

	_, err = p.GetRepo(ctx, "acme", "secret")
	require.Error(t, err)
	assert.True(t, IsUnauthorized(err))
}

// 2) ListBranches
func TestGitHub_ListBranches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/api/branches", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		_, _ = w.Write([]byte(`[
			{"name":"main","commit":{"sha":"abc123"},"protected":true},
			{"name":"dev","commit":{"sha":"def456"},"protected":false}
		]`))
	})
	p, _ := newMockGitHub(t, mux)
	branches, err := p.ListBranches(context.Background(), "acme", "api")
	require.NoError(t, err)
	require.Len(t, branches, 2)
	assert.Equal(t, "main", branches[0].Name)
	assert.Equal(t, "abc123", branches[0].CommitSHA)
	assert.True(t, branches[0].Protected)
	assert.Equal(t, "dev", branches[1].Name)
	assert.False(t, branches[1].Protected)
}

// 3) GetFileContent (含 base64 解码)
func TestGitHub_GetFileContent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/api/contents/Makefile", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "develop", r.URL.Query().Get("ref"))
		_, _ = w.Write([]byte(`{
			"type": "file",
			"encoding": "base64",
			"content": "aGVsbG8g\nd29ybGQ=\n"
		}`))
	})
	mux.HandleFunc("/repos/acme/api/contents/README.md", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"dir"}`))
	})
	p, _ := newMockGitHub(t, mux)

	data, err := p.GetFileContent(context.Background(), "acme", "api", "Makefile", "develop")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	// 不是 file 时报错
	_, err = p.GetFileContent(context.Background(), "acme", "api", "README.md", "")
	require.Error(t, err)
}

// 4) CreateWebhook
func TestGitHub_CreateWebhook(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/api/hooks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "web", got["name"])
		assert.Equal(t, true, got["active"])

		cfg := got["config"].(map[string]any)
		assert.Equal(t, "https://helios.example.com/webhooks/github/42", cfg["url"])
		assert.Equal(t, "supersecret", cfg["secret"])
		assert.Equal(t, "json", cfg["content_type"])

		events := got["events"].([]any)
		assert.Contains(t, events, "push")

		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id": 99887766, "url": "https://api.github.com/repos/acme/api/hooks/99887766", "active": true}`))
	})

	p, _ := newMockGitHub(t, mux)
	info, err := p.CreateWebhook(context.Background(), "acme", "api", WebhookSpec{
		URL:    "https://helios.example.com/webhooks/github/42",
		Secret: "supersecret",
		Events: []string{"push"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(99887766), info.ID)
	assert.True(t, info.Active)
}

// 5) CreateWebhook 无 token 拒绝
func TestGitHub_CreateWebhook_NoToken(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{BaseURL: "http://nowhere", Token: ""})
	_, err := p.CreateWebhook(context.Background(), "a", "b", WebhookSpec{})
	require.Error(t, err)
	assert.True(t, IsUnauthorized(err))
}

// 6) DeleteWebhook
func TestGitHub_DeleteWebhook(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/api/hooks/12345", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(204)
	})
	p, _ := newMockGitHub(t, mux)
	err := p.DeleteWebhook(context.Background(), "acme", "api", 12345)
	require.NoError(t, err)
}

// 7) VerifyWebhookSignature: 正确签名 + 错签名 + 错前缀 + 空
func TestGitHub_VerifyWebhookSignature(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{})
	secret := "topsecret"
	payload := []byte(`{"hello":"world"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	assert.True(t, p.VerifyWebhookSignature(secret, payload, good))
	assert.False(t, p.VerifyWebhookSignature(secret, payload, "sha256=deadbeef"))
	assert.False(t, p.VerifyWebhookSignature(secret, payload, strings.TrimPrefix(good, "sha256=")), "missing prefix should fail")
	assert.False(t, p.VerifyWebhookSignature("", payload, good), "empty secret should fail")
	assert.False(t, p.VerifyWebhookSignature(secret, payload, ""), "empty signature should fail")
	assert.False(t, p.VerifyWebhookSignature(secret, []byte("tampered"), good))
}

// 8) ParsePushEvent (分支 push)
func TestGitHub_ParsePushEvent_Branch(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{})
	body := []byte(`{
		"ref": "refs/heads/main",
		"before": "111",
		"after": "222",
		"repository": {
			"name": "api",
			"full_name": "acme/api",
			"default_branch": "main",
			"private": false,
			"clone_url": "https://github.com/acme/api.git",
			"ssh_url": "git@github.com:acme/api.git",
			"html_url": "https://github.com/acme/api",
			"owner": {"login": "acme", "name": "acme"}
		},
		"commits": [
			{"id":"222","message":"feat: x","url":"https://x","author":{"name":"Jie","email":"jie@example.com"}}
		],
		"pusher": {"name":"Jie","email":"jie@example.com"}
	}`)
	ev, err := p.ParsePushEvent("push", body)
	require.NoError(t, err)
	assert.Equal(t, "main", ev.Branch)
	assert.Empty(t, ev.Tag)
	assert.Equal(t, "222", ev.After)
	assert.Equal(t, "acme", ev.Repository.Owner)
	assert.Equal(t, "acme/api", ev.Repository.FullName)
	require.Len(t, ev.Commits, 1)
	assert.Equal(t, "feat: x", ev.Commits[0].Message)
	assert.Equal(t, "Jie", ev.PusherName)
}

// 9) ParsePushEvent (tag push) + 非 push 类型
func TestGitHub_ParsePushEvent_Tag(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{})
	body := []byte(`{"ref":"refs/tags/v1.0","repository":{"name":"api","full_name":"acme/api","owner":{"login":"acme"}},"pusher":{}}`)
	ev, err := p.ParsePushEvent("push", body)
	require.NoError(t, err)
	assert.Empty(t, ev.Branch)
	assert.Equal(t, "v1.0", ev.Tag)

	// 非 push event
	_, err = p.ParsePushEvent("pull_request", body)
	require.Error(t, err)
	pe, ok := err.(*ProviderError)
	require.True(t, ok)
	assert.Equal(t, ErrUnsupportedEvent, pe.Code)
}

// 10) ParsePushEvent 缺 ref
func TestGitHub_ParsePushEvent_MissingRef(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{})
	_, err := p.ParsePushEvent("push", []byte(`{"repository":{}}`))
	require.Error(t, err)
}

// 11) ProviderError.Error() 格式 & Name
func TestGitHub_Name(t *testing.T) {
	p := NewGitHubProvider(GitHubConfig{})
	assert.Equal(t, "github", p.Name())
	e := &ProviderError{Code: ErrNotFound, Op: "GetRepo", Message: "boom"}
	assert.Contains(t, e.Error(), "GetRepo")
	assert.Contains(t, e.Error(), "boom")
}
