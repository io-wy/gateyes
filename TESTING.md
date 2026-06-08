# Testing Gateyes

## Default Regression

Run the full local suite:

```bash
go test ./...
```

This covers config loading, database adapters, repositories, provider adapters, routing, response orchestration, HTTP handlers, middleware, plugins, cache, limiter, metrics, and tracing.

## Focused Suites

Gateway/provider compatibility:

```bash
go test ./internal/service/provider ./internal/service/responses ./internal/transport/http/handler
```

Config loading and `.env` behavior:

```bash
go test ./internal/app/config
```

Architecture dependency check:

```bash
make lint-arch
```

Plugin protobuf generation check:

```bash
make proto
go test ./pkg/plugin/v1
```

## Live Provider Tests

Live tests are opt-in. They require reachable providers and secrets supplied through `.env` or process environment variables.

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config.yaml \
go test ./internal/transport/http/handler -run TestLiveProviderCompatibility -count=1 -v
```

Limit to selected providers:

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config.yaml \
GATEYES_LIVE_PROVIDERS=codexapis \
go test ./internal/transport/http/handler -run TestLiveProviderCompatibility -count=1 -v
```

The live matrix checks model listing, Responses API text and stream flows, long history handling, and provider-specific chat/messages tool-call behavior when supported.

## Direct gRPC vLLM Probe

For a real vLLM gRPC provider:

```bash
GATEYES_LIVE=1 \
GATEYES_LIVE_CONFIG=configs/config_grpc.yaml \
go test ./internal/service/provider -run TestLiveGRPCVLLMProvider -count=1 -v
```

Expected environment variables for that config are:

- `VLLM_GRPC_TARGET`
- `VLLM_GRPC_API_KEY`
- `VLLM_GRPC_MODEL`

## Manual Smoke Check

Start the gateway:

```bash
go run ./cmd/gateway -config configs/config.yaml
```

List models:

```bash
curl -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  http://127.0.0.1:8083/v1/models
```

Call Responses API:

```bash
curl -X POST http://127.0.0.1:8083/v1/responses \
  -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"model":"mock-model","input":"hello"}'
```

## CI Expectations

Before committing structural or runtime changes, run:

```bash
make lint-arch
go test ./...
go vet ./...
```

For protocol changes, also run:

```bash
make proto
git diff -- pkg/plugin/v1 proto/plugin/v1
```

## Monitoring Assets

Prometheus and Grafana assets live under `docs/docs-project/assets/`:

- `prometheus-alerts.yml`
- `prometheus-alerts.example.yml`
- `grafana-dashboard.json`
- `grafana-dashboard.example.json`
