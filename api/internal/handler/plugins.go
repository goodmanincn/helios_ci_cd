// Package handler — plugins.go: 插件市场 REST API (M9 MVP).
//
// 端点:
//
//   GET    /api/v1/plugins                   列表 (?q= ?category= ?verified=true)
//   GET    /api/v1/plugins/installed         当前 org 已安装
//   GET    /api/v1/plugins/:namespace/:name  详情 (含全部 versions + 当前 org 是否已安装)
//   POST   /api/v1/plugins/:namespace/:name/install   body {version} → 安装到当前 org
//   DELETE /api/v1/plugins/:namespace/:name/install   卸载
//
// 设计:
//   - 列表 / 详情对所有登录用户可见 (插件是全局资源, 隔离在安装关系上)
//   - 安装/卸载操作 active org 维度
//   - 删除 plugin 本身不开 API (本轮只能 seed 写, 不能从 UI 删)
package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/repository"
)

type PluginHandler struct {
	db    *gorm.DB
	store repository.PluginStore
}

func NewPluginHandler(db *gorm.DB) *PluginHandler {
	return &PluginHandler{db: db, store: repository.NewPluginRepository(db)}
}

func (h *PluginHandler) Register(g *gin.RouterGroup) {
	g.GET("/plugins", h.list)
	g.GET("/plugins/installed", h.listInstalled)
	g.GET("/plugins/:namespace/:name", h.get)
	g.POST("/plugins/:namespace/:name/install", h.install)
	g.DELETE("/plugins/:namespace/:name/install", h.uninstall)
}

func (h *PluginHandler) list(c *gin.Context) {
	if _, ok := activeOrg(c); !ok {
		return
	}
	list, err := h.store.List(repository.PluginFilter{
		Category: c.Query("category"),
		Q:        c.Query("q"),
		Verified: c.Query("verified") == "true",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	if list == nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	c.JSON(http.StatusOK, list)
}

type pluginDetailResp struct {
	Plugin    any   `json:"plugin"`
	Versions  any   `json:"versions"`
	Installed *bool `json:"installed,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

func (h *PluginHandler) get(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	slug, ok := joinSlug(c)
	if !ok {
		return
	}
	p, versions, err := h.store.GetBySlug(slug)
	if err != nil {
		respondPluginErr(c, err)
		return
	}
	resp := pluginDetailResp{Plugin: p, Versions: versions}
	if inst, err := h.store.GetInstallation(orgID, p.ID); err == nil {
		installed := true
		resp.Installed = &installed
		for _, v := range versions {
			if v.ID == inst.VersionID {
				resp.InstalledVersion = v.Version
				break
			}
		}
	} else if !errors.Is(err, repository.ErrPluginNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "installation lookup failed"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

type installReq struct {
	Version string `json:"version"` // "v1" / "1.2.3" / "latest" / "" (→ latest)
}

type installResp struct {
	PluginID  int64  `json:"plugin_id"`
	VersionID int64  `json:"version_id"`
	Version   string `json:"version"`
	OrgID     int64  `json:"org_id"`
}

func (h *PluginHandler) install(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	slug, ok := joinSlug(c)
	if !ok {
		return
	}
	var req installReq
	_ = c.ShouldBindJSON(&req) // body 可空 → 默认 latest

	p, _, err := h.store.GetBySlug(slug)
	if err != nil {
		respondPluginErr(c, err)
		return
	}
	v, err := h.store.GetVersionByName(p.ID, req.Version)
	if err != nil {
		respondPluginErr(c, err)
		return
	}

	uid := userIDFrom(c)
	var by *int64
	if uid > 0 {
		by = &uid
	}
	if _, err := h.store.InstallToOrg(orgID, p.ID, v.ID, by); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "install failed: " + err.Error()})
		return
	}
	_ = h.store.IncrementDownloads(p.ID)
	c.JSON(http.StatusCreated, installResp{
		PluginID: p.ID, VersionID: v.ID, Version: v.Version, OrgID: orgID,
	})
}

func (h *PluginHandler) uninstall(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	slug, ok := joinSlug(c)
	if !ok {
		return
	}
	p, _, err := h.store.GetBySlug(slug)
	if err != nil {
		respondPluginErr(c, err)
		return
	}
	if err := h.store.Uninstall(orgID, p.ID); err != nil {
		respondPluginErr(c, err)
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *PluginHandler) listInstalled(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	list, err := h.store.ListInstalled(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list installed failed"})
		return
	}
	if list == nil {
		list = []repository.InstalledPlugin{}
	}
	c.JSON(http.StatusOK, list)
}

// ----- helpers -----

func joinSlug(c *gin.Context) (string, bool) {
	ns := strings.TrimSpace(c.Param("namespace"))
	nm := strings.TrimSpace(c.Param("name"))
	if ns == "" || nm == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace/name required"})
		return "", false
	}
	return ns + "/" + nm, true
}

func respondPluginErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrPluginNotFound),
		errors.Is(err, repository.ErrPluginVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
