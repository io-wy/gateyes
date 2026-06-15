# Gateyes Load Testing

This directory contains a mock upstream LLM server and [k6](https://k6.io) load-test scripts for validating Gateyes performance, capacity, and reliability before production rollouts.

## Layout

```
tests/load/
├── mock_upstream/main.go          # OpenAI-compatible mock LLM server
├── k6/
│   ├── constants.js               # Shared env-var defaults
│   ├── chat-completions.js        # Non-streaming load test
│   └── chat-completions-stream.js # SSE streaming load test
└── README.md                      # This file
```

## Quick start

### 1. Start dependencies

Use the local docker-compose stack:

```bash
docker compose up -d postgres redis
```

### 2. Start the mock upstream

In one terminal:

```bash
make load-mock-upstream
```

This listens on `:18080` and returns OpenAI-compatible `/v1/chat/completions` responses. Default behavior:

- Fixed latency before first token: `50ms`
- Completion tokens per response: `64`
- Streaming token rate: `20 tokens/s`
- Failure rate: `0`

Override via flags:

```bash
go run ./tests/load/mock_upstream/main.go \
  -addr :18080 \
  -delay 100ms \
  -output-tokens 256 \
  -tokens-per-sec 50 \
  -fail-rate 0.05 \
  -stream-jitter 10ms
```

### 3. Configure Gateyes to use the mock upstream

Create a dedicated config for load testing, e.g. `configs/loadtest.yaml`:

```yaml
server:
  listenAddr: ":8028"

database:
  driver: postgres
  dsn: "postgres://gateyes:gateyes_pw@localhost:5432/gateyes?sslmode=disable"
  autoMigrate: true

metrics:
  namespace: gateway
  enabled: true

router:
  strategy: least_load

providers:
  - name: mock-openai
    type: openai
    baseURL: http://localhost:18080
    endpoint: chat
    apiKey: mock-key
    model: mock-model
    timeout: 60
    enabled: true

apiKeys:
  - key: demo-key-001
    secret: demo-secret
    quota: 100000000
    qps: 100000

admin:
  defaultTenant: default
```

Start Gateyes:

```bash
go run ./cmd/gateway -config configs/loadtest.yaml
```

### 4. Run a load test

Non-streaming:

```bash
make load-chat
```

Streaming:

```bash
make load-chat-stream
```

Override defaults via environment variables:

```bash
GATEYES_URL=http://localhost:8028 \
GATEYES_API_KEY=demo-key-001:demo-secret \
GATEYES_MODEL=mock-model \
GATEYES_MAX_TOKENS=256 \
GATEYES_TARGET_CONCURRENCY=200 \
GATEYES_STRESS_CONCURRENCY=500 \
GATEYES_DURATION_STEADY=5m \
make load-chat
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `GATEYES_URL` | `http://localhost:8028` | Gateyes base URL |
| `GATEYES_API_KEY` | `demo-key-001:demo-secret` | API key and secret (`key:secret`) |
| `GATEYES_MODEL` | `mock-model` | Model name to request |
| `GATEYES_MAX_TOKENS` | `128` | `max_tokens` in request |
| `GATEYES_TARGET_CONCURRENCY` | `100` | Steady-state VUs |
| `GATEYES_STRESS_CONCURRENCY` | `300` | Peak stress VUs |
| `GATEYES_DURATION_RAMP` | `1m` | Ramp-up duration |
| `GATEYES_DURATION_STEADY` | `3m` | Steady-state duration |
| `GATEYES_DURATION_STRESS` | `2m` | Stress duration |
| `GATEYES_DURATION_RAMP_DOWN` | `1m` | Ramp-down duration |

## Profiling the gateway during a load test

Gateyes exposes Go pprof on `:6060`. Capture profiles while k6 is running:

```bash
# Terminal 1: run load test
make load-chat

# Terminal 2: capture CPU profile
make pprof-cpu

# Or heap profile
make pprof-heap
```

Useful pprof views:

```bash
go tool pprof -http=:8080 /tmp/gateyes-cpu.pb.gz
go tool pprof -http=:8080 /tmp/gateyes-heap.pb.gz
```

## What to watch

During a test, open Grafana (`http://localhost:3000`) and monitor:

- `gateway_llm_requests_total` — RPS by surface/provider/result
- `gateway_llm_request_duration_seconds` — end-to-end P50/P95/P99
- `gateway_llm_upstream_duration_seconds` — upstream-only latency
- `gateway_llm_time_to_first_token_seconds` — TTFT for streaming
- `gateway_llm_active_streams` — concurrent streaming sessions
- `gateway_llm_inflight_requests` — in-flight request pressure
- `gateway_llm_errors_total` — error breakdown
- `gateway_cache_lookups_total` — cache hit/miss behavior
- Go runtime metrics: `go_goroutines`, `go_memstats_heap_alloc_bytes`

## Suggested test scenarios

1. **Baseline** — `TARGET=100`, `STRESS=300`, verify P95 < 5s and error rate < 1%.
2. **Sustained soak** — run `load-chat` for 30m+ to catch goroutine or connection leaks.
3. **Streaming saturation** — `load-chat-stream` with high concurrency to find active-stream limits.
4. **Failure injection** — start mock upstream with `-fail-rate 0.1` to exercise retries/circuit breaker.
5. **Cache warmup** — send identical prompts repeatedly and watch `gateway_cache_lookups_total` hit rate.

## Adding new k6 scripts

1. Create `tests/load/k6/<name>.js`.
2. Import shared constants from `./constants.js`.
3. Add a corresponding `make load-<name>` target.
4. Document the scenario and key metrics in this README.
