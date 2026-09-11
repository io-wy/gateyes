# Gateyes gRPC Routing Plugins

This module provides three independent implementations of the Gateyes `RouterPlugin` gRPC API:

| Service | Default address | Behavior |
| --- | --- | --- |
| `vllm-prefix-cache-router` | `:50051` | vLLM Production Stack compatible prefix-aware HashTrie routing |
| `least-load-router` | `:50052` | Weighted routing using fresh vLLM queue/KV signals with in-flight fallback |
| `session-affinity-router` | `:50053` | Stateless weighted rendezvous hashing by model and session |

Gateyes currently activates the first healthy plugin with `type: router`. Configure only one of these services as a router in a Gateway instance.

## Build and test

```bash
cd plugins/routers
make test
make test-race
make build
```

The binaries are written to `plugins/routers/bin` by default.

## Run locally

```bash
./bin/vllm-prefix-cache-router --listen=:50051 --prefix-min-match-length=128
./bin/least-load-router --listen=:50052 --signal-max-age=15s
./bin/session-affinity-router --listen=:50053
```

Each process also registers the standard gRPC Health service. All services are fail-open from Gateyes's perspective: if a plugin is unavailable or times out, Gateyes keeps its built-in candidate order.

## Gateway configuration

Use one entry at a time:

```yaml
grpcPlugins:
  - name: vllm-prefix-cache-router
    type: router
    address: localhost:50051
    timeout: 100
```

For containers on the Compose network, replace `localhost` with the service name, such as `vllm-prefix-cache-router:50051`.

## vLLM prefix-cache router

This is a Go port of the vLLM Production Stack `prefixaware` algorithm and its `HashTrie`:

1. Concatenate textual chat message content exactly as vLLM does. Gateyes supplies this as the presence-aware `RouteContext.prefix_text`; older Gateyes versions fall back to `input_text`.
2. Split the prompt into chunks of 128 Unicode characters.
3. Hash every chunk independently with xxHash64.
4. Walk a trie of chunk hashes and intersect the stored providers with currently available candidates at every level.
5. Select uniformly from the provider set at the longest matching prefix.
6. Insert the complete prompt path for the selected provider.
7. If the match is shorter than `--prefix-min-match-length`, route to the provider with the lowest QPS observed by this plugin, then still insert the prompt so the trie is seeded.

The default `--prefix-min-match-length=0` matches vLLM. Because match lengths are quantized to the chunk size, `128` is a practical value when prefix locality should require at least one full matching chunk. Keep `--chunk-size=128` for strict vLLM compatibility.

Like vLLM's implementation, this HashTrie deliberately has no TTL or eviction and assumes the backend prefix cache is not evicted. Restart the router to clear learned placement after a broad vLLM cache reset.

This is not the LMCache-backed vLLM `kvaware` algorithm. `kvaware` tokenizes prompts and asks LMCache Controller for actual token-block placement. Gateyes's current router protobuf does not contain LMCache instance IDs, controller connectivity, token IDs, LoRA IDs, multimodal hashes, or cache salts, so claiming `kvaware` equivalence would be incorrect.

Flags:

```text
--listen=:50051
--prefix-min-match-length=0
--chunk-size=128
```

## Least-load router

When `signals_updated_at_unix_ms` is within `--signal-max-age`, candidates are ordered by:

```text
(queue_waiting * 4 + queue_running + gpu_kv_cache_usage * 2 + cpu_kv_cache_usage) / weight
```

When those vLLM signals are absent or stale, the score is `current_load / weight`. Healthy candidates precede unhealthy ones; TTFT, average latency, and provider name break ties deterministically.

Flags:

```text
--listen=:50052
--signal-max-age=15s
```

Gateyes must enable `router.inferenceMetrics` and configure a `metricsURL` for each vLLM provider to populate the inference fields.

## Session-affinity router

The session router computes weighted rendezvous scores using `model`, `session_id`, and provider name. It keeps no local mapping, so the result is stable across restarts and multiple replicas. Adding or removing a provider only remaps sessions whose winning provider changes. If `session_id` is empty, the incoming Gateyes order is preserved.

Flags:

```text
--listen=:50053
```

## Containers

Build a single service image from the repository root:

```bash
docker build -f plugins/routers/Dockerfile --target vllm-prefix-cache-router -t gateyes/vllm-prefix-cache-router .
docker build -f plugins/routers/Dockerfile --target least-load-router -t gateyes/least-load-router .
docker build -f plugins/routers/Dockerfile --target session-affinity-router -t gateyes/session-affinity-router .
```

Or start the examples together:

```bash
docker compose -f plugins/routers/docker-compose.yaml up --build
```

Running all three processes is useful for evaluation, but a Gateway should reference only one of them as its active router.

## Upstream parity reference

The prefix implementation was checked against vLLM Production Stack `main` on 2026-09-09:

- `src/vllm_router/routers/routing_logic.py`, `PrefixAwareRouter`
- `src/vllm_router/prefix/hashtrie.py`, `HashTrie`
- `src/tests/test_prefixaware_router.py`

The upstream source is Apache-2.0 licensed. This implementation preserves the observable algorithm while using Gateyes protobuf requests and Go concurrency primitives.
