package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

// reverseRouterPlugin orders candidates alphabetically in reverse,
// so we can verify the plugin actually changed the order.
type reverseRouterPlugin struct {
	pluginv1.UnimplementedRouterPluginServer
}

func (s *reverseRouterPlugin) OrderCandidates(ctx context.Context, req *pluginv1.OrderCandidatesRequest) (*pluginv1.OrderCandidatesResponse, error) {
	names := make([]string, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		names = append(names, c.Name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	slog.Info("[GRPC_ROUTER_PLUGIN] received candidates", "count", len(names), "ordered", names)
	return &pluginv1.OrderCandidatesResponse{OrderedNames: names}, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		panic(err)
	}
	s := grpc.NewServer()
	pluginv1.RegisterRouterPluginServer(s, &reverseRouterPlugin{})

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	logger.Info("gRPC router plugin listening", "addr", ":50051")
	if err := s.Serve(lis); err != nil {
		panic(err)
	}
}
