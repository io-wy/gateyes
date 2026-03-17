package limiter

import (
	"context"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

type Limiter struct {
	cfg         config.LimiterConfig
	globalToken *TokenBucket
	userTokens  map[string]*TokenBucket
	queue       chan *Request
	wg          sync.WaitGroup
	stopCh      chan struct{}
	mu          sync.RWMutex
}

type TokenBucket struct {
	rate     int
	burst    int
	tokens   int
	lastFill time.Time
	mu       sync.Mutex
}

type Request struct {
	Context context.Context
	Key     string
	Tokens  int
	Result  chan bool
}

func NewLimiter(cfg config.LimiterConfig) *Limiter {
	l := &Limiter{
		cfg:         cfg,
		globalToken: NewTokenBucket(cfg.GlobalTPM/60, cfg.Burst),
		userTokens:  make(map[string]*TokenBucket),
		queue:       make(chan *Request, cfg.QueueSize),
		stopCh:      make(chan struct{}),
	}

	// 定期补充 token
	go l.refillLoop()

	// 队列消费
	go l.consumeLoop()

	return l
}

func NewTokenBucket(rate, burst int) *TB {
	return &TB{
		rate:   rate,
		burst:  burst,
		tokens: burst,
	}
}

type TB = TokenBucket

func (t *TokenBucket) TryConsume(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(t.lastFill)
	t.tokens += int(elapsed.Seconds()) * t.rate
	if t.tokens > t.burst {
		t.tokens = t.burst
	}
	t.lastFill = now

	if t.tokens >= n {
		t.tokens -= n
		return true
	}
	return false
}

func (l *Limiter) refillLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.globalToken.TryConsume(0) // trigger refill
			l.mu.RLock()
			for _, tb := range l.userTokens {
				tb.TryConsume(0)
			}
			l.mu.RUnlock()
		case <-l.stopCh:
			return
		}
	}
}

func (l *Limiter) consumeLoop() {
	for {
		select {
		case req := <-l.queue:
			allowed := l.check(req.Key, req.Tokens)
			select {
			case req.Result <- allowed:
			default:
			}
		case <-l.stopCh:
			return
		}
	}
}

func (l *Limiter) check(key string, tokens int) bool {
	// global check
	if !l.globalToken.TryConsume(tokens) {
		return false
	}

	// user check
	l.mu.RLock()
	userTB, exists := l.userTokens[key]
	l.mu.RUnlock()

	if !exists {
		l.mu.Lock()
		if _, ok := l.userTokens[key]; !ok {
			l.userTokens[key] = NewTokenBucket(l.cfg.GlobalQPS, l.cfg.Burst)
		}
		userTB = l.userTokens[key]
		l.mu.Unlock()
	}

	return userTB.TryConsume(1) // per request limit
}

func (l *Limiter) Allow(ctx context.Context, key string, tokens int) bool {
	req := &Request{
		Context: ctx,
		Key:     key,
		Tokens:  tokens,
		Result:  make(chan bool, 1),
	}

	select {
	case l.queue <- req:
		select {
		case result := <-req.Result:
			return result
		case <-ctx.Done():
			return false
		}
	case <-ctx.Done():
		return false
	}
}

func (l *Limiter) Stop() {
	close(l.stopCh)
	l.wg.Wait()
}

func (l *Limiter) QueueSize() int {
	return len(l.queue)
}
