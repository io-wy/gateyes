package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishAndProcess(t *testing.T) {
	bus := New(Options{Buffer: 16, Workers: 2})
	bus.Start(context.Background())
	defer bus.Close()

	var counter atomic.Int64
	for i := 0; i < 8; i++ {
		ok := bus.Publish(func(ctx context.Context) {
			counter.Add(1)
		})
		if !ok {
			t.Fatalf("Publish() iter %d returned false", i)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && counter.Load() < 8 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := counter.Load(); got != 8 {
		t.Fatalf("counter = %d, want 8", got)
	}
	if got := bus.Processed(); got != 8 {
		t.Fatalf("Processed() = %d, want 8", got)
	}
}

func TestPublishDropsWhenFull(t *testing.T) {
	// Buffer 1, single worker, blocking handler => second publish should drop.
	release := make(chan struct{})
	bus := New(Options{Buffer: 1, Workers: 1})
	bus.Start(context.Background())
	defer func() {
		close(release)
		bus.Close()
	}()

	// First publish occupies the worker; the handler blocks until release.
	if !bus.Publish(func(ctx context.Context) { <-release }) {
		t.Fatal("first Publish returned false")
	}
	// Wait for worker to start consuming so buffer is empty again,
	// then fill buffer.
	time.Sleep(50 * time.Millisecond)
	if !bus.Publish(func(ctx context.Context) {}) {
		t.Fatal("second Publish (fills buffer) returned false")
	}
	// Now buffer is full and worker is busy. Third publish should drop.
	if bus.Publish(func(ctx context.Context) {}) {
		t.Fatal("third Publish returned true, expected drop")
	}
	if got := bus.Dropped(); got != 1 {
		t.Fatalf("Dropped() = %d, want 1", got)
	}
}

func TestCloseDrainsPendingWork(t *testing.T) {
	bus := New(Options{Buffer: 32, Workers: 2})
	bus.Start(context.Background())

	var counter atomic.Int64
	for i := 0; i < 16; i++ {
		bus.Publish(func(ctx context.Context) {
			time.Sleep(2 * time.Millisecond)
			counter.Add(1)
		})
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if got := counter.Load(); got != 16 {
		t.Fatalf("counter after Close = %d, want 16 (drain failed)", got)
	}
}

func TestPublishAfterCloseReturnsFalse(t *testing.T) {
	bus := New(Options{Buffer: 4, Workers: 1})
	bus.Start(context.Background())
	if err := bus.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if bus.Publish(func(ctx context.Context) {}) {
		t.Fatal("Publish after Close returned true")
	}
}

func TestPanicInHandlerDoesNotKillWorker(t *testing.T) {
	bus := New(Options{Buffer: 4, Workers: 1})
	bus.Start(context.Background())
	defer bus.Close()

	var ok atomic.Bool
	bus.Publish(func(ctx context.Context) { panic("boom") })

	wg := sync.WaitGroup{}
	wg.Add(1)
	bus.Publish(func(ctx context.Context) {
		ok.Store(true)
		wg.Done()
	})
	wg.Wait()
	if !ok.Load() {
		t.Fatal("worker did not survive prior panic")
	}
}

func TestHandlerContextHasTimeout(t *testing.T) {
	bus := New(Options{Buffer: 4, Workers: 1, HandlerTimeout: 50 * time.Millisecond})
	bus.Start(context.Background())
	defer bus.Close()

	got := make(chan time.Duration, 1)
	bus.Publish(func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if !ok {
			got <- 0
			return
		}
		got <- time.Until(deadline)
	})
	select {
	case d := <-got:
		if d <= 0 || d > 100*time.Millisecond {
			t.Fatalf("handler ctx deadline = %v, want ~50ms", d)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not run")
	}
}

func TestStartIsIdempotent(t *testing.T) {
	bus := New(Options{Buffer: 4, Workers: 1})
	bus.Start(context.Background())
	bus.Start(context.Background())
	defer bus.Close()
	// Should not deadlock or double-spawn workers.
}

func TestPublishNilHandlerReturnsFalse(t *testing.T) {
	bus := New(Options{Buffer: 4, Workers: 1})
	bus.Start(context.Background())
	defer bus.Close()
	if bus.Publish(nil) {
		t.Fatal("Publish(nil) returned true")
	}
}
