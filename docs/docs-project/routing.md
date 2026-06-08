# 智能路由与负载均衡

本文档说明 Gateyes 如何将请求路由到最优 provider，覆盖从候选过滤到最终选择的完整管线。

---

## 路由管线总览

```
Tenant Providers
      │
      ▼
┌─────────────┐
│ Filter      │  健康/能力/权重过滤 + registry 元数据
└──────┬──────┘
       │ candidates
       ▼
┌─────────────┐
│ ruleEngine  │  分流规则（first match wins）
└──────┬──────┘
       │ filtered candidates
       ▼
┌─────────────┐
│ ranker      │  重排序（预留 ML，当前直通）
└──────┬──────┘
       │ ranked candidates
       ▼
┌─────────────┐
│ affinity    │  软固定层（Session / Prefix）
│   (Pin)     │
└──────┬──────┘
       │ pinned candidates
       ▼
┌─────────────┐
│ strategy    │  最终排序/选择
└──────┬──────┘
       │ ordered candidates
       ▼
  业务层重试 / fallback
```

整条管线耗时 < 1.5μs，对请求延迟几乎无影响。

---

## 1. 候选过滤

### 1.1 健康检查过滤

```
enabled=false 或 drain=true    → 排除
health_status=unhealthy        → 排除
health_status=healthy/degraded → 允许
```

### 1.2 能力感知过滤

根据请求特征匹配 provider 能力：

| 请求特征 | 要求的能力 |
|----------|-----------|
| `stream=true` | `supports_stream` |
| 含 `tools` | `supports_tools` |
| 含 `images` | `supports_images` |
| 含 `structured_output` | `supports_structured_output` |
| surface=chat | `supports_chat` |
| surface=responses | `supports_responses` |
| surface=messages | `supports_messages` |

### 1.3 权重排序

候选列表按 `routing_weight` 降序排列，权重默认为 1。

---

## 2. 规则引擎（ruleEngine）

按输入特征做分流，语义类似 Clash 规则：顺序匹配，first match wins。

### 匹配条件

| 条件 | 说明 |
|------|------|
| `models` | 模型名匹配 |
| `minPromptTokens` / `maxPromptTokens` | prompt token 范围 |
| `hasTools` | 是否含工具调用 |
| `hasImages` | 是否含图片输入 |
| `hasStructuredOutput` | 是否含结构化输出 |
| `stream` | 是否流式 |
| `anyRegex` | prompt 文本正则匹配 |

### 配置示例

```yaml
router:
  strategy: least_load
  ruleEngine:
    enabled: true
    rules:
      - name: long-context-to-local
        match:
          minPromptTokens: 8000
        action:
          providers: [vllm-llama3]
      - name: code-traffic
        match:
          hasTools: true
          anyRegex:
            - "(?i)stack trace"
            - "(?i)debug"
        action:
          providers: [sglang-qwen]
```

规则命中后，候选集收缩到 `action.providers`。如果与当前 tenant 候选集无交集，回退到原候选集。

---

## 3. 亲和层（Affinity）

软固定层，不改变候选集大小，只把偏好 provider 移到队首。

### SessionAffinity

按 `sessionID` 做加权一致哈希，同一 session 固定到同一 provider。

```yaml
router:
  affinity:
    enabled: true
    sessionTTL: 10m
```

适用场景：多轮对话保持上下文连续性。

### PrefixAffinity

按 prompt 前 N 个 rune 做哈希，同一前缀固定到同一 provider。

```yaml
router:
  affinity:
    prefixTTL: 30m
    prefixDepth: 64
```

适用场景：提升后端 prefix-cache 命中率（vLLM 等推理引擎对相同前缀共享 KV cache）。

---

## 4. 路由策略

### 可用策略

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| `round_robin` | 轮询 | 负载均匀，provider 能力相近 |
| `random` | 随机 | 简单负载分散 |
| `least_load` | 最小并发负载 | 实时负载均衡 |
| `cost_based` | 成本优先 | 成本敏感场景 |
| `sticky` | 会话粘性（已迁移到 affinity） | 向后兼容 |

### 配置

```yaml
router:
  strategy: least_load
```

---

## 5. 重试与 Fallback

路由返回的是排序后的候选列表，业务层按顺序尝试：

1. 调用第一个 provider
2. 失败（超时/5xx/连接错误）→ 尝试下一个
3. 记录 retry / fallback 指标
4. 全部失败 → 返回 `no_provider` 错误

---

## 6. 负载跟踪

路由器维护 `loads map[string]int64` 记录每个 provider 的当前并发数：

- 请求发出前：`router.IncLoad(provider)`
- 请求完成后：`router.DecLoad(provider)`

`least_load` 策略基于实时并发计数排序。
