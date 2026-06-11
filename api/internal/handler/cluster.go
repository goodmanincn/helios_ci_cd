// Package handler — cluster.go: 集群管理 API (E4.2/E4.5/E4.6)。
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
	g.GET("/clusters/:id/workloads", h.workloads)
	g.GET("/clusters/:id/events", h.events)
	g.GET("/clusters/:id/deployments/:name/history", h.deploymentHistory)
	g.POST("/clusters/:id/deployments/:name/rollback", h.rollback)
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

// ===== GET /clusters/:id/workloads =====

func (h *ClusterHandler) workloads(c *gin.Context) {
	p, ok := h.resolveProvider(c)
	if !ok {
		return
	}
	ns := c.Query("ns")
	if ns == "" {
		ns = "default"
	}
	list, err := p.ListWorkloads(c.Request.Context(), ns)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ===== GET /clusters/:id/events =====

func (h *ClusterHandler) events(c *gin.Context) {
	p, ok := h.resolveProvider(c)
	if !ok {
		return
	}
	ns := c.Query("ns")
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 100
	}
	list, err := p.GetEvents(c.Request.Context(), ns, limit)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ===== GET /clusters/:id/deployments/:name/history =====

func (h *ClusterHandler) deploymentHistory(c *gin.Context) {
	p, ok := h.resolveProvider(c)
	if !ok {
		return
	}
	ns := c.Query("ns")
	if ns == "" {
		ns = "default"
	}
	name := c.Param("name")
	hist, err := p.GetDeploymentHistory(c.Request.Context(), ns, name)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, hist)
}

// ===== POST /clusters/:id/deployments/:name/rollback =====

func (h *ClusterHandler) rollback(c *gin.Context) {
	p, ok := h.resolveProvider(c)
	if !ok {
		return
	}
	ns := c.Query("ns")
	if ns == "" {
		ns = "default"
	}
	name := c.Param("name")
	toRevision, _ := strconv.ParseInt(c.Query("to"), 10, 64)
	if toRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to revision required"})
		return
	}
	if err := p.Rollback(c.Request.Context(), ns, name, toRevision); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rollback initiated"})
}

// ---- helpers ----

func (h *ClusterHandler) resolveProvider(c *gin.Context) (cluster.Provider, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return nil, false
	}
	cl, err := h.store.Get(id)
	if err != nil {
		if err == repository.ErrClusterNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return nil, false
	}
	if cl.Provider != "selfhosted" {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "provider not supported yet"})
		return nil, false
	}
	var cfgMap map[string]string
	if err := json.Unmarshal(cl.Config, &cfgMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid cluster config"})
		return nil, false
	}
	p, err := selfhosted.New(cluster.ClusterConfig{
		Provider:   cl.Provider,
		Kubeconfig: []byte(cfgMap["kubeconfig"]),
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return nil, false
	}
	return p, true
}

func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate"))
}
