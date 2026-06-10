package git

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubProvider 实现 Provider 接口,基于 GitHub REST v3 API。
//
// 鉴权: Personal Access Token (Bearer) 或 fine-grained token。
//   - 公共仓库的 GetRepo/ListBranches/GetFileContent 可不带 token (有速率限制)
//   - 创建 webhook 必须带 token (admin:repo_hook 或 repo)
//
// 速率限制: 默认 5000 req/h (带 token) / 60 req/h (匿名)。
type GitHubProvider struct {
	baseURL string // 默认 https://api.github.com,测试时可指向 httptest
	token   string // PAT,空则匿名
	hc      *http.Client
}

// GitHubConfig GitHub Provider 配置。
type GitHubConfig struct {
	BaseURL string        // 留空走默认
	Token   string        // 可选
	Timeout time.Duration // 留空 = 30s
}

// NewGitHubProvider 创建 GitHub Provider。
func NewGitHubProvider(cfg GitHubConfig) *GitHubProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &GitHubProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		hc:      &http.Client{Timeout: cfg.Timeout},
	}
}

// Name 返回平台名。
func (g *GitHubProvider) Name() string { return "github" }

// ===== HTTP 请求基础设施 =====

func (g *GitHubProvider) do(ctx context.Context, op, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, &ProviderError{Code: ErrInvalidRequest, Op: op, Message: "marshal body: " + err.Error()}
		}
		reader = bytes.NewReader(buf)
	}

	url := g.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, &ProviderError{Code: ErrUnknown, Op: op, Message: "new request: " + err.Error()}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, 0, &ProviderError{Code: ErrServer, Op: op, Message: "do: " + err.Error()}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024)) // 8MB cap

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, resp.StatusCode, nil
	}

	code := classifyStatus(resp.StatusCode)
	return nil, resp.StatusCode, &ProviderError{
		Code:    code,
		Status:  resp.StatusCode,
		Op:      op,
		Message: fmt.Sprintf("http %d: %s", resp.StatusCode, snippet(data, 200)),
	}
}

func classifyStatus(s int) ErrorCode {
	switch {
	case s == 401:
		return ErrUnauthorized
	case s == 403:
		return ErrUnauthorized // GitHub 速率限制也是 403,统一归这里
	case s == 404:
		return ErrNotFound
	case s == 429:
		return ErrRateLimited
	case s >= 500:
		return ErrServer
	case s >= 400:
		return ErrInvalidRequest
	}
	return ErrUnknown
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ===== Provider 实现 =====

// GetRepo GET /repos/{owner}/{repo}
func (g *GitHubProvider) GetRepo(ctx context.Context, owner, repo string) (*Repository, error) {
	data, _, err := g.do(ctx, "GetRepo", http.MethodGet, "/repos/"+owner+"/"+repo, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		CloneURL      string `json:"clone_url"`
		SSHURL        string `json:"ssh_url"`
		HTMLURL       string `json:"html_url"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &ProviderError{Code: ErrServer, Op: "GetRepo", Message: "decode: " + err.Error()}
	}
	return &Repository{
		Owner:         raw.Owner.Login,
		Name:          raw.Name,
		FullName:      raw.FullName,
		DefaultBranch: raw.DefaultBranch,
		Private:       raw.Private,
		CloneURL:      raw.CloneURL,
		SSHURL:        raw.SSHURL,
		HTMLURL:       raw.HTMLURL,
	}, nil
}

// ListBranches GET /repos/{owner}/{repo}/branches
// 注意: 只拉第一页 (最多 100),足够 MVP。
func (g *GitHubProvider) ListBranches(ctx context.Context, owner, repo string) ([]Branch, error) {
	data, _, err := g.do(ctx, "ListBranches", http.MethodGet, "/repos/"+owner+"/"+repo+"/branches?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Protected bool `json:"protected"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &ProviderError{Code: ErrServer, Op: "ListBranches", Message: "decode: " + err.Error()}
	}
	out := make([]Branch, 0, len(raw))
	for _, b := range raw {
		out = append(out, Branch{Name: b.Name, CommitSHA: b.Commit.SHA, Protected: b.Protected})
	}
	return out, nil
}

// GetFileContent GET /repos/{owner}/{repo}/contents/{path}?ref=...
func (g *GitHubProvider) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	url := "/repos/" + owner + "/" + repo + "/contents/" + strings.TrimLeft(path, "/")
	if ref != "" {
		url += "?ref=" + ref
	}
	data, _, err := g.do(ctx, "GetFileContent", http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &ProviderError{Code: ErrServer, Op: "GetFileContent", Message: "decode: " + err.Error()}
	}
	if raw.Type != "file" {
		return nil, &ProviderError{Code: ErrInvalidRequest, Op: "GetFileContent", Message: "path is not a file: type=" + raw.Type}
	}
	if raw.Encoding != "base64" {
		return nil, &ProviderError{Code: ErrServer, Op: "GetFileContent", Message: "unsupported encoding: " + raw.Encoding}
	}
	// GitHub 返回的 content 会换行,base64 解码需要先去掉
	cleaned := strings.ReplaceAll(raw.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, &ProviderError{Code: ErrServer, Op: "GetFileContent", Message: "base64 decode: " + err.Error()}
	}
	return decoded, nil
}

// CreateWebhook POST /repos/{owner}/{repo}/hooks
func (g *GitHubProvider) CreateWebhook(ctx context.Context, owner, repo string, spec WebhookSpec) (*WebhookInfo, error) {
	if g.token == "" {
		return nil, &ProviderError{Code: ErrUnauthorized, Op: "CreateWebhook", Message: "token required to create webhook"}
	}
	events := spec.Events
	if len(events) == 0 {
		events = []string{"push"}
	}
	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": events,
		"config": map[string]string{
			"url":          spec.URL,
			"content_type": "json",
			"secret":       spec.Secret,
			"insecure_ssl": "0",
		},
	}
	data, _, err := g.do(ctx, "CreateWebhook", http.MethodPost, "/repos/"+owner+"/"+repo+"/hooks", body)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID     int64  `json:"id"`
		URL    string `json:"url"`
		Active bool   `json:"active"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &ProviderError{Code: ErrServer, Op: "CreateWebhook", Message: "decode: " + err.Error()}
	}
	return &WebhookInfo{ID: raw.ID, URL: raw.URL, Active: raw.Active}, nil
}

// DeleteWebhook DELETE /repos/{owner}/{repo}/hooks/{hook_id}
func (g *GitHubProvider) DeleteWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	if g.token == "" {
		return &ProviderError{Code: ErrUnauthorized, Op: "DeleteWebhook", Message: "token required"}
	}
	_, _, err := g.do(ctx, "DeleteWebhook", http.MethodDelete, fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, hookID), nil)
	return err
}

// VerifyWebhookSignature 校验 X-Hub-Signature-256 header。
// signature 形如 "sha256=<hex>"。
func (g *GitHubProvider) VerifyWebhookSignature(secret string, payload []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	got, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// ParsePushEvent 解析 push 事件 body。
// eventType 取自 header X-GitHub-Event。
func (g *GitHubProvider) ParsePushEvent(eventType string, payload []byte) (*PushEvent, error) {
	if eventType != "push" {
		return nil, &ProviderError{Code: ErrUnsupportedEvent, Op: "ParsePushEvent", Message: "unsupported event: " + eventType}
	}
	var raw struct {
		Ref     string `json:"ref"`
		Before  string `json:"before"`
		After   string `json:"after"`
		Created bool   `json:"created"`
		Deleted bool   `json:"deleted"`
		Repository struct {
			Name          string `json:"name"`
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
			CloneURL      string `json:"clone_url"`
			SSHURL        string `json:"ssh_url"`
			HTMLURL       string `json:"html_url"`
			Owner         struct {
				Name  string `json:"name"`
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		Commits []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
			URL     string `json:"url"`
			Author  struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"author"`
		} `json:"commits"`
		Pusher struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"pusher"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, &ProviderError{Code: ErrInvalidRequest, Op: "ParsePushEvent", Message: "decode: " + err.Error()}
	}
	if raw.Ref == "" {
		return nil, &ProviderError{Code: ErrInvalidRequest, Op: "ParsePushEvent", Message: "missing ref"}
	}
	ev := &PushEvent{
		Ref:    raw.Ref,
		Before: raw.Before,
		After:  raw.After,
		Repository: Repository{
			Owner:         coalesce(raw.Repository.Owner.Login, raw.Repository.Owner.Name),
			Name:          raw.Repository.Name,
			FullName:      raw.Repository.FullName,
			DefaultBranch: raw.Repository.DefaultBranch,
			Private:       raw.Repository.Private,
			CloneURL:      raw.Repository.CloneURL,
			SSHURL:        raw.Repository.SSHURL,
			HTMLURL:       raw.Repository.HTMLURL,
		},
		PusherName:  raw.Pusher.Name,
		PusherEmail: raw.Pusher.Email,
	}

	switch {
	case strings.HasPrefix(raw.Ref, "refs/heads/"):
		ev.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	case strings.HasPrefix(raw.Ref, "refs/tags/"):
		ev.Tag = strings.TrimPrefix(raw.Ref, "refs/tags/")
	}

	for _, c := range raw.Commits {
		ev.Commits = append(ev.Commits, Commit{
			SHA:         c.ID,
			Message:     c.Message,
			AuthorEmail: c.Author.Email,
			AuthorName:  c.Author.Name,
			URL:         c.URL,
		})
	}
	return ev, nil
}

func coalesce(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// 编译期保证 GitHubProvider 实现 Provider 接口。
var _ Provider = (*GitHubProvider)(nil)

// ErrNoPayload 解析空 payload 时的便捷错误 (测试用)。
var ErrNoPayload = errors.New("git: empty payload")
