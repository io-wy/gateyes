package handler

import (
	"net/http"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gin-gonic/gin"
)

func (h *AdminHandler) CheckProviders(c *gin.Context) {
	if h.healthChecker == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "health checker not configured")
		return
	}
	if err := h.healthChecker.ForceCheck(c.Request.Context()); err != nil {
		writeInternalError(c, err)
		return
	}
	tenantID := h.adminTenantID(c)
	writeOK(c, h.providerResponses(c, tenantID))
}

func (h *AdminHandler) GetProviders(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	writeOK(c, h.providerResponses(c, tenantID))
}

func (h *AdminHandler) GetProvider(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	providers := h.providerResponses(c, tenantID)
	for _, item := range providers {
		if item["name"] == c.Param("name") {
			writeOK(c, item)
			return
		}
	}

	writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
}

func (h *AdminHandler) GetProviderStats(c *gin.Context) {
	h.GetProvider(c)
}

type CreateProviderRequest struct {
	Name                     string            `json:"name" binding:"required"`
	Type                     string            `json:"type"`
	Vendor                   string            `json:"vendor"`
	BaseURL                  string            `json:"base_url"`
	Endpoint                 string            `json:"endpoint"`
	APIKey                   string            `json:"api_key"`
	Model                    string            `json:"model" binding:"required"`
	RoutingWeight            int               `json:"routing_weight"`
	PriceInput               float64           `json:"price_input"`
	PriceOutput              float64           `json:"price_output"`
	MaxTokens                int               `json:"max_tokens"`
	Timeout                  int               `json:"timeout"`
	Enabled                  bool              `json:"enabled"`
	Headers                  map[string]string `json:"headers"`
	ExtraBody                map[string]any    `json:"extra_body"`
	SupportsChat             *bool             `json:"supports_chat"`
	SupportsResponses        *bool             `json:"supports_responses"`
	SupportsMessages         *bool             `json:"supports_messages"`
	SupportsStream           *bool             `json:"supports_stream"`
	SupportsTools            *bool             `json:"supports_tools"`
	SupportsImages           *bool             `json:"supports_images"`
	SupportsStructuredOutput *bool             `json:"supports_structured_output"`
	SupportsLongContext      *bool             `json:"supports_long_context"`
	SupportsEmbeddings       *bool             `json:"supports_embeddings"`
}

func (h *AdminHandler) CreateProvider(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var req CreateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}

	record := provider.DefaultRegistryRecordFromConfig(providerConfigFromCreateRequest(req))
	applyProviderCapabilityOverrides(&record, providerCapabilityOverrides{
		SupportsChat:             req.SupportsChat,
		SupportsResponses:        req.SupportsResponses,
		SupportsMessages:         req.SupportsMessages,
		SupportsStream:           req.SupportsStream,
		SupportsTools:            req.SupportsTools,
		SupportsImages:           req.SupportsImages,
		SupportsStructuredOutput: req.SupportsStructuredOutput,
		SupportsLongContext:      req.SupportsLongContext,
		SupportsEmbeddings:       req.SupportsEmbeddings,
	})
	created, err := h.providerRuntimeSvc.Upsert(c.Request.Context(), record)
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	if tenantID, ok := h.scopeTenantID(c, identity); ok && tenantID != "" {
		if err := h.appendTenantProvider(c.Request.Context(), tenantID, created.Name); err != nil {
			writeInternalError(c, err)
			return
		}
	}
	h.recordAudit(c, "provider.create", "provider", created.Name, providerRegistryToResponse(*created))
	writeOK(c, providerRegistryToResponse(*created))
}

type UpdateProviderRequest struct {
	Enabled                  *bool             `json:"enabled"`
	Drain                    *bool             `json:"drain"`
	HealthStatus             *string           `json:"health_status"`
	RoutingWeight            *int              `json:"routing_weight"`
	Type                     *string           `json:"type"`
	Vendor                   *string           `json:"vendor"`
	BaseURL                  *string           `json:"base_url"`
	Endpoint                 *string           `json:"endpoint"`
	APIKey                   *string           `json:"api_key"`
	Model                    *string           `json:"model"`
	PriceInput               *float64          `json:"price_input"`
	PriceOutput              *float64          `json:"price_output"`
	MaxTokens                *int              `json:"max_tokens"`
	Timeout                  *int              `json:"timeout"`
	Headers                  map[string]string `json:"headers"`
	ExtraBody                map[string]any    `json:"extra_body"`
	SupportsChat             *bool             `json:"supports_chat"`
	SupportsResponses        *bool             `json:"supports_responses"`
	SupportsMessages         *bool             `json:"supports_messages"`
	SupportsStream           *bool             `json:"supports_stream"`
	SupportsTools            *bool             `json:"supports_tools"`
	SupportsImages           *bool             `json:"supports_images"`
	SupportsStructuredOutput *bool             `json:"supports_structured_output"`
	SupportsLongContext      *bool             `json:"supports_long_context"`
	SupportsEmbeddings       *bool             `json:"supports_embeddings"`
}

func (h *AdminHandler) UpdateProvider(c *gin.Context) {
	var req UpdateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.HealthStatus != nil && !validProviderHealthStatus(*req.HealthStatus) {
		writeError(c, http.StatusBadRequest, CodeInvalidParameter, "invalid health_status")
		return
	}

	current, err := h.store.GetProviderRegistry(c.Request.Context(), c.Param("name"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	updated := mergeProviderUpdate(*current, req)
	record, err := h.providerRuntimeSvc.Upsert(c.Request.Context(), updated)
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	h.recordAudit(c, "provider.update", "provider", record.Name, req)
	writeOK(c, providerRegistryToResponse(*record))
}

func (h *AdminHandler) DeleteProvider(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	name := c.Param("name")
	if err := h.providerRuntimeSvc.Delete(c.Request.Context(), name); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	if tenantID, ok := h.scopeTenantID(c, identity); ok && tenantID != "" {
		_ = h.removeTenantProvider(c.Request.Context(), tenantID, name)
	}
	h.recordAudit(c, "provider.delete", "provider", name, gin.H{"name": name})
	writeOK(c, gin.H{"name": name, "deleted": true})
}

type CreateAPIKeyRequest struct {
	UserID           string     `json:"user_id" binding:"required"`
	ProjectID        string     `json:"project_id"`
	BudgetUSD        float64    `json:"budget_usd"`
	RateLimitQPS     int        `json:"rate_limit_qps"`
	AllowedModels    []string   `json:"allowed_models"`
	AllowedProviders []string   `json:"allowed_providers"`
	AllowedServices  []string   `json:"allowed_services"`
	ExpiresAt        *time.Time `json:"expires_at"`
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

func providerStatus(stats *provider.ProviderStats) string {
	if stats == nil {
		return "unknown"
	}
	return stats.Status
}

type CreateProjectRequest struct {
	TenantID  string                          `json:"tenant_id"`
	Slug      string                          `json:"slug" binding:"required"`
	Name      string                          `json:"name" binding:"required"`
	BudgetUSD float64                         `json:"budget_usd"`
	Policy    *repository.ServicePolicyConfig `json:"policy"`
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
	response := gin.H{
		"name":                       record.Name,
		"type":                       record.Type,
		"base_url":                   record.BaseURL,
		"model":                      record.Model,
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
		"supports_embeddings":        record.SupportsEmbeddings,
		"created_at":                 record.CreatedAt,
		"updated_at":                 record.UpdatedAt,
	}
	if record.RuntimeConfig != nil {
		response["timeout"] = record.RuntimeConfig.Timeout
		response["max_tokens"] = record.RuntimeConfig.MaxTokens
		response["price_input"] = record.RuntimeConfig.PriceInput
		response["price_output"] = record.RuntimeConfig.PriceOutput
		response["headers"] = record.RuntimeConfig.Headers
		response["extra_body"] = record.RuntimeConfig.ExtraBody
		response["has_api_key"] = record.RuntimeConfig.APIKey != ""
	}
	return response
}

type providerCapabilityOverrides struct {
	SupportsChat             *bool
	SupportsResponses        *bool
	SupportsMessages         *bool
	SupportsStream           *bool
	SupportsTools            *bool
	SupportsImages           *bool
	SupportsStructuredOutput *bool
	SupportsLongContext      *bool
	SupportsEmbeddings       *bool
}

func providerConfigFromCreateRequest(req CreateProviderRequest) config.ProviderConfig {
	weight := req.RoutingWeight
	if weight <= 0 {
		weight = 1
	}
	return config.ProviderConfig{
		Name:        req.Name,
		Type:        req.Type,
		Vendor:      req.Vendor,
		BaseURL:     req.BaseURL,
		Endpoint:    req.Endpoint,
		APIKey:      req.APIKey,
		Model:       req.Model,
		Weight:      weight,
		PriceInput:  req.PriceInput,
		PriceOutput: req.PriceOutput,
		MaxTokens:   req.MaxTokens,
		Timeout:     req.Timeout,
		Enabled:     req.Enabled,
		Headers:     req.Headers,
		ExtraBody:   req.ExtraBody,
	}
}

func mergeProviderUpdate(current repository.ProviderRegistryRecord, req UpdateProviderRequest) repository.ProviderRegistryRecord {
	next := current
	if req.Type != nil {
		next.Type = *req.Type
	}
	if req.Vendor != nil {
		next.Vendor = *req.Vendor
	}
	if req.BaseURL != nil {
		next.BaseURL = *req.BaseURL
	}
	if req.Endpoint != nil {
		next.Endpoint = *req.Endpoint
	}
	if req.Model != nil {
		next.Model = *req.Model
	}
	if req.Enabled != nil {
		next.Enabled = *req.Enabled
	}
	if req.Drain != nil {
		next.Drain = *req.Drain
	}
	if req.HealthStatus != nil {
		next.HealthStatus = *req.HealthStatus
	}
	if req.RoutingWeight != nil {
		next.RoutingWeight = *req.RoutingWeight
	}
	if next.RuntimeConfig == nil {
		next.RuntimeConfig = &repository.ProviderRuntimeConfig{Enabled: next.Enabled}
	}
	if req.APIKey != nil {
		next.RuntimeConfig.APIKey = *req.APIKey
	}
	if req.PriceInput != nil {
		next.RuntimeConfig.PriceInput = *req.PriceInput
	}
	if req.PriceOutput != nil {
		next.RuntimeConfig.PriceOutput = *req.PriceOutput
	}
	if req.MaxTokens != nil {
		next.RuntimeConfig.MaxTokens = *req.MaxTokens
	}
	if req.Timeout != nil {
		next.RuntimeConfig.Timeout = *req.Timeout
	}
	if req.Headers != nil {
		next.RuntimeConfig.Headers = req.Headers
	}
	if req.ExtraBody != nil {
		next.RuntimeConfig.ExtraBody = req.ExtraBody
	}
	next.RuntimeConfig.Enabled = next.Enabled
	applyProviderCapabilityOverrides(&next, providerCapabilityOverrides{
		SupportsChat:             req.SupportsChat,
		SupportsResponses:        req.SupportsResponses,
		SupportsMessages:         req.SupportsMessages,
		SupportsStream:           req.SupportsStream,
		SupportsTools:            req.SupportsTools,
		SupportsImages:           req.SupportsImages,
		SupportsStructuredOutput: req.SupportsStructuredOutput,
		SupportsLongContext:      req.SupportsLongContext,
		SupportsEmbeddings:       req.SupportsEmbeddings,
	})
	return next
}

func applyProviderCapabilityOverrides(record *repository.ProviderRegistryRecord, overrides providerCapabilityOverrides) {
	if overrides.SupportsChat != nil {
		record.SupportsChat = *overrides.SupportsChat
	}
	if overrides.SupportsResponses != nil {
		record.SupportsResponses = *overrides.SupportsResponses
	}
	if overrides.SupportsMessages != nil {
		record.SupportsMessages = *overrides.SupportsMessages
	}
	if overrides.SupportsStream != nil {
		record.SupportsStream = *overrides.SupportsStream
	}
	if overrides.SupportsTools != nil {
		record.SupportsTools = *overrides.SupportsTools
	}
	if overrides.SupportsImages != nil {
		record.SupportsImages = *overrides.SupportsImages
	}
	if overrides.SupportsStructuredOutput != nil {
		record.SupportsStructuredOutput = *overrides.SupportsStructuredOutput
	}
	if overrides.SupportsLongContext != nil {
		record.SupportsLongContext = *overrides.SupportsLongContext
	}
	if overrides.SupportsEmbeddings != nil {
		record.SupportsEmbeddings = *overrides.SupportsEmbeddings
	}
}
