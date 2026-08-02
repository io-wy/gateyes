package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func (h *AdminHandler) GetCatalog(c *gin.Context) {
	tenantID := h.adminTenantID(c)

	providers := h.providerResponses(c, tenantID)
	services, err := h.store.ListServices(c.Request.Context(), tenantID, repository.ServiceFilter{
		ProjectID:     c.Query("project_id"),
		PublishStatus: c.Query("publish_status"),
	})
	if err != nil {
		writeInternalError(c, err)
		return
	}

	serviceItems := make([]gin.H, 0, len(services))
	for _, service := range services {
		serviceItems = append(serviceItems, serviceToResponse(service))
	}

	writeOK(c, gin.H{
		"tenant_id": tenantID,
		"counts": gin.H{
			"providers": len(providers),
			"services":  len(serviceItems),
		},
		"providers": providers,
		"services":  serviceItems,
	})
}
