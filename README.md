[English](./README.en.md) | 简体中文

# Gateyes

> 为 LLM 应用而生的生产级 API Gateway

Gateyes 是一个用 Go 编写的高性能 LLM API Gateway，在应用与上游模型提供商之间提供统一接入层。核心设计哲学是 **provider-native adapter** ——不做协议抹平，而是做精确转接，让 OpenAI、Anthropic、vLLM 等每种平台的原生能力都能被完整暴露。

```
应用层  ->  Gateyes Gateway  ->  OpenAI / Anthropic / vLLM / ...
              |
              +-> 多租户隔离 + RBAC
              +-> 限流 + 熔断 + 智能路由
              +-> 预算管控 + 审计日志
              +-> Prometheus + Grafana + OTLP 追踪
```

---

## 核心亮点

### 1. 四路 API 统一接入，零摩擦迁移

| 接口 | 协议来源 | 一句话说明 |
|---|---|---|
| `POST /v1/responses` | OpenAI Responses API | 内部主链路，所有能力从这里发散 |
| `POST /v1/chat/completions` | OpenAI Chat Completions | 存量业务零改动接入 |
| `POST /v1/messages` | Anthropic Messages API | Anthropic 生态直接对接 |
| `POST /v1/embeddings` | OpenAI Embeddings API | 文本向量化统一出口 |

三路文本生成接口共享同一套业务编排：provider 选择、重试、熔断、流式处理、usage 记录。换 provider 不改代码，换模型只改请求体。

### 2. Provider-Native Adapter，不削足适履

不做"最小公分母"协议抹平：

- **OpenAI adapter**：保留 tool_call、function_call、json_schema、responses 端点
- **Anthropic adapter**：完整保留 thinking、tool_use、citations 等特有字段
- **grpc-vllm adapter**：gRPC 直连 vLLM，支持 tokenizer 本地 decode，支持流式输出

新增 adapter 只需实现 `Provider` 接口，通过 `vendor` profile + `headers`/`extraBody` 覆盖即可扩展。

### 3. 企业级多租户隔离

运行时鉴权链路：`api_key:api_secret -> user -> tenant -> role`

| 隔离维度 | 说明 |
|---|---|
| 数据隔离 | user / api key / project / usage / responses 全隔离 |
| Provider 可见性 | 按 tenant 绑定可用 provider，未绑定即不可见 |
| 模型白名单 | user + api_key + virtual_key 三级 AND 关系精确控制 |
| 角色体系 | `super_admin` / `tenant_admin` / `tenant_user` 固定角色 |

### 4. 四层预算治理 + 多维度限流

**预算治理**（virtual_key -> api_key -> project -> tenant）：
- `hard_reject`：预算耗尽直接拒绝
- `soft_alert`：告警但不阻断，触发 webhook
- `grace`：宽限期模式，超支部分后续记账

**限流**（Redis Lua token bucket / 内存降级）：
- 全局 QPS/TPM、租户 TPM/RPM、Provider TPM/RPM、模型 QPS 多维度独立判定
- 无 Redis 时自动降级为内存模式，fail-open 容错

### 5. 智能路由 + 熔断自愈

| 路由策略 | 场景 |
|---|---|
| `round_robin` | 简单轮询 |
| `least_load` | 基于实时并发数的最小负载 |
| `cost_based` | 按配置价格优先低成本 provider |
| `sticky` | 同 session 命中同一 provider（SessionAffinity） |
| `ruleEngine` | 按 prompt 长度、工具调用等特征分流 |
| `prefix_affinity` | 同 prompt 前缀命中同一 provider（提升 prefix-cache 命中率） |

**熔断机制**：三态模型（healthy / degraded / unhealthy），定时探活 + 手动触发，状态变更自动持久化并告警。

### 6. 完整的可观测性体系

- **14 个 Prometheus 指标**：请求/延迟/上游延迟/TTFT/流式时长/token/错误/重试/熔断
- **OTLP 链路追踪**：HTTP exporter，W3C traceparent 传播
- **审计日志**：admin 关键写操作全记录
- **Grafana 基线 dashboard**：开箱即用

### 7. 生产级可靠性

- Graceful shutdown：SIGTERM 信号处理 + 连接排空
- 配置热重载：`POST /admin/reload` 无需重启
- Provider 动态管理：运行时增删改
- 三库兼容：SQLite（开发）/ PostgreSQL（生产）/ MySQL
- TDD 测试覆盖：`go test ./...` 全量回归

### 8. 性能实测

| 指标 | 数值 |
|---|---|
| Gateway 自身开销（P50） | ~28 ms |
| 端到端 P95 | ~170 ms |
| 单并发 RPS | ~8 req/s |
| 流式首 token 延迟（TTFT） | ~130 ms |
| 并发 CC=1 成功率 | 100% |

完整 benchmark 报告见 [`docs/tech-highlights-report.md`](./docs/tech-highlights-report.md)。

---

## 快速开始

### Docker Compose 一键部署（推荐）

```bash
git clone https://github.com/io-wy/gateyes.git && cd gateyes
cp .env.example .env
# 编辑 .env，填写 OPENAI_API_KEY 或 ANTHROPIC_API_KEY
docker compose up --build -d
curl http://127.0.0.1:8083/health
```

| 服务 | 地址 |
|---|---|
| Gateway | http://127.0.0.1:8083 |
| Prometheus | http://127.0.0.1:9090 |
| Grafana | http://127.0.0.1:3000 |

### 手动部署

```bash
go build -o ./bin/gateway ./cmd/gateway
cp configs/config.example.yaml configs/config.yaml
# 编辑 configs/config.yaml
./bin/gateway -config configs/config.yaml
```

### 发送第一个请求

```bash
curl -X POST http://localhost:8083/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hello"}]}'
```

### 零成本体验全部功能

使用内置 mock upstream，无需真实 API key：

```bash
# 1. 启动 mock 上游
go run ./benchmark/cmd/mockupstream/main.go -port 19999

# 2. 启动 gateway（使用 mock 配置）
./bin/gateway -config configs/demo-mock.yaml

# 3. 体验 Responses / Chat / Messages / Embeddings 全部接口
curl -X POST http://localhost:8083/v1/responses \
  -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"model": "mock-model", "input": "hello"}'
```

---

## 架构概览

```
HTTP Request
    |
    v
[Gin Router]
    |
    +-- Auth Middleware  --------->  api_key -> user -> tenant -> role
    +-- Guard Middleware  -------->  模型白名单 + 配额 + 预算 + 限流
    |
    v
[Handler]
    |
    +-- OpenAI / Anthropic 兼容转换
    |
    v
[Responses Service]
    |
    +-- 查询 tenant 可用 provider
    +-- 健康/能力/权重过滤
    +-- ruleEngine -> ranker -> strategy 排序
    +-- 重试 / fallback
    +-- response 持久化
    +-- usage 记录 + 多级预算扣减
    |
    v
[Provider Adapter]  ->  OpenAI / Anthropic / grpc-vllm
```

详细架构见 [`docs/runtime-mechanisms.md`](./docs/runtime-mechanisms.md)。

---

## API 文档

完整 API 规范（含所有 endpoint、请求/响应 schema、认证方式）：

- [`docs/openapi.json`](./docs/openapi.json) — OpenAPI 3.0 规范
- 导入 Postman / Swagger UI / Stoplight 即可使用

---

## 文档导航

完整文档索引见 [`docs/README.md`](./docs/README.md)。

快速定位：

| 我想... | 看这里 |
|---|---|
| 部署 Gateway | [`docs/deployment.md`](./docs/deployment.md) |
| 理解鉴权/限流/路由/预算实现 | [`docs/runtime-mechanisms.md`](./docs/runtime-mechanisms.md) |
| 接入新 Provider | [`docs/provider-protocol.md`](./docs/provider-protocol.md) |
| 配置监控与告警 | [`docs/monitoring.md`](./docs/monitoring.md) |
| 排查线上故障 | [`docs/runbook.md`](./docs/runbook.md) |
| 了解 benchmark 数据 | [`docs/tech-highlights-report.md`](./docs/tech-highlights-report.md) |

---

## 运行要求

- Go 1.25+
- PostgreSQL（推荐生产）或 SQLite（开发）
- Redis（推荐，用于分布式限流；不部署时自动降级内存模式）

---

## 与同类项目对比

| 维度 | Gateyes | 典型网关 |
|---|---|---|
| 协议策略 | Provider-native（保留各平台特性） | 最小公分母（抹平差异） |
| 内部主链路 | Responses API | Chat Completions |
| 多租户 | 完整隔离 + RBAC | 通常无或弱隔离 |
| 预算管控 | 四级预算 + 三种策略 | 通常仅 API Key 级别 |
| 限流 | Redis Lua 多维度 token bucket | 通常简单 QPS 限流 |
| 路由 | 5 种策略 + ruleEngine + Affinity（session/prefix） | 通常仅轮询/随机 |
| L1 缓存 | Redis + 内存 LRU，精确匹配 | 通常无 |
| 熔断 | 内置健康检查 + 三态熔断 | 通常无或简单超时 |
| gRPC 上游 | 原生支持 vLLM gRPC | 通常仅 HTTP |
| 可观测性 | 14 指标 + OTLP + 审计日志 | 通常基础指标 |

---

## License

MIT
