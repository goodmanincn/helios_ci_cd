package rbac

import (
	"embed"
	"fmt"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

//go:embed model.conf
var modelFS embed.FS

// Enforcer is the global RBAC enforcer
var (
	enforcer *casbin.Enforcer
	once     sync.Once
)

// Role constants
const (
	RoleOrgOwner         = "org_owner"
	RoleAdmin            = "admin"
	RoleDeveloper        = "developer"
	RoleOperator         = "operator"
	RoleViewer           = "viewer"
	RoleApprover         = "approver"
	RoleProjectOwner     = "project_owner"
	RoleProjectMaintainer = "project_maintainer"
	RoleProjectMember    = "project_member"
)

// Init initializes the RBAC enforcer
func Init(gdb *gorm.DB) error {
	var err error
	once.Do(func() {
		var adapter *gormadapter.Adapter
		adapter, err = gormadapter.NewAdapterByDB(gdb)
		if err != nil {
			return
		}

		modelContent, readErr := modelFS.ReadFile("model.conf")
		if readErr != nil {
			err = readErr
			return
		}

		m, mErr := model.NewModelFromString(string(modelContent))
		if mErr != nil {
			err = mErr
			return
		}
		enforcer, err = casbin.NewEnforcer(m, adapter)
		if err != nil {
			return
		}

		err = enforcer.LoadPolicy()
	})
	return err
}

// GetEnforcer returns the global enforcer
func GetEnforcer() (*casbin.Enforcer, error) {
	if enforcer == nil {
		return nil, fmt.Errorf("rbac not initialized")
	}
	return enforcer, nil
}

// Enforce checks if a user has permission to perform an action on a resource in a domain
func Enforce(userID, orgID, resource, action string) (bool, error) {
	if enforcer == nil {
		return false, fmt.Errorf("rbac not initialized")
	}
	return enforcer.Enforce(userID, orgID, resource, action)
}

// AddRoleForUser adds a role to a user in a domain
func AddRoleForUser(userID, role, orgID string) (bool, error) {
	if enforcer == nil {
		return false, fmt.Errorf("rbac not initialized")
	}
	return enforcer.AddRoleForUserInDomain(userID, role, orgID)
}

// DeleteRoleForUser removes a role from a user in a domain
func DeleteRoleForUser(userID, role, orgID string) (bool, error) {
	if enforcer == nil {
		return false, fmt.Errorf("rbac not initialized")
	}
	return enforcer.DeleteRoleForUserInDomain(userID, role, orgID)
}

// GetRolesForUser gets all roles for a user in a domain
func GetRolesForUser(userID, orgID string) ([]string, error) {
	if enforcer == nil {
		return nil, fmt.Errorf("rbac not initialized")
	}
	return enforcer.GetRolesForUserInDomain(userID, orgID), nil
}

// AddPolicy adds a policy
func AddPolicy(role, orgID, resource, action string) (bool, error) {
	if enforcer == nil {
		return false, fmt.Errorf("rbac not initialized")
	}
	return enforcer.AddPolicy(role, orgID, resource, action)
}

// SeedBuiltinRoles seeds the built-in roles with default permissions
func SeedBuiltinRoles() error {
	if enforcer == nil {
		return fmt.Errorf("rbac not initialized")
	}

	// Define permissions for each role
	policies := getBuiltinPolicies()

	for _, p := range policies {
		exists, err := enforcer.HasPolicy(p.Role, p.OrgID, p.Resource, p.Action)
		if err != nil {
			return err
		}
		if !exists {
			_, err = enforcer.AddPolicy(p.Role, p.OrgID, p.Resource, p.Action)
			if err != nil {
				return err
			}
		}
	}

	return enforcer.SavePolicy()
}

type policy struct {
	Role     string
	OrgID    string
	Resource string
	Action   string
}

func getBuiltinPolicies() []policy {
	orgWildcard := "*"
	resources := []string{
		"orgs",
		"orgs/*",
		"projects",
		"projects/*",
		"pipelines",
		"pipelines/*",
		"runs",
		"runs/*",
		"users",
		"users/*",
		"roles",
		"roles/*",
		"secrets",
		"secrets/*",
		"clusters",
		"clusters/*",
		"audit",
		"audit/*",
	}

	actions := []string{"read", "write", "delete", "create"}

	var policies []policy

	// Org Owner has full access
	for _, res := range resources {
		for _, act := range actions {
			policies = append(policies, policy{
				Role:     RoleOrgOwner,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   act,
			})
		}
	}

	// Admin has almost full access except org deletion
	for _, res := range resources {
		if res == "orgs" {
			policies = append(policies, policy{
				Role:     RoleAdmin,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   "read",
			})
			policies = append(policies, policy{
				Role:     RoleAdmin,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   "write",
			})
			continue
		}
		for _, act := range actions {
			policies = append(policies, policy{
				Role:     RoleAdmin,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   act,
			})
		}
	}

	// Developer can manage projects, pipelines, runs
	devResources := []string{"projects", "projects/*", "pipelines", "pipelines/*", "runs", "runs/*", "secrets", "secrets/*"}
	for _, res := range devResources {
		for _, act := range actions {
			policies = append(policies, policy{
				Role:     RoleDeveloper,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   act,
			})
		}
	}
	// Read access to other resources
	policies = append(policies, policy{
		Role:     RoleDeveloper,
		OrgID:    orgWildcard,
		Resource: "orgs",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleDeveloper,
		OrgID:    orgWildcard,
		Resource: "orgs/*",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleDeveloper,
		OrgID:    orgWildcard,
		Resource: "clusters",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleDeveloper,
		OrgID:    orgWildcard,
		Resource: "clusters/*",
		Action:   "read",
	})

	// Operator can manage clusters and runs
	opResources := []string{"clusters", "clusters/*", "runs", "runs/*", "pipelines", "pipelines/*"}
	for _, res := range opResources {
		for _, act := range actions {
			policies = append(policies, policy{
				Role:     RoleOperator,
				OrgID:    orgWildcard,
				Resource: res,
				Action:   act,
			})
		}
	}
	// Read access to other resources
	policies = append(policies, policy{
		Role:     RoleOperator,
		OrgID:    orgWildcard,
		Resource: "orgs",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleOperator,
		OrgID:    orgWildcard,
		Resource: "orgs/*",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleOperator,
		OrgID:    orgWildcard,
		Resource: "projects",
		Action:   "read",
	})
	policies = append(policies, policy{
		Role:     RoleOperator,
		OrgID:    orgWildcard,
		Resource: "projects/*",
		Action:   "read",
	})

	// Viewer has read-only access
	for _, res := range resources {
		policies = append(policies, policy{
			Role:     RoleViewer,
			OrgID:    orgWildcard,
			Resource: res,
			Action:   "read",
		})
	}

	// Approver can approve deployments
	approverResources := []string{"runs", "runs/*", "pipelines", "pipelines/*", "projects", "projects/*"}
	for _, res := range approverResources {
		policies = append(policies, policy{
			Role:     RoleApprover,
			OrgID:    orgWildcard,
			Resource: res,
			Action:   "read",
		})
		policies = append(policies, policy{
			Role:     RoleApprover,
			OrgID:    orgWildcard,
			Resource: res,
			Action:   "write",
		})
	}

	return policies
}
