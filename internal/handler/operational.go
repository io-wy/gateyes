package handler

import (
	"net/http"

	"gateyes/internal/middleware"

	"github.com/gin-gonic/gin"
)

type OperationalHandler struct {
	cache *middleware.CacheMiddleware
}

func NewOperationalHandler(cache *middleware.CacheMiddleware) *OperationalHandler {
	return &OperationalHandler{cache: cache}
}

func (h *OperationalHandler) Health(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func (h *OperationalHandler) CacheStats(c *gin.Context) {
	if h == nil || h.cache == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, h.cache.GetStats())
}
