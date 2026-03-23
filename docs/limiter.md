# Limiter 限流机制

本文档描述 Gateyes 限流模块的实现细节。

## 概述

限流模块位于 `internal/service/limiter`，采用**两层令牌桶算法**：

1. **全局层**：限制 token 总量（按 token 数）
2. **用户层**：限制单用户请求速率（按请求数）

## 核心结构

```go
type Limiter struct {
    cfg         config.LimiterConfig
    globalToken *TokenBucket           // 全局 token 桶
    userTokens  map[string]*TokenBucket // per-apiKey 桶
    queue       chan *Request           // 单消费者队列
    stopCh      chan struct{}
    mu          sync.RWMutex
}
```

## 配置

```yaml
limiter:
  globalQPS: 1000           # 全局默认用户 QPS（当用户未配置时使用）
  globalTPM: 1000000        # 全局每分钟 token 上限
  globalTokenBurst: 100000  # 全局 token 桶突发容量
  perUserRequestBurst: 100  # 每用户请求突发容量
  queueSize: 1000           # 队列缓冲长度
```

| 配置项 | 用途 | 默认值 |
|--------|------|--------|
| globalQPS | 全局默认用户请求速率 | 1000 |
| globalTPM | 全局每分钟 token 上限 | 1000000 |
| globalTokenBurst | 全局 token 桶容量 | globalTPM/60 |
| perUserRequestBurst | 单用户请求突发容量 | 100 |
| queueSize | 队列大小 | 1000 |

## 令牌桶算法

### TokenBucket 结构

```go
type TokenBucket struct {
    rate     int        // 每秒补充速率
    burst    int        // 桶容量上限
    tokens   int        // 当前 token 数量
    lastFill time.Time  // 上次填充时间
}
```

### TryConsume 消费逻辑

```go
func (t *TokenBucket) TryConsume(n int) bool {
    // 1. 计算从上次填充到现在经过的秒数
    elapsed := now.Sub(t.lastFill)

    // 2. 补充 token: tokens += elapsed * rate
    t.tokens += int(float64(elapsed.Nanoseconds()) / 1e9 * float64(t.rate))

    // 3. 超过 burst 则截断
    if t.tokens > t.burst {
        t.tokens = t.burst
    }
    t.lastFill = now

    // 4. 尝试消费 n 个 token
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

## 两层限流

### check() 逻辑

```go
func (l *Limiter) check(key string, userQPS, tokens int) bool {
    // === 第1层: 全局 token bucket ===
    // 按预估 token 数消费（包含 prompt + output）
    if !l.globalToken.TryConsume(tokens) {
        return false  // 全局 token 不足，拒绝
    }

    // === 第2层: per-apiKey bucket ===
    // userQPS > 0 使用用户配置，否则 fallback 到全局默认
    rate := l.cfg.GlobalQPS
    if userQPS > 0 {
        rate = userQPS
    }

    // 获取或创建用户 bucket
    userTB := l.getOrCreateUserBucket(key, rate)

    // 每次消费 1（限制请求数而非 token 数）
    return userTB.TryConsume(1)
}
```

**限流维度**：

| 层级 | 维度 | 消费单位 |
|------|------|----------|
| 全局层 | 全局 token 总数 | tokens（预估 prompt + output） |
| 用户层 | per-apiKey 请求数 | 1（每次 1 请求） |

## 异步队列

### 队列结构

```go
type Request struct {
    Context     context.Context  // 请求上下文，用于取消检查
    Key         string           // apiKey
    UserQPS     int              // 用户配置的 QPS（0 表示使用全局默认）
    Tokens      int              // 预估 token 数
    Result      chan bool        // 结果通道
}
```

### 消费循环

```go
func (l *Limiter) consumeLoop() {
    for {
        select {
        case req := <-l.queue:
            // 先检查 context 是否已取消
            select {
            case <-req.Context.Done():
                req.sendResult(false)  // 已取消直接返回 false
                continue
            default:
            }

            // 执行两层限流检查
            allowed := l.check(req.Key, req.UserQPS, req.Tokens)
            req.sendResult(allowed)

        case <-l.stopCh:
            // 优雅停止：drain 剩余队列
            for req := range l.queue {
                req.sendResult(false)
            }
            return
        }
    }
}
```

### Allow() 入口

```go
func (l *Limiter) Allow(ctx context.Context, key string, userQPS, admissionTokens int) bool {
    req := &Request{
        Context:     ctx,
        Key:         key,
        UserQPS:     userQPS,
        Tokens:      admissionTokens,
        Result:      make(chan bool, 1),
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
    // 1. 估算 prompt token
    promptTokens := r.EstimatePromptTokens()

    // 2. 获取 output budget
    maxTokens := r.MaxOutputTokens
    if maxTokens <= 0 {
        maxTokens = r.MaxTokens
    }
    if maxTokens <= 0 {
        maxTokens = DefaultMaxOutputTokens  // 4096
    }

    // 3. 返回总和
    return promptTokens + maxTokens
}
```

这样设计是因为：
- 只算 prompt 会导致长输出请求"白嫖"限流
- output budget 优先用用户指定的 `max_tokens`，没指定用保守默认值

## 请求流程

```
HTTP 请求进入
    ↓
middleware.GuardLLMRequest()
    ↓
extractRequestMeta()  → 估算 admissionTokens
    ↓
limiter.Allow(ctx, key, userQPS, admissionTokens)
    ↓
Request 入队 l.queue
    ↓
consumeLoop() 取出请求
    ↓
检查 req.Context.Done()
    ↓
check(key, userQPS, tokens) → 两层限流
    ↓
返回结果到 req.Result
    ↓
Allow() 返回 true/false
    ↓
通过 → 继续处理
失败 → 返回 429
```

## 初始化流程

```go
func NewLimiter(cfg config.LimiterConfig) *Limiter {
    // 兼容处理
    globalBurst := cfg.GlobalTokenBurst
    if globalBurst <= 0 {
        globalBurst = cfg.GlobalTPM / 60
    }

    l := &Limiter{
        cfg:         cfg,
        globalToken: NewTokenBucket(cfg.GlobalTPM/60, globalBurst),
        userTokens:  make(map[string]*TokenBucket),
        queue:       make(chan *Request, cfg.QueueSize),
    }

    // 启动两个 goroutine
    go l.refillLoop()    // 每秒补充 token
    go l.consumeLoop()   // 消费队列

    return l
}
```

### 定期补充

```go
func (l *Limiter) refillLoop() {
    ticker := time.NewTicker(time.Second)
    for {
        select {
        case <-ticker.C:
            // trigger refill: 传入 n=0 只补充不消费
            l.globalToken.TryConsume(0)

            // 补充所有用户 bucket
            for _, tb := range l.userTokens {
                tb.TryConsume(0)
            }
        case <-l.stopCh:
            return
        }
    }
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

- `Stop()` 关闭 `stopCh`
- `consumeLoop` 遍历剩余队列，返回 false
- 已发送但未处理的请求不会hang

### 4. Burst 处理

- `GlobalTokenBurst`: 全局 token 桶的突发容量，影响全局 token 上限
- `PerUserRequestBurst`: 单用户请求桶的突发容量，影响单用户 QPS 突发能力
- 两者分开配置，灵活调优

## 边界与限制

1. **没有 tenant 维度**: 当前仅按 apiKey 隔离
2. **没有 provider 维度**: 所有请求统一限流
3. **用户 bucket 只增不减**: 长期运行可能会内存膨胀（可考虑增加 idle 清理）
4. **整数除法精度**: `GlobalTPM/60` 低值时可能有精度损失

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