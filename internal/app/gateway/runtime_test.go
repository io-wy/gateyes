package gateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeServer struct {
	started  chan struct{}
	shutdown chan struct{}
	release  chan struct{}
	err      error
}

func (s *fakeServer) Start() error {
	close(s.started)
	<-s.release
	return s.err
}

func (s *fakeServer) Shutdown(context.Context) error {
	close(s.shutdown)
	close(s.release)
	return nil
}

func TestRuntimeRunsHooksAndShutdown(t *testing.T) {
	server := &fakeServer{started: make(chan struct{}), shutdown: make(chan struct{}), release: make(chan struct{}), err: errClosed}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := NewRuntime(server, Options{ServerClosedError: errClosed})
	started := false
	jobRan := make(chan struct{})
	shutdownRan := false
	runtime.OnStart("start", func(context.Context) {
		started = true
	})
	runtime.Go("job", func(context.Context) {
		close(jobRan)
		cancel()
	})
	runtime.OnShutdown("shutdown", func(context.Context) error {
		shutdownRan = true
		return nil
	})

	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !started {
		t.Fatal("start hook did not run")
	}
	select {
	case <-jobRan:
	case <-time.After(time.Second):
		t.Fatal("job did not run")
	}
	select {
	case <-server.shutdown:
	case <-time.After(time.Second):
		t.Fatal("server shutdown did not run")
	}
	if !shutdownRan {
		t.Fatal("shutdown hook did not run")
	}
}

var errClosed = errors.New("server closed")
