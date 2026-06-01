package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func (h *AdminHandler) ListResponses(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	filter := repository.ResponseFilter{
		ProviderName: c.Query("provider_name"),
		Model:        c.Query("model"),
		Status:       c.Query("status"),
		ProjectID:    c.Query("project_id"),
		APIKeyID:     c.Query("api_key_id"),
		UserID:       c.Query("user_id"),
		Query:        c.Query("q"),
		Limit:        100,
	}
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			filter.Limit = parsed
		}
	}
	if raw := c.Query("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			filter.Offset = parsed
		}
	}
	if raw := c.Query("start_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid start_time")
			return
		}
		filter.StartTime = parsed
	}
	if raw := c.Query("end_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid end_time")
			return
		}
		filter.EndTime = parsed
	}
	total, err := h.store.CountResponses(c.Request.Context(), tenantID, filter)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	items, err := h.store.ListResponses(c.Request.Context(), tenantID, filter)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, responseToResponse(item))
	}
	writeList(c, result, int64(total))
}

func (h *AdminHandler) GetResponseTrace(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	record, err := h.store.GetResponse(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeResponseNotFound, "response not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	if len(record.RouteTraceBody) == 0 {
		writeOK(c, gin.H{
			"response_id": record.ID,
			"trace":       gin.H{},
		})
		return
	}
	var trace any
	if err := json.Unmarshal(record.RouteTraceBody, &trace); err != nil {
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"response_id": record.ID,
		"trace":       trace,
	})
}

func responseToResponse(record repository.ResponseRecord) gin.H {
	return gin.H{
		"id":            record.ID,
		"tenant_id":     record.TenantID,
		"project_id":    record.ProjectID,
		"user_id":       record.UserID,
		"api_key_id":    record.APIKeyID,
		"provider_name": record.ProviderName,
		"model":         record.Model,
		"status":        record.Status,
		"created_at":    record.CreatedAt,
		"updated_at":    record.UpdatedAt,
	}
}
