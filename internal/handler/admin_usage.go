package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gin-gonic/gin"
)

type DashboardSummary struct {
	TotalRequests   int64   `json:"totalRequests"`
	SuccessRate     float64 `json:"successRate"`
	AvgLatencyMs    float64 `json:"avgLatencyMs"`
	ActiveProviders int     `json:"activeProviders"`
	HealthyBudgets  int     `json:"healthyBudgets"`
	TotalBudgets    int     `json:"totalBudgets"`
}

func (h *AdminHandler) Dashboard(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	usageStats, err := h.store.GetUsageSummary(c.Request.Context(), tenantID)
	if err != nil {
		writeInternalError(c, err)
		return
	}

	providers := h.providerResponses(c, tenantID)
	activeProviders := 0
	for _, p := range providers {
		if status, ok := p["status"].(string); ok && status == provider.ProviderHealthHealthy {
			activeProviders++
		}
	}

	var successRate float64
	if usageStats.TotalRequests > 0 {
		successRate = float64(usageStats.SuccessRequests) / float64(usageStats.TotalRequests)
	}

	writeOK(c, DashboardSummary{
		TotalRequests:   usageStats.TotalRequests,
		SuccessRate:     successRate,
		AvgLatencyMs:    usageStats.AvgLatencyMs,
		ActiveProviders: activeProviders,
		HealthyBudgets:  0,
		TotalBudgets:    0,
	})
}

func (h *AdminHandler) GetBudgets(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	apiKeyID := c.Query("api_key_id")
	projectID := c.Query("project_id")

	status, err := h.store.GetBudgetStatus(c.Request.Context(), tenantID, projectID, apiKeyID)
	if err != nil {
		writeInternalError(c, err)
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
		writeInternalError(c, err)
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
		writeInternalError(c, err)
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
