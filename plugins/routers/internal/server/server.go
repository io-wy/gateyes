package server

import (
	"context"
	"errors"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

const gracefulStopTimeout = 5 * time.Second

func New(router pluginv1.RouterPluginServer) *grpc.Server {
	grpcServer := grpc.NewServer()
	pluginv1.RegisterRouterPluginServer(grpcServer, router)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(pluginv1.RouterPlugin_ServiceDesc.ServiceName, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	return grpcServer
}

func Serve(ctx context.Context, address string, router pluginv1.RouterPluginServer) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	return ServeListener(ctx, listener, router)
}

func ServeListener(ctx context.Context, listener net.Listener, router pluginv1.RouterPluginServer) error {
	grpcServer := New(router)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- grpcServer.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(gracefulStopTimeout):
		grpcServer.Stop()
	}
	err := <-serveErr
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}
