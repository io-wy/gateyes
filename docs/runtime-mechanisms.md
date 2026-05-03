# Runtime Mechanisms

本文档描述 Gateyes 当前实现中的六个核心机制：

- 鉴权
- 限流
- 路由
- 权限模型
- 预算治理
- 监控

> **注意**：L1 精确匹配缓存已于 2026-05-03 重新引入（`internal/service/cache`）。provider 上游的 prefix caching / `prompt_tokens_details.cached_tokens` 仍是更大幅度的缓存节省，但 gateway 层现在可以通过 Redis / 内存 LRU 避免重复上游调用。
> 
> 详见 [cache.md](./cache.md) 和 [affinity.md](./affinity.md)。

文档目标是帮助维护者理解"现在的代码实际上在做什么"。本文按当前实现编写，不代表未来目标设计，也不主动修正现有行为。

## 适用范围

- 程序入口：`cmd/gateway/main.go`
- HTTP 入口与路由分组：`internal/handler/server.go`
- 中间件：`internal/middleware`
- 协议兼容、provider adapter、provider registry metadata：`internal/service/provider`
- 主业务编排：`internal/service/responses`
- 核心服务：`internal/service/auth`、`internal/service/limiter`、`internal/service/router`、`internal/service/budget`
- 监控：`internal/handler/metrics.go`、`internal/middleware/otel.go`
- 持久化：`internal/repository`、`internal/repository/sqlstore`

## 请求主链路

当前 `/v1` 请求的主链路可以概括为：

1. Gin 路由接收请求。
2. `mw.Auth()` 解析 `Authorization` 并完成数据库鉴权（支持 Virtual Key 映射）。
3. 对 LLM 写请求，`mw.GuardLLMRequest()` 继续执行：
   - 读取请求体并提取 `model`
   - 估算 admission tokens（`prompt + output budget`）
   - 检查模型白名单
   - 检查 quota
   - 执行预算预检查（四级：virtual_key -> api_key -> project -> tenant）
   - 执行限流（全局 + 用户 + 租户 + 模型维度）
4. `handler` 负责：
   - 绑定 JSON
   - 调用 `internal/service/provider` 中的 compatibility helper 做 OpenAI / Anthropic 兼容转换
   - 返回 JSON 或 SSE
5. `responses.Service` 负责：
   - 查询 tenant 可用 provider
   - 按 provider registry 的 health/drain/capability 过滤 candidate providers
   - 排序 candidate providers 并执行 retry / fallback
   - 写入 `responses` 表中的 `in_progress` 记录
   - 调用上游 provider
   - 写回 `responses`
   - 写 usage（含多级预算扣减）

和本文六个机制最相关的入口文件：

- `internal/handler/server.go`
- `internal/service/provider`
- `internal/middleware/middleware.go`
- `internal/middleware/guard.go`
- `internal/service/responses/service.go`
- `internal/handler/metrics.go`

## 鉴权

### 鉴权输入

运行时和管理接口统一使用：

```text
Authorization: Bearer <api_key>:<api_secret>
```

另外 `mw.Auth()` 也支持：

```text
X-Api-Key: <api_key>:<api_secret>
```

这主要是为了兼容 Anthropic SDK 风格请求。

对应逻辑在：

- `internal/service/auth/auth.go`
- `internal/middleware/middleware.go`
- `internal/repository/sqlstore/identity.go`

### 鉴权算法

`mw.Auth()` 的处理流程如下：

1. 从请求头读取 `Authorization`
2. 调用 `auth.ExtractKey()` 按 `Bearer <key>:<secret>` 解析
3. 调用 `auth.Authenticate(ctx, key, secret)`
4. `Authenticate` 内部先尝试 API Key 认证，再 fallback 到 Virtual Key 认证
5. API Key 认证：通过 `store.Authenticate(ctx, key)` 按 `api_key` 查询数据库
6. Virtual Key 认证：通过 `store.AuthenticateVirtualKey(ctx, key)` 查询 VK，再加载父 API Key 身份
7. 取回 `AuthIdentity` 后检查三类状态是否都是 `active`
   - `api_keys.status`
   - `users.status`
   - `tenants.status`
8. 使用 `repository.VerifySecret()` 校验 `api_secret` 的 SHA-256 哈希
9. 成功后把 `AuthIdentity` 放入 Gin context

可以概括为：

```text
Authorization header
-> ExtractKey
-> try API Key auth
-> if not found, try Virtual Key auth
-> check api/user/tenant status
-> verify api_secret hash
-> attach identity to request context
```

### Virtual Key 映射

Virtual Key（VK）是 API Key 的受限子凭证，允许在不暴露主凭证的前提下授权第三方调用。

VK 认证流程在 `auth.authenticateVirtualKey()` 中：

1. 通过 `store.AuthenticateVirtualKey(ctx, key)` 查询 VK 记录
2. 校验 VK 的 `secret` 哈希
3. 加载父 API Key 的完整身份（`store.Authenticate(ctx, vk.APIKeyID)`）
4. 检查父身份的 api/user/tenant 三类状态是否 active
5. **叠加 VK 的限制到父身份**（overlay）：
   - `VirtualKeyID` -> 标记当前请求来自 VK
   - `VirtualKeyBudgetUSD` / `VirtualKeySpentUSD` / `VirtualKeyBudgetPolicy` -> VK 独立预算
   - `APIKeyRateLimitQPS` -> VK 独立限速（覆盖父 key 的 QPS）
   - `APIKeyModels` -> VK 允许的模型子集
   - `APIKeyProviders` -> VK 允许的 provider 子集
   - `CallbackURL` -> VK 回调地址
6. 返回叠加后的 `AuthIdentity`

叠加规则：VK 字段仅在非零/非空时覆盖父身份字段，否则继承父身份的值。

VK 的 CRUD 管理接口：

- `POST /admin/virtual-keys` -> 创建
- `GET /admin/virtual-keys` -> 列表
- `GET /admin/virtual-keys/:id` -> 查询
- `PUT /admin/virtual-keys/:id` -> 更新
- `DELETE /admin/virtual-keys/:id` -> 删除

VK 记录包含：`budget_usd`、`spent_usd`、`budget_policy`、`rate_limit_qps`、`allowed_models`、`allowed_providers`、`callback_url`、`expires_at`、`metadata` 等字段。

### 鉴权身份载荷

中间件写入 context 的身份结构是 `repository.AuthIdentity`，主要字段包括：

- `APIKeyID`
- `APIKey`
- `UserID`
- `UserName`
- `TenantID`
- `TenantSlug`
- `Role`
- `Quota`
- `Used`
- `QPS`
- `Models`

以及 project-aware 字段：

- `ProjectID`
- `ProjectSlug`
- `ProjectName`
- `ProjectStatus`
- `ProjectBudgetUSD` / `ProjectSpentUSD` / `ProjectBudgetPolicy`
- `APIKeyBudgetUSD` / `APIKeySpentUSD` / `APIKeyBudgetPolicy`
- `TenantBudgetUSD` / `TenantSpentUSD` / `TenantBudgetPolicy`

以及 Virtual Key 字段：

- `VirtualKeyID`
- `VirtualKeyBudgetUSD` / `VirtualKeySpentUSD` / `VirtualKeyBudgetPolicy`
- `CallbackURL`
- `APIKeyModels` / `APIKeyProviders` / `APIKeyServices` / `APIKeyRateLimitQPS`

这意味着后续 quota、模型白名单、角色校验、tenant 作用域计算、预算治理都依赖同一个身份对象完成。

### 模型白名单

模型白名单逻辑在 `auth.CheckModel()`，当前实现是**两层 AND 关系**：

| 层级 | 字段 | 数据来源 | 配置方式 |
|---|---|---|---|
| User 级别 | `identity.Models` | `user_models` 表 | 创建/更新 user 时传入 `models` |
| API Key 级别 | `identity.APIKeyModels` | `api_keys.allowed_models` | 创建/更新 api_key 时传入 `allowed_models` |

判定规则：

- 如果两层都为空，视为允许所有模型
- 如果 `identity.Models` 不为空，请求 `model` 必须在其列出的模型名中
- 如果 `identity.APIKeyModels` 不为空，请求 `model` 必须在其列出的模型名中
- 两层是 **AND** 关系：如果都非空，必须同时满足

这是"请求模型名"层面的校验，不是"最终选中的 provider/model"层面的校验。

#### Virtual Key 的模型白名单

VK 认证时，`virtual_keys.allowed_models` 会**覆盖**到 `identity.APIKeyModels` 上（overlay）。这意味着 VK 可以进一步收紧父 API Key 允许的模型范围，但不能放宽。

#### 多租户模型隔离的推荐做法

当前没有直接的 `tenants.allowed_models` 字段，tenant 级别的模型隔离通过**组合机制**实现：

1. **Provider 绑定隔离**（粗粒度）：
   - `POST /admin/tenants/:id/providers` 绑定 tenant 可用 provider
   - 路由时 `ListTenantProviders()` 只返回绑定的 provider
   - 未绑定的 provider 下的模型自然不可选

2. **User / API Key 白名单**（细粒度）：
   - 创建 user 时指定 `models: ["glm-5.1", "gpt-4o-mini"]`
   - 创建 api_key 时指定 `allowed_models: ["glm-5.1"]`
   - 两层 AND 关系实现精确控制

3. **Virtual Key 子集**（第三方授权场景）：
   - 创建 VK 时指定 `allowed_models`，在父 key 范围内进一步收紧

典型场景配置示例：

```
tenant-alpha 绑定 provider: [mock-primary]          -> 只能用 mock-primary 下的模型
  └─ user-alice  models: ["glm-5.1"]                -> alice 只能请求 glm-5.1
      └─ key-001  allowed_models: ["glm-5.1"]       -> key 层面再确认
      └─ vk-001   allowed_models: ["glm-5.1"]       -> 第三方授权也只能用 glm-5.1

tenant-beta 绑定 provider: [mock-secondary]         -> 只能用 mock-secondary 下的模型
  └─ user-bob    models: ["gpt-4o-mini"]            -> bob 只能请求 gpt-4o-mini
```

如果 tenant-alpha 的 user 请求 `gpt-4o-mini`：
- 模型白名单检查：`identity.Models` 中不包含 `gpt-4o-mini` → 403 `model_not_allowed`

### Provider 白名单

Provider 白名单逻辑在 `auth.CheckProvider()`：

- 如果 `identity.APIKeyProviders` 为空，视为允许所有 provider
- 如果不为空，只允许请求命中的 provider 在白名单中

这对 VK 场景特别有用：可以通过 `allowed_providers` 限制 VK 只能访问特定 provider。

### Quota 预检查与实际扣费

当前 quota 有两次参与：

1. 预检查：`mw.GuardLLMRequest()` 中调用 `auth.HasQuota(identity, estimatedAdmissionTokens)`
2. 实际记账：响应成功后 `auth.RecordUsage(...)` 调用 `store.ConsumeQuota(...)`

预检查用的是 admission tokens，真正入账用的是响应里的 `total_tokens`。

### 使用记录写入

`auth.RecordUsage()` / `auth.RecordBillableUsage()` 的行为：

1. 先更新 `api_keys.last_used_at`
2. 如果本次状态是 `success`，调用 `ConsumeQuota(userID, totalTokens)`
3. 成功后把 `identity.Used` 原地加上 `totalTokens`
4. 如果 `cost > 0`，按序扣减四级预算：
   - `ConsumeVirtualKeyBudget(virtualKeyID, cost)` — 仅在 VK 请求时
   - `ConsumeAPIKeyBudget(apiKeyID, cost)`
   - `ConsumeProjectBudget(projectID, cost)` — 仅在有 project 时
   - `ConsumeTenantBudget(tenantID, cost)`
5. 写入 `usage_records`

这里的 `identity.Used` 是本请求上下文中的内存视图，不是订阅式同步状态。

`RecordBillableUsage()` 和 `RecordUsage()` 的区别：`RecordBillableUsage()` 在 `totalTokens > 0` 时就扣减 quota，而 `RecordUsage()` 仅在 `status == "success"` 时扣减。

## 权限模型

### 角色定义

当前固定角色定义在 `internal/repository/interfaces.go`：

- `super_admin`
- `tenant_admin`
- `tenant_user`

没有动态角色表，也没有策略引擎。角色判断是固定字符串比较。

### 路由级权限分层

HTTP 路由分组在 `internal/handler/server.go`，当前分四层：

1. `/v1/*`
   - 统一走 `mw.Auth()`
2. `/v1` 下的写请求
   - 额外走 `mw.GuardLLMRequest()`
   - 覆盖 `POST /v1/responses`
   - 覆盖 `POST /v1/chat/completions`
   - 覆盖 `POST /v1/messages`
   - 覆盖 `POST /v1/embeddings`
3. `/service/:prefix/*`
   - 统一走 `mw.Auth()`
   - 用于 Service Catalog 服务路由
4. `/admin/*`
   - 统一走 `mw.Auth()`
   - 再走 `mw.RequireRoles(tenant_admin, super_admin)`
5. `/admin/tenants/*`
   - 在 admin 基础上再走 `mw.RequireRoles(super_admin)`

可以概括为：

```text
tenant_user   -> 只能访问 /v1/*
tenant_admin  -> 可访问 /v1/* + /admin/*
super_admin   -> 可访问 /v1/* + /admin/* + /admin/tenants/*
```

### 权限判断算法

当前角色判断逻辑非常直接：

```text
HasRole(role, allowed...) -> 线性遍历 allowed，存在完全相等项则通过
```

没有角色继承树，也没有 deny rule。

### Tenant 作用域

权限不是只有角色，还叠加 tenant 作用域。

当前 tenant 作用域策略：

- `tenant_admin` 和 `tenant_user` 默认只能看到自己的 `identity.TenantID`
- `super_admin` 在部分接口上可以传 `tenant_id` 查询或操作目标 tenant
- 对 tenant 管理接口，只有 `super_admin` 可访问

Admin Handler 里的两个辅助逻辑很关键：

- `scopeTenantID()`：读接口作用域
- `resolveTargetTenant()`：写接口作用域

当前规则：

- `super_admin` 创建用户时必须显式提供 `tenant_id`
- 非 `super_admin` 创建用户时自动落到自己的 tenant
- 非 `super_admin` 不允许创建 `super_admin`
- 非 `super_admin` 也不允许把用户更新成 `super_admin`

### 当前权限模型边界

当前权限模型是"固定角色 + tenant 作用域"：

- 优点是简单，链路清晰
- 缺点是表达能力有限

当前不支持：

- 自定义角色
- 资源级策略
- 字段级策略
- 审计规则
- 基于操作集合的策略编排

## 限流

### 限流入口

限流发生在 `mw.GuardLLMRequest()` 中，在真正调用 provider 之前执行。

当前调用顺序：

```text
1. limiter.Allow(ctx, identity.APIKey, EffectiveRateLimitQPS(identity), estimatedTokens)
   -> 全局 TPM/RPM + 用户 QPS
2. limiter.CheckTenant(identity.TenantID, estimatedTokens)
   -> 租户维度 TPM + RPM
3. limiter.CheckModel(meta.Model, estimatedTokens)
   -> 模型维度 TPM + RPM
```

也就是说，当前限流维度包括：

- 全局（TPM + RPM + 用户 QPS）
- 租户（TPM + RPM）
- 模型（TPM + RPM）

每个维度的限流独立判定，任一维度不通过即拒绝请求。

### 配置项

限流配置定义在 `LimiterConfig`（`internal/config/config.go`）：

**全局维度**：
- `globalQPS` — 全局默认 QPS
- `globalTPM` — 全局每分钟 token 上限
- `globalTokenBurst` — 全局 token 桶突发容量
- `globalRPM` — 全局每分钟请求上限
- `globalRPMBurst` — 全局 RPM 桶突发容量

**租户维度**：
- `tenantTPM` / `tenantTPMBurst` — 每租户每分钟 token 上限及突发容量
- `tenantRPM` / `tenantRPMBurst` — 每租户每分钟请求上限及突发容量

**Provider 维度**：
- `providerTPM` / `providerTPMBurst` — 每 provider 每分钟 token 上限及突发容量
- `providerRPM` / `providerRPMBurst` — 每 provider 每分钟请求上限及突发容量

**模型维度**：
- `modelTPM` / `modelTPMBurst` — 每 model 每分钟 token 上限及突发容量
- `modelRPM` / `modelRPMBurst` — 每 model 每分钟请求上限及突发容量

**通用**：
- `perUserRequestBurst` — 每用户请求突发容量
- `queueSize` — 队列大小

所有维度配置值为 0 时表示该维度限流禁用。

默认示例位于 `configs/config.yaml`。

### 请求元数据提取

限流前，中间件会先读取请求体并估算 admission tokens：

1. 读取整个 body
2. 反序列化为 `provider.ResponseRequest`
3. 调用 `Normalize()`
4. 调用 `EstimateAdmissionTokens()`

`EstimateAdmissionTokens()` 当前算法是：

```text
prompt_tokens = EstimatePromptTokens()
output_budget = max(max_output_tokens, max_tokens, 4096)
return prompt_tokens + output_budget
```

其中 `EstimatePromptTokens()` 仍然使用粗估算法：

```text
len(content) / 4
```

这是一个粗估算法，不是精确 tokenizer。

对 embeddings 请求（路径含 `/embeddings`），使用 `estimateEmbeddingTokens()` 单独估算。

### 限流结构

`Limiter` 由以下部分组成：

- 全局 token bucket（TPM）+ 全局 RPM bucket
- 用户维度 `map[apiKey]*userBucket`
- 租户维度 `bucketMap`（TPM + RPM）
- Provider 维度 `bucketMap`（TPM + RPM）
- 模型维度 `bucketMap`（TPM + RPM）
- 一个单消费者队列 `chan *Request`

构造时会启动两个 goroutine：

- `refillLoop()`
- `consumeLoop()`

### Redis 分布式限流

当配置了 `redis.addr` 时，Limiter 会启用 Redis 分布式限流模式。通过 `SetRedis(rdb)` 注入 Redis 客户端。

**Redis 模式 vs 内存模式**：

| 维度 | Redis 模式 | 内存模式 |
|------|-----------|---------|
| 全局 | Lua 脚本原子操作 | in-memory TokenBucket |
| 用户 | in-memory（VK QPS 是本进程级） | in-memory |
| 租户 | Lua 脚本 | in-memory bucketMap |
| Provider | Lua 脚本 | in-memory bucketMap |
| 模型 | Lua 脚本 | in-memory bucketMap |

**Lua Token Bucket 脚本**（`internal/service/limiter/redis.go`）：

使用 Redis Hash 存储桶状态（`t`=当前令牌数，`l`=上次填充时间），脚本逻辑：

```text
1. HGET key 获取当前 tokens 和 last_fill
2. 计算时间差 elapsed = now - last_fill（毫秒）
3. tokens = min(burst, tokens + rate * elapsed / 1000)
4. 如果 tokens >= consume，则扣减并返回 1（允许）
5. 否则返回 0（拒绝）
6. HSET 更新状态，EXPIRE 设置 120 秒 TTL
```

**Fail-Open 容错**：

```go
result, err := tokenBucketLua.Run(ctx, rdb, keys, args).Int()
if err != nil {
    return true // Redis 故障时放行，避免级联宕机
}
```

Redis 不可用时自动降级为放行，保证请求不被限流服务阻塞。

**Key 命名规则**：

```text
gateyes:rl:g:t          -> 全局 TPM
gateyes:rl:g:r          -> 全局 RPM
gateyes:rl:ten:<id>:t   -> 租户 TPM
gateyes:rl:ten:<id>:r   -> 租户 RPM
gateyes:rl:prov:<name>:t -> Provider TPM
gateyes:rl:prov:<name>:r -> Provider RPM
gateyes:rl:mod:<name>:t  -> 模型 TPM
gateyes:rl:mod:<name>:r  -> 模型 RPM
```

### Token Bucket 算法

`TokenBucket.TryConsume(n)` 当前算法：

1. 计算从 `lastFill` 到现在经过了多少秒
2. `tokens += elapsed_seconds * rate`
3. 如果超过 `burst`，截断到 `burst`
4. 如果 `tokens >= n`，则扣减并返回 `true`
5. 否则返回 `false`

可以写成：

```text
tokens = min(burst, tokens + elapsed * rate)
if tokens >= n:
    tokens -= n
    allow
else:
    deny
```

### 多层限流逻辑

当前 `GuardLLMRequest()` 中限流检查的执行顺序：

1. `limiter.Allow()` — 全局 TPM/RPM + 用户 QPS
2. `limiter.CheckTenant()` — 租户维度 TPM + RPM
3. `limiter.CheckModel()` — 模型维度 TPM + RPM

每层独立判定，任一层不通过则返回 `429 Too Many Requests`。

用户 QPS 的生效逻辑：

- 如果 VK 设置了 `rate_limit_qps`，优先使用 VK 的值
- 如果 API Key 设置了 `qps`，使用 key 的值
- 否则使用全局默认 `globalQPS`

由 `auth.EffectiveRateLimitQPS(identity)` 统一计算。

### 队列行为

`Allow()` 不是直接调用 `check()`，而是先把请求塞入队列。

流程是：

1. 把 `Request` 发送到 `queue`
2. 单消费者 `consumeLoop()` 取出请求并执行 `check()`
3. 把结果写回 `req.Result`
4. 调用方同步等待结果或等待 `ctx.Done()`

所以 `queueSize` 当前代表：

- 限流判定前的缓冲长度
- 不是等待令牌的长期排队系统

### 当前限流边界

当前限流实现有几个重要边界：

- Provider 维度限流（`CheckProvider`）已实现但在 Guard 中间件中未调用，可由业务层自行使用
- 没有按真实 completion token 限流
- 每个请求只做一次准入判定，不做后续补扣
- 多实例部署时用户维度限流仍然是本进程内存级（非分布式）

## 预算治理

### 概述

预算治理是多层成本控制机制，在请求准入阶段和响应完成阶段分两步执行。

预算层级从细到粗：

```text
virtual_key -> api_key -> project -> tenant
```

每一级独立配置预算上限（`budget_usd`）和耗尽策略（`budget_policy`）。

### 预算策略

定义在 `repository.BudgetPolicy*` 常量：

| 策略 | 常量 | 行为 |
|------|------|------|
| 硬拒绝 | `hard_reject` | 预算耗尽时直接拒绝请求 |
| 软告警 | `soft_alert` | 预算耗尽时仍放行，但触发告警通知 |
| 宽限期 | `grace` | 预算耗尽时仍放行，超支部分在后续记账中记录 |

默认策略为 `hard_reject`。

### 预算预检查

预检查发生在 `mw.GuardLLMRequest()` 中，在限流之前执行。

`budget.Service.Check()` 按序检查四级预算：

```text
1. CheckVirtualKeyBudget(virtualKeyID, estimatedCost)  -- 仅 VK 请求
2. CheckAPIKeyBudget(apiKeyID, estimatedCost)
3. CheckProjectBudget(projectID, estimatedCost)         -- 仅有 project 时
4. CheckTenantBudget(tenantID, estimatedCost)
```

每一级检查返回 `BudgetCheckResult`：

- `Allowed` — 是否放行
- `Policy` — 匹配的策略
- `Remaining` — 剩余预算

判定规则：

- 任一级 `hard_reject` 且超预算 -> 立即返回 429
- 任一级 `soft_alert` 且超预算 -> 标记 `AlertSent`，放行请求
- 任一级 `grace` 且超预算 -> 放行请求
- 空策略按 `hard_reject` 处理

预检查的 `estimatedCost` 当前传入 0（不基于预估成本拒绝），主要依赖已耗尽状态判定。后续可接入基于 token 单价的实时成本预估。

### 预算扣减

响应成功后，`auth.RecordUsage()` 在 `cost > 0` 时按序扣减四级预算：

```text
1. ConsumeVirtualKeyBudget(virtualKeyID, cost)  -- 仅 VK 请求
2. ConsumeAPIKeyBudget(apiKeyID, cost)
3. ConsumeProjectBudget(projectID, cost)         -- 仅有 project 时
4. ConsumeTenantBudget(tenantID, cost)
```

`cost` 由上游 provider 返回的 `usage` 字段计算：

```text
cost = prompt_tokens * price_input + completion_tokens * price_output
```

价格来自 `ProviderRegistryRecord.RuntimeConfig.PriceInput` / `PriceOutput`。

任一级扣减返回 `false`（超预算）会返回 `ErrBudgetExceeded`，但此时请求已经完成，主要用于标记状态。

### 预算状态查询

管理接口：

- `GET /admin/budgets` -> 查看多级预算状态
- `GET /admin/usage/summary` -> 使用量汇总
- `GET /admin/usage/breakdown` -> 按 provider/model/user 维度分解
- `GET /admin/usage/trend` -> 使用趋势

### 告警联动

当 `soft_alert` 策略触发时，`GuardLLMRequest` 会调用：

```text
alertSvc.NotifyBudgetExhausted(BudgetExhausted{
    TenantID, ProjectID, APIKeyID, Model, BudgetScope
})
```

告警服务支持多种渠道（webhook、slack），通过 `alert.channels` 配置，并使用 Redis 做告警去重（`dedupWindowSeconds`，默认 300 秒）。

### 审计日志

预算相关操作会写入审计日志（`audit_logs` 表），通过 `AuditLogStore.CreateAuditLog()` 记录。

审计日志查询接口：

- `GET /admin/audit` -> 列出审计日志，支持按 action、resource_type、时间范围过滤

## 路由

### 路由器职责

`internal/service/router` 的职责是"对候选 provider 集合做分流、排序和选择"，不负责鉴权，也不负责上游协议转换。

候选 provider 不是全局 provider 列表，而是：

1. 先从数据库读取 tenant 可用 provider 名单
2. 再通过 `ProviderMgr.FilterRoutableByNames()` 得到候选列表（含健康检查和能力过滤）
3. `responses.Service.buildRouteContext(...)` 提取输入特征
4. `router.OrderCandidates(...)` 执行分流和排序

所以当前路由的第一层过滤是 tenant 绑定关系。

### Provider 健康检查

`HealthChecker`（`internal/service/provider/healthcheck.go`）负责定期探测 provider 健康状态。

**三态健康模型**：

| 状态 | 常量 | 含义 |
|------|------|------|
| 健康 | `healthy` | 正常服务 |
| 降级 | `degraded` | 单次探测失败，仍可接受流量 |
| 不健康 | `unhealthy` | 连续失败次数 >= `failureThreshold`，路由过滤时排除 |

**探测机制**：

- 配置 `healthCheck.enabled=true` 时启动后台 goroutine
- 按 `healthCheck.intervalSeconds`（默认 60s）周期性执行
- 对每个 provider 发送一个最小化请求（`max_output_tokens=8`）
- 探测超时由 `healthCheck.timeoutSeconds`（默认 15s）控制

**故障判定**：

```text
连续失败次数 < failureThreshold -> degraded
连续失败次数 >= failureThreshold -> unhealthy
探测成功 -> 重置连续失败计数，状态恢复 healthy
```

**状态变更联动**：

- 状态变更时持久化到 `provider_registry` 表
- 内存中同步更新 `Manager.registry` 和 `ProviderStats.Status`
- 触发 `alert.NotifyProviderStateChanged()` 告警

**路由过滤**：

`registryAllowsRequest()` 在候选过滤时执行：

- `enabled=false` 或 `drain=true` -> 排除
- `health_status=unhealthy` -> 排除
- `health_status=healthy` 或 `degraded` -> 允许

### 能力感知过滤

`ProviderRegistryRecord` 维护每个 provider 的能力标记：

- `supports_chat` — 支持 `/v1/chat/completions` surface
- `supports_responses` — 支持 `/v1/responses` surface
- `supports_messages` — 支持 `/v1/messages` surface（Anthropic）
- `supports_stream` — 支持流式响应
- `supports_tools` — 支持工具调用
- `supports_images` — 支持图片输入
- `supports_structured_output` — 支持结构化输出
- `supports_long_context` — 支持长上下文（`max_tokens >= 32000`）
- `supports_embeddings` — 支持 embedding 请求

`registryAllowsRequest()` 在路由过滤时根据请求特征匹配能力：

```text
1. 请求 surface=chat -> 要求 supports_chat=true
2. 请求 surface=responses -> 要求 supports_responses=true
3. 请求 surface=messages -> 要求 supports_messages=true
4. 请求 stream=true -> 要求 supports_stream=true
5. 请求含 tools -> 要求 supports_tools=true
6. 请求含 images -> 要求 supports_images=true
7. 请求含 structured_output -> 要求 supports_structured_output=true
```

能力标记在 provider 注册时从配置自动推断，也可通过 admin API 更新。

### 路由权重

`ProviderRegistryRecord.RoutingWeight` 用于候选排序。`FilterRoutableByNames()` 返回的候选列表按 `routing_weight` 降序排列。权重默认为 1。

### 当前选择流程

当前主链路的路由现在是四段式：

1. `ListTenantProviders(identity.TenantID)`
2. `ProviderMgr.FilterRoutableByNames(providerNames, req)` — 含健康/能力/权重过滤
3. `responses.Service.buildRouteContext(...)` 提取：
   - `model`
   - `sessionID`
   - `inputText`
   - `promptTokens`
   - `stream`
   - `hasTools`
   - `hasImages`
   - `hasStructuredOutput`
4. `router.OrderCandidates(...)` 依次执行：
   - `ruleEngine`（分流过滤）
   - `ranker`（重排序）
   - `affinity`（软固定）
   - `strategy`（最终选择）
5. 主业务层自行按排序后的列表重试 / fallback
6. 请求成功后调用 `router.PromoteAffinity(...)` 更新 affinity 状态

因此，当前 router 仍然不负责业务重试，但它已经成为"候选集过滤 + 排序 + 亲和固定"的统一入口。

### 负载跟踪

路由器内部维护了一个 `loads map[string]int64`。

主业务在调用 provider 前后会分别执行：

- `router.IncLoad(providerName)`
- `router.DecLoad(providerName)`

这让 `least_load` 策略可以基于当前内存中的并发计数工作。

### 路由四层

#### 1. ruleEngine

`ruleEngine` 是分流辅助层，语义类似 Clash 规则：

- 按顺序匹配，`first match wins`
- 命中规则后，把候选 provider 收缩到 `action.providers`
- 如果规则命中但与当前 tenant 候选集没有交集，则回退到原候选集，不直接打空

当前支持的匹配条件：

- `models`
- `minPromptTokens`
- `maxPromptTokens`
- `hasTools`
- `hasImages`
- `hasStructuredOutput`
- `stream`
- `anyRegex`

这层是"候选集过滤"，不是最终选择器。

#### 2. ranker

`ranker` 是独立排序入口。

当前状态：

- `ranker.enabled=false`：默认关闭
- `ranker.method=ml_rank`：已经预留配置和代码入口，但当前只返回原顺序

也就是说，`ml_rank` 目前只是显式 `TODO`，没有真正引入 `LightGBM` / `BERT` 推断。

#### 3. affinity

`affinity` 是软固定层，位于 ranker 之后、strategy 之前。它不改变候选集大小，只把"偏好 provider"移到队首。

支持的亲和类型：

- **SessionAffinity**：按 `sessionID` 做加权一致哈希，同一 session 固定到同一 provider。配置 `router.affinity.sessionTTL` 控制记忆时长。
- **PrefixAffinity**：按请求 prompt 的前 `prefixDepth` 个 rune 做哈希，同一前缀固定到同一 provider。用于提升后端 prefix-cache 命中率（如 vLLM）。配置 `router.affinity.prefixTTL` 和 `router.affinity.prefixDepth`。

 affinity 层与 strategy 是解耦的：
- affinity 先把偏好 provider pin 到队首
- strategy 再对剩余候选做最终排序
- 请求成功后，`PromoteAffinity()` 更新记忆状态

旧版 `sticky` 策略已迁移为 SessionAffinity，保留配置兼容。

#### 4. strategy

`strategy` 是最终排序/选择策略。

### 五种 strategy

#### 1. `round_robin`

算法：

```text
start = index % len(candidates)
ordered = candidates[start:] + candidates[:start]
index = (index + 1) % len(candidates)
```

特点：

- 简单
- 全局共享一个递增索引
- 返回的是"轮转后的候选顺序"，不是直接选单个 provider

#### 2. `random`

算法：

```text
ordered = shuffle(candidates)
```

特点：

- 随机打乱候选顺序
- 不看成本，不看当前负载

#### 3. `least_load`

算法：

```text
ordered = sortBy(loads[name] asc)
```

特点：

- 基于本进程内存中的实时负载
- 不依赖数据库统计

#### 4. `cost_based`

算法：

```text
ordered = sortBy(UnitCost asc)
```

特点：

- 完全不看负载
- 完全不看延迟
- 只看配置中的价格字段

#### 5. `sticky`（已迁移到 affinity 层）

`sticky` 策略的行为已迁移到 `SessionAffinity`，保留配置字符串仅用于向后兼容。

当 `router.strategy="sticky"` 时，路由器自动启用 `SessionAffinity`，实际执行流程：

1. affinity 层按 `sessionID` 做加权一致哈希，把命中 provider pin 到队首
2. strategy 层直接返回 affinity 已排好的顺序

旧算法（31 进制字符哈希 + 轮转）已由 `SessionAffinity.Pin` 中的 FNV-1a 加权一致哈希替代。
- 如果 `sessionID` 为空，会回退到 round-robin 风格的轮转顺序

## 监控

### 暴露方式

Prometheus 指标通过：

```text
GET /metrics
```

对外暴露。

运行时链路是：

```text
cmd/gateway/main.go
-> handler.NewMetricsFromConfig(cfg.Metrics)
-> internal/handler/server.go 注册 /metrics
-> internal/handler/metrics.go
-> promhttp.Handler()
```

当前 `metrics.enabled=false` 时，路由仍会注册，但 handler 会返回 `404`。

### OTLP Tracing

系统已集成 OpenTelemetry tracing，通过 `internal/middleware/otel.go` 和 `cmd/gateway/main.go` 中的 `initTracer()` 实现。

**配置**（`TracingConfig`）：

```yaml
tracing:
  enabled: true
  exporter: otlp       # "otlp" 或 "stdout"
  endpoint: http://localhost:4318  # OTLP HTTP endpoint
```

**启动流程**：

1. 根据 `exporter` 创建 `SpanExporter`：
   - `otlp` -> `otlptracehttp`（支持 `endpoint` 配置）
   - 默认 -> `stdouttrace`（调试用）
2. 创建 `TracerProvider`，服务名 `gateyes-gateway`
3. 设置全局 `TextMapPropagator`（`TraceContext` + `Baggage`）
4. 优雅关闭时 `Shutdown()` 确保所有 span 刷出

**中间件集成**：

`OtelTrace()` Gin 中间件在每个请求上：

1. 从请求头提取 `traceparent` / `tracestate`（支持 W3C 传播）
2. 创建 span：`<METHOD> <route_pattern>`
3. 设置 HTTP 属性：`http.method`、`http.path`、`http.route`、`http.host` 等
4. 请求完成后记录 `http.status_code`
5. 5xx 状态码标记为 `Error`

**业务层 span**：

`middleware.StartSpan()` 和 `middleware.SpanFromContext()` 供业务层创建子 span。

**与日志的关联**：

`cmd/gateway/main.go` 使用 `middleware.NewTraceHandler` 包装 slog，自动在每条日志中注入 trace_id 和 span_id，实现日志与 trace 的关联。

### 主指标口径

当前已经统一成 `surface + provider + result` 三类核心维度。

主指标包括：

- `gateway_llm_requests_total{surface,result,provider}`
- `gateway_llm_request_duration_seconds{surface,provider,result}`
- `gateway_llm_upstream_duration_seconds{surface,provider,result}`
- `gateway_llm_time_to_first_token_seconds{surface,provider}`
- `gateway_llm_stream_duration_seconds{surface,provider,result}`
- `gateway_llm_tokens_total{provider,token_type}`
- `gateway_llm_errors_total{surface,provider,error_class}`
- `gateway_llm_retries_total{provider}`
- `gateway_llm_fallbacks_total{provider}`
- `gateway_provider_requests_total{provider,result}`
- `gateway_provider_circuit_state{tenant_id,provider}`

### Label 语义

#### `surface`

固定值：

- `responses`
- `chat_completions`
- `messages`
- `embeddings`
- `models`
- `admin`

主 LLM 写请求稳定落在前三个值。

#### `provider`

表示最终命中的 provider 名。

如果错误发生在 middleware 或 handler 早期、尚未选到 provider，则为：

```text
provider="none"
```

#### `result`

当前统一枚举：

- `success`
- `client_error`
- `auth_error`
- `rate_limited`
- `timeout`
- `upstream_error`
- `internal_error`

#### `error_class`

用于更细粒度错误归类，例如：

- `invalid_api_key`
- `inactive_api_key`
- `forbidden`
- `invalid_request`
- `model_not_allowed`
- `quota_exceeded`
- `rate_limited`
- `budget_exceeded`
- `budget_check_error`
- `tenant_rate_limited`
- `model_rate_limited`
- `upstream_4xx`
- `upstream_5xx`
- `timeout`
- `no_provider`
- `internal_error`

### 埋点位置

#### Middleware

`internal/middleware` 现在会把前置拦截也记入主 metrics：

- `Auth()`：
  - `invalid_api_key`
  - `inactive_api_key`
- `RequireRoles()`：
  - `forbidden`
- `GuardLLMRequest()`：
  - `invalid_request`
  - `model_not_allowed`
  - `quota_exceeded`
  - `budget_exceeded`
  - `budget_check_error`
  - `rate_limited`
  - `tenant_rate_limited`
  - `model_rate_limited`

#### Handler

成功路径主要在 `observeResponse()` / `observeResponseWithUpstream()`：

- 计请求成功数
- 记 request / upstream duration
- 记 token 计数
- 记 retry / fallback
- 记 provider request

#### Streaming

流式 handler 会单独记录：

- `gateway_llm_active_streams`
- `gateway_llm_time_to_first_token_seconds`
- `gateway_llm_stream_duration_seconds`

其中：

- TTFT 在首个可见文本事件到达时记录
- stream duration 在流正常结束或带错误结束时记录

### ProviderStats 和 Prometheus 的边界

`internal/service/provider/stats.go` 的 `ProviderStats` 仍然保留，用于：

- `/admin/providers`
- `/admin/providers/:name/stats`

它不是 Prometheus 主口径，而是操作面展示口径。

可以理解为：

- Prometheus：监控、告警、Grafana
- ProviderStats：后台页面和人工运维查看

### 审计日志

审计日志记录管理操作（CRUD、配置变更、权限操作等），存储在 `audit_logs` 表。

审计日志字段：`tenant_id`、`actor_user_id`、`actor_api_key_id`、`actor_role`、`action`、`resource_type`、`resource_id`、`request_id`、`ip_address`、`payload`。

查询接口：`GET /admin/audit`

### 当前边界

当前监控仍然有几个已知边界：

- OTLP tracing 已实现，但 span 覆盖范围主要集中在 HTTP 入口层，业务层子 span 需逐步补充
- 已补 `X-Request-ID` + `traceparent` 响应头与应用日志关联，构成基础 tracing 体系
- `provider_circuit_state` 只有在显式同步时才会更新，不是后台定时采集
- 仓库内已提供 Prometheus / Grafana 基线资产，但阈值仍需按生产环境调优

## 维护建议

如果后续继续演进这六个机制，建议优先保持两个原则：

1. 先区分"当前行为文档"和"目标设计文档"
2. 涉及限流、路由、预算时，先明确维度，再谈实现

推荐的维度思考顺序：

- 谁是作用域主体：tenant、user、api key、virtual key、provider
- 限的是请求数、prompt token、completion token，还是总 token
- 路由依据是成本、负载、模型能力、健康状态，还是 session 粘性
- 权限依据是固定角色，还是策略系统
- 预算策略是硬拒绝、软告警，还是宽限期
- 监控覆盖是 metrics、traces、还是 audit logs

当前这份文档的用途是帮助你在改动前先看清楚"代码现在到底怎么跑"。后续如果你需要，我可以再把这份文档拆成：

- 面向维护者的内部机制文档
- 面向 API 使用者的运行时说明
