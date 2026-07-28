package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
)

type CreateRoleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type UpdateRoleRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Permissions *[]string `json:"permissions"`
}

type RoleResponse struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSystem    bool     `json:"is_system"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func roleToResponse(role repository.RoleRecord) RoleResponse {
	return RoleResponse{
		ID:          role.ID,
		TenantID:    role.TenantID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    role.IsSystem,
		Permissions: role.Permissions,
		CreatedAt:   role.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   role.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *AdminHandler) CreateRole(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	var req CreateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		if t := c.Query("tenant_id"); t != "" {
			tenantID = t
		}
	}

	role, err := h.store.CreateRole(c.Request.Context(), repository.CreateRoleParams{
		TenantID:      tenantID,
		Name:          req.Name,
		Description:   req.Description,
		PermissionIDs: resolvePermissionIDs(c.Request.Context(), h.store, req.Permissions),
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}

	h.recordAudit(c, "role.create", "role", role.ID, req)
	writeOK(c, roleToResponse(*role))
}

func (h *AdminHandler) ListRoles(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = c.Query("tenant_id")
	}

	roles, err := h.store.ListRoles(c.Request.Context(), tenantID, repository.RoleFilter{IncludeSystem: true})
	if err != nil {
		writeInternalError(c, err)
		return
	}

	result := make([]RoleResponse, 0, len(roles))
	for _, role := range roles {
		result = append(result, roleToResponse(role))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetRole(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	role, err := h.store.GetRole(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "role not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	writeOK(c, roleToResponse(*role))
}

func (h *AdminHandler) UpdateRole(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	var req UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	var permIDs *[]string
	if req.Permissions != nil {
		ids := resolvePermissionIDs(c.Request.Context(), h.store, *req.Permissions)
		permIDs = &ids
	}

	role, err := h.store.UpdateRole(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateRoleParams{
		Name:          req.Name,
		Description:   req.Description,
		PermissionIDs: permIDs,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "role not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	h.recordAudit(c, "role.update", "role", role.ID, req)
	writeOK(c, roleToResponse(*role))
}

func (h *AdminHandler) DeleteRole(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	if err := h.store.DeleteRole(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "role not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	h.recordAudit(c, "role.delete", "role", c.Param("id"), gin.H{"role_id": c.Param("id")})
	writeOKMsg(c, "role deleted", nil)
}

func (h *AdminHandler) ListPermissions(c *gin.Context) {
	perms, err := h.store.ListPermissions(c.Request.Context())
	if err != nil {
		writeInternalError(c, err)
		return
	}

	result := make([]gin.H, 0, len(perms))
	for _, p := range perms {
		result = append(result, gin.H{
			"id":          p.ID,
			"code":        p.Code,
			"name":        p.Name,
			"description": p.Description,
		})
	}
	writeOK(c, result)
}

func resolvePermissionIDs(ctx context.Context, store repository.Store, codes []string) []string {
	if len(codes) == 0 {
		return nil
	}
	perms, err := store.ListPermissions(ctx)
	if err != nil {
		return nil
	}
	codeToID := make(map[string]string, len(perms))
	for _, p := range perms {
		codeToID[p.Code] = p.ID
	}
	ids := make([]string, 0, len(codes))
	for _, code := range codes {
		if id, ok := codeToID[code]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}
