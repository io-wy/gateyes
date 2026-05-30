package handler

import (
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *AdminHandler) CreateUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	tenantID, ok := h.resolveTargetTenant(c, identity, req.TenantID)
	if !ok {
		return
	}
	if _, err := h.store.GetTenant(c.Request.Context(), tenantID); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeBadRequest, "tenant not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	role := req.Role
	if role == "" {
		role = repository.RoleTenantUser
	}
	if role == repository.RoleSuperAdmin && identity.Role != repository.RoleSuperAdmin {
		writeError(c, http.StatusForbidden, CodeInsufficientRole, "forbidden")
		return
	}

	quota := req.Quota
	if quota == 0 {
		quota = -1
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
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	writeOK(c, gin.H{
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
	})
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
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	result := make([]gin.H, 0, len(users))
	for _, user := range users {
		result = append(result, userToResponse(user))
	}

	writeOK(c, result)
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
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, userToResponse(*user))
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
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.Role != nil && *req.Role == repository.RoleSuperAdmin && identity.Role != repository.RoleSuperAdmin {
		writeError(c, http.StatusForbidden, CodeInsufficientRole, "forbidden")
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
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	h.recordAudit(c, "user.update", "user", user.ID, req)
	writeOK(c, userToResponse(*user))
}

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID := scopedTenant(identity)
	if identity.Role == repository.RoleSuperAdmin {
		tenantID = ""
	}

	if err := h.store.DeleteUser(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	h.recordAudit(c, "user.delete", "user", c.Param("id"), gin.H{"user_id": c.Param("id")})
	writeOKMsg(c, "user deleted", nil)
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
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	writeOK(c, gin.H{
		"id":        user.ID,
		"used":      user.Used,
		"remaining": remaining(user),
	})
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
			writeError(c, http.StatusNotFound, CodeUserNotFound, "user not found")
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
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
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	// 计算使用率
	usagePercent := 0.0
	if user.Quota > 0 {
		usagePercent = float64(user.Used) / float64(user.Quota) * 100
	}

	writeOK(c, gin.H{
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
	})
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

