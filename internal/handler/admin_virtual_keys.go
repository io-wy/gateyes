package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/adminconsole"
)

type CreateVirtualKeyRequest struct {
	UserID           string   `json:"user_id"`
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
	identity, _ := middleware.Identity(c)
	items, err := h.consoleSvc.ListVirtualKeys(c.Request.Context(), identity, repository.VirtualKeyFilter{
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
	identity, _ := middleware.Identity(c)
	record, err := h.consoleSvc.GetVirtualKey(c.Request.Context(), identity, c.Param("id"))
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

	result, err := h.consoleSvc.CreateVirtualKey(c.Request.Context(), identity, adminconsole.CreateVirtualKeyInput{
		UserID:           req.UserID,
		APIKeyID:         req.APIKeyID,
		ProjectID:        req.ProjectID,
		Name:             req.Name,
		BudgetUSD:        req.BudgetUSD,
		BudgetPolicy:     req.BudgetPolicy,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		CallbackURL:      req.CallbackURL,
	})
	if err != nil {
		if errors.Is(err, adminconsole.ErrMissingUserID) {
			writeError(c, http.StatusBadRequest, CodeMissingRequiredField, err.Error())
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		if errors.Is(err, adminconsole.ErrExceededParent) {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}

	response := virtualKeyToResponse(*result.Record)
	response["secret"] = result.Secret
	response["token"] = result.Token
	h.recordAudit(c, "virtual_key.create", "virtual_key", result.Record.ID, req)
	writeJSON(c, http.StatusCreated, CodeOK, "", response)
}

func (h *AdminHandler) UpdateVirtualKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req UpdateVirtualKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	record, err := h.consoleSvc.UpdateVirtualKey(c.Request.Context(), identity, c.Param("id"), repository.UpdateVirtualKeyParams{
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
		if errors.Is(err, adminconsole.ErrExceededParent) {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	h.recordAudit(c, "virtual_key.update", "virtual_key", record.ID, req)
	h.invalidateAPIKeyCache(record.Key)
	writeOK(c, virtualKeyToResponse(*record))
}

func (h *AdminHandler) DeleteVirtualKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var key string
	if record, err := h.consoleSvc.GetVirtualKey(c.Request.Context(), identity, c.Param("id")); err == nil {
		key = record.Key
	}
	if err := h.consoleSvc.DeleteVirtualKey(c.Request.Context(), identity, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeVirtualKeyNotFound, "virtual key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	h.invalidateAPIKeyCache(key)
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
