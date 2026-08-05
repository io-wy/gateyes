package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

// Permission is a fine-grained action identifier used for RBAC.
type Permission string

const (
	PermProviderRead  Permission = "provider:read"
	PermProviderWrite Permission = "provider:write"

	PermAPIKeyRead  Permission = "api_key:read"
	PermAPIKeyWrite Permission = "api_key:write"

	PermUserRead  Permission = "user:read"
	PermUserWrite Permission = "user:write"

	PermTenantRead  Permission = "tenant:read"
	PermTenantWrite Permission = "tenant:write"

	PermProjectRead  Permission = "project:read"
	PermProjectWrite Permission = "project:write"

	PermServiceRead  Permission = "service:read"
	PermServiceWrite Permission = "service:write"

	PermVirtualKeyRead  Permission = "virtual_key:read"
	PermVirtualKeyWrite Permission = "virtual_key:write"

	PermUsageRead    Permission = "usage:read"
	PermResponseRead Permission = "response:read"
	PermBudgetRead   Permission = "budget:read"
	PermAuditRead    Permission = "audit:read"
	PermConfigWrite  Permission = "config:write"
)

// rolePermissions maps each role to its allowed permissions.
// Kept as a fallback when the database-backed RBAC service is unavailable.
var rolePermissions = map[string][]Permission{
	repository.RoleSuperAdmin: {
		PermProviderRead, PermProviderWrite,
		PermAPIKeyRead, PermAPIKeyWrite,
		PermUserRead, PermUserWrite,
		PermTenantRead, PermTenantWrite,
		PermProjectRead, PermProjectWrite,
		PermServiceRead, PermServiceWrite,
		PermVirtualKeyRead, PermVirtualKeyWrite,
		PermUsageRead, PermResponseRead, PermBudgetRead, PermAuditRead, PermConfigWrite,
	},
	repository.RoleTenantAdmin: {
		PermProviderRead, PermProviderWrite,
		PermAPIKeyRead, PermAPIKeyWrite,
		PermUserRead, PermUserWrite,
		PermTenantRead, PermTenantWrite,
		PermProjectRead, PermProjectWrite,
		PermServiceRead, PermServiceWrite,
		PermVirtualKeyRead, PermVirtualKeyWrite,
		PermUsageRead, PermResponseRead, PermBudgetRead, PermAuditRead, PermConfigWrite,
	},
	repository.RoleTenantUser: {
		PermAPIKeyRead, PermAPIKeyWrite,
		PermServiceRead,
		PermVirtualKeyRead, PermVirtualKeyWrite,
		PermUsageRead, PermResponseRead,
	},
}

// PermissionsForRole returns the legacy fallback permissions for a role.
func PermissionsForRole(role string) []string {
	perms := rolePermissions[role]
	result := make([]string, 0, len(perms))
	for _, perm := range perms {
		result = append(result, string(perm))
	}
	return result
}

// RequirePermission returns a gin middleware that checks whether the
// authenticated identity has the given permission. It uses the database-backed
// RBAC service when available, falling back to the legacy hard-coded map.
func (m *Middleware) RequirePermission(perm Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok || identity == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    40001,
				"success": false,
				"message": "invalid API key",
				"data":    nil,
			})
			return
		}

		// Prefer database-backed RBAC; fall back to legacy map on failure.
		allowed := false
		if m.rbac != nil && identity.UserID != "" {
			allowed = m.rbac.HasPermission(c.Request.Context(), identity.UserID, string(perm))
		}
		if !allowed {
			perms := rolePermissions[identity.Role]
			allowed = slices.Contains(perms, perm)
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    40101,
				"success": false,
				"message": "insufficient role",
				"data":    nil,
			})
			return
		}
		c.Next()
	}
}
