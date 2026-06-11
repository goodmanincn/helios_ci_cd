// Package handler — pipeline_templates.go: 流水线模板市场 + 克隆 API (M8 T8.2.1)。
//
// 端点:
//
//	GET    /api/v1/pipeline-templates              列表 (?category= ?tag= ?q=)
//	POST   /api/v1/pipeline-templates              创建私有模板 (org_id 自动取 active)
//	GET    /api/v1/pipeline-templates/:id          获取详情
//	PUT    /api/v1/pipeline-templates/:id          更新 (非 builtin)
//	DELETE /api/v1/pipeline-templates/:id          删除 (非 builtin)
//	POST   /api/v1/pipelines/from-template         克隆模板到目标 project
//	                                                {template_id|template_slug, project_id, name, description?}
//
// 设计要点:
//   - 列表合并 全局 (org_id IS NULL) + 当前 org 私有
//   - 跨 org 私有模板返 404 (避免存在性泄露)
//   - builtin 模板不可改不可删
//   - 克隆: 校验 spec → 在事务里 INSERT pipelines + pipeline_versions(v=1) + 更新 current_version_id
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/pkg/dsl"
)

type PipelineTemplateHandler struct {
	db    *gorm.DB
	store repository.PipelineTemplateStore
}

func NewPipelineTemplateHandler(db *gorm.DB) *PipelineTemplateHandler {
	return &PipelineTemplateHandler{
		db:    db,
		store: repository.NewPipelineTemplateRepository(db),
	}
}

func (h *PipelineTemplateHandler) Register(g *gin.RouterGroup) {
	g.GET("/pipeline-templates", h.list)
	g.POST("/pipeline-templates", h.create)
	g.GET("/pipeline-templates/:id", h.get)
	g.PUT("/pipeline-templates/:id", h.update)
	g.DELETE("/pipeline-templates/:id", h.delete)
	g.POST("/pipelines/from-template", h.cloneToPipeline)
}

// ===== handlers =====

func (h *PipelineTemplateHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	list, err := h.store.List(orgID, repository.TemplateFilter{
		Category: c.Query("category"),
		Tag:      c.Query("tag"),
		Q:        c.Query("q"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if list == nil {
		list = []model.PipelineTemplate{}
	}
	c.JSON(http.StatusOK, list)
}

type createTemplateReq struct {
	Slug        string   `json:"slug" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	SpecRaw     string   `json:"spec_raw" binding:"required"`
}

func (h *PipelineTemplateHandler) create(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	var req createTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 校验 spec 必须合法 (否则克隆出来必跑不通)
	pipeline, errs := dsl.ValidateRaw([]byte(req.SpecRaw))
	if len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, validateResp{Valid: false, Errors: toErrItems(errs)})
		return
	}

	uid := userIDFrom(c)
	var createdBy *int64
	if uid > 0 {
		createdBy = &uid
	}

	t := &model.PipelineTemplate{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Tags:        req.Tags,
		Spec:        datatypes.JSON(mustJSON(pipeline)),
		SpecRaw:     req.SpecRaw,
		Builtin:     false,
		OrgID:       &orgID,
		CreatedBy:   createdBy,
	}
	if err := h.store.Create(t); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "slug already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create failed"})
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *PipelineTemplateHandler) get(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	t, err := h.loadVisible(c.Param("id"), orgID)
	if err != nil {
		respondTemplateErr(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

type updateTemplateReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	SpecRaw     string   `json:"spec_raw"`
}

func (h *PipelineTemplateHandler) update(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	t, err := h.loadVisible(c.Param("id"), orgID)
	if err != nil {
		respondTemplateErr(c, err)
		return
	}
	if t.Builtin {
		c.JSON(http.StatusForbidden, gin.H{"error": repository.ErrPipelineTemplateBuiltin.Error()})
		return
	}
	// 私有模板不能跨 org 改 (loadVisible 已过滤, 这里再保险)
	if t.OrgID == nil || *t.OrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot modify foreign template"})
		return
	}

	var req updateTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != "" {
		t.Name = req.Name
	}
	// 空串描述也允许清空, 用指针更准确但保持简单
	if req.Description != "" {
		t.Description = req.Description
	}
	if req.Category != "" {
		t.Category = req.Category
	}
	if req.Tags != nil {
		t.Tags = req.Tags
	}
	if req.SpecRaw != "" {
		pipeline, errs := dsl.ValidateRaw([]byte(req.SpecRaw))
		if len(errs) > 0 {
			c.JSON(http.StatusUnprocessableEntity, validateResp{Valid: false, Errors: toErrItems(errs)})
			return
		}
		t.Spec = datatypes.JSON(mustJSON(pipeline))
		t.SpecRaw = req.SpecRaw
	}
	if err := h.store.Update(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *PipelineTemplateHandler) delete(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	t, err := h.loadVisible(c.Param("id"), orgID)
	if err != nil {
		respondTemplateErr(c, err)
		return
	}
	if t.Builtin {
		c.JSON(http.StatusForbidden, gin.H{"error": repository.ErrPipelineTemplateBuiltin.Error()})
		return
	}
	if t.OrgID == nil || *t.OrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete foreign template"})
		return
	}
	if err := h.store.Delete(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ===== POST /pipelines/from-template =====

type cloneTemplateReq struct {
	TemplateID   int64  `json:"template_id"`
	TemplateSlug string `json:"template_slug"`
	ProjectID    int64  `json:"project_id" binding:"required"`
	Name         string `json:"name" binding:"required"` // 新 pipeline 名
	Description  string `json:"description"`
}

type cloneTemplateResp struct {
	PipelineID    int64  `json:"pipeline_id"`
	VersionID     int64  `json:"version_id"`
	Version       int    `json:"version"`
	PipelineName  string `json:"pipeline_name"`
	TemplateSlug  string `json:"template_slug"`
}

func (h *PipelineTemplateHandler) cloneToPipeline(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	var req cloneTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TemplateID == 0 && req.TemplateSlug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template_id or template_slug required"})
		return
	}

	// 1) 解析模板
	var (
		tmpl *model.PipelineTemplate
		err  error
	)
	if req.TemplateSlug != "" {
		tmpl, err = h.store.GetBySlug(req.TemplateSlug)
	} else {
		tmpl, err = h.store.Get(req.TemplateID)
	}
	if err != nil {
		respondTemplateErr(c, err)
		return
	}
	if !tmpl.VisibleToOrg(orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	// 2) 校验目标 project 属于当前 org
	var proj model.Project
	if err := h.db.First(&proj, req.ProjectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project lookup failed"})
		return
	}
	if proj.OrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "project not in active org"})
		return
	}

	// 3) 二次校验模板 spec (防止 seed 后 DSL 升级导致老模板失效)
	parsed, errs := dsl.ValidateRaw([]byte(tmpl.SpecRaw))
	if len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":  "template spec no longer valid",
			"errors": toErrItems(errs),
		})
		return
	}

	uid := userIDFrom(c)
	var createdBy *int64
	if uid > 0 {
		createdBy = &uid
	}

	// 4) 事务: 创建 pipeline + version + 更新 current_version_id
	var resp cloneTemplateResp
	err = h.db.Transaction(func(tx *gorm.DB) error {
		p := model.Pipeline{
			ProjectID:   proj.ID,
			Name:        req.Name,
			Description: req.Description,
			Enabled:     true,
			CreatedBy:   createdBy,
		}
		if err := tx.Create(&p).Error; err != nil {
			return err
		}

		specJSON := map[string]any{}
		if parsed != nil {
			b, _ := json.Marshal(parsed)
			_ = json.Unmarshal(b, &specJSON)
		}
		pv := model.PipelineVersion{
			PipelineID: p.ID,
			Version:    1,
			Spec:       datatypes.JSON(mustJSON(specJSON)),
			SpecRaw:    tmpl.SpecRaw,
			Message:    "cloned from template " + tmpl.Slug,
			CreatedBy:  createdBy,
		}
		if err := tx.Create(&pv).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Pipeline{}).Where("id = ?", p.ID).
			Update("current_version_id", pv.ID).Error; err != nil {
			return err
		}
		resp = cloneTemplateResp{
			PipelineID:   p.ID,
			VersionID:    pv.ID,
			Version:      pv.Version,
			PipelineName: p.Name,
			TemplateSlug: tmpl.Slug,
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "clone failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// ===== helpers =====

func (h *PipelineTemplateHandler) loadVisible(idStr string, orgID int64) (*model.PipelineTemplate, error) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, errBadTemplateID
	}
	t, err := h.store.Get(id)
	if err != nil {
		return nil, err
	}
	if !t.VisibleToOrg(orgID) {
		return nil, repository.ErrPipelineTemplateNotFound
	}
	return t, nil
}

var errBadTemplateID = errors.New("invalid template id")

func respondTemplateErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errBadTemplateID):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
	case errors.Is(err, repository.ErrPipelineTemplateNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
