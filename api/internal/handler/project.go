// Package handler ...
package handler

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/internal/service"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// ProjectHandler 项目 REST 端点。
type ProjectHandler struct {
	svc *service.ProjectService
	enq queue.Enqueuer // 可空 (旧测试路径), 生产路径必传
}

func NewProjectHandler(svc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{svc: svc}
}

// NewProjectHandlerWithQueue 携带 enqueuer 的构造, 用于触发 webhook 自动注册等异步任务。
func NewProjectHandlerWithQueue(svc *service.ProjectService, enq queue.Enqueuer) *ProjectHandler {
	return &ProjectHandler{svc: svc, enq: enq}
}

// Register 在 /api/v1/projects 下挂载所有路由(调用方负责加 RequireAuth)。
func (h *ProjectHandler) Register(g *gin.RouterGroup) {
	g.GET("/projects", h.list)
	g.POST("/projects", h.create)
	g.GET("/projects/:id", h.get)
	g.PATCH("/projects/:id", h.update)
	g.DELETE("/projects/:id", h.del)
	g.POST("/projects/:id/sync", h.sync)
}

// activeOrg 从 claims 选当前生效的 org:优先 X-Org-ID 头,其次 claims.OrgIDs[0]。
// 同时校验头里指定的 org 必须在用户的 orgs 列表里。
func activeOrg(c *gin.Context) (int64, bool) {
	cl, ok := middleware.ClaimsFrom(c)
	if !ok || len(cl.OrgIDs) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "user has no organization"})
		return 0, false
	}
	if v := strings.TrimSpace(c.GetHeader("X-Org-ID")); v != "" {
		want, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid X-Org-ID header"})
			return 0, false
		}
		for _, id := range cl.OrgIDs {
			if id == want {
				return want, true
			}
		}
		c.JSON(http.StatusForbidden, gin.H{"error": "user not in requested org"})
		return 0, false
	}
	return cl.OrgIDs[0], true
}

func userIDFrom(c *gin.Context) int64 {
	cl, ok := middleware.ClaimsFrom(c)
	if !ok {
		return 0
	}
	id, _ := strconv.ParseInt(cl.Subject, 10, 64)
	return id
}

func parseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// 把 service 业务错误映射到 HTTP。
func projectErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrProjectNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrProjectSlugTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidSlug),
		errors.Is(err, service.ErrInvalidRepoURL),
		errors.Is(err, service.ErrUnsupportedRepoType),
		errors.Is(err, service.ErrInvalidVisibility),
		errors.Is(err, service.ErrProjectNameRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// === 端点 ===

func (h *ProjectHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, total, err := h.svc.List(c.Request.Context(), repository.ListProjectsFilter{
		OrgID:  orgID,
		Query:  strings.TrimSpace(c.Query("q")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

type createProjectReq struct {
	Name          string `json:"name"           binding:"required"`
	Slug          string `json:"slug"           binding:"required"`
	Description   string `json:"description"`
	RepoURL       string `json:"repo_url"       binding:"required"`
	RepoType      string `json:"repo_type"      binding:"required"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
}

func (h *ProjectHandler) create(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	var req createProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	p, err := h.svc.Create(c.Request.Context(), service.CreateProjectInput{
		OrgID:         orgID,
		CreatedBy:     userIDFrom(c),
		Name:          req.Name,
		Slug:          req.Slug,
		Description:   req.Description,
		RepoURL:       req.RepoURL,
		RepoType:      req.RepoType,
		DefaultBranch: req.DefaultBranch,
		Visibility:    req.Visibility,
	})
	if err != nil {
		projectErr(c, err)
		return
	}

	// 创建成功后异步注册 webhook (仅 github, 失败不影响创建结果)
	h.enqueueWebhookRegister(c, p)

	c.JSON(http.StatusCreated, p)
}

// enqueueWebhookRegister 在项目创建/更新后投递 webhook 注册任务。
// 失败仅打日志, 不影响主流程; 重试与最终失败状态由 worker 写回 project.config。
func (h *ProjectHandler) enqueueWebhookRegister(c *gin.Context, p *model.Project) {
	if h.enq == nil {
		return
	}
	if p.RepoType != "github" {
		return // 当前只支持 github 自动注册
	}
	owner, repo, ok := parseOwnerRepoFromURL(p.RepoURL)
	if !ok {
		log.Printf("[project] cannot parse owner/repo from %q, skip webhook register", p.RepoURL)
		return
	}
	taskID, err := h.enq.EnqueueWebhookRegister(c.Request.Context(), &tasks.WebhookRegisterPayload{
		ProjectID: p.ID,
		RepoURL:   p.RepoURL,
		Owner:     owner,
		Repo:      repo,
	})
	if err != nil {
		log.Printf("[project] enqueue webhook register failed: project_id=%d err=%v", p.ID, err)
		return
	}
	log.Printf("[project] webhook register enqueued: project_id=%d owner=%s repo=%s task_id=%s",
		p.ID, owner, repo, taskID)
}

// parseOwnerRepoFromURL 从仓库 URL 解析 owner / repo。支持:
//   - https://github.com/octocat/Hello-World[.git]
//   - git@github.com:octocat/Hello-World.git
//   - ssh://git@github.com/octocat/Hello-World.git
//   - 本地路径 (返回 false, 让上层跳过)
func parseOwnerRepoFromURL(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	// scp-like: git@host:owner/repo.git
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") {
		idx := strings.Index(raw, ":")
		path := strings.TrimPrefix(raw[idx+1:], "/")
		return splitOwnerRepo(path)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ssh" {
		return "", "", false
	}
	return splitOwnerRepo(strings.TrimPrefix(u.Path, "/"))
}

func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (h *ProjectHandler) get(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	p, err := h.svc.GetByID(c.Request.Context(), orgID, id)
	if err != nil {
		projectErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

type updateProjectReq struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	DefaultBranch *string `json:"default_branch"`
	Visibility    *string `json:"visibility"`
}

func (h *ProjectHandler) update(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req updateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	p, err := h.svc.Update(c.Request.Context(), service.UpdateProjectInput{
		OrgID:         orgID,
		ID:            id,
		Name:          req.Name,
		Description:   req.Description,
		DefaultBranch: req.DefaultBranch,
		Visibility:    req.Visibility,
	})
	if err != nil {
		projectErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *ProjectHandler) del(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), orgID, id); err != nil {
		projectErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// sync 触发 Git 同步 — M1.2 才接入,先返回 202 占位。
func (h *ProjectHandler) sync(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetByID(c.Request.Context(), orgID, id); err != nil {
		projectErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message":    "sync queued (not implemented until M1.2)",
		"project_id": id,
	})
}
