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
	providers, err := h.providerRuntimeSvc.List(c.Request.Context(), tenantID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	writeOK(c, providerViewsToResponses(providers))
}

func (h *AdminHandler) GetProviders(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	providers, err := h.providerRuntimeSvc.List(c.Request.Context(), tenantID)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	writeOK(c, providerViewsToResponses(providers))
}

func (h *AdminHandler) GetProvider(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	providerView, err := h.providerRuntimeSvc.Get(c.Request.Context(), tenantID, c.Param("name"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, providerViewToResponse(*providerView))
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
	Labels                   map[string]string `json:"labels"`
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
	tenantID, _ := h.scopeTenantID(c, identity)
	created, err := h.providerRuntimeSvc.CreateForTenant(c.Request.Context(), tenantID, record)
	if err != nil {
		writeError(c, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	h.recordAudit(c, "provider.create", "provider", created.Name, providerRegistryToResponse(*created))
	writeOK(c, providerRegistryToResponse(*created))
}

type UpdateProviderRequest = provider.RegistryPatch

func (h *AdminHandler) UpdateProvider(c *gin.Context) {
	var req UpdateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	record, err := h.providerRuntimeSvc.Update(c.Request.Context(), c.Param("name"), req)
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
			return
		}
		writeError(c, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	h.recordAudit(c, "provider.update", "provider", record.Name, req)
	writeOK(c, providerRegistryToResponse(*record))
}

func (h *AdminHandler) DeleteProvider(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	name := c.Param("name")
	tenantID, _ := h.scopeTenantID(c, identity)
	if err := h.providerRuntimeSvc.DeleteForTenant(c.Request.Context(), tenantID, name); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeProviderNotFound, "provider not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "provider.delete", "provider", name, gin.H{"name": name})
	writeOK(c, gin.H{"name": name, "deleted": true})
}

type CreateAPIKeyRequest struct {
	UserID           string     `json:"user_id"`
	ProjectID        string     `json:"project_id"`
	BudgetUSD        float64    `json:"budget_usd"`
	RateLimitQPS     int        `json:"rate_limit_qps"`
	AllowedModels    []string   `json:"allowed_models"`
	AllowedProviders []string   `json:"allowed_providers"`
	AllowedServices  []string   `json:"allowed_services"`
	ExpiresAt        *time.Time `json:"expires_at"`
}

func providerStatus(stats *provider.ProviderStats) string {
	if stats == nil {
		return "unknown"
	}
	return stats.Status
}

func providerViewsToResponses(views []provider.ProviderView) []gin.H {
	result := make([]gin.H, 0, len(views))
	for _, view := range views {
		result = append(result, providerViewToResponse(view))
	}
	return result
}

func providerViewToResponse(view provider.ProviderView) gin.H {
	usageStats := view.Usage
	item := gin.H{
		"name":             view.Provider.Name(),
		"type":             view.Provider.Type(),
		"model":            view.Provider.Model(),
		"base_url":         view.Provider.BaseURL(),
		"status":           providerStatus(view.Stats),
		"current_load":     providerLoad(view.Stats),
		"total_requests":   usageStats.TotalRequests,
		"success_requests": usageStats.SuccessRequests,
		"failed_requests":  usageStats.FailedRequests,
		"total_tokens":     usageStats.TotalTokens,
		"total_cost_usd":   usageStats.TotalCostUSD,
		"avg_latency_ms":   usageStats.AvgLatencyMs,
		"error_rate":       errorRate(usageStats.TotalRequests, usageStats.FailedRequests),
	}
	if view.Registry != nil {
		for key, value := range providerRegistryToResponse(*view.Registry) {
			item[key] = value
		}
	}
	return item
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
		response["labels"] = record.RuntimeConfig.Labels
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
		Labels:      req.Labels,
	}
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
