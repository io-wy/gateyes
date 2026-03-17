[English](./README.en.md) | 简体中文

# Gateyes

Gateyes 是一个用 Go 编写的 LLM Gateway，目标是作为应用与模型提供商之间的统一接入层。

当前仓库已经实现了一套可运行的最小核心能力：

- 基于静态 API Key 的请求认证
- 面向聊天场景的上游代理转发
- 多个 provider 之间的基础路由策略
- 内存缓存与基础限流
- SSE 流式转发
- 管理接口与基础观测接口

项目的长期方向不止于当前这套 API 形态。Gateyes 计划演进为一个支持多种 provider 协议的网关，例如 OpenAI 风格接口、Anthropic 以及其他模型厂商，通过不同 adapter 统一接入。

## 当前状态

这个仓库现在更适合被理解为“早期可运行版本”，还不是完整平台。

当前已经落地的接口：

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `GET /v1/responses`，用于 WebSocket
- `GET /v1/models`
- `GET /health`
- `GET /ready`
- `GET /metrics`
- `GET /debug/pprof/*`
- `GET/POST/PUT/DELETE /admin/...`

当前实现边界：

- 运行时认证依赖配置文件中的 `apiKeys`
- 上游 provider 调用目前基于 chat-completions 风格请求
- 缓存、用户数据、provider 状态都在内存中
- 管理端的用户接口已经存在，但还没有接入运行时认证链路
- provider 统计接口已经暴露，但完整的实时统计还没有全部打通

这份 README 会严格区分“已经实现”和“未来规划”，避免把想法写成既有能力。

## 为什么做这个项目

很多团队一开始都是直接在业务代码里调用单一模型厂商 SDK。随着需求增加，通常会逐步遇到这些问题：

- 认证逻辑分散
- 配额和限流难以统一治理
- 更换 provider 的成本高
- 观测能力重复建设
- 缺少一层稳定的网关来隔离上游差异

Gateyes 的目标就是逐步补上这一层，从一个足够小、足够容易跑起来的网关开始往前迭代。

## 架构概览

当前请求流程：

1. 客户端调用 `/v1/*` 接口
2. 认证中间件校验 `Authorization: Bearer key:secret`
3. 对非流式请求先尝试命中缓存
4. 限流器执行全局和按 key 的限制
5. Router 从已配置 provider 中选择一个上游
6. Provider 将请求转发到上游模型服务
7. 结果以普通 JSON 或 SSE 的形式返回

当前核心目录：

- `cmd/gateway`：程序入口
- `internal/handler`：HTTP handler、路由与服务装配
- `internal/service/provider`：上游 provider 调用逻辑
- `internal/service/router`：路由策略
- `internal/service/limiter`：限流与排队
- `internal/service/cache`：内存 KV 缓存
- `internal/service/streaming`：流式透传
- `internal/repository`：内存中的 API Key / User 仓库
- `internal/config`：YAML 配置加载与环境变量替换

## 快速开始

### 运行要求

- Go 1.21+

### 构建

```bash
go build -o ./bin/gateway ./cmd/gateway
```

### 配置

直接编辑 [`configs/config.yaml`](./configs/config.yaml)。

最小示例：

```yaml
server:
  listenAddr: ":8080"

metrics:
  namespace: gateway
  enabled: true

router:
  strategy: round_robin
  stickySession: false

limiter:
  globalQPS: 1000
  globalTPM: 1000000
  burst: 100
  queueSize: 1000

cache:
  enabled: true
  maxSize: 10000
  ttl: 3600

providers:
  - name: primary
    type: openai
    baseURL: https://your-upstream.example.com
    apiKey: ${UPSTREAM_API_KEY}
    model: your-model-name
    weight: 10
    priceInput: 0.0001
    priceOutput: 0.0003
    maxTokens: 4096
    timeout: 60

apiKeys:
  - key: demo-key
    secret: demo-secret
    quota: 1000000
    qps: 100
    models: []

admin:
  adminKey: change-me
```

`internal/config/config.go` 里的加载逻辑支持 `${ENV_VAR}` 形式的环境变量替换，因此上游密钥可以放在环境变量里。

### 启动

```bash
./bin/gateway -config configs/config.yaml
```

默认监听 `:8080`。

## API 示例

### 健康检查

```bash
curl http://localhost:8080/health
```

### Chat Completions

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer demo-key:demo-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model-name",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

### Responses API

当前 `POST /v1/responses` 内部仍然复用 chat 处理链路。

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Authorization: Bearer demo-key:demo-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model-name",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

### Streaming

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer demo-key:demo-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model-name",
    "stream": true,
    "messages": [
      {"role": "user", "content": "say hi"}
    ]
  }'
```

### Models

```bash
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer demo-key:demo-secret"
```

### 管理接口

```bash
curl http://localhost:8080/admin/dashboard \
  -H "X-Admin-Key: change-me"
```

当前服务还暴露了这些管理接口：

- `GET /admin/providers`
- `GET /admin/providers/:name`
- `GET /admin/providers/:name/stats`
- `GET /admin/users`
- `POST /admin/users`
- `GET /admin/users/:id`
- `PUT /admin/users/:id`
- `DELETE /admin/users/:id`
- `POST /admin/users/:id/reset`

## 配置说明

当前生效的配置结构以 [`internal/config/config.go`](./internal/config/config.go) 为准。

主要字段：

- `server.listenAddr`
- `server.readTimeout`
- `server.writeTimeout`
- `metrics.namespace`
- `metrics.enabled`
- `router.strategy`
- `router.stickySession`
- `limiter.globalQPS`
- `limiter.globalTPM`
- `limiter.burst`
- `limiter.queueSize`
- `cache.enabled`
- `cache.maxSize`
- `cache.ttl`
- `providers[]`
- `apiKeys[]`
- `admin.adminKey`

当前 Router 支持的策略：

- `round_robin`
- `random`
- `least_load`
- `cost_based`
- `sticky`

当前 provider 配置假定上游具备 chat-completions 风格能力，主要字段包括：

- `baseURL`
- `apiKey`
- `model`
- `timeout`

配置里的 `type` 字段已经预留出来，便于后续接入不同 provider adapter，但当前实现还没有依据 `type` 切换不同协议逻辑。

## 开发说明

建议先读这些文件：

- [`cmd/gateway/main.go`](./cmd/gateway/main.go)
- [`internal/handler/handler.go`](./internal/handler/handler.go)
- [`internal/handler/admin.go`](./internal/handler/admin.go)
- [`internal/service/provider/provider.go`](./internal/service/provider/provider.go)
- [`internal/service/router/router.go`](./internal/service/router/router.go)
- [`TESTING.md`](./TESTING.md)

当前比较明确的实现缺口：

- admin 创建的用户还没有进入实际鉴权链路
- 用户、缓存、状态都没有持久化
- provider 统计只接了部分骨架
- 路由策略目前是基础实现
- 非 chat-completions 风格的 provider adapter 还没实现

## Roadmap

接下来比较合理的演进方向包括：

- 更完整的智能路由，结合健康度、延迟、成本、失败历史做选择
- 对 Anthropic 等非 OpenAI 风格协议提供专用 adapter
- 持久化用户、API Key、配额和使用量
- 多租户隔离
- 更完整的管理后台与使用量统计
- 熔断、重试、健康检查
- 更准确的 token 计量
- 分布式缓存与共享限流

README 故意把这些内容放在 roadmap，而不是写成“已经支持”，这样对外信息才不会失真。
