package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/internal/service"
)

// ProjectHandler 项目 REST 端点。
type ProjectHandler struct {
	svc *service.ProjectService
}

func NewProjectHandler(svc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{svc: svc}
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
	c.JSON(http.StatusCreated, p)
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
