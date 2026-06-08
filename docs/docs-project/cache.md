# L1 Response Cache

> 适用版本：v0.2.0+
> 最后更新：2026-05-03

---

## 概述

L1 Cache 是 Gateway 层的**精确匹配响应缓存**。当两个请求在 `(tenant, model, prompt, stream, surface)` 五个维度上完全一致时，后者直接返回缓存结果，无需再次调用上游 provider。

**设计边界**：
- L1 是**精确匹配**（exact-match），不做语义相似度判断
- 语义缓存属于未来的 L2 层，不在当前范围内
- 缓存的是上游返回的**完整响应体**，不是 embedding 向量

**核心价值**：
- 消除重复上游调用，降低 API 成本
- 减少延迟（缓存命中时 ~60ns 内存查找 vs 数百 ms 上游 RTT）
- 在上游故障时提供降级能力（Redis 故障→内存缓存→直接透传）

---

## 架构

```
┌─────────────────┐
│  responses.Service │
│  (business layer)  │
└────────┬────────┘
         │ lookupCache / writeCache
         ▼
┌─────────────────┐
│   FallbackCache  │
│  (wrapper layer) │
└────────┬────────┘
    primary │     │ fallback
            ▼     ▼
    ┌─────────┐ ┌──────────┐
    │RedisCache│ │MemoryCache│
    │ (shared) │ │ (per-pod) │
    └─────────┘ └──────────┘
```

### 三层结构

| 层级 | 类型 | 职责 |
|------|------|------|
| 业务层 | `responses.Service` | 决策是否跳过缓存、构建 cache key、流式回放缓存 |
| 包装层 | `FallbackCache` | Redis 故障时自动降级到内存缓存 |
| 后端层 | `RedisCache` / `MemoryCache` | 实际存储与检索 |

---

## Cache Key 设计

### 输入维度

```go
type KeyInput struct {
    TenantID    string // 多租户隔离
    Model       string // 不同模型响应不同
    PromptCanon string // 规范化后的请求体
    Stream      bool   // 流式/非流式隔离
    Surface     string // "responses" | "chat_completions" | "messages" | "embeddings"
}
```

### 生成算法

```
key = "gw:l1:" + hex(sha256(tenantID + NUL + model + NUL + surface + NUL + promptCanon + NUL + streamFlag))
```

**为什么用 SHA-256**：
- 固定长度 64 字符，不受 prompt 长度影响
- 碰撞概率极低（2^256），适合缓存场景
- Redis key 长度可控

**为什么用 CanonicalizeJSON**：
- `{"a":1,"b":2}` 和 `{"b":2,"a":1}` 是同一个 key
- 排除 JSON key 顺序差异导致的缓存失效

### 性能基准

| 操作 | 耗时 | 备注 |
|------|------|------|
| BuildKey | ~450 ns/op | SHA-256 哈希 |
| CanonicalizeJSON | ~6.6 μs/op | **当前瓶颈**，JSON 编解码+排序 |
| MemoryCache.Get | ~60 ns/op | O(1) map 查找 |
| MemoryCache.Set | ~25 ns/op | 0 allocs/op |
| RedisCache.Get | ~45 μs/op | 含网络往返 |

> **优化建议**：CanonicalizeJSON 是 cache key 构建的主要开销。如果 prompt 很大（>4KB），建议考虑前置 hash 或截断。

---

## FallbackCache：故障降级

### 行为契约

| 操作 | primary (Redis) | fallback (Memory) | 返回值 |
|------|----------------|-------------------|--------|
| Get | hit → 返回 | miss/error → 尝试 fallback | primary 优先 |
| Set | 同步写入 | fire-and-forget 写入 | primary 的错误 |
| Delete | 同步删除 | 同步删除 | 永远 nil |
| Stats | primary 的统计 | — | primary 优先 |

**Fail-Open 设计**：
- Redis 连接断开时 → 自动降级到 MemoryCache
- MemoryCache 也没有 → 视为 miss，继续走上游
- 上游返回后 → 写入两级缓存，下次即可命中

### 为什么不用 Redis Cluster 做主从冗余

- Gateway 已经依赖 Redis 做分布式限流，Redis 故障本身是小概率事件
- MemoryCache 作为进程内 LRU，在 Redis 故障期间提供"最近最热"的缓存命中
- 如果 Redis 和 MemoryCache 都不可用，Fail-Open 保证请求仍能通过

---

## 存储格式

### Entry 结构

```go
type Entry struct {
    Response  []byte `json:"response,omitempty"`   // 非流式：完整响应体
    StreamRaw []byte `json:"stream_raw,omitempty"` // 流式：SSE 原始数据
    Stream    bool   `json:"stream"`               // 是否流式
    Model     string `json:"model"`                // 实际使用的模型
    Provider  string `json:"provider"`             // 实际命中的 provider
    Usage     Usage  `json:"usage"`                // token 用量
    CreatedAt int64  `json:"created_at"`           // 缓存时间戳
}
```

### Redis 存储

- 序列化：JSON
- TTL：由配置 `cache.redis.defaultTTL` 控制，默认 1 小时
- Key 前缀：`gw:l1:`

### 内存存储

- 数据结构：map + 双向链表（标准 LRU）
- 容量：配置 `cache.memory.capacity`，默认 1024 条
- TTL：惰性淘汰（Get 时检查过期时间）
- 并发安全：`sync.Mutex` 全锁

---

## 缓存命中与跳过策略

### 什么请求会被缓存

- 成功返回的 LLM 写请求（responses / chat / messages）
- 成功返回的 embeddings 请求
- 非流式和流式响应都会被缓存（分开存储）

### 什么请求**跳过**缓存

| 场景 | 原因 |
|------|------|
| 请求包含 `tools` | tool call 结果非确定性，缓存可能返回过期 tool 定义 |
| 请求包含 `images` | 图片输入不可哈希 |
| 请求体解析失败 | 无法提取 canonical prompt |
| 上游返回错误 | 只缓存成功响应 |
| 配置 `cache.enabled=false` | 全局关闭 |

### 流式缓存回放

流式命中时，`replayCachedStream` 会：
1. 发送 `EventResponseStarted`
2. 发送 `EventResponseCompleted`（携带完整缓存响应）
3. 关闭 channel

客户端感知与正常流式响应一致。

---

## 分布式一致性

### 一致性模型

L1 Cache 采用 **最终一致性**（eventual consistency）：
- 写入 Redis 后，所有 Gateway pod 立即可见
- 写入 MemoryCache 后，仅本 pod 可见
- TTL 到期后自动失效

### 多副本场景

| 场景 | 行为 |
|------|------|
| Pod A 缓存写入 | Pod B 通过 Redis 可见 |
| Redis 故障期间 Pod A 写入 MemoryCache | Pod B 不可见，各自独立 |
| Redis 恢复后 | 新写入同步到 Redis，历史 MemoryCache 条目逐步被 LRU 淘汰 |

### 缓存失效

当前没有主动失效机制（如订阅 Redis pub/sub 做集群失效）。缓存通过 TTL 自动过期。如果需要强制刷新：
- 调短 TTL
- 或重启 Gateway（MemoryCache 清空）

---

## 配置

```yaml
cache:
  enabled: true
  # Redis 主缓存（多副本共享）
  redis:
    enabled: true
    defaultTTL: 1h
  # 内存 fallback（单副本本地）
  memory:
    enabled: true
    capacity: 1024
    defaultTTL: 30m
```

完整配置示例见 `configs/config.example.yaml`。

---

## 监控指标

Cache 命中/未命中通过 `CacheMetrics` 接口上报：

| 指标 | 类型 | label |
|------|------|-------|
| `gateway_cache_hits_total` | Counter | `layer=l1` / `layer=l1_stream` |
| `gateway_cache_misses_total` | Counter | `layer=l1` / `layer=l1_stream` |

计算命中率：
```promql
rate(gateway_cache_hits_total[5m])
/
(rate(gateway_cache_hits_total[5m]) + rate(gateway_cache_misses_total[5m]))
```

---

## 测试覆盖

`internal/service/cache` 测试覆盖 **90.9%**，包含：

- 单元测试：MemoryCache Get/Set/Delete/TTL/LRU 淘汰
- 集成测试：RedisCache JSON 序列化、TTL 边界
- 分布式一致性测试：并发 Set/Get、Redis 故障降级、碰撞抵抗
- Benchmark：Get/Set/BuildKey/CanonicalizeJSON

---

## 边界与限制

1. **精确匹配局限**：`"hello"` 和 `"hello "`（多了空格）不会命中同一缓存
2. **CanonicalizeJSON 开销**：大 prompt 时 ~6.6μs 的编解码开销不可忽视
3. **内存缓存容量**：默认 1024 条，生产环境建议根据 pod 内存调整
4. **无主动失效**：TTL 是唯一的失效机制
5. **流式回放简化**：流式命中只发 2 个事件，不还原原始 SSE 分块节奏
