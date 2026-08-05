package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
)

func (h *AdminHandler) Me(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok || identity == nil {
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid identity")
		return
	}

	me, err := h.consoleSvc.Me(c.Request.Context(), identity)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	writeOK(c, me)
}
