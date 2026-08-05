# Gateyes 限流、缓存与 Redis 深度解析

---

# 第一部分：限流深度解析

## 1. 三种限流算法的本质区别

### 1.1 令牌桶（Token Bucket）

```
桶容量 = burst
补充速率 = rate/秒

初始：桶满（tokens = burst）
每过 Δt 秒：tokens += rate * Δt，但不超过 burst
请求来时：如果 tokens >= n，扣减 n 并放行；否则拒绝
```

**关键特性**：

- **允许突发**：桶满时可以一次性消耗 burst 个 token
- **平滑限流**：长期速率稳定在 rate，短期允许毛刺
- **内存友好**：只需要记录 tokens 和 last_fill 两个值

**Gateyes 实现**：

```go
type TokenBucket struct {
    rate     int        // 每秒补充速率
    burst    int        // 桶容量上限
    tokens   int        // 当前 token 数量
    lastFill time.Time  // 上次填充时间
    mu       sync.Mutex
}
```

**为什么选令牌桶？**

- LLM 请求的 token 消耗不均匀：有的请求 100 tokens，有的 8000 tokens
- 令牌桶天然适合"不均匀消费场景"
- 突发流量（如批量推理）需要被容忍

### 1.2 漏桶（Leaky Bucket）

```
桶容量 = capacity
漏水速率 = rate/秒

请求来时：如果桶未满，放入桶中排队
处理时：按固定 rate 从桶中取出处理
```

**关键特性**：

- **绝对平滑**：输出速率严格等于 rate，不允许任何突发
- **需要队列**：请求可能排队等待
- **不适合 LLM**：LLM 请求不能排队等处理，用户要的是实时响应

**为什么 Gateyes 不用漏桶？**

- LLM 网关不能让用户请求排队等 10 秒
- 漏桶的排队机制会引入不可控的延迟

### 1.3 滑动窗口（Sliding Window）

```
窗口大小 = 1分钟
限制 = 1000 请求

每来一个请求：检查过去 1 分钟内有多少请求
如果 >= 1000：拒绝
否则：放行，记录当前请求时间戳
```

**关键特性**：

- **精确计数**：在任意时刻都能准确知道窗口内的请求数
- **内存开销大**：需要保存窗口内所有请求的时间戳
- **实现复杂**：需要定期清理过期时间戳

**变体**：

- **固定窗口**：把一分钟分成 10 个 6 秒窗口，每个窗口单独计数（存在边界突发问题）
- **滑动日志**：保存每个请求的时间戳，查询时过滤（内存消耗大）

**为什么 Gateyes 不用滑动窗口？**

- 多维度（5 个维度 * 2 个指标 = 10 个窗口），内存压力大
- Redis 中保存时间戳列表比 Hash 更耗空间
- 令牌桶的"近似平滑"对网关场景已经足够

### 1.4 三种算法对比总结

| 维度       | 令牌桶             | 漏桶         | 滑动窗口         |
| ---------- | ------------------ | ------------ | ---------------- |
| 允许突发   | ✅                 | ❌           | ❌               |
| 内存开销   | 低（2个值）        | 中（队列）   | 高（时间戳列表） |
| 精确度     | 近似               | 精确         | 精确             |
| 适合场景   | API 网关、网络流量 | 固定速率处理 | 精确计数场景     |
| 实现复杂度 | 低                 | 中           | 高               |

---

## 2. 分布式限流的难点

### 2.1 单机限流的问题

```
Instance A: 允许 50 req/s
Instance B: 允许 50 req/s

用户实际速率 = 100 req/s，但配置是 50 req/s
```

**问题**：每个实例独立计数，总和超过限制。

**解决方案**：共享状态，让所有实例看到同一个计数器。

### 2.2 分布式计数器的选择

| 方案                    | 优点       | 缺点                                        |
| ----------------------- | ---------- | ------------------------------------------- |
| 数据库（行锁）          | 强一致     | 性能差，死锁风险                            |
| Redis INCR              | 简单，原子 | 无法处理 token bucket 的"补充+扣减"复合逻辑 |
| Redis Lua               | 原子，灵活 | 需要写 Lua，Cluster 下有坑                  |
| 中心服务（如 Sentinel） | 功能丰富   | 增加依赖，单点风险                          |

**Gateyes 选择 Redis Lua**：原子性 + 灵活性 + 无额外依赖。

### 2.3 时间同步问题

令牌桶依赖 `last_fill` 时间戳。分布式环境下：

```
Instance A: now = 10000ms, last_fill = 9000ms → elapsed = 1000ms
Instance B: now = 10050ms, last_fill = 9000ms → elapsed = 1050ms
```

**问题**：不同实例的 `now` 可能有偏差。

**解决**：Gateyes 使用 Redis 服务器的时钟（`now` 在 Lua 脚本中用 `ARGV[4]` 传入，基于实例本地时间）。如果 NTP 同步正常（偏差 < 10ms），误差可以忽略。

更精确的做法：Lua 脚本中用 `redis.call('TIME')` 获取 Redis 服务器时间。但 `TIME` 返回秒+微秒，需要解析，增加脚本复杂度。Gateyes 的折中方案是传入客户端时间，依赖 NTP。

### 2.4 网络延迟的影响

```
客户端 → 发送 Lua 脚本 → Redis 执行 → 返回结果 → 客户端处理
     5ms              1ms          5ms              1ms
```

总延迟约 12ms。如果限流检查在关键路径上，这 12ms 会叠加到每个请求。

**Gateyes 的优化**：

- 异步队列：限流检查在独立 goroutine 中，不阻塞主请求处理
- 但代价是增加了请求的整体延迟（等待队列 + 检查结果）

---

## 3. Lua 脚本原子性原理

### 3.1 Redis 为什么是单线程

Redis 的核心设计：

- 所有命令在一个线程中顺序执行
- 没有锁、没有上下文切换
- 纯内存操作，每个命令执行时间极短（微秒级）

```
客户端 A: SET key1 value1
客户端 B: GET key1
客户端 C: DEL key1

执行顺序：SET → GET → DEL（不会交错）
```

### 3.2 Lua 脚本如何保证原子性

Lua 脚本在 Redis 中是作为一个**整体**执行的：

```
1. Redis 收到 EVAL 命令
2. 加载 Lua 脚本（如果缓存了则跳过）
3. 在单线程中执行整个脚本
4. 脚本执行期间，不处理其他客户端的命令
5. 返回脚本结果
```

```lua
-- 这个脚本执行期间，其他命令被阻塞
local tokens = redis.call('HGET', key, 't')   -- 步骤1
local last_fill = redis.call('HGET', key, 'l') -- 步骤2
-- ... 计算 ...
redis.call('HSET', key, 't', tokens, 'l', now) -- 步骤3
```

**关键**：步骤1、2、3 之间不会有其他命令插入。如果不用 Lua：

```go
// 伪代码：不用 Lua，用 Go 代码实现
tokens := rdb.HGet(ctx, key, "t").Val()        // 此时 tokens = 100
// ← 另一个实例执行了同样的逻辑，扣减到 80
// ← 本实例继续用旧的 tokens = 100 做判断，超卖！
```

### 3.3 Lua 脚本的限制

**不能执行阻塞命令**：

- `BLPOP`、`BRPOP`、`BLMOVE` 等会阻塞 Redis 的脚本中不允许使用
- 因为脚本执行期间 Redis 不处理其他命令，阻塞命令会导致 Redis 僵死

**执行时间限制**：

- Lua 脚本默认最长执行 5 秒（`lua-time-limit` 配置）
- 超过后 Redis 开始回复 `BUSY` 错误给其他客户端
- 但不会终止脚本执行（避免数据不一致）

**Gateyes 的 Lua 脚本执行时间**：

- 两次 Hash 读 + 一次 Hash 写 + 一次 EXPIRE
- 典型执行时间 < 1ms
- 远低于 5 秒限制

### 3.4 脚本缓存

Redis 会缓存 Lua 脚本：

```go
// 第一次执行：发送脚本全文
redis.Eval(ctx, script, []string{key}, rate, burst, n, now)

// 后续执行：发送脚本的 SHA1 哈希（如果缓存命中）
redis.EvalSha(ctx, sha1, []string{key}, rate, burst, n, now)
```

`redis.NewScript()` 在 go-redis 中会自动处理缓存：第一次用 EVAL，后续用 EVALSHA。

---

## 4. Redis Cluster 下多 key 事务的坑

### 4.1 Cluster 的槽机制

Redis Cluster 把 16384 个槽分配到不同节点：

```
Node A: slots 0-5460
Node B: slots 5461-10922
Node C: slots 10923-16383
```

每个 key 通过 CRC16(key) % 16384 计算槽号。

### 4.2 Lua 脚本的限制

```lua
-- 这个脚本要求 key1 和 key2 在同一节点
redis.call('HGET', key1, 't')
redis.call('HGET', key2, 't')
```

**如果 key1 和 key2 不在同一槽**：

```
(error) CROSSSLOT Keys in request don't hash to the same slot
```

### 4.3 Hash Tag 原理

Redis Cluster 支持 hash tag：只计算 `{}` 内的内容作为槽的输入。

```
user:{1000}:profile  → CRC16("1000") % 16384
user:{1000}:settings → CRC16("1000") % 16384
user:{1001}:profile  → CRC16("1001") % 16384
```

`user:1000:profile` 和 `user:1000:settings` 有相同的 hash tag `{1000}`，所以落在同一槽。

### 4.4 Gateyes 的 Hash Tag 设计

修改前（有问题）：

```
gateyes:rl:ten:tenant-a:t   → slot(CRC16("gateyes:rl:ten:tenant-a:t"))
gateyes:rl:ten:tenant-a:r   → slot(CRC16("gateyes:rl:ten:tenant-a:r"))
```

两个 key 的 CRC16 结果不同，可能落在不同槽。

修改后（正确）：

```
gateyes:rl:{tenant-a}:ten:tenant-a:t  → slot(CRC16("tenant-a"))
gateyes:rl:{tenant-a}:ten:tenant-a:r  → slot(CRC16("tenant-a"))
```

相同的 hash tag `{tenant-a}`，保证同一槽。

### 4.5 如果不用 Hash Tag 的替代方案

**方案 A：多个独立 Lua 调用**

```go
// 分两次调用，每次一个 key
redisTryConsume(tpmKey, ...)
redisTryConsume(rpmKey, ...)
```

**缺点**：失去原子性，两次调用之间可能有其他实例修改状态。

**方案 B：本地缓存 + 异步同步**

```go
// 本地维护计数器，定期把状态写回 Redis
```

**缺点**：复杂度爆炸，状态不一致窗口大。

**方案 C：Redis Cluster 的 Hash Tag（最优）**

Gateyes 选择了方案 C。

---

# 第二部分：缓存深度解析

## 1. 缓存层次设计

### 1.1 L1 / L2 / L3 缓存

```
请求 → L1 (进程内 Memory LRU)
     → miss → L2 (分布式 Redis)
            → miss → L3 (上游 Provider)
                   → 写入 L2 → 写入 L1
```

**Gateyes 实际只有 L1 + L2**：

- L1: 内存 LRU（进程内，重启丢失）
- L2: Redis（分布式，多实例共享）
- L3: 上游 Provider（真正的数据源）

**为什么没有 L3 缓存（如本地磁盘）？**

- LLM 响应通常不大（几 KB 到几十 KB）
- 磁盘 I/O 比网络还慢
- 内存足够放热点数据

### 1.2 Fallback Cache 设计

```go
type FallbackCache struct {
    primary  Cache // Redis
    fallback Cache // Memory LRU
}

func (f *FallbackCache) Get(ctx, key) (Entry, bool) {
    // 先读 Redis
    if entry, ok := f.primary.Get(ctx, key); ok {
        // 同时回填内存（Redis 有但内存没有时）
        f.fallback.Set(ctx, key, entry)
        return entry, true
    }
    // Redis miss，读内存
    return f.fallback.Get(ctx, key)
}

func (f *FallbackCache) Set(ctx, key, entry) {
    // 先写 Redis，再写内存
    f.primary.Set(ctx, key, entry)
    f.fallback.Set(ctx, key, entry)
}
```

**为什么写时先写 Redis 再写内存？**

- 如果先写内存，Redis 写失败 → 内存有新数据，Redis 没有 → 其他实例读不到
- 先写 Redis，即使内存写失败，其他实例也能从 Redis 读到

**为什么读时 Redis miss 还要读内存？**

- Redis 可能暂时不可用（网络抖动）
- 内存里可能有之前 Redis 正常时写入的数据
- 这是"fail-soft"：不保证最新，但保证有数据可用

### 1.3 缓存一致性模型

Gateyes 的缓存是 **Eventual Consistency（最终一致）**：

```
Instance A 写入缓存 K=V1
Instance B 读取缓存 K → 可能读到 V1（从 Redis），也可能读到旧值（从内存）
```

**为什么不强一致？**

- LLM 响应本身就是"可接受过时"的
- 强一致需要分布式锁或 2PC，性能损耗大
- 缓存 TTL 短（默认不长），不一致窗口有限

**如果出现不一致怎么办？**

- 等 TTL 过期后自动恢复
- 或者提供 admin 接口手动清除缓存（Gateyes 目前没有）

---

## 2. 缓存 Key 设计

### 2.1 当前设计

```go
func buildCacheKey(req *provider.ResponseRequest) string {
    // 包含：model + 请求体序列化后的 hash
    body, _ := json.Marshal(req)
    hash := sha256.Sum256(body)
    return fmt.Sprintf("cache:%s:%x", req.Model, hash)
}
```

**问题**：

- 没有版本号：如果响应格式变了，旧缓存可能返回不兼容的 JSON
- 没有租户隔离：不同租户的相同请求会命中同一个缓存（可能泄漏数据）

**改进方案**：

```go
func buildCacheKey(tenantID string, schemaVersion int, req *provider.ResponseRequest) string {
    body, _ := json.Marshal(req)
    hash := sha256.Sum256(body)
    return fmt.Sprintf("cache:v%d:%s:%s:%x", schemaVersion, tenantID, req.Model, hash)
}
```

### 2.2 缓存 Value 设计

```go
type Entry struct {
    Response  []byte    // 响应 JSON
    Stream    bool      // 是否是流式响应
    Model     string    // 模型名
    Provider  string    // provider 名
    Usage     Usage     // token 使用量
    CreatedAt int64     // 创建时间戳
}
```

**为什么存 CreatedAt 而不是 TTL？**

- 写入时不知道对方的 TTL 配置
- 读取时根据 CreatedAt 和当前时间判断是否过期
- 更灵活：不同业务可以有不同的 TTL 策略

---

## 3. 流式响应缓存

### 3.1 流式 vs 非流式的缓存差异

| 维度     | 非流式        | 流式                    |
| -------- | ------------- | ----------------------- |
| 缓存内容 | 完整响应 JSON | 完整响应 JSON           |
| 命中时   | 直接返回 JSON | 从 JSON 重建 SSE 事件流 |
| 缓存 key | 相同          | 相同                    |
| 存储大小 | 相同          | 相同                    |

### 3.2 重建 SSE 流的代码逻辑

```go
func replayCachedStream(ctx, entry cache.Entry, out chan<- ResponseEvent) {
    resp := entry.Response
  
    // 1. 发送 ResponseStarted 事件
    out <- ResponseEvent{
        Type: EventResponseStarted,
        Response: &provider.Response{
            ID: resp.ID,
            Model: resp.Model,
            Status: "in_progress",
        },
    }
  
    // 2. 把响应内容拆成 SSE delta 事件
    for _, output := range resp.Output {
        switch output.Type {
        case "message":
            for _, content := range output.Content {
                if content.Text != "" {
                    out <- ResponseEvent{
                        Type:  EventContentDelta,
                        Delta: content.Text,
                    }
                }
            }
        case "function_call":
            out <- ResponseEvent{
                Type:   EventToolCallDone,
                Output: &output,
            }
        }
    }
  
    // 3. 发送 ResponseCompleted 事件
    out <- ResponseEvent{
        Type:         EventResponseCompleted,
        Response:     resp,
        FinishReason: "stop",
    }
}
```

**面试会问**：为什么不直接缓存 SSE 字节流？

**答**：

1. SSE 字节流包含 `id:`, `event:`, `data:` 等协议帧，不同客户端可能解析方式不同
2. 字节流中的 `created` 时间戳、`id` 等字段如果复用会暴露"这是缓存"的事实
3. 存 JSON 更紧凑，没有协议开销
4. 重建时可以按当前请求的 trace_id 重新生成响应 ID

### 3.3 流式缓存的边界情况

**问题 1：缓存的流式响应和原始流的事件数量可能不同**

原始流：50 个 delta 事件
缓存重建：把所有文本合并成 1 个 delta 事件

**影响**：客户端收到的事件数量变了，但内容一样。大多数 SSE 客户端只关心内容，不关心事件数量。

**问题 2：tool_call 的流式处理**

原始流：`delta: {"function": {"name": "get_"}}` → `delta: {"function": {"arguments": "{\"url\":"}}` → ...
缓存重建：直接发送完整的 tool_call

**影响**：客户端收到的是完整的 function_call，不是增量。OpenAI SDK 可以处理（它会在内部组装）。

---

## 4. 缓存三大问题完整防护

### 4.1 缓存穿透（Cache Penetration）

**现象**：大量请求查询一个**一定不存在**的 key，每次都穿透到上游。

**典型场景**：

- 攻击者发送随机 model 名（如 `gpt-99999`）
- 每次都 miss，每次都打到 provider

**Gateyes 现状**：没有专门的防穿透机制。

**解决方案**：

```go
// 方案 A：布隆过滤器
// 启动时加载所有合法 model 名到布隆过滤器
// 请求来时先查布隆过滤器，不存在的直接拒绝

// 方案 B：空值缓存
// 对于不存在的 key，缓存一个空值（短 TTL，如 10 秒）
func (c *Cache) Get(ctx, key) (Entry, bool) {
    entry, ok := c.backend.Get(ctx, key)
    if ok {
        if entry.IsEmpty {
            return Entry{}, false // 空值，表示之前查过且不存在
        }
        return entry, true
    }
    // 未命中，查上游
    resp, err := upstream.Call(key)
    if err != nil {
        // 上游返回 404，缓存空值
        c.backend.Set(ctx, key, Entry{IsEmpty: true}, 10*time.Second)
        return Entry{}, false
    }
    c.backend.Set(ctx, key, resp, defaultTTL)
    return resp, true
}

// 方案 C：请求合法性校验（Gateyes 已做）
// GuardLLMRequest 会校验 model 白名单，非法 model 直接拒绝
// 这实际上已经防止了大部分穿透攻击
```

### 4.2 缓存击穿（Cache Breakdown）

**现象**：一个**热点 key** 过期，大量请求同时打到上游。

**典型场景**：

- "写一篇关于 AI 的摘要" 这个 prompt 被大量用户使用
- 缓存 TTL 到了，1000 个并发请求同时 miss
- 1000 个请求同时打到 OpenAI API

**Gateyes 现状**：没有专门的防击穿机制。

**解决方案**：

```go
// 方案 A：互斥锁（Mutex）
type Cache struct {
    backend CacheBackend
    locks   map[string]*sync.Mutex // 每个 key 一个锁
}

func (c *Cache) Get(ctx, key) (Entry, bool) {
    // 1. 先查缓存
    if entry, ok := c.backend.Get(ctx, key); ok {
        return entry, true
    }
  
    // 2. 获取 key 的互斥锁
    lock := c.getLock(key)
    lock.Lock()
    defer lock.Unlock()
  
    // 3. 双重检查（其他 goroutine 可能已经写入）
    if entry, ok := c.backend.Get(ctx, key); ok {
        return entry, true
    }
  
    // 4. 只有一个人去查上游
    resp, err := upstream.Call(key)
    if err != nil {
        return Entry{}, false
    }
    c.backend.Set(ctx, key, resp, defaultTTL)
    return resp, true
}

// 方案 B：逻辑过期（Logical Expiration）
// 不真正删除 key，而是标记为"已过期"
// 一个 goroutine 去刷新，其他 goroutine 返回旧值
type Entry struct {
    Data      []byte
    ExpireAt  time.Time
    LogicalAt time.Time // 逻辑过期时间（比 ExpireAt 早）
}

func (c *Cache) Get(ctx, key) (Entry, bool) {
    entry, ok := c.backend.Get(ctx, key)
    if !ok {
        return Entry{}, false
    }
  
    // 逻辑过期但物理未过期：返回旧值，异步刷新
    if time.Now().After(entry.LogicalAt) && time.Now().Before(entry.ExpireAt) {
        go c.refresh(key) // 异步刷新
        return entry, true // 返回旧值
    }
  
    // 物理过期：走正常逻辑
    if time.Now().After(entry.ExpireAt) {
        return Entry{}, false
    }
  
    return entry, true
}
```

### 4.3 缓存雪崩（Cache Avalanche）

**现象**：大量 key **同时过期**，大量请求同时打到上游。

**典型场景**：

- 所有缓存设置相同的 TTL（如 3600 秒）
- 某一时刻大量 key 同时过期
- 瞬间流量打到上游，导致上游宕机

**Gateyes 现状**：TTL 是统一的（从 entry.CreatedAt 计算）。

**解决方案**：

```go
// 方案 A：随机 TTL
// 在基础 TTL 上增加随机偏移
ttl := baseTTL + time.Duration(rand.Intn(300)) * time.Second // 基础 TTL + 0~300 秒随机

// 方案 B：阶梯过期
// 不同维度的缓存设置不同 TTL
// 热点数据 TTL 更长，冷数据 TTL 更短

// 方案 C：永不过期 + 后台刷新
// 热点 key 设置永不过期，后台 goroutine 定期刷新
```

---

# 第三部分：Redis 深度解析

## 1. Redis 单线程模型

### 1.1 为什么单线程还能这么快？

```
Redis 处理流程：
1. 从网络读取请求（epoll，非阻塞 I/O）
2. 解析请求（内存操作，O(1)）
3. 执行命令（内存操作，O(1)）
4. 发送响应（内核缓冲区）
```

**关键**：Redis 的命令执行时间极短（微秒级），瓶颈不在 CPU，而在网络 I/O。单线程避免了：

- 线程上下文切换开销
- 锁竞争
- 数据同步的复杂度

### 1.2 什么时候单线程会成为瓶颈？

```
场景 A：大 key 操作
DEL 一个包含 100 万个元素的 Hash → 执行时间 100ms
这 100ms 内，Redis 不处理任何其他请求

场景 B：复杂 Lua 脚本
脚本执行 5 秒 → 5 秒内 Redis 完全阻塞

场景 C：高并发 + 大 value
每次请求读 1MB 的 value，1000 并发 → 网络带宽成为瓶颈
```

**Gateyes 的限流 Lua 脚本**：

- 两次 HGET + 一次 HSET + 一次 EXPIRE
- 执行时间 < 1ms
- 对 Redis 性能影响极小

### 1.3 Redis 6.0+ 的多线程 I/O

Redis 6.0 引入了**多线程 I/O**（命令执行仍是单线程）：

```
网络读取 → 多线程并行
命令执行 → 单线程串行
网络写入 → 多线程并行
```

**对 Gateyes 的影响**：

- 使用 Redis 6.0+ 时，高并发场景下网络 I/O 不再是瓶颈
- 但 Lua 脚本执行仍是单线程，大脚本或大量并发 Lua 仍可能阻塞

---

## 2. Pipeline / 事务 / Lua 的区别

### 2.1 Pipeline（管道）

```
客户端：发送命令1 → 发送命令2 → 发送命令3（不等待响应）
客户端：接收响应1 ← 接收响应2 ← 接收响应3
```

**特点**：

- 只是减少了网络往返次数（RTT）
- 命令之间**没有原子性保证**
- 其他客户端的命令可能穿插在 pipeline 的命令之间

**不适合限流场景**：因为限流需要"读 → 计算 → 写"的原子性。

### 2.2 事务（MULTI / EXEC）

```
MULTI
SET key1 value1
SET key2 value2
EXEC
```

**特点**：

- 命令放入队列，EXEC 时一次性执行
- 执行期间不处理其他命令（类似 Lua）
- **但**：如果某个命令语法错误，整个事务不会执行
- **Watch 乐观锁**：可以监控 key，如果被修改则事务回滚

**不适合限流场景**：

- 事务中的命令是预先确定的，不能根据读取结果做条件判断
- 限流需要"读取当前 token → 判断 → 写回"的条件逻辑

### 2.3 Lua 脚本

```
EVAL "...script..." 1 key1 arg1 arg2
```

**特点**：

- 脚本作为整体执行，原子性
- 可以在脚本内做条件判断、循环
- 脚本执行期间不处理其他命令

**最适合限流场景**：因为需要"读 → 条件判断 → 写"的原子性。

### 2.4 对比总结

| 特性         | Pipeline | 事务 | Lua |
| ------------ | -------- | ---- | --- |
| 减少 RTT     | ✅       | ✅   | ✅  |
| 原子性       | ❌       | ✅   | ✅  |
| 条件判断     | ❌       | ❌   | ✅  |
| 执行期间阻塞 | ❌       | ✅   | ✅  |
| 适合限流     | ❌       | ❌   | ✅  |

---

## 3. Redis Cluster 槽迁移

### 3.1 槽迁移的流程

```
1. 目标节点导入槽：CLUSTER SETSLOT <slot> IMPORTING <source-node-id>
2. 源节点导出槽：CLUSTER SETSLOT <slot> MIGRATING <target-node-id>
3. 源节点发送键：MIGRATE <target-ip> <target-port> <key> 0 <timeout>
4. 更新槽归属：CLUSTER SETSLOT <slot> NODE <target-node-id>
```

### 3.2 迁移期间的客户端影响

```
客户端请求 key → Redis 返回 MOVED 或 ASK

MOVED：key 已经迁移到新节点，客户端需要重定向
ASK：key 正在迁移中，先问目标节点
```

go-redis 的 ClusterClient 会自动处理 MOVED 和 ASK 重定向，对应用透明。

### 3.3 对 Gateyes 的影响

**正常情况**：go-redis 自动处理重定向，无需修改。

**极端情况**：

- 槽迁移期间，Lua 脚本中的 key 可能暂时不可用
- 但这种情况极少见，且 Gateyes 的 fail-open 设计会放行

---

## 4. Redis 持久化对缓存的影响

### 4.1 RDB 快照

```
BGSAVE → fork 子进程 → 子进程写 RDB 文件 → 主进程继续服务
```

**对缓存的影响**：

- RDB 是某个时间点的快照，重启后恢复到该时间点
- 缓存数据丢失：RDB 期间新写入的缓存数据在下次 RDB 前可能丢失
- 但缓存数据本身就可以重建，丢失不致命

### 4.2 AOF 日志

```
每次写操作追加到 AOF 文件
```

**对缓存的影响**：

- AOF 更持久，但文件更大，恢复更慢
- 缓存数据量大时，AOF 文件会膨胀

### 4.3 混合模式（RDB + AOF）

Redis 4.0+ 支持：RDB 做全量快照，AOF 记录增量。

**Gateyes 建议**：

- 缓存数据用 **RDB + 较短 TTL**，不需要 AOF
- 限流数据用 **AOF 或混合模式**，因为限流状态重启后需要恢复
- 但 Gateyes 的限流状态是临时的（120s TTL），丢失也不致命

---

## 5. 生产环境 Redis 调优

### 5.1 连接池配置

```yaml
redis:
  poolSize: 50        # 连接池大小（默认 10 * CPU 核数）
  minIdleConns: 10    # 最小空闲连接
  maxRetries: 3       # 命令失败重试次数
  dialTimeoutMs: 5000 # 连接超时
  readTimeoutMs: 3000 # 读超时
  writeTimeoutMs: 3000 # 写超时
```

**poolSize 怎么定？**

- 公式：`并发 QPS * 平均命令执行时间 / 1000`
- 例如：1000 QPS，命令平均 10ms → 需要 10 个连接
- 留 3-5 倍余量：poolSize = 50

### 5.2 内存配置

```
maxmemory 4gb
maxmemory-policy allkeys-lru
```

**为什么用 allkeys-lru？**

- 缓存数据全部可以重建，LRU 淘汰最合理
- 不需要 allkeys-lfu（缓存访问频率差异不大）

### 5.3 监控指标

| 指标                            | 告警阈值        | 含义                          |
| ------------------------------- | --------------- | ----------------------------- |
| used_memory                     | > 80% maxmemory | 内存不足，开始淘汰            |
| connected_clients               | > poolSize * 2  | 连接泄漏                      |
| blocked_clients                 | > 0             | 有阻塞命令（Lua 执行太久）    |
| instantaneous_ops_per_sec       | > 100000        | 负载过高                      |
| keyspace_hits / keyspace_misses | hit rate < 50%  | 缓存失效                      |
| expired_keys                    | 突增            | 大量 key 同时过期（雪崩风险） |
| evicted_keys                    | > 0             | 内存淘汰开始                  |

### 5.4 高可用架构

```
        ┌─────────────┐
        │   Sentinel  │
        │  (3 nodes)  │
        └──────┬──────┘
               │ 监控 + 自动故障转移
        ┌──────┴──────┐
        │             │
   ┌────┴────┐   ┌────┴────┐
   │  Master │   │  Slave  │
   │  (写)   │   │  (读)   │
   └─────────┘   └─────────┘
```

**Gateyes 的配置**：

- 只用一个 Redis 实例（主从用于高可用，不是读写分离）
- 因为限流和缓存都是写多读多，读写分离收益不大
- Sentinel 自动故障转移，主宕机时从变主

---

# 总结

| 主题                   | 核心要点                                        |
| ---------------------- | ----------------------------------------------- |
| **限流算法**     | 令牌桶最适合网关：允许突发、内存友好、实现简单  |
| **分布式限流**   | Redis Lua 保证原子性，Cluster 下必须加 hash tag |
| **缓存设计**     | Redis + 内存双层，fail-soft，最终一致           |
| **流式缓存**     | 存 JSON 重建 SSE，不存字节流                    |
| **Redis 单线程** | 命令执行原子，Lua 执行期间阻塞，脚本必须短小    |
| **生产调优**     | 连接池、内存策略、监控指标、高可用架构          |
