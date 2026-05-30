# 性能成本优化

本文档说明 Gateyes 如何在"本地 GPU 模型 + 云上 LLM API"的混合场景下实现性能与成本的平衡。

---

## 1. L1 响应缓存

精确匹配缓存，消除重复上游调用。

### 缓存维度

两个请求在以下维度完全一致时命中缓存：
- `tenantID` — 多租户隔离
- `model` — 不同模型响应不同
- `prompt` — 规范化后的请求体
- `stream` — 流式/非流式隔离
- `surface` — API 端点类型

### 双后端设计

```
┌─────────────────┐
│  FallbackCache   │
└────┬──────┬─────┘
     │      │
     ▼      ▼
┌────────┐ ┌──────────┐
│ Redis  │ │ Memory   │
│(shared)│ │ (per-pod)│
└────────┘ └──────────┘
```

- **Redis**：多副本共享，默认 TTL 1h
- **Memory LRU**：单副本本地，默认容量 1024 条
- **Fail-Open**：Redis 故障时自动降级到内存缓存

### 性能

| 操作 | 耗时 |
|------|------|
| 内存缓存命中 | ~60 ns |
| Redis 缓存命中 | ~45 μs |
| 上游调用（云上 API） | 100-500 ms |

**缓存命中一次 = 节省一次云上 API 调用成本**

### 跳过缓存的场景

- 请求含 `tools`（tool call 非确定性）
- 请求含 `images`（不可哈希）
- 上游返回错误（只缓存成功响应）

---

## 2. 预算治理

四级预算体系，从细到粗：

```
virtual_key → api_key → project → tenant
```

### 预算策略

| 策略 | 行为 |
|------|------|
| `hard_reject` | 预算耗尽直接拒绝（默认） |
| `soft_alert` | 预算告警但放行 |
| `grace` | 宽限期模式 |

### 成本计算

```
cost = prompt_tokens * price_input + completion_tokens * price_output
```

价格来自 `ProviderRegistryRecord.RuntimeConfig`：
- 本地 GPU 模型：`price_input = 0`，`price_output = 0`（或按内部核算价）
- 云上 API：按实际单价配置

### 配置示例

```yaml
providers:
  - name: openai-gpt4o
    type: openai
    priceInput: 0.005      # $/1K tokens
    priceOutput: 0.015

  - name: vllm-llama3
    type: openai
    priceInput: 0          # 本地部署，无按 token 计费
    priceOutput: 0
```

---

## 3. 成本感知路由

`cost_based` 策略按 provider 单价排序，优先选择更便宜的 provider。

```yaml
router:
  strategy: cost_based
```

混合场景典型配置：

```yaml
providers:
  - name: vllm-llama3
    type: openai
    priceInput: 0
    priceOutput: 0
    routingWeight: 5

  - name: openai-gpt4o
    type: openai
    priceInput: 0.005
    priceOutput: 0.015
    routingWeight: 1

router:
  strategy: cost_based
```

此时：
- 本地 GPU 模型（免费）优先被选中
- 只有本地模型不可用（故障/满载/能力不匹配）时才 fallback 到云上 API
- 云上 API 的用量和成本被精确追踪

---

## 4. 本地 GPU 优先的推荐配置

```yaml
providers:
  # 本地 GPU 模型：低成本、低延迟、固定 capacity
  - name: vllm-llama3-8b
    type: openai
    baseURL: http://vllm-router.llm-ns.svc.cluster.local/v1
    model: meta-llama/Llama-3.1-8B-Instruct
    priceInput: 0
    priceOutput: 0
    routingWeight: 10
    maxTokens: 8192

  - name: vllm-llama3-70b
    type: openai
    baseURL: http://vllm-router-70b.llm-ns.svc.cluster.local/v1
    model: meta-llama/Llama-3.1-70B-Instruct
    priceInput: 0
    priceOutput: 0
    routingWeight: 8
    maxTokens: 32768

  # 云上 API：兜底 + 特殊能力
  - name: openai-gpt4o
    type: openai
    baseURL: https://api.openai.com/v1
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4o
    priceInput: 0.005
    priceOutput: 0.015
    routingWeight: 1

  - name: anthropic-claude
    type: anthropic
    baseURL: https://api.anthropic.com
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-3-5-sonnet-20241022
    priceInput: 0.003
    priceOutput: 0.015
    routingWeight: 1

router:
  strategy: cost_based
  ruleEngine:
    enabled: true
    rules:
      # 长上下文走 70B 本地模型
      - name: long-context
        match:
          minPromptTokens: 4000
        action:
          providers: [vllm-llama3-70b]
      # 工具调用走 GPT-4o
      - name: tool-use
        match:
          hasTools: true
        action:
          providers: [openai-gpt4o]
```

---

## 5. 监控成本

### 关键指标

```promql
# 按 provider 统计请求量和成本
sum by (provider) (
  rate(gateway_llm_requests_total{result="success"}[5m])
)

# 按 provider 统计 token 消耗
sum by (provider, token_type) (
  rate(gateway_llm_tokens_total[5m])
)
```

### 管理接口

```bash
# 查看多级预算状态
curl http://localhost:8083/admin/budgets \
  -H "Authorization: Bearer admin:secret"

# 查看使用量汇总
curl http://localhost:8083/admin/usage/summary \
  -H "Authorization: Bearer admin:secret"

# 按 provider 分解
curl http://localhost:8083/admin/usage/breakdown?dimension=provider \
  -H "Authorization: Bearer admin:secret"
```
