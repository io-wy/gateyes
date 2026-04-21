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
go test ./internal/service/provider ./internal/service/responses ./internal/handler
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

### 3.5 grpc-vllm 专用配置示例

仓库内提供了一个最小 gRPC 配置样例：

```bash
configs/config_grpc.yaml
```

它默认从环境变量读取：

- `VLLM_GRPC_TARGET`
- `VLLM_GRPC_API_KEY`
- `VLLM_GRPC_MODEL`

如果 vLLM gRPC server 不要求鉴权，可以把 `apiKey` 留空或不给环境变量。

### 3.6 启动真实 vLLM gRPC server

官方入口示例：

```bash
python -m vllm.entrypoints.grpc_server --model $env:VLLM_GRPC_MODEL --host 0.0.0.0 --port 50051
```

然后把：

```bash
$env:VLLM_GRPC_TARGET="127.0.0.1:50051"
```

指向真实服务地址。

### 3.7 当前 gateway live 覆盖矩阵

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
- gRPC provider
  - 只走 `/v1/responses` 主链路
  - 不强行跑 `chat tool call` / `messages tool call`
  - 因为当前 `grpc-vllm` adapter 只收口了 text + stream + structured output，请求级 tools/images 明确不支持
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

## 4. Direct grpc-vllm live probe

这个入口是 provider 级真 gRPC 探针，不经过 mock，也不依赖 HTTP upstream。

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_CONFIG="configs/config_grpc.yaml"
go test ./internal/service/provider -run TestLiveGRPCVLLMProvider -v
```

它会对每个选中的 `type=grpc` + `vendor=vllm` provider 执行：

- `GetTokenizer`
  - 拉取真实 tokenizer archive
  - 校验 archive 非空，且如果服务端返回 sha256 就校验 hash
- `CreateResponse`
  - 真实调用 `Generate`
  - 校验 token ids 能被本地 tokenizer decode 成非空文本
- `StreamResponse`
  - 真实调用流式 `Generate`
  - 校验至少收到一个非空 delta
  - 校验最终 completed response 非空

## 5. Cherry Studio / SDK smoke check

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

## 6. Monitoring assets

完整监控基线文件：

- `docs/prometheus-alerts.yml`
- `docs/grafana-dashboard.json`

最小样例文件仍保留：

- `docs/prometheus-alerts.example.yml`
- `docs/grafana-dashboard.example.json`
