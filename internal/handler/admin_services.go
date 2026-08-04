package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/catalog"
)

type CreateServiceRequest struct {
	TenantID        string                   `json:"tenant_id"`
	ProjectID       string                   `json:"project_id"`
	Name            string                   `json:"name" binding:"required"`
	RequestPrefix   string                   `json:"request_prefix" binding:"required"`
	Description     string                   `json:"description"`
	DefaultProvider string                   `json:"default_provider"`
	DefaultModel    string                   `json:"default_model"`
	Enabled         bool                     `json:"enabled"`
	Config          repository.ServiceConfig `json:"config"`
	AutoPublish     bool                     `json:"auto_publish"`
}

func (h *AdminHandler) CreateService(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var req CreateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	tenantID := identity.TenantID
	if identity.Role == repository.RoleSuperAdmin && strings.TrimSpace(req.TenantID) != "" {
		tenantID = req.TenantID
	}
	if !h.ensureServiceDefaultProvider(c, tenantID, req.DefaultProvider) {
		return
	}

	result, err := h.catalogSvc.CreateServiceWithOptions(c.Request.Context(), repository.CreateServiceParams{
		TenantID:        tenantID,
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		RequestPrefix:   req.RequestPrefix,
		Description:     req.Description,
		DefaultProvider: req.DefaultProvider,
		DefaultModel:    req.DefaultModel,
		Enabled:         req.Enabled,
		Config:          req.Config,
	}, catalog.CreateServiceOptions{AutoPublish: req.AutoPublish})
	if err != nil {
		writeInternalError(c, err)
		return
	}

	writeOK(c, gin.H{
		"service":         serviceToResponse(*result.Service),
		"initial_version": serviceVersionToResponse(result.InitialVersion),
	})
	h.recordAudit(c, "service.create", "service", result.Service.ID, req)
}

func (h *AdminHandler) ListServices(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	var enabled *bool
	if raw := c.Query("enabled"); raw != "" {
		value := raw == "true"
		enabled = &value
	}
	services, err := h.consoleSvc.ListServices(c.Request.Context(), identity, repository.ServiceFilter{
		ProjectID:     c.Query("project_id"),
		PublishStatus: c.Query("publish_status"),
		Enabled:       enabled,
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(services))
	for _, item := range services {
		result = append(result, serviceToResponse(item))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetService(c *gin.Context) {
	identity, _ := middleware.Identity(c)
	result, err := h.consoleSvc.GetService(c.Request.Context(), identity, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"service":  serviceToResponse(*result.Service),
		"versions": serviceVersionsToResponse(result.Versions),
	})
}

type UpdateServiceRequest struct {
	ProjectID       *string                   `json:"project_id"`
	Name            *string                   `json:"name"`
	RequestPrefix   *string                   `json:"request_prefix"`
	Description     *string                   `json:"description"`
	DefaultProvider *string                   `json:"default_provider"`
	DefaultModel    *string                   `json:"default_model"`
	Enabled         *bool                     `json:"enabled"`
	Config          *repository.ServiceConfig `json:"config"`
}

func (h *AdminHandler) UpdateService(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req UpdateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	if req.DefaultProvider != nil && !h.ensureServiceDefaultProvider(c, tenantID, *req.DefaultProvider) {
		return
	}
	record, err := h.catalogSvc.UpdateService(c.Request.Context(), tenantID, c.Param("id"), repository.UpdateServiceParams{
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		RequestPrefix:   req.RequestPrefix,
		Description:     req.Description,
		DefaultProvider: req.DefaultProvider,
		DefaultModel:    req.DefaultModel,
		Enabled:         req.Enabled,
		Config:          req.Config,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "service.update", "service", record.ID, req)
	writeOK(c, serviceToResponse(*record))
}

func (h *AdminHandler) ensureServiceDefaultProvider(c *gin.Context, tenantID, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	providers, err := h.store.ListTenantProviders(c.Request.Context(), tenantID)
	if err != nil {
		writeInternalError(c, err)
		return false
	}
	for _, item := range providers {
		if item == name {
			if _, ok := h.providerMgr.Registry(name); ok {
				return true
			}
			if _, ok := h.providerMgr.Get(name); ok {
				return true
			}
			break
		}
	}
	writeError(c, http.StatusBadRequest, CodeBadRequest, "default_provider is not available for tenant")
	return false
}

func (h *AdminHandler) DeleteService(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	if err := h.catalogSvc.DeleteService(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "service.delete", "service", c.Param("id"), nil)
	writeOK(c, gin.H{"id": c.Param("id"), "deleted": true})
}

func (h *AdminHandler) ListServiceVersions(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	versions, err := h.catalogSvc.ListServiceVersions(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, serviceVersionsToResponse(versions))
}

func (h *AdminHandler) CreateServiceVersion(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	version, err := h.catalogSvc.CreateServiceVersion(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "service_version.create", "service", c.Param("id"), gin.H{"service_id": c.Param("id"), "version_id": version.ID})
	writeOK(c, serviceVersionToResponse(version))
}

type PublishServiceRequest struct {
	VersionID string `json:"version_id" binding:"required"`
	Mode      string `json:"mode"`
}

func (h *AdminHandler) PublishServiceVersion(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req PublishServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	record, version, err := h.catalogSvc.PublishServiceVersion(c.Request.Context(), tenantID, c.Param("id"), req.VersionID, req.Mode)
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service/version not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"service": serviceToResponse(*record),
		"version": serviceVersionToResponse(version),
	})
	h.recordAudit(c, "service.publish", "service", record.ID, req)
}

func (h *AdminHandler) PromoteStagedServiceVersion(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	record, version, err := h.catalogSvc.PromoteStagedServiceVersion(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "staged version not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"service": serviceToResponse(*record),
		"version": serviceVersionToResponse(version),
	})
	h.recordAudit(c, "service.promote", "service", record.ID, gin.H{"service_id": record.ID, "version_id": version.ID})
}

type RollbackServiceRequest struct {
	VersionID string `json:"version_id" binding:"required"`
}

func (h *AdminHandler) RollbackServiceVersion(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req RollbackServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	record, version, err := h.catalogSvc.RollbackServiceVersion(c.Request.Context(), tenantID, c.Param("id"), req.VersionID)
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service/version not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, gin.H{
		"service": serviceToResponse(*record),
		"version": serviceVersionToResponse(version),
	})
	h.recordAudit(c, "service.rollback", "service", record.ID, req)
}

type CreateServiceSubscriptionRequest struct {
	ProjectID             string   `json:"project_id"`
	ConsumerName          string   `json:"consumer_name" binding:"required"`
	ConsumerEmail         string   `json:"consumer_email"`
	ConsumerUserID        string   `json:"consumer_user_id"`
	RequestedBudgetUSD    float64  `json:"requested_budget_usd"`
	RequestedRateLimitQPS int      `json:"requested_rate_limit_qps"`
	AllowedSurfaces       []string `json:"allowed_surfaces"`
}

func (h *AdminHandler) CreateServiceSubscription(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req CreateServiceSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	subscription, err := h.catalogSvc.CreateSubscription(c.Request.Context(), tenantID, c.Param("id"), repository.CreateServiceSubscriptionParams{
		ProjectID:             req.ProjectID,
		ConsumerName:          req.ConsumerName,
		ConsumerEmail:         req.ConsumerEmail,
		ConsumerUserID:        req.ConsumerUserID,
		RequestedBudgetUSD:    req.RequestedBudgetUSD,
		RequestedRateLimitQPS: req.RequestedRateLimitQPS,
		AllowedSurfaces:       req.AllowedSurfaces,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.recordAudit(c, "service_subscription.create", "service_subscription", subscription.ID, req)
	writeOK(c, serviceSubscriptionToResponse(*subscription))
}

func (h *AdminHandler) ListServiceSubscriptions(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	items, err := h.catalogSvc.ListSubscriptions(c.Request.Context(), tenantID, c.Param("id"), repository.ServiceSubscriptionFilter{
		Status: c.Query("status"),
	})
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "service not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, serviceSubscriptionToResponse(item))
	}
	writeOK(c, result)
}

func (h *AdminHandler) GetServiceSubscription(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	record, err := h.catalogSvc.GetSubscription(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "subscription not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	writeOK(c, serviceSubscriptionToResponse(*record))
}

type ReviewServiceSubscriptionRequest struct {
	Decision   string `json:"decision" binding:"required"`
	ReviewNote string `json:"review_note"`
}

func (h *AdminHandler) ReviewServiceSubscription(c *gin.Context) {
	tenantID := h.adminTenantID(c)
	var req ReviewServiceSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	result, err := h.catalogSvc.ReviewSubscription(c.Request.Context(), tenantID, c.Param("id"), req.Decision, req.ReviewNote)
	if err != nil {
		if err == repository.ErrNotFound {
			writeError(c, http.StatusNotFound, CodeServiceNotFound, "subscription not found")
			return
		}
		writeInternalError(c, err)
		return
	}
	payload := gin.H{
		"subscription": serviceSubscriptionToResponse(*result.Subscription),
	}
	if result.APIKey != nil {
		payload["api_key"] = apiKeyToResponse(*result.APIKey)
		payload["api_secret"] = result.APISecret
		payload["token"] = result.APIKey.Key + ":" + result.APISecret
	}
	h.recordAudit(c, "service_subscription.review", "service_subscription", result.Subscription.ID, req)
	writeOK(c, payload)
}

func serviceToResponse(record repository.ServiceRecord) gin.H {
	return gin.H{
		"id":                   record.ID,
		"tenant_id":            record.TenantID,
		"project_id":           record.ProjectID,
		"project_slug":         record.ProjectSlug,
		"name":                 record.Name,
		"request_prefix":       record.RequestPrefix,
		"description":          record.Description,
		"default_provider":     record.DefaultProvider,
		"default_model":        record.DefaultModel,
		"publish_status":       record.PublishStatus,
		"published_version_id": record.PublishedVersionID,
		"staged_version_id":    record.StagedVersionID,
		"enabled":              record.Enabled,
		"config":               record.Config,
		"created_at":           record.CreatedAt,
		"updated_at":           record.UpdatedAt,
	}
}

func serviceVersionToResponse(record *repository.ServiceVersionRecord) gin.H {
	if record == nil {
		return nil
	}
	return gin.H{
		"id":         record.ID,
		"service_id": record.ServiceID,
		"tenant_id":  record.TenantID,
		"version":    record.Version,
		"status":     record.Status,
		"snapshot":   record.Snapshot,
		"created_at": record.CreatedAt,
		"updated_at": record.UpdatedAt,
	}
}

func serviceVersionsToResponse(items []repository.ServiceVersionRecord) []gin.H {
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		copied := item
		result = append(result, serviceVersionToResponse(&copied))
	}
	return result
}

func serviceSubscriptionToResponse(record repository.ServiceSubscriptionRecord) gin.H {
	return gin.H{
		"id":                       record.ID,
		"tenant_id":                record.TenantID,
		"service_id":               record.ServiceID,
		"project_id":               record.ProjectID,
		"project_slug":             record.ProjectSlug,
		"consumer_name":            record.ConsumerName,
		"consumer_email":           record.ConsumerEmail,
		"consumer_user_id":         record.ConsumerUserID,
		"status":                   record.Status,
		"requested_budget_usd":     record.RequestedBudgetUSD,
		"requested_rate_limit_qps": record.RequestedRateLimitQPS,
		"allowed_surfaces":         record.AllowedSurfaces,
		"approved_api_key_id":      record.ApprovedAPIKeyID,
		"approved_user_id":         record.ApprovedUserID,
		"review_note":              record.ReviewNote,
		"approved_at":              record.ApprovedAt,
		"created_at":               record.CreatedAt,
		"updated_at":               record.UpdatedAt,
	}
}
