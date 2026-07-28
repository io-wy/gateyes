# Runtime Mechanisms

> 最后更新：2026-07-28

本文档按运行时链路盘点 Gateyes 当前已经落地的核心机制：API 入口、认证、RBAC、限流、预算、路由、缓存、观测、审计和后台管理。

---

## 1. API 入口

### OpenAI / Anthropic 兼容入口

所有 LLM 请求先进入 `internal/handler/server.go`，再由 handler 转成内部 `provider.ResponseRequest`。

| Surface | 路由 | 说明 |
| --- | --- | --- |
| Responses | `POST /v1/responses` | OpenAI Responses 主入口，支持 JSON 和 SSE |
| Chat Completions | `POST /v1/chat/completions` | OpenAI SDK 兼容 |
| Messages | `POST /v1/messages` | Anthropic SDK 兼容 |
| Embeddings | `POST /v1/embeddings` | 选择支持 embeddings 的 provider |
| Images | `POST /v1/images/generations` | 选择支持 images 的 provider |
| Service runtime | `POST /service/:prefix/*` | 通过 prompt service catalog 调用业务级服务 |

`GET /v1/models` 返回当前租户可用模型、provider、健康状态和能力信息。

### Admin API

后台接口主路径是 `/admin/v1/*`，同时保留 `/admin/*` legacy alias。

| 模块 | 代表路由 | 权限 |
| --- | --- | --- |
| Dashboard / usage / cache | `/admin/v1/dashboard`, `/admin/v1/cache/summary` | `usage:read` |
| Providers | `/admin/v1/providers` | `provider:read/write` |
| API keys / virtual keys | `/admin/v1/keys`, `/admin/v1/virtual-keys` | `api_key:*`, `virtual_key:*` |
| Services | `/admin/v1/services` | `service:read/write` |
| Responses | `/admin/v1/responses/:id`, `/admin/v1/responses/:id/trace` | `response:read` |
| Roles / permissions | `/admin/v1/roles`, `/admin/v1/permissions` | `config:write` |

---

## 2. 认证

### LLM 请求认证

支持两种请求头：

```text
Authorization: Bearer <api_key>:<api_secret>
X-Api-Key: <api_key>:<api_secret>
```

流程：

```text
header
  -> auth.ExtractKey
  -> store.Authenticate(api key)
  -> fallback: store.AuthenticateVirtualKey(virtual key)
  -> 校验 api_keys / users / tenants / projects 状态
  -> SHA-256 secret hash 比对
  -> AuthIdentity 写入 Gin context
```

Virtual Key 认证成功后会加载父 API Key 身份，并受父级 API Key、Project、Tenant 的状态和预算边界约束。

关键代码：

- `internal/service/auth/auth.go`
- `internal/handler/middleware/auth.go`
- `internal/repository/sqlstore/api_keys.go`

### Admin 认证

`mw.AdminAuth()` 同时支持：

- API Key：用于 bootstrap、脚本和兼容调用
- JWT access token：用于前端管理台和 OIDC 登录后访问

OIDC 入口：

- `GET /admin/auth/oidc/login`
- `GET /admin/auth/callback`
- `POST /admin/auth/refresh`
- `POST /admin/auth/logout`

关键代码：

- `internal/handler/admin_auth.go`
- `internal/service/oidc/oidc.go`
- `internal/service/oidc/jwt.go`

---

## 3. RBAC 权限

Admin API 不再只依赖粗粒度角色，而是用 permission middleware 做细粒度控制。

### 内置权限

权限码定义在 `internal/handler/middleware/rbac.go`，覆盖 provider、api_key、virtual_key、user、tenant、project、service、usage、response、budget、audit、config 等资源。

示例：

| 权限 | 用途 |
| --- | --- |
| `provider:read/write` | 查看和修改 provider |
| `api_key:read/write` | 管理 API Key |
| `virtual_key:read/write` | 管理 Virtual Key |
| `service:read/write` | 管理 prompt service |
| `usage:read` | 查看 dashboard、usage、cache summary |
| `response:read` | 查看响应详情和 trace |

### 权限解析

```text
RequirePermission
  -> 优先查询 database-backed RBAC service
  -> 命中后写入进程内缓存和 Redis permission set
  -> 查询失败或未配置时 fallback 到内置角色权限表
```

关键代码：

- `internal/handler/middleware/rbac.go`
- `internal/service/rbac/rbac.go`
- `internal/repository/sqlstore/role.go`
- `internal/repository/db/migrations/025_rbac_tables.sql`

---

## 4. 限流

详见 [`limiter.md`](./limiter.md)。运行时入口是 `mw.GuardLLMRequest()`。

当前维度：

- 全局 QPS / RPM / TPM
- 租户 RPM / TPM
- 用户 QPS
- Provider RPM / TPM
- Model RPM / TPM

消费单位：

```text
prompt_tokens + max_output_tokens
```

Redis 可用时使用 Lua 脚本保证分布式原子扣减；Redis 不可用时降级到进程内令牌桶，并保持 fail-open。

---

## 5. 预算治理

预算链路：

```text
virtual_key -> api_key -> project -> tenant
```

策略：

| 策略 | 行为 |
| --- | --- |
| `hard_reject` | 预算耗尽时拒绝 |
| `soft_alert` | 放行但触发告警 |
| `grace` | 放行并继续记账 |

执行点：

1. 请求准入阶段：`mw.GuardLLMRequest()` 做预算预检查。
2. 响应完成后：`responses.Service` 根据真实 usage 和 provider price 计算成本并写入用量。

---

## 6. Provider 路由

Provider registry 会把 YAML 配置和运行时状态合并成可路由记录。

候选 provider 产生流程：

```text
租户 provider scope
  -> ProviderMgr.FilterRoutableByNames
  -> health / drain / enabled 过滤
  -> surface / stream / tools / images / structured output capability 过滤
  -> model alias / model scope 过滤
  -> router.OrderCandidates
```

路由能力：

| 策略 | 说明 |
| --- | --- |
| `round_robin` | 轮询 |
| `least_load` | 当前并发/负载优先 |
| `least_tpm` | token 吞吐低者优先 |
| `cost_based` | 单价低者优先 |
| `sticky` | 同 session 命中同一 provider |
| `ruleEngine` | 按模型、工具、图片、prompt token、正则等规则过滤 |
| `prefix_affinity` | 相同 prompt 前缀尽量打到同一 provider，放大上游 prefix/KV cache |

关键代码：

- `internal/service/provider/registry.go`
- `internal/service/provider/manager.go`
- `internal/service/router`

---

## 7. 缓存与 Token 优化

详见 [`cache.md`](./cache.md)。

当前缓存层是 `responses.Service` 内的 L1 exact-match response cache：

- backend：`auto` 自动选择 Redis，Redis 不可用时 fallback 到 memory；也可指定 `memory`
- key：tenant、model、surface、stream、bucket、canonical request payload
- singleflight：相同 cache key 并发 miss 只打一笔上游请求
- stream replay：流式响应会缓存 SSE 原始数据并回放
- cache hints：请求头可控制 skip、bucket、TTL

Prompt rewrite 会在 cache key 构建和上游请求前清理动态噪声：

- Anthropic system prompt 中动态 `cch` 值归一化
- Claude Code `# currentDate` 标记稳定化
- OpenAI Chat / Responses 缺省时注入稳定 `prompt_cache_key`

响应头会暴露 `X-Gateyes-Cache`、`X-Gateyes-Cache-Rewrites`、`X-Gateyes-Prompt-Cache-Key`，前端 Dashboard 通过 `GET /admin/v1/cache/summary` 展示命中率和延迟。

---

## 8. 监控与 Trace

### Prometheus

`GET /metrics` 暴露 Prometheus 指标。核心指标见 [`monitoring.md`](./monitoring.md)。

当前重要指标族：

- `gateway_llm_requests_total`
- `gateway_llm_request_duration_seconds`
- `gateway_llm_upstream_duration_seconds`
- `gateway_llm_time_to_first_token_seconds`
- `gateway_llm_stream_duration_seconds`
- `gateway_llm_tokens_total`
- `gateway_llm_prompt_cache_ratio`
- `gateway_cache_lookups_total`
- `gateway_cache_writes_total`
- `gateway_provider_prefix_cache_hit_rate_ratio`

### OTLP Tracing

`mw.OtelTrace()` 负责 W3C traceparent 提取和 span 创建，底层 exporter 在 `internal/pkg/trace` 初始化。

关键代码：

- `internal/handler/middleware/otel.go`
- `internal/pkg/trace`

---

## 9. 审计与异步落库

Admin 关键写操作会记录到 `audit_logs` 表：

- provider create/update/delete
- user / tenant / project / api key / virtual key 变更
- service 发布、回滚、订阅审核
- OIDC 登录回调和 logout

请求完成后的 usage、budget、audit、alert 等工作通过 `internal/pkg/eventbus` 异步执行，避免阻塞主请求路径；当事件队列满时会记录 metrics 并退化为 detached goroutine，保证计费数据不丢。

---

## 10. 维护建议

1. 生产数据库使用 PostgreSQL，SQLite 仅用于开发或单机试玩。
2. Redis 推荐部署，用于分布式限流、RBAC permission cache、L1 cache 和告警去重。
3. Provider API key 使用 `.env`、K8s Secret 或外部密钥管理，不写入仓库。
4. 配置热重载使用 `POST /admin/v1/reload`。
5. 健康探针使用 `/ready` 和 `/health`。
6. 回归测试使用 `go test ./...` 和 `npm --prefix web run build`。
