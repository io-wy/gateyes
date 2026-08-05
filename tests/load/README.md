# Gateyes 负载测试

本目录包含一个 mock 上游 LLM 服务器和 [k6](https://k6.io) 负载测试脚本，用于在生产发布前验证 Gateyes 的性能、容量和可靠性。

## 目录结构

```
tests/load/
├── mock_upstream/main.go          # OpenAI-compatible mock LLM server
├── k6/
│   ├── constants.js               # 共享环境变量默认值
│   ├── chat-completions.js        # 非流式负载测试
│   └── chat-completions-stream.js # SSE 流式负载测试
└── README.md                      # 本文件
```

## 快速开始

### 1. 启动依赖

使用本地 docker-compose 栈：

```bash
docker compose up -d postgres redis
```

### 2. 启动 mock 上游

在一个终端中运行：

```bash
make load-mock-upstream
```

该服务监听 `:18080`，返回 OpenAI-compatible `/v1/chat/completions` 响应。默认行为：

- 首 token 前固定延迟：`50ms`
- 每次响应的 completion tokens：`64`
- 流式 token 速率：`20 tokens/s`
- 失败率：`0`

可通过 flag 覆盖：

```bash
go run ./tests/load/mock_upstream/main.go \
  -addr :18080 \
  -delay 100ms \
  -output-tokens 256 \
  -tokens-per-sec 50 \
  -fail-rate 0.05 \
  -stream-jitter 10ms
```

### 3. 配置 Gateyes 使用 mock 上游

创建一份专用负载测试配置，例如 `configs/loadtest.yaml`：

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

启动 Gateyes：

```bash
go run ./cmd/gateway -config configs/loadtest.yaml
```

### 4. 运行负载测试

非流式：

```bash
make load-chat
```

流式：

```bash
make load-chat-stream
```

通过环境变量覆盖默认值：

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

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `GATEYES_URL` | `http://localhost:8028` | Gateyes 基础 URL |
| `GATEYES_API_KEY` | `demo-key-001:demo-secret` | API key 与 secret（格式 `key:secret`） |
| `GATEYES_MODEL` | `mock-model` | 请求的模型名 |
| `GATEYES_MAX_TOKENS` | `128` | 请求中的 `max_tokens` |
| `GATEYES_TARGET_CONCURRENCY` | `100` | 稳态并发 VUs |
| `GATEYES_STRESS_CONCURRENCY` | `300` | 峰值压力 VUs |
| `GATEYES_DURATION_RAMP` | `1m` | 爬坡时长 |
| `GATEYES_DURATION_STEADY` | `3m` | 稳态时长 |
| `GATEYES_DURATION_STRESS` | `2m` | 压力时长 |
| `GATEYES_DURATION_RAMP_DOWN` | `1m` | 下坡时长 |

## 负载测试期间分析网关

Gateyes 在 `:6060` 暴露 Go pprof。在 k6 运行期间采集 profile：

```bash
# 终端 1：运行负载测试
make load-chat

# 终端 2：采集 CPU profile
make pprof-cpu

# 或采集 heap profile
make pprof-heap
```

常用 pprof 视图：

```bash
go tool pprof -http=:8080 /tmp/gateyes-cpu.pb.gz
go tool pprof -http=:8080 /tmp/gateyes-heap.pb.gz
```

## 需要关注的指标

测试期间打开 Grafana（`http://localhost:3000`）并关注：

- `gateway_llm_requests_total` —— 按 surface/provider/result 的 RPS
- `gateway_llm_request_duration_seconds` —— 端到端 P50/P95/P99
- `gateway_llm_upstream_duration_seconds` —— 仅上游延迟
- `gateway_llm_time_to_first_token_seconds` —— 流式首 token 时间
- `gateway_llm_active_streams` —— 并发流式会话数
- `gateway_llm_inflight_requests` —— 在途请求压力
- `gateway_llm_errors_total` —— 错误分布
- `gateway_cache_lookups_total` —— 缓存命中/未命中行为
- Go runtime 指标：`go_goroutines`、`go_memstats_heap_alloc_bytes`

## 建议测试场景

1. **基线** —— `TARGET=100`、`STRESS=300`，验证 P95 < 5s 且错误率 < 1%。
2. **持续浸泡** —— 运行 `load-chat` 30 分钟以上，排查 goroutine 或连接泄漏。
3. **流式饱和** —— 高并发下运行 `load-chat-stream`，寻找 active-stream 上限。
4. **故障注入** —— 以 `-fail-rate 0.1` 启动 mock 上游，验证重试/熔断。
5. **缓存预热** —— 重复发送相同 prompt，观察 `gateway_cache_lookups_total` 命中率。

## 新增 k6 脚本

1. 创建 `tests/load/k6/<name>.js`。
2. 从 `./constants.js` 引入共享常量。
3. 在 Makefile 中添加对应的 `make load-<name>` 目标。
4. 在本 README 中记录场景说明与关键指标。
