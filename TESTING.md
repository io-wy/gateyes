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
- provider 的 `apiKey` / `baseURL` 可真实访问
- 允许测试机访问外网

### 3.2 全量启用

```bash
$env:GATEYES_LIVE="1"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.3 只测指定 provider

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_PROVIDERS="minimax,codex for me"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.4 使用自定义配置文件

```bash
$env:GATEYES_LIVE="1"
$env:GATEYES_LIVE_CONFIG="configs/config.yaml"
go test ./internal/handler -run TestLiveProviderCompatibility -v
```

### 3.5 覆盖的 live 场景

- `/v1/responses` 非流式文本
- `/v1/responses` 流式 SSE
- 长 history 回归
- OpenAI-compatible provider:
  - `/v1/chat/completions` tool call
  - `/v1/chat/completions` stream
- Anthropic-compatible provider:
  - `/v1/messages` tool call
  - `/v1/messages` stream

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
