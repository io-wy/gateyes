package handler

import (
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

func (h *AdminHandler) Dashboard(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	userStats, err := h.store.Stats(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	usageStats, err := h.store.GetUsageSummary(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}

	writeOK(c, gin.H{
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
	})
}

func (h *AdminHandler) GetBudgets(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return
	}

	apiKeyID := c.Query("api_key_id")
	projectID := c.Query("project_id")

	status, err := h.store.GetBudgetStatus(c.Request.Context(), tenantID, projectID, apiKeyID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, status)
}

func (h *AdminHandler) GetUsageSummary(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	filter, ok := h.usageFilter(c, identity)
	if !ok {
		return
	}
	summary, err := h.store.GetUsageSummaryFiltered(c.Request.Context(), filter)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, gin.H{
		"filter":  usageFilterToResponse(filter),
		"summary": summary,
	})
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
		writeError(c, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	writeOK(c, gin.H{
		"filter":    usageFilterToResponse(filter),
		"dimension": dimension,
		"rows":      rows,
	})
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
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	writeOK(c, gin.H{
		"filter": usageFilterToResponse(filter),
		"period": period,
		"rows":   rows,
	})
}

func errorRate(total, failed int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total)
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
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid start_time")
			return repository.UsageFilter{}, false
		}
		filter.StartTime = parsed
	}
	if raw := c.Query("end_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid end_time")
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

