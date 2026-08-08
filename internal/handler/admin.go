package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/adminconsole"
	authSvc "github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

type AdminHandler struct {
	store              repository.Store
	providerMgr        *provider.Manager
	providerRuntimeSvc *provider.RuntimeRegistryService
	authSvc            *authSvc.Auth
	consoleSvc         *adminconsole.Service
	catalogSvc         *catalog.Service
	routerSvc          *router.Router
	reloader           *config.Reloader
	healthChecker      *provider.HealthChecker
	metrics            *Metrics
	pluginDir          string
	configuredPlugins  []repository.PluginRecord
	startedAt          time.Time
}

func NewAdminHandler(store repository.Store, providerMgr *provider.Manager, catalogSvc *catalog.Service, reloader *config.Reloader) *AdminHandler {
	providerRuntimeSvc := provider.NewRuntimeRegistryService(store, providerMgr)
	return &AdminHandler{
		store:              store,
		providerMgr:        providerMgr,
		providerRuntimeSvc: providerRuntimeSvc,
		consoleSvc:         adminconsole.New(store, catalogSvc, providerRuntimeSvc),
		catalogSvc:         catalogSvc,
		reloader:           reloader,
		pluginDir:          "./plugins",
		startedAt:          time.Now(),
	}
}

func (h *AdminHandler) SetHealthChecker(hc *provider.HealthChecker) {
	h.healthChecker = hc
}

func (h *AdminHandler) SetMetrics(metrics *Metrics) {
	h.metrics = metrics
}

func (h *AdminHandler) SetRouter(routerSvc *router.Router) {
	h.routerSvc = routerSvc
}

func (h *AdminHandler) SetAuthService(authService *authSvc.Auth) {
	h.authSvc = authService
}

func (h *AdminHandler) SetPluginDirectory(dir string) {
	if dir != "" {
		h.pluginDir = dir
	}
}

func (h *AdminHandler) SetConfiguredPlugins(grpcPlugins []config.GRPCPluginConfig, wasmPlugins []config.WASMPluginConfig) {
	plugins := make([]repository.PluginRecord, 0, len(grpcPlugins)+len(wasmPlugins))
	now := h.startedAt
	if now.IsZero() {
		now = time.Now()
	}
	for _, item := range grpcPlugins {
		record := configuredGRPCPluginRecord(item, now)
		if record.Name != "" {
			plugins = append(plugins, record)
		}
	}
	for _, item := range wasmPlugins {
		record := configuredWASMPluginRecord(item, now)
		if record.Name != "" {
			plugins = append(plugins, record)
		}
	}
	h.configuredPlugins = plugins
}

func (h *AdminHandler) ReloadConfig(c *gin.Context) {
	if h.reloader == nil {
		writeError(c, http.StatusNotImplemented, CodeServiceUnavailable, "reloader not configured")
		return
	}
	if err := h.reloader.Reload(c.Request.Context()); err != nil {
		writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
		return
	}
	h.recordAudit(c, "config.reload", "config", "runtime", gin.H{"status": "success"})
	writeOK(c, gin.H{"reloaded": true})
}

func (h *AdminHandler) adminTenantID(c *gin.Context) string {
	identity, _ := middleware.Identity(c)
	if identity.Role == repository.RoleSuperAdmin {
		if tenantID := c.Query("tenant_id"); tenantID != "" {
			return tenantID
		}
	}
	return identity.TenantID
}

func (h *AdminHandler) scopeTenantID(c *gin.Context, identity *repository.AuthIdentity) (string, bool) {
	if identity.Role == repository.RoleSuperAdmin {
		if tenantID := c.Query("tenant_id"); tenantID != "" {
			return tenantID, true
		}
	}
	return identity.TenantID, true
}

func (h *AdminHandler) resolveTargetTenant(c *gin.Context, identity *repository.AuthIdentity, requested string) (string, bool) {
	if identity.Role == repository.RoleSuperAdmin {
		if requested == "" {
			writeError(c, http.StatusBadRequest, CodeMissingRequiredField, "tenant_id is required")
			return "", false
		}
		return requested, true
	}
	return identity.TenantID, true
}

func (h *AdminHandler) invalidateAPIKeyCache(keys ...string) {
	if h.authSvc == nil {
		return
	}
	for _, key := range keys {
		h.authSvc.InvalidateKey(key)
	}
}

func (h *AdminHandler) invalidateAPIKeyIdentity(apiKeyID string) {
	if h.authSvc != nil {
		h.authSvc.InvalidateAPIKey(apiKeyID)
	}
}

func (h *AdminHandler) invalidateUserIdentities(userID string) {
	if h.authSvc != nil {
		h.authSvc.InvalidateUser(userID)
	}
}

func (h *AdminHandler) invalidateProjectIdentities(projectID string) {
	if h.authSvc != nil {
		h.authSvc.InvalidateProject(projectID)
	}
}

func (h *AdminHandler) invalidateTenantIdentities(tenantID string) {
	if h.authSvc != nil {
		h.authSvc.InvalidateTenant(tenantID)
	}
}

func scopedTenant(identity *repository.AuthIdentity) string {
	if identity == nil {
		return ""
	}
	return identity.TenantID
}

func isTenantUser(identity *repository.AuthIdentity) bool {
	return identity != nil && identity.Role == repository.RoleTenantUser
}

func (h *AdminHandler) requireAdminIdentity(c *gin.Context) (*repository.AuthIdentity, string, bool) {
	identity, _ := middleware.Identity(c)
	tenantID, ok := h.scopeTenantID(c, identity)
	if !ok {
		return nil, "", false
	}
	return identity, tenantID, true
}

func (h *AdminHandler) handleNotFound(c *gin.Context, err error, code Code, msg string) bool {
	if err == repository.ErrNotFound {
		writeError(c, http.StatusNotFound, code, msg)
		return true
	}
	return false
}

func writeInternalError(c *gin.Context, err error) {
	writeError(c, http.StatusInternalServerError, CodeInternalError, err.Error())
}

func providerNames(items []provider.Provider) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names
}

func validEntityStatus(value string) bool {
	switch value {
	case repository.StatusActive, repository.StatusInactive, repository.StatusRevoked:
		return true
	default:
		return false
	}
}

func zeroTimeOrValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
