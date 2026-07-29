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
// # Kafka backend
//
// When Kafka is configured, PublishEvent writes typed events to a Kafka topic
// and consumers process them through a consumer group. If Kafka is disabled or
// temporarily unavailable, PublishEvent falls back to in-memory dispatch so
// local development and tests remain lightweight.
package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

// Handler is the work performed off the hot path. The supplied context is
// derived from the bus's lifetime context with a fixed timeout.
type Handler func(ctx context.Context)

// Event is a serializable, durable work item. Use it with PublishEvent when
// the work must survive a gateway process restart.
type Event struct {
	Key     string `json:"key,omitempty"`
	Type    string `json:"type"`
	Payload []byte `json:"payload"`
}

// EventHandler processes a single durable event. A non-nil error tells the
// Kafka consumer not to commit the message so Kafka can redeliver it.
type EventHandler func(ctx context.Context, payload []byte) error

// Event type constants for cross-package coordination.
const (
	EventTypeResponseDetails = "response:details"
	EventTypeResponseUpdate  = "response:update"
	EventTypeBatchItem       = "batch:item"
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

	// handlers map event types to processors. Used by both in-memory fallback
	// and Kafka dispatch paths.
	handlers   map[string]EventHandler
	handlersMu sync.RWMutex

	// kafka backend (optional)
	kafkaWriter *kafka.Writer
	kafkaReader *kafka.Reader
	kafkaTopic  string
}

// KafkaOptions configures the optional Kafka durable event backend.
type KafkaOptions struct {
	// Enabled turns on Kafka publishing/consuming when Brokers and Topic are set.
	Enabled bool
	// Brokers is the Kafka bootstrap broker list.
	Brokers []string
	// Topic stores all typed eventbus events.
	Topic string
	// ConsumerGroup enables durable consumption. Default: "gateyes".
	ConsumerGroup string
	// ClientID identifies this gateway process to Kafka.
	ClientID string
	// BatchSize controls producer batching. Default: 100.
	BatchSize int
	// BatchTimeout controls producer flush latency. Default: 50ms.
	BatchTimeout time.Duration
	// ReadMinBytes / ReadMaxBytes tune reader fetch sizes.
	ReadMinBytes int
	ReadMaxBytes int
	// MaxAttempts controls producer retries. Default: 3.
	MaxAttempts int
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
	// Kafka configures the optional Kafka durable backend.
	// Optional; if disabled, PublishEvent uses in-memory fallback.
	Kafka KafkaOptions
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

	kafkaOpts := opts.Kafka
	var kafkaWriter *kafka.Writer
	var kafkaReader *kafka.Reader
	if kafkaOpts.Enabled && len(kafkaOpts.Brokers) > 0 && kafkaOpts.Topic != "" {
		if kafkaOpts.ConsumerGroup == "" {
			kafkaOpts.ConsumerGroup = "gateyes"
		}
		if kafkaOpts.ClientID == "" {
			kafkaOpts.ClientID = "gateyes-gateway"
		}
		if kafkaOpts.BatchSize <= 0 {
			kafkaOpts.BatchSize = 100
		}
		if kafkaOpts.BatchTimeout <= 0 {
			kafkaOpts.BatchTimeout = 50 * time.Millisecond
		}
		if kafkaOpts.ReadMinBytes <= 0 {
			kafkaOpts.ReadMinBytes = 1
		}
		if kafkaOpts.ReadMaxBytes <= 0 {
			kafkaOpts.ReadMaxBytes = 10e6
		}
		if kafkaOpts.MaxAttempts <= 0 {
			kafkaOpts.MaxAttempts = 3
		}
		kafkaWriter = &kafka.Writer{
			Addr:         kafka.TCP(kafkaOpts.Brokers...),
			Topic:        kafkaOpts.Topic,
			Balancer:     &kafka.Hash{},
			BatchSize:    kafkaOpts.BatchSize,
			BatchTimeout: kafkaOpts.BatchTimeout,
			MaxAttempts:  kafkaOpts.MaxAttempts,
		}
		kafkaReader = kafka.NewReader(kafka.ReaderConfig{
			Brokers:  kafkaOpts.Brokers,
			GroupID:  kafkaOpts.ConsumerGroup,
			Topic:    kafkaOpts.Topic,
			MinBytes: kafkaOpts.ReadMinBytes,
			MaxBytes: kafkaOpts.ReadMaxBytes,
		})
	}

	return &Bus{
		ch:          make(chan Handler, opts.Buffer),
		workers:     opts.Workers,
		timeout:     opts.HandlerTimeout,
		metrics:     opts.Metrics,
		handlers:    make(map[string]EventHandler),
		kafkaWriter: kafkaWriter,
		kafkaReader: kafkaReader,
		kafkaTopic:  kafkaOpts.Topic,
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
		if b.kafkaReader != nil {
			b.wg.Add(1)
			go b.kafkaReaderLoop(runCtx)
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

// PublishEvent persists a typed event. When Kafka is configured, the event is
// written to Kafka. If Kafka is disabled or temporarily unavailable, the bus
// falls back to in-memory dispatch so that single-process deployments and tests
// continue to work. Returns false only when the bus is closed or no handler is
// registered for the event type.
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
	if b.kafkaWriter != nil {
		if err := b.writeKafka(ctx, e); err == nil {
			return true
		} else {
			slog.Warn("eventbus kafka write failed, falling back",
				"topic", b.kafkaTopic,
				"type", e.Type,
				"error", err,
			)
		}
	}
	return b.dispatchEventInMemory(e)
}

func (b *Bus) writeKafka(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := e.Key
	if key == "" {
		key = e.Type
	}
	return b.kafkaWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: body,
		Headers: []kafka.Header{
			{Key: "event_type", Value: []byte(e.Type)},
		},
		Time: time.Now(),
	})
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

func (b *Bus) kafkaReaderLoop(ctx context.Context) {
	defer b.wg.Done()
	for {
		msg, err := b.kafkaReader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			slog.Error("eventbus kafka fetch failed", "topic", b.kafkaTopic, "error", err)
			time.Sleep(time.Second)
			continue
		}
		if b.handleKafkaMessage(ctx, msg) {
			if err := b.kafkaReader.CommitMessages(ctx, msg); err != nil {
				slog.Error("eventbus kafka commit failed",
					"topic", msg.Topic,
					"partition", msg.Partition,
					"offset", msg.Offset,
					"error", err,
				)
			}
		}
	}
}

func (b *Bus) handleKafkaMessage(ctx context.Context, msg kafka.Message) bool {
	defer func() {
		b.processed.Add(1)
		if b.metrics != nil {
			b.metrics.IncEventBusProcessed()
		}
		if r := recover(); r != nil {
			slog.Error("eventbus kafka handler panic",
				"recover", r,
				"stack", string(debug.Stack()),
			)
			b.panics.Add(1)
			if b.metrics != nil {
				b.metrics.IncEventBusPanics()
			}
		}
	}()

	var e Event
	if err := json.Unmarshal(msg.Value, &e); err != nil {
		slog.Error("eventbus failed to parse kafka message, committing to prevent poison",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"error", err,
		)
		return true
	}
	h := b.handler(e.Type)
	if h == nil {
		slog.Error("eventbus no handler for kafka message, committing to prevent poison",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"type", e.Type,
		)
		return true
	}

	hCtx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	if err := h(hCtx, e.Payload); err != nil {
		slog.Error("eventbus kafka handler failed, message will be redelivered",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"type", e.Type,
			"error", err,
		)
		return false
	}
	return true
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
		if b.kafkaReader != nil && b.cancel != nil {
			// Cancel the Kafka reader first so it stops blocking on fetch.
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
		if b.kafkaReader != nil {
			_ = b.kafkaReader.Close()
		}
		if b.kafkaWriter != nil {
			_ = b.kafkaWriter.Close()
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
