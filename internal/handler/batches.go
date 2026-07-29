package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	batchSvc "github.com/gateyes/gateway/internal/service/batch"
)

func (h *Handler) CreateBatch(c *gin.Context) {
	if h.batch == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "batch service not configured")
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}
	rawBody := captureRequestBody(c)
	var req batchSvc.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	job, err := h.batch.Create(c.Request.Context(), identity, req, rawBody)
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
		return
	}
	writeJSON(c, http.StatusAccepted, CodeOK, "accepted", batchJobToResponse(*job))
}

func (h *Handler) ListBatches(c *gin.Context) {
	if h.batch == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "batch service not configured")
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}
	limit := parsePositiveInt(c.Query("limit"), 50)
	offset := parsePositiveInt(c.Query("offset"), 0)
	jobs, err := h.batch.List(c.Request.Context(), identity.TenantID, limit, offset)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	items := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, batchJobToResponse(job))
	}
	writeOK(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) GetBatch(c *gin.Context) {
	if h.batch == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "batch service not configured")
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}
	job, err := h.batch.Get(c.Request.Context(), identity.TenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeInvalidParameter, "batch not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, batchJobToResponse(*job))
}

func (h *Handler) ListBatchItems(c *gin.Context) {
	if h.batch == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "batch service not configured")
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}
	items, err := h.batch.Items(c.Request.Context(), identity.TenantID, c.Param("id"))
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, batchItemToResponse(item))
	}
	writeOK(c, gin.H{"items": result, "total": len(result)})
}

func batchJobToResponse(record repository.BatchJobRecord) gin.H {
	return gin.H{
		"id":              record.ID,
		"tenant_id":       record.TenantID,
		"project_id":      record.ProjectID,
		"user_id":         record.UserID,
		"api_key_id":      record.APIKeyID,
		"status":          record.Status,
		"endpoint":        record.Endpoint,
		"model":           record.Model,
		"total_items":     record.TotalItems,
		"completed_items": record.CompletedItems,
		"failed_items":    record.FailedItems,
		"error":           record.Error,
		"created_at":      record.CreatedAt,
		"updated_at":      record.UpdatedAt,
	}
}

func batchItemToResponse(record repository.BatchItemRecord) gin.H {
	return gin.H{
		"id":            record.ID,
		"job_id":        record.JobID,
		"tenant_id":     record.TenantID,
		"index":         record.Index,
		"custom_id":     record.CustomID,
		"status":        record.Status,
		"request_body":  jsonRawOrString(record.RequestBody),
		"response_body": jsonRawOrString(record.ResponseBody),
		"error":         record.Error,
		"response_id":   record.ResponseID,
		"created_at":    record.CreatedAt,
		"updated_at":    record.UpdatedAt,
	}
}

func jsonRawOrString(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return string(raw)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
