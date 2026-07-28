package handler

import "github.com/gin-gonic/gin"

func (h *AdminHandler) GetCacheSummary(c *gin.Context) {
	if h.metrics == nil {
		writeOK(c, CacheSummary{Enabled: false})
		return
	}
	writeOK(c, h.metrics.CacheSummary())
}
