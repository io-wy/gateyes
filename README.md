[English](./README.en.md) | 简体中文

# Gateyes

Gateyes 是一个用 Go 编写的 LLM Gateway，定位是应用与上游模型提供商之间的统一接入层。

当前版本已经从内存原型推进到可持久化、可管理的早期可运行版本，重点放在：

- 统一 API 接入
- 多 provider 路由
- 数据库存储用户、API Key、租户和 usage
- 多租户隔离
- 固定角色 RBAC
- 通过 adapter 接入不同上游协议

## 当前已实现

API：

- `GET /health`
- `GET /ready`
- `GET /metrics`
- `GET /debug/pprof/*`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `GET /v1/responses/:id`
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
- `GET /admin/tenants`
- `POST /admin/tenants`
- `GET /admin/tenants/:id`
- `PUT /admin/tenants/:id`
- `POST /admin/tenants/:id/providers`

能力：

- 运行时鉴权走数据库，不再只依赖内存配置
- 支持 SQLite / PostgreSQL / MySQL 三种 `database/sql` 驱动
- 启动时自动执行 migration
- 配置中的 `apiKeys` 会作为 bootstrap 数据写入数据库
- 启动时自动确保默认 tenant，并回填历史无 tenant 数据
- 启动时自动创建 bootstrap `super_admin`
- admin 创建用户时会生成 `api_key` 和 `api_secret`
- admin 创建出的 key 可以直接用于 `/v1/*` 请求
- 支持多租户隔离：
  - user / api key / usage / responses
  - tenant 可见 provider 列表
- 支持固定角色 RBAC：
  - `super_admin`
  - `tenant_admin`
  - `tenant_user`
- `/admin/*` 已统一改为 Bearer 鉴权，不再使用 `X-Admin-Key`
- 用户状态、租户状态、配额、模型白名单进入运行时鉴权链路
- provider adapter 已支持：
  - `openai`
  - `anthropic`
- 支持基础路由策略：
  - `round_robin`
  - `random`
  - `least_load`
  - `cost_based`
  - `sticky`
- 支持非流式内存缓存
- 支持基础限流
- 支持 SSE 流式转发
- 支持 provider 统计接口
- 请求 usage 会写入数据库
- `POST /v1/responses` 已是独立 handler
- `GET /v1/responses/:id` 可读取已持久化 response

## 当前边界

这版仍然是早期网关，不是完整平台。

当前还没做完的部分：

- `POST /v1/responses` 虽然已经独立成路由和持久化对象，但内部仍复用 chat provider 能力做协议映射，不是完整的 Responses 协议兼容层
- stream 请求的 usage/token 统计仍然是近似值，不是精确闭环
- provider 目前仍然从配置加载，不是数据库动态管理
- Anthropic adapter 当前聚焦 messages/chat 主路径
- 预算、billing、熔断、重试、主动健康检查还没接上
- cache 仍然是内存实现，不是分布式缓存

## 架构概览

当前请求流程：

1. 客户端请求 `/v1/*`
2. `Authorization: Bearer key:secret` 进入鉴权
3. 从数据库读取 key、user、tenant、角色、模型权限、配额
4. 非流式请求先尝试 cache
5. limiter 执行限流
6. 根据 tenant 可用 provider 集合做路由选择
7. provider adapter 将内部统一请求转换成上游协议
8. 响应返回客户端，并写入 usage / responses

核心目录：

- `cmd/gateway`：程序入口
- `internal/config`：配置结构
- `internal/db`：数据库连接和 migration
- `internal/repository`：领域接口
- `internal/repository/sqlstore`：`database/sql` 实现
- `internal/handler`：HTTP handler 和 admin API
- `internal/service/auth`：运行时鉴权和角色判断
- `internal/service/provider`：provider interface + OpenAI / Anthropic adapter
- `internal/service/router`：路由策略
- `internal/service/cache`：内存缓存
- `internal/service/limiter`：限流
- `internal/service/streaming`：SSE 转发

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
    apiKey: ${OPENAI_API_KEY}
    model: gpt-4o-mini
    maxTokens: 4096
    timeout: 60
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

默认 bootstrap admin 由配置中的以下字段生成：

- `admin.defaultTenant`
- `admin.bootstrapKey`
- `admin.bootstrapSecret`

固定角色说明：

- `super_admin`：跨 tenant 管理，拥有 tenant 管理能力
- `tenant_admin`：管理本 tenant 的用户、provider 绑定和统计
- `tenant_user`：只允许访问 `/v1/*`

## API 示例

健康检查：

```bash
curl http://localhost:8080/health
```

聊天请求：

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

Responses 请求：

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

读取已创建 response：

```bash
curl http://localhost:8080/v1/responses/<response_id> \
  -H "Authorization: Bearer test-key-001:test-secret"
```

创建 tenant：

```bash
curl -X POST http://localhost:8080/admin/tenants \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "demo",
    "name": "Demo Tenant"
  }'
```

为 tenant 绑定 provider：

```bash
curl -X POST http://localhost:8080/admin/tenants/demo/providers \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{
    "providers": ["openai-primary"]
  }'
```

创建 tenant 用户：

```bash
curl -X POST http://localhost:8080/admin/users \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "demo",
    "name": "demo-user",
    "role": "tenant_user",
    "quota": 1000000,
    "qps": 20,
    "models": ["gpt-4o-mini"]
  }'
```

返回会包含：

- `api_key`
- `api_secret`
- `token`

可以直接用返回的 `token` 访问 `/v1/*`。

## 配置说明

当前主要配置字段：

- `server.*`
- `database.*`
- `metrics.*`
- `router.*`
- `limiter.*`
- `cache.*`
- `providers[]`
- `apiKeys[]`
- `admin.defaultTenant`
- `admin.bootstrapKey`
- `admin.bootstrapSecret`

`providers[].type` 当前支持：

- `openai`
- `anthropic`

`apiKeys[]` 当前主要用于 bootstrap：

- 首次启动或后续启动时，会被同步到数据库
- 默认归属 `admin.defaultTenant`
- role 默认为 `tenant_user`

## 开发验证

本轮改造后，已本地执行：

```bash
go test ./...
```

通过。

## Roadmap

下一阶段比较合理的方向：

- 智能路由，结合健康度、延迟、成本、失败率
- provider 动态管理与热更新
- request / stream 更精确的 usage 统计
- 预算与 billing
- 熔断、重试、主动健康检查
- Redis / 分布式缓存
- 更完整的 Anthropic / OpenAI / 其他厂商协议兼容层
