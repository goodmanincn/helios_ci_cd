// Package handler — /api/v1/me/* 资源 (T1.7.1 sidebar 计数徽章用)。
//
// 当前只暴露 counters: 项目数 + 执行记录数, 给 sidebar 徽章。
// 后续可扩为通知 / 最近活动等。
//
// 设计: 走简单 COUNT(*), 不分页不过滤。M1 阶段没有完善的 org 多租户隔离,
// 跟 RunHandler 一致 — 任何登录用户能看全部 (内网部署语义)。M2 加 org_id 维度。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// MeHandler 当前用户相关聚合端点。
type MeHandler struct {
	db *gorm.DB
}

func NewMeHandler(db *gorm.DB) *MeHandler {
	return &MeHandler{db: db}
}

// Register 挂到受保护 /api/v1。
func (h *MeHandler) Register(g *gin.RouterGroup) {
	g.GET("/me/counters", h.counters)
}

// GET /api/v1/me/counters → {projects: N, runs: N}
// 给 sidebar 徽章用。任何查询失败都返 0, 不让前端因小接口炸掉布局。
func (h *MeHandler) counters(c *gin.Context) {
	ctx := c.Request.Context()
	var projects, runs int64
	_ = h.db.WithContext(ctx).Model(&model.Project{}).Count(&projects).Error
	_ = h.db.WithContext(ctx).Model(&model.Run{}).Count(&runs).Error
	c.JSON(http.StatusOK, gin.H{
		"projects": projects,
		"runs":     runs,
	})
}
