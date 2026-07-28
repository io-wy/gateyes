# L1 Response Cache

> 最后更新：2026-07-28

---

## 概述

L1 Cache 是 Gateway 层的**精确匹配响应缓存**。当两个请求在 `(tenant, model, normalized request, stream, surface, bucket)` 维度上完全一致时，后者直接返回缓存结果，无需再次调用上游 provider。

**设计边界**：
- L1 是**精确匹配**（exact-match），不做语义相似度判断
- 语义缓存属于未来的 L2 层，不在当前范围内
- 缓存的是上游返回的**完整响应体**，不是 embedding 向量
- embeddings / images 当前不走 responses.Service 的 L1 response cache

**核心价值**：
- 消除重复上游调用，降低 API 成本
- 减少延迟（缓存命中时 ~60ns 内存查找 vs 数百 ms 上游 RTT）
- 在上游故障时提供降级能力（Redis 故障→内存缓存→直接透传）
- 通过 prompt rewrite 收敛动态请求差异，提高 gateway cache 和 provider prompt cache 命中机会

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
    Surface     string // "responses" | "chat" | "messages"
    Bucket      string // 可选请求级 bucket
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

### Key payload

`responses.Service.buildCacheKey` 会把以下字段纳入 canonical payload：

- `model`
- normalized `messages`
- `stream`
- `max_output_tokens` / `max_tokens`
- `tools`
- `output_format`
- `options.system`
- `options.thinking`
- `options.raw`

`prompt_cache_key` 不进入 gateway L1 key；它是 provider 侧 prompt cache hint，不影响响应语义。

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
- 非流式和流式响应都会被缓存（分开存储）

### 什么请求**跳过**缓存

| 场景 | 原因 |
|------|------|
| 请求包含 `tools` | tool call 结果非确定性，缓存可能返回过期 tool 定义 |
| 上游返回错误 | 只缓存成功响应 |
| 配置 `cache.enabled=false` | 全局关闭 |
| 配置 `cache.skipStream=true` | 跳过流式响应缓存 |
| 请求头 `X-Gateyes-Cache-Skip: true` | 单请求跳过 |

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
  backend: auto        # auto | memory；auto 优先 Redis，失败时回退内存
  defaultTTL: 300      # seconds；0 表示不过期
  capacity: 1000       # memory cache max entries
  skipStream: false
  skipTools: true
  singleflight: true
  promptRewrite: true
```

完整配置示例见 `configs/config.example.yaml`。

### Cache hints

请求头可以覆盖单次请求行为：

| Header | 作用 |
| --- | --- |
| `X-Gateyes-Cache-Skip: true` | 跳过 lookup/write |
| `X-Gateyes-Cache-Bucket: <value>` | 增加 key 维度，用于 A/B 或隔离 |
| `X-Gateyes-Cache-TTL: <duration>` | 覆盖本次写入 TTL |

响应头会暴露本次 cache 结果：

| Header | 说明 |
| --- | --- |
| `X-Gateyes-Cache-Result` | `hit` / `miss` / `skip` / `error` |
| `X-Gateyes-Cache-Layer` | `l1` / `l1_stream` |
| `X-Gateyes-Cache-Reason` | skip/error 原因 |
| `X-Gateyes-Cache-Rewrites` | prompt rewrite 命中的规则 |
| `X-Gateyes-Prompt-Cache-Key` | 注入或透传的 provider prompt cache key |

## Prompt Rewrite

`cache.promptRewrite=true` 时，gateway 会在 cache key 构建和上游请求前做保守归一化：

| Rewrite | 规则 | 目的 |
| --- | --- | --- |
| `anthropic_cch` | 将 Anthropic system prompt 中 `x-anthropic-billing-header` 的动态 `cch` 值替换为固定值 | 避免客户端动态计费标记破坏缓存 |
| `claude_code_date` | 稳定 Claude Code 注入的 `# currentDate` 日期格式 | 避免等价格式差异造成 cache key 分裂 |
| `prompt_cache_key` | OpenAI Chat/Responses 缺省时自动注入稳定 key | 提升 provider 侧 prompt cache bucketing |

本地消融测试 `TestPromptRewriteSavesUpstreamTokensOnDynamicCCH` 验证了动态 `cch` 场景：

| 组别 | 上游调用 | 上游 token |
| --- | ---: | ---: |
| baseline | 2 | 26 |
| prompt rewrite | 1 | 13 |
| saved | 1 | 13 |

---

## 监控指标

Cache 命中/未命中通过 `CacheMetrics` 接口上报：

| 指标 | 类型 | label |
|------|------|-------|
| `gateway_cache_lookups_total` | Counter | `layer,result` |
| `gateway_cache_writes_total` | Counter | `layer,result` |
| `gateway_cache_get_duration_seconds` | Histogram | `layer` |
| `gateway_cache_value_size_bytes` | Histogram | `layer` |
| `gateway_llm_tokens_total{token_type="cached"}` | Counter | provider 侧 cached tokens |
| `gateway_llm_prompt_cache_ratio` | Histogram | provider 侧 cached/prompt 比例 |

计算命中率：
```promql
rate(gateway_cache_lookups_total{result="hit"}[5m])
/
rate(gateway_cache_lookups_total[5m])
```

Admin API 也提供 `GET /admin/v1/cache/summary`，前端 Dashboard 会展示 lookup、hit rate、平均 lookup latency、平均 value size。

---

## 测试覆盖

相关测试包含：

- 单元测试：MemoryCache Get/Set/Delete/TTL/LRU 淘汰
- 集成测试：RedisCache JSON 序列化、TTL 边界
- 分布式一致性测试：并发 Set/Get、Redis 故障降级、碰撞抵抗
- responses 层：cache skip reason、headers/trace、stream replay、singleflight、prompt rewrite 消融
- Benchmark：Get/Set/BuildKey/CanonicalizeJSON

---

## 边界与限制

1. **精确匹配局限**：`"hello"` 和 `"hello "`（多了空格）不会命中同一缓存
2. **CanonicalizeJSON 开销**：大 prompt 时 ~6.6μs 的编解码开销不可忽视
3. **内存缓存容量**：默认 1024 条，生产环境建议根据 pod 内存调整
4. **无主动失效**：TTL 是唯一的失效机制
5. **流式回放简化**：流式命中只发 2 个事件，不还原原始 SSE 分块节奏
6. **Provider prompt cache 非强保证**：`prompt_cache_key` 和 prompt rewrite 只能提高命中机会，真实 cached tokens 仍以上游 usage/metrics 为准
