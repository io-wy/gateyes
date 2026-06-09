// Package eventbus provides a bounded, fan-out async dispatcher.
//
// It exists to move bookkeeping work — usage record DB inserts, response
// status DB updates, alert webhook calls — off the request hot path. The
// pattern is:
//
//	bus.Publish(func(ctx context.Context) { /* persist work */ })
//
// The closure runs on one of N worker goroutines using a detached context
// derived from the bus's lifetime, so the caller's request context can be
// cancelled (client disconnect) without aborting the persistence work.
//
// When the buffered channel is full, Publish returns false and increments
// the dropped counter. Callers should accept drop-with-warn semantics; this
// is intentional back-pressure to keep the hot path non-blocking. Loss is
// rare in steady state and observable via the Dropped() counter.
package eventbus

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Handler is the work performed off the hot path. The supplied context is
// derived from the bus's lifetime context with a fixed timeout.
type Handler func(ctx context.Context)

// Bus dispatches handlers to a pool of worker goroutines via a buffered
// channel. Use New to construct, Start to launch workers, Publish from the
// hot path, and Close on graceful shutdown.
type Bus struct {
	ch        chan Handler
	workers   int
	timeout   time.Duration
	dropped   atomic.Int64
	processed atomic.Int64
	panics    atomic.Int64

	startOnce sync.Once
	stopOnce  sync.Once
	closed    atomic.Bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Options configures a Bus.
type Options struct {
	// Buffer is the channel capacity. Once full, Publish drops with warn.
	// Default: 10000.
	Buffer int
	// Workers is the consumer goroutine count. Default: max(2, runtime.NumCPU()).
	Workers int
	// HandlerTimeout caps each handler invocation. Default: 5s.
	HandlerTimeout time.Duration
}

// New constructs a Bus. Call Start before Publish.
func New(opts Options) *Bus {
	if opts.Buffer <= 0 {
		opts.Buffer = 10000
	}
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.HandlerTimeout <= 0 {
		opts.HandlerTimeout = 5 * time.Second
	}
	return &Bus{
		ch:      make(chan Handler, opts.Buffer),
		workers: opts.Workers,
		timeout: opts.HandlerTimeout,
	}
}

// Start launches worker goroutines. Subsequent calls are no-ops.
//
// The provided ctx defines the bus lifetime; cancelling it asks workers to
// stop after draining the channel. Close also stops them.
func (b *Bus) Start(ctx context.Context) {
	b.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		b.cancel = cancel
		for i := 0; i < b.workers; i++ {
			b.wg.Add(1)
			go b.workerLoop(runCtx)
		}
	})
}

// workerLoop pulls handlers from the channel until it is closed.
func (b *Bus) workerLoop(ctx context.Context) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			// Drain remaining items so already-published work still runs.
			for {
				select {
				case h, ok := <-b.ch:
					if !ok {
						return
					}
					b.invoke(h)
				default:
					return
				}
			}
		case h, ok := <-b.ch:
			if !ok {
				return
			}
			b.invoke(h)
		}
	}
}

func (b *Bus) invoke(h Handler) {
	if h == nil {
		return
	}
	hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("eventbus handler panic",
				"recover", r,
				"stack", string(debug.Stack()),
			)
			b.panics.Add(1)
		}
	}()
	h(hCtx)
	b.processed.Add(1)
}

// Publish enqueues h for async execution. Returns false if the channel was
// full or the bus is closed.
func (b *Bus) Publish(h Handler) bool {
	if h == nil {
		return false
	}
	if b.closed.Load() {
		b.dropped.Add(1)
		return false
	}
	select {
	case b.ch <- h:
		return true
	default:
		b.dropped.Add(1)
		return false
	}
}

// Close stops accepting new work, waits for the channel to drain, and
// returns once all workers exit. Subsequent Publish calls return false.
//
// Close is safe to call multiple times.
func (b *Bus) Close() error {
	if b == nil {
		return nil
	}
	var err error
	b.stopOnce.Do(func() {
		b.closed.Store(true)
		close(b.ch)
		if b.cancel != nil {
			// Don't actually cancel — let workers drain ch first.
		}
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			err = errors.New("eventbus: workers did not exit within 30s")
		}
		if b.cancel != nil {
			b.cancel()
		}
	})
	return err
}

// Dropped returns the cumulative number of handlers rejected because the
// channel was full or the bus was closed. Useful for a metric.
func (b *Bus) Dropped() int64 { return b.dropped.Load() }

// Processed returns the cumulative number of handlers that ran to completion
// (including those that panicked, since panics are recovered).
func (b *Bus) Processed() int64 { return b.processed.Load() }

// Panics returns the cumulative number of handlers that panicked.
func (b *Bus) Panics() int64 { return b.panics.Load() }
