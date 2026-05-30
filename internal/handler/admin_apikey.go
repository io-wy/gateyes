package handler

import (
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
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

	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	user, err := h.store.GetUser(c.Request.Context(), tenantID, req.UserID)
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	record, err := h.store.CreateAPIKey(c.Request.Context(), repository.CreateAPIKeyParams{
		UserID:           user.ID,
		ProjectID:        req.ProjectID,
		Key:              apiKey,
		SecretHash:       repository.HashSecret(apiSecret),
		Status:           repository.StatusActive,
		BudgetUSD:        req.BudgetUSD,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedServices:  req.AllowedServices,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	response := apiKeyToResponse(*record)
	response["api_secret"] = apiSecret
	response["token"] = record.Key + ":" + apiSecret
	h.recordAudit(c, "api_key.create", "api_key", record.ID, req)
	writeOK(c, response)
}

func (h *AdminHandler) ListAPIKeys(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	items, err := h.store.ListAPIKeys(c.Request.Context(), tenantID, repository.APIKeyFilter{
		UserID:    c.Query("user_id"),
		ProjectID: c.Query("project_id"),
		Status:    c.Query("status"),
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
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
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	record, err := h.store.GetAPIKey(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, apiKeyToResponse(*record))
}

type UpdateAPIKeyRequest struct {
	ProjectID        *string   `json:"project_id"`
	Status           *string   `json:"status"`
	BudgetUSD        *float64  `json:"budget_usd"`
	BudgetPolicy     *string   `json:"budget_policy"`
	RateLimitQPS     *int      `json:"rate_limit_qps"`
	AllowedModels    *[]string `json:"allowed_models"`
	AllowedProviders *[]string `json:"allowed_providers"`
	AllowedServices  *[]string `json:"allowed_services"`
}

func (h *AdminHandler) UpdateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.Status != nil && !validEntityStatus(*req.Status) {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid status")
		return
	}

	record, err := h.store.UpdateAPIKey(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateAPIKeyParams{
		ProjectID:        req.ProjectID,
		Status:           req.Status,
		BudgetUSD:        req.BudgetUSD,
		BudgetPolicy:     req.BudgetPolicy,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedServices:  req.AllowedServices,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, apiKeyToResponse(*record))
}

func (h *AdminHandler) RotateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	newKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	newSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	record, err := h.store.RotateAPIKey(c.Request.Context(), tenantID, c.Param("id"), repository.RotateAPIKeyParams{
		NewKey:        newKey,
		NewSecretHash: repository.HashSecret(newSecret),
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	response := apiKeyToResponse(*record)
	response["api_secret"] = newSecret
	response["token"] = record.Key + ":" + newSecret
	h.recordAudit(c, "api_key.rotate", "api_key", record.ID, gin.H{"api_key_id": record.ID})
	writeOK(c, response)
}

func (h *AdminHandler) RevokeAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	status := repository.StatusRevoked
	now := time.Now().UTC()
	record, err := h.store.UpdateAPIKey(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateAPIKeyParams{
		Status:    &status,
		RevokedAt: &now,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeAPIKeyNotFound, "api key not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
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
	return response
}

