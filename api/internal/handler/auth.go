// Package handler HTTP 路由层 — 把请求参数翻译给 service,响应序列化。
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/authstore"
	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/service"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

// AuthHandler 暴露 /auth/* 端点。
type AuthHandler struct {
	users     *service.UserService
	issuer    *heliosjwt.Issuer
	authstore *authstore.Store
	db        *gorm.DB
}

func NewAuthHandler(users *service.UserService, issuer *heliosjwt.Issuer, store *authstore.Store, db *gorm.DB) *AuthHandler {
	return &AuthHandler{users: users, issuer: issuer, authstore: store, db: db}
}

// Register 把路由挂到一个 gin.RouterGroup。authMW 用于受保护端点。
func (h *AuthHandler) Register(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	rg.POST("/login", h.login)
	rg.POST("/refresh", h.refresh)

	prot := rg.Group("")
	prot.Use(authMW)
	prot.POST("/logout", h.logout)
	prot.GET("/me", h.me)
}

// ===== handlers =====

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type tokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         userView  `json:"user"`
}

type userView struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

func toUserView(u *model.User) userView {
	return userView{ID: u.ID, Username: u.Username, Email: u.Email, DisplayName: u.DisplayName}
}

func (h *AuthHandler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "message": err.Error()})
		return
	}
	u, err := h.users.VerifyPassword(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrUserInactive) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials", "message": "用户名或密码错误"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login_failed", "message": err.Error()})
		return
	}

	pair, err := h.issuePair(c.Request.Context(), u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue_failed", "message": err.Error()})
		return
	}

	// 异步更新 last_login_at,不阻塞
	go func(uid int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.users.TouchLastLogin(ctx, uid)
	}(u.ID)

	c.JSON(http.StatusOK, pair)
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (h *AuthHandler) refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "message": err.Error()})
		return
	}
	claims, err := h.issuer.Parse(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_refresh", "message": err.Error()})
		return
	}
	if claims.TokenType != heliosjwt.TokenTypeRefresh {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "wrong_token_type", "message": "expected refresh token"})
		return
	}
	// 一次性消费 (rotate)
	uid, ok, err := h.authstore.ConsumeRefresh(c.Request.Context(), claims.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "store_error", "message": err.Error()})
		return
	}
	if !ok || uid != claims.UserID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh_consumed", "message": "refresh token 已使用或不存在"})
		return
	}
	u, err := h.users.GetByID(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_gone", "message": err.Error()})
		return
	}
	pair, err := h.issuePair(c.Request.Context(), u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue_failed", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pair)
}

func (h *AuthHandler) logout(c *gin.Context) {
	cl, ok := middleware.ClaimsFrom(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	// 拉黑当前 access token
	_ = h.authstore.BlacklistAccess(c.Request.Context(), cl.ID, cl.ExpiresAt.Time)
	// 客户端可顺便传 refresh_token 主动撤销;不传也可以靠自然过期
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.RefreshToken != "" {
		if rc, err := h.issuer.Parse(req.RefreshToken); err == nil {
			_ = h.authstore.RevokeRefresh(c.Request.Context(), rc.ID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) me(c *gin.Context) {
	cl, ok := middleware.ClaimsFrom(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	u, err := h.users.GetByID(c.Request.Context(), cl.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_gone", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":  toUserView(u),
		"roles": cl.Roles,
		"orgs":  cl.OrgIDs,
		"jti":   cl.ID,
	})
}

// ===== helpers =====

func (h *AuthHandler) issuePair(ctx context.Context, u *model.User) (*tokenPair, error) {
	// 取用户所属 org + role (简化:只列 owner/member,不查 RBAC)
	type row struct {
		OrgID int64
		Role  string
	}
	var rows []row
	_ = h.db.WithContext(ctx).Table("org_members").
		Select("org_id, role").
		Where("user_id = ?", u.ID).
		Scan(&rows).Error
	orgs := make([]int64, 0, len(rows))
	roles := make([]string, 0, len(rows))
	for _, r := range rows {
		orgs = append(orgs, r.OrgID)
		roles = append(roles, r.Role)
	}

	access, _, exp, err := h.issuer.IssueAccess(u.ID, u.Username, orgs, roles)
	if err != nil {
		return nil, err
	}
	refresh, rJTI, rExp, err := h.issuer.IssueRefresh(u.ID, u.Username)
	if err != nil {
		return nil, err
	}
	if err := h.authstore.RegisterRefresh(ctx, rJTI, u.ID, rExp); err != nil {
		return nil, err
	}
	return &tokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresAt:    exp,
		User:         toUserView(u),
	}, nil
}
