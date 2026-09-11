package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gateyes/gateway/plugins/routers/internal/leastload"
	"github.com/gateyes/gateway/plugins/routers/internal/server"
)

func main() {
	address := flag.String("listen", ":50052", "gRPC listen address")
	signalMaxAge := flag.Duration("signal-max-age", 15*time.Second, "maximum age of vLLM inference signals")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("starting least-load router", "address", *address, "signal_max_age", *signalMaxAge)
	if err := server.Serve(ctx, *address, leastload.New(leastload.Config{SignalMaxAge: *signalMaxAge})); err != nil {
		slog.Error("least-load router stopped", "error", err)
		os.Exit(1)
	}
}
