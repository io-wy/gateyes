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
//
// # Redis Streams backend
//
// When a *redis.Client is supplied, the bus can also persist typed events to
// a Redis Stream and consume them via a consumer group. This gives the
// bookkeeping path durability across gateway process restarts (provided Redis
// itself is configured with AOF). The closure-based Publish API remains
// in-memory only; durable work should use PublishEvent with a registered
// EventHandler.
package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Handler is the work performed off the hot path. The supplied context is
// derived from the bus's lifetime context with a fixed timeout.
type Handler func(ctx context.Context)

// Event is a serializable, durable work item. Use it with PublishEvent when
// the work must survive a gateway process restart.
type Event struct {
	Type    string `json:"type"`
	Payload []byte `json:"payload"`
}

// EventHandler processes a single durable event. A non-nil error tells the
// consumer not to ACK the message so Redis will redeliver it.
type EventHandler func(ctx context.Context, payload []byte) error

// Event type constants for cross-package coordination.
const (
	EventTypeResponseDetails = "response:details"
	EventTypeResponseUpdate  = "response:update"
)

// Metrics receives lifecycle observations from a Bus. Implementations must be
// safe for concurrent use. A nil Metrics is valid and discards all observations.
type Metrics interface {
	IncEventBusDropped()
	IncEventBusProcessed()
	IncEventBusPanics()
	SetEventBusQueueSize(size int)
}

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

	metrics Metrics

	// handlers map event types to processors. Used by both in-memory and
	// Redis Streams dispatch paths.
	handlers   map[string]EventHandler
	handlersMu sync.RWMutex

	// redis stream backend (optional)
	rdb           *redis.Client
	stream        string
	group         string
	consumer      string
	maxLen        int64
	readBlock     time.Duration
	claimMinIdle  time.Duration
	claimInterval time.Duration
	streamSem     chan struct{}
}

// StreamOptions configures the optional Redis Streams backend.
type StreamOptions struct {
	// Redis client. If nil, the bus runs in in-memory mode only.
	Redis *redis.Client
	// Stream name. Default: "gateyes:events".
	StreamName string
	// Consumer group name. Default: "gateyes".
	ConsumerGroup string
	// Consumer name. Default: os.Hostname() or "gateyes-gateway".
	ConsumerName string
	// MaxLen caps the stream length (approximate). Default: 100000.
	MaxLen int64
	// ReadBlock is how long XReadGroup blocks waiting for new messages.
	// Default: 1s.
	ReadBlock time.Duration
	// ClaimMinIdle is the idle time before a pending message can be claimed
	// by another consumer. Default: 60s.
	ClaimMinIdle time.Duration
	// ClaimInterval controls how often idle pending messages are reclaimed.
	// Default: ClaimMinIdle / 2, minimum 10s.
	ClaimInterval time.Duration
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
	// Metrics receives dropped/processed/panic/queue observations.
	// Optional.
	Metrics Metrics
	// Stream configures the optional Redis Streams backend.
	// Optional; if Redis is nil, the bus is in-memory only.
	Stream StreamOptions
}

// New constructs a Bus. Call Start before Publish or PublishEvent.
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

	stream := opts.Stream
	if stream.Redis != nil {
		if stream.StreamName == "" {
			stream.StreamName = "gateyes:events"
		}
		if stream.ConsumerGroup == "" {
			stream.ConsumerGroup = "gateyes"
		}
		if stream.ConsumerName == "" {
			if host, err := os.Hostname(); err == nil && host != "" {
				stream.ConsumerName = host
			} else {
				stream.ConsumerName = "gateyes-gateway"
			}
		}
		if stream.MaxLen <= 0 {
			stream.MaxLen = 100000
		}
		if stream.ReadBlock <= 0 {
			stream.ReadBlock = time.Second
		}
		if stream.ClaimMinIdle <= 0 {
			stream.ClaimMinIdle = 60 * time.Second
		}
		if stream.ClaimInterval <= 0 {
			stream.ClaimInterval = stream.ClaimMinIdle / 2
			if stream.ClaimInterval < 10*time.Second {
				stream.ClaimInterval = 10 * time.Second
			}
		}
	}

	return &Bus{
		ch:            make(chan Handler, opts.Buffer),
		workers:       opts.Workers,
		timeout:       opts.HandlerTimeout,
		metrics:       opts.Metrics,
		handlers:      make(map[string]EventHandler),
		rdb:           stream.Redis,
		stream:        stream.StreamName,
		group:         stream.ConsumerGroup,
		consumer:      stream.ConsumerName,
		maxLen:        stream.MaxLen,
		readBlock:     stream.ReadBlock,
		claimMinIdle:  stream.ClaimMinIdle,
		claimInterval: stream.ClaimInterval,
		streamSem:     make(chan struct{}, opts.Workers),
	}
}

// SetMetrics wires a metrics receiver after construction. Subsequent calls
// replace the previous receiver. Safe to call with nil to disable observations.
func (b *Bus) SetMetrics(m Metrics) {
	b.metrics = m
}

// RegisterEventHandler registers a handler for durable events of the given
// type. Register handlers before Start. The last registration wins.
func (b *Bus) RegisterEventHandler(eventType string, h EventHandler) {
	if b == nil || h == nil {
		return
	}
	b.handlersMu.Lock()
	defer b.handlersMu.Unlock()
	b.handlers[eventType] = h
}

func (b *Bus) handler(eventType string) EventHandler {
	b.handlersMu.RLock()
	defer b.handlersMu.RUnlock()
	return b.handlers[eventType]
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
		if b.rdb != nil {
			if err := b.ensureGroup(runCtx); err != nil {
				slog.Error("eventbus failed to create redis stream consumer group",
					"stream", b.stream,
					"group", b.group,
					"error", err,
				)
			}
			b.wg.Add(1)
			go b.streamReader(runCtx)
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
					b.observeQueueSize()
					b.invoke(h)
				default:
					return
				}
			}
		case h, ok := <-b.ch:
			if !ok {
				return
			}
			b.observeQueueSize()
			b.invoke(h)
		}
	}
}

func (b *Bus) observeQueueSize() {
	if b.metrics != nil {
		b.metrics.SetEventBusQueueSize(len(b.ch))
	}
}

func (b *Bus) invoke(h Handler) {
	if h == nil {
		return
	}
	defer func() {
		b.processed.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusProcessed()
		}
		if r := recover(); r != nil {
			slog.Error("eventbus handler panic",
				"recover", r,
				"stack", string(debug.Stack()),
			)
			b.panics.Add(1)
			if b.metrics != nil {
				b.metrics.IncEventBusPanics()
			}
		}
	}()
	hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	h(hCtx)
}

// Publish enqueues h for async execution. Returns false if the channel was
// full or the bus is closed.
func (b *Bus) Publish(h Handler) bool {
	if h == nil {
		return false
	}
	if b.closed.Load() {
		b.dropped.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusDropped()
		}
		return false
	}
	select {
	case b.ch <- h:
		b.observeQueueSize()
		return true
	default:
		b.dropped.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusDropped()
		}
		return false
	}
}

// PublishEvent persists a typed event. When Redis Streams is configured, the
// event is written to the stream; otherwise it is dispatched in-memory.
// If Redis is unavailable, the bus falls back to in-memory dispatch so that
// data is not lost. Returns false only when the bus is closed or no handler
// is registered for the event type.
func (b *Bus) PublishEvent(ctx context.Context, e Event) bool {
	if b == nil {
		return false
	}
	if b.closed.Load() {
		b.dropped.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusDropped()
		}
		return false
	}
	if b.rdb != nil {
		if err := b.xadd(ctx, e); err == nil {
			return true
		} else {
			slog.Warn("eventbus redis stream XAdd failed, falling back to in-memory dispatch",
				"stream", b.stream,
				"error", err,
			)
		}
	}
	return b.dispatchEventInMemory(e)
}

func (b *Bus) xadd(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: b.stream,
		MaxLen: b.maxLen,
		Approx: true,
		Values: map[string]interface{}{
			"event": string(body),
		},
	}).Err()
}

func (b *Bus) dispatchEventInMemory(e Event) bool {
	h := b.handler(e.Type)
	if h == nil {
		b.dropped.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusDropped()
		}
		slog.Error("eventbus no handler registered for event type", "type", e.Type)
		return false
	}

	dispatched := b.Publish(func(ctx context.Context) {
		hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
		defer cancel()
		if err := h(hCtx, e.Payload); err != nil {
			slog.Error("eventbus in-memory event handler failed",
				"type", e.Type,
				"error", err,
			)
		}
	})
	if !dispatched {
		// Channel full or bus closing: run synchronously so durable work is
		// not dropped.
		hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
		defer cancel()
		if err := h(hCtx, e.Payload); err != nil {
			slog.Error("eventbus inline event handler failed",
				"type", e.Type,
				"error", err,
			)
		}
	}
	return true
}

// streamReader consumes messages from the Redis Stream and dispatches them.
func (b *Bus) streamReader(ctx context.Context) {
	defer b.wg.Done()
	claimTicker := time.NewTicker(b.claimInterval)
	defer claimTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-claimTicker.C:
			b.claimAndProcess(ctx)
		default:
		}

		if b.readAndProcess(ctx, ">", b.readBlock) {
			continue
		}
		if b.readAndProcess(ctx, "0", 0) {
			continue
		}
		if b.claimAndProcess(ctx) {
			continue
		}
	}
}

func (b *Bus) ensureGroup(ctx context.Context) error {
	err := b.rdb.XGroupCreateMkStream(ctx, b.stream, b.group, "0").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (b *Bus) readAndProcess(ctx context.Context, id string, block time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	streams, err := b.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    b.group,
		Consumer: b.consumer,
		Streams:  []string{b.stream, id},
		Count:    10,
		Block:    block,
	}).Result()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, redis.ErrClosed) || errors.Is(err, redis.Nil) {
			return false
		}
		// Timeout from blocking read is not an error.
		if strings.Contains(err.Error(), "timeout") {
			return false
		}
		slog.Error("eventbus XReadGroup failed",
			"stream", b.stream,
			"group", b.group,
			"id", id,
			"error", err,
		)
		time.Sleep(time.Second)
		return false
	}
	return b.processStreams(ctx, streams)
}

func (b *Bus) claimAndProcess(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	start := "-"
	processed := false
	for {
		claimed, next, err := b.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   b.stream,
			Group:    b.group,
			Consumer: b.consumer,
			MinIdle:  b.claimMinIdle,
			Start:    start,
			Count:    10,
		}).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, redis.ErrClosed) {
				slog.Error("eventbus XAutoClaim failed",
					"stream", b.stream,
					"group", b.group,
					"error", err,
				)
			}
			return processed
		}
		if len(claimed) > 0 {
			b.processStreams(ctx, []redis.XStream{{Messages: claimed}})
			processed = true
		}
		if next == "0-0" || len(claimed) < 10 {
			break
		}
		start = next
	}
	return processed
}

func (b *Bus) processStreams(ctx context.Context, streams []redis.XStream) bool {
	processed := false
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			processed = true
			b.processStreamMessage(ctx, msg)
		}
	}
	return processed
}

func (b *Bus) processStreamMessage(ctx context.Context, msg redis.XMessage) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.streamSem <- struct{}{}
		defer func() { <-b.streamSem }()
		b.handleStreamMessage(ctx, msg)
	}()
}

func (b *Bus) handleStreamMessage(ctx context.Context, msg redis.XMessage) {
	defer func() {
		b.processed.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusProcessed()
		}
		if r := recover(); r != nil {
			slog.Error("eventbus stream handler panic",
				"recover", r,
				"stack", string(debug.Stack()),
			)
			b.panics.Add(1)
			if b.metrics != nil {
				b.metrics.IncEventBusPanics()
			}
		}
	}()

	e, err := b.parseMessage(msg)
	if err != nil {
		slog.Error("eventbus failed to parse stream message, acknowledging to prevent poison",
			"stream", b.stream,
			"message_id", msg.ID,
			"error", err,
		)
		b.ack(ctx, msg.ID)
		return
	}

	h := b.handler(e.Type)
	if h == nil {
		slog.Error("eventbus no handler for stream message, acknowledging to prevent poison",
			"stream", b.stream,
			"message_id", msg.ID,
			"type", e.Type,
		)
		b.ack(ctx, msg.ID)
		return
	}

	hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	if err := h(hCtx, e.Payload); err != nil {
		slog.Error("eventbus stream handler failed, message will be redelivered",
			"stream", b.stream,
			"message_id", msg.ID,
			"type", e.Type,
			"error", err,
		)
		return
	}
	b.ack(ctx, msg.ID)
}

func (b *Bus) parseMessage(msg redis.XMessage) (Event, error) {
	var e Event
	body, ok := msg.Values["event"].(string)
	if !ok {
		return e, fmt.Errorf("message missing event field")
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		return e, fmt.Errorf("unmarshal event: %w", err)
	}
	return e, nil
}

func (b *Bus) ack(ctx context.Context, ids ...string) {
	if err := b.rdb.XAck(ctx, b.stream, b.group, ids...).Err(); err != nil {
		slog.Error("eventbus XAck failed",
			"stream", b.stream,
			"group", b.group,
			"message_ids", ids,
			"error", err,
		)
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
		if b.rdb != nil && b.cancel != nil {
			// Cancel the stream reader first so it stops blocking on Redis.
			b.cancel()
		}
		close(b.ch)
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
