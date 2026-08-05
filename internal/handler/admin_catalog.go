package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
)

func (h *AdminHandler) GetCatalog(c *gin.Context) {
	identity, _ := middleware.Identity(c)

	view, err := h.consoleSvc.Catalog(c.Request.Context(), identity, c.Query("project_id"), c.Query("publish_status"))
	if err != nil {
		writeInternalError(c, err)
		return
	}

	providers := providerViewsToResponses(view.Providers)
	serviceItems := make([]gin.H, 0, len(view.Services))
	for _, service := range view.Services {
		serviceItems = append(serviceItems, serviceToResponse(service))
	}

	writeOK(c, gin.H{
		"tenant_id": view.TenantID,
		"counts": gin.H{
			"providers": len(providers),
			"services":  len(serviceItems),
		},
		"providers": providers,
		"services":  serviceItems,
	})
}
