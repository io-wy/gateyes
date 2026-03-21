package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

type AdminHandler struct {
	store       repository.Store
	providerMgr *provider.Manager
	startedAt   time.Time
}

func NewAdminHandler(store repository.Store, providerMgr *provider.Manager) *AdminHandler {
	return &AdminHandler{
		store:       store,
		providerMgr: providerMgr,
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

type CreateUserRequest struct {
	TenantID string   `json:"tenant_id"`
	Name     string   `json:"name" binding:"required"`
	Email    string   `json:"email"`
	Role     string   `json:"role"`
	Quota    int      `json:"quota"`
	QPS      int      `json:"qps"`
	Models   []string `json:"models"`
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
		TenantID:   tenantID,
		Name:       req.Name,
		Email:      req.Email,
		Role:       role,
		Quota:      quota,
		QPS:        req.QPS,
		Models:     req.Models,
		Status:     repository.StatusActive,
		APIKey:     apiKey,
		SecretHash: repository.HashSecret(apiSecret),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"id":          user.ID,
		"tenant_id":   user.TenantID,
		"tenant_slug": user.TenantSlug,
		"api_key":     user.APIKey,
		"api_secret":  apiSecret,
		"token":       user.APIKey + ":" + apiSecret,
		"name":        user.Name,
		"email":       user.Email,
		"role":        user.Role,
		"quota":       user.Quota,
		"used":        user.Used,
		"remaining":   remaining(user),
		"qps":         user.QPS,
		"models":      user.Models,
		"status":      user.Status,
		"created_at":  user.CreatedAt,
	}})
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
	Role   *string   `json:"role"`
	Quota  *int      `json:"quota"`
	QPS    *int      `json:"qps"`
	Models *[]string `json:"models"`
	Status *string   `json:"status"`
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
		Role:   req.Role,
		Quota:  req.Quota,
		QPS:    req.QPS,
		Models: req.Models,
		Status: req.Status,
	})
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
		// 可以后续扩展解析 days 参数
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
			"id":            user.ID,
			"name":          user.Name,
			"quota":         user.Quota,
			"used":          user.Used,
			"remaining":     remaining(user),
			"usage_percent": usagePercent,
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
	ID   string `json:"id"`
	Slug string `json:"slug" binding:"required"`
	Name string `json:"name"`
}

func (h *AdminHandler) CreateTenant(c *gin.Context) {
	var req CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenant, err := h.store.EnsureTenant(c.Request.Context(), repository.EnsureTenantParams{
		ID:     req.ID,
		Slug:   req.Slug,
		Name:   req.Name,
		Status: repository.StatusActive,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.ReplaceTenantProviders(c.Request.Context(), tenant.ID, providerNames(h.providerMgr.List())); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	var req UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenant, err := h.store.UpdateTenant(c.Request.Context(), c.Param("id"), repository.UpdateTenantParams{
		Name:   req.Name,
		Status: req.Status,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
		result = append(result, gin.H{
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
			"avg_latency_ms":   usageStats.AvgLatencyMs,
			"error_rate":       errorRate(usageStats.TotalRequests, usageStats.FailedRequests),
		})
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

func providerLoad(stats *provider.ProviderStats) int64 {
	if stats == nil {
		return 0
	}
	return stats.CurrentLoad
}

func userToResponse(user repository.UserRecord) gin.H {
	return gin.H{
		"id":          user.ID,
		"tenant_id":   user.TenantID,
		"tenant_slug": user.TenantSlug,
		"api_key":     user.APIKey,
		"name":        user.Name,
		"email":       user.Email,
		"role":        user.Role,
		"quota":       user.Quota,
		"used":        user.Used,
		"remaining":   remaining(&user),
		"qps":         user.QPS,
		"models":      user.Models,
		"status":      user.Status,
		"created_at":  user.CreatedAt,
		"updated_at":  user.UpdatedAt,
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
