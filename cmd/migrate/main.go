package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	action := flag.String("action", "up", "migration action: up or status")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	switch *action {
	case "up":
		if err := database.Migrate(context.Background()); err != nil {
			slog.Error("failed to apply migrations", "error", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	case "status":
		if err := database.Migrate(context.Background()); err != nil {
			slog.Error("migration status check failed", "error", err)
			os.Exit(1)
		}
		slog.Info("database schema is up to date")
	default:
		slog.Error("unsupported action", "action", *action)
		fmt.Fprintf(os.Stderr, "unsupported action: %s\n", *action)
		os.Exit(2)
	}
}
