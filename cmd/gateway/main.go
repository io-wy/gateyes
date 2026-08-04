package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/gateyes/gateway/internal/app/gateway"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	if err := gateway.Run(context.Background(), *configPath); err != nil {
		slog.Error("gateway stopped with error", "error", err)
		os.Exit(1)
	}
}
