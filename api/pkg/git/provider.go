// Package git 抽象不同代码托管平台 (GitHub/GitLab/...) 的通用能力。
//
// 设计原则:
//   - Provider 接口语义中立,不暴露平台特有字段
//   - 错误统一为 ErrNotFound / ErrUnauthorized / ErrServer
//   - 各平台的实现独立成文件 (github.go / gitlab.go ...)
package git

import "context"

// Repository 仓库基本信息 (跨平台通用)。
type Repository struct {
	Owner         string // 仓库所有者 (user/org)
	Name          string // 仓库名
	FullName      string // owner/name
	DefaultBranch string
	Private       bool
	CloneURL      string // HTTPS clone URL
	SSHURL        string // SSH clone URL
	HTMLURL       string // 浏览器访问 URL
}

// Branch 分支信息。
type Branch struct {
	Name      string
	CommitSHA string
	Protected bool
}

// Commit 提交摘要 (push 事件常用)。
type Commit struct {
	SHA       string
	Message   string
	AuthorEmail string
	AuthorName  string
	URL       string
}

// PushEvent 推送事件解析后的统一表达。
type PushEvent struct {
	Ref        string   // 完整 ref, 如 refs/heads/main
	Branch     string   // 短名, 已剥离 refs/heads/
	Tag        string   // tag 推送时填,非 tag 为空
	Before     string   // 推送前 commit SHA
	After      string   // 推送后 commit SHA (HEAD)
	Repository Repository
	Commits    []Commit
	PusherName  string
	PusherEmail string
}

// WebhookSpec 创建 webhook 的请求体。
type WebhookSpec struct {
	URL    string   // 接收回调的完整 URL
	Secret string   // HMAC-SHA256 共享密钥
	Events []string // 订阅事件,如 ["push", "pull_request"]
}

// WebhookInfo 创建后返回的 webhook 元信息。
type WebhookInfo struct {
	ID     int64
	URL    string
	Active bool
}

// Provider 代码托管平台抽象。
//
// 实现需要保证:
//   - 所有方法接受 context.Context 用于取消和超时
//   - 错误必须包装为 *ProviderError, 通过 Code 区分类型
//   - VerifyWebhookSignature 必须使用恒定时间比较 (hmac.Equal)
type Provider interface {
	// Name 平台名 (github / gitlab / ...)
	Name() string

	// GetRepo 拉取仓库元信息。owner/repo 大小写敏感。
	GetRepo(ctx context.Context, owner, repo string) (*Repository, error)

	// ListBranches 列出仓库所有分支。
	ListBranches(ctx context.Context, owner, repo string) ([]Branch, error)

	// GetFileContent 拉取指定 ref 上的文件内容 (UTF-8)。
	// ref 可以是分支名/tag/commit SHA;为空时使用默认分支。
	GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error)

	// CreateWebhook 在仓库上注册 webhook,返回平台分配的 ID。
	CreateWebhook(ctx context.Context, owner, repo string, spec WebhookSpec) (*WebhookInfo, error)

	// DeleteWebhook 删除 webhook (用于项目删除时清理)。
	DeleteWebhook(ctx context.Context, owner, repo string, hookID int64) error

	// VerifyWebhookSignature 校验 webhook 请求的签名是否合法。
	// 对 GitHub: header 名 X-Hub-Signature-256, value 形如 "sha256=<hex>"
	VerifyWebhookSignature(secret string, payload []byte, signature string) bool

	// ParsePushEvent 把 webhook 原始 body 解析为 PushEvent。
	// 不是 push 类型 event 时返回 ErrUnsupportedEvent。
	ParsePushEvent(eventType string, payload []byte) (*PushEvent, error)
}

// ErrorCode 错误分类。
type ErrorCode int

const (
	ErrUnknown ErrorCode = iota
	ErrNotFound
	ErrUnauthorized
	ErrRateLimited
	ErrServer
	ErrInvalidRequest
	ErrUnsupportedEvent
)

// ProviderError 平台调用统一错误。
type ProviderError struct {
	Code    ErrorCode
	Status  int    // HTTP 状态码 (无则 0)
	Message string // 平台返回的错误信息或本地描述
	Op      string // 出错的操作名,如 "GetRepo"
}

func (e *ProviderError) Error() string {
	if e.Op != "" {
		return "git: " + e.Op + ": " + e.Message
	}
	return "git: " + e.Message
}

// IsNotFound 判断错误是否是 not found (供上层方便分支)。
func IsNotFound(err error) bool {
	if pe, ok := err.(*ProviderError); ok {
		return pe.Code == ErrNotFound
	}
	return false
}

// IsUnauthorized 判断是否是 401/403。
func IsUnauthorized(err error) bool {
	if pe, ok := err.(*ProviderError); ok {
		return pe.Code == ErrUnauthorized
	}
	return false
}
