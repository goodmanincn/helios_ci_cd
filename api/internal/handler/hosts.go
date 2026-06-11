// Package handler — hosts.go: 主机管理 API (E6.1)。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
)

// HostHandler 主机管理 API。
type HostHandler struct {
	db     *gorm.DB
	store  repository.HostStore
	groups repository.HostGroupStore
}

func NewHostHandler(db *gorm.DB) *HostHandler {
	return &HostHandler{
		db:     db,
		store:  repository.NewHostRepository(db),
		groups: repository.NewHostGroupRepository(db),
	}
}

// Register 挂到受保护 /api/v1。
func (h *HostHandler) Register(g *gin.RouterGroup) {
	g.GET("/hosts", h.list)
	g.POST("/hosts", h.create)
	g.GET("/hosts/:id", h.get)
	g.PUT("/hosts/:id", h.update)
	g.DELETE("/hosts/:id", h.delete)
	g.POST("/hosts/:id/test", h.test)
}

// ===== GET /hosts =====

func (h *HostHandler) list(c *gin.Context) {
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	q := c.Query("q")
	label := c.Query("label")
	group := c.Query("group") // 按组名过滤 (T6.1.3)

	// 按组查走 join 路径; 没指定组则按 org 拉全量再内存过滤
	var list []model.Host
	var err error
	if group != "" {
		g, gerr := h.groups.GetByName(orgID, group)
		if gerr != nil {
			if errors.Is(gerr, repository.ErrHostGroupNotFound) {
				c.JSON(http.StatusOK, []model.Host{})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "group lookup failed"})
			return
		}
		list, err = h.groups.ListMembers(g.ID)
	} else {
		list, err = h.store.ListByOrg(orgID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	// 内存过滤 q + label
	out := make([]model.Host, 0, len(list))
	for _, h := range list {
		if q != "" && !strings.Contains(h.Name, q) && !strings.Contains(h.IP, q) {
			continue
		}
		if label != "" && !hostHasLabel(h.Labels, label) {
			continue
		}
		out = append(out, h)
	}
	c.JSON(http.StatusOK, out)
}

// ===== POST /hosts =====

type createHostReq struct {
	Name         string         `json:"name" binding:"required"`
	IP           string         `json:"ip" binding:"required"`
	SSHPort      int            `json:"ssh_port"`
	SSHUser      string         `json:"ssh_user"`
	CredentialID *int64         `json:"credential_id"`
	Labels       datatypes.JSON `json:"labels"`
}

func (h *HostHandler) create(c *gin.Context) {
	var req createHostReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SSHPort <= 0 {
		req.SSHPort = 22
	}
	if req.SSHUser == "" {
		req.SSHUser = "root"
	}

	orgID, ok := activeOrg(c)
	if !ok {
		return
	}
	host := &model.Host{
		OrgID:        orgID,
		Name:         req.Name,
		IP:           req.IP,
		SSHPort:      req.SSHPort,
		SSHUser:      req.SSHUser,
		CredentialID: req.CredentialID,
		Labels:       req.Labels,
		Status:       "unknown",
	}
	if err := h.store.Create(host); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create failed"})
		return
	}
	c.JSON(http.StatusCreated, host)
}

// ===== GET /hosts/:id =====

func (h *HostHandler) get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	host, err := h.store.Get(id)
	if err != nil {
		if err == repository.ErrHostNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, host)
}

// ===== PUT /hosts/:id =====

type updateHostReq struct {
	Name         string         `json:"name"`
	IP           string         `json:"ip"`
	SSHPort      int            `json:"ssh_port"`
	SSHUser      string         `json:"ssh_user"`
	CredentialID *int64         `json:"credential_id"`
	Labels       datatypes.JSON `json:"labels"`
}

func (h *HostHandler) update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req updateHostReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, err := h.store.Get(id)
	if err != nil {
		if err == repository.ErrHostNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	if req.Name != "" {
		host.Name = req.Name
	}
	if req.IP != "" {
		host.IP = req.IP
	}
	if req.SSHPort > 0 {
		host.SSHPort = req.SSHPort
	}
	if req.SSHUser != "" {
		host.SSHUser = req.SSHUser
	}
	if req.CredentialID != nil {
		host.CredentialID = req.CredentialID
	}
	if req.Labels != nil {
		host.Labels = req.Labels
	}

	if err := h.store.Update(host); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, host)
}

// ===== DELETE /hosts/:id =====

func (h *HostHandler) delete(c *gin.Context) {
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

// ===== POST /hosts/:id/test =====

func (h *HostHandler) test(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	host, err := h.store.Get(id)
	if err != nil {
		if err == repository.ErrHostNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	// 1. 先测 TCP 连通性
	addr := net.JoinHostPort(host.IP, strconv.Itoa(host.SSHPort))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("tcp connect failed: %v", err)})
		return
	}
	conn.Close()

	// 2. SSH 握手 (密码/密钥留 E6.2 接入凭据表后实现; 此处只做匿名探测)
	sshCfg := &ssh.ClientConfig{
		User:            host.SSHUser,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // dev 场景; prod 配 known_hosts
		Timeout:         5 * time.Second,
	}
	sshClient, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		// SSH 握手失败可能是认证问题,至少 TCP 通了
		c.JSON(http.StatusOK, gin.H{
			"reachable": true,
			"ssh_ok":    false,
			"message":   fmt.Sprintf("tcp ok, ssh handshake failed: %v", err),
		})
		return
	}
	defer sshClient.Close()

	// 3. 跑 uname -a
	session, err := sshClient.NewSession()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"reachable": true,
			"ssh_ok":    true,
			"message":   fmt.Sprintf("ssh connected, session failed: %v", err),
		})
		return
	}
	defer session.Close()

	out, err := session.CombinedOutput("uname -a")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"reachable": true,
			"ssh_ok":    true,
			"message":   fmt.Sprintf("ssh connected, uname failed: %v", err),
		})
		return
	}

	// 更新状态
	host.Status = "online"
	now := time.Now()
	host.LastHeartbeat = &now
	_ = h.store.Update(host)

	c.JSON(http.StatusOK, gin.H{
		"reachable": true,
		"ssh_ok":    true,
		"uname":     strings.TrimSpace(string(out)),
	})
}

// ---- helpers ----

func hostHasLabel(labels datatypes.JSON, query string) bool {
	var m map[string]string
	if err := json.Unmarshal(labels, &m); err != nil {
		return false
	}
	// 支持两种格式: "key=value" 或 "key"
	if strings.Contains(query, "=") {
		parts := strings.SplitN(query, "=", 2)
		return m[parts[0]] == parts[1]
	}
	_, ok := m[query]
	return ok
}
