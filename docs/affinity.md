# Affinity Layer（亲和层）

> 适用版本：v0.2.0+
> 最后更新：2026-05-03

---

## 概述

Affinity Layer 是 Router 中的一个**软固定层**，位于 ranker 之后、strategy 之前。它的作用是在不改变候选集大小的前提下，把"偏好 provider"移到队首，让后续策略更有可能选中它。

**核心目标**：
- **SessionAffinity**：同一 session 的请求固定到同一 provider，保证多轮对话上下文连续性
- **PrefixAffinity**：同一 prompt 前缀的请求固定到同一 provider，提升后端 prefix-cache 命中率（如 vLLM）

**设计原则**：
- 软固定，非强制绑定（strategy 仍可重新排序）
- 与 strategy 解耦（affinity 只管 pin，strategy 只管选）
- 记忆状态可 TTL 过期，防止 provider 长期独占

---

## 在路由管线中的位置

```
Tenant Providers
      │
      ▼
┌─────────────┐
│ FilterRoutable│  健康/能力/权重过滤
└──────┬──────┘
       │ candidates
       ▼
┌─────────────┐
│  ruleEngine  │  分流规则过滤
└──────┬──────┘
       │ filtered candidates
       ▼
┌─────────────┐
│   ranker     │  重排序（预留 ML）
└──────┬──────┘
       │ ranked candidates
       ▼
┌─────────────┐
│  affinity    │  ← 软固定层（本文档）
│   (Pin)      │     把偏好 provider 移到队首
└──────┬──────┘
       │ pinned candidates
       ▼
┌─────────────┐
│   strategy   │  最终排序/选择
└──────┬──────┘
       │ ordered candidates
       ▼
  业务层重试/fallback
```

---

## 接口设计

```go
type Affinity interface {
    // Pin 把偏好 provider 移到 candidates[0]，其余保持相对顺序
    Pin(ctx context.Context, candidates []string, routeCtx RouteContext) []string

    // Promote 在请求成功后更新 affinity 状态
    Promote(ctx context.Context, chosen string, routeCtx RouteContext)
}
```

### 关键约定

- `Pin` 不改变候选集大小，只重排
- 如果 affinity 没有记忆（首次请求或 TTL 过期），直接返回原顺序
- `Promote` 是异步的，不阻塞响应返回

---

## 三种实现

### 1. SessionAffinity

按 `sessionID` 做**加权一致哈希**，同一 session 固定到同一 provider。

**算法**：
```
hash = fnv1a(sessionID + providerName)
weight = hash % 10000
chosen = max(weight) over all candidates
```

**特性**：
- 确定性：相同 sessionID 总是得到相同 provider
- 分布性：不同 session 均匀分布到所有候选 provider
- TTL 控制：记忆过期后重新计算

**配置**：
```yaml
router:
  affinity:
    sessionTTL: 10m  # session 记忆时长
```

**向后兼容**：
旧版 `strategy: sticky` 已迁移为 `SessionAffinity`。当 `strategy="sticky"` 时，路由器自动创建 `SessionAffinity` 实例，strategy 层直接返回 affinity 已排好的顺序。

### 2. PrefixAffinity

按请求 prompt 的**前 N 个 rune** 做哈希，同一前缀固定到同一 provider。

**算法**：
```
prefix = inputText[0:prefixDepth]  // rune 级别截断
hash   = sha256(prefix + providerName)
weight = hash % 10000
chosen = max(weight) over all candidates
```

**用途**：
- 提升后端 prefix-cache 命中率（vLLM 等推理引擎会对相同前缀 KV cache）
- 适合 prompts 有固定模板前缀的场景（如 system prompt 相同）

**配置**：
```yaml
router:
  affinity:
    prefixTTL: 30m   # 前缀记忆时长
    prefixDepth: 64  # 前 64 个 rune 参与哈希
```

### 3. CompositeAffinity

组合多种 affinity，**第一个非平凡 pin 获胜**。

```
for each affinity in chain:
    pinned = affinity.Pin(candidates, routeCtx)
    if pinned[0] != candidates[0]:
        return pinned  // 第一个产生效果的 affinity 获胜
return candidates
```

默认链：`[SessionAffinity, PrefixAffinity]`

这意味着：
1. 如果 session 有记忆 → 按 session 固定
2. 否则如果前缀有记忆 → 按前缀固定
3. 否则 → 无 affinity 效果

---

## 性能基准

测试环境：Intel i9-13980HX, Windows 11, Go 1.24

| 操作 | 耗时 | allocs |
|------|------|--------|
| SessionAffinity.Pin | ~90 ns/op | 1 alloc |
| PrefixAffinity.Pin | ~500 ns/op | 4 allocs |
| CompositeAffinity.Pin | ~110 ns/op | 1 alloc |
| Router.OrderCandidates (含 affinity) | ~1.05 μs/op | 11 allocs |
| Router.Select (含 affinity+strategy) | ~270 ns/op | 4 allocs |

**解读**：
- affinity 层开销极低（<1μs），对整体路由延迟无显著影响
- PrefixAffinity 比 SessionAffinity 慢 ~5x，因为涉及 SHA-256 和字符串截断
- 整体路由选择（含过滤+规则+排序+亲和+策略）< 1.1μs

---

## 配置示例

```yaml
router:
  strategy: round_robin  # 主策略
  affinity:
    enabled: true
    sessionTTL: 10m      # session 记忆 10 分钟
    prefixTTL: 30m       # prefix 记忆 30 分钟
    prefixDepth: 64      # 前 64 个 rune
```

### 旧配置兼容

```yaml
# 旧版（仍然有效，自动映射到 affinity 层）
router:
  strategy: sticky

# 等价新版
router:
  strategy: round_robin  # strategy 不再负责 sticky
  affinity:
    enabled: true
    sessionTTL: 10m
```

---

## 与 Strategy 的协作

| strategy | affinity 生效方式 |
|----------|-------------------|
| round_robin | affinity pin 到队首，round_robin 从队首开始轮转 |
| random | affinity pin 到队首，random 打乱其余候选 |
| least_load | affinity pin 到队首，least_load 按负载排序其余候选 |
| cost_based | affinity pin 到队首，cost_based 按单价排序其余候选 |
| sticky | affinity 全权负责，strategy 直接返回 affinity 结果 |

---

## 测试覆盖

`internal/service/router` 测试覆盖 **84.4%**，affinity 相关测试包括：

- SessionAffinity：Pin/Promote/TTL 过期/空 session 回退/哈希分布均匀性
- PrefixAffinity：Pin/Promote/前缀截断/rune 边界
- CompositeAffinity：链式优先级/全部 miss 回退
- Router 集成：affinity + strategy 组合验证、sticky 向后兼容
- Benchmark：Pin/Select/OrderCandidates 性能基准

---

## 边界与限制

1. **软固定非强制**：affinity 只把偏好 provider 移到队首，如果 strategy 是 random，仍有概率选到别的 provider
2. **TTL 全局统一**：不能按 provider 或 tenant 设置不同 TTL
3. **内存状态非持久**：affinity 记忆存储在进程内存中，重启后丢失
4. **无跨 pod 同步**：多副本部署时，每个 pod 的 affinity 记忆独立
5. **prefixDepth 固定**：不能按 prompt 长度自适应调整前缀深度
