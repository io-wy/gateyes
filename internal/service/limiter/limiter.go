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
	UserQPS int // 用户配置的 QPS，0 表示使用全局默认
	Tokens  int // 预估 token 数（prompt + output budget）
	Result  chan bool
}

func NewLimiter(cfg config.LimiterConfig) *Limiter {
	// P4 fix: 拆分 burst 配置，GlobalTokenBurst 用于全局 token 桶，PerUserRequestBurst 用于用户请求桶
	globalBurst := cfg.GlobalTokenBurst
	if globalBurst <= 0 {
		globalBurst = cfg.GlobalTPM / 60 // 兼容旧配置
		if globalBurst <= 0 {
			globalBurst = 100
		}
	}
	perUserBurst := cfg.PerUserRequestBurst
	if perUserBurst <= 0 {
		perUserBurst = 100 // 默认值
	}

	l := &Limiter{
		cfg:         cfg,
		globalToken: NewTokenBucket(cfg.GlobalTPM/60, globalBurst),
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
		rate:     rate,
		burst:    burst,
		tokens:   burst,
		lastFill: time.Now(),
	}
}

type TB = TokenBucket

func (t *TokenBucket) TryConsume(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(t.lastFill)
	// 使用 float64 避免整数精度丢失
	t.tokens += int(float64(elapsed.Nanoseconds()) / 1e9 * float64(t.rate))
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
			// P2 fix: 检查 context 是否已取消，避免处理已取消的请求
			select {
			case <-req.Context.Done():
				req.sendResult(false)
				continue
			default:
			}
			allowed := l.check(req.Key, req.UserQPS, req.Tokens)
			req.sendResult(allowed)
		case <-l.stopCh:
			// P7 fix: stop 时 drain 队列，给剩余请求返回 false
			for req := range l.queue {
				req.sendResult(false)
			}
			return
		}
	}
}

func (r *Request) sendResult(result bool) {
	select {
	case r.Result <- result:
	default:
	}
}

func (l *Limiter) check(key string, userQPS, tokens int) bool {
	// global check: 按 token 数限流
	if !l.globalToken.TryConsume(tokens) {
		return false
	}

	// user check: 按请求数限流
	// P1 fix: userQPS > 0 时使用用户配置，否则 fallback 到全局默认
	rate := l.cfg.GlobalQPS
	if userQPS > 0 {
		rate = userQPS
	}

	l.mu.RLock()
	userTB, exists := l.userTokens[key]
	l.mu.RUnlock()

	if !exists {
		l.mu.Lock()
		if _, ok := l.userTokens[key]; !ok {
			l.userTokens[key] = NewTokenBucket(rate, l.cfg.PerUserRequestBurst)
		}
		userTB = l.userTokens[key]
		l.mu.Unlock()
	}

	return userTB.TryConsume(1) // per request limit
}

func (l *Limiter) Allow(ctx context.Context, key string, userQPS, admissionTokens int) bool {
	req := &Request{
		Context: ctx,
		Key:     key,
		UserQPS: userQPS,
		Tokens:  admissionTokens,
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
