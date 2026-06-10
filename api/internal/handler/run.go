// Package handler — run 详情/列表 REST (T1.6.1) + cancel/retry (T1.6.4)。
//
// 设计:
//   - 用 GORM 走读 (内部 model.Run/Stage/Step), 不走 runrepo (那是 worker 写仓储)
//   - 详情一次性返回 run + project 摘要 + stages[].steps[] 嵌套树, 避免前端多次往返
//   - 列表轻量, 不含日志大小估算; 支持 ?project_id= / ?branch= / ?status= / ?limit=
//   - cancel: 调 runstate.Machine.MarkCanceled, 状态机自身保证幂等 + 非法转换返错
//     真正 docker kill 留给 worker 端的 ctx 取消传播 (M1 dev 简化, M2 加 control-plane signal)
//   - retry: 复刻 webhook CreateRunForPush 的事务套路, 新建 run + 入队 git_checkout
//     新 run 沿用原 pipeline/version/branch/commit, number=pipeline 内 max+1, trigger_type=retry
//   - 通过 RequireAuth, 当前未做 org 级过滤 (M1 简化: 任何登录用户可读所有 run, 配合内网部署)
package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/service"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// Enqueuer 只取 RunHandler 需要的子集 (避免拉整个 queue.Enqueuer)。
// api/cmd/api main.go 装配真实 AsynqEnqueuer 时它已自动满足。
type Enqueuer interface {
	EnqueueGitCheckout(ctx context.Context, p *tasks.GitCheckoutPayload) (taskID string, err error)
}

// RunHandler runs 资源端点。
type RunHandler struct {
	db       *gorm.DB
	machine  *runstate.Machine // 可空 → cancel 返 503
	enq      Enqueuer          // 可空 → retry 返 503
	approval ApprovalLister    // 可空 → detail 不带 approval 字段
}

// NewRunHandler 构造。machine/enq 都可空 (T1.6.1 阶段只读取时调用方传 nil)。
func NewRunHandler(db *gorm.DB) *RunHandler {
	return &RunHandler{db: db}
}

// WithRunControl 注入 cancel/retry 所需依赖, 返回自身以支持链式。
func (h *RunHandler) WithRunControl(m *runstate.Machine, enq Enqueuer) *RunHandler {
	h.machine = m
	h.enq = enq
	return h
}

// WithApproval 注入审批 lister, 让 GET /runs/:id 详情携带 approval_requests.
func (h *RunHandler) WithApproval(svc ApprovalLister) *RunHandler {
	h.approval = svc
	return h
}

// Register 挂到 /api/v1, 调用方加 RequireAuth。
func (h *RunHandler) Register(g *gin.RouterGroup) {
	g.GET("/runs", h.list)
	g.GET("/runs/:id", h.detail)
	g.POST("/runs/:id/cancel", h.cancel)
	g.POST("/runs/:id/retry", h.retry)
}

// ===== DTO (前端使用, 字段命名 snake_case) =====

type stepDTO struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name,omitempty"`
	Index      *int       `json:"index,omitempty"`
	Status     string     `json:"status,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	LogObject  string     `json:"log_object,omitempty"`
	LogSize    int64      `json:"log_size,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
}

type stageDTO struct {
	ID         int64      `json:"id"`
	StageID    string     `json:"stage_id"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
	Steps      []stepDTO  `json:"steps"`
}

type projectSummaryDTO struct {
	ID       int64  `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	RepoURL  string `json:"repo_url,omitempty"`
	RepoType string `json:"repo_type,omitempty"`
}

type runListItemDTO struct {
	ID          int64              `json:"id"`
	Number      int                `json:"number"`
	Status      string             `json:"status"`
	Branch      string             `json:"branch,omitempty"`
	CommitSHA   string             `json:"commit_sha,omitempty"`
	Message     string             `json:"message,omitempty"`
	TriggerType string             `json:"trigger_type,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	FinishedAt  *time.Time         `json:"finished_at,omitempty"`
	DurationMs  int64              `json:"duration_ms,omitempty"`
	Project     *projectSummaryDTO `json:"project,omitempty"`
}

type runDetailDTO struct {
	runListItemDTO
	PipelineID       int64                      `json:"pipeline_id"`
	VersionID        int64                      `json:"version_id"`
	Stages           []stageDTO                 `json:"stages"`
	ApprovalRequests []service.ApprovalSummary  `json:"approval_requests,omitempty"`
}

// ===== list =====

// GET /api/v1/runs?project_id=&pipeline_id=&branch=&status=&limit=&before_id=
// 默认 limit=20, 最大 100; 按 id desc 排序;before_id 用于游标翻页 (id < before_id)。
func (h *RunHandler) list(c *gin.Context) {
	q := h.db.WithContext(c.Request.Context()).Model(&model.Run{})

	// project_id → JOIN pipelines 过滤
	if v := c.Query("project_id"); v != "" {
		pid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
		q = q.Where("pipeline_id IN (?)",
			h.db.Model(&model.Pipeline{}).Select("id").Where("project_id = ?", pid))
	}
	if v := c.Query("pipeline_id"); v != "" {
		pid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline_id"})
			return
		}
		q = q.Where("pipeline_id = ?", pid)
	}
	if v := strings.TrimSpace(c.Query("branch")); v != "" {
		q = q.Where("branch = ?", v)
	}
	if v := strings.TrimSpace(c.Query("status")); v != "" {
		q = q.Where("status = ?", v)
	}
	if v := c.Query("before_id"); v != "" {
		bid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid before_id"})
			return
		}
		q = q.Where("id < ?", bid)
	}

	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			limit = n
		}
	}

	var runs []model.Run
	if err := q.Order("id DESC").Limit(limit).Find(&runs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 一次查这批 run 关联的 project 摘要 (pipelines → projects)
	projSum := h.batchLoadProjects(c, runs)

	items := make([]runListItemDTO, 0, len(runs))
	for _, r := range runs {
		items = append(items, toRunListItem(r, projSum[r.PipelineID]))
	}
	var nextCursor *int64
	if len(items) == limit {
		v := items[len(items)-1].ID
		nextCursor = &v
	}
	c.JSON(http.StatusOK, gin.H{
		"items":   items,
		"limit":   limit,
		"next_id": nextCursor,
	})
}

// ===== detail =====

// GET /api/v1/runs/:id
// 返回 run + project 摘要 + stages[].steps[] (按 stage.id asc + step.step_index asc 排)
func (h *RunHandler) detail(c *gin.Context) {
	rid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || rid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	ctx := c.Request.Context()

	var run model.Run
	if err := h.db.WithContext(ctx).First(&run, rid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// project 摘要 (pipeline → project)
	projSum := h.batchLoadProjects(c, []model.Run{run})

	// stages + steps
	var stages []model.Stage
	if err := h.db.WithContext(ctx).Where("run_id = ?", rid).Order("id ASC").Find(&stages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load stages: " + err.Error()})
		return
	}
	stageIDs := make([]int64, 0, len(stages))
	for _, s := range stages {
		stageIDs = append(stageIDs, s.ID)
	}
	stepsByStage := make(map[int64][]model.Step, len(stageIDs))
	if len(stageIDs) > 0 {
		var steps []model.Step
		if err := h.db.WithContext(ctx).
			Where("stage_record_id IN ?", stageIDs).
			Order("step_index ASC, id ASC").
			Find(&steps).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "load steps: " + err.Error()})
			return
		}
		for _, st := range steps {
			stepsByStage[st.StageRecordID] = append(stepsByStage[st.StageRecordID], st)
		}
	}

	stageDTOs := make([]stageDTO, 0, len(stages))
	for _, s := range stages {
		stageDTOs = append(stageDTOs, toStageDTO(s, stepsByStage[s.ID]))
	}

	resp := runDetailDTO{
		runListItemDTO: toRunListItem(run, projSum[run.PipelineID]),
		PipelineID:     run.PipelineID,
		VersionID:      run.VersionID,
		Stages:         stageDTOs,
	}
	// approval_requests 内嵌 (T2.6.2). lister 空或查错都不阻塞详情返回.
	if h.approval != nil {
		if list, aerr := h.approval.ListByRun(ctx, rid); aerr != nil {
			log.Printf("[run] load approval requests run=%d err=%v", rid, aerr)
		} else if len(list) > 0 {
			resp.ApprovalRequests = list
		}
	}
	c.JSON(http.StatusOK, resp)
}

// ===== cancel (T1.6.4) =====

// POST /api/v1/runs/:id/cancel
// 调 runstate.MarkCanceled, 状态机已保证幂等 + 非法转换返错。
// 真正 docker kill 由 worker 端按 ctx 取消传播 (M1 简化, M2 加 control signal)。
func (h *RunHandler) cancel(c *gin.Context) {
	rid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || rid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	if h.machine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "run control not configured"})
		return
	}
	ctx := c.Request.Context()

	// 先确认 run 存在 + 拿当前状态返回给前端 (兼容幂等场景)
	var run model.Run
	if err := h.db.WithContext(ctx).Select("id", "status").First(&run, rid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 鉴权后 actor 取 uid 拼到 reason 让 audit_logs 看得到
	actor := "user"
	if v, ok := c.Get(middleware.CtxUserIDKey); ok {
		actor = fmt.Sprintf("uid=%v", v)
	}

	if err := h.machine.MarkCanceled(ctx, rid, runstate.TransitionOpts{
		Reason: "canceled by " + actor,
	}); err != nil {
		// 终态 run 重复取消 → 返 409 让 UI 友好提示
		if errors.Is(err, runstate.ErrTerminal) {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "run already in terminal state",
				"status": run.Status,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     rid,
		"status": "canceled",
	})
}

// ===== retry (T1.6.4) =====

// POST /api/v1/runs/:id/retry
// 在事务里新建 run (复用原 pipeline/version/branch/commit, number=max+1, trigger_type=retry),
// 然后入队 git_checkout。原 run 不动。
func (h *RunHandler) retry(c *gin.Context) {
	rid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || rid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	if h.enq == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "enqueuer not configured"})
		return
	}
	ctx := c.Request.Context()

	// 1. 读原 run
	var src model.Run
	if err := h.db.WithContext(ctx).First(&src, rid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 只允许 retry 已经停下来的 run (避免重复触发)
	if src.Status == "pending" || src.Status == "running" {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "run still in flight; cancel first",
			"status": src.Status,
		})
		return
	}

	// 2. 查 pipeline 拿 project_id (后续算 number + checkout 入队都要)
	var pipeline model.Pipeline
	if err := h.db.WithContext(ctx).First(&pipeline, src.PipelineID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load pipeline: " + err.Error()})
		return
	}
	var project model.Project
	if err := h.db.WithContext(ctx).First(&project, pipeline.ProjectID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load project: " + err.Error()})
		return
	}

	// 3. 事务: max number + 1 → 落新 run
	var newRunID int64
	var newRunNumber int
	tdJSON := datatypes.JSON([]byte(fmt.Sprintf(`{"source":"retry","origin_run_id":%d}`, src.ID)))
	err = h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxN int
		if err := tx.Model(&model.Run{}).
			Where("pipeline_id = ?", pipeline.ID).
			Select("COALESCE(MAX(number), 0)").
			Row().Scan(&maxN); err != nil {
			return fmt.Errorf("max run number: %w", err)
		}
		newRunNumber = maxN + 1
		nr := model.Run{
			PipelineID:  pipeline.ID,
			VersionID:   src.VersionID,
			Number:      newRunNumber,
			Status:      "pending",
			TriggerType: "retry",
			TriggerData: tdJSON,
			CommitSHA:   src.CommitSHA,
			Branch:      src.Branch,
			Message:     src.Message,
			CreatedAt:   time.Now(),
		}
		if err := tx.Create(&nr).Error; err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		newRunID = nr.ID
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 入队 (失败不回滚 run, 落审计后让用户在 UI 看错误, 与 webhook 一致)
	taskID, eqErr := h.enq.EnqueueGitCheckout(ctx, &tasks.GitCheckoutPayload{
		RunID:     newRunID,
		ProjectID: project.ID,
		RepoURL:   project.RepoURL,
		Branch:    src.Branch,
		CommitSHA: src.CommitSHA,
	})
	if eqErr != nil {
		log.Printf("enqueue git_checkout for retry failed: run_id=%d origin=%d err=%v",
			newRunID, src.ID, eqErr)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"id":            newRunID,
		"number":        newRunNumber,
		"status":        "pending",
		"origin_run_id": src.ID,
		"task_id":       taskID,
	})
}

// ===== helpers =====

// batchLoadProjects 给 runs 批量查关联的 project 摘要, 返回 map[pipeline_id]*projectSummaryDTO。
// 用 pipelines 表中转 (run.pipeline_id → pipelines.project_id → projects)。
func (h *RunHandler) batchLoadProjects(c *gin.Context, runs []model.Run) map[int64]*projectSummaryDTO {
	out := map[int64]*projectSummaryDTO{}
	if len(runs) == 0 {
		return out
	}
	pipelineIDs := make([]int64, 0, len(runs))
	seen := map[int64]bool{}
	for _, r := range runs {
		if !seen[r.PipelineID] {
			seen[r.PipelineID] = true
			pipelineIDs = append(pipelineIDs, r.PipelineID)
		}
	}
	type row struct {
		PipelineID int64  `gorm:"column:pipeline_id"`
		ID         int64  `gorm:"column:id"`
		Slug       string `gorm:"column:slug"`
		Name       string `gorm:"column:name"`
		RepoURL    string `gorm:"column:repo_url"`
		RepoType   string `gorm:"column:repo_type"`
	}
	var rows []row
	if err := h.db.WithContext(c.Request.Context()).
		Table("pipelines AS p").
		Select("p.id AS pipeline_id, pr.id, pr.slug, pr.name, pr.repo_url, pr.repo_type").
		Joins("JOIN projects pr ON pr.id = p.project_id").
		Where("p.id IN ?", pipelineIDs).
		Find(&rows).Error; err != nil {
		// 失败不阻塞详情/列表, 只是 project 字段缺
		return out
	}
	for _, r := range rows {
		out[r.PipelineID] = &projectSummaryDTO{
			ID: r.ID, Slug: r.Slug, Name: r.Name,
			RepoURL: r.RepoURL, RepoType: r.RepoType,
		}
	}
	return out
}

func toRunListItem(r model.Run, proj *projectSummaryDTO) runListItemDTO {
	d := runListItemDTO{
		ID:          r.ID,
		Number:      r.Number,
		Status:      r.Status,
		Branch:      r.Branch,
		CommitSHA:   r.CommitSHA,
		Message:     r.Message,
		TriggerType: r.TriggerType,
		CreatedAt:   r.CreatedAt,
		StartedAt:   r.StartedAt,
		FinishedAt:  r.FinishedAt,
		Project:     proj,
	}
	d.DurationMs = computeDuration(r.StartedAt, r.FinishedAt, int64(r.DurationMs))
	return d
}

func toStageDTO(s model.Stage, steps []model.Step) stageDTO {
	out := stageDTO{
		ID:         s.ID,
		StageID:    s.StageID,
		Name:       s.Name,
		Status:     s.Status,
		StartedAt:  s.StartedAt,
		FinishedAt: s.FinishedAt,
		Steps:      make([]stepDTO, 0, len(steps)),
	}
	out.DurationMs = computeDuration(s.StartedAt, s.FinishedAt, 0)
	for _, st := range steps {
		dur := computeDuration(st.StartedAt, st.FinishedAt, 0)
		out.Steps = append(out.Steps, stepDTO{
			ID:         st.ID,
			Name:       st.Name,
			Index:      st.StepIndex,
			Status:     st.Status,
			ExitCode:   st.ExitCode,
			LogObject:  st.LogObject,
			LogSize:    st.LogSize,
			StartedAt:  st.StartedAt,
			FinishedAt: st.FinishedAt,
			DurationMs: dur,
		})
	}
	return out
}

// computeDuration 优先用 stored, 否则用 finished-started, 否则 0。
func computeDuration(started, finished *time.Time, stored int64) int64 {
	if stored > 0 {
		return stored
	}
	if started != nil && finished != nil && finished.After(*started) {
		return finished.Sub(*started).Milliseconds()
	}
	return 0
}
