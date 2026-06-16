// Package handler — pipeline.go: pipeline DSL 校验/编辑/版本 API (M2/M3)。
//
// 端点:
//   - GET  /api/v1/pipelines — 列表 (?project_id=)
//   - GET  /api/v1/pipelines/:id — 详情 (含 current spec_raw)
//   - POST /api/v1/pipelines/validate — 只校验不入库
//   - PUT  /api/v1/pipelines/:id/spec — 保存 spec,创建新版本
//   - GET  /api/v1/pipelines/:id/versions — 版本历史列表
//   - POST /api/v1/pipelines/:id/versions/:v/restore — 回滚到指定版本
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// PipelineHandler 流水线 DSL/version API。
type PipelineHandler struct {
	db  *gorm.DB
	enq queue.Enqueuer // 可选, trigger 时必传
}

func NewPipelineHandler(db *gorm.DB) *PipelineHandler { return &PipelineHandler{db: db} }

// WithQueue 注入任务队列 (用于 trigger). 链式返回.
func (h *PipelineHandler) WithQueue(enq queue.Enqueuer) *PipelineHandler {
	h.enq = enq
	return h
}

// WithEnqueuer alias of WithQueue, 兼容已有测试.
func (h *PipelineHandler) WithEnqueuer(enq queue.Enqueuer) *PipelineHandler {
	return h.WithQueue(enq)
}

// Register 挂到受保护 /api/v1。
func (h *PipelineHandler) Register(g *gin.RouterGroup) {
	g.GET("/pipelines", h.list)
	g.GET("/pipelines/:id", h.get)
	g.POST("/pipelines/validate", h.validate)
	g.PUT("/pipelines/:id/spec", h.updateSpec)
	g.GET("/pipelines/:id/versions", h.listVersions)
	g.POST("/pipelines/:id/versions/:v/restore", h.restoreVersion)
	g.POST("/pipelines/:id/trigger", h.trigger)
}

// ===== GET /pipelines =====

type pipelineView struct {
	ID               int64  `json:"id"`
	ProjectID        int64  `json:"project_id"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	CurrentVersionID *int64 `json:"current_version_id,omitempty"`
	Enabled          bool   `json:"enabled"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	SpecRaw          string `json:"spec_raw,omitempty"`
	Version          int    `json:"version,omitempty"`
}

func (h *PipelineHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	q := h.db.WithContext(c.Request.Context()).
		Model(&model.Pipeline{}).
		Joins("JOIN projects ON projects.id = pipelines.project_id").
		Where("projects.org_id = ?", orgID).
		Where("pipelines.deleted_at IS NULL")

	if pidStr := c.Query("project_id"); pidStr != "" {
		pid, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
		q = q.Where("pipelines.project_id = ?", pid)
	}

	var pipelines []model.Pipeline
	if err := q.Order("pipelines.id DESC").Limit(200).Find(&pipelines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	out := make([]pipelineView, 0, len(pipelines))
	for _, p := range pipelines {
		out = append(out, pipelineView{
			ID:               p.ID,
			ProjectID:        p.ProjectID,
			Name:             p.Name,
			Description:      p.Description,
			CurrentVersionID: p.CurrentVersionID,
			Enabled:          p.Enabled,
			CreatedAt:        p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			UpdatedAt:        p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, out)
}

// ===== GET /pipelines/:id =====

func (h *PipelineHandler) get(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline id"})
		return
	}

	var p model.Pipeline
	err = h.db.WithContext(c.Request.Context()).
		Joins("JOIN projects ON projects.id = pipelines.project_id").
		Where("projects.org_id = ?", orgID).
		Where("pipelines.id = ?", id).
		First(&p).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	view := pipelineView{
		ID:               p.ID,
		ProjectID:        p.ProjectID,
		Name:             p.Name,
		Description:      p.Description,
		CurrentVersionID: p.CurrentVersionID,
		Enabled:          p.Enabled,
		CreatedAt:        p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if p.CurrentVersionID != nil {
		var pv model.PipelineVersion
		if err := h.db.First(&pv, *p.CurrentVersionID).Error; err == nil {
			view.SpecRaw = pv.SpecRaw
			view.Version = pv.Version
		}
	}

	c.JSON(http.StatusOK, view)
}

// ===== /pipelines/validate =====

type validateReq struct {
	SpecRaw string `json:"spec_raw"`
	Spec    string `json:"spec"`
}

type validateResp struct {
	Valid    bool                 `json:"valid"`
	Errors   []validateErrItem    `json:"errors"`
	Warnings []validateErrItem    `json:"warnings"`
	Pipeline *dsl.Pipeline        `json:"pipeline,omitempty"`
	Summary  *validatePipelineSum `json:"summary,omitempty"`
}

type validateErrItem struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

type validatePipelineSum struct {
	Name       string   `json:"name,omitempty"`
	Version    string   `json:"version,omitempty"`
	StageCount int      `json:"stage_count"`
	StageIDs   []string `json:"stage_ids,omitempty"`
}

func (h *PipelineHandler) validate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	contentType := c.GetHeader("Content-Type")
	var raw string
	switch {
	case contentType == "application/json" || contentType == "":
		var req validateReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json: " + err.Error()})
			return
		}
		raw = req.SpecRaw
		if raw == "" {
			raw = req.Spec
		}
	case contentType == "text/yaml" || contentType == "application/yaml" || contentType == "text/plain":
		b, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
			return
		}
		raw = string(b)
	default:
		c.JSON(http.StatusUnsupportedMediaType,
			gin.H{"error": "expected application/json or text/yaml"})
		return
	}
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "spec_raw (or text body) is required"})
		return
	}

	pipeline, errs := dsl.ValidateRaw([]byte(raw))
	resp := validateResp{
		Valid:    len(errs) == 0,
		Errors:   toErrItems(errs),
		Warnings: []validateErrItem{},
	}
	if pipeline != nil {
		resp.Pipeline = pipeline
		ids := make([]string, 0, len(pipeline.Stages))
		for _, s := range pipeline.Stages {
			ids = append(ids, s.ID)
		}
		resp.Summary = &validatePipelineSum{
			Name:       pipeline.Name,
			Version:    pipeline.Version,
			StageCount: len(pipeline.Stages),
			StageIDs:   ids,
		}
	}
	c.JSON(http.StatusOK, resp)
}

func toErrItems(es dsl.Errors) []validateErrItem {
	out := make([]validateErrItem, 0, len(es))
	for _, e := range es {
		out = append(out, validateErrItem{
			Kind:    string(e.Kind),
			Message: e.Message,
			Path:    e.Path,
			Line:    e.Line,
			Column:  e.Column,
		})
	}
	return out
}

// ===== PUT /pipelines/:id/spec =====

type updateSpecReq struct {
	SpecRaw        string `json:"spec_raw"`
	Message        string `json:"message"`
	BaseVersionID  *int64 `json:"base_version_id,omitempty"`
}

func (h *PipelineHandler) updateSpec(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline id"})
		return
	}
	var req updateSpecReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json: " + err.Error()})
		return
	}
	if req.SpecRaw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "spec_raw is required"})
		return
	}

	// 1. 校验
	pipeline, errs := dsl.ValidateRaw([]byte(req.SpecRaw))
	if len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, validateResp{
			Valid:  false,
			Errors: toErrItems(errs),
		})
		return
	}

	// 2. 事务: 检查 pipeline 存在 → 冲突检测 → 创建 version → 更新 current_version_id
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var p model.Pipeline
		if err := tx.First(&p, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return err // 404
			}
			return err
		}

		// 冲突保护 (T3.6.4)
		if req.BaseVersionID != nil && p.CurrentVersionID != nil && *req.BaseVersionID != *p.CurrentVersionID {
			c.JSON(http.StatusConflict, gin.H{
				"error":            "version conflict",
				"current_version_id": p.CurrentVersionID,
			})
			return gorm.ErrInvalidTransaction // 触发 rollback,但 gin 已写响应
		}

		// 计算下一个 version 号
		var maxVer int
		tx.Model(&model.PipelineVersion{}).Select("COALESCE(MAX(version),0)").Where("pipeline_id = ?", id).Scan(&maxVer)

		specJSON := map[string]any{}
		if pipeline != nil {
			b, _ := json.Marshal(pipeline)
			_ = json.Unmarshal(b, &specJSON)
		}

		pv := model.PipelineVersion{
			PipelineID: id,
			Version:    maxVer + 1,
			Spec:       datatypes.JSON(mustJSON(specJSON)),
			SpecRaw:    req.SpecRaw,
			Message:    req.Message,
		}
		if err := tx.Create(&pv).Error; err != nil {
			return err
		}

		// 更新 pipeline current_version_id
		if err := tx.Model(&model.Pipeline{}).Where("id = ?", id).Update("current_version_id", pv.ID).Error; err != nil {
			return err
		}

		c.JSON(http.StatusOK, pv)
		return nil
	})

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
			return
		}
		if err == gorm.ErrInvalidTransaction {
			return // 409 已在上面的 tx 里写过了
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save failed: " + err.Error()})
	}
}

// ===== GET /pipelines/:id/versions =====

func (h *PipelineHandler) listVersions(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline id"})
		return
	}
	var versions []model.PipelineVersion
	if err := h.db.Where("pipeline_id = ?", id).Order("version DESC").Find(&versions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, versions)
}

// ===== POST /pipelines/:id/versions/:v/restore =====

func (h *PipelineHandler) restoreVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline id"})
		return
	}
	ver, err := strconv.Atoi(c.Param("v"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version number"})
		return
	}

	var old model.PipelineVersion
	if err := h.db.Where("pipeline_id = ? AND version = ?", id, ver).First(&old).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "version not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	// 创建新版本,内容 = 旧 spec
	var maxVer int
	h.db.Model(&model.PipelineVersion{}).Select("COALESCE(MAX(version),0)").Where("pipeline_id = ?", id).Scan(&maxVer)

	pv := model.PipelineVersion{
		PipelineID: id,
		Version:    maxVer + 1,
		Spec:       old.Spec,
		SpecRaw:    old.SpecRaw,
		Message:    "Restore from v" + strconv.Itoa(ver),
	}
	if err := h.db.Create(&pv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create version failed"})
		return
	}
	if err := h.db.Model(&model.Pipeline{}).Where("id = ?", id).Update("current_version_id", pv.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update pipeline failed"})
		return
	}
	c.JSON(http.StatusOK, pv)
}

// ---- helpers ----

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ===== POST /pipelines/:id/trigger =====
//
// 手动触发一次试运行. 在事务里:
//   1. 查 pipeline + current_version
//   2. 创建 run (trigger_type=manual, status=pending)
//   3. enqueue helios:run:orchestrate → worker 调度 stage
//
// 返 run 摘要, 前端拿到 id 后跳 /runs/:id.
func (h *PipelineHandler) trigger(c *gin.Context) {
	if h.enq == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task queue not configured"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var result struct {
		RunID      int64  `json:"run_id"`
		Number     int    `json:"number"`
		PipelineID int64  `json:"pipeline_id"`
		TaskID     string `json:"task_id"`
	}

	orgID, _ := activeOrg(c)

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var pipeline model.Pipeline
		if err := tx.Joins("JOIN projects ON projects.id = pipelines.project_id").
			Where("pipelines.id = ? AND projects.org_id = ?", id, orgID).
			First(&pipeline).Error; err != nil {
			return fmt.Errorf("pipeline not found")
		}
		if pipeline.CurrentVersionID == nil {
			return fmt.Errorf("pipeline has no saved version — save first")
		}
		var version model.PipelineVersion
		if err := tx.First(&version, *pipeline.CurrentVersionID).Error; err != nil {
			return fmt.Errorf("version not found")
		}

		var maxN int
		if err := tx.Model(&model.Run{}).
			Where("pipeline_id = ?", pipeline.ID).
			Select("COALESCE(MAX(number), 0)").
			Row().Scan(&maxN); err != nil {
			return fmt.Errorf("scan max number: %w", err)
		}

		uid := userIDFrom(c)
		triggerData, _ := json.Marshal(map[string]any{
			"source":  "manual",
			"user_id": uid,
		})

		run := model.Run{
			PipelineID:  pipeline.ID,
			VersionID:   version.ID,
			Number:      maxN + 1,
			Status:      "pending",
			TriggerType: "manual",
			TriggerData: datatypes.JSON(triggerData),
			Branch:      "",
			Message:     "manual trigger",
			CreatedAt:   time.Now(),
		}
		if err := tx.Create(&run).Error; err != nil {
			return fmt.Errorf("create run: %w", err)
		}

		taskID, err := h.enq.EnqueueRunOrchestrate(c.Request.Context(), &tasks.RunOrchestratePayload{
			RunID:     run.ID,
			ProjectID: pipeline.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("enqueue orchestrate: %w", err)
		}

		result.RunID = run.ID
		result.Number = run.Number
		result.PipelineID = pipeline.ID
		result.TaskID = taskID
		return nil
	})

	if err != nil {
		switch {
		case err.Error() == "pipeline not found":
			c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
		case err.Error() == "pipeline has no saved version — save first":
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusCreated, result)
}
