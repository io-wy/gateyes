package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
)

type CreateVirtualKeyRequest struct {
	UserID           string   `json:"user_id" binding:"required"`
	APIKeyID         string   `json:"api_key_id" binding:"required"`
	ProjectID        string   `json:"project_id"`
	Name             string   `json:"name"`
	BudgetUSD        float64  `json:"budget_usd"`
	BudgetPolicy     string   `json:"budget_policy"`
	RateLimitQPS     int      `json:"rate_limit_qps"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedProviders []string `json:"allowed_providers"`
	CallbackURL      string   `json:"callback_url"`
}

type UpdateVirtualKeyRequest struct {
	Name             *string   `json:"name"`
	Status           *string   `json:"status"`
	BudgetUSD        *float64  `json:"budget_usd"`
	BudgetPolicy     *string   `json:"budget_policy"`
	RateLimitQPS     *int      `json:"rate_limit_qps"`
	AllowedModels    *[]string `json:"allowed_models"`
	AllowedProviders *[]string `json:"allowed_providers"`
	CallbackURL      *string   `json:"callback_url"`
}

func (h *AdminHandler) ListVirtualKeys(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	items, err := h.store.ListVirtualKeys(c.Request.Context(), tenantID, repository.VirtualKeyFilter{
		UserID:    c.Query("user_id"),
		ProjectID: c.Query("project_id"),
		APIKeyID:  c.Query("api_key_id"),
		Status:    c.Query("status"),
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, virtualKeyToResponse(item))
	}
	writeList(c, result, int64(len(items)))
}

func (h *AdminHandler) GetVirtualKey(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	record, err := h.store.GetVirtualKey(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeVirtualKeyNotFound, "virtual key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	writeOK(c, virtualKeyToResponse(*record))
}

func (h *AdminHandler) CreateVirtualKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var req CreateVirtualKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	vk, err := repository.GenerateToken("vk-", 8)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	vkSecret, err := repository.GenerateToken("vs-", 16)
	if err != nil {
		writeInternalError(c, err)
		return
	}

	record, err := h.store.CreateVirtualKey(c.Request.Context(), repository.CreateVirtualKeyParams{
		TenantID:         tenantID,
		UserID:           req.UserID,
		APIKeyID:         req.APIKeyID,
		ProjectID:        req.ProjectID,
		Name:             req.Name,
		Key:              vk,
		SecretHash:       repository.HashSecret(vkSecret),
		BudgetUSD:        req.BudgetUSD,
		BudgetPolicy:     req.BudgetPolicy,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		CallbackURL:      req.CallbackURL,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}

	response := virtualKeyToResponse(*record)
	response["secret"] = vkSecret
	response["token"] = record.Key + ":" + vkSecret
	h.recordAudit(c, "virtual_key.create", "virtual_key", record.ID, req)
	writeJSON(c, http.StatusCreated, CodeOK, "", response)
}

func (h *AdminHandler) UpdateVirtualKey(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	var req UpdateVirtualKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	record, err := h.store.UpdateVirtualKey(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateVirtualKeyParams{
		Name:             req.Name,
		Status:           req.Status,
		BudgetUSD:        req.BudgetUSD,
		BudgetPolicy:     req.BudgetPolicy,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		CallbackURL:      req.CallbackURL,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeVirtualKeyNotFound, "virtual key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	h.recordAudit(c, "virtual_key.update", "virtual_key", record.ID, req)
	writeOK(c, virtualKeyToResponse(*record))
}

func (h *AdminHandler) DeleteVirtualKey(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	if err := h.store.DeleteVirtualKey(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeVirtualKeyNotFound, "virtual key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	h.recordAudit(c, "virtual_key.delete", "virtual_key", c.Param("id"), nil)
	writeOKMsg(c, "virtual key deleted", gin.H{"deleted": true})
}

func virtualKeyToResponse(record repository.VirtualKeyRecord) gin.H {
	resp := gin.H{
		"id":                record.ID,
		"tenant_id":         record.TenantID,
		"project_id":        record.ProjectID,
		"user_id":           record.UserID,
		"api_key_id":        record.APIKeyID,
		"name":              record.Name,
		"key":               record.Key,
		"status":            record.Status,
		"budget_usd":        record.BudgetUSD,
		"spent_usd":         record.SpentUSD,
		"budget_policy":     record.BudgetPolicy,
		"rate_limit_qps":    record.RateLimitQPS,
		"allowed_models":    record.AllowedModels,
		"allowed_providers": record.AllowedProviders,
		"metadata":          record.Metadata,
		"callback_url":      record.CallbackURL,
		"created_at":        record.CreatedAt.Format(time.RFC3339),
		"updated_at":        record.UpdatedAt.Format(time.RFC3339),
	}
	if record.ExpiresAt != nil {
		resp["expires_at"] = *record.ExpiresAt
	}
	if record.RevokedAt != nil {
		resp["revoked_at"] = *record.RevokedAt
	}
	return resp
}
