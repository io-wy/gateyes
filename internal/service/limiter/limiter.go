package limiter

import (
	"context"
	"errors"
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

func (bm *bucketMap) getOrCreate(key string, rate float64, burst int) *bucketEntry {
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

func (bm *bucketMap) tryConsume(key string, n int, rate float64, burst int) bool {
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
	lastAccess atomic.Int64 // UnixNano, atomic to avoid data race in check().
}

type Limiter struct {
	cfg            config.LimiterConfig
	rdb            *redis.Client
	globalToken    *TokenBucket
	globalRPM      *TokenBucket
	globalRequests *TokenBucket
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
	rate     float64
	burst    int
	tokens   float64
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
	globalBurst := tokenBurst(cfg.GlobalTPM, cfg.GlobalTokenBurst, 100)
	globalRPMRate := perMinuteRate(cfg.GlobalRPM)
	globalRPMBurst := perMinuteBurst(cfg.GlobalRPM, cfg.GlobalRPMBurst, 10)
	globalRequestRate, globalRequestBurst := requestLimit(cfg.GlobalQPS, cfg.GlobalQPSBurst, cfg.GlobalRPM, cfg.GlobalRPMBurst)
	perUserBurst := cfg.PerUserRequestBurst
	if perUserBurst <= 0 {
		perUserBurst = 100
	}
	cfg.PerUserRequestBurst = perUserBurst

	l := &Limiter{
		cfg:            cfg,
		globalToken:    NewTokenBucket(perMinuteRate(cfg.GlobalTPM), globalBurst),
		globalRPM:      NewTokenBucket(globalRPMRate, globalRPMBurst),
		globalRequests: NewTokenBucket(globalRequestRate, globalRequestBurst),
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

type LimitRequest struct {
	TenantID string
	Provider string
	Model    string
	Tokens   int
}

type Decision struct {
	Allowed bool
	Reason  string
	Scope   string
}

func (d Decision) ProviderScoped() bool {
	return d.Scope == "provider"
}

type limitSpec struct {
	key     string
	bucket  *TokenBucket
	rate    float64
	burst   int
	consume int
	reason  string
	scope   string
}

func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastFill: time.Now(),
	}
}

func (t *TokenBucket) TryConsume(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.refillLocked(now)

	consume := float64(n)
	if t.tokens >= consume {
		t.tokens -= consume
		return true
	}
	return false
}

func (t *TokenBucket) refillLocked(now time.Time) {
	elapsed := now.Sub(t.lastFill)
	t.tokens += elapsed.Seconds() * t.rate
	if t.tokens > float64(t.burst) {
		t.tokens = float64(t.burst)
	}
	t.lastFill = now
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
			l.globalRequests.TryConsume(0)
			l.mu.Lock()
			now := time.Now()
			for k, ub := range l.userTokens {
				ub.bucket.TryConsume(0)
				if now.Sub(time.Unix(0, ub.lastAccess.Load())) > userTokenTTL {
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
		if !redisTryConsume(l.rdb, limiterKey("g", "t"), tokens, perMinuteRate(l.cfg.GlobalTPM), tokenBurst(l.cfg.GlobalTPM, l.cfg.GlobalTokenBurst, 100)) {
			return false
		}
		if l.cfg.GlobalRPM > 0 && !redisTryConsume(l.rdb, limiterKey("g", "r"), 1, perMinuteRate(l.cfg.GlobalRPM), perMinuteBurst(l.cfg.GlobalRPM, l.cfg.GlobalRPMBurst, 10)) {
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
	ratePerSecond := float64(rate)
	burst := l.cfg.PerUserRequestBurst
	if userQPS > 0 && rate > 0 && burst > rate {
		burst = rate
	}

	l.mu.RLock()
	ub, exists := l.userTokens[key]
	needsRebuild := exists && (ub.bucket.rate != ratePerSecond || ub.bucket.burst != burst)
	l.mu.RUnlock()

	if !exists || needsRebuild {
		l.mu.Lock()
		ub, exists = l.userTokens[key]
		needsRebuild = exists && (ub.bucket.rate != ratePerSecond || ub.bucket.burst != burst)
		if !exists || needsRebuild {
			l.userTokens[key] = &userBucket{
				bucket:     NewTokenBucket(ratePerSecond, burst),
				lastAccess: atomic.Int64{},
			}
			l.userTokens[key].lastAccess.Store(time.Now().UnixNano())
		}
		ub = l.userTokens[key]
		l.mu.Unlock()
	}

	ok := ub.bucket.TryConsume(1)
	if ok {
		ub.lastAccess.Store(time.Now().UnixNano())
	}
	return ok
}

func (l *Limiter) Check(ctx context.Context, req LimitRequest) Decision {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Decision{Allowed: false, Reason: "context_cancelled"}
	}
	specs := l.buildLimitSpecs(req, l.rdb != nil)
	if len(specs) == 0 {
		return Decision{Allowed: true}
	}
	if l.rdb != nil {
		return redisCheck(ctx, l.rdb, specs)
	}
	return l.localCheck(ctx, specs)
}

func (l *Limiter) buildLimitSpecs(req LimitRequest, redisKeys bool) []limitSpec {
	tokens := req.Tokens
	if tokens < 0 {
		tokens = 0
	}
	specs := make([]limitSpec, 0, 8)
	add := func(scope, id, metric string, bucket *TokenBucket, bm *bucketMap, rate float64, burst, consume int, reason string) {
		if rate <= 0 || burst <= 0 || consume < 0 {
			return
		}
		spec := limitSpec{
			rate:    rate,
			burst:   burst,
			consume: consume,
			reason:  reason,
			scope:   scope,
		}
		if redisKeys {
			spec.key = atomicLimiterKey(scope, id, metric)
		} else if bucket != nil {
			spec.bucket = bucket
		} else if bm != nil {
			spec.bucket = bm.getOrCreate(id, rate, burst).bucket
		}
		specs = append(specs, spec)
	}

	add("global", "global", "tokens", l.globalToken, nil, perMinuteRate(l.cfg.GlobalTPM), tokenBurst(l.cfg.GlobalTPM, l.cfg.GlobalTokenBurst, 100), tokens, "global_tokens")
	rate, burst := requestLimit(l.cfg.GlobalQPS, l.cfg.GlobalQPSBurst, l.cfg.GlobalRPM, l.cfg.GlobalRPMBurst)
	add("global", "global", "qps", l.globalRequests, nil, rate, burst, 1, "global_qps")

	if req.TenantID != "" {
		add("tenant", req.TenantID, "tokens", nil, l.tenantTokens, perMinuteRate(l.cfg.TenantTPM), tokenBurst(l.cfg.TenantTPM, l.cfg.TenantTPMBurst, 0), tokens, "tenant_tokens")
		rate, burst = requestLimit(l.cfg.TenantQPS, l.cfg.TenantQPSBurst, l.cfg.TenantRPM, l.cfg.TenantRPMBurst)
		add("tenant", req.TenantID, "qps", nil, l.tenantRPM, rate, burst, 1, "tenant_qps")
	}
	if req.Provider != "" {
		add("provider", req.Provider, "tokens", nil, l.providerTokens, perMinuteRate(l.cfg.ProviderTPM), tokenBurst(l.cfg.ProviderTPM, l.cfg.ProviderTPMBurst, 0), tokens, "provider_tokens")
		rate, burst = requestLimit(l.cfg.ProviderQPS, l.cfg.ProviderQPSBurst, l.cfg.ProviderRPM, l.cfg.ProviderRPMBurst)
		add("provider", req.Provider, "qps", nil, l.providerRPM, rate, burst, 1, "provider_qps")
	}
	if req.Model != "" {
		add("model", req.Model, "tokens", nil, l.modelTokens, perMinuteRate(l.cfg.ModelTPM), tokenBurst(l.cfg.ModelTPM, l.cfg.ModelTPMBurst, 0), tokens, "model_tokens")
		rate, burst = requestLimit(l.cfg.ModelQPS, l.cfg.ModelQPSBurst, l.cfg.ModelRPM, l.cfg.ModelRPMBurst)
		add("model", req.Model, "qps", nil, l.modelRPM, rate, burst, 1, "model_qps")
	}
	return specs
}

func (l *Limiter) localCheck(ctx context.Context, specs []limitSpec) Decision {
	locked := make([]*TokenBucket, 0, len(specs))
	for _, spec := range specs {
		if spec.bucket == nil {
			continue
		}
		spec.bucket.mu.Lock()
		locked = append(locked, spec.bucket)
	}
	defer func() {
		for i := len(locked) - 1; i >= 0; i-- {
			locked[i].mu.Unlock()
		}
	}()

	if err := ctx.Err(); err != nil {
		return Decision{Allowed: false, Reason: "context_cancelled"}
	}

	now := time.Now()
	for _, spec := range specs {
		if spec.bucket == nil {
			continue
		}
		spec.bucket.refillLocked(now)
		if spec.bucket.tokens < float64(spec.consume) {
			return Decision{Allowed: false, Reason: spec.reason, Scope: spec.scope}
		}
	}
	for _, spec := range specs {
		if spec.bucket == nil {
			continue
		}
		spec.bucket.tokens -= float64(spec.consume)
	}
	return Decision{Allowed: true}
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
	globalBurst := tokenBurst(newCfg.GlobalTPM, newCfg.GlobalTokenBurst, 100)
	globalRPMRate := perMinuteRate(newCfg.GlobalRPM)
	globalRPMBurst := perMinuteBurst(newCfg.GlobalRPM, newCfg.GlobalRPMBurst, 10)
	globalRequestRate, globalRequestBurst := requestLimit(newCfg.GlobalQPS, newCfg.GlobalQPSBurst, newCfg.GlobalRPM, newCfg.GlobalRPMBurst)
	perUserBurst := newCfg.PerUserRequestBurst
	if perUserBurst <= 0 {
		perUserBurst = 100
	}
	newCfg.PerUserRequestBurst = perUserBurst

	l.cfg = newCfg
	l.globalToken = NewTokenBucket(perMinuteRate(newCfg.GlobalTPM), globalBurst)
	l.globalRPM = NewTokenBucket(globalRPMRate, globalRPMBurst)
	l.globalRequests = NewTokenBucket(globalRequestRate, globalRequestBurst)
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
		if !redisTryConsume(l.rdb, limiterKey("ten", tenantID, "t"), tokens, perMinuteRate(l.cfg.TenantTPM), tokenBurst(l.cfg.TenantTPM, l.cfg.TenantTPMBurst, 0)) {
			return false
		}
		rate, burst := requestLimit(l.cfg.TenantQPS, l.cfg.TenantQPSBurst, l.cfg.TenantRPM, l.cfg.TenantRPMBurst)
		return redisTryConsume(l.rdb, limiterKey("ten", tenantID, "r"), 1, rate, burst)
	}
	if !l.tenantTokens.tryConsume(tenantID, tokens, perMinuteRate(l.cfg.TenantTPM), tokenBurst(l.cfg.TenantTPM, l.cfg.TenantTPMBurst, 0)) {
		return false
	}
	rate, burst := requestLimit(l.cfg.TenantQPS, l.cfg.TenantQPSBurst, l.cfg.TenantRPM, l.cfg.TenantRPMBurst)
	return l.tenantRPM.tryConsume(tenantID, 1, rate, burst)
}

// CheckProvider 检查 provider 维度限流（token + RPM）
func (l *Limiter) CheckProvider(provider string, tokens int) bool {
	if provider == "" {
		return true
	}
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("prov", provider, "t"), tokens, perMinuteRate(l.cfg.ProviderTPM), tokenBurst(l.cfg.ProviderTPM, l.cfg.ProviderTPMBurst, 0)) {
			return false
		}
		rate, burst := requestLimit(l.cfg.ProviderQPS, l.cfg.ProviderQPSBurst, l.cfg.ProviderRPM, l.cfg.ProviderRPMBurst)
		return redisTryConsume(l.rdb, limiterKey("prov", provider, "r"), 1, rate, burst)
	}
	if !l.providerTokens.tryConsume(provider, tokens, perMinuteRate(l.cfg.ProviderTPM), tokenBurst(l.cfg.ProviderTPM, l.cfg.ProviderTPMBurst, 0)) {
		return false
	}
	rate, burst := requestLimit(l.cfg.ProviderQPS, l.cfg.ProviderQPSBurst, l.cfg.ProviderRPM, l.cfg.ProviderRPMBurst)
	return l.providerRPM.tryConsume(provider, 1, rate, burst)
}

// CheckModel 检查 model 维度限流（token + RPM）
func (l *Limiter) CheckModel(model string, tokens int) bool {
	if model == "" {
		return true
	}
	if l.rdb != nil {
		if !redisTryConsume(l.rdb, limiterKey("mod", model, "t"), tokens, perMinuteRate(l.cfg.ModelTPM), tokenBurst(l.cfg.ModelTPM, l.cfg.ModelTPMBurst, 0)) {
			return false
		}
		rate, burst := requestLimit(l.cfg.ModelQPS, l.cfg.ModelQPSBurst, l.cfg.ModelRPM, l.cfg.ModelRPMBurst)
		return redisTryConsume(l.rdb, limiterKey("mod", model, "r"), 1, rate, burst)
	}
	if !l.modelTokens.tryConsume(model, tokens, perMinuteRate(l.cfg.ModelTPM), tokenBurst(l.cfg.ModelTPM, l.cfg.ModelTPMBurst, 0)) {
		return false
	}
	rate, burst := requestLimit(l.cfg.ModelQPS, l.cfg.ModelQPSBurst, l.cfg.ModelRPM, l.cfg.ModelRPMBurst)
	return l.modelRPM.tryConsume(model, 1, rate, burst)
}

func perMinuteRate(value int) float64 {
	if value <= 0 {
		return 0
	}
	return float64(value) / 60.0
}

func perMinuteBurst(limit, configured, disabledDefault int) int {
	if limit <= 0 {
		return 0
	}
	if configured > 0 {
		return configured
	}
	burst := limit / 60
	if burst <= 0 {
		burst = 1
	}
	if burst <= 0 && disabledDefault > 0 {
		burst = disabledDefault
	}
	return burst
}

func tokenBurst(limit, configured, disabledDefault int) int {
	if limit <= 0 {
		return disabledDefault
	}
	return perMinuteBurst(limit, configured, disabledDefault)
}

func requestLimit(qps, qpsBurst, rpm, rpmBurst int) (float64, int) {
	if qps > 0 {
		burst := qpsBurst
		if burst <= 0 {
			burst = qps
		}
		return float64(qps), burst
	}
	if rpm > 0 {
		return perMinuteRate(rpm), perMinuteBurst(rpm, rpmBurst, 0)
	}
	return 0, 0
}

func deniedFromErr(err error) Decision {
	if err == nil {
		return Decision{Allowed: true}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Decision{Allowed: false, Reason: "context_cancelled"}
	}
	return Decision{Allowed: false, Reason: "redis_error"}
}
