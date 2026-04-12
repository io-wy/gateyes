# 监控与告警

本文档描述 Gateyes 当前的监控出口、Prometheus 指标口径，以及最小的告警规则和 Grafana dashboard 样例。

当前实现只使用仓库内已有技术：

- `Gin`
- `prometheus/client_golang`
- `promauto`
- `promhttp`
- `slog`

不包含 OpenTelemetry、trace backend 或外部 APM SDK。

## 1. 暴露方式

默认通过：

```text
GET /metrics
```

暴露 Prometheus 格式指标。

配置项：

```yaml
metrics:
  namespace: gateway
  enabled: true
```

说明：

- `namespace` 默认为 `gateway`
- `enabled=false` 时仍保留 `/metrics` 路由，但 handler 会返回 `404`

## 2. 当前指标口径

核心口径已经统一为：

- `surface`
- `provider`
- `result`

### 2.1 指标清单

| 指标 | labels | 说明 |
|---|---|---|
| `gateway_llm_requests_total` | `surface,result,provider` | LLM 请求总数，成功失败都记 |
| `gateway_llm_inflight_requests` | `surface` | 当前进行中的 LLM 请求 |
| `gateway_llm_request_duration_seconds` | `surface,provider,result` | 端到端请求时延 |
| `gateway_llm_upstream_duration_seconds` | `surface,provider,result` | 上游调用时延 |
| `gateway_llm_time_to_first_token_seconds` | `surface,provider` | **流式首 token 时延** |
| `gateway_llm_active_streams` | `surface` | 当前活跃流数 |
| `gateway_llm_stream_duration_seconds` | `surface,provider,result` | 流式总持续时间 |
| `gateway_llm_tokens_total` | `provider,token_type` | `prompt/completion/cached/total` token 计数 |
| `gateway_llm_errors_total` | `surface,provider,error_class` | 更细粒度错误分类 |
| `gateway_llm_retries_total` | `provider` | retry 总数 |
| `gateway_llm_fallbacks_total` | `provider` | fallback 总数 |
| `gateway_provider_requests_total` | `provider,result` | provider 维度请求数 |
| `gateway_provider_circuit_state` | `tenant_id,provider` | circuit breaker 状态 |

### 2.2 Label 语义

#### `surface`

固定值：

- `responses`
- `chat_completions`
- `messages`
- `models`
- `admin`

#### `provider`

最终命中的 provider 名。

如果错误发生在 middleware 或 handler 早期，还没有选到 provider，则为：

```text
provider="none"
```

#### `result`

固定枚举：

- `success`
- `client_error`
- `auth_error`
- `rate_limited`
- `timeout`
- `upstream_error`
- `internal_error`

#### `error_class`

常见值：

- `invalid_api_key`
- `inactive_api_key`
- `forbidden`
- `invalid_request`
- `model_not_allowed`
- `quota_exceeded`
- `rate_limited`
- `upstream_4xx`
- `upstream_5xx`
- `timeout`
- `no_provider`
- `internal_error`

## 3. 埋点边界

### Middleware

以下错误会直接进入 `gateway_llm_requests_total` 和 `gateway_llm_errors_total`：

- 鉴权失败
- inactive key
- role forbidden
- invalid JSON / invalid request
- 模型白名单拒绝
- quota exceeded
- limiter 拒绝

### Handler

成功路径会记录：

- 请求成功数
- request/upstream duration
- token 计数
- retry/fallback
- provider request

### Streaming

流式路径会额外记录：

- `gateway_llm_active_streams`
- `gateway_llm_time_to_first_token_seconds`
- `gateway_llm_stream_duration_seconds`

## 4. 推荐 Prometheus 查询

### 4.1 成功请求速率

```promql
sum by (surface, provider) (
  rate(gateway_llm_requests_total{result="success"}[5m])
)
```

### 4.2 错误率

```promql
sum by (surface, provider) (
  rate(gateway_llm_requests_total{result!="success"}[5m])
)
/
sum by (surface, provider) (
  rate(gateway_llm_requests_total[5m])
)
```

### 4.3 request duration p95

```promql
histogram_quantile(
  0.95,
  sum by (le, surface, provider) (
    rate(gateway_llm_request_duration_seconds_bucket[5m])
  )
)
```

### 4.4 upstream duration p95

```promql
histogram_quantile(
  0.95,
  sum by (le, surface, provider) (
    rate(gateway_llm_upstream_duration_seconds_bucket[5m])
  )
)
```

### 4.5 token 吞吐

```promql
sum by (provider, token_type) (
  rate(gateway_llm_tokens_total[5m])
)
```

## 5. 最小样例文件

仓库内已附带两个最小样例：

- [`docs/prometheus-alerts.example.yml`](./prometheus-alerts.example.yml)
- [`docs/grafana-dashboard.example.json`](./grafana-dashboard.example.json)

这两个文件按默认 namespace `gateway` 编写。

如果你修改了 `metrics.namespace`，需要同步替换查询里的前缀。

## 6. 当前边界

- 没有 tracing
- 没有现成的 request-id / log correlation
- `provider_circuit_state` 依赖显式同步，不是后台周期采集
- 样例 dashboard / alert 规则只覆盖最小监控面，不等于完整生产告警体系
