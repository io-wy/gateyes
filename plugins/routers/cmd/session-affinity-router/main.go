package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gateyes/gateway/plugins/routers/internal/server"
	"github.com/gateyes/gateway/plugins/routers/internal/sessionaffinity"
)

func main() {
	address := flag.String("listen", ":50053", "gRPC listen address")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("starting session-affinity router", "address", *address)
	if err := server.Serve(ctx, *address, sessionaffinity.New()); err != nil {
		slog.Error("session-affinity router stopped", "error", err)
		os.Exit(1)
	}
}
