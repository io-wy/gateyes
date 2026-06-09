package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
)

func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	filter := repository.AuditLogFilter{
		Action:       c.Query("action"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
		ActorUserID:  c.Query("actor_user_id"),
		Limit:        100,
	}
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			filter.Limit = parsed
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
	items, err := h.store.ListAuditLogs(c.Request.Context(), tenantID, filter)
	if err != nil {
		writeError(c, http.StatusInternalServerError, CodeDatabaseError, err.Error())
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, auditLogToResponse(item))
	}
	writeList(c, result, int64(len(result)))
}

func (h *AdminHandler) recordAudit(c *gin.Context, action, resourceType, resourceID string, payload any) {
	if h == nil || h.store == nil || c == nil {
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		return
	}
	var payloadBytes []byte
	if payload != nil {
		payloadBytes, _ = json.Marshal(payload)
	}
	requestCtx, _ := middleware.GetRequestContext(c)
	requestID := ""
	if requestCtx != nil {
		requestID = requestCtx.RequestID
	}
	_ = h.store.CreateAuditLog(c.Request.Context(), repository.AuditLogRecord{
		TenantID:      identity.TenantID,
		ActorUserID:   identity.UserID,
		ActorAPIKeyID: identity.APIKeyID,
		ActorRole:     identity.Role,
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		RequestID:     requestID,
		IPAddress:     c.ClientIP(),
		Payload:       payloadBytes,
	})
}

func auditLogToResponse(record repository.AuditLogRecord) gin.H {
	payload := any(nil)
	if len(record.Payload) > 0 {
		var decoded any
		if json.Unmarshal(record.Payload, &decoded) == nil {
			payload = decoded
		}
	}
	return gin.H{
		"id":               record.ID,
		"tenant_id":        record.TenantID,
		"actor_user_id":    record.ActorUserID,
		"actor_api_key_id": record.ActorAPIKeyID,
		"actor_role":       record.ActorRole,
		"action":           record.Action,
		"resource_type":    record.ResourceType,
		"resource_id":      record.ResourceID,
		"request_id":       record.RequestID,
		"ip_address":       record.IPAddress,
		"payload":          payload,
		"created_at":       record.CreatedAt,
	}
}
