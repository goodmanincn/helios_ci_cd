// Package handler — cluster.go: 集群接入向导与连通性测试 (E4.2)。
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/pkg/cluster"
	"github.com/helios-cicd/helios/api/pkg/cluster/selfhosted"
)

// ClusterHandler 集群管理 API。
type ClusterHandler struct {
	db    *gorm.DB
	store repository.ClusterStore
}

func NewClusterHandler(db *gorm.DB) *ClusterHandler {
	return &ClusterHandler{db: db, store: repository.NewClusterRepository(db)}
}

// Register 挂到受保护 /api/v1。
func (h *ClusterHandler) Register(g *gin.RouterGroup) {
	g.GET("/clusters", h.list)
	g.POST("/clusters", h.create)
	g.GET("/clusters/:id", h.get)
	g.DELETE("/clusters/:id", h.delete)
	g.POST("/clusters/test", h.test)
}

// ===== GET /clusters =====

func (h *ClusterHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	list, err := h.store.ListByOrg(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ===== POST /clusters =====

type createClusterReq struct {
	Name       string `json:"name" binding:"required"`
	Provider   string `json:"provider" binding:"required"`
	Region     string `json:"region"`
	Kubeconfig string `json:"kubeconfig"` // selfhosted 必填
}

func (h *ClusterHandler) create(c *gin.Context) {
	var req createClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Provider == "selfhosted" && req.Kubeconfig == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kubeconfig is required for selfhosted"})
		return
	}

	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	configMap := map[string]string{"kubeconfig": req.Kubeconfig}
	configBytes, _ := json.Marshal(configMap)
	cl := &model.Cluster{
		OrgID:    orgID,
		Name:     req.Name,
		Provider: req.Provider,
		Region:   req.Region,
		Config:   datatypes.JSON(configBytes),
		Status:   "unknown",
	}
	if err := h.store.Create(cl); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create failed"})
		return
	}
	c.JSON(http.StatusCreated, cl)
}

// ===== GET /clusters/:id =====

func (h *ClusterHandler) get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	cl, err := h.store.Get(id)
	if err != nil {
		if err == repository.ErrClusterNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, cl)
}

// ===== DELETE /clusters/:id =====

func (h *ClusterHandler) delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.store.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// ===== POST /clusters/test =====

type testClusterReq struct {
	Provider   string `json:"provider" binding:"required"`
	Kubeconfig string `json:"kubeconfig" binding:"required"`
}

func (h *ClusterHandler) test(c *gin.Context) {
	var req testClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p, err := selfhosted.New(cluster.ClusterConfig{
		Provider:   req.Provider,
		Kubeconfig: []byte(req.Kubeconfig),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	info, err := p.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// ---- helpers ----

func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate"))
}
