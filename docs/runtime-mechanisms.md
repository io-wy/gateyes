# Runtime Mechanisms

本文档描述 Gateyes 当前实现中的五个核心机制：

- 缓存
- 鉴权
- 限流
- 路由
- 权限模型

文档目标是帮助维护者理解“现在的代码实际上在做什么”。本文按当前实现编写，不代表未来目标设计，也不主动修正现有行为。

## 适用范围

- 程序入口：`cmd/gateway/main.go`
- HTTP 入口与路由分组：`internal/handler/server.go`
- 中间件：`internal/middleware`
- 主业务编排：`internal/service/responses`
- 核心服务：`internal/service/auth`、`internal/service/cache`、`internal/service/limiter`、`internal/service/router`
- 持久化：`internal/repository`、`internal/repository/sqlstore`

## 请求主链路

当前 `/v1` 请求的主链路可以概括为：

1. Gin 路由接收请求。
2. `mw.Auth()` 解析 `Authorization` 并完成数据库鉴权。
3. 对 LLM 写请求，`mw.GuardLLMRequest()` 继续执行：
   - 读取请求体并提取 `model`
   - 估算 prompt token
   - 检查模型白名单
   - 检查 quota
   - 执行限流
4. `handler` 负责：
   - 绑定 JSON
   - 在 Chat Completions 和 Responses 之间做兼容转换
   - 返回 JSON 或 SSE
5. `responses.Service` 负责：
   - 查询 tenant 可用 provider
   - 选择 provider
   - 写入 `responses` 表中的 `in_progress` 记录
   - 命中缓存则直接返回缓存结果
   - 未命中则调用上游 provider
   - 写回 `responses`
   - 写 usage

和本文五个机制最相关的入口文件：

- `internal/handler/server.go`
- `internal/middleware/middleware.go`
- `internal/service/responses/service.go`

## 鉴权

### 鉴权输入

运行时和管理接口统一使用：

```text
Authorization: Bearer <api_key>:<api_secret>
```

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

1. 预检查：`mw.GuardLLMRequest()` 中调用 `auth.HasQuota(identity, estimatedPromptTokens)`
2. 实际记账：响应成功后 `auth.RecordUsage(...)` 调用 `store.ConsumeQuota(...)`

预检查用的是“估算 prompt token”，真正入账用的是响应里的 `total_tokens`。

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
limiter.Allow(ctx, identity.APIKey, estimatedPromptTokens)
```

也就是说，当前限流维度基于 `APIKey`，而不是 `UserID` 或 `TenantID`。

### 配置项

限流配置定义在 `LimiterConfig`：

- `globalQPS`
- `globalTPM`
- `burst`
- `queueSize`

默认示例位于 `configs/config.yaml`。

### 请求元数据提取

限流前，中间件会先读取请求体并估算 token：

1. 读取整个 body
2. 反序列化为 `provider.ResponseRequest`
3. 调用 `Normalize()`
4. 调用 `EstimatePromptTokens()`

`EstimatePromptTokens()` 的算法非常简单：

```text
for each input message:
    total += RoughTokenCount(message.Signature())

if total == 0:
    return 1
```

`RoughTokenCount(content)` 当前实现是：

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

当前 `check(key, tokens)` 的执行顺序：

1. 先检查全局桶：`globalToken.TryConsume(tokens)`
2. 再检查当前 `apiKey` 的桶：`userTB.TryConsume(1)`

含义如下：

- 全局桶按“估算 token 数”限流
- key 桶按“请求数”限流

注意两个配置名的实际语义：

- `globalTPM` 真正在当前实现里对应“全局 prompt-token 速率”
- `globalQPS` 实际被用作“每个 APIKey 的请求速率”

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

- 没有使用 `identity.QPS` 或用户表中的 `qps`
- 没有按 tenant 做聚合限流
- 没有按真实 completion token 限流
- 没有按 provider 区分限流
- 每个请求只做一次准入判定，不做后续补扣

## 路由

### 路由器职责

`internal/service/router` 的职责是“在候选 provider 集合里选一个 provider”，不负责鉴权，也不负责上游协议转换。

候选 provider 不是全局 provider 列表，而是：

1. 先从数据库读取 tenant 可用 provider 名单
2. 再通过 `ProviderMgr.ListByNames()` 得到候选列表
3. 再交给 `Router.SelectFrom(...)`

所以当前路由的第一层过滤是 tenant 绑定关系。

### 当前选择流程

`responses.Service.selectProvider()` 的流程：

1. `ListTenantProviders(identity.TenantID)`
2. `ProviderMgr.ListByNames(providerNames)`
3. `Router.SelectFrom(candidates, sessionID)`

当前实现中，选择逻辑不使用请求里的 `model` 做额外过滤。

### 负载跟踪

路由器内部维护了一个 `loads map[string]int64`。

主业务在调用 provider 前后会分别执行：

- `router.IncLoad(providerName)`
- `router.DecLoad(providerName)`

这让 `least_load` 策略可以基于当前内存中的并发计数工作。

### 五种策略

#### 1. round_robin

算法：

```text
selected = candidates[index % len(candidates)]
index = (index + 1) % len(candidates)
```

特点：

- 简单
- 全局共享一个递增索引

#### 2. random

算法：

```text
selected = candidates[rand.Intn(len(candidates))]
```

特点：

- 随机选取
- 不看成本，不看当前负载

#### 3. least_load

算法：

```text
selected = provider with minimal loads[name]
```

特点：

- 基于本进程内存中的实时负载
- 不依赖数据库统计

#### 4. cost_based

算法：

```text
selected = provider with minimal UnitCost()
```

特点：

- 完全不看负载
- 完全不看延迟
- 只看配置中的价格字段

#### 5. sticky

算法：

1. 如果 `sessionID` 为空，回退到 `round_robin`
2. 否则按字符做 31 进制累乘哈希
3. `index = hash % len(candidates)`
4. 返回该位置 provider

即：

```text
hash = 0
for ch in sessionID:
    hash = hash*31 + int(ch)
selected = candidates[abs(hash % len(candidates))]
```

特点：

- 同一 `sessionID` 在同一候选集下会倾向命中同一 provider
- 没有一致性哈希环
- 候选集变化时映射可能整体漂移

## 缓存

### 缓存位置

当前缓存是进程内内存缓存，实现位于：

- `internal/service/cache/kv_cache.go`
- `internal/service/responses/service.go`

只有非流式请求会走缓存。

### 读写时机

在 `responses.Service.Create()` 中：

1. `prepare()` 先选 provider 并写 `responses` 表中的 `in_progress` 记录
2. 如果 `cfg.Cache.Enabled && cache != nil && !req.Stream`
   - 先查缓存
3. 未命中才真正调用上游 provider
4. 成功后再把响应 JSON 写入缓存

也就是说，当前缓存命中发生在：

- 已完成鉴权
- 已完成限流
- 已完成 provider 选择
- 已写入一条新的 response 记录

### Cache Key 算法

`ResponseRequest.CacheKey()` 当前算法：

1. 写入请求里的 `model`
2. 逐条遍历 `InputMessages()`
3. 对每条消息拼接：
   - `role`
   - `:`
   - `message.Signature()`
4. 用换行连接

概括为：

```text
key_material =
    req.model + "\n" +
    message_1.role + ":" + message_1.signature + "\n" +
    message_2.role + ":" + message_2.signature + "\n" + ...
```

真正落到缓存 map 之前，会再做一次：

```text
sha256(key_material)
```

### Message Signature 算法

`Message.Signature()` 当前包含：

- 普通文本内容
- `tool_call_id`
- tool call 的 `id`
- tool call 的 `function.name`
- tool call 的 `function.arguments`

这让缓存 key 能区分普通对话和 tool call 轨迹。

### 缓存数据结构

缓存对象是：

- `items map[string]*CacheItem`
- `accessOrder []string`

每个 `CacheItem` 保存：

- `Value`
- `ExpiresAt`
- `AccessTime`
- `HitCount`

### 读算法

`Cache.Get(prompt)` 的流程：

1. 对传入 key material 做 SHA-256
2. 加写锁
3. 如果命中且未过期：
   - 更新 `AccessTime`
   - `HitCount++`
   - 全局 `hitCount++`
   - 把 key 移到 `accessOrder` 尾部
   - 返回值
4. 如果命中但已过期：
   - 删除该项
   - 从 `accessOrder` 移除
5. 未命中：
   - `missCount++`
   - 返回 miss

### 写算法

`Cache.Set(prompt, response)` 的流程：

1. 计算 SHA-256 key
2. 加写锁
3. 如果 key 已存在：
   - 覆盖 `Value`
   - 刷新 `ExpiresAt`
   - 刷新 `AccessTime`
   - 把 key 移到末尾
4. 如果 key 不存在且缓存已满：
   - 淘汰 `accessOrder[0]`
5. 插入新项
6. 把 key 追加到 `accessOrder` 尾部

### 淘汰和过期

当前同时存在两种移除机制：

- 容量淘汰：近似 LRU
- TTL 过期：后台每分钟清理一次

近似 LRU 的原因是：

- 访问顺序用切片维护
- 每次命中会把 key 移到末尾
- 容量满时移除头部

### 命中后的业务处理

命中缓存后，`responses.Service.useCachedResponse()` 会：

1. 反序列化缓存中的响应 JSON
2. 重写：
   - `resp.ID`
   - `resp.Model`
   - `resp.Created`
   - `resp.Status`
3. 先做 quota 可用性检查
4. 把 `responses` 表中的当前请求记录更新为 `completed`
5. 调用 `auth.RecordUsage(...)`
   - provider 名记为 `cache`
   - latency 记为 `0`

所以当前缓存不是“纯静态旁路缓存”，而是仍然参与：

- 响应持久化
- usage 记账
- quota 消耗

### 当前缓存边界

当前缓存实现的几个边界：

- 仅支持进程内缓存
- 仅支持非流式请求
- 缓存值是完整响应 JSON
- 命中后 usage provider 被记为 `cache`
- 没有主动失效机制

## 维护建议

如果后续继续演进这五个机制，建议优先保持两个原则：

1. 先区分“当前行为文档”和“目标设计文档”
2. 涉及缓存、限流、路由时，先明确维度，再谈实现

推荐的维度思考顺序：

- 谁是作用域主体：tenant、user、api key、provider
- 限的是请求数、prompt token、completion token，还是总 token
- 路由依据是成本、负载、模型能力，还是 session 粘性
- 权限依据是固定角色，还是策略系统

当前这份文档的用途是帮助你在改动前先看清楚“代码现在到底怎么跑”。后续如果你需要，我可以再把这份文档拆成：

- 面向维护者的内部机制文档
- 面向 API 使用者的运行时说明
