package server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
	"github.com/gateyes/gateway/plugins/routers/internal/sessionaffinity"
)

func TestNewRegistersRouterAndHealthServices(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := New(sessionaffinity.New())
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(grpcServer.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error: %v", err)
	}
	defer conn.Close()

	health, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil || health.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Health.Check() = (%v,%v), want SERVING", health, err)
	}

	response, err := pluginv1.NewRouterPluginClient(conn).OrderCandidates(ctx, &pluginv1.OrderCandidatesRequest{
		Candidates: []*pluginv1.Candidate{{Name: "p1"}, {Name: "p2"}},
		Context:    &pluginv1.RouteContext{SessionId: "session-1"},
	})
	if err != nil || len(response.OrderedNames) != 2 {
		t.Fatalf("OrderCandidates() = (%v,%v), want two candidates", response, err)
	}
}

func TestServeStopsWhenContextIsCanceled(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeListener(ctx, listener, sessionaffinity.New())
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeListener() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ServeListener() did not stop after cancellation")
	}
}
