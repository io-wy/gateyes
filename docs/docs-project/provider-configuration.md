# Provider 配置指南

本文档说明如何在 Gateyes 中配置两类上游：**本地 GPU 推理引擎** 与 **云上 LLM API**。

---

## 统一接入方式

Gateyes 对两类上游使用**同一种接入模式**：

```yaml
providers:
  - name: <唯一标识>
    type: openai        # 或 anthropic
    baseURL: <endpoint>
    apiKey: <密钥>
    model: <模型名>
```

本地推理引擎（vLLM、SGLang）默认暴露 OpenAI-compatible HTTP API，无需特殊适配。

---

## 本地 GPU 模型接入

### vLLM

vLLM 默认通过 FastAPI 暴露 OpenAI-compatible HTTP 服务，端口 `8000`。

```yaml
providers:
  - name: vllm-llama3
    type: openai
    baseURL: http://vllm-router.llm-ns.svc.cluster.local/v1
    model: meta-llama/Llama-3.1-8B-Instruct
    timeout: 60
    maxTokens: 8192
    enabled: true
```

**部署方式**：
- 裸机 / Docker：`vllm serve meta-llama/Llama-3.1-8B-Instruct`
- K8s（Production Stack）：Helm chart 部署 Router + Worker
- KServe：`LLMInferenceService` CRD

Gateyes 只需指向 vLLM Router 或 KServe Gateway 暴露的 HTTP Service 即可。

### SGLang

SGLang 同样默认暴露 OpenAI-compatible HTTP 服务。

```yaml
providers:
  - name: sglang-qwen
    type: openai
    baseURL: http://sglang-gateway.llm-ns.svc.cluster.local/v1
    model: Qwen/Qwen2.5-72B-Instruct
    timeout: 60
    enabled: true
```

---

## 云上 LLM API 接入

### OpenAI

```yaml
providers:
  - name: openai-gpt4o
    type: openai
    baseURL: https://api.openai.com/v1
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4o
    timeout: 30
    enabled: true
```

### Anthropic

```yaml
providers:
  - name: anthropic-claude
    type: anthropic
    baseURL: https://api.anthropic.com
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-3-5-sonnet-20241022
    timeout: 30
    enabled: true
```

---

## Provider 注册表（Runtime Config）

Provider 启动时从配置加载，同时写入数据库注册表，支持运行时动态调整：

| 字段 | 说明 | 可热更新 |
|------|------|----------|
| `enabled` | 是否启用 | ✅ |
| `drain` | 是否排空（不再接收新请求） | ✅ |
| `health_status` | healthy / degraded / unhealthy | 自动 |
| `routing_weight` | 路由权重（越大越优先） | ✅ |
| `supports_stream` | 是否支持流式 | ❌ |
| `supports_tools` | 是否支持工具调用 | ❌ |
| `supports_images` | 是否支持图片输入 | ❌ |
| `price_input` | 输入 token 单价 | ✅ |
| `price_output` | 输出 token 单价 | ✅ |

运行时更新接口：

```bash
# 设置 provider 为排空状态
curl -X PUT http://localhost:8083/admin/providers/vllm-llama3 \
  -H "Authorization: Bearer admin:secret" \
  -H "Content-Type: application/json" \
  -d '{"drain":true}'

# 调整路由权重
curl -X PUT http://localhost:8083/admin/providers/vllm-llama3 \
  -H "Authorization: Bearer admin:secret" \
  -H "Content-Type: application/json" \
  -d '{"routing_weight":10}'
```

---

## 协议边界

Gateyes 内部使用单一协议，外部通过兼容层适配多路 API：

```
Client
  │
  ├── /v1/responses      → 内部 ResponseRequest
  ├── /v1/chat/completions → 转换为 ResponseRequest
  └── /v1/messages       → 转换为 ResponseRequest
                              │
                              ▼
                    responses.Service
                              │
                              ▼
                    Provider Adapter
                    ├─ type: openai → OpenAI HTTP API
                    └─ type: anthropic → Anthropic HTTP API
```

新增 provider 只需实现 `Provider` 接口：

```go
type Provider interface {
    Name() string
    Type() string
    Create(ctx context.Context, req *ResponseRequest) (*Response, error)
    CreateStream(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, error)
    HealthCheck(ctx context.Context) error
}
```

---

## 健康检查

Provider 健康检查通过 HTTP 发送最小化请求验证可用性：

```yaml
healthCheck:
  enabled: true
  intervalSeconds: 60
  timeoutSeconds: 15
  failureThreshold: 3
```

- 探测请求：`max_output_tokens=8` 的 completions 请求
- 状态自动同步到注册表
- 状态变更触发告警通知
