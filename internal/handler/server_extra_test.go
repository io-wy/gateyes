package handler

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
)

func TestServerStartInvalidAddressAndShutdown(t *testing.T) {
	srv := &Server{
		cfg: config.ServerConfig{
			ListenAddr: "bad::addr",
		},
		engine: gin.New(),
	}
	if err := srv.Start(); err == nil {
		t.Fatal("Server.Start(invalid addr) error = nil, want non-nil")
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Server.Shutdown() error: %v", err)
	}
}
