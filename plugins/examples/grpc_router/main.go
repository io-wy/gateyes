package main

import (
	"context"
	"fmt"
	"net"
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
	fmt.Printf("[GRPC_ROUTER_PLUGIN] received %d candidates, ordered reverse: %v\n", len(names), names)
	return &pluginv1.OrderCandidatesResponse{OrderedNames: names}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		panic(err)
	}
	s := grpc.NewServer()
	pluginv1.RegisterRouterPluginServer(s, &reverseRouterPlugin{})

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, hs)

	fmt.Println("gRPC router plugin listening on :50051")
	if err := s.Serve(lis); err != nil {
		panic(err)
	}
}
