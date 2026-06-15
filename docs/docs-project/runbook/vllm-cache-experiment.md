# vLLM Prefix Cache 实验手册

本手册描述如何在本地启动多个 vLLM 实例，通过 gateyes 路由，并同时观测：

1. **vLLM 侧 KV / Prefix Cache 命中**（来自 vLLM `/metrics`）
2. **gateyes L1 Response Cache 命中**（gateway 自身缓存层）
3. **上游 API 返回的 prompt cached 比例**（来自 OpenAI/vLLM 的 `usage.cached_tokens`）

## 前置条件

- Go 1.23+
- `vllm` CLI 已在 PATH 中（`vllm --help` 可验证）
- 至少一张可跑 `Qwen3-0.6B` 的 GPU（或 CPU 模式，实验时间会显著增加）
- PostgreSQL / SQLite 数据库可连接（`configs/config.yaml` 已配置）

## 1. 启动本地 vLLM 实例

使用仓库内建的 launcher 起 2 个 qwen3-0.6B 实例，端口从 8001 开始：

```bash
go run ./cmd/vllm \
  --model Qwen/Qwen3-0.6B \
  --instances 2 \
  --base-port 8001 \
  --enable-prefix-caching
```

看到 `all vllm instances are ready` 后，launcher 会打印可直接贴进 `configs/config.yaml` 的 provider 片段。保持这个终端运行，按 `Ctrl-C` 可同时停止所有实例。

## 2. 启用 gateway 中的 vLLM provider

编辑 `configs/config.yaml`：

1. 确保 `router.inferenceMetrics.enabled: true`（已默认开启）
2. 把 `providers` 中两个 `vllm-qwen3-0.6B-*` 的 `enabled: false` 改为 `enabled: true`
3. 如需只看本地模型，可把云端 provider 临时 `enabled: false`

启动 gateway：

```bash
go run ./cmd/gateway --config configs/config.yaml
```

## 3. 验证指标端点

gateway 的 `/metrics` 会暴露以下关键序列：

| 指标 | 含义 | 来源 |
|---|---|---|
| `gateway_llm_tokens_total{token_type="cached"}` | 上游返回的 cached prompt tokens 累计 | provider API response |
| `gateway_llm_prompt_cache_ratio_bucket` | 每次请求 `cached_tokens / prompt_tokens` 的分布 | provider API response |
| `gateway_cache_lookups_total{result="hit"}` | gateway L1 cache 命中次数 | gateway cache layer |
| `gateway_cache_lookups_total{result="miss"}` | gateway L1 cache 未命中次数 | gateway cache layer |
| `gateway_provider_gpu_cache_usage_ratio{provider="vllm-qwen3-0.6B-8001"}` | vLLM GPU KV cache 占用率 | vLLM `/metrics` |
| `gateway_provider_cpu_cache_usage_ratio{provider="..."}` | vLLM CPU KV cache 占用率 | vLLM `/metrics` |
| `gateway_provider_prefix_cache_hit_rate_ratio{provider="..."}` | vLLM prefix cache 命中率 | vLLM `/metrics` |
| `gateway_provider_prefix_cache_queries{provider="..."}` | vLLM prefix cache 查询累计 | vLLM `/metrics` |
| `gateway_provider_prefix_cache_hits{provider="..."}` | vLLM prefix cache 命中累计 | vLLM `/metrics` |

测试：

```bash
curl -s http://127.0.0.1:8028/metrics | grep -E 'llm_tokens_total|cache_lookups|prefix_cache|gpu_cache_usage'
```

## 4. 用 curl 模拟多会话负载

下面脚本发送 **相同 system prompt + 相似 user prompt**，制造 prefix cache 条件：

```bash
#!/bin/bash
KEY="demo-key-001"
SECRET="${GATEYES_DEMO_SECRET}"

for i in $(seq 1 20); do
  curl -s http://127.0.0.1:8028/v1/chat/completions \
    -H "Authorization: Bearer ${KEY}:${SECRET}" \
    -H "Content-Type: application/json" \
    -d '{
      "model": "Qwen/Qwen3-0.6B",
      "messages": [
        {"role": "system", "content": "You are a helpful coding assistant. Always answer in Chinese."},
        {"role": "user", "content": "Explain what a prefix cache is, iteration '"$i"'"}
      ],
      "max_tokens": 64
    }' &
done
wait
```

说明：
- 相同的 system prompt 会被 vLLM prefix cache 命中
- 相同的 `(model, messages)` 会被 gateyes L1 cache 命中（如果 `cache.enabled: true`）

## 5. 关键 PromQL 查询

在 Prometheus / Grafana 中：

### vLLM prefix cache 命中率

```promql
rate(gateway_provider_prefix_cache_hits[5m])
/
rate(gateway_provider_prefix_cache_queries[5m])
```

或直接用已算好的 ratio：

```promql
gateway_provider_prefix_cache_hit_rate_ratio
```

### 上游返回的 prompt cache 比例

```promql
rate(gateway_llm_tokens_total{token_type="cached"}[5m])
/
rate(gateway_llm_tokens_total{token_type="prompt"}[5m])
```

### gateway L1 cache 命中率

```promql
rate(gateway_cache_lookups_total{result="hit"}[5m])
/
rate(gateway_cache_lookups_total[5m])
```

### 多实例负载与 KV cache 占用

```promql
gateway_provider_current_load

gateway_provider_gpu_cache_usage_ratio
```

## 6. 用 codexcli 开多个会话

codexcli / Claude Code 多窗口同时工作时，每个会话相当于独立客户端。建议：

1. 在 3-4 个 codexcli 会话中同时向 `http://127.0.0.1:8028/v1/chat/completions` 发送请求
2. 使用 **相同 system prompt** 但 **不同 user prompt**，验证 prefix cache 命中随请求数上升
3. 使用 **完全相同请求**，验证 gateyes L1 cache 命中（第二次请求应几乎瞬间返回）

## 7. 常见问题

### vLLM `/metrics` 没有 `cache_query_total`

确认启动参数包含 `--enable-prefix-caching`。较老版本 vLLM 可能使用 `vllm:cache_config_*` 系列指标，可用：

```bash
curl -s http://127.0.0.1:8001/metrics | grep -i cache
```

自行确认 metric 名称。本代码已按 vLLM v0.14+ 的 `cache_query_total` / `cache_query_hit` 解析。

### gateway 报告 provider unhealthy

`healthCheck.enabled: true` 时，gateway 会定期向 provider 发 probe。本地 vLLM 首次加载模型可能较慢，可把 `healthCheck.timeoutSeconds` 调大，或临时关闭健康检查。

### 多实例间 prefix cache 不共享

vLLM prefix cache 是进程内缓存。使用 `least_load` 策略时，请求会分散到多个实例，导致每个实例的 prefix cache 被稀释。如需最大化 vLLM 侧命中率，可：

- 临时只启用一个 vLLM provider
- 或在 router 前加 session affinity，把同会话固定到同一实例

## 8. 实验结束

1. 在 gateway 终端按 `Ctrl-C`
2. 在 vLLM launcher 终端按 `Ctrl-C`
3. 如需恢复默认配置，把 vLLM provider 重新设为 `enabled: false`
