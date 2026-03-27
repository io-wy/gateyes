[English](./README.en.md) | 简体中文

# Gateyes

Gateyes 是一个用 Go 编写的 LLM API Gateway，定位是应用和上游模型提供商之间的统一接入层。

当前版本以中文 README 为准，重点已经从内存原型推进到可持久化、可管理的早期可运行版本，核心方向是：

- `Responses API` 作为主入口
- `Chat Completions` 作为兼容层保留
- 多租户隔离
- 固定角色 RBAC
- 运行时数据库鉴权
- provider-native adapter（当前内置 OpenAI / Anthropic）

## 当前已实现

### API

- `GET /health`
- `GET /ready`
- `GET /metrics`
- `POST /v1/responses`
- `GET /v1/responses/:id`
- `POST /v1/chat/completions`
- `POST /v1/messages`
- `GET /v1/models`
- `GET /admin/dashboard`
- `GET /admin/providers`
- `GET /admin/providers/:name`
- `GET /admin/providers/:name/stats`
- `GET /admin/users`
- `POST /admin/users`
- `GET /admin/users/:id`
- `PUT /admin/users/:id`
- `DELETE /admin/users/:id`
- `POST /admin/users/:id/reset`
- `GET /admin/users/:id/usage`
- `GET /admin/tenants`
- `POST /admin/tenants`
- `GET /admin/tenants/:id`
- `PUT /admin/tenants/:id`
- `POST /admin/tenants/:id/providers`

### 功能

- 运行时鉴权从数据库读取 `api_key -> user -> tenant`
- 支持 SQLite / PostgreSQL / MySQL 三种 `database/sql` 驱动
- 启动时自动执行 migration
- 配置中的 `apiKeys` 会作为 bootstrap 数据写入数据库
- 启动时自动确保默认 tenant，并回填历史无 tenant 数据
- 启动时可自动创建 bootstrap `super_admin`
- admin 创建用户时会生成 `api_key` 和 `api_secret`
- `/admin/*` 和 `/v1/*` 统一使用 Bearer 鉴权，不再区分 `X-Admin-Key`
- 多租户隔离已经覆盖：
  - user
  - api key
  - usage
  - responses
  - tenant 可见 provider 列表
- 固定角色 RBAC：
  - `super_admin`
  - `tenant_admin`
  - `tenant_user`
- middleware 已接管横切能力：
  - 鉴权
  - 角色校验
  - 模型白名单校验
  - 配额预检查
  - 基础限流
- `POST /v1/responses` 是内部主链路
- `POST /v1/chat/completions` 仅做 compatibility shim，内部会转换到 responses service
- `POST /v1/messages` 提供 Anthropic-compatible 入口，内部同样会转换到 responses service
- OpenAI / Anthropic compatibility DTO 与 SSE 编码已收敛到 `internal/protocol/apicompat`
- `GET /v1/responses/:id` 可读取已持久化 response
- provider 抽象已改为 response-first
- 当前内置 provider adapter：
  - `openai`（支持 `chat` 和 `responses` 两种端点）
  - `anthropic`
- 基础路由策略：
  - `round_robin`
  - `random`
  - `least_load`
  - `cost_based`
  - `sticky`
- 支持非流式内存缓存
- 支持 SSE 流式输出
- 请求 usage 会写入数据库
- provider 统计可通过 admin API 查看
- 增强缓存：
  - LRU 淘汰策略（按访问时间）
  - 命中率/淘汰次数统计
  - 后台定时清理过期数据
- Token 管理：
  - 用户使用量趋势查询
  - Tenant 使用量趋势查询
  - 配额告警 webhook（HMAC 签名 + 24h 去重）
- TDD 测试覆盖：
  - cache、alert、router、limiter

## 当前架构

补充机制文档：

- [`docs/runtime-mechanisms.md`](./docs/runtime-mechanisms.md)：缓存、鉴权、限流、路由、权限模型的当前实现说明

当前链路已经调整为：

1. HTTP 请求进入 Gin router
2. `internal/middleware` 处理鉴权、角色校验、模型/配额预检查、限流
3. `internal/handler` 只负责：
   - 请求绑定
   - 调用 `internal/protocol/apicompat` 做 compatibility 转换
   - HTTP / SSE 编码
4. `internal/service/responses` 负责主业务编排：
   - 选择 tenant 可用 provider
   - 创建 / 更新 response 持久化记录
   - 调用 provider
   - 写入 usage
   - 处理缓存与流式收尾
5. `internal/service/provider` 负责把统一 response 请求映射成上游协议
6. `internal/protocol/apicompat` 负责：
   - OpenAI / Anthropic 请求转内部 canonical request
   - 内部 canonical response 转兼容协议响应
   - SSE stateful stream encoder
7. `internal/repository` / `internal/repository/sqlstore` 负责数据库访问

### 目录分层

- `cmd/gateway`：程序入口与装配
- `internal/config`：配置结构
- `internal/db`：数据库连接与 migration
- `internal/middleware`：鉴权（Auth）、角色（RBAC）、请求守卫（模型白名单 + 配额 + 限流）
- `internal/handler`：HTTP handler 和 admin API
- `internal/protocol/apicompat`：OpenAI / Anthropic 兼容协议转换与流式编码
- `internal/service/responses`：responses 主业务编排
- `internal/service/provider`：provider interface + OpenAI / Anthropic adapter
- `internal/service/router`：路由策略
- `internal/service/cache`：内存缓存（LRU + 统计 + 后台清理）
- `internal/service/limiter`：基础限流器
- `internal/service/alert`：配额告警 webhook
- `internal/repository`：领域接口
- `internal/repository/sqlstore`：`database/sql` 实现

## 快速开始

### 运行要求

- Go 1.25+

### 构建

```bash
go build -o ./bin/gateway ./cmd/gateway
```

### 配置

编辑 [`configs/config.yaml`](./configs/config.yaml)。

最小示例：

```yaml
server:
  listenAddr: ":8080"

database:
  driver: sqlite
  dsn: gateyes.db
  autoMigrate: true

providers:
  - name: openai-primary
    type: openai
    baseURL: https://api.openai.com/v1
    endpoint: chat
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4o-mini
    maxTokens: 4096
    timeout: 60
    enabled: true

  - name: anthropic-primary
    type: anthropic
    baseURL: https://api.anthropic.com
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-3-5-sonnet-latest
    maxTokens: 4096
    timeout: 60
    enabled: true

  - name: longcat-primary
    type: openai
    baseURL: https://api.longcat.chat/openai
    endpoint: chat
    apiKey: ${LONGCAT_API_KEY}
    model: LongCat-Flash-Chat
    weight: 10
    enabled: true

apiKeys:
  - key: test-key-001
    secret: test-secret
    quota: 1000000
    qps: 100
    models: []

admin:
  defaultTenant: default
  bootstrapKey: admin-key-001
  bootstrapSecret: admin-secret-001
```

**Provider `endpoint` 配置说明：**
- `chat`：使用 `/v1/chat/completions` 端点（OpenAI 兼容）
- `responses`：使用 `/responses` 端点（OpenAI 新版 Responses API）
- 默认：`chat`

数据库支持：

- `sqlite`
- `postgres`
- `mysql`

### 启动

```bash
./bin/gateway -config configs/config.yaml
```

## 鉴权与角色

运行时和管理端统一使用：

```text
Authorization: Bearer <api_key>:<api_secret>
```

固定角色：

- `super_admin`：跨 tenant 管理，拥有 tenant 管理能力
- `tenant_admin`：管理本 tenant 的用户、provider 绑定和统计
- `tenant_user`：访问 `/v1/*`

## API 示例

### Responses 主接口

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "input": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

当前也兼容旧写法：

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

### Chat Completions 兼容接口

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

### 读取已创建 response

```bash
curl http://localhost:8080/v1/responses/<response_id> \
  -H "Authorization: Bearer test-key-001:test-secret"
```

### 创建 tenant

```bash
curl -X POST http://localhost:8080/admin/tenants \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "demo",
    "name": "Demo Tenant"
  }'
```

### 为 tenant 绑定 provider

```bash
curl -X POST http://localhost:8080/admin/tenants/demo/providers \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{
    "providers": ["openai-primary", "anthropic-primary"]
  }'
```

## 当前边界

这版已经是可运行网关，但还不是完整平台。当前已知边界：

- `Responses API` 仍是”收敛后的统一实现”，不是完整覆盖 OpenAI 全量 Responses 协议字段
- `POST /v1/chat/completions` 保留为兼容接口，不再作为内部主链路
- provider 目前仍从配置加载，不是数据库动态管理
- retry、fallback、circuit breaker 已接上基础实现，但真实 upstream provider 兼容性仍要按 provider 单独验证
- billing、预算和主动健康检查还不是完整生产方案
- cache 仍然是内存实现，不是分布式缓存
- 流式 usage 在依赖上游事件完整度时可以精确回填；上游不给完整信息时会回退到近似估算
- 当前 `go test ./...` 已全绿

## 当前验证快照（2026-03-26）

### 自动化测试

- `go test ./...` 已通过

### 当前 provider 网关人工验证快照

以下现象来自本轮协议兼容重构前的人工实测，主要用于记录特定 upstream provider 的历史兼容性问题：

- `longcat-primary`
  - `/v1/chat/completions` 非流式可用
  - `/v1/chat/completions` 流式有输出但 delta 抽取不稳定
  - `/v1/responses` 实测过超时，也实测过返回 `output: null`
- `minimax`
  - `/v1/messages` 非流式可用
  - `/v1/responses` 非流式可用
  - tool use 基本可用
  - stream 统一抽象仍不稳定

更细的验证记录见 [`TESTING.md`](./TESTING.md)。

## 接下来适合做的事

- 更完整的 OpenAI Responses 协议兼容层
- provider 动态管理与热更新
- 更细粒度 RBAC / 审计日志
- 预算、账单、告警
- 熔断、重试、健康检查
- 分布式缓存
- 更多 provider adapter（除 OpenAI / Anthropic 外继续扩展）
