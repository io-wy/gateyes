package limiter

import (
	"context"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

type bucketMap struct {
	buckets map[string]*TokenBucket
	mu      sync.RWMutex
}

func newBucketMap() *bucketMap {
	return &bucketMap{buckets: make(map[string]*TokenBucket)}
}

func (bm *bucketMap) getOrCreate(key string, rate, burst int) *TokenBucket {
	bm.mu.RLock()
	if b, ok := bm.buckets[key]; ok {
		bm.mu.RUnlock()
		return b
	}
	bm.mu.RUnlock()
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if b, ok := bm.buckets[key]; ok {
		return b
	}
	b := NewTokenBucket(rate, burst)
	bm.buckets[key] = b
	return b
}

func (bm *bucketMap) tryConsume(key string, n, rate, burst int) bool {
	if rate <= 0 || burst <= 0 {
		return true
	}
	return bm.getOrCreate(key, rate, burst).TryConsume(n)
}

func (bm *bucketMap) refillAll() {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	for _, b := range bm.buckets {
		b.TryConsume(0)
	}
}

const userTokenTTL = 10 * time.Minute

type userBucket struct {
	bucket     *TokenBucket
	lastAccess time.Time
}

type Limiter struct {
	cfg            config.LimiterConfig
	globalToken    *TokenBucket
	globalRPM      *TokenBucket
	userTokens     map[string]*userBucket
	tenantTokens   *bucketMap
	tenantRPM      *bucketMap
	providerTokens *bucketMap
	providerRPM    *bucketMap
	modelTokens    *bucketMap
	modelRPM       *bucketMap
	queue          chan *Request
	wg             sync.WaitGroup
	stopCh         chan struct{}
	mu             sync.RWMutex
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
	globalBurst := cfg.GlobalTokenBurst
	if globalBurst <= 0 {
		globalBurst = cfg.GlobalTPM / 60
		if globalBurst <= 0 {
			globalBurst = 100
		}
	}
	globalRPMRate := cfg.GlobalRPM / 60
	if cfg.GlobalRPM > 0 && globalRPMRate <= 0 {
		globalRPMRate = 1
	}
	globalRPMBurst := cfg.GlobalRPMBurst
	if cfg.GlobalRPM > 0 && globalRPMBurst <= 0 {
		globalRPMBurst = cfg.GlobalRPM / 60
		if globalRPMBurst <= 0 {
			globalRPMBurst = 10
		}
	}
	perUserBurst := cfg.PerUserRequestBurst
	if perUserBurst <= 0 {
		perUserBurst = 100
	}
	cfg.PerUserRequestBurst = perUserBurst

	l := &Limiter{
		cfg:            cfg,
		globalToken:    NewTokenBucket(cfg.GlobalTPM/60, globalBurst),
		globalRPM:      NewTokenBucket(globalRPMRate, globalRPMBurst),
		userTokens:     make(map[string]*userBucket),
		tenantTokens:   newBucketMap(),
		tenantRPM:      newBucketMap(),
		providerTokens: newBucketMap(),
		providerRPM:    newBucketMap(),
		modelTokens:    newBucketMap(),
		modelRPM:       newBucketMap(),
		queue:          make(chan *Request, cfg.QueueSize),
		stopCh:         make(chan struct{}),
	}

	l.wg.Add(2)
	go l.refillLoop()
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
	defer l.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.globalToken.TryConsume(0)
			l.globalRPM.TryConsume(0)
			l.mu.Lock()
			now := time.Now()
			for k, ub := range l.userTokens {
				ub.bucket.TryConsume(0)
				if now.Sub(ub.lastAccess) > userTokenTTL {
					delete(l.userTokens, k)
				}
			}
			l.mu.Unlock()
			l.tenantTokens.refillAll()
			l.tenantRPM.refillAll()
			l.providerTokens.refillAll()
			l.providerRPM.refillAll()
			l.modelTokens.refillAll()
			l.modelRPM.refillAll()
		case <-l.stopCh:
			return
		}
	}
}

func (l *Limiter) consumeLoop() {
	defer l.wg.Done()
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
			for {
				select {
				case req := <-l.queue:
					req.sendResult(false)
				default:
					return
				}
			}
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

	// global RPM check
	if l.cfg.GlobalRPM > 0 && !l.globalRPM.TryConsume(1) {
		return false
	}

	// user check: 按请求数限流
	// P1 fix: userQPS > 0 时使用用户配置，否则 fallback 到全局默认
	rate := l.cfg.GlobalQPS
	if userQPS > 0 {
		rate = userQPS
	}

	l.mu.RLock()
	ub, exists := l.userTokens[key]
	l.mu.RUnlock()

	if !exists {
		l.mu.Lock()
		if _, ok := l.userTokens[key]; !ok {
			l.userTokens[key] = &userBucket{
				bucket:     NewTokenBucket(rate, l.cfg.PerUserRequestBurst),
				lastAccess: time.Now(),
			}
		}
		ub = l.userTokens[key]
		l.mu.Unlock()
	}

	ok := ub.bucket.TryConsume(1)
	if ok {
		ub.lastAccess = time.Now()
	}
	return ok
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

// CheckTenant 检查租户维度限流（token + RPM）
func (l *Limiter) CheckTenant(tenantID string, tokens int) bool {
	if tenantID == "" {
		return true
	}
	if !l.tenantTokens.tryConsume(tenantID, tokens, l.cfg.TenantTPM/60, l.cfg.TenantTPMBurst) {
		return false
	}
	return l.tenantRPM.tryConsume(tenantID, 1, l.cfg.TenantRPM/60, l.cfg.TenantRPMBurst)
}

// CheckProvider 检查 provider 维度限流（token + RPM）
func (l *Limiter) CheckProvider(provider string, tokens int) bool {
	if provider == "" {
		return true
	}
	if !l.providerTokens.tryConsume(provider, tokens, l.cfg.ProviderTPM/60, l.cfg.ProviderTPMBurst) {
		return false
	}
	return l.providerRPM.tryConsume(provider, 1, l.cfg.ProviderRPM/60, l.cfg.ProviderRPMBurst)
}

// CheckModel 检查 model 维度限流（token + RPM）
func (l *Limiter) CheckModel(model string, tokens int) bool {
	if model == "" {
		return true
	}
	if !l.modelTokens.tryConsume(model, tokens, l.cfg.ModelTPM/60, l.cfg.ModelTPMBurst) {
		return false
	}
	return l.modelRPM.tryConsume(model, 1, l.cfg.ModelRPM/60, l.cfg.ModelRPMBurst)
}
