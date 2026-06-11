package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/pkg/audit"
)

// AuditHandler handles audit log endpoints
type AuditHandler struct {
	logger *audit.Logger
	db     *gorm.DB
}

// NewAuditHandler creates a new AuditHandler
func NewAuditHandler(logger *audit.Logger, db *gorm.DB) *AuditHandler {
	return &AuditHandler{
		logger: logger,
		db:     db,
	}
}

// ListAuditLogsRequest represents a request to list audit logs
type ListAuditLogsRequest struct {
	OrgID    string `form:"org_id" binding:"required"`
	ActorID  string `form:"actor_id,omitempty"`
	Action   string `form:"action,omitempty"`
	Resource string `form:"resource,omitempty"`
	FromTime string `form:"from_time,omitempty"`
	ToTime   string `form:"to_time,omitempty"`
	Limit    int    `form:"limit,default=50"`
	Offset   int    `form:"offset,default=0"`
}

// ListAuditLogsResponse represents a response with audit logs
type ListAuditLogsResponse struct {
	Logs  []audit.Entry `json:"logs"`
	Total int64         `json:"total"`
}

// List lists audit logs
// @Summary List audit logs
// @Description Get audit logs with filtering and pagination
// @Tags audit
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param actor_id query string false "Actor ID"
// @Param action query string false "Action type"
// @Param resource query string false "Resource type"
// @Param from_time query string false "From time (RFC3339)"
// @Param to_time query string false "To time (RFC3339)"
// @Param limit query int false "Limit (default 50)"
// @Param offset query int false "Offset (default 0)"
// @Success 200 {object} ListAuditLogsResponse
// @Router /api/v1/audit [get]
func (h *AuditHandler) List(c *gin.Context) {
	var req ListAuditLogsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	opts := audit.QueryOptions{
		OrgID:     req.OrgID,
		ActorID:   req.ActorID,
		Resource:  req.Resource,
		Limit:     req.Limit,
		Offset:    req.Offset,
	}

	if req.Action != "" {
		opts.Action = audit.Action(req.Action)
	}

	if req.FromTime != "" {
		if t, err := time.Parse(time.RFC3339, req.FromTime); err == nil {
			opts.FromTime = &t
		}
	}

	if req.ToTime != "" {
		if t, err := time.Parse(time.RFC3339, req.ToTime); err == nil {
			opts.ToTime = &t
		}
	}

	logs, total, err := h.logger.Query(c.Request.Context(), opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ListAuditLogsResponse{
		Logs:  logs,
		Total: total,
	})
}

// Get gets a single audit log entry
// @Summary Get audit log entry
// @Description Get a single audit log entry by ID
// @Tags audit
// @Accept json
// @Produce json
// @Param id path string true "Audit log ID"
// @Success 200 {object} audit.Entry
// @Router /api/v1/audit/{id} [get]
func (h *AuditHandler) Get(c *gin.Context) {
	id := c.Param("id")

	entry, err := h.logger.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Audit log not found"})
		return
	}

	c.JSON(http.StatusOK, entry)
}

// ArchiveRequest represents a request to archive audit logs
type ArchiveRequest struct {
	Before string `json:"before" binding:"required"`
}

// Archive archives old audit logs
// @Summary Archive audit logs
// @Description Archive old audit logs
// @Tags audit
// @Accept json
// @Produce json
// @Param request body ArchiveRequest true "Archive request"
// @Success 200 {object} gin.H
// @Router /api/v1/audit/archive [post]
func (h *AuditHandler) Archive(c *gin.Context) {
	// For now, this is a placeholder
	// In production, you would have an async job that archives old logs
	c.JSON(http.StatusOK, gin.H{"message": "Archiving not implemented yet"})
}

// StatsResponse represents audit stats response
type StatsResponse struct {
	TotalLogs     int64            `json:"total_logs"`
	ActionsCounts map[string]int64 `json:"actions_counts"`
	TopActors     []ActorStats     `json:"top_actors"`
	TopResources  []ResourceStats  `json:"top_resources"`
}

// ActorStats represents actor statistics
type ActorStats struct {
	ActorID string `json:"actor_id"`
	Count   int64  `json:"count"`
}

// ResourceStats represents resource statistics
type ResourceStats struct {
	Resource string `json:"resource"`
	Count    int64  `json:"count"`
}

// Stats gets audit statistics
// @Summary Get audit stats
// @Description Get audit log statistics
// @Tags audit
// @Accept json
// @Produce json
// @Param org_id query string true "Organization ID"
// @Param days query int false "Number of days (default 30)"
// @Success 200 {object} StatsResponse
// @Router /api/v1/audit/stats [get]
func (h *AuditHandler) Stats(c *gin.Context) {
	orgID := c.Query("org_id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id is required"})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid days parameter"})
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	ctx := c.Request.Context()
	var stats StatsResponse

	// Total logs
	if err := h.db.WithContext(ctx).
		Model(&audit.Entry{}).
		Where("org_id = ? AND created_at >= ?", orgID, cutoff).
		Count(&stats.TotalLogs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Action counts
	type actionCount struct {
		Action string
		Count  int64
	}
	var actionCounts []actionCount
	if err := h.db.WithContext(ctx).
		Model(&audit.Entry{}).
		Select("action, count(*) as count").
		Where("org_id = ? AND created_at >= ?", orgID, cutoff).
		Group("action").
		Scan(&actionCounts).Error; err == nil {
		stats.ActionsCounts = make(map[string]int64)
		for _, ac := range actionCounts {
			stats.ActionsCounts[ac.Action] = ac.Count
		}
	}

	// Top actors (placeholder)
	stats.TopActors = []ActorStats{}

	// Top resources (placeholder)
	stats.TopResources = []ResourceStats{}

	c.JSON(http.StatusOK, stats)
}
