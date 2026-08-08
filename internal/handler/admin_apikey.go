package handler

import (
	"errors"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/adminconsole"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (h *AdminHandler) CreateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	result, err := h.consoleSvc.CreateAPIKey(c.Request.Context(), identity, adminconsole.CreateAPIKeyInput{
		UserID:           req.UserID,
		ProjectID:        req.ProjectID,
		BudgetUSD:        req.BudgetUSD,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedServices:  req.AllowedServices,
		ExpiresAt:        req.ExpiresAt,
	})
	if err != nil {
		if errors.Is(err, adminconsole.ErrMissingUserID) {
			writeError(c, http.StatusBadRequest, CodeMissingRequiredField, err.Error())
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	response := apiKeyToResponse(*result.Record)
	response["api_secret"] = result.Secret
	response["token"] = result.Token
	h.recordAudit(c, "api_key.create", "api_key", result.Record.ID, req)
	writeOK(c, response)
}

func (h *AdminHandler) ListAPIKeys(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	items, err := h.consoleSvc.ListAPIKeys(c.Request.Context(), identity, repository.APIKeyFilter{
		UserID:    c.Query("user_id"),
		ProjectID: c.Query("project_id"),
		Status:    c.Query("status"),
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, apiKeyToResponse(item))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	record, err := h.consoleSvc.GetAPIKey(c.Request.Context(), identity, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, apiKeyToResponse(*record))
}

type UpdateAPIKeyRequest struct {
	ProjectID        *string    `json:"project_id"`
	Status           *string    `json:"status"`
	BudgetUSD        *float64   `json:"budget_usd"`
	BudgetPolicy     *string    `json:"budget_policy"`
	RateLimitQPS     *int       `json:"rate_limit_qps"`
	AllowedModels    *[]string  `json:"allowed_models"`
	AllowedProviders *[]string  `json:"allowed_providers"`
	AllowedServices  *[]string  `json:"allowed_services"`
	ExpiresAt        *time.Time `json:"expires_at"`
}

func (h *AdminHandler) UpdateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.Status != nil && !validEntityStatus(*req.Status) {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid status")
		return
	}

	record, err := h.consoleSvc.UpdateAPIKey(c.Request.Context(), identity, c.Param("id"), repository.UpdateAPIKeyParams{
		ProjectID:        req.ProjectID,
		Status:           req.Status,
		BudgetUSD:        req.BudgetUSD,
		BudgetPolicy:     req.BudgetPolicy,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedServices:  req.AllowedServices,
		ExpiresAt:        req.ExpiresAt,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		if errors.Is(err, adminconsole.ErrForbidden) {
			writeError(c, http.StatusForbidden, CodeInsufficientRole, "tenant user cannot update key policy")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.invalidateAPIKeyCache(record.Key)
	h.invalidateAPIKeyIdentity(record.ID)
	writeOK(c, apiKeyToResponse(*record))
}

func (h *AdminHandler) RotateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	result, err := h.consoleSvc.RotateAPIKey(c.Request.Context(), identity, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	response := apiKeyToResponse(*result.Record)
	response["api_secret"] = result.Secret
	response["token"] = result.Token
	h.invalidateAPIKeyCache(result.OldKey, result.Record.Key)
	h.invalidateAPIKeyIdentity(result.Record.ID)
	h.recordAudit(c, "api_key.rotate", "api_key", result.Record.ID, gin.H{"api_key_id": result.Record.ID})
	writeOK(c, response)
}

func (h *AdminHandler) RevokeAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	record, err := h.consoleSvc.RevokeAPIKey(c.Request.Context(), identity, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.invalidateAPIKeyCache(record.Key)
	h.invalidateAPIKeyIdentity(record.ID)
	h.recordAudit(c, "api_key.revoke", "api_key", record.ID, gin.H{"api_key_id": record.ID})
	writeOK(c, apiKeyToResponse(*record))
}

type CreateUserRequest struct {
	TenantID     string   `json:"tenant_id"`
	ProjectID    string   `json:"project_id"`
	Name         string   `json:"name" binding:"required"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Quota        int      `json:"quota"`
	QPS          int      `json:"qps"`
	KeyBudgetUSD float64  `json:"key_budget_usd"`
	Models       []string `json:"models"`
}

func apiKeyToResponse(record repository.APIKeyRecord) gin.H {
	response := gin.H{
		"id":                record.ID,
		"tenant_id":         record.TenantID,
		"tenant_slug":       record.TenantSlug,
		"user_id":           record.UserID,
		"user_name":         record.UserName,
		"user_email":        record.UserEmail,
		"project_id":        record.ProjectID,
		"project_slug":      record.ProjectSlug,
		"api_key":           record.Key,
		"status":            record.Status,
		"budget_usd":        record.BudgetUSD,
		"spent_usd":         record.SpentUSD,
		"budget_policy":     record.BudgetPolicy,
		"rate_limit_qps":    record.RateLimitQPS,
		"allowed_models":    record.AllowedModels,
		"allowed_providers": record.AllowedProviders,
		"allowed_services":  record.AllowedServices,
		"created_at":        record.CreatedAt,
		"updated_at":        record.UpdatedAt,
	}
	if record.LastUsedAt != nil {
		response["last_used_at"] = *record.LastUsedAt
	}
	if record.RevokedAt != nil {
		response["revoked_at"] = *record.RevokedAt
	}
	if record.ExpiresAt != nil {
		response["expires_at"] = *record.ExpiresAt
	}
	if record.RotatedAt != nil {
		response["rotated_at"] = *record.RotatedAt
	}
	response["rotation_reminder_sent"] = record.RotationReminderSent
	return response
}
