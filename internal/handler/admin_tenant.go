package handler

import (
	"context"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *AdminHandler) ListTenants(c *gin.Context) {
	tenants, err := h.store.ListTenants(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	result := make([]gin.H, 0, len(tenants))
	for _, item := range tenants {
		result = append(result, tenantToResponse(item))
	}
	writeOK(c, result)
}

type CreateTenantRequest struct {
	ID        string                          `json:"id"`
	Slug      string                          `json:"slug" binding:"required"`
	Name      string                          `json:"name"`
	BudgetUSD float64                         `json:"budget_usd"`
	Policy    *repository.ServicePolicyConfig `json:"policy"`
}

func (h *AdminHandler) CreateTenant(c *gin.Context) {
	var req CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenant, err := h.store.EnsureTenant(c.Request.Context(), repository.EnsureTenantParams{
		ID:        req.ID,
		Slug:      req.Slug,
		Name:      req.Name,
		Status:    repository.StatusActive,
		BudgetUSD: req.BudgetUSD,
		Policy:    req.Policy,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	if err := h.store.ReplaceTenantProviders(c.Request.Context(), tenant.ID, providerNames(h.providerMgr.List())); err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	h.recordAudit(c, "tenant.create", "tenant", tenant.ID, req)
	writeOK(c, tenantToResponse(*tenant))
}

func (h *AdminHandler) GetTenant(c *gin.Context) {
	tenant, err := h.store.GetTenant(c.Request.Context(), c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "tenant not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	providers, err := h.store.ListTenantProviders(c.Request.Context(), tenant.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	writeOK(c, gin.H{
		"tenant":    tenantToResponse(*tenant),
		"providers": providers,
	})
}

type UpdateTenantRequest struct {
	Name         *string                         `json:"name"`
	Status       *string                         `json:"status"`
	BudgetUSD    *float64                        `json:"budget_usd"`
	BudgetPolicy *string                         `json:"budget_policy"`
	Policy       *repository.ServicePolicyConfig `json:"policy"`
}

func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	var req UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenant, err := h.store.UpdateTenant(c.Request.Context(), c.Param("id"), repository.UpdateTenantParams{
		Name:         req.Name,
		Status:       req.Status,
		BudgetUSD:    req.BudgetUSD,
		BudgetPolicy: req.BudgetPolicy,
		Policy:       req.Policy,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "tenant not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	h.recordAudit(c, "tenant.update", "tenant", tenant.ID, req)
	writeOK(c, tenantToResponse(*tenant))
}

type ReplaceTenantProvidersRequest struct {
	Providers []string `json:"providers"`
}

func (h *AdminHandler) ReplaceTenantProviders(c *gin.Context) {
	var req ReplaceTenantProvidersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if !h.allProvidersExist(req.Providers) {
		writeError(c, http.StatusBadRequest, CodeBadRequest, "unknown provider in list")
		return
	}

	tenant, err := h.store.GetTenant(c.Request.Context(), c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "tenant not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	if err := h.store.ReplaceTenantProviders(c.Request.Context(), tenant.ID, req.Providers); err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	h.recordAudit(c, "tenant.replace_providers", "tenant", tenant.ID, req)
	writeOK(c, gin.H{
		"tenant_id": tenant.ID,
		"providers": req.Providers,
	})
}

func (h *AdminHandler) DeleteTenant(c *gin.Context) {
	if err := h.store.DeleteTenant(c.Request.Context(), c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "tenant not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	h.recordAudit(c, "tenant.delete", "tenant", c.Param("id"), gin.H{"tenant_id": c.Param("id")})
	writeOK(c, gin.H{"id": c.Param("id"), "deleted": true})
}

func (h *AdminHandler) allProvidersExist(names []string) bool {
	known := make(map[string]struct{})
	for _, providerItem := range h.providerMgr.List() {
		known[providerItem.Name()] = struct{}{}
	}
	for _, name := range names {
		if _, ok := known[name]; !ok {
			return false
		}
	}
	return true
}

func (h *AdminHandler) appendTenantProvider(ctx context.Context, tenantID, name string) error {
	names, err := h.store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, item := range names {
		if item == name {
			return nil
		}
	}
	names = append(names, name)
	return h.store.ReplaceTenantProviders(ctx, tenantID, names)
}

func (h *AdminHandler) removeTenantProvider(ctx context.Context, tenantID, name string) error {
	names, err := h.store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	filtered := make([]string, 0, len(names))
	for _, item := range names {
		if item != name {
			filtered = append(filtered, item)
		}
	}
	return h.store.ReplaceTenantProviders(ctx, tenantID, filtered)
}

func tenantToResponse(tenant repository.TenantRecord) gin.H {
	return gin.H{
		"id":            tenant.ID,
		"slug":          tenant.Slug,
		"name":          tenant.Name,
		"status":        tenant.Status,
		"budget_usd":    tenant.BudgetUSD,
		"spent_usd":     tenant.SpentUSD,
		"budget_policy": tenant.BudgetPolicy,
		"policy":        tenant.Policy,
		"created_at":    tenant.CreatedAt,
		"updated_at":    tenant.UpdatedAt,
	}
}
