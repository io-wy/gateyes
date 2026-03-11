package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTPBridge struct {
	target http.Handler
}

func NewHTTPBridge(target http.Handler) *HTTPBridge {
	return &HTTPBridge{target: target}
}

func (h *HTTPBridge) Handle(c *gin.Context) {
	if h == nil || h.target == nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	h.target.ServeHTTP(c.Writer, c.Request)
}
