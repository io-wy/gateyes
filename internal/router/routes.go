package router

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func normalizePrefix(prefix string) string {
	clean := strings.TrimSpace(prefix)
	if clean == "" {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = strings.TrimSuffix(clean, "/")
	if clean == "" {
		return "/"
	}
	return clean
}

func registerProxy(engine *gin.Engine, prefix string, handler gin.HandlerFunc) {
	if prefix == "/" {
		engine.Any("/", handler)
		engine.Any("/*proxyPath", handler)
		return
	}

	engine.Any(prefix, handler)
	engine.Any(prefix+"/*proxyPath", handler)
}
