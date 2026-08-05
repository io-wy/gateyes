# Gateyes 测试指南

## 默认回归测试

运行完整本地套件：

```bash
go test ./...
```

覆盖范围包括：配置加载、数据库适配器、仓库层、provider 适配器、路由、响应编排、HTTP handler、中间件、插件、缓存、限流、指标与链路追踪。

## 定向测试套件

Gateway / provider 兼容性：

```bash
go test ./internal/service/provider ./internal/service/responses ./internal/transport/http/handler
```

配置加载与 `.env` 行为：

```bash
go test ./internal/app/config
```

架构依赖检查：

```bash
make lint-arch
```

Plugin protobuf 生成检查：

```bash
make proto
go test ./pkg/plugin/v1
```

## 真实 Provider 测试

真实环境测试为 opt-in。需要可访问的 provider 以及通过 `.env` 或进程环境变量注入的密钥。

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config.yaml \
go test ./internal/transport/http/handler -run TestLiveProviderCompatibility -count=1 -v
```

限定到指定 provider：

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config.yaml \
GATEYES_LIVE_PROVIDERS=codexapis \
go test ./internal/transport/http/handler -run TestLiveProviderCompatibility -count=1 -v
```

真实矩阵会检查模型列表、Responses API 的文本与流式流程、长历史处理，以及 provider 支持的 chat/messages tool-call 行为。

## 直接 gRPC vLLM 探测

针对真实 vLLM gRPC provider：

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config_grpc.yaml \
go test ./internal/service/provider -run TestLiveGRPCVLLMProvider -count=1 -v
```

该配置期望以下环境变量：

- `VLLM_GRPC_TARGET`
- `VLLM_GRPC_API_KEY`
- `VLLM_GRPC_MODEL`

## 手动冒烟检查

启动网关：

```bash
go run ./cmd/gateway -config configs/config.yaml
```

列出模型：

```bash
curl -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  http://127.0.0.1:8083/v1/models
```

调用 Responses API：

```bash
curl -X POST http://127.0.0.1:8083/v1/responses \
  -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"model":"mock-model","input":"hello"}'
```

## CI 预期

在提交结构性或运行时变更前，运行：

```bash
make lint-arch
go test ./...
go vet ./...
```

若涉及协议变更，额外运行：

```bash
make proto
git diff -- pkg/plugin/v1 proto/plugin/v1
```

## 监控资产

Prometheus 与 Grafana 资产位于 `docs/docs-project/assets/`：

- `prometheus-alerts.yml`
- `prometheus-alerts.example.yml`
- `grafana-dashboard.json`
- `grafana-dashboard.example.json`
