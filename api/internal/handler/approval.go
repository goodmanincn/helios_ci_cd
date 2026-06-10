// Package handler — ApprovalHandler: 人工审批 REST 端点 (T2.6.2).
//
// 路由:
//   POST /api/v1/runs/:id/approvals/:stage_id/approve  {"comment":"..."}
//   POST /api/v1/runs/:id/approvals/:stage_id/reject   {"comment":"..."}
//
// 设计:
//   - GET list 不开独立端点; runs/:id 详情会内嵌 approval_requests 列表 (RunHandler.detail 接 ApprovalLister)
//   - 鉴权拿 claims.Username 校验是否在 RequiredApprovers (含 '*' 通配)
//   - service 错误 → HTTP 状态码映射: not-found→404, not-approver→403, run/request-not-pending→409, already-voted→409
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/service"
)

// ApprovalVoter 窄接口, 让 handler 不直接依赖 *service.ApprovalService (测试用 fake).
type ApprovalVoter interface {
	Approve(ctx context.Context, in service.VoteInput) (*service.VoteResult, error)
	Reject(ctx context.Context, in service.VoteInput) (*service.VoteResult, error)
}

// ApprovalLister 给 RunHandler.detail 用; 列指定 run 的所有 request + 投票.
type ApprovalLister interface {
	ListByRun(ctx context.Context, runID int64) ([]service.ApprovalSummary, error)
}

// ApprovalHandler 路由层.
type ApprovalHandler struct {
	svc ApprovalVoter
}

// NewApprovalHandler 构造.
func NewApprovalHandler(svc ApprovalVoter) *ApprovalHandler {
	return &ApprovalHandler{svc: svc}
}

// Register 挂到 protected 组.
func (h *ApprovalHandler) Register(g *gin.RouterGroup) {
	g.POST("/runs/:id/approvals/:stage_id/approve", h.approve)
	g.POST("/runs/:id/approvals/:stage_id/reject", h.reject)
}

type approvalBody struct {
	Comment string `json:"comment"`
}

func (h *ApprovalHandler) approve(c *gin.Context) {
	h.vote(c, "approve")
}

func (h *ApprovalHandler) reject(c *gin.Context) {
	h.vote(c, "reject")
}

func (h *ApprovalHandler) vote(c *gin.Context, decision string) {
	rid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || rid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	stageID := strings.TrimSpace(c.Param("stage_id"))
	if stageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stage_id required"})
		return
	}

	var body approvalBody
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
			return
		}
	}
	// reject 强制有 comment (UI 也已经在客户端层强制)
	if decision == "reject" && len(strings.TrimSpace(body.Comment)) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reject requires comment (>=3 chars)"})
		return
	}

	claims, ok := middleware.ClaimsFrom(c)
	if !ok || claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	username := claims.Username
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token missing username"})
		return
	}

	in := service.VoteInput{
		RunID:    rid,
		StageID:  stageID,
		UserID:   &claims.UserID,
		Username: username,
		Comment:  body.Comment,
	}

	var (
		res    *service.VoteResult
		voteErr error
	)
	if decision == "approve" {
		res, voteErr = h.svc.Approve(c.Request.Context(), in)
	} else {
		res, voteErr = h.svc.Reject(c.Request.Context(), in)
	}
	if voteErr != nil {
		approvalErrToStatus(c, voteErr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"request_id":      res.Request.ID,
		"request_status":  res.Request.Status,
		"vote_id":         res.Vote.ID,
		"decision":        res.Vote.Decision,
		"username":        res.Vote.Username,
		"next_run_status": res.NextRunStatus,
	})
}

func approvalErrToStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrApprovalNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrNotApprover):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrAlreadyVoted):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrRunNotInApproval),
		errors.Is(err, service.ErrRequestNotPending),
		errors.Is(err, service.ErrUnsupportedMode):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrApprovalDBUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
