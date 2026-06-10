// Package handler — secret.go: Secrets 保险箱 CRUD (M2 T2.5.2)。
//
// 端点 (全部受 authMW 保护):
//   GET    /api/v1/secrets               列表 (按 scope 过滤, 不返 value)
//   POST   /api/v1/secrets               创建 (encrypt 后入库)
//   GET    /api/v1/secrets/:id           取单个 (元数据, 不含 value)
//   PUT    /api/v1/secrets/:id           轮换 value (元数据可改 description)
//   DELETE /api/v1/secrets/:id           软删
//
// 安全约束:
//   - 任何端点 *永远* 不返 value (即便是 owner). 想用就在 stage 里 reference, runner 解密.
//   - scope=org/project/pipeline + scope_id, 必须与 caller 当前 org 关联 (M2 简化: 只校 org)
//   - encrypt/decrypt 都走 *crypto.Vault, vault 在 main.go 注入; vault nil 时 503 (容错降级)
//
// 审计 (M3 单独 task): secret CRUD 都该写 audit_logs, 这里先打 log.Printf 占位.
package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/pkg/crypto"
)

// SecretHandler.
type SecretHandler struct {
	db    *gorm.DB
	vault *crypto.Vault // 可空, 空时所有写/解密路径返 503
}

func NewSecretHandler(db *gorm.DB, vault *crypto.Vault) *SecretHandler {
	return &SecretHandler{db: db, vault: vault}
}

// Register 挂到受保护 /api/v1.
func (h *SecretHandler) Register(g *gin.RouterGroup) {
	g.GET("/secrets", h.list)
	g.POST("/secrets", h.create)
	g.GET("/secrets/:id", h.get)
	g.PUT("/secrets/:id", h.update)
	g.DELETE("/secrets/:id", h.del)
}

// ===== DTO =====

type secretView struct {
	ID          int64  `json:"id"`
	Scope       string `json:"scope"`
	ScopeID     int64  `json:"scope_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	KEKID       string `json:"kek_id,omitempty"`
	CreatedBy   *int64 `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toSecretView(s *model.Secret) *secretView {
	return &secretView{
		ID:          s.ID,
		Scope:       s.Scope,
		ScopeID:     s.ScopeID,
		Name:        s.Name,
		Type:        s.Type,
		Description: s.Description,
		KEKID:       s.EncryptionKEKID,
		CreatedBy:   s.CreatedBy,
		CreatedAt:   s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

type secretListResp struct {
	Items []*secretView `json:"items"`
	Total int64         `json:"total"`
}

// ===== 端点 =====

// GET /secrets?scope=org&scope_id=1&type=text&q=...
func (h *SecretHandler) list(c *gin.Context) {
	q := h.db.WithContext(c.Request.Context()).Model(&model.Secret{})

	// 当前 org 视图: 简化 M2 — 只允许列出当前 org 范围内 (scope=org+org_id, 或 scope=project/pipeline
	// 但 scope_id 指向当前 org 名下的 project/pipeline). 暂用前者宽口径过滤, 后续接 RBAC 严格化.
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}

	scope := strings.TrimSpace(c.Query("scope"))
	scopeIDStr := strings.TrimSpace(c.Query("scope_id"))
	typ := strings.TrimSpace(c.Query("type"))
	keyword := strings.TrimSpace(c.Query("q"))

	if scope != "" {
		if !inOptionsSecret(scope, "org", "project", "pipeline") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope (allowed: org / project / pipeline)"})
			return
		}
		q = q.Where("scope = ?", scope)
		if scopeIDStr != "" {
			sid, err := strconv.ParseInt(scopeIDStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope_id"})
				return
			}
			q = q.Where("scope_id = ?", sid)
		}
	} else {
		// 默认: 只返本 org 名下 secrets (scope=org + scope_id=orgID), M3 接 RBAC 后再扩
		q = q.Where("scope = ? AND scope_id = ?", "org", orgID)
	}

	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name ILIKE ? OR description ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var items []model.Secret
	if err := q.Order("id DESC").Limit(200).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	out := make([]*secretView, 0, len(items))
	for i := range items {
		out = append(out, toSecretView(&items[i]))
	}
	c.JSON(http.StatusOK, secretListResp{Items: out, Total: total})
}

type createSecretReq struct {
	Scope       string `json:"scope"        binding:"required"`
	ScopeID     int64  `json:"scope_id"     binding:"required"`
	Name        string `json:"name"         binding:"required"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Value       string `json:"value"        binding:"required"`
}

// POST /secrets
func (h *SecretHandler) create(c *gin.Context) {
	if h.vault == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vault not configured (HELIOS_KEK_BASE64?)"})
		return
	}
	orgID, ok := activeOrg(c)
	if !ok {
		return
	}

	var req createSecretReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if !inOptionsSecret(req.Scope, "org", "project", "pipeline") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope"})
		return
	}
	// scope=org: scope_id 必须是 caller 当前 org
	if req.Scope == "org" && req.ScopeID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "scope_id must match active org"})
		return
	}
	if req.Type == "" {
		req.Type = "text"
	}
	if !inOptionsSecret(req.Type, "text", "file", "kubeconfig", "ssh-key", "cloud-credential") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}

	enc, err := h.vault.Encrypt([]byte(req.Value))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt: " + err.Error()})
		return
	}
	uid := userIDFrom(c)
	s := model.Secret{
		Scope:           req.Scope,
		ScopeID:         req.ScopeID,
		Name:            req.Name,
		Type:            req.Type,
		Description:     req.Description,
		EncryptedValue:  enc,
		EncryptionKEKID: h.vault.PrimaryID(),
	}
	if uid > 0 {
		s.CreatedBy = &uid
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&s).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create: " + err.Error()})
		return
	}
	log.Printf("[secret] created id=%d scope=%s scope_id=%d name=%s by uid=%d",
		s.ID, s.Scope, s.ScopeID, s.Name, uid)
	c.JSON(http.StatusCreated, toSecretView(&s))
}

// GET /secrets/:id (元数据, 不含 value)
func (h *SecretHandler) get(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	s, ok := loadSecretOrErr(c, h.db, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toSecretView(s))
}

type updateSecretReq struct {
	// value 可选; 提供则轮换密文 (重新加密)
	Value       *string `json:"value,omitempty"`
	Description *string `json:"description,omitempty"`
}

// PUT /secrets/:id (轮换 value 或改 description)
func (h *SecretHandler) update(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req updateSecretReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Value == nil && req.Description == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}
	s, ok := loadSecretOrErr(c, h.db, id)
	if !ok {
		return
	}
	if req.Value != nil {
		if h.vault == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vault not configured"})
			return
		}
		enc, err := h.vault.Encrypt([]byte(*req.Value))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt: " + err.Error()})
			return
		}
		s.EncryptedValue = enc
		s.EncryptionKEKID = h.vault.PrimaryID()
	}
	if req.Description != nil {
		s.Description = *req.Description
	}
	if err := h.db.WithContext(c.Request.Context()).Save(s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[secret] updated id=%d (value_rotated=%v)", s.ID, req.Value != nil)
	c.JSON(http.StatusOK, toSecretView(s))
}

// DELETE /secrets/:id (软删)
func (h *SecretHandler) del(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	res := h.db.WithContext(c.Request.Context()).Delete(&model.Secret{}, id)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}
	log.Printf("[secret] deleted id=%d", id)
	c.Status(http.StatusNoContent)
}

// ===== 内部 API: 给 engine/worker 解密用 =====

// ResolveSecrets 给 stage 的 secrets[] 列表, 在指定 scope 下查 value, 返 name→plaintext.
// 这是 secret 走出 API 边界的 *唯一* 路径; 调用 audit 写日志 (M3 拆出来).
//
// scope/scopeID 通常是 stage 所在 pipeline 的 org/project, 由 caller 决定.
// 找不到的 name 走 errs 累积, 不中断其它 secret 解密.
func (h *SecretHandler) ResolveSecrets(
	ctx context.Context,
	scope string, scopeID int64, names []string,
) (resolved map[string]string, missing []string, err error) {
	return ResolveSecrets(ctx, h.db, h.vault, scope, scopeID, names)
}

// ResolveSecrets 包级函数, 同上但不需要 handler 实例 (engine 端用).
func ResolveSecrets(
	ctx context.Context,
	db *gorm.DB,
	vault *crypto.Vault,
	scope string, scopeID int64, names []string,
) (map[string]string, []string, error) {
	if vault == nil {
		return nil, nil, errors.New("vault not configured")
	}
	if len(names) == 0 {
		return map[string]string{}, nil, nil
	}
	var rows []model.Secret
	if err := db.WithContext(ctx).
		Where("scope = ? AND scope_id = ? AND name IN ?", scope, scopeID, names).
		Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	found := make(map[string]string, len(rows))
	for i := range rows {
		plain, err := vault.Decrypt(rows[i].EncryptedValue)
		if err != nil {
			// 单条解密失败不毒所有; 当成 missing
			log.Printf("[secret] decrypt failed id=%d name=%s err=%v",
				rows[i].ID, rows[i].Name, err)
			continue
		}
		found[rows[i].Name] = string(plain)
	}
	var missing []string
	for _, n := range names {
		if _, ok := found[n]; !ok {
			missing = append(missing, n)
		}
	}
	return found, missing, nil
}

// ===== helpers =====

func loadSecretOrErr(c *gin.Context, db *gorm.DB, id int64) (*model.Secret, bool) {
	var s model.Secret
	if err := db.WithContext(c.Request.Context()).First(&s, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	// org 校验 (M2 简化版): scope=org 必须是当前 org
	orgID, ok := activeOrg(c)
	if !ok {
		return nil, false
	}
	if s.Scope == "org" && s.ScopeID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not in active org"})
		return nil, false
	}
	_ = middleware.CtxUserIDKey // touch import
	return &s, true
}

func inOptionsSecret(v string, opts ...string) bool {
	for _, o := range opts {
		if v == o {
			return true
		}
	}
	return false
}
