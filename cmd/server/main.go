package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gateyes/internal/app"
	"gateyes/internal/config"
	"gateyes/internal/handler"
)

var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // set via ldflags in CI.
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		BuildType: BuildType,
	}

	application, err := app.New(cfg, buildInfo)
	if err != nil {
		log.Fatalf("bootstrap app: %v", err)
	}
	defer application.Cleanup()

	go func() {
		if serveErr := application.Server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Fatalf("listen server: %v", serveErr)
		}
	}()

	log.Printf("gateyes started on %s", application.Server.Addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutdown signal received")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := application.Server.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}
