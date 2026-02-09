package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/middleware"
	"gateyes/internal/router"
	"gateyes/internal/server"
)

func main() {
	configPath := flag.String("config", "config/gateyes.json", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Warn("failed to load config, using defaults", "error", err)
		cfg = config.DefaultConfig()
	}

	metrics := middleware.NewMetrics(cfg.Metrics.Namespace)
	handler, err := router.New(&cfg, metrics)
	if err != nil {
		slog.Error("failed to build router", "error", err)
		os.Exit(1)
	}

	srv := server.New(&cfg, handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateyes listening", "addr", cfg.Server.ListenAddr)
		if err := srv.Start(); err != nil {
			if err == server.ErrServerClosed {
				return
			}
			slog.Error("server stopped with error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
