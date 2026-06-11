package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/helios-cicd/helios/api/pkg/rbac"
)

// RBACConfig is the config for RBAC middleware
type RBACConfig struct {
	ResourceExtractor func(*gin.Context) string
	ActionExtractor   func(*gin.Context) string
	OrgIDExtractor    func(*gin.Context) string
}

// DefaultRBACConfig is the default RBAC config
var DefaultRBACConfig = RBACConfig{
	ResourceExtractor: defaultResourceExtractor,
	ActionExtractor:   defaultActionExtractor,
	OrgIDExtractor:    defaultOrgIDExtractor,
}

// RBAC returns a RBAC middleware
func RBAC(config ...RBACConfig) gin.HandlerFunc {
	cfg := DefaultRBACConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Extract resource, action, org ID
		resource := cfg.ResourceExtractor(c)
		action := cfg.ActionExtractor(c)
		orgID := cfg.OrgIDExtractor(c)

		// Enforce RBAC
		allowed, err := rbac.Enforce(userID.(string), orgID, resource, action)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		c.Next()
	}
}

func defaultResourceExtractor(c *gin.Context) string {
	path := c.Request.URL.Path
	// Remove /api/v1 prefix if present
	if strings.HasPrefix(path, "/api/v1/") {
		path = path[len("/api/v1/"):]
	}
	// Extract resource from path
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func defaultActionExtractor(c *gin.Context) string {
	method := c.Request.Method
	switch method {
	case http.MethodGet:
		return "read"
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "write"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}

func defaultOrgIDExtractor(c *gin.Context) string {
	// Try to get org ID from path param
	if orgID := c.Param("org_id"); orgID != "" {
		return orgID
	}
	// Try to get from header
	if orgID := c.GetHeader("X-Org-ID"); orgID != "" {
		return orgID
	}
	// Try to get from context
	if orgID, exists := c.Get("org_id"); exists {
		return orgID.(string)
	}
	// Return wildcard as fallback
	return "*"
}
