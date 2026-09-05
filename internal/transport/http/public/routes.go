// Package public owns route registration for the public and service-runtime
// HTTP surfaces. Request handling remains on the application handler through
// the narrow Handler contract below, keeping transport independent of it.
package public

import (
	"github.com/gin-gonic/gin"

	mw "github.com/gateyes/gateway/internal/handler/middleware"
)

type Handler interface {
	Health(*gin.Context)
	Ready(*gin.Context)
	Metrics(*gin.Context)
	GetResponse(*gin.Context)
	Models(*gin.Context)
	CreateBatch(*gin.Context)
	ListBatches(*gin.Context)
	GetBatch(*gin.Context)
	CancelBatch(*gin.Context)
	ListBatchItems(*gin.Context)
	Responses(*gin.Context)
	Chat(*gin.Context)
	AnthropicMessages(*gin.Context)
	Embeddings(*gin.Context)
	ImageGenerations(*gin.Context)
	ServiceResponses(*gin.Context)
	ServiceChat(*gin.Context)
	ServiceMessages(*gin.Context)
	ServiceInvoke(*gin.Context)
}

// RegisterRoutes installs health, public v1, and service-runtime routes. The
// middleware order intentionally matches the legacy registration in handler.
func RegisterRoutes(engine *gin.Engine, h Handler, middleware *mw.Middleware) {
	engine.GET("/health", h.Health)
	engine.GET("/ready", h.Ready)
	engine.GET("/metrics", h.Metrics)

	v1 := engine.Group("/v1")
	if middleware != nil {
		v1.Use(middleware.Auth())
	}
	v1.GET("/responses/:id", h.GetResponse)
	v1.GET("/models", h.Models)
	v1.POST("/batches", h.CreateBatch)
	v1.GET("/batches", h.ListBatches)
	v1.GET("/batches/:id", h.GetBatch)
	v1.POST("/batches/:id/cancel", h.CancelBatch)
	v1.GET("/batches/:id/items", h.ListBatchItems)

	llm := v1.Group("")
	if middleware != nil {
		llm.Use(middleware.GuardLLMRequest())
	}
	llm.POST("/responses", h.Responses)
	llm.POST("/chat/completions", h.Chat)
	llm.POST("/messages", h.AnthropicMessages)
	llm.POST("/embeddings", h.Embeddings)
	llm.POST("/images/generations", h.ImageGenerations)

	service := engine.Group("/service/:prefix")
	if middleware != nil {
		service.Use(middleware.Auth())
	}
	service.POST("/responses", h.ServiceResponses)
	service.POST("/chat/completions", h.ServiceChat)
	service.POST("/messages", h.ServiceMessages)
	service.POST("/invoke", h.ServiceInvoke)
}
