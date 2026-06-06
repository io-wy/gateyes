# Runtime Mechanisms

Gateyes 六个核心机制：鉴权、权限、限流、预算、路由、监控。

---

## 鉴权

### 输入格式

```text
Authorization: Bearer <api_key>:<api_secret>
```

兼容 `X-Api-Key: <api_key>:<api_secret>`（Anthropic SDK 风格）。

### 流程

```text
Authorization header
    -> ExtractKey (按 ":" 分割 key 和 secret)
    -> try API Key auth (store.Authenticate)
    -> if not found, try Virtual Key auth (store.AuthenticateVirtualKey)
    -> check api_keys.status + users.status + tenants.status == active
    -> verify api_secret SHA-256 hash
    -> AuthIdentity 放入 Gin context
```

- Virtual Key 认证成功后加载父 API Key 身份
- 任一级状态非 `active` 即拒绝

**关键代码**：
- `internal/service/auth/auth.go` — 认证逻辑
- `internal/middleware/middleware.go` — `mw.Auth()`
- `internal/repository/sqlstore/identity.go` — 身份查询

---

## 权限模型

### 角色

固定三种角色（`internal/repository/interfaces.go`）：

| 角色 | 权限 |
|------|------|
| `super_admin` | 全部接口 |
| `tenant_admin` | `/v1/*` + `/admin/*` |
| `tenant_user` | `/v1/*` |

### 路由分层

```text
/v1/*           → mw.Auth()
  POST 写请求   → + mw.GuardLLMRequest() (模型白名单 + 配额 + 预算 + 限流)
/service/*      → mw.Auth()
/admin/*        → mw.Auth() + RequireRoles(tenant_admin, super_admin)
/admin/tenants/* → + RequireRoles(super_admin)
```

---

## 限流

详见 [`limiter.md`](./limiter.md)。概要：

- **多维度令牌桶**：全局 TPM/RPM、用户 QPS、租户 TPM/RPM、Provider TPM/RPM、Model TPM/RPM
- **双后端**：Redis Lua 原子脚本（分布式）/ 内存桶（降级）
- **Fail-open**：Redis 故障时自动放行
- **消费单位**：`prompt_tokens + max_output_tokens`

---

## 预算治理

### 预算层级

```text
virtual_key -> api_key -> project -> tenant
```

每级独立配置 `budget_usd` + `budget_policy`。

### 策略

| 策略 | 行为 |
|------|------|
| `hard_reject` | 耗尽即拒（默认） |
| `soft_alert` | 放行但告警 |
| `grace` | 放行，超支记账 |

### 执行时机

1. **预检查**（`mw.GuardLLMRequest()`，请求准入阶段）：
   - 取 `virtual_key` → `api_key` → `project` → `tenant` 四级预算
   - 任一级 `hard_reject` + 已耗尽 → 直接拒绝
   - 记录 budget alert（软告警/宽限期）

2. **扣减**（`responses.Service` 响应完成后）：
   - 根据 `responses.usage` 实际 token 数
   - 按四级预算链从细到粗扣减
   - `cost_usd = prompt_tokens * price_input + completion_tokens * price_output`

---

## 路由

### 职责

`internal/service/router`：对候选 provider 集合做分流、排序和选择。不负责鉴权和协议转换。

### 候选 provider 产生流程

```text
1. 查询 tenant 可用 provider 名单
2. ProviderMgr.FilterRoutableByNames() — 健康/能力/drain 过滤
3. buildRouteContext() — 提取输入特征（model, promptTokens, stream, hasTools, hasImages）
4. router.OrderCandidates() — 分流 + 排序
```

### 健康检查

三态模型（`internal/service/provider/healthcheck.go`）：

| 状态 | 含义 |
|------|------|
| `healthy` | 正常 |
| `degraded` | 单次失败，仍可接受流量 |
| `unhealthy` | 连续失败 >= threshold，路由排除 |

### 路由策略

| 策略 | 说明 |
|------|------|
| `round_robin` | 轮询 |
| `least_load` | 实时并发最小负载 |
| `cost_based` | 价格优先 |
| `sticky` | 同 session 命中同一 provider |
| `ruleEngine` | 按特征分流 |
| `prefix_affinity` | 同 prompt 前缀命中同一 provider |

配置：`router.strategy` + `router.ranker.enabled`。

### 路由插件

支持 gRPC RouterPlugin（`OrderCandidates`）自定义排序。详见 [`plugin-development.md`](./plugin-development.md)。

---

## 监控

### Prometheus 指标

通过 `GET /metrics` 暴露。14 个核心指标：

| 指标 | 说明 |
|------|------|
| `gateway_requests_total` | 请求总数（按 surface/provider/status） |
| `gateway_request_duration_seconds` | 请求延迟 |
| `gateway_upstream_duration_seconds` | 上游延迟 |
| `gateway_ttft_seconds` | 流式首 token 延迟 |
| `gateway_stream_duration_seconds` | 流式总时长 |
| `gateway_tokens_total` | token 消耗 |
| `gateway_errors_total` | 错误数 |
| `gateway_retries_total` | 重试数 |
| `gateway_fallback_total` | fallback 数 |
| `gateway_circuit_breaker_state` | 熔断器状态 |

配置：
```yaml
metrics:
  enabled: true
  namespace: gateway
```

### OTLP Tracing

W3C traceparent 传播，HTTP exporter。

```yaml
tracing:
  enabled: true
  endpoint: "http://jaeger:4318"
  serviceName: "gateyes"
```

**关键代码**：`internal/middleware/otel.go`

### 审计日志

admin 关键写操作（创建/删除 tenant/user/provider/api_key 等）全记录到 `audit_logs` 表。

---

## 维护建议

1. **生产数据库**：PostgreSQL，SQLite 仅开发使用
2. **Redis**：推荐部署，分布式限流和告警去重依赖；不部署时自动降级内存模式
3. **密钥管理**：Provider API key 不放仓库，用 K8s Secret 或外部密钥管理器
4. **配置热重载**：`POST /admin/reload` 无需重启
5. **健康探针**：`/ready`（readiness）、`/health`（liveness）
6. **测试回归**：`go test ./...`
