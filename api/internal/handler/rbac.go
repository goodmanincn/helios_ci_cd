package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/helios-cicd/helios/api/pkg/rbac"
)

// RoleHandler handles role-related requests
type RoleHandler struct{}

// NewRoleHandler creates a new RoleHandler
func NewRoleHandler() *RoleHandler {
	return &RoleHandler{}
}

// ListRoles lists all available roles
// @Summary List roles
// @Description Get all available roles
// @Tags roles
// @Accept json
// @Produce json
// @Success 200 {array} string
// @Router /api/v1/roles [get]
func (h *RoleHandler) ListRoles(c *gin.Context) {
	roles := []string{
		rbac.RoleOrgOwner,
		rbac.RoleAdmin,
		rbac.RoleDeveloper,
		rbac.RoleOperator,
		rbac.RoleViewer,
		rbac.RoleApprover,
		rbac.RoleProjectOwner,
		rbac.RoleProjectMaintainer,
		rbac.RoleProjectMember,
	}

	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

// GetUserRoles gets roles for a user
// @Summary Get user roles
// @Description Get roles assigned to a user
// @Tags roles
// @Accept json
// @Produce json
// @Param org_id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Success 200 {array} string
// @Router /api/v1/orgs/:org_id/users/:user_id/roles [get]
func (h *RoleHandler) GetUserRoles(c *gin.Context) {
	orgID := c.Param("org_id")
	userID := c.Param("user_id")

	roles, err := rbac.GetRolesForUser(userID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_id": userID, "org_id": orgID, "roles": roles})
}

// AssignRole assigns a role to a user
// @Summary Assign role
// @Description Assign a role to a user in an organization
// @Tags roles
// @Accept json
// @Produce json
// @Param org_id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Param body body AssignRoleRequest true "Role assignment request"
// @Success 200 {object} gin.H
// @Router /api/v1/orgs/:org_id/users/:user_id/roles [post]
func (h *RoleHandler) AssignRole(c *gin.Context) {
	orgID := c.Param("org_id")
	userID := c.Param("user_id")

	var req AssignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ok, err := rbac.AddRoleForUser(userID, req.Role, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role already assigned"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role assigned successfully"})
}

// RemoveRole removes a role from a user
// @Summary Remove role
// @Description Remove a role from a user in an organization
// @Tags roles
// @Accept json
// @Produce json
// @Param org_id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Param role path string true "Role"
// @Success 200 {object} gin.H
// @Router /api/v1/orgs/:org_id/users/:user_id/roles/:role [delete]
func (h *RoleHandler) RemoveRole(c *gin.Context) {
	orgID := c.Param("org_id")
	userID := c.Param("user_id")
	role := c.Param("role")

	ok, err := rbac.DeleteRoleForUser(userID, role, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role removed successfully"})
}

// AssignRoleRequest is the request body for assigning a role
type AssignRoleRequest struct {
	Role string `json:"role" binding:"required"`
}
