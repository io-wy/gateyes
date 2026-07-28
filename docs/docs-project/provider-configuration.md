# Provider 配置指南

> 最后更新：2026-07-28

本文档说明 Gateyes 当前如何接入本地推理引擎、云上 LLM API，以及如何通过 Admin API / 前端管理运行时 provider。

---

## 1. Provider 配置模型

Gateyes 把 OpenAI-compatible、Anthropic-compatible、Azure-compatible、本地 vLLM/SGLang 都收敛为同一种 provider 配置。

```yaml
providers:
  - name: <唯一标识>
    type: openai          # openai | anthropic | azure
    vendor: openai        # openai | anthropic | deepseek | vllm | sglang | ...
    endpoint: chat        # chat | responses | embeddings
    baseURL: <endpoint>
    apiKey: ${PROVIDER_API_KEY}
    model: <上游真实模型名>
    weight: 10
    priceInput: 0.000005
    priceOutput: 0.000015
    maxTokens: 128000
    timeout: 60
    enabled: true
    capabilities:
      chat: true
      responses: true
      messages: true
      stream: true
      tools: true
      images: false
      structuredOutput: true
      longContext: true
      embeddings: false
    modelAliases:
      claude-sonnet-4-6: LongCat-Flash-Chat
```

重要字段：

| 字段 | 说明 |
| --- | --- |
| `type` | provider adapter 类型，决定协议转换方式 |
| `vendor` | 供应商标签，用于默认配置、前端 catalog 和观测归类 |
| `endpoint` | OpenAI provider 使用 `chat` 或 `responses` 决定上游 API 路径；embedding provider 可用 `embeddings` |
| `baseURL` | 上游 API base URL |
| `model` | provider 实际请求的模型名 |
| `modelAliases` | 客户端模型名到上游真实模型名的映射 |
| `weight` | 路由权重 |
| `priceInput` / `priceOutput` | 成本计算单价 |
| `headers` / `extraBody` | 上游私有请求头和额外 body 字段 |
| `metricsURL` | vLLM 等推理服务的 Prometheus `/metrics` 地址，用于 prefix/KV cache 观测和 inference-aware routing |
| `capabilities` | chat/responses/messages/stream/tools/images/structured output/long context/embeddings 能力开关 |

敏感字段可以放在 `.env` 或 provider 专属 `envFile` 中，配置 loader 会在启动时注入。

---

## 2. 本地 GPU 模型

### vLLM

vLLM 默认暴露 OpenAI-compatible HTTP API。

```yaml
providers:
  - name: vllm-llama3
    type: openai
    vendor: vllm
    endpoint: chat
    baseURL: http://vllm-router.llm-ns.svc.cluster.local/v1
    model: meta-llama/Llama-3.1-8B-Instruct
    weight: 8
    timeout: 120
    maxTokens: 8192
    enabled: true
    metricsURL: http://vllm-router.llm-ns.svc.cluster.local/metrics
    capabilities:
      messages: false
      images: false
      embeddings: false
```

部署入口：

- 裸机 / Docker：`vllm serve meta-llama/Llama-3.1-8B-Instruct`
- Kubernetes：vLLM Router + Worker Service
- KServe：`LLMInferenceService` 暴露的 OpenAI-compatible Gateway

### SGLang

```yaml
providers:
  - name: sglang-qwen
    type: openai
    vendor: sglang
    endpoint: chat
    baseURL: http://sglang-gateway.llm-ns.svc.cluster.local/v1
    model: Qwen/Qwen2.5-72B-Instruct
    weight: 6
    timeout: 120
    maxTokens: 32768
    enabled: true
    capabilities:
      messages: false
      images: false
      embeddings: false
```

---

## 3. 云上 LLM API

### OpenAI Chat

```yaml
providers:
  - name: openai-chat
    type: openai
    vendor: openai
    endpoint: chat
    baseURL: https://api.openai.com/v1
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4.1-mini
    timeout: 60
    maxTokens: 128000
    enabled: true
```

### OpenAI Responses

```yaml
providers:
  - name: openai-responses
    type: openai
    vendor: openai
    endpoint: responses
    baseURL: https://api.openai.com/v1
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4.1
    timeout: 60
    enabled: true
```

### Anthropic

```yaml
providers:
  - name: anthropic-claude
    type: anthropic
    vendor: anthropic
    baseURL: https://api.anthropic.com/v1
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-sonnet-4-20250514
    timeout: 60
    maxTokens: 200000
    enabled: true
    capabilities:
      structuredOutput: false
      embeddings: false
```

---

## 4. Provider Registry 与热更新

启动时，配置文件中的 provider 会写入数据库注册表。运行时可以通过 Admin API 或前端 Providers 页面修改。

注册表字段：

| 字段 | 说明 | 可热更新 |
| --- | --- | --- |
| `enabled` | 是否启用 | 是 |
| `drain` | 是否排空，不再接收新请求 | 是 |
| `health_status` | `healthy` / `degraded` / `unhealthy` | 自动或手动 |
| `routing_weight` | 路由权重 | 是 |
| `type` / `vendor` / `endpoint` / `base_url` / `model` | 协议和模型信息 | 是 |
| `price_input` / `price_output` | 计费单价 | 是 |
| `timeout` / `max_tokens` | 请求约束 | 是 |
| `headers` / `extra_body` | 上游私有扩展 | 是 |
| `supports_*` | 能力 catalog | 是 |

示例：

```bash
# 设置 provider 为排空状态
curl -X PUT http://127.0.0.1:8028/admin/v1/providers/vllm-llama3 \
  -H "Authorization: Bearer <admin_key>:<admin_secret>" \
  -H "Content-Type: application/json" \
  -d '{"drain":true}'

# 调整路由权重
curl -X PUT http://127.0.0.1:8028/admin/v1/providers/vllm-llama3 \
  -H "Authorization: Bearer <admin_key>:<admin_secret>" \
  -H "Content-Type: application/json" \
  -d '{"routing_weight":10}'
```

前端 Providers 页面内置 provider catalog preset，可一键填充 OpenAI Chat、OpenAI Responses、Anthropic、DeepSeek、vLLM / Local 的默认 `type/vendor/base_url/endpoint/model/capabilities`。

---

## 5. 协议边界

外部 API surface 会先转成内部 `ResponseRequest`，再由 provider adapter 转成对应上游协议。

```text
Client
  |
  +-- /v1/responses          -> ResponseRequest
  +-- /v1/chat/completions   -> ResponseRequest
  +-- /v1/messages           -> ResponseRequest
  +-- /service/:prefix/*     -> ResponseRequest
                                  |
                                  v
                            responses.Service
                                  |
                                  v
                            Provider Adapter
                            +-- type=openai     -> OpenAI-compatible HTTP API
                            +-- type=anthropic  -> Anthropic Messages API
                            +-- type=azure      -> Azure OpenAI-compatible API
```

当前 `Provider` 接口：

```go
type Provider interface {
    Name() string
    Type() string
    BaseURL() string
    Model() string
    Weight() int
    UnitCost() float64
    Cost(promptTokens, completionTokens int) float64
    CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error)
    StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error)
    CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
    CreateImageGeneration(ctx context.Context, req *ImageGenerationRequest) (*ImageGenerationResponse, error)
}
```

关键代码：

- `internal/service/provider/types.go`
- `internal/service/provider/registry.go`
- `internal/service/provider/openai_client.go`
- `internal/service/provider/anthropic_client.go`

---

## 6. 健康检查与能力过滤

健康状态：

| 状态 | 路由行为 |
| --- | --- |
| `healthy` | 正常参与路由 |
| `degraded` | 仍可参与路由，但可被排序策略降权 |
| `unhealthy` | 从候选 provider 中排除 |

能力过滤会在路由前执行：

- 请求 `stream=true` 时要求 `supports_stream=true`
- 请求 tools 时要求 `supports_tools=true`
- 请求图片输入时要求 `supports_images=true`
- 请求 structured output 时要求 `supports_structured_output=true`
- embeddings / images API 会选择对应能力 provider

健康检查配置：

```yaml
healthCheck:
  enabled: true
  intervalSeconds: 30
  timeoutSeconds: 5
  failureThreshold: 2
```

---

## 7. 缓存相关建议

Provider 配置会影响缓存收益：

1. 对 vLLM 等本地推理服务设置 `metricsURL`，便于观察 `provider_prefix_cache_hit_rate_ratio`。
2. 对 OpenAI-compatible provider 设置正确 `endpoint`，`responses` 路径会透传或注入 `prompt_cache_key`。
3. 需要放大上游 prefix/KV cache 时，优先使用 `prefix_affinity` 或 sticky 路由，避免相同 prompt 前缀被分散到不同实例。
4. 不要把 provider prefix/KV cache 和 Gateyes L1 response cache 混为一谈：前者减少推理侧预填成本，后者直接跳过上游调用。
