# Testing

## 1. Default regression

本地默认回归：

```bash
go test ./...
```

这会覆盖：

- 协议兼容回归：`/v1/responses`、`/v1/chat/completions`、`/v1/messages`
- SSE 编码与工具调用
- provider request normalization
- handler E2E（基于 mock upstream）
- 生命周期、连接池、middleware、metrics

## 2. Focused compatibility suites

只跑协议/网关兼容相关：

```bash
go test ./internal/protocol/apicompat ./internal/service/provider ./internal/service/responses ./internal/handler
```

## 3. Live provider compatibility

真实上游兼容性回归默认关闭，只有显式设置环境变量才会执行。

### 3.1 运行条件

- `configs/config.yaml` 中存在可用且 `enabled: true` 的 provider
- provider 的 `apiKey` / `baseURL` 可以真实访问
- 测试机允许访问外网

### 3.2 全量启用

```bash
$env:GATEYES_LIVE="1"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.3 只测指定 provider

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_PROVIDERS="minimax,longcat-primary"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.4 使用自定义配置文件

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_CONFIG="configs/config.yaml"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.5 当前 live 覆盖矩阵

每个选中的 provider 都会先把 tenant 可见 provider 收敛到单个目标 provider，然后执行：

- `models`
  - `GET /v1/models`
  - 断言只暴露当前 tenant 可见模型
  - 断言 `id/provider/owned_by` 与目标 provider 一致
- `responses_text`
  - `POST /v1/responses`
  - 断言返回 `completed`、非空文本、非空 `response_id`
  - 随后 `GET /v1/responses/:id`
  - 断言持久化后的 response 可回读，且模型、状态、文本有效
- `responses_stream`
  - `POST /v1/responses` with `stream=true`
  - 断言 SSE 以 `[DONE]` 结束
  - 断言没有 error event
  - 断言至少出现 `response.completed`
  - 断言至少有可见输出事件：`response.output_text.delta` 或 `response.output_item.done`
- `long_history`
  - 上百轮上下文的长 history 请求
  - 断言仍能返回非空输出
- OpenAI-compatible provider
  - `chat_tool_call`
    - `POST /v1/chat/completions`
    - 断言 `finish_reason=tool_calls`
    - 断言返回 `get_probe_status` tool call
  - `chat_stream`
    - `POST /v1/chat/completions` with `stream=true`
    - 断言 SSE 以 `[DONE]` 结束
    - 断言没有 error event
    - 断言流中出现 `assistant role`、`tool_calls`、`finish_reason=tool_calls`
- Anthropic-compatible provider
  - `anthropic_tool_call`
    - `POST /v1/messages`
    - 断言 `stop_reason=tool_use`
    - 断言返回 `get_probe_status` tool_use block
  - `anthropic_stream`
    - `POST /v1/messages` with `stream=true`
    - 断言 SSE 以 `[DONE]` 结束
    - 断言没有 error event
    - 断言流中出现 `message_start`、`message_stop`、`tool_use`、`stop_reason=tool_use`

## 4. Cherry Studio / SDK smoke check

建议先用最稳的 provider 单独验证一遍：

1. 在 `configs/config.yaml` 里确认目标 provider 的 `enabled`、`type`、`vendor`、`baseURL`、`apiKey` 正确。
2. 启动网关：

```bash
go run ./cmd/gateway -config configs/config.yaml
```

3. 先执行 live harness：

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_PROVIDERS="minimax"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

4. 再到 Cherry Studio 填：
   - Base URL: `http://127.0.0.1:8083/v1`
   - API Key: `test-key-001:test-secret`
   - OpenAI 协议模型：用 `/v1/models` 返回里的 `id`

## 5. Monitoring assets

完整监控基线文件：

- `docs/prometheus-alerts.yml`
- `docs/grafana-dashboard.json`

最小样例文件仍保留：

- `docs/prometheus-alerts.example.yml`
- `docs/grafana-dashboard.example.json`
