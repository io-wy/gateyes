# Gateyes

Gateyes is a production-oriented LLM API gateway. It exposes OpenAI-compatible and Anthropic-compatible endpoints, routes requests across providers, and centralizes auth, quota, budgets, caching, tracing, metrics, and plugin execution.

## What It Provides

- Unified API surfaces: `/v1/responses`, `/v1/chat/completions`, `/v1/messages`, `/v1/embeddings`, `/v1/images/generations`
- Multi-tenant auth and RBAC with API-key based identities
- Provider routing with fallback, retry, circuit breaker, and health checks
- Redis-backed distributed rate limiting with in-memory fallback
- Project, tenant, API-key, and virtual-key budget controls
- L1 response cache with Redis + memory fallback
- Prometheus metrics, OTLP tracing, audit logs, and provider runtime stats
- WASM and gRPC plugin support across gateway lifecycle phases

## Repository Layout

```text
cmd/                         binaries: gateway, migration runner, and vllm helper
configs/                     runtime config examples and local config entrypoints
deploy/                      Docker and Helm deployment assets, Grafana/Prometheus
docs/docs-project/           project documentation, runbooks, and operations guides
docs/docs-ref/               reusable reference material and archived templates
docs/docs-tmp/               temporary or design-stage material
internal/app/config/         config loading, validation, and reload orchestration
internal/domain/plugin/      internal plugin domain types and contracts
internal/handler/            HTTP handlers, middleware, metrics, and server wiring
internal/handler/middleware/ auth, guard, rate-limit filter pipeline, and tracing
internal/pkg/                internal shared libraries: db/redis wrappers, trace, eventbus, logging
internal/repository/         repository contracts and domain persistence interfaces
internal/repository/db/      database driver, connection pool, and migrations
internal/repository/sqlstore/ SQL implementations of repository.Store
internal/service/            business services: routing, provider, limiter, responses, auth, etc.
internal/service/extension/  extension runtimes: WASM filter and gRPC/WASM plugin adapters
internal/testutil/           shared test helpers and fixtures
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


## Cold Start (Shared PostgreSQL)

For local development with a shared PostgreSQL instance (container name `postgres` on port `5432`):

```bash
# 1. Configure secrets (if you haven't already)
cp .env.example .env
# edit .env: GATEYES_ADMIN_BOOTSTRAP_SECRET, GATEYES_DEMO_SECRET, provider keys

# 2. One-shot: infra + DB + gateway + verified admin
make give-me-an-admin
```

The command will:
1. Start `postgres:16-alpine` and `redis:7-alpine` containers if absent.
2. Create the `gateyes` role and database, and fix PostgreSQL 15+ `public` schema ownership so migrations can run.
3. Start the gateway and wait for `/ready` to return 200.
4. Verify that the bootstrap admin (`admin-key-001`) can authenticate.

After it finishes, you can use:

- **Admin**: `Authorization: Bearer admin-key-001:$GATEYES_ADMIN_BOOTSTRAP_SECRET`
- **Demo user**: `Authorization: Bearer demo-key-001:$GATEYES_DEMO_SECRET`

Manual equivalent:

```bash
make provision-db   # only DB provisioning
make run            # go run ./cmd/gateway -config configs/config.yaml
```

### Why the `public` schema fix matters

PostgreSQL 15+ no longer grants `CREATE` on the `public` schema to non-superusers.
If you create a dedicated `gateyes` role/database in a shared Postgres instance, the gateway's migrations will fail with `permission denied for schema public`.
`make give-me-an-admin` handles this automatically by running `ALTER SCHEMA public OWNER TO gateyes`.

## Alerts

Gateyes can send alerts when budgets are exhausted, providers change state, or quotas cross thresholds.

### Webhook (default)

Set `alert.webhookURL` / `webhookSecret` in `config.yaml`, or use `alert.channels` for multiple webhooks with label-based routing.

### Feishu (Lark)

Use the official Feishu SDK channel to send text or interactive card messages to a user or group chat.

1. Create a Feishu app and grant it the `im:chat:send` / `im:message:send` permissions.
2. Add to `configs/config.yaml`:

```yaml
alert:
  enabled: true
  channels:
    - name: feishu-ops
      type: feishu
      feishuAppId: ${FEISHU_APP_ID}
      feishuAppSecret: ${FEISHU_APP_SECRET}
      feishuReceiveType: chat_id   # or open_id / user_id / email
      feishuReceiveId: ${FEISHU_CHAT_ID}
      feishuMsgType: interactive   # or text
      labels:
        severity: critical
```

3. Set the secrets in `.env`:

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_CHAT_ID=oc_xxx
```

## API Surfaces

| Endpoint | Compatibility | Purpose |
| --- | --- | --- |
| `/v1/responses` | OpenAI Responses | Primary internal request path |
| `/v1/chat/completions` | OpenAI Chat Completions | Existing OpenAI SDK clients |
| `/v1/messages` | Anthropic Messages | Anthropic SDK clients |
| `/v1/embeddings` | OpenAI Embeddings | Text embeddings |
| `/v1/images/generations` | OpenAI Images | Image generation |

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
