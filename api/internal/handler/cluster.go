// Package handler — cluster.go: 集群管理 API (E4.2/E4.5/E4.6)。
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/pkg/cluster"
	"github.com/helios-cicd/helios/api/pkg/cluster/aliyun"
	"github.com/helios-cicd/helios/api/pkg/cluster/selfhosted"
	"github.com/helios-cicd/helios/api/pkg/cluster/tencent"
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
	g.POST("/clusters/discover", h.discover)
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
	Name       string          `json:"name" binding:"required"`
	Provider   string          `json:"provider" binding:"required"`
	Region     string          `json:"region"`
	Kubeconfig string          `json:"kubeconfig"` // selfhosted
	Cloud      json.RawMessage `json:"cloud"`      // tke/ack 凭据 JSON
}

func (h *ClusterHandler) create(c *gin.Context) {
	var req createClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	configBytes, err := h.buildClusterConfig(req.Provider, req.Kubeconfig, req.Cloud)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
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
	Provider   string          `json:"provider" binding:"required"`
	Kubeconfig string          `json:"kubeconfig"`
	Cloud      json.RawMessage `json:"cloud"`
}

func (h *ClusterHandler) test(c *gin.Context) {
	var req testClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfgBytes, err := h.buildClusterConfig(req.Provider, req.Kubeconfig, req.Cloud)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.providerFromConfig(req.Provider, cfgBytes)
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

type discoverClusterReq struct {
	Provider string          `json:"provider" binding:"required"`
	Cloud    json.RawMessage `json:"cloud" binding:"required"`
}

func (h *ClusterHandler) discover(c *gin.Context) {
	var req discoverClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	switch req.Provider {
	case "tke":
		var creds tencent.CloudCredentials
		if err := json.Unmarshal(req.Cloud, &creds); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cloud credentials"})
			return
		}
		list, err := tencent.ListClusters(ctx, creds)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"clusters": list})
	case "ack":
		var creds aliyun.CloudCredentials
		if err := json.Unmarshal(req.Cloud, &creds); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cloud credentials"})
			return
		}
		list, err := aliyun.ListClusters(ctx, creds)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"clusters": list})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "discover only supports tke or ack"})
	}
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

func (h *ClusterHandler) buildClusterConfig(provider, kubeconfig string, cloud json.RawMessage) ([]byte, error) {
	switch provider {
	case "selfhosted":
		if kubeconfig == "" {
			return nil, fmt.Errorf("kubeconfig is required for selfhosted")
		}
		return json.Marshal(map[string]string{"kubeconfig": kubeconfig})
	case "tke", "ack":
		if len(cloud) == 0 {
			return nil, fmt.Errorf("cloud credentials required for %s", provider)
		}
		// 存到 config.kubeconfig 字段 (历史命名; 内容是 cloud JSON)
		return json.Marshal(map[string]string{"kubeconfig": string(cloud)})
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}

func (h *ClusterHandler) providerFromConfig(provider string, configBytes []byte) (cluster.Provider, error) {
	var cfgMap map[string]string
	if err := json.Unmarshal(configBytes, &cfgMap); err != nil {
		return nil, err
	}
	raw := []byte(cfgMap["kubeconfig"])
	switch provider {
	case "selfhosted":
		return selfhosted.New(cluster.ClusterConfig{Provider: provider, Kubeconfig: raw})
	case "tke":
		return tencent.New(cluster.ClusterConfig{Provider: provider, Kubeconfig: raw})
	case "ack":
		return aliyun.New(cluster.ClusterConfig{Provider: provider, Kubeconfig: raw})
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}

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
	var cfgMap map[string]string
	if err := json.Unmarshal(cl.Config, &cfgMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid cluster config"})
		return nil, false
	}

	p, pErr := h.providerFromConfig(cl.Provider, cl.Config)
	if pErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": pErr.Error()})
		return nil, false
	}
	return p, true
}

func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate"))
}
