package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
)

type AdminHandler struct {
	store       repository.Store
	providerMgr *provider.Manager
	catalogSvc  *catalog.Service
	startedAt   time.Time
}

func NewAdminHandler(store repository.Store, providerMgr *provider.Manager, catalogSvc *catalog.Service) *AdminHandler {
	return &AdminHandler{
		store:       store,
		providerMgr: providerMgr,
		catalogSvc:  catalogSvc,
		startedAt:   time.Now(),
	}
}

func (h *AdminHandler) GetProviders(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": h.providerResponses(c, tenantID)})
}

func (h *AdminHandler) GetProvider(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	providers := h.providerResponses(c, tenantID)
	for _, item := range providers {
		if item["name"] == c.Param("name") {
			c.JSON(http.StatusOK, gin.H{"data": item})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
}

func (h *AdminHandler) GetProviderStats(c *gin.Context) {
	h.GetProvider(c)
}

type UpdateProviderRequest struct {
	Enabled                  *bool   `json:"enabled"`
	Drain                    *bool   `json:"drain"`
	HealthStatus             *string `json:"health_status"`
	RoutingWeight            *int    `json:"routing_weight"`
	SupportsChat             *bool   `json:"supports_chat"`
	SupportsResponses        *bool   `json:"supports_responses"`
	SupportsMessages         *bool   `json:"supports_messages"`
	SupportsStream           *bool   `json:"supports_stream"`
	SupportsTools            *bool   `json:"supports_tools"`
	SupportsImages           *bool   `json:"supports_images"`
	SupportsStructuredOutput *bool   `json:"supports_structured_output"`
	SupportsLongContext      *bool   `json:"supports_long_context"`
}

func (h *AdminHandler) UpdateProvider(c *gin.Context) {
	var req UpdateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HealthStatus != nil && !validProviderHealthStatus(*req.HealthStatus) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid health_status"})
		return
	}

	record, err := h.store.UpdateProviderRegistry(c.Request.Context(), c.Param("name"), repository.UpdateProviderRegistryParams{
		Enabled:                  req.Enabled,
		Drain:                    req.Drain,
		HealthStatus:             req.HealthStatus,
		RoutingWeight:            req.RoutingWeight,
		SupportsChat:             req.SupportsChat,
		SupportsResponses:        req.SupportsResponses,
		SupportsMessages:         req.SupportsMessages,
		SupportsStream:           req.SupportsStream,
		SupportsTools:            req.SupportsTools,
		SupportsImages:           req.SupportsImages,
		SupportsStructuredOutput: req.SupportsStructuredOutput,
		SupportsLongContext:      req.SupportsLongContext,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{*record})
	h.recordAudit(c, "provider.update", "provider", record.Name, req)
	c.JSON(http.StatusOK, gin.H{"data": providerRegistryToResponse(*record)})
}

type CreateAPIKeyRequest struct {
	UserID           string   `json:"user_id" binding:"required"`
	ProjectID        string   `json:"project_id"`
	BudgetUSD        float64  `json:"budget_usd"`
	RateLimitQPS     int      `json:"rate_limit_qps"`
	AllowedModels    []string `json:"allowed_models"`
	AllowedProviders []string `json:"allowed_providers"`
	AllowedServices  []string `json:"allowed_services"`
}

func (h *AdminHandler) CreateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	user, err := h.store.GetUser(c.Request.Context(), tenantID, req.UserID)
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := apiKeyToResponse(*record)
	response["api_secret"] = apiSecret
	response["token"] = record.Key + ":" + apiSecret
	h.recordAudit(c, "api_key.create", "api_key", record.ID, req)
	c.JSON(http.StatusCreated, gin.H{"data": response})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, apiKeyToResponse(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
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
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apiKeyToResponse(*record)})
}

type UpdateAPIKeyRequest struct {
	ProjectID        *string   `json:"project_id"`
	Status           *string   `json:"status"`
	BudgetUSD        *float64  `json:"budget_usd"`
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Status != nil && !validEntityStatus(*req.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	record, err := h.store.UpdateAPIKey(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateAPIKeyParams{
		ProjectID:        req.ProjectID,
		Status:           req.Status,
		BudgetUSD:        req.BudgetUSD,
		RateLimitQPS:     req.RateLimitQPS,
		AllowedModels:    req.AllowedModels,
		AllowedProviders: req.AllowedProviders,
		AllowedServices:  req.AllowedServices,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apiKeyToResponse(*record)})
}

func (h *AdminHandler) RotateAPIKey(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	newKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	newSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	record, err := h.store.RotateAPIKey(c.Request.Context(), tenantID, c.Param("id"), repository.RotateAPIKeyParams{
		NewKey:        newKey,
		NewSecretHash: repository.HashSecret(newSecret),
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := apiKeyToResponse(*record)
	response["api_secret"] = newSecret
	response["token"] = record.Key + ":" + newSecret
	h.recordAudit(c, "api_key.rotate", "api_key", record.ID, gin.H{"api_key_id": record.ID})
	c.JSON(http.StatusOK, gin.H{"data": response})
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
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.recordAudit(c, "api_key.revoke", "api_key", record.ID, gin.H{"api_key_id": record.ID})
	c.JSON(http.StatusOK, gin.H{"data": apiKeyToResponse(*record)})
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

func (h *AdminHandler) CreateUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID, ok := h.resolveTargetTenant(c, identity, req.TenantID)
	if !ok {
		return
	}
	if _, err := h.store.GetTenant(c.Request.Context(), tenantID); err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	role := req.Role
	if role == "" {
		role = repository.RoleTenantUser
	}
	if role == repository.RoleSuperAdmin && identity.Role != repository.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	quota := req.Quota
	if quota == 0 {
		quota = -1
	}

	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	user, err := h.store.CreateUser(c.Request.Context(), repository.CreateUserParams{
		TenantID:     tenantID,
		ProjectID:    req.ProjectID,
		Name:         req.Name,
		Email:        req.Email,
		Role:         role,
		Quota:        quota,
		QPS:          req.QPS,
		KeyBudgetUSD: req.KeyBudgetUSD,
		Models:       req.Models,
		Status:       repository.StatusActive,
		APIKey:       apiKey,
		SecretHash:   repository.HashSecret(apiSecret),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"id":             user.ID,
		"tenant_id":      user.TenantID,
		"tenant_slug":    user.TenantSlug,
		"api_key":        user.APIKey,
		"api_secret":     apiSecret,
		"token":          user.APIKey + ":" + apiSecret,
		"name":           user.Name,
		"email":          user.Email,
		"role":           user.Role,
		"quota":          user.Quota,
		"used":           user.Used,
		"remaining":      remaining(user),
		"qps":            user.QPS,
		"project_id":     user.ProjectID,
		"key_budget_usd": user.KeyBudgetUSD,
		"key_spent_usd":  user.KeySpentUSD,
		"models":         user.Models,
		"status":         user.Status,
		"created_at":     user.CreatedAt,
	}})
	h.recordAudit(c, "user.create", "user", user.ID, req)
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	users, err := h.store.ListUsers(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]gin.H, 0, len(users))
	for _, user := range users {
		result = append(result, userToResponse(user))
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AdminHandler) GetUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	user, err := h.store.GetUser(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": userToResponse(*user)})
}

type UpdateUserRequest struct {
	Role         *string   `json:"role"`
	Quota        *int      `json:"quota"`
	QPS          *int      `json:"qps"`
	ProjectID    *string   `json:"project_id"`
	KeyBudgetUSD *float64  `json:"key_budget_usd"`
	Models       *[]string `json:"models"`
	Status       *string   `json:"status"`
}

func (h *AdminHandler) UpdateUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role != nil && *req.Role == repository.RoleSuperAdmin && identity.Role != repository.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	user, err := h.store.UpdateUser(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateUserParams{
		Role:         req.Role,
		Quota:        req.Quota,
		QPS:          req.QPS,
		ProjectID:    req.ProjectID,
		KeyBudgetUSD: req.KeyBudgetUSD,
		Models:       req.Models,
		Status:       req.Status,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, "user.update", "user", user.ID, req)
	c.JSON(http.StatusOK, gin.H{"data": userToResponse(*user)})
}

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	if err := h.store.DeleteUser(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, "user.delete", "user", c.Param("id"), gin.H{"user_id": c.Param("id")})
	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func (h *AdminHandler) ResetUserUsage(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	user, err := h.store.ResetUserUsage(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"id":        user.ID,
		"used":      user.Used,
		"remaining": remaining(user),
	}})
	h.recordAudit(c, "user.reset_usage", "user", user.ID, gin.H{"user_id": user.ID})
}

func (h *AdminHandler) GetUserUsage(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	user, err := h.store.GetUser(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取使用趋势（默认7天）
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	trend, err := h.store.GetUserUsageTrend(c.Request.Context(), user.TenantID, user.ID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 计算使用率
	usagePercent := 0.0
	if user.Quota > 0 {
		usagePercent = float64(user.Used) / float64(user.Quota) * 100
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"user": gin.H{
			"id":             user.ID,
			"name":           user.Name,
			"quota":          user.Quota,
			"used":           user.Used,
			"remaining":      remaining(user),
			"usage_percent":  usagePercent,
			"project_id":     user.ProjectID,
			"key_budget_usd": user.KeyBudgetUSD,
			"key_spent_usd":  user.KeySpentUSD,
		},
		"trend": trend,
	}})
}

func (h *AdminHandler) Dashboard(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	userStats, err := h.store.Stats(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	usageStats, err := h.store.GetUsageSummary(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"providers": gin.H{
			"list":             h.providerResponses(c, tenantID),
			"total_requests":   usageStats.TotalRequests,
			"success_requests": usageStats.SuccessRequests,
			"failed_requests":  usageStats.FailedRequests,
			"total_tokens":     usageStats.TotalTokens,
			"total_cost_usd":   usageStats.TotalCostUSD,
			"avg_latency_ms":   usageStats.AvgLatencyMs,
			"error_rate":       errorRate(usageStats.TotalRequests, usageStats.FailedRequests),
		},
		"users": gin.H{
			"total_users":   userStats.TotalUsers,
			"active_users":  userStats.ActiveUsers,
			"total_quota":   userStats.TotalQuota,
			"total_used":    userStats.TotalUsed,
			"usage_percent": usagePercent(userStats),
		},
		"uptime": time.Since(h.startedAt).String(),
	}})
}

func (h *AdminHandler) ListTenants(c *gin.Context) {
	tenants, err := h.store.ListTenants(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tenants})
}

type CreateTenantRequest struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug" binding:"required"`
	Name      string  `json:"name"`
	BudgetUSD float64 `json:"budget_usd"`
}

func (h *AdminHandler) CreateTenant(c *gin.Context) {
	var req CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenant, err := h.store.EnsureTenant(c.Request.Context(), repository.EnsureTenantParams{
		ID:        req.ID,
		Slug:      req.Slug,
		Name:      req.Name,
		Status:    repository.StatusActive,
		BudgetUSD: req.BudgetUSD,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.ReplaceTenantProviders(c.Request.Context(), tenant.ID, providerNames(h.providerMgr.List())); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, "tenant.create", "tenant", tenant.ID, req)
	c.JSON(http.StatusCreated, gin.H{"data": tenant})
}

func (h *AdminHandler) GetTenant(c *gin.Context) {
	tenant, err := h.store.GetTenant(c.Request.Context(), c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	providers, err := h.store.ListTenantProviders(c.Request.Context(), tenant.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"tenant":    tenant,
		"providers": providers,
	}})
}

type UpdateTenantRequest struct {
	Name      *string  `json:"name"`
	Status    *string  `json:"status"`
	BudgetUSD *float64 `json:"budget_usd"`
}

func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	var req UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenant, err := h.store.UpdateTenant(c.Request.Context(), c.Param("id"), repository.UpdateTenantParams{
		Name:      req.Name,
		Status:    req.Status,
		BudgetUSD: req.BudgetUSD,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, "tenant.update", "tenant", tenant.ID, req)
	c.JSON(http.StatusOK, gin.H{"data": tenant})
}

type ReplaceTenantProvidersRequest struct {
	Providers []string `json:"providers"`
}

func (h *AdminHandler) ReplaceTenantProviders(c *gin.Context) {
	var req ReplaceTenantProvidersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.allProvidersExist(req.Providers) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown provider in list"})
		return
	}

	tenant, err := h.store.GetTenant(c.Request.Context(), c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.ReplaceTenantProviders(c.Request.Context(), tenant.ID, req.Providers); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.recordAudit(c, "tenant.replace_providers", "tenant", tenant.ID, req)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"tenant_id": tenant.ID,
		"providers": req.Providers,
	}})
}

func (h *AdminHandler) providerResponses(c *gin.Context, tenantID string) []gin.H {
	usageByProvider, err := h.store.GetProviderUsageSummary(c.Request.Context(), tenantID)
	if err != nil {
		return nil
	}

	var providers []provider.Provider
	if tenantID == "" {
		providers = h.providerMgr.List()
	} else {
		providerNames, err := h.store.ListTenantProviders(c.Request.Context(), tenantID)
		if err != nil {
			return nil
		}
		providers = h.providerMgr.ListByNames(providerNames)
	}

	statsByName := make(map[string]*provider.ProviderStats)
	for _, item := range h.providerMgr.Stats.List() {
		statsByName[item.Name] = item
	}

	result := make([]gin.H, 0, len(providers))
	for _, providerItem := range providers {
		globalStats := statsByName[providerItem.Name()]
		usageStats := usageByProvider[providerItem.Name()]
		item := gin.H{
			"name":             providerItem.Name(),
			"type":             providerItem.Type(),
			"model":            providerItem.Model(),
			"base_url":         providerItem.BaseURL(),
			"status":           providerStatus(globalStats),
			"current_load":     providerLoad(globalStats),
			"total_requests":   usageStats.TotalRequests,
			"success_requests": usageStats.SuccessRequests,
			"failed_requests":  usageStats.FailedRequests,
			"total_tokens":     usageStats.TotalTokens,
			"total_cost_usd":   usageStats.TotalCostUSD,
			"avg_latency_ms":   usageStats.AvgLatencyMs,
			"error_rate":       errorRate(usageStats.TotalRequests, usageStats.FailedRequests),
		}
		if record, ok := h.providerMgr.Registry(providerItem.Name()); ok {
			for key, value := range providerRegistryToResponse(record) {
				item[key] = value
			}
		}
		result = append(result, item)
	}

	return result
}

func (h *AdminHandler) scopeTenantID(c *gin.Context, identity *repository.AuthIdentity) (string, bool) {
	if identity.Role == repository.RoleSuperAdmin {
		return c.Query("tenant_id"), true
	}
	return identity.TenantID, true
}

func (h *AdminHandler) resolveTargetTenant(c *gin.Context, identity *repository.AuthIdentity, requested string) (string, bool) {
	if identity.Role == repository.RoleSuperAdmin {
		if requested == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id is required"})
			return "", false
		}
		return requested, true
	}
	return identity.TenantID, true
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

func scopedTenant(identity *repository.AuthIdentity) string {
	if identity == nil {
		return ""
	}
	return identity.TenantID
}

func providerNames(items []provider.Provider) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names
}

func providerStatus(stats *provider.ProviderStats) string {
	if stats == nil {
		return "unknown"
	}
	return stats.Status
}

type CreateProjectRequest struct {
	TenantID  string  `json:"tenant_id"`
	Slug      string  `json:"slug" binding:"required"`
	Name      string  `json:"name" binding:"required"`
	BudgetUSD float64 `json:"budget_usd"`
}

func (h *AdminHandler) CreateProject(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tenantID, ok := h.resolveTargetTenant(c, identity, req.TenantID)
	if !ok {
		return
	}
	project, err := h.store.CreateProject(c.Request.Context(), repository.CreateProjectParams{
		TenantID:  tenantID,
		Slug:      req.Slug,
		Name:      req.Name,
		Status:    repository.StatusActive,
		BudgetUSD: req.BudgetUSD,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.recordAudit(c, "project.create", "project", project.ID, req)
	c.JSON(http.StatusCreated, gin.H{"data": projectToResponse(*project)})
}

func (h *AdminHandler) ListProjects(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	projects, err := h.store.ListProjects(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]gin.H, 0, len(projects))
	for _, item := range projects {
		result = append(result, projectToResponse(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AdminHandler) GetProject(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	project, err := h.store.GetProject(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projectToResponse(*project)})
}

func (h *AdminHandler) GetProjectUsage(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	project, err := h.store.GetProject(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}
	summary, err := h.store.GetProjectUsageSummary(c.Request.Context(), project.TenantID, project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	trend, err := h.store.GetProjectUsageTrend(c.Request.Context(), project.TenantID, project.ID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"project": projectToResponse(*project),
		"summary": summary,
		"trend":   trend,
	}})
}

type UpdateProjectRequest struct {
	Name      *string  `json:"name"`
	Status    *string  `json:"status"`
	BudgetUSD *float64 `json:"budget_usd"`
}

func (h *AdminHandler) UpdateProject(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	project, err := h.store.UpdateProject(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateProjectParams{
		Name:      req.Name,
		Status:    req.Status,
		BudgetUSD: req.BudgetUSD,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projectToResponse(*project)})
}

func (h *AdminHandler) GetResponseTrace(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}
	record, err := h.store.GetResponse(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "response not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(record.RouteTraceBody) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"response_id": record.ID,
			"trace":       gin.H{},
		}})
		return
	}
	var trace any
	if err := json.Unmarshal(record.RouteTraceBody, &trace); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"response_id": record.ID,
		"trace":       trace,
	}})
}

func (h *AdminHandler) GetUsageSummary(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	filter, ok := h.usageFilter(c, identity)
	if !ok {
		return
	}
	summary, err := h.store.GetUsageSummaryFiltered(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"filter":  usageFilterToResponse(filter),
		"summary": summary,
	}})
}

func (h *AdminHandler) GetUsageBreakdown(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	filter, ok := h.usageFilter(c, identity)
	if !ok {
		return
	}
	dimension := c.DefaultQuery("dimension", "provider")
	rows, err := h.store.GetUsageBreakdown(c.Request.Context(), filter, dimension)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"filter":    usageFilterToResponse(filter),
		"dimension": dimension,
		"rows":      rows,
	}})
}

func (h *AdminHandler) GetUsageTrend(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	filter, ok := h.usageFilter(c, identity)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", "day")
	limit := 30
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	rows, err := h.store.GetUsageTimeBuckets(c.Request.Context(), filter, period, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"filter": usageFilterToResponse(filter),
		"period": period,
		"rows":   rows,
	}})
}

func providerLoad(stats *provider.ProviderStats) int64 {
	if stats == nil {
		return 0
	}
	return stats.CurrentLoad
}

func validProviderHealthStatus(value string) bool {
	switch value {
	case provider.ProviderHealthHealthy, provider.ProviderHealthDegraded, provider.ProviderHealthUnhealthy:
		return true
	default:
		return false
	}
}

func providerRegistryToResponse(record repository.ProviderRegistryRecord) gin.H {
	return gin.H{
		"vendor":                     record.Vendor,
		"endpoint":                   record.Endpoint,
		"enabled":                    record.Enabled,
		"drain":                      record.Drain,
		"health_status":              record.HealthStatus,
		"routing_weight":             record.RoutingWeight,
		"supports_chat":              record.SupportsChat,
		"supports_responses":         record.SupportsResponses,
		"supports_messages":          record.SupportsMessages,
		"supports_stream":            record.SupportsStream,
		"supports_tools":             record.SupportsTools,
		"supports_images":            record.SupportsImages,
		"supports_structured_output": record.SupportsStructuredOutput,
		"supports_long_context":      record.SupportsLongContext,
	}
}

func userToResponse(user repository.UserRecord) gin.H {
	return gin.H{
		"id":             user.ID,
		"tenant_id":      user.TenantID,
		"tenant_slug":    user.TenantSlug,
		"api_key":        user.APIKey,
		"project_id":     user.ProjectID,
		"name":           user.Name,
		"email":          user.Email,
		"role":           user.Role,
		"quota":          user.Quota,
		"used":           user.Used,
		"remaining":      remaining(&user),
		"qps":            user.QPS,
		"key_budget_usd": user.KeyBudgetUSD,
		"key_spent_usd":  user.KeySpentUSD,
		"models":         user.Models,
		"status":         user.Status,
		"created_at":     user.CreatedAt,
		"updated_at":     user.UpdatedAt,
	}
}

func projectToResponse(project repository.ProjectRecord) gin.H {
	return gin.H{
		"id":          project.ID,
		"tenant_id":   project.TenantID,
		"tenant_slug": project.TenantSlug,
		"slug":        project.Slug,
		"name":        project.Name,
		"status":      project.Status,
		"budget_usd":  project.BudgetUSD,
		"spent_usd":   project.SpentUSD,
		"created_at":  project.CreatedAt,
		"updated_at":  project.UpdatedAt,
	}
}

func remaining(user *repository.UserRecord) int {
	if user.Quota <= 0 {
		return -1
	}
	return user.Quota - user.Used
}

func usagePercent(stats *repository.UserStats) float64 {
	if stats.TotalQuota <= 0 {
		return 0
	}
	return float64(stats.TotalUsed) / float64(stats.TotalQuota) * 100
}

func errorRate(total, failed int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total)
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

func validEntityStatus(value string) bool {
	switch value {
	case repository.StatusActive, repository.StatusInactive, repository.StatusRevoked:
		return true
	default:
		return false
	}
}

func (h *AdminHandler) usageFilter(c *gin.Context, identity *repository.AuthIdentity) (repository.UsageFilter, bool) {
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return repository.UsageFilter{}, false
	}
	filter := repository.UsageFilter{
		TenantID:     tenantID,
		ProjectID:    c.Query("project_id"),
		UserID:       c.Query("user_id"),
		APIKeyID:     c.Query("api_key_id"),
		ProviderName: c.Query("provider"),
		Model:        c.Query("model"),
	}
	if raw := c.Query("start_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_time"})
			return repository.UsageFilter{}, false
		}
		filter.StartTime = parsed
	}
	if raw := c.Query("end_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_time"})
			return repository.UsageFilter{}, false
		}
		filter.EndTime = parsed
	}
	if filter.StartTime.IsZero() && filter.EndTime.IsZero() {
		days := 30
		if raw := c.Query("days"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				days = parsed
			}
		}
		filter.StartTime = time.Now().UTC().AddDate(0, 0, -days)
	}
	return filter, true
}

func usageFilterToResponse(filter repository.UsageFilter) gin.H {
	return gin.H{
		"tenant_id":  filter.TenantID,
		"project_id": filter.ProjectID,
		"user_id":    filter.UserID,
		"api_key_id": filter.APIKeyID,
		"provider":   filter.ProviderName,
		"model":      filter.Model,
		"start_time": zeroTimeOrValue(filter.StartTime),
		"end_time":   zeroTimeOrValue(filter.EndTime),
	}
}

func zeroTimeOrValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
