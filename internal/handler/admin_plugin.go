package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

const maxWasmFileSize = 10 * 1024 * 1024 // 10 MB

var validPluginPhases = map[string]struct{}{
	"pre_route":    {},
	"post_route":   {},
	"pre_upstream": {},
	"post_upstream": {},
	"audit":        {},
}

type CreatePluginRequest struct {
	Name        string         `json:"name" binding:"required"`
	Type        string         `json:"type" binding:"required"`
	Description string         `json:"description"`
	Author      string         `json:"author"`
	Phases      []string       `json:"phases"`
	Address     string         `json:"address"`
	TimeoutMs   int            `json:"timeout_ms"`
	MemoryPages int            `json:"memory_pages"`
	Enabled     bool           `json:"enabled"`
	Source      string         `json:"source"`
	Config      map[string]any `json:"config"`
}

type UpdatePluginRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Author      *string         `json:"author"`
	Phases      *[]string       `json:"phases"`
	Address     *string         `json:"address"`
	TimeoutMs   *int            `json:"timeout_ms"`
	MemoryPages *int            `json:"memory_pages"`
	Enabled     *bool           `json:"enabled"`
	Source      *string         `json:"source"`
	Config      *map[string]any `json:"config"`
}

func (h *AdminHandler) ListPlugins(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var filter repository.PluginFilter
	if raw := c.Query("type"); raw != "" {
		filter.Type = raw
	}
	if raw := c.Query("enabled"); raw != "" {
		value := raw == "true"
		filter.Enabled = &value
	}
	if raw := c.Query("source"); raw != "" {
		filter.Source = raw
	}

	plugins, err := h.store.ListPlugins(c.Request.Context(), tenantID, filter)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(plugins))
	for _, item := range plugins {
		result = append(result, pluginToResponse(item))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetPlugin(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	record, err := h.store.GetPlugin(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodePluginNotFound, "plugin not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, pluginToResponse(*record))
}

func (h *AdminHandler) CreatePlugin(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req CreatePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if err := validatePluginRequest(req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
		return
	}

	record, err := h.store.CreatePlugin(c.Request.Context(), repository.CreatePluginParams{
		TenantID:    tenantID,
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		Author:      req.Author,
		Phases:      req.Phases,
		Address:     req.Address,
		TimeoutMs:   req.TimeoutMs,
		MemoryPages: req.MemoryPages,
		Enabled:     req.Enabled,
		Source:      defaultSource(req.Source),
		Config:      req.Config,
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "plugin.create", "plugin", record.ID, req)
	writeOK(c, pluginToResponse(*record))
}

func (h *AdminHandler) UpdatePlugin(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req UpdatePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.Phases != nil {
		if err := validatePhases(*req.Phases); err != nil {
			writeError(c, http.StatusBadRequest, CodeInvalidParameter, err.Error())
			return
		}
	}

	record, err := h.store.UpdatePlugin(c.Request.Context(), tenantID, c.Param("id"), repository.UpdatePluginParams{
		Name:        req.Name,
		Description: req.Description,
		Author:      req.Author,
		Phases:      req.Phases,
		Address:     req.Address,
		TimeoutMs:   req.TimeoutMs,
		MemoryPages: req.MemoryPages,
		Enabled:     req.Enabled,
		Source:      req.Source,
		Config:      req.Config,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodePluginNotFound, "plugin not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "plugin.update", "plugin", record.ID, req)
	writeOK(c, pluginToResponse(*record))
}

func (h *AdminHandler) DeletePlugin(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	record, err := h.store.GetPlugin(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodePluginNotFound, "plugin not found")
			return
		}
		writeInternalError(c, err)
		return
	}

	if record.Type == "wasm" && record.FilePath != "" {
		_ = os.Remove(record.FilePath)
	}

	if err := h.store.DeletePlugin(c.Request.Context(), tenantID, record.ID); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodePluginNotFound, "plugin not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "plugin.delete", "plugin", record.ID, nil)
	writeOK(c, gin.H{"id": record.ID, "deleted": true})
}

func (h *AdminHandler) UploadPlugin(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		writeError(c, http.StatusBadRequest, CodeMissingRequiredField, "name is required")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, "missing file")
		return
	}
	defer file.Close()

	if header.Size > maxWasmFileSize {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, fmt.Sprintf("file exceeds %d bytes", maxWasmFileSize))
		return
	}
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".wasm") {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "only .wasm files allowed")
		return
	}

	pluginID := uuid.New().String()
	marketDir := filepath.Join(h.pluginDir, "marketplace", tenantID)
	if err := os.MkdirAll(marketDir, 0o755); err != nil {
		writeInternalError(c, fmt.Errorf("create plugin dir: %w", err))
		return
	}
	filePath := filepath.Join(marketDir, pluginID+".wasm")

	out, err := os.Create(filePath)
	if err != nil {
		writeInternalError(c, fmt.Errorf("create plugin file: %w", err))
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeInternalError(c, fmt.Errorf("write plugin file: %w", err))
		return
	}

	phases := parsePhases(c.PostForm("phases"))
	if len(phases) == 0 {
		phases = []string{"post_upstream"}
	}
	timeoutMs := parseInt(c.PostForm("timeout_ms"), 50)
	memoryPages := parseInt(c.PostForm("memory_pages"), 1)

	record, err := h.store.CreatePlugin(c.Request.Context(), repository.CreatePluginParams{
		TenantID:    tenantID,
		Name:        name,
		Type:        "wasm",
		Description: c.PostForm("description"),
		Author:      c.PostForm("author"),
		Phases:      phases,
		FilePath:    filePath,
		TimeoutMs:   timeoutMs,
		MemoryPages: memoryPages,
		Enabled:     c.PostForm("enabled") == "true",
		Source:      "custom",
		Config:      map[string]any{},
	})
	if err != nil {
		_ = os.Remove(filePath)
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "plugin.upload", "plugin", record.ID, gin.H{"name": record.Name})
	writeOK(c, pluginToResponse(*record))
}

func pluginToResponse(record repository.PluginRecord) gin.H {
	return gin.H{
		"id":           record.ID,
		"tenant_id":    record.TenantID,
		"name":         record.Name,
		"type":         record.Type,
		"description":  record.Description,
		"author":       record.Author,
		"phases":       record.Phases,
		"file_path":    record.FilePath,
		"address":      record.Address,
		"timeout_ms":   record.TimeoutMs,
		"memory_pages": record.MemoryPages,
		"enabled":      record.Enabled,
		"source":       record.Source,
		"config":       record.Config,
		"created_at":   record.CreatedAt,
		"updated_at":   record.UpdatedAt,
	}
}

func validatePluginRequest(req CreatePluginRequest) error {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type != "wasm" && req.Type != "grpc" {
		return fmt.Errorf("type must be wasm or grpc")
	}
	if req.Type == "grpc" && strings.TrimSpace(req.Address) == "" {
		return fmt.Errorf("address is required for grpc plugins")
	}
	if req.Type == "wasm" && strings.TrimSpace(req.Address) != "" {
		return fmt.Errorf("address is not used for wasm plugins")
	}
	return validatePhases(req.Phases)
}

func validatePhases(phases []string) error {
	if len(phases) == 0 {
		return fmt.Errorf("at least one phase is required")
	}
	for _, p := range phases {
		p = strings.ToLower(strings.TrimSpace(p))
		if _, ok := validPluginPhases[p]; !ok {
			return fmt.Errorf("invalid phase: %s", p)
		}
	}
	return nil
}

func defaultSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return "custom"
	}
	return s
}

func parsePhases(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseInt(raw string, defaultValue int) int {
	if raw == "" {
		return defaultValue
	}
	var value int
	_, err := fmt.Sscanf(raw, "%d", &value)
	if err != nil {
		return defaultValue
	}
	return value
}
