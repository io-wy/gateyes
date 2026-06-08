package limiter

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/app/config"
)

type bucketEntry struct {
	bucket     *TokenBucket
	lastAccess atomic.Int64 // UnixNano, atomic to avoid data race in getOrCreate RLock path
}

type bucketMap struct {
	buckets map[string]*bucketEntry
	mu      sync.RWMutex
	ttl     time.Duration
}

func newBucketMap() *bucketMap {
	return &bucketMap{buckets: make(map[string]*bucketEntry), ttl: 10 * time.Minute}
}

func (bm *bucketMap) getOrCreate(key string, rate, burst int) *bucketEntry {
	now := time.Now().UnixNano()
	bm.mu.RLock()
	if e, ok := bm.buckets[key]; ok {
		bm.mu.RUnlock()
		e.lastAccess.Store(now)
		return e
	}
	bm.mu.RUnlock()
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if e, ok := bm.buckets[key]; ok {
		e.lastAccess.Store(now)
		return e
	}
	e := &bucketEntry{bucket: NewTokenBucket(rate, burst)}
	e.lastAccess.Store(now)
	bm.buckets[key] = e
	return e
}

func (bm *bucketMap) tryConsume(key string, n, rate, burst int) bool {
	if rate <= 0 || burst <= 0 {
		return true
	}
	return bm.getOrCreate(key, rate, burst).bucket.TryConsume(n)
}

func (bm *bucketMap) refillAll() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	now := time.Now().UnixNano()
	ttlNanos := bm.ttl.Nanoseconds()
	for k, e := range bm.buckets {
		e.bucket.TryConsume(0)
		if bm.ttl > 0 && now-e.lastAccess.Load() > ttlNanos {
			delete(bm.buckets, k)
		}
	}
}

const userTokenTTL = 10 * time.Minute

type userBucket struct {
	bucket     *TokenBucket
	lastAccess time.Time
}

type Limiter struct {
	cfg            config.LimiterConfig
	rdb            *redis.Client
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
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("g", "t"), tokens, l.cfg.GlobalTPM/60, l.cfg.GlobalTokenBurst) {
			return false
		}
		if l.cfg.GlobalRPM > 0 && !redisTryConsume(l.rdb, limiterKey("g", "r"), 1, l.cfg.GlobalRPM/60, l.cfg.GlobalRPMBurst) {
			return false
		}
	} else {
		if !l.globalToken.TryConsume(tokens) {
			return false
		}
		if l.cfg.GlobalRPM > 0 && !l.globalRPM.TryConsume(1) {
			return false
		}
	}

	// user check: 按请求数限流
	// P1 fix: userQPS > 0 时使用用户配置，否则 fallback 到全局默认
	rate := l.cfg.GlobalQPS
	if userQPS > 0 {
		rate = userQPS
	}
	burst := l.cfg.PerUserRequestBurst
	if userQPS > 0 && rate > 0 && burst > rate {
		burst = rate
	}

	l.mu.RLock()
	ub, exists := l.userTokens[key]
	needsRebuild := exists && (ub.bucket.rate != rate || ub.bucket.burst != burst)
	l.mu.RUnlock()

	if !exists || needsRebuild {
		l.mu.Lock()
		ub, exists = l.userTokens[key]
		needsRebuild = exists && (ub.bucket.rate != rate || ub.bucket.burst != burst)
		if !exists || needsRebuild {
			l.userTokens[key] = &userBucket{
				bucket:     NewTokenBucket(rate, burst),
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

// Reload updates runtime-safe limiter parameters from a new config.
func (l *Limiter) Reload(cfg *config.Config) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	newCfg := cfg.Limiter
	globalBurst := newCfg.GlobalTokenBurst
	if globalBurst <= 0 {
		globalBurst = newCfg.GlobalTPM / 60
		if globalBurst <= 0 {
			globalBurst = 100
		}
	}
	globalRPMRate := newCfg.GlobalRPM / 60
	if newCfg.GlobalRPM > 0 && globalRPMRate <= 0 {
		globalRPMRate = 1
	}
	globalRPMBurst := newCfg.GlobalRPMBurst
	if newCfg.GlobalRPM > 0 && globalRPMBurst <= 0 {
		globalRPMBurst = newCfg.GlobalRPM / 60
		if globalRPMBurst <= 0 {
			globalRPMBurst = 10
		}
	}
	perUserBurst := newCfg.PerUserRequestBurst
	if perUserBurst <= 0 {
		perUserBurst = 100
	}
	newCfg.PerUserRequestBurst = perUserBurst

	l.cfg = newCfg
	l.globalToken = NewTokenBucket(newCfg.GlobalTPM/60, globalBurst)
	l.globalRPM = NewTokenBucket(globalRPMRate, globalRPMBurst)
	l.userTokens = make(map[string]*userBucket)
	return nil
}

func (l *Limiter) Name() string { return "limiter" }

// SetRedis enables distributed rate limiting via Redis.
func (l *Limiter) SetRedis(rdb *redis.Client) {
	l.rdb = rdb
}

// CheckTenant 检查租户维度限流（token + RPM）
func (l *Limiter) CheckTenant(tenantID string, tokens int) bool {
	if tenantID == "" {
		return true
	}
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("ten", tenantID, "t"), tokens, l.cfg.TenantTPM/60, l.cfg.TenantTPMBurst) {
			return false
		}
		return redisTryConsume(l.rdb, limiterKey("ten", tenantID, "r"), 1, l.cfg.TenantRPM/60, l.cfg.TenantRPMBurst)
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
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("prov", provider, "t"), tokens, l.cfg.ProviderTPM/60, l.cfg.ProviderTPMBurst) {
			return false
		}
		return redisTryConsume(l.rdb, limiterKey("prov", provider, "r"), 1, l.cfg.ProviderRPM/60, l.cfg.ProviderRPMBurst)
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
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("mod", model, "t"), tokens, l.cfg.ModelTPM/60, l.cfg.ModelTPMBurst) {
			return false
		}
		return redisTryConsume(l.rdb, limiterKey("mod", model, "r"), 1, l.cfg.ModelRPM/60, l.cfg.ModelRPMBurst)
	}
	if !l.modelTokens.tryConsume(model, tokens, l.cfg.ModelTPM/60, l.cfg.ModelTPMBurst) {
		return false
	}
	return l.modelRPM.tryConsume(model, 1, l.cfg.ModelRPM/60, l.cfg.ModelRPMBurst)
}
