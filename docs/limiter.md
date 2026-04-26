# Limiter 限流机制

本文档描述 Gateyes 限流模块的实现细节。

## 概述

限流模块位于 `internal/service/limiter`，采用**多维度令牌桶算法**，支持内存和 Redis 两种后端：

1. **全局层**：限制全局 token 总量（TPM）和请求总量（RPM）
2. **用户层**：限制单用户请求速率（QPS）
3. **租户层**：限制单租户 token（TPM）和请求（RPM）
4. **Provider 层**：限制单 provider token（TPM）和请求（RPM）
5. **Model 层**：限制单 model token（TPM）和请求（RPM）

当 Redis 可用时，全局层、租户层、Provider 层、Model 层均使用 Redis 分布式令牌桶，实现多实例间共享限流状态。用户层始终使用内存令牌桶。当 Redis 不可用时，所有维度自动降级到内存令牌桶（fail-open）。

### 架构总览

```
HTTP 请求进入
    |
    v
Limiter.Allow(ctx, key, userQPS, tokens)
    |
    v
Request 入队 l.queue
    |
    v
consumeLoop() 取出请求
    |
    +-- 全局层检查 (TPM + RPM)
    |       |
    |       +-- Redis 可用? --> Redis Lua 令牌桶 (gateyes:rl:g:t / gateyes:rl:g:r)
    |       |
    |       +-- Redis 不可用? --> 内存令牌桶 (globalToken / globalRPM)
    |
    +-- 用户层检查 (QPS)
    |       |
    |       +-- 始终使用内存令牌桶 (userTokens[key])
    |
    v
通过 --> CheckTenant(tenantID, tokens)
    |
    +-- Redis 可用? --> Redis Lua 令牌桶 (gateyes:rl:ten:{id}:t / gateyes:rl:ten:{id}:r)
    +-- Redis 不可用? --> 内存 bucketMap
    |
    v
通过 --> CheckProvider(provider, tokens)
    |
    +-- Redis 可用? --> Redis Lua 令牌桶 (gateyes:rl:prov:{name}:t / gateyes:rl:prov:{name}:r)
    +-- Redis 不可用? --> 内存 bucketMap
    |
    v
通过 --> CheckModel(model, tokens)
    |
    +-- Redis 可用? --> Redis Lua 令牌桶 (gateyes:rl:mod:{name}:t / gateyes:rl:mod:{name}:r)
    +-- Redis 不可用? --> 内存 bucketMap
    |
    v
所有维度通过 --> 返回 true
任一维度拒绝 --> 返回 429
```

## 核心结构

```go
type Limiter struct {
    cfg            config.LimiterConfig
    rdb            *redis.Client           // Redis 客户端，nil 表示纯内存模式
    globalToken    *TokenBucket            // 全局 TPM 桶（内存）
    globalRPM      *TokenBucket            // 全局 RPM 桶（内存）
    userTokens     map[string]*userBucket  // per-apiKey QPS 桶（内存）
    tenantTokens   *bucketMap              // 租户 TPM 桶（内存降级用）
    tenantRPM      *bucketMap              // 租户 RPM 桶（内存降级用）
    providerTokens *bucketMap              // provider TPM 桶（内存降级用）
    providerRPM    *bucketMap              // provider RPM 桶（内存降级用）
    modelTokens    *bucketMap              // model TPM 桶（内存降级用）
    modelRPM       *bucketMap              // model RPM 桶（内存降级用）
    queue          chan *Request           // 单消费者队列
    wg             sync.WaitGroup
    stopCh         chan struct{}
    mu             sync.RWMutex
}
```

### bucketMap 结构

`bucketMap` 封装了并发安全的动态令牌桶集合，用于租户/Provider/Model 维度的内存降级：

```go
type bucketMap struct {
    buckets map[string]*TokenBucket
    mu      sync.RWMutex
}
```

- `getOrCreate(key, rate, burst)` — 双重检查锁，按需创建令牌桶
- `tryConsume(key, n, rate, burst)` — rate/burst <= 0 时直接放行（该维度未配置）
- `refillAll()` — refillLoop 每秒调用，补充所有桶

### userBucket 结构

```go
type userBucket struct {
    bucket     *TokenBucket
    lastAccess time.Time
}
```

用户桶带 `lastAccess` 时间戳，超过 10 分钟（`userTokenTTL`）未访问的桶会在 `refillLoop` 中自动清理，防止内存膨胀。

## 配置

```yaml
limiter:
  globalQPS: 1000            # 全局默认用户 QPS
  globalTPM: 1000000         # 全局每分钟 token 上限
  globalTokenBurst: 100000   # 全局 token 桶突发容量
  globalRPM: 0               # 全局每分钟请求上限，0=禁用
  globalRPMBurst: 0          # 全局 RPM 桶突发容量
  perUserRequestBurst: 100   # 每用户请求突发容量
  tenantTPM: 0               # 每租户每分钟 token 上限，0=禁用
  tenantTPMBurst: 0          # 租户 token 桶突发容量
  tenantRPM: 0               # 每租户每分钟请求上限，0=禁用
  tenantRPMBurst: 0          # 租户 RPM 桶突发容量
  providerTPM: 0             # 每 provider 每分钟 token 上限，0=禁用
  providerTPMBurst: 0        # provider token 桶突发容量
  providerRPM: 0             # 每 provider 每分钟请求上限，0=禁用
  providerRPMBurst: 0        # provider RPM 桶突发容量
  modelTPM: 0                # 每 model 每分钟 token 上限，0=禁用
  modelTPMBurst: 0           # model token 桶突发容量
  modelRPM: 0                # 每 model 每分钟请求上限，0=禁用
  modelRPMBurst: 0           # model RPM 桶突发容量
  queueSize: 1000            # 队列缓冲长度
```

### 配置项详解

| 配置项 | 用途 | 默认值 |
|--------|------|--------|
| globalQPS | 全局默认用户请求速率 | 1000 |
| globalTPM | 全局每分钟 token 上限 | 1000000 |
| globalTokenBurst | 全局 token 桶突发容量 | globalTPM/60 |
| globalRPM | 全局每分钟请求上限，0 禁用 | 0 |
| globalRPMBurst | 全局 RPM 桶突发容量 | globalRPM/60 |
| perUserRequestBurst | 单用户请求突发容量 | 100 |
| tenantTPM | 每租户每分钟 token 上限 | 0（禁用） |
| tenantTPMBurst | 租户 token 桶突发容量 | tenantTPM/60 |
| tenantRPM | 每租户每分钟请求上限 | 0（禁用） |
| tenantRPMBurst | 租户 RPM 桶突发容量 | tenantRPM/60 |
| providerTPM | 每 provider 每分钟 token 上限 | 0（禁用） |
| providerTPMBurst | provider token 桶突发容量 | providerTPM/60 |
| providerRPM | 每 provider 每分钟请求上限 | 0（禁用） |
| providerRPMBurst | provider RPM 桶突发容量 | providerRPM/60 |
| modelTPM | 每 model 每分钟 token 上限 | 0（禁用） |
| modelTPMBurst | model token 桶突发容量 | modelTPM/60 |
| modelRPM | 每 model 每分钟请求上限 | 0（禁用） |
| modelRPMBurst | model RPM 桶突发容量 | modelRPM/60 |
| queueSize | 队列大小 | 1000 |

### 禁用语义

所有 `*TPM` / `*RPM` 字段值为 0 时表示该维度不启用。`bucketMap.tryConsume` 在 rate <= 0 或 burst <= 0 时直接返回 `true`，跳过限流检查。

## 令牌桶算法

### TokenBucket 结构（内存）

```go
type TokenBucket struct {
    rate     int        // 每秒补充速率
    burst    int        // 桶容量上限
    tokens   int        // 当前 token 数量
    lastFill time.Time  // 上次填充时间
    mu       sync.Mutex
}
```

### TryConsume 消费逻辑

```go
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
```

**算法公式**：

```
tokens = min(burst, tokens + elapsed_seconds * rate)
if tokens >= n:
    tokens -= n
    allow
else:
    deny
```

## Redis 分布式令牌桶

当通过 `SetRedis(rdb)` 设置了 Redis 客户端后，全局层、租户层、Provider 层、Model 层的限流检查使用 Redis 原子 Lua 脚本实现令牌桶，确保多实例部署时限流状态一致。

### Lua 脚本实现

```lua
-- KEYS[1] = bucket key
-- ARGV[1] = rate (tokens/second), ARGV[2] = burst, ARGV[3] = consume, ARGV[4] = now_ms
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local consume = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

local tokens = tonumber(redis.call('HGET', key, 't'))
local last_fill = tonumber(redis.call('HGET', key, 'l'))

if tokens == nil then tokens = burst end
if last_fill == nil then last_fill = now end

local elapsed = math.max(0, now - last_fill)
tokens = math.min(burst, tokens + (rate * elapsed) / 1000)

local ok = 0
if tokens >= consume then
    tokens = tokens - consume
    ok = 1
end

redis.call('HSET', key, 't', tokens, 'l', now)
redis.call('EXPIRE', key, 120)

return ok
```

### Redis 数据结构

每个限流维度对应一个 Redis Hash，存储两个字段：

| Hash 字段 | 含义 |
|-----------|------|
| `t` | 当前 token 数量 |
| `l` | 上次填充时间（毫秒时间戳） |

每个 key 设置 120 秒 TTL，长时间不活跃的桶自动过期。

### Redis Key 命名规则

```
gateyes:rl:g:t                           # 全局 TPM
gateyes:rl:g:r                           # 全局 RPM
gateyes:rl:ten:{tenantID}:t              # 租户 TPM
gateyes:rl:ten:{tenantID}:r              # 租户 RPM
gateyes:rl:prov:{provider}:t             # provider TPM
gateyes:rl:prov:{provider}:r             # provider RPM
gateyes:rl:mod:{model}:t                 # model TPM
gateyes:rl:mod:{model}:r                 # model RPM
```

### Fail-Open 降级策略

```go
func redisTryConsume(rdb *redis.Client, redisKey string, n, rate, burst int) bool {
    if rate <= 0 || burst <= 0 {
        return true // 维度未配置，直接放行
    }
    now := time.Now().UnixMilli()
    result, err := tokenBucketLua.Run(context.Background(), rdb, []string{redisKey}, rate, burst, n, now).Int()
    if err != nil {
        return true // Redis 出错，fail-open 放行
    }
    return result == 1
}
```

Redis 调用失败时（网络异常、Redis 宕机等），`redisTryConsume` 返回 `true`（放行），保证服务可用性优先于限流精确性。

### SetRedis() 方法

```go
func (l *Limiter) SetRedis(rdb *redis.Client) {
    l.rdb = rdb
}
```

- 在服务启动时，如果 Redis 配置可用，调用此方法切换到分布式模式
- 设置后，`check()` / `CheckTenant()` / `CheckProvider()` / `CheckModel()` 中的 `l.rdb != nil` 分支生效
- `rdb` 为 `nil` 时，所有维度走内存令牌桶

## 多维度限流

### check() 逻辑（全局 + 用户层）

```go
func (l *Limiter) check(key string, userQPS, tokens int) bool {
    // === 第1层: 全局 TPM + RPM ===
    if l.rdb != nil {
        if !redisTryConsume(l.rdb, limiterKey("g", "t"), tokens, l.cfg.GlobalTPM/60, l.cfg.GlobalTokenBurst) {
            return false
        }
        if l.cfg.GlobalRPM > 0 && !redisTryConsume(l.rdb, limiterKey("g", "r"), 1, ...) {
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

    // === 第2层: per-apiKey QPS ===
    rate := l.cfg.GlobalQPS
    if userQPS > 0 {
        rate = userQPS
    }
    userTB := l.getOrCreateUserBucket(key, rate)
    return userTB.TryConsume(1)
}
```

### CheckTenant / CheckProvider / CheckModel

三个方法结构一致，以 `CheckTenant` 为例：

```go
func (l *Limiter) CheckTenant(tenantID string, tokens int) bool {
    if tenantID == "" {
        return true // 空值跳过
    }
    if l.rdb != nil {
        // Redis 路径
        if !redisTryConsume(l.rdb, limiterKey("ten", tenantID, "t"), tokens, l.cfg.TenantTPM/60, l.cfg.TenantTPMBurst) {
            return false
        }
        return redisTryConsume(l.rdb, limiterKey("ten", tenantID, "r"), 1, l.cfg.TenantRPM/60, l.cfg.TenantRPMBurst)
    }
    // 内存降级路径
    if !l.tenantTokens.tryConsume(tenantID, tokens, l.cfg.TenantTPM/60, l.cfg.TenantTPMBurst) {
        return false
    }
    return l.tenantRPM.tryConsume(tenantID, 1, l.cfg.TenantRPM/60, l.cfg.TenantRPMBurst)
}
```

**限流维度总览**：

| 层级 | 维度 | 消费单位 | 方法 | Redis Key 前缀 |
|------|------|----------|------|----------------|
| 全局层 | 全局 TPM | tokens（预估 prompt + output） | `check()` | `gateways:rl:g:t` |
| 全局层 | 全局 RPM | 1（每次请求） | `check()` | `gateways:rl:g:r` |
| 用户层 | per-apiKey QPS | 1（每次请求） | `check()` | 纯内存 |
| 租户层 | tenant TPM | tokens | `CheckTenant()` | `gateways:rl:ten:{id}:t` |
| 租户层 | tenant RPM | 1 | `CheckTenant()` | `gateways:rl:ten:{id}:r` |
| Provider 层 | provider TPM | tokens | `CheckProvider()` | `gateways:rl:prov:{name}:t` |
| Provider 层 | provider RPM | 1 | `CheckProvider()` | `gateways:rl:prov:{name}:r` |
| Model 层 | model TPM | tokens | `CheckModel()` | `gateways:rl:mod:{name}:t` |
| Model 层 | model RPM | 1 | `CheckModel()` | `gateways:rl:mod:{name}:r` |

### 调用顺序

```
Allow(ctx, key, userQPS, tokens)
    |
    v
consumeLoop() → check(key, userQPS, tokens)
    |               |
    |               +-- 全局 TPM/RPM 检查
    |               +-- 用户 QPS 检查
    |
    v (通过后)
handler 层依次调用:
    CheckTenant(tenantID, tokens)
    CheckProvider(provider, tokens)
    CheckModel(model, tokens)
```

`check()` 在 `consumeLoop` 内执行，保证串行。`CheckTenant` / `CheckProvider` / `CheckModel` 在 handler 层按需调用，每个维度独立检查，任一拒绝即返回 429。

## 异步队列

### 队列结构

```go
type Request struct {
    Context context.Context  // 请求上下文，用于取消检查
    Key     string           // apiKey
    UserQPS int              // 用户配置的 QPS（0 表示使用全局默认）
    Tokens  int              // 预估 token 数（prompt + output budget）
    Result  chan bool        // 结果通道
}
```

### 消费循环

```go
func (l *Limiter) consumeLoop() {
    defer l.wg.Done()
    for {
        select {
        case req := <-l.queue:
            // 先检查 context 是否已取消
            select {
            case <-req.Context.Done():
                req.sendResult(false)
                continue
            default:
            }
            // 执行限流检查（全局 + 用户层）
            allowed := l.check(req.Key, req.UserQPS, req.Tokens)
            req.sendResult(allowed)
        case <-l.stopCh:
            // 优雅停止：drain 剩余队列
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
```

### Allow() 入口

```go
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
```

## Token 估算

### EstimateAdmissionTokens

限流使用的 token 数是 **prompt + output budget**：

```go
func (r *ResponseRequest) EstimateAdmissionTokens() int {
    promptTokens := r.EstimatePromptTokens()
    maxTokens := r.MaxOutputTokens
    if maxTokens <= 0 {
        maxTokens = r.MaxTokens
    }
    if maxTokens <= 0 {
        maxTokens = DefaultMaxOutputTokens  // 4096
    }
    return promptTokens + maxTokens
}
```

设计原因：
- 只算 prompt 会导致长输出请求"白嫖"限流
- output budget 优先用用户指定的 `max_tokens`，没指定用保守默认值

## 请求流程

```
HTTP 请求进入
    |
    v
middleware.GuardLLMRequest()
    |
    v
extractRequestMeta()  → 估算 admissionTokens
    |
    v
limiter.Allow(ctx, key, userQPS, admissionTokens)
    |
    v
Request 入队 l.queue
    |
    v
consumeLoop() 取出请求
    |
    v
检查 req.Context.Done()
    |
    v
check(key, userQPS, tokens) → 全局 TPM/RPM + 用户 QPS
    |
    v
返回结果到 req.Result
    |
    v
Allow() 返回 true/false
    |
    v (通过后 handler 层继续)
CheckTenant(tenantID, tokens)  → 租户 TPM + RPM
CheckProvider(provider, tokens) → provider TPM + RPM
CheckModel(model, tokens)      → model TPM + RPM
    |
    v
所有维度通过 → 继续处理
任一维度拒绝 → 返回 429
```

## 初始化流程

```go
func NewLimiter(cfg config.LimiterConfig) *Limiter {
    // 兼容处理：burst 为 0 时自动推算
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
    go l.refillLoop()    // 每秒补充 token + 清理过期用户桶
    go l.consumeLoop()   // 消费队列

    return l
}
```

初始创建时 `rdb` 为 `nil`，纯内存模式。启动后可通过 `SetRedis(rdb)` 切换到分布式模式。

### 定期补充

```go
func (l *Limiter) refillLoop() {
    defer l.wg.Done()
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            l.globalToken.TryConsume(0)
            l.globalRPM.TryConsume(0)
            // 清理超过 userTokenTTL 的用户桶
            l.mu.Lock()
            now := time.Now()
            for k, ub := range l.userTokens {
                ub.bucket.TryConsume(0)
                if now.Sub(ub.lastAccess) > userTokenTTL {
                    delete(l.userTokens, k)
                }
            }
            l.mu.Unlock()
            // 补充 bucketMap 桶
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
```

注意：Redis 模式下不需要 refillLoop 补充 Redis 桶——Lua 脚本在每次消费时自行计算补充量。refillLoop 主要服务内存桶。

### Reload 热更新

```go
func (l *Limiter) Reload(cfg *config.Config) error {
    l.mu.Lock()
    defer l.mu.Unlock()
    // 重新推算 burst，重建内存令牌桶
    // 注意：Redis 侧的桶会在下次消费时自动使用新配置
    l.cfg = newCfg
    l.globalToken = NewTokenBucket(newCfg.GlobalTPM/60, globalBurst)
    l.globalRPM = NewTokenBucket(globalRPMRate, globalRPMBurst)
    return nil
}
```

## 关键行为

### 1. userQPS 配置生效

- 用户配置 `QPS > 0` 时，使用用户值
- 用户配置 `QPS <= 0` 时，fallback 到全局 `GlobalQPS`

### 2. context 取消

- `consumeLoop` 在处理请求前先检查 `req.Context.Done()`
- 已取消的请求直接返回 false，不消耗 token

### 3. 优雅停止

- `Stop()` 关闭 `stopCh`，并通过 `wg.Wait()` 等待 goroutine 退出
- `consumeLoop` drain 剩余队列，给未处理请求返回 false
- 已发送但未处理的请求不会 hang

### 4. Burst 处理

- `GlobalTokenBurst`：全局 token 桶的突发容量
- `GlobalRPMBurst`：全局 RPM 桶的突发容量
- `PerUserRequestBurst`：单用户请求桶的突发容量
- `TenantTPMBurst` / `TenantRPMBurst`：租户维度突发容量
- `ProviderTPMBurst` / `ProviderRPMBurst`：Provider 维度突发容量
- `ModelTPMBurst` / `ModelRPMBurst`：Model 维度突发容量
- burst 为 0 时自动推算为 TPM/60 或 RPM/60，保证最小值为 1

### 5. Redis 后端切换

- 初始为纯内存模式（`rdb == nil`）
- `SetRedis(rdb)` 切换到分布式模式
- Redis 不可用时自动降级到内存桶（fail-open）
- 用户层（QPS）始终使用内存桶，不受 Redis 影响

### 6. 空 ID 跳过

`CheckTenant` / `CheckProvider` / `CheckModel` 在 ID 为空字符串时直接返回 `true`，跳过检查。适用于未关联租户/Provider/Model 的请求。

## 边界与限制

1. **用户桶自动清理**：超过 `userTokenTTL`（10 分钟）未访问的用户桶会被 `refillLoop` 清理
2. **bucketMap 只增不减**：租户/Provider/Model 维度的内存桶目前不会自动清理，长期运行可能内存膨胀（可考虑增加 idle 清理）
3. **整数除法精度**：`TPM/60` / `RPM/60` 低值时可能有精度损失，NewLimiter 中有最小值兜底（1）
4. **Redis Lua 脚本无原子性保证**：依赖 Redis 单线程执行保证原子性，Redis Cluster 模式下需确保 key 路由到同一 slot
5. **Reload 不清理已有桶**：热更新会重建全局桶，但用户桶和 bucketMap 中的桶不会被清理

## 测试用例

| 测试 | 验证点 |
|------|--------|
| TestTokenBucket_TryConsume | burst 耗尽后拒绝 |
| TestTokenBucket_Refill | 等待后 token 补充 |
| TestLimiter_PerUserQPS | 不同 userQPS 有不同限流效果 |
| TestLimiter_DifferentUsers | 不同 apiKey 独立限流 |
| TestLimiter_QueueSize | 队列不超出容量 |
| TestLimiter_Concurrent | 并发安全 |
| TestLimiter_Cancel | context 取消时返回 false |
| TestLimiter_UserQPSConfig | 用户配置 QPS 优先级高于全局默认 |
