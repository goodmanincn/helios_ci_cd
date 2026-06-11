//go:build oidc_wip
// +build oidc_wip

package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/pkg/oidc"
	"github.com/helios-cicd/helios/api/pkg/rbac"
)

// OIDCHandler handles OIDC authentication
type OIDCHandler struct {
	db          *gorm.DB
	oidcManager *oidc.Manager
	userRepo    *repository.UserRepository
	orgRepo     *repository.OrganizationRepository
}

// NewOIDCHandler creates a new OIDC handler
func NewOIDCHandler(db *gorm.DB, oidcManager *oidc.Manager) *OIDCHandler {
	return &OIDCHandler{
		db:          db,
		oidcManager: oidcManager,
		userRepo:    repository.NewUserRepository(db),
		orgRepo:     repository.NewOrganizationRepository(db),
	}
}

// LoginRequest represents a login request
type LoginRequest struct {
	OrgID       string `json:"org_id" binding:"required"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// Login starts the OIDC login flow
// @Summary OIDC login
// @Description Start OIDC authentication flow
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login request"
// @Success 200 {object} LoginResponse
// @Router /api/v1/auth/oidc/login [post]
func (h *OIDCHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get OIDC provider
	provider, ok := h.oidcManager.GetProvider(req.OrgID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC provider not found for organization"})
		return
	}

	// Generate state for CSRF protection
	state := oidc.GenerateState()

	// Store state with expiration
	// In production, use Redis with TTL
	// For now, we'll just return it

	// Generate auth URL
	authURL := provider.AuthCodeURL(state)

	c.JSON(http.StatusOK, LoginResponse{
		AuthURL: authURL,
		State:   state,
	})
}

// CallbackRequest represents a callback request
type CallbackRequest struct {
	Code  string `form:"code" binding:"required"`
	State string `form:"state" binding:"required"`
	OrgID string `form:"org_id" binding:"required"`
}

// Callback handles OIDC callback
// @Summary OIDC callback
// @Description Handle OIDC authentication callback
// @Tags auth
// @Accept x-www-form-urlencoded
// @Produce json
// @Param code query string true "Authorization code"
// @Param state query string true "State parameter"
// @Param org_id query string true "Organization ID"
// @Success 200 {object} TokenResponse
// @Router /api/v1/auth/oidc/callback [get]
func (h *OIDCHandler) Callback(c *gin.Context) {
	var req CallbackRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get OIDC provider
	provider, ok := h.oidcManager.GetProvider(req.OrgID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC provider not found for organization"})
		return
	}

	// Exchange code for token
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := provider.Exchange(ctx, req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to exchange code: " + err.Error()})
		return
	}

	// Verify ID token
	idToken, err := provider.VerifyIDToken(ctx, token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to verify token: " + err.Error()})
		return
	}

	// Get user info
	userInfo, err := provider.GetUserInfo(ctx, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user info: " + err.Error()})
		return
	}

	// Find or create user
	user, err := h.findOrCreateUser(c, userInfo, idToken.Issuer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process user: " + err.Error()})
		return
	}

	// Ensure user is in organization
	if err := h.ensureUserInOrg(c, user.ID, req.OrgID, userInfo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add user to organization: " + err.Error()})
		return
	}

	// TODO: Generate access and refresh tokens
	// For now, return a placeholder
	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"user":    user,
	})
}

// findOrCreateUser finds an existing user or creates a new one
func (h *OIDCHandler) findOrCreateUser(c *gin.Context, info *oidc.UserInfo, issuer string) (*model.User, error) {
	// Try to find user by external ID
	extID := issuer + ":" + info.Sub
	user, err := h.userRepo.FindByExternalID(c.Request.Context(), extID)
	if err == nil {
		return user, nil
	}

	// Try to find user by email
	user, err = h.userRepo.FindByEmail(c.Request.Context(), info.Email)
	if err == nil {
		// Update external ID if not set
		if user.ExternalID == "" {
			user.ExternalID = extID
			if err := h.userRepo.Update(c.Request.Context(), user); err != nil {
				return nil, err
			}
		}
		return user, nil
	}

	// Create new user
	user = &model.User{
		ID:         uuid.NewString(),
		Username:   info.Email, // Use email as username if name not available
		Email:      info.Email,
		ExternalID: extID,
	}
	if info.Name != "" {
		user.Username = info.Name
	}

	if err := h.userRepo.Create(c.Request.Context(), user); err != nil {
		return nil, err
	}

	return user, nil
}

// ensureUserInOrg ensures a user is in an organization
func (h *OIDCHandler) ensureUserInOrg(c *gin.Context, userID, orgID string, info *oidc.UserInfo) error {
	// Check if user is already in org
	exists, err := h.orgRepo.UserExistsInOrg(c.Request.Context(), orgID, userID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Add user to org with default role
	if err := h.orgRepo.AddUserToOrg(c.Request.Context(), orgID, userID); err != nil {
		return err
	}

	// Assign default role (Developer)
	_, err = rbac.AddRoleForUser(userID, rbac.RoleDeveloper, orgID)
	return err
}

// RegisterProviderRequest represents a request to register an OIDC provider
type RegisterProviderRequest struct {
	Issuer       string   `json:"issuer" binding:"required"`
	ClientID     string   `json:"client_id" binding:"required"`
	ClientSecret string   `json:"client_secret" binding:"required"`
	RedirectURL  string   `json:"redirect_url" binding:"required"`
	Scopes       []string `json:"scopes,omitempty"`
}

// RegisterProvider registers an OIDC provider for an organization
// @Summary Register OIDC provider
// @Description Register OIDC provider for organization
// @Tags auth
// @Accept json
// @Produce json
// @Param org_id path string true "Organization ID"
// @Param request body RegisterProviderRequest true "Provider configuration"
// @Success 200 {object} gin.H
// @Router /api/v1/orgs/{org_id}/oidc/providers [post]
func (h *OIDCHandler) RegisterProvider(c *gin.Context) {
	orgID := c.Param("org_id")

	var req RegisterProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg := oidc.Config{
		Issuer:       req.Issuer,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		RedirectURL:  req.RedirectURL,
		Scopes:       req.Scopes,
	}

	// TODO: Save configuration to database
	// For now, just register in memory
	if _, err := h.oidcManager.RegisterProvider(orgID, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register provider: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Provider registered successfully"})
}

// ListProviders lists OIDC providers for an organization
// @Summary List OIDC providers
// @Description List OIDC providers for organization
// @Tags auth
// @Accept json
// @Produce json
// @Param org_id path string true "Organization ID"
// @Success 200 {object} gin.H
// @Router /api/v1/orgs/{org_id}/oidc/providers [get]
func (h *OIDCHandler) ListProviders(c *gin.Context) {
	orgID := c.Param("org_id")

	_, ok := h.oidcManager.GetProvider(orgID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"providers": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Provider listing not fully implemented yet"})
}
