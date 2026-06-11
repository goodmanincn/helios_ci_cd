// Package handler — host_groups.go: 主机组管理 API (T6.1.3)。
//
// 端点:
//   GET    /api/v1/host-groups            列出 org 下全部组
//   POST   /api/v1/host-groups            创建组
//   GET    /api/v1/host-groups/:id        获取单组 (含成员)
//   PUT    /api/v1/host-groups/:id        改名 / vars
//   DELETE /api/v1/host-groups/:id        删除组 (成员自动级联)
//   POST   /api/v1/host-groups/:id/members      加成员 {host_ids:[...]}
//   DELETE /api/v1/host-groups/:id/members/:hid 删一个成员
//
// 跨 org 校验:
//   - get/update/delete 都校验组 org_id == active org, 跨 org 返 404 (不泄露存在性)
//   - 加成员时校验 host 也属于同 org
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
)

type HostGroupHandler struct {
	db      *gorm.DB
	groups  repository.HostGroupStore
	hosts   repository.HostStore
}

func NewHostGroupHandler(db *gorm.DB) *HostGroupHandler {
	return &HostGroupHandler{
		db:     db,
		groups: repository.NewHostGroupRepository(db),
		hosts:  repository.NewHostRepository(db),
	}
}

func (h *HostGroupHandler) Register(g *gin.RouterGroup) {
	g.GET("/host-groups", h.list)
	g.POST("/host-groups", h.create)
	g.GET("/host-groups/:id", h.get)
	g.PUT("/host-groups/:id", h.update)
	g.DELETE("/host-groups/:id", h.delete)
	g.POST("/host-groups/:id/members", h.addMembers)
	g.DELETE("/host-groups/:id/members/:hid", h.removeMember)
}

// ===== handlers =====

func (h *HostGroupHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	list, err := h.groups.ListByOrg(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if list == nil {
		list = []model.HostGroup{}
	}
	c.JSON(http.StatusOK, list)
}

type createHostGroupReq struct {
	Name string         `json:"name" binding:"required"`
	Vars datatypes.JSON `json:"vars"`
}

func (h *HostGroupHandler) create(c *gin.Context) {
	var req createHostGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g := &model.HostGroup{OrgID: orgID, Name: req.Name, Vars: req.Vars}
	if err := h.groups.Create(g); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create failed"})
		return
	}
	c.JSON(http.StatusCreated, g)
}

func (h *HostGroupHandler) get(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g, err := h.loadOwned(c.Param("id"), orgID)
	if err != nil {
		respondGroupErr(c, err)
		return
	}
	members, err := h.groups.ListMembers(g.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list members failed"})
		return
	}
	if members == nil {
		members = []model.Host{}
	}
	c.JSON(http.StatusOK, gin.H{"group": g, "members": members})
}

type updateHostGroupReq struct {
	Name string         `json:"name"`
	Vars datatypes.JSON `json:"vars"`
}

func (h *HostGroupHandler) update(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g, err := h.loadOwned(c.Param("id"), orgID)
	if err != nil {
		respondGroupErr(c, err)
		return
	}
	var req updateHostGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != "" {
		g.Name = req.Name
	}
	if req.Vars != nil {
		g.Vars = req.Vars
	}
	if err := h.groups.Update(g); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (h *HostGroupHandler) delete(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g, err := h.loadOwned(c.Param("id"), orgID)
	if err != nil {
		respondGroupErr(c, err)
		return
	}
	if err := h.groups.Delete(g.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

type addMembersReq struct {
	HostIDs []int64 `json:"host_ids" binding:"required,min=1"`
}

func (h *HostGroupHandler) addMembers(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g, err := h.loadOwned(c.Param("id"), orgID)
	if err != nil {
		respondGroupErr(c, err)
		return
	}
	var req addMembersReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 校验所有 host 都属于同 org, 任一不匹配整批失败 (避免静默部分成功)
	for _, hid := range req.HostIDs {
		host, err := h.hosts.Get(hid)
		if err != nil {
			if errors.Is(err, repository.ErrHostNotFound) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "host not found", "host_id": hid})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
			return
		}
		if host.OrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "host not in active org", "host_id": hid})
			return
		}
	}

	added := 0
	for _, hid := range req.HostIDs {
		if err := h.groups.AddMember(g.ID, hid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "add member failed", "host_id": hid})
			return
		}
		added++
	}
	c.JSON(http.StatusOK, gin.H{"added": added})
}

func (h *HostGroupHandler) removeMember(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	g, err := h.loadOwned(c.Param("id"), orgID)
	if err != nil {
		respondGroupErr(c, err)
		return
	}
	hid, err := strconv.ParseInt(c.Param("hid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host id"})
		return
	}
	if err := h.groups.RemoveMember(g.ID, hid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "remove failed"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ===== helpers =====

// loadOwned 加载组并校验 org_id, 不匹配返 ErrHostGroupNotFound (避免存在性泄露)。
func (h *HostGroupHandler) loadOwned(idStr string, orgID int64) (*model.HostGroup, error) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, errBadGroupID
	}
	g, err := h.groups.Get(id)
	if err != nil {
		return nil, err
	}
	if g.OrgID != orgID {
		return nil, repository.ErrHostGroupNotFound
	}
	return g, nil
}

var errBadGroupID = errors.New("invalid group id")

func respondGroupErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errBadGroupID):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
	case errors.Is(err, repository.ErrHostGroupNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "host group not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
