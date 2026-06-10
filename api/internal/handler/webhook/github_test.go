package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/git"
)

// ===== fake RunStore =====

type fakeStore struct {
	project     *model.Project
	getErr      error
	createErr   error
	gotProject  *model.Project
	gotEvent    *git.PushEvent
	nextRunID   int64
	nextRunNum  int
	createCount int
}

func (f *fakeStore) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.project == nil {
		return nil, ErrProjectNotFound
	}
	cp := *f.project
	cp.ID = id
	return &cp, nil
}

func (f *fakeStore) CreateRunForPush(ctx context.Context, p *model.Project, ev *git.PushEvent) (int64, int, error) {
	f.createCount++
	if f.createErr != nil {
		return 0, 0, f.createErr
	}
	f.gotProject = p
	f.gotEvent = ev
	return f.nextRunID, f.nextRunNum, nil
}

// ===== helpers =====

func init() { gin.SetMode(gin.TestMode) }

func buildRouter(h *GitHubHandler) *gin.Engine {
	r := gin.New()
	g := r.Group("/api/v1")
	h.Register(g)
	return r
}

func signGitHub(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func samplePushBody(branch string) []byte {
	body := map[string]any{
		"ref":    "refs/heads/" + branch,
		"before": "aaa",
		"after":  "bbb",
		"repository": map[string]any{
			"name":           "api",
			"full_name":      "acme/api",
			"default_branch": "main",
			"private":        false,
			"clone_url":      "https://github.com/acme/api.git",
			"ssh_url":        "git@github.com:acme/api.git",
			"html_url":       "https://github.com/acme/api",
			"owner":          map[string]string{"login": "acme"},
		},
		"commits": []map[string]any{
			{
				"id":      "bbb",
				"message": "feat: add x",
				"url":     "https://x",
				"author":  map[string]string{"name": "Jie", "email": "jie@example.com"},
			},
		},
		"pusher": map[string]string{"name": "Jie", "email": "jie@example.com"},
	}
	b, _ := json.Marshal(body)
	return b
}

func projectGitHub(defaultBranch, secret string) *model.Project {
	cfgBytes := []byte(`{}`)
	if secret != "" {
		cfgBytes = []byte(`{"webhook_secret":"` + secret + `"}`)
	}
	return &model.Project{
		OrgID:         1,
		Name:          "api",
		Slug:          "api",
		RepoURL:       "https://github.com/acme/api.git",
		RepoType:      "github",
		DefaultBranch: defaultBranch,
		Visibility:    "private",
		Config:        datatypes.JSON(cfgBytes),
	}
}

func doRequest(t *testing.T, r http.Handler, projectID, event, sig string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/v1/webhooks/github/" + projectID
	req := httptest.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ===== tests =====

// 1. happy path: 默认分支 push → 202 + 触发 createRun
func TestWebhook_HappyPath(t *testing.T) {
	secret := "topsecret"
	store := &fakeStore{
		project:    projectGitHub("main", secret),
		nextRunID:  42,
		nextRunNum: 7,
	}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "push", signGitHub(secret, body), body)

	require.Equal(t, http.StatusAccepted, rec.Code, "body=%s", rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(42), resp["run_id"])
	assert.Equal(t, float64(7), resp["run_number"])
	assert.Equal(t, "main", resp["branch"])
	assert.Equal(t, "bbb", resp["commit"])

	require.Equal(t, 1, store.createCount)
	require.NotNil(t, store.gotEvent)
	assert.Equal(t, "main", store.gotEvent.Branch)
	assert.Equal(t, "bbb", store.gotEvent.After)
}

// 2. 错签名 → 401
func TestWebhook_BadSignature(t *testing.T) {
	store := &fakeStore{project: projectGitHub("main", "topsecret")}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "push", "sha256=deadbeef", body)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, 0, store.createCount)
}

// 3. 缺签名 header → 401
func TestWebhook_MissingSignature(t *testing.T) {
	store := &fakeStore{project: projectGitHub("main", "topsecret")}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "1", "push", "", samplePushBody("main"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// 4. 缺 event header → 400
func TestWebhook_MissingEvent(t *testing.T) {
	store := &fakeStore{project: projectGitHub("main", "topsecret")}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "", signGitHub("topsecret", body), body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 5. ping → 200 (无需签名校验,因为还没读 body 之前就返回)
//
// 注意:GitHub 实际会对 ping 也带签名,但我们的实现是 ping 直接 short-circuit。
// 这符合 "尽快响应 ping" 的实际期望。
func TestWebhook_Ping(t *testing.T) {
	store := &fakeStore{project: projectGitHub("main", "topsecret")}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "1", "ping", "", []byte(`{}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "pong")
}

// 6. 非默认分支 push → 202 (filtered, 不建 run)
func TestWebhook_NonDefaultBranch(t *testing.T) {
	secret := "topsecret"
	store := &fakeStore{project: projectGitHub("main", secret)}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("feature/x")
	rec := doRequest(t, r, "1", "push", signGitHub(secret, body), body)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "branch filtered")
	assert.Equal(t, 0, store.createCount)
}

// 7. tag push → 202 (非分支 ref ignored)
func TestWebhook_TagRef(t *testing.T) {
	secret := "topsecret"
	store := &fakeStore{project: projectGitHub("main", secret)}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	tagBody := []byte(`{"ref":"refs/tags/v1","repository":{"name":"api","full_name":"acme/api","default_branch":"main","owner":{"login":"acme"}},"pusher":{}}`)
	rec := doRequest(t, r, "1", "push", signGitHub(secret, tagBody), tagBody)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "non-branch ref ignored")
	assert.Equal(t, 0, store.createCount)
}

// 8. 项目不存在 → 404
func TestWebhook_ProjectNotFound(t *testing.T) {
	store := &fakeStore{} // project nil
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "999", "push", "sha256=x", samplePushBody("main"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// 9. 非数字 project_id → 400
func TestWebhook_BadProjectID(t *testing.T) {
	store := &fakeStore{}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "abc", "push", "sha256=x", []byte("{}"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 10. project 没配 secret + dev_secret 也空 → 400 (secret not set)
func TestWebhook_NoSecretConfigured(t *testing.T) {
	store := &fakeStore{project: projectGitHub("main", "")} // config 无 secret
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "push", "sha256=anything", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no webhook secret")
}

// 11. dev_secret 兜底生效
func TestWebhook_DevSecretFallback(t *testing.T) {
	devSecret := "global-dev-secret"
	store := &fakeStore{
		project:    projectGitHub("main", ""), // 项目没配
		nextRunID:  1,
		nextRunNum: 1,
	}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), devSecret)
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "push", signGitHub(devSecret, body), body)
	assert.Equal(t, http.StatusAccepted, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, 1, store.createCount)
}

// 12. 非 push 事件 (e.g. pull_request) → 202 静默
func TestWebhook_NonPushEvent(t *testing.T) {
	secret := "topsecret"
	store := &fakeStore{project: projectGitHub("main", secret)}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := []byte(`{"action":"opened"}`)
	rec := doRequest(t, r, "1", "pull_request", signGitHub(secret, body), body)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "not processed")
	assert.Equal(t, 0, store.createCount)
}

// 13. repo_type 不是 github → 400
func TestWebhook_WrongRepoType(t *testing.T) {
	p := projectGitHub("main", "x")
	p.RepoType = "gitlab"
	store := &fakeStore{project: p}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "1", "push", "sha256=x", samplePushBody("main"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 14. store DB 错误 → 500
func TestWebhook_StoreDBError(t *testing.T) {
	store := &fakeStore{getErr: errors.New("connection refused")}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	rec := doRequest(t, r, "1", "push", "sha256=x", []byte("{}"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// 15. createRun 失败 → 500
func TestWebhook_CreateRunFails(t *testing.T) {
	secret := "topsecret"
	store := &fakeStore{
		project:   projectGitHub("main", secret),
		createErr: errors.New("disk full"),
	}
	h := NewGitHubHandler(store, git.NewGitHubProvider(git.GitHubConfig{}), "")
	r := buildRouter(h)

	body := samplePushBody("main")
	rec := doRequest(t, r, "1", "push", signGitHub(secret, body), body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "create run")
}
