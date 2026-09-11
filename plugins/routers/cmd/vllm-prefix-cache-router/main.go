package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gateyes/gateway/plugins/routers/internal/prefixaware"
	"github.com/gateyes/gateway/plugins/routers/internal/server"
)

func main() {
	address := flag.String("listen", ":50051", "gRPC listen address")
	minMatchLength := flag.Int("prefix-min-match-length", 0, "minimum matched prompt characters before prefix routing is used")
	chunkSize := flag.Int("chunk-size", 128, "prompt chunk size in Unicode characters; vLLM default is 128")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("starting vLLM prefix-cache router", "address", *address, "prefix_min_match_length", *minMatchLength, "chunk_size", *chunkSize)
	if err := server.Serve(ctx, *address, prefixaware.New(prefixaware.Config{
		PrefixMinMatchLength: *minMatchLength,
		ChunkSize:            *chunkSize,
	})); err != nil {
		slog.Error("vLLM prefix-cache router stopped", "error", err)
		os.Exit(1)
	}
}
