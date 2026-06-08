# Gateyes

Gateyes is a production-oriented LLM API gateway. It exposes OpenAI-compatible and Anthropic-compatible endpoints, routes requests across providers, and centralizes auth, quota, budgets, caching, tracing, metrics, and plugin execution.

## What It Provides

- Unified API surfaces: `/v1/responses`, `/v1/chat/completions`, `/v1/messages`, `/v1/embeddings`
- Multi-tenant auth and RBAC with API-key based identities
- Provider routing with fallback, retry, circuit breaker, and health checks
- Redis-backed distributed rate limiting with in-memory fallback
- Project, tenant, API-key, and virtual-key budget controls
- L1 response cache with Redis + memory fallback
- Prometheus metrics, OTLP tracing, audit logs, and provider runtime stats
- WASM and gRPC plugin support across gateway lifecycle phases

## Repository Layout

```text
cmd/                         binaries: gateway and migration runner
configs/                     runtime config examples and local config entrypoints
deploy/                      Docker and Helm deployment assets
docs/docs-project/           project documentation and operations guides
docs/docs-ref/               reusable reference material and archived templates
docs/docs-tmp/               temporary or design-stage material
internal/app/config/         config loading, validation, and reload orchestration
internal/platform/           infrastructure adapters: db, redis, sqlstore, trace, eventbus
internal/repository/         repository contracts and domain persistence interfaces
internal/plugin/             internal plugin contracts used by services
internal/service/            business services: routing, provider, limiter, responses, etc.
internal/extension/          extension runtimes: WASM filter and gRPC/WASM plugin adapters
internal/transport/http/     HTTP handlers and middleware
pkg/plugin/v1/               generated public gRPC plugin contracts
proto/plugin/v1/             plugin protocol source files
plugins/                     plugin SDK and examples
```

## Configuration

Gateyes loads YAML config through Viper. Local `.env` values are loaded before parsing `${VAR}` placeholders; real process environment variables take precedence over `.env`.

```bash
cp .env.example .env
cp configs/config.example.yaml configs/config.yaml
# edit .env: database DSN, provider keys, bootstrap secrets
```

Run locally:

```bash
go run ./cmd/gateway -config configs/config.yaml
```

For the demo config:

```bash
go run ./cmd/gateway -config configs/demo-mock.yaml
```

## API Surfaces

| Endpoint | Compatibility | Purpose |
| --- | --- | --- |
| `/v1/responses` | OpenAI Responses | Primary internal request path |
| `/v1/chat/completions` | OpenAI Chat Completions | Existing OpenAI SDK clients |
| `/v1/messages` | Anthropic Messages | Anthropic SDK clients |
| `/v1/embeddings` | OpenAI Embeddings | Text embeddings |

All request surfaces share provider selection, retry/fallback, usage persistence, budgets, rate limits, tracing, and metrics.

## Plugins

Plugin protocol sources live in `proto/plugin/v1/`; generated Go contracts live in `pkg/plugin/v1/`.

```bash
make proto
```

- WASM plugins use the local SDK in `plugins/sdk/gateyes`.
- gRPC plugins import `github.com/gateyes/gateway/pkg/plugin/v1`.
- Runtime adapters live under `internal/extension/plugin`.

Full guide: [docs/docs-project/plugin-development.md](./docs/docs-project/plugin-development.md)

## Development Commands

```bash
make fmt          # gofmt all packages
make test         # go test ./...
make vet          # go vet ./...
make lint-arch    # harness layer dependency check
make proto        # regenerate plugin protobuf code
make run          # run gateway with configs/config.example.yaml
```

## Testing

Default regression:

```bash
go test ./...
```

Focused gateway compatibility:

```bash
go test ./internal/service/provider ./internal/service/responses ./internal/transport/http/handler
```

More details: [TESTING.md](./TESTING.md)

## Documentation

| Document | Purpose |
| --- | --- |
| [architecture.md](./docs/docs-project/architecture.md) | Architecture and responsibility boundaries |
| [runtime-mechanisms.md](./docs/docs-project/runtime-mechanisms.md) | Auth, routing, quota, cache, budget mechanics |
| [provider-configuration.md](./docs/docs-project/provider-configuration.md) | Provider setup |
| [plugin-development.md](./docs/docs-project/plugin-development.md) | WASM and gRPC plugin guide |
| [deployment.md](./docs/docs-project/deployment.md) | Docker Compose, Helm, production notes |
| [monitoring.md](./docs/docs-project/monitoring.md) | Metrics and alerts |
| [operations/runbook.md](./docs/docs-project/operations/runbook.md) | Operational runbook |

## Deployment

Docker Compose:

```bash
docker compose up --build -d
```

Helm:

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-prod.yaml
```

Use `.env` for local development secrets. In production, use Kubernetes Secrets or an external secret manager; do not commit provider keys or bootstrap secrets.
