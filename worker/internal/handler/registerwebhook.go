// Package handler 后台任务处理器。
//
// 本文件: helios:project:webhook-register
//
// 流程:
//  1. 解析 payload (project_id / owner / repo)
//  2. 从 DB 取 project (确保还存在)
//  3. 从 project.config 读 webhook_secret;若空则生成 32 字节随机
//  4. 调 GitProvider.CreateWebhook
//  5. 成功: 回写 config.webhook_id / webhook_secret / webhook_registered_at, 清 webhook_error
//  6. 失败: 写 config.webhook_error,return error → 触发 Asynq 重试
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/git"
	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// WebhookRegister 处理 helios:project:webhook-register。
type WebhookRegister struct {
	repo       *projectrepo.Repo
	provider   git.Provider
	publicURL  string // 外部可访问的 helios api 基址, 如 https://helios.example.com
	devSecret  string // 当 project.config.webhook_secret 缺失时的兜底 (开发环境)
}

// NewWebhookRegister 构造。publicURL 必填; devSecret 可空。
func NewWebhookRegister(repo *projectrepo.Repo, provider git.Provider, publicURL, devSecret string) *WebhookRegister {
	return &WebhookRegister{repo: repo, provider: provider, publicURL: publicURL, devSecret: devSecret}
}

// ProcessTask Asynq 入口。
func (h *WebhookRegister) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalWebhookRegister(t.Payload())
	if err != nil {
		// 非可恢复错误, 直接 SkipRetry
		log.Printf("[webhook-register] invalid payload: %v", err)
		return asynq.SkipRetry
	}

	proj, err := h.repo.Get(ctx, p.ProjectID)
	if err != nil {
		if errors.Is(err, projectrepo.ErrNotFound) {
			log.Printf("[webhook-register] project %d gone, skip", p.ProjectID)
			return asynq.SkipRetry
		}
		return fmt.Errorf("get project: %w", err)
	}

	if proj.RepoType != "github" {
		log.Printf("[webhook-register] project %d repo_type=%s != github, skip", p.ProjectID, proj.RepoType)
		_ = h.writeError(ctx, p.ProjectID, "repo_type is not github")
		return asynq.SkipRetry
	}

	// 1. secret 决策: 优先用 config 中已有的, 否则用 devSecret (dev), 否则生成新的
	secret := readSecret(proj.Config)
	if secret == "" {
		secret = h.devSecret
	}
	if secret == "" {
		s, err := randomHex(32)
		if err != nil {
			return fmt.Errorf("gen secret: %w", err)
		}
		secret = s
	}

	// 2. webhook URL
	publicURL := h.publicURL
	if publicURL == "" {
		publicURL = os.Getenv("HELIOS_PUBLIC_API_BASE")
	}
	if publicURL == "" {
		return fmt.Errorf("public api base URL not configured (set HELIOS_PUBLIC_API_BASE)")
	}
	hookURL := fmt.Sprintf("%s/api/v1/webhooks/github/%d", trimRightSlash(publicURL), p.ProjectID)

	// 3. 调 provider 创建 webhook
	info, err := h.provider.CreateWebhook(ctx, p.Owner, p.Repo, git.WebhookSpec{
		URL:    hookURL,
		Secret: secret,
		Events: []string{"push", "pull_request"},
	})
	if err != nil {
		// 把错误写到 config 让 UI 能看到, 但让 Asynq 继续重试
		_ = h.writeError(ctx, p.ProjectID, err.Error())
		// 4xx (auth/invalid) 不重试
		if git.IsUnauthorized(err) {
			log.Printf("[webhook-register] project %d unauthorized: %v, skip retry", p.ProjectID, err)
			return asynq.SkipRetry
		}
		return fmt.Errorf("create webhook: %w", err)
	}

	// 4. 成功 → 回写 config
	patch := map[string]any{
		"webhook_id":             info.ID,
		"webhook_url":            info.URL,
		"webhook_secret":         secret,
		"webhook_registered_at":  time.Now().UTC().Format(time.RFC3339),
		"webhook_provider":       "github",
		"webhook_active":         info.Active,
		"webhook_error":          "", // 清错
	}
	if err := h.repo.MergeConfig(ctx, p.ProjectID, patch); err != nil {
		return fmt.Errorf("write back config: %w", err)
	}
	log.Printf("[webhook-register] project %d ok (hook_id=%d url=%s)", p.ProjectID, info.ID, info.URL)
	return nil
}

// OnRetryExhausted Asynq retry 用尽时调用, 把最终错误写入 config。
func (h *WebhookRegister) OnRetryExhausted(ctx context.Context, t *asynq.Task, finalErr error) {
	p, err := tasks.UnmarshalWebhookRegister(t.Payload())
	if err != nil {
		return
	}
	_ = h.writeError(ctx, p.ProjectID, fmt.Sprintf("retries exhausted: %v", finalErr))
}

func (h *WebhookRegister) writeError(ctx context.Context, projectID int64, msg string) error {
	return h.repo.MergeConfig(ctx, projectID, map[string]any{
		"webhook_error":         msg,
		"webhook_error_at":      time.Now().UTC().Format(time.RFC3339),
	})
}

// ===== utils =====

func readSecret(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg["webhook_secret"].(string); ok {
		return v
	}
	return ""
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
