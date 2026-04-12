package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

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

func TestServerBuildHTTPServerUsesLifecycleTimeouts(t *testing.T) {
	srv := &Server{
		cfg: config.ServerConfig{
			ListenAddr:      ":8080",
			ReadTimeout:     3,
			WriteTimeout:    7,
			IdleTimeout:     11,
			ShutdownTimeout: 13,
		},
		engine: gin.New(),
	}

	httpSrv := srv.buildHTTPServer()
	if httpSrv == nil {
		t.Fatal("buildHTTPServer() = nil, want configured server")
	}
	if httpSrv.Addr != ":8080" {
		t.Fatalf("buildHTTPServer() addr = %q, want :8080", httpSrv.Addr)
	}
	if httpSrv.ReadTimeout != 3*time.Second || httpSrv.WriteTimeout != 7*time.Second || httpSrv.IdleTimeout != 11*time.Second {
		t.Fatalf("buildHTTPServer() timeouts = (%s,%s,%s), want (3s,7s,11s)", httpSrv.ReadTimeout, httpSrv.WriteTimeout, httpSrv.IdleTimeout)
	}
	if _, ok := httpSrv.Handler.(*gin.Engine); !ok {
		t.Fatalf("buildHTTPServer() handler = %T, want *gin.Engine", httpSrv.Handler)
	}
}

func TestServerShutdownWithoutStartReturnsNilWhenServerPreset(t *testing.T) {
	srv := &Server{
		cfg:    config.ServerConfig{ShutdownTimeout: 5},
		engine: gin.New(),
		srv:    &http.Server{},
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() preset server error = %v, want nil", err)
	}
}
