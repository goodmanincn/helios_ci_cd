// Package webhook 处理外部代码托管平台的 webhook 回调。
//
// 当前实现:
//   - GitHub push event → 落 runs 表 → 返回 202
//
// 安全:
//   - 强制 HMAC-SHA256 签名校验 (X-Hub-Signature-256)
//   - 签名失败立即 401, 不暴露任何业务信息
//   - 验签前先读 body, 用 io.LimitReader 防 OOM (max 10MB)
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/git"
)

// maxPayloadSize webhook body 最大字节数 (10MB), GitHub 实际不会超 25MB,我们截止 10MB 防滥用。
const maxPayloadSize = 10 * 1024 * 1024

// RunStore webhook 落库依赖 (抽接口便于单元测试)。
type RunStore interface {
	// GetProject 取项目;不存在返回 ErrProjectNotFound。
	GetProject(ctx context.Context, projectID int64) (*model.Project, error)
	// CreateRunForPush 把 push event 转成 run + (可能) 自动建 pipeline/version。
	// 返回 (run_id, run_number, error)。
	CreateRunForPush(ctx context.Context, project *model.Project, ev *git.PushEvent) (int64, int, error)
}

// ErrProjectNotFound store 层未找到。
var ErrProjectNotFound = errors.New("project not found")

// GitHubHandler 接收 GitHub webhook。
//
// 路由: POST /api/v1/webhooks/github/:project_id
type GitHubHandler struct {
	store    RunStore
	provider git.Provider
	// devSecret 当项目未配置 webhook_secret 时的兜底密钥(仅 dev/测试,生产应留空)。
	devSecret string
}

// NewGitHubHandler 构造。dev 兜底密钥通过 HELIOS_WEBHOOK_DEV_SECRET env 注入。
func NewGitHubHandler(store RunStore, provider git.Provider, devSecret string) *GitHubHandler {
	return &GitHubHandler{store: store, provider: provider, devSecret: devSecret}
}

// Register 在 router 上挂载路由 (不需要 auth 中间件)。
func (h *GitHubHandler) Register(r *gin.RouterGroup) {
	r.POST("/webhooks/github/:project_id", h.handle)
}

type webhookError struct {
	status int
	msg    string
}

func (e *webhookError) Error() string { return e.msg }

var (
	errBadProjectID     = &webhookError{status: http.StatusBadRequest, msg: "invalid project_id"}
	errMissingEvent     = &webhookError{status: http.StatusBadRequest, msg: "missing X-GitHub-Event header"}
	errMissingSignature = &webhookError{status: http.StatusUnauthorized, msg: "missing X-Hub-Signature-256 header"}
	errBadSignature     = &webhookError{status: http.StatusUnauthorized, msg: "invalid webhook signature"}
	errSecretNotSet     = &webhookError{status: http.StatusBadRequest, msg: "project has no webhook secret configured"}
	errReadBody         = &webhookError{status: http.StatusBadRequest, msg: "failed to read body"}
)

func (h *GitHubHandler) handle(c *gin.Context) {
	// 1. project_id 解析
	pidStr := c.Param("project_id")
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil || pid <= 0 {
		respondErr(c, errBadProjectID)
		return
	}

	// 2. 必备 header (event type)
	eventType := c.GetHeader("X-GitHub-Event")
	if eventType == "" {
		respondErr(c, errMissingEvent)
		return
	}
	// GitHub 注册 webhook 时会发一次 ping,直接 200
	if eventType == "ping" {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
		return
	}

	// 3. 读取项目 (校验存在 + repo_type)
	project, err := h.store.GetProject(c.Request.Context(), pid)
	if err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if project.RepoType != "github" {
		respondErr(c, &webhookError{status: http.StatusBadRequest, msg: "project repo_type is not github"})
		return
	}

	// 4. 读 body (限大小)
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxPayloadSize+1))
	if err != nil {
		respondErr(c, errReadBody)
		return
	}
	if len(body) > maxPayloadSize {
		respondErr(c, &webhookError{status: http.StatusRequestEntityTooLarge, msg: "payload too large"})
		return
	}

	// 5. 签名校验
	sig := c.GetHeader("X-Hub-Signature-256")
	if sig == "" {
		respondErr(c, errMissingSignature)
		return
	}
	secret := h.resolveSecret(project)
	if secret == "" {
		respondErr(c, errSecretNotSet)
		return
	}
	if !h.provider.VerifyWebhookSignature(secret, body, sig) {
		respondErr(c, errBadSignature)
		return
	}

	// 6. 解析 push event (其他事件先静默接受)
	if eventType != "push" {
		c.JSON(http.StatusAccepted, gin.H{"message": "event accepted but not processed", "event": eventType})
		return
	}

	ev, err := h.provider.ParsePushEvent(eventType, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse push event: " + err.Error()})
		return
	}

	// 7. 过滤: 必须是分支推送 (tag 暂不支持,M2 再加)
	if ev.Branch == "" {
		c.JSON(http.StatusAccepted, gin.H{"message": "non-branch ref ignored", "ref": ev.Ref})
		return
	}

	// 8. 过滤: 默认只接受默认分支 push (M2 加项目级 branches 白名单)
	if ev.Branch != project.DefaultBranch {
		c.JSON(http.StatusAccepted, gin.H{
			"message":        "branch filtered (not default branch)",
			"branch":         ev.Branch,
			"default_branch": project.DefaultBranch,
		})
		return
	}

	// 9. 创建 run 记录 (status=pending)
	runID, runNumber, err := h.store.CreateRunForPush(c.Request.Context(), project, ev)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create run: " + err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":    "run queued",
		"run_id":     runID,
		"run_number": runNumber,
		"branch":     ev.Branch,
		"commit":     ev.After,
	})
}

// resolveSecret 从项目 config.webhook_secret 取,否则用 dev 兜底。
//
// config 结构约定:
//
//	{"webhook_secret": "xxx"}
func (h *GitHubHandler) resolveSecret(p *model.Project) string {
	if len(p.Config) > 0 {
		var cfg struct {
			WebhookSecret string `json:"webhook_secret"`
		}
		if err := json.Unmarshal(p.Config, &cfg); err == nil && cfg.WebhookSecret != "" {
			return cfg.WebhookSecret
		}
	}
	return h.devSecret
}

func respondErr(c *gin.Context, e *webhookError) {
	c.JSON(e.status, gin.H{"error": e.msg})
}

// ===== GORM 实现 (生产路径) =====

// GormRunStore 用 GORM 实现 RunStore。
type GormRunStore struct {
	db *gorm.DB
}

func NewGormRunStore(db *gorm.DB) *GormRunStore { return &GormRunStore{db: db} }

func (s *GormRunStore) GetProject(ctx context.Context, projectID int64) (*model.Project, error) {
	var p model.Project
	err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateRunForPush 在事务里:
//  1. 拿/建 default pipeline
//  2. 拿/建 v1 pipeline_version (M1 阶段 spec 占位)
//  3. 算 run.number = max+1
//  4. 落 runs 记录 (status=pending)
func (s *GormRunStore) CreateRunForPush(ctx context.Context, p *model.Project, ev *git.PushEvent) (int64, int, error) {
	var runID int64
	var runNumber int

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 拿/建 pipeline
		var pipeline model.Pipeline
		err := tx.Where("project_id = ?", p.ID).Order("id ASC").First(&pipeline).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pipeline = model.Pipeline{
				ProjectID:   p.ID,
				Name:        "default",
				Description: "Auto-created on first webhook (M1)",
				Enabled:     true,
			}
			if err := tx.Create(&pipeline).Error; err != nil {
				return fmt.Errorf("create default pipeline: %w", err)
			}
		} else if err != nil {
			return err
		}

		// 2. 拿/建 pipeline_version
		var version model.PipelineVersion
		err = tx.Where("pipeline_id = ?", pipeline.ID).Order("version DESC").First(&version).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			version = model.PipelineVersion{
				PipelineID: pipeline.ID,
				Version:    1,
				Spec:       datatypes.JSON([]byte(`{"stages":[]}`)),
				SpecRaw:    "# placeholder (M1)\nstages: []\n",
				Message:    "auto-created on first webhook",
			}
			if err := tx.Create(&version).Error; err != nil {
				return fmt.Errorf("create default version: %w", err)
			}
			pipeline.CurrentVersionID = &version.ID
			if err := tx.Save(&pipeline).Error; err != nil {
				return fmt.Errorf("update pipeline.current_version: %w", err)
			}
		} else if err != nil {
			return err
		}

		// 3. 算 run.number
		var maxN int
		if err := tx.Model(&model.Run{}).
			Where("pipeline_id = ?", pipeline.ID).
			Select("COALESCE(MAX(number), 0)").
			Row().Scan(&maxN); err != nil {
			return fmt.Errorf("scan max run number: %w", err)
		}
		runNumber = maxN + 1

		// 4. trigger_data
		triggerData := map[string]any{
			"source":       "github_webhook",
			"pusher":       ev.PusherName,
			"pusher_email": ev.PusherEmail,
			"ref":          ev.Ref,
			"before":       ev.Before,
			"commit_count": len(ev.Commits),
		}
		tdBytes, _ := json.Marshal(triggerData)

		msg := ""
		if len(ev.Commits) > 0 {
			msg = ev.Commits[len(ev.Commits)-1].Message
		}

		run := model.Run{
			PipelineID:  pipeline.ID,
			VersionID:   version.ID,
			Number:      runNumber,
			Status:      "pending",
			TriggerType: "push",
			TriggerData: datatypes.JSON(tdBytes),
			CommitSHA:   ev.After,
			Branch:      ev.Branch,
			Message:     msg,
			CreatedAt:   time.Now(),
		}
		if err := tx.Create(&run).Error; err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		runID = run.ID
		return nil
	})

	return runID, runNumber, err
}
