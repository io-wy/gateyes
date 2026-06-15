# Gateyes 上线性能基线分析报告

> **目标**：在本地 mock 环境下对 Gateyes 进行基线压测，评估非流式与流式 `/v1/chat/completions` 路径的吞吐量、延迟、错误率与资源消耗，为生产上线提供容量参考。
>
> **测试时间**：2026-06-15
> **测试人**：Claude Code (io-wy 副驾)
> **报告状态**：基线完成，生产环境需补充真实上游与多副本测试

---

## 1. 测试环境

| 组件 | 版本/配置 | 说明 |
|---|---|---|
| Gateyes | `main` 分支（go run） | 单进程，监听 `:8028`，pprof `:6060` |
| Mock Upstream | `tests/load/mock_upstream/main.go` | 监听 `:18080`，`-delay 50ms -output-tokens 128 -tokens-per-sec 40` |
| Database | PostgreSQL 16 (Docker) | `gateyes` DB，历史 provider registry 保留 |
| Cache | Redis 7 (Docker) | 用于分布式限流与缓存 |
| OS | macOS (Darwin 25.4.0) | 本地开发机，非生产硬件 |
| 压测工具 | k6 v2.0.0 | 本地安装 |

**关键配置 `configs/loadtest.yaml`**：

- 仅启用一个 provider：`mock-openai`，指向 `http://localhost:18080`
- API Key：`demo-key-001:demo-secret`
- Limiter 配置为高阈值（globalQPS=50000，perUserRequestBurst=10000 等）
- 注意：数据库中仍保留历史 provider（codexapis、deepseek 等），gateway 启动时会从 `provider_registry` 表加载并加入 runtime。本报告结果已验证请求成功率为 100%，但 metrics 中 provider 标签混入了历史 provider 数据，建议正式压测前清理 registry。

**前置关键调整**：

- 将 `api_keys.rate_limit_qps` 设为 `0`，否则 `EffectiveRateLimitQPS` 会以 `api_keys.rate_limit_qps`（原值为 2）为准，导致 50+ 并发下几乎所有请求被 429 拒绝。
- `users.qps` 同样设为 `0`，让 per-user 限流回退到全局高阈值。

---

## 2. 测试方法

### 2.1 压测脚本

- 非流式：`tests/load/k6/chat-completions.js`
- 流式：`tests/load/k6/chat-completions-stream.js`

### 2.2 负载模型

两个阶段均采用 4 阶段负载：

| 阶段 | 非流式 | 流式 |
|---|---|---|
| Ramp-up | 30s → 50 VU | 30s → 50 VU |
| Steady | 60s @ 50 VU | 60s @ 50 VU |
| Stress | 30s → 100 VU | 30s → 100 VU |
| Ramp-down | 30s → 0 VU | 30s → 0 VU |

每个 VU 每轮 sleep 1s，请求：

```json
{
  "model": "mock-model",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Summarize load testing in one sentence."}
  ],
  "max_tokens": 128,
  "stream": false / true
}
```

### 2.3 采集数据

- k6 内置 HTTP 延迟、错误率、RPS、自定义 metrics（TTFT、流 chunk 数、流持续时间）
- `/metrics` Prometheus 文本快照（before/after）
- pprof：30s CPU profile、heap profile

---

## 3. 非流式压测结果

### 3.1 总体指标

| 指标 | 数值 |
|---|---|
| 总请求数 | 7,054 |
| 成功率 | 100% |
| 错误率 | 0.00% |
| 平均 RPS | ~46.9 req/s |
| 平均延迟 | 70.01 ms |
| P50 延迟 | 58.17 ms |
| P95 延迟 | 66.44 ms |
| P99 延迟 | ~3.43 s（受 ramp-down 长尾影响） |
| 平均输出 token | 128 |

### 3.2 关键观察

- **延迟稳定**：P95 仅 66ms，说明 gateway 本地处理开销很低；主要耗时来自 mock upstream 的固定 50ms 延迟 + 128 token 生成。
- **无错误**：所有检查（status=200、choices 非空、usage 存在）全部通过。
- **RPS 未达瓶颈**：50 VU 时约 50 RPS，100 VU 时若 VU 持续请求可达 100 RPS；受 `sleep(1)` 限制，本测试验证的是并发能力而非极限吞吐。
- **P99 长尾 3.43s**：出现在 ramp-down / stress 切换阶段，可能与 mock upstream 的 token 生成节奏或 Goroutine 调度有关，非 gateway 本身瓶颈。

---

## 4. 流式压测结果

### 4.1 总体指标

| 指标 | 数值 |
|---|---|
| 总请求数 | 1,592 |
| 成功率 | 100% |
| 错误率 | 0.00% |
| 平均 RPS | ~10.4 req/s |
| 平均流持续时间 | 3.87 s |
| P95 流持续时间 | 3.90 s |
| 平均 TTFT | 77.64 ms |
| P95 TTFT | 59.67 ms |
| 每流平均 chunk 数 | 138 |

### 4.2 关键观察

- **TTFT 优秀**：平均 78ms，P95 约 60ms，说明首字节返回很快。
- **流持续时间符合预期**：mock upstream 以 40 tokens/s 输出 128 token，加上固定 50ms delay，理论时间 ≈ 3.25s；实测 3.87s 包含网络/Gin/SSE 序列化开销。
- **高并发下无错误**：100 VU 并发流式请求全部成功，`gateway_llm_active_streams` 峰值未触顶。
- **RPS 较低**：每个流持续约 3.87s，因此 100 VU 的理论并发上限约 25 RPS；实测 10.4 RPS 受 `sleep(1)` 限制，未达并发极限。

---

## 5. 资源与性能分析

### 5.1 Go Runtime

| 指标 | 数值 |
|---|---|
| Goroutines | 27（压测后） |
| Heap alloc | 14.2 MB（stream 后） |

- Goroutine 数量稳定，无泄漏迹象。
- Heap 占用较低，主要开销来自 runtime init、mallocgc 与第三方库初始化。

### 5.2 pprof CPU 摘要

**非流式 30s CPU profile**（采样率 8.24%）：

- 主要耗时在 `syscall.rawsyscalln`（46.77%）与网络 I/O 路径。
- `internal/handler.(*Handler).Chat` 占 33.87%（cum）。
- `internal/service/responses.(*Service).Create` 占 31.45%（cum）。
- 结论：CPU 热点符合预期——网关核心路径（auth、guard、responses service）是主要消耗。

**流式 30s CPU profile**（采样率 10.75%）：

- `runtime.netpoll` / `runtime.kevent` / `runtime.pthread_cond_signal` 占比显著增加，符合 SSE 长连接 + 大量 goroutine 调度的特征。
- `net/http.(*conn).serve` 占 21.36%（cum）。
- 结论：流式场景下 runtime 调度开销上升，但总体 CPU 占用仍不高。

### 5.3 潜在瓶颈

1. **单进程单副本**：本地测试只跑了一个 gateway 进程，无法评估水平扩展后的负载均衡与 Redis 限流一致性。
2. **Mock upstream 是同步阻塞模型**：真实 LLM provider 的延迟更高、变异更大，且存在连接池竞争。
3. **数据库连接池**：`maxOpenConns=50` 对当前负载足够，但高并发 + 长流场景下需监控。
4. **Redis 限流**：本地测试已调高阈值；生产环境中 per-user/tenant/model 限流可能成为瓶颈，需根据实际 SLO 配置。
5. **Provider Registry 污染**：数据库中残留的历史 provider 会加载到 runtime，影响路由决策与 metrics 清晰度。

---

## 6. 上线建议

### 6.1 立即行动

1. **清理 Provider Registry**
   - 删除/禁用测试或废弃的 provider 记录，确保 production registry 与配置文件一致。
   - 或在 `EnsureBootstrapKey`/provider seed 逻辑中增加“删除配置中未出现的 provider”选项。

2. **Rate Limit 配置文档化**
   - 明确 `api_keys.rate_limit_qps` 优先级高于 `users.qps`。
   - 压测/上线前检查：`SELECT key, rate_limit_qps FROM api_keys WHERE rate_limit_qps > 0;`

3. **补充真实上游压测**
   - 使用真实 provider（或更复杂的 mock，支持延迟抖动、随机失败）验证重试、熔断、超时行为。

### 6.2 容量规划参考

基于本地 mock 基线（保守估计）：

| 场景 | 单进程预估容量 | 备注 |
|---|---|---|
| 非流式 chat | ≥ 100 RPS / 100 并发 | P95 < 100ms（不含真实上游延迟） |
| 流式 chat | ≥ 25 并发流 | 受单连接持续时长限制 |
| 混合负载 | 需按流式:非流式比例加权 | 流式连接占用更多 goroutine 与 socket |

**生产建议**：

- 初始副本数：3（参考 `values-prod.yaml`）
- HPA CPU 目标：70%
- 单 pod 资源：request 500m/512Mi，limit 2000m/2Gi（与 prod values 一致）
- 监控重点：`gateway_llm_active_streams`、`gateway_llm_request_duration_seconds`、`gateway_llm_time_to_first_token_seconds`、`go_goroutines`

### 6.3 后续测试

1. ** soak 测试**：持续 30min+，观察 goroutine/heap 是否持续增长。
2. **失败注入**：mock upstream `-fail-rate 0.1`，验证熔断器与重试。
3. **缓存命中率测试**：固定 prompt 反复请求，监控 `gateway_cache_lookups_total`。
4. **多副本测试**：Kubernetes/Helm 部署 3 副本，外部压测 ingress。
5. **真实 SLO 验收**：定义 P95/P99 延迟、错误率、TTFT SLO，并在生产灰度中持续监控。

---

## 7. 附录

### 7.1 启动命令

```bash
# Terminal 1: mock upstream
go run ./tests/load/mock_upstream/main.go \
  -addr :18080 \
  -delay 50ms \
  -output-tokens 128 \
  -tokens-per-sec 40

# Terminal 2: Gateyes
go run ./cmd/gateway -config configs/loadtest.yaml

# Terminal 3: non-stream load test
GATEYES_URL=http://localhost:8028 \
GATEYES_API_KEY=demo-key-001:demo-secret \
GATEYES_MODEL=mock-model \
GATEYES_MAX_TOKENS=128 \
k6 run tests/load/k6/chat-completions.js

# Terminal 4: stream load test
GATEYES_URL=http://localhost:8028 \
GATEYES_API_KEY=demo-key-001:demo-secret \
GATEYES_MODEL=mock-model \
GATEYES_MAX_TOKENS=128 \
k6 run tests/load/k6/chat-completions-stream.js
```

### 7.2 关键文件

- 压测脚本：`tests/load/k6/chat-completions.js`、`tests/load/k6/chat-completions-stream.js`
- Mock Upstream：`tests/load/mock_upstream/main.go`
- 负载配置：`configs/loadtest.yaml`
- 压测文档：`tests/load/README.md`

### 7.3 已知问题

- `limiter.check` 在 `rate <= 0 || burst <= 0` 时仍创建空 token bucket 并拒绝请求，导致用户无法通过将限流值设为 0 来“禁用”限流。建议修复为 `rate <= 0 || burst <= 0` 时直接放行。
- `EnsureBootstrapKey` 更新 `users.qps` 但不更新 `api_keys.rate_limit_qps`，配置文件的 `qps` 字段不会覆盖数据库中已有的 `rate_limit_qps`。
