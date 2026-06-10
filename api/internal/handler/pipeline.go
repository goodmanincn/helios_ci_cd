// Package handler — pipeline.go: pipeline DSL 校验/编辑 API (T2.1.4 / M2 起点)。
//
// 当前端点:
//   - POST /api/v1/pipelines/validate — 只校验不入库, 编辑器实时调用
//
// 后续 (M2 增量) 会加: CRUD pipeline version, 触发 ad-hoc run 等。
// 这一刻刻意把 PipelineHandler 独立, 不与 RunHandler / ProjectHandler 混。
package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// PipelineHandler 流水线 DSL/version API。
type PipelineHandler struct{}

func NewPipelineHandler() *PipelineHandler { return &PipelineHandler{} }

// Register 挂到受保护 /api/v1。
func (h *PipelineHandler) Register(g *gin.RouterGroup) {
	g.POST("/pipelines/validate", h.validate)
}

// ===== /pipelines/validate =====

type validateReq struct {
	// spec_raw 优先 (与 spec/04 + M1 service 字段命名一致); 兼容 spec 字段
	SpecRaw string `json:"spec_raw"`
	Spec    string `json:"spec"`
}

type validateResp struct {
	Valid    bool                  `json:"valid"`
	Errors   []validateErrItem     `json:"errors"`
	Warnings []validateErrItem     `json:"warnings"`
	Pipeline *dsl.Pipeline         `json:"pipeline,omitempty"`
	Summary  *validatePipelineSum  `json:"summary,omitempty"`
}

type validateErrItem struct {
	Kind    string `json:"kind"`    // syntax / schema / semantic
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

type validatePipelineSum struct {
	Name       string `json:"name,omitempty"`
	Version    string `json:"version,omitempty"`
	StageCount int    `json:"stage_count"`
	StageIDs   []string `json:"stage_ids,omitempty"`
}

// validate POST /pipelines/validate
//
// 容错:
//   - 空 body 返 400 (没意义调用)
//   - YAML 哪怕全错也返 200 + valid=false (前端拿错误列表显示, 而不是依赖 4xx)
//   - 大小限制: body > 256KB 拒掉 (避免 DoS, 编辑器场景一般 < 64KB)
func (h *PipelineHandler) validate(c *gin.Context) {
	// 限制 body 大小
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)

	// 同时支持 JSON 和 text/yaml
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
		// 直接吃 body 内容当 YAML, 方便 CLI / curl
		b, err := io.ReadAll(c.Request.Body)
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
		Warnings: []validateErrItem{}, // M2 后续加 warning 级别 (e.g. 未使用变量)
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
