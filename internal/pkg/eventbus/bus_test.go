package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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

type fakeMetrics struct {
	dropped   atomic.Int64
	processed atomic.Int64
	panics    atomic.Int64
	queueSize atomic.Int64
}

func (f *fakeMetrics) IncEventBusDropped()   { f.dropped.Add(1) }
func (f *fakeMetrics) IncEventBusProcessed() { f.processed.Add(1) }
func (f *fakeMetrics) IncEventBusPanics()    { f.panics.Add(1) }
func (f *fakeMetrics) SetEventBusQueueSize(size int) {
	f.queueSize.Store(int64(size))
}

func TestMetricsObserved(t *testing.T) {
	fm := &fakeMetrics{}
	bus := New(Options{Buffer: 2, Workers: 1, Metrics: fm})
	bus.Start(context.Background())
	defer bus.Close()

	if !bus.Publish(func(ctx context.Context) {}) {
		t.Fatal("first publish should succeed")
	}
	if !bus.Publish(func(ctx context.Context) { panic("boom") }) {
		t.Fatal("second publish should succeed")
	}
	if bus.Publish(func(ctx context.Context) {}) {
		t.Fatal("third publish should drop")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && fm.processed.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := fm.dropped.Load(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if got := fm.processed.Load(); got != 2 {
		t.Fatalf("processed = %d, want 2", got)
	}
	if got := fm.panics.Load(); got != 1 {
		t.Fatalf("panics = %d, want 1", got)
	}
}

func redisTestBus(t *testing.T) (*Bus, *redis.Client, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := New(Options{
		Buffer:  4,
		Workers: 1,
		Stream: StreamOptions{
			Redis:         rdb,
			StreamName:    "test:events",
			ConsumerGroup: "test-group",
			ConsumerName:  "test-consumer",
			ReadBlock:     100 * time.Millisecond,
			ClaimMinIdle:  200 * time.Millisecond,
			ClaimInterval: 100 * time.Millisecond,
		},
	})
	return bus, rdb, mr
}

func TestRedisStreamPublishAndConsume(t *testing.T) {
	bus, rdb, mr := redisTestBus(t)
	defer bus.Close()
	defer mr.Close()

	got := make(chan string, 1)
	bus.RegisterEventHandler("test:echo", func(ctx context.Context, payload []byte) error {
		got <- string(payload)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	if !bus.PublishEvent(context.Background(), Event{Type: "test:echo", Payload: []byte("hello")}) {
		t.Fatal("PublishEvent returned false")
	}

	select {
	case v := <-got:
		if v != "hello" {
			t.Fatalf("payload = %q, want hello", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	// Give ACK time to land, then assert no pending messages.
	time.Sleep(100 * time.Millisecond)
	pending, err := rdb.XPending(context.Background(), "test:events", "test-group").Result()
	if err != nil {
		t.Fatalf("XPending: %v", err)
	}
	if pending.Count != 0 {
		t.Fatalf("pending count = %d, want 0", pending.Count)
	}
}

func TestRedisStreamHandlerErrorRedelivers(t *testing.T) {
	bus, rdb, mr := redisTestBus(t)
	defer bus.Close()
	defer mr.Close()

	var calls atomic.Int32
	bus.RegisterEventHandler("test:retry", func(ctx context.Context, payload []byte) error {
		if calls.Add(1) == 1 {
			return errors.New("transient")
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	if !bus.PublishEvent(context.Background(), Event{Type: "test:retry", Payload: []byte("x")}) {
		t.Fatal("PublishEvent returned false")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2 (fail + redeliver)", calls.Load())
	}

	// After success the message must be ACKed.
	time.Sleep(100 * time.Millisecond)
	pending, err := rdb.XPending(context.Background(), "test:events", "test-group").Result()
	if err != nil {
		t.Fatalf("XPending: %v", err)
	}
	if pending.Count != 0 {
		t.Fatalf("pending count = %d, want 0 after ACK", pending.Count)
	}
}

func TestRedisStreamFallbackWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := New(Options{
		Buffer:  4,
		Workers: 1,
		Stream: StreamOptions{
			Redis:         rdb,
			StreamName:    "test:events",
			ConsumerGroup: "test-group",
			ConsumerName:  "test-consumer",
		},
	})

	var called atomic.Bool
	bus.RegisterEventHandler("test:fallback", func(ctx context.Context, payload []byte) error {
		called.Store(true)
		return nil
	})

	bus.Start(context.Background())
	defer bus.Close()
	mr.Close() // kill redis before publish

	if !bus.PublishEvent(context.Background(), Event{Type: "test:fallback", Payload: []byte("x")}) {
		t.Fatal("PublishEvent returned false, expected in-memory fallback")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !called.Load() {
		time.Sleep(50 * time.Millisecond)
	}
	if !called.Load() {
		t.Fatal("in-memory fallback handler was not called after Redis failure")
	}
}
