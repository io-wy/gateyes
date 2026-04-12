# Runtime Mechanisms

本文档描述 Gateyes 当前实现中的五个核心机制：

- 鉴权
- 限流
- 路由
- 权限模型
- 监控

> **注意**：应用层缓存已在 2026-04-01 移除。provider 上游的 prefix caching / `prompt_tokens_details.cached_tokens` 才是真正的缓存节省，gateway 层无法控制。真正的缓存节省来自 provider 侧。

文档目标是帮助维护者理解“现在的代码实际上在做什么”。本文按当前实现编写，不代表未来目标设计，也不主动修正现有行为。

## 适用范围

- 程序入口：`cmd/gateway/main.go`
- HTTP 入口与路由分组：`internal/handler/server.go`
- 中间件：`internal/middleware`
- 协议兼容层：`internal/protocol/apicompat`
- 主业务编排：`internal/service/responses`
- 核心服务：`internal/service/auth`、`internal/service/limiter`、`internal/service/router`
- 监控：`internal/handler/metrics.go`
- 持久化：`internal/repository`、`internal/repository/sqlstore`

## 请求主链路

当前 `/v1` 请求的主链路可以概括为：

1. Gin 路由接收请求。
2. `mw.Auth()` 解析 `Authorization` 并完成数据库鉴权。
3. 对 LLM 写请求，`mw.GuardLLMRequest()` 继续执行：
   - 读取请求体并提取 `model`
   - 估算 admission tokens（`prompt + output budget`）
   - 检查模型白名单
   - 检查 quota
   - 执行限流
4. `handler` 负责：
   - 绑定 JSON
   - 调用 `internal/protocol/apicompat` 做 OpenAI / Anthropic 兼容转换
   - 返回 JSON 或 SSE
5. `responses.Service` 负责：
   - 查询 tenant 可用 provider
   - 排序 candidate providers 并执行 retry / fallback
   - 写入 `responses` 表中的 `in_progress` 记录
   - 调用上游 provider
   - 写回 `responses`
   - 写 usage

和本文五个机制最相关的入口文件：

- `internal/handler/server.go`
- `internal/protocol/apicompat`
- `internal/middleware/middleware.go`
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
4. `Authenticate` 内部先通过 `store.Authenticate(ctx, key)` 仅按 `api_key` 查询数据库
5. 取回 `AuthIdentity` 后检查三类状态是否都是 `active`
   - `api_keys.status`
   - `users.status`
   - `tenants.status`
6. 使用 `repository.VerifySecret()` 校验 `api_secret` 的 SHA-256 哈希
7. 成功后把 `AuthIdentity` 放入 Gin context

可以概括为：

```text
Authorization header
-> ExtractKey
-> load identity by api_key
-> check api/user/tenant status
-> verify api_secret hash
-> attach identity to request context
```

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

这意味着后续 quota、模型白名单、角色校验、tenant 作用域计算都依赖同一个身份对象完成。

### 模型白名单

模型白名单逻辑在 `auth.CheckModel()`：

- 如果 `identity.Models` 为空，视为允许所有模型
- 如果不为空，只允许请求里的 `model` 精确匹配白名单条目

这是“请求模型名”层面的校验，不是“最终选中的 provider/model”层面的校验。

### Quota 预检查与实际扣费

当前 quota 有两次参与：

1. 预检查：`mw.GuardLLMRequest()` 中调用 `auth.HasQuota(identity, estimatedAdmissionTokens)`
2. 实际记账：响应成功后 `auth.RecordUsage(...)` 调用 `store.ConsumeQuota(...)`

预检查用的是 admission tokens，真正入账用的是响应里的 `total_tokens`。

### 使用记录写入

`auth.RecordUsage()` 的行为：

1. 先更新 `api_keys.last_used_at`
2. 如果本次状态是 `success`，调用 `ConsumeQuota(userID, totalTokens)`
3. 成功后把 `identity.Used` 原地加上 `totalTokens`
4. 写入 `usage_records`

这里的 `identity.Used` 是本请求上下文中的内存视图，不是订阅式同步状态。

## 权限模型

### 角色定义

当前固定角色定义在 `internal/repository/interfaces.go`：

- `super_admin`
- `tenant_admin`
- `tenant_user`

没有动态角色表，也没有策略引擎。角色判断是固定字符串比较。

### 路由级权限分层

HTTP 路由分组在 `internal/handler/server.go`，当前分三层：

1. `/v1/*`
   - 统一走 `mw.Auth()`
2. `/v1` 下的写请求
   - 额外走 `mw.GuardLLMRequest()`
   - 覆盖 `POST /v1/responses`
   - 覆盖 `POST /v1/chat/completions`
   - 覆盖 `POST /v1/messages`
3. `/admin/*`
   - 统一走 `mw.Auth()`
   - 再走 `mw.RequireRoles(tenant_admin, super_admin)`
4. `/admin/tenants/*`
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

当前权限模型是“固定角色 + tenant 作用域”：

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

当前调用方式：

```text
limiter.Allow(ctx, identity.APIKey, identity.QPS, estimatedAdmissionTokens)
```

也就是说，当前限流维度仍然基于 `APIKey`，但每个 key 的请求速率会优先使用用户自己的 `identity.QPS`。

### 配置项

限流配置定义在 `LimiterConfig`：

- `globalQPS`
- `globalTPM`
- `globalTokenBurst`
- `perUserRequestBurst`
- `queueSize`

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

### 限流结构

`Limiter` 当前由三部分组成：

- 一个全局 token bucket
- 一个 `map[apiKey]*TokenBucket`
- 一个单消费者队列 `chan *Request`

构造时会启动两个 goroutine：

- `refillLoop()`
- `consumeLoop()`

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

### 两层限流逻辑

当前 `check(key, userQPS, tokens)` 的执行顺序：

1. 先检查全局桶：`globalToken.TryConsume(tokens)`
2. 再检查当前 `apiKey` 的请求桶：`userTB.TryConsume(1)`

含义如下：

- 全局桶按“估算 token 数”限流
- key 桶按“请求数”限流

注意两个配置名的实际语义：

- `globalTPM` 真正在当前实现里对应“全局 token 速率”
- `globalQPS` 是用户未配置 `QPS` 时的默认请求速率
- `perUserRequestBurst` 是每个 APIKey 请求桶的突发容量

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

- 没有按 tenant 做聚合限流
- 没有按真实 completion token 限流
- 没有按 provider 区分限流
- 每个请求只做一次准入判定，不做后续补扣

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
  - `rate_limited`

也就是说，middleware 不再只是返回 HTTP 错误，不记 Prometheus。

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

### 当前边界

当前监控仍然有几个已知边界：

- 没有 tracing / span 体系
- 没有 request-id 级别的 metrics/log correlation
- `provider_circuit_state` 只有在显式同步时才会更新，不是后台定时采集
- Prometheus alert rules 和 Grafana dashboard 只提供最小样例，不代表完整生产规则

## 路由

### 路由器职责

`internal/service/router` 的职责是“对候选 provider 集合做分流、排序和选择”，不负责鉴权，也不负责上游协议转换。

候选 provider 不是全局 provider 列表，而是：

1. 先从数据库读取 tenant 可用 provider 名单
2. 再通过 `ProviderMgr.ListByNames()` 得到候选列表
3. `responses.Service.buildRouteContext(...)` 提取输入特征
4. `router.OrderCandidates(...)` 执行分流和排序

所以当前路由的第一层过滤是 tenant 绑定关系。

### 当前选择流程

当前主链路的路由现在是三段式：

1. `ListTenantProviders(identity.TenantID)`
2. `ProviderMgr.ListByNames(providerNames)`
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
   - `ruleEngine`
   - `ranker`
   - `strategy`
5. 主业务层自行按排序后的列表重试 / fallback

因此，当前 router 仍然不负责业务重试，但它已经成为“候选集过滤 + 排序”的统一入口。

### 负载跟踪

路由器内部维护了一个 `loads map[string]int64`。

主业务在调用 provider 前后会分别执行：

- `router.IncLoad(providerName)`
- `router.DecLoad(providerName)`

这让 `least_load` 策略可以基于当前内存中的并发计数工作。

### 路由三层

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

这层是“候选集过滤”，不是最终选择器。

#### 2. ranker

`ranker` 是独立排序入口。

当前状态：

- `ranker.enabled=false`：默认关闭
- `ranker.method=ml_rank`：已经预留配置和代码入口，但当前只返回原顺序

也就是说，`ml_rank` 目前只是显式 `TODO`，没有真正引入 `LightGBM` / `BERT` 推断。

#### 3. strategy

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
- 返回的是“轮转后的候选顺序”，不是直接选单个 provider

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

#### 5. `sticky`

算法：

1. 如果 `sessionID` 为空，回退到 `round_robin`
2. 否则按字符做 31 进制累乘哈希
3. `index = hash % len(candidates)`
4. 以该位置作为排序起点轮转候选顺序

即：

```text
hash = 0
for ch in sessionID:
    hash = hash*31 + int(ch)
start = abs(hash % len(candidates))
ordered = candidates[start:] + candidates[:start]
```

特点：

- 同一 `sessionID` 在同一候选集下会倾向命中同一 provider
- 没有一致性哈希环
- 候选集变化时映射可能整体漂移
- 如果 `sessionID` 为空，会回退到 round-robin 风格的轮转顺序

## 维护建议

如果后续继续演进这四个机制，建议优先保持两个原则：

1. 先区分”当前行为文档”和”目标设计文档”
2. 涉及限流、路由时，先明确维度，再谈实现

推荐的维度思考顺序：

- 谁是作用域主体：tenant、user、api key、provider
- 限的是请求数、prompt token、completion token，还是总 token
- 路由依据是成本、负载、模型能力，还是 session 粘性
- 权限依据是固定角色，还是策略系统

当前这份文档的用途是帮助你在改动前先看清楚”代码现在到底怎么跑”。后续如果你需要，我可以再把这份文档拆成：

- 面向维护者的内部机制文档
- 面向 API 使用者的运行时说明




