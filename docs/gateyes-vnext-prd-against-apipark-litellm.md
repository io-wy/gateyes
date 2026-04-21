# Gateyes vNext PRD

对标对象：APIPark、LiteLLM  
更新时间：2026-04-21

## 1. 文档目标

这份 PRD 不是“列一堆别人有的功能”，而是基于三个系统的真实定位和能力边界，回答一个更实际的问题：

> Gateyes 下一阶段到底应该做成什么，才能在严格对比 APIPark 和 LiteLLM 之后，既不做成阉割版 competitor，也不做成失焦的 feature dump。

本文把需求分成两层：

1. **严格能力差距**：APIPark / LiteLLM 有、Gateyes 现在还没有或没做完整的能力。
2. **产品要求收敛**：哪些必须做成 P0，哪些适合 P1/P2，哪些应该明确不做。

---

## 2. 对标对象的真实定位

### 2.1 APIPark 的定位

APIPark 官方把自己定义为：

- all-in-one AI gateway + API developer portal [1]
- 支持 100+ AI 模型接入与统一数据格式 [1][2]
- 可以把 AI 模型和 prompt 包装成标准 REST API [1][2]
- 支持 API portal、consumer、subscription review、版本发布、API 生命周期管理 [1][2]
- 同时也是 cloud-native API gateway，而不只是 LLM proxy [1]

一句话概括：

**APIPark 是 AI Gateway + API 产品管理平台 + Developer Portal 的组合体。**

### 2.2 LiteLLM 的定位

LiteLLM 官方把自己定义为：

- 100+ LLM provider 的统一调用层 [3][4]
- Proxy Server / AI Gateway + Python SDK 双形态 [3][4]
- 核心强调：
  - auth/authz
  - multi-tenant spend tracking / budgets
  - virtual keys
  - per-project logging / guardrails / caching
  - routing / retry / fallback / load balancing
  - admin dashboard UI [3][4]

一句话概括：

**LiteLLM 是偏 LLM 运行时治理与平台接入的 AI Gateway / Proxy，不是完整 API portal 产品。**

### 2.3 Gateyes 当前定位

从当前 Gateyes 本地 README、runtime 文档和实际运行态看：

- 它已经是一个可运行的 LLM API Gateway
- 已具备：
  - Responses / Chat / Messages 三类入口
  - tenant + RBAC + DB-backed auth
  - provider abstraction
  - retry / fallback / circuit breaker
  - usage record / basic provider stats / metrics
  - ruleEngine + strategy routing
  - provider-owned compatibility layer
- 但它现在更像：

**早期可运行 gateway kernel + 基础 control plane，而不是完整 API 产品平台，也不是成熟的 LLM FinOps / governance 平台。**

---

## 3. 严格差距对比

## 3.1 总体结论

### 相对 LiteLLM

Gateyes 当前已经接近一个“轻量自研 AI gateway kernel”，但在以下方面明显落后：

- provider 覆盖面
- virtual keys / project / team 维度治理
- budget / spend / quota 产品化
- per-project policy（logging / guardrails / caching）
- 成熟 dashboard / operator UX
- 对 100+ provider 的标准化兼容

### 相对 APIPark

Gateyes 当前与 APIPark 的差距更大，因为 APIPark 不只是 gateway：

- 缺 API developer portal
- 缺 service / consumer / subscription 审批流
- 缺 API 发布版本 / 生命周期管理
- 缺 prompt-to-API 产品层能力
- 缺“AI service + REST service”统一产品面

所以严格来说：

- **Gateyes 当前不是 APIPark 的完整替代品**
- **Gateyes 当前也还不是 LiteLLM 的成熟替代品**
- 但 Gateyes 已经具备一个更简洁、Go 实现、可控的 gateway 内核雏形

---

## 3.2 逐能力域对比

| 能力域 | APIPark | LiteLLM | Gateyes 当前 | 结论 |
|---|---|---|---|---|
| 多 provider 统一接入 | 强，官方宣称 100+ [1][2] | 强，官方宣称 100+ [3][4] | 弱到中，目前内置 OpenAI/Anthropic 类 adapter，vendor 扩展有限 | Gateyes 明显不足 |
| OpenAI-compatible 统一接口 | 有 [1][2] | 有 [3][4] | 有 | 基本具备 |
| Anthropic-compatible 接口 | 有 [1][2] | 原生/兼容均有 [3][4] | 有 | 基本具备 |
| Responses API | 有统一 AI 服务层 [1][2] | 官方文档明确支持 Responses API [3] | 有，但 outward 语义尚未完全统一 | Gateyes 仍需打磨 |
| Retry / fallback / load balancing | 有负载均衡和容灾 [1] | 官方明确有 routing / retry / fallback / load balancing [3][4] | 有 | 基础已具备 |
| Provider capability registry | 隐含存在于产品配置管理 [2] | 比较完善的 model/provider config 体系 [3][4] | 没有显式能力注册表 | Gateyes 缺 |
| Virtual keys | 未强调为核心卖点 | 核心能力 [3] | 没有 | Gateyes 缺 |
| Project / team / user 维度预算 | APIPark 有调用统计和平台治理 [1][2] | 明确有 project/user spend tracking + budgets [3][4] | 只有 quota / usage record / tenant 维度，不是预算系统 | Gateyes 缺 |
| 多租户 | 有 [1][2] | 有 [3] | 有 | 基础具备 |
| Auth / AuthZ | 有 [1][2] | 有 [3] | 有，但粒度偏基础 | Gateyes 需增强 |
| API lifecycle management | 强 [1][2] | 弱 | 弱 | Gateyes 与 APIPark 差距大 |
| API Portal / Developer Portal | 强 [1][2] | 有 dashboard，但不是完整 API portal [3] | 无 | Gateyes 缺 |
| Consumer / subscription 审批 | 强 [2] | 非核心 | 无 | Gateyes 缺 |
| Prompt 模板封装 API | 强 [1][2] | 非核心 | 无 | Gateyes 缺 |
| REST API 与 AI API 一体化治理 | 强 [2] | 非核心 | 当前基本聚焦 LLM gateway | Gateyes 缺 |
| Dashboard / 管理 UI | 强 [1][2] | 强 [3] | 几乎无真正产品级 UI | Gateyes 缺 |
| Logging / observability integration | 强 [1][2] | 强，官方明确回调/observability [3] | 有 metrics 和基础日志，但 integrations 不强 | Gateyes 缺 |
| Cost accounting / FinOps | 有调用统计 [1][2] | 强 [3][4] | 有 usage records，但不是 FinOps | Gateyes 缺 |
| Guardrails / policy | 有合规治理描述 [1] | 明确支持 guardrails [3] | 很弱 | Gateyes 缺 |
| Caching | 非重点 | 明确支持 per-project caching [3] | 无真正产品级缓存能力 | Gateyes 缺 |
| Model catalog / model metadata | 有 provider/model 管理 [2] | 强 [3][4] | `/v1/models` 仅 provider->model 映射输出 | Gateyes 弱 |
| API versioning / publishing | 强 [2] | 弱 | 无 | Gateyes 缺 |
| Enterprise operator workflow | 强 | 中到强 | 弱 | Gateyes 缺 |

---

## 4. 产品定位建议

如果严格对比 APIPark 和 LiteLLM，Gateyes 不能同时追两条路线。

推荐定位：

> **Gateyes = 面向企业/团队内部场景的 Go-based AI Gateway + Lightweight Control Plane**

不建议直接把它定义成：

- APIPark 完整替代品
- 或 LiteLLM 的全量 feature clone

原因：

1. APIPark 的强项是 portal / lifecycle / API 产品化。
2. LiteLLM 的强项是 provider breadth + spend governance + platform tooling。
3. Gateyes 当前最强的部分，是：
   - Go 实现的 gateway kernel
   - provider 协议统一内核
   - 真实请求链路的简洁性
   - 可控的 routing / persistence / accounting 基础

所以更合理的产品目标不是“做所有东西”，而是：

### Gateyes vNext 主张

- 比 LiteLLM 更简洁、可控、Go-native
- 比 APIPark 更聚焦 gateway 内核，不先走重 portal 化
- 在 **AI Gateway 运行时治理** 上达到平台可用级
- 只在必要处吸收 APIPark 的 service / publish / consumer 能力

---

## 5. 用户与场景

### 5.1 目标用户

1. **平台工程团队**
   - 统一公司内所有 LLM provider 接入
   - 管理成本、配额、权限、路由、观测
2. **AI 应用团队**
   - 不想直接适配 OpenAI / Anthropic / Bedrock / self-hosted 差异
   - 希望统一 SDK / 统一 API surface
3. **内部 AI 平台 / Agent 平台维护者**
   - 希望把 provider 接入、鉴权、回退、统计、配额放到中间层

### 5.2 核心场景

1. 同一个应用在多个模型厂商之间切换，而不改业务代码
2. 按 tenant / project / team 控制模型可见性与预算
3. 统一记录请求、token、成本、失败、fallback
4. 在 provider 故障时自动 retry / fallback
5. 以统一 API 暴露给 agents、internal apps、automation tools

### 5.3 明确非目标

在 P0/P1 阶段，不把以下内容定义为必做：

1. 完整的 public API marketplace
2. 对外商业化 developer portal
3. 与 APIPark 同等级的 API full lifecycle 平台
4. LiteLLM 级别的超大 provider 矩阵一次性覆盖

---

## 6. 产品目标

### 6.1 业务目标

1. 成为团队/企业内部统一 AI gateway
2. 降低业务接入多模型的适配成本
3. 让成本、权限、路由、观测进入统一 control plane
4. 在故障和 provider 波动下保持可控退化

### 6.2 技术目标

1. 统一 `responses/chat/messages` outward behavior
2. 显式 provider capability registry
3. 支持 virtual keys / project / team / tenant 四级治理中的至少三级
4. 完成 gateway 级 FinOps 基础能力
5. 形成平台可操作的 admin surface

---

## 7. PRD 范围

## 7.1 P0 必做

### P0-1. Provider Registry & Capability Model

**问题**

当前 provider 主要还是“配置 + model 名映射”，没有显式能力模型。

**要求**

1. provider 需要有结构化 capability metadata：
   - supports_chat
   - supports_responses
   - supports_messages
   - supports_stream
   - supports_tools
   - supports_images
   - supports_structured_output
   - supports_long_context
   - health_status
   - routing_weight
2. `/v1/models` 不只是列模型名，还要可按 capability/health 过滤。
3. 路由阶段不能只按 `model == provider.Model()`，而要结合 capability 与 route context。

**验收**

1. 坏 provider 可被标记为 unhealthy / disabled / drain。
2. `responses/chat/messages` 能基于 capability 过滤 provider。
3. 管理面可看到 provider 当前 capability 和 health。

### P0-2. Virtual Keys / Project / Team Governance

**问题**

当前只有 API key + user + tenant + quota，不足以对标 LiteLLM 的 project/user spend governance。

**要求**

1. 增加 project 概念：
   - 一个 tenant 下可有多个 project
2. 增加 team 或 group 概念（二选一即可，推荐先做 project）
3. 支持 virtual key：
   - 外部调用使用 virtual key
   - 后端再映射到 tenant/project/user/policy
4. key scope 支持：
   - allowed_models
   - allowed_providers
   - budget_limit
   - rate_limit

**验收**

1. 一个 tenant 下多个 project 预算可隔离。
2. key 可以撤销 / rotate / disable。
3. request usage 能归属到 project 维度。

### P0-3. FinOps Baseline

**问题**

当前只有 usage record，不是完整 spend/budget 系统。

**要求**

1. 为每次请求计算：
   - prompt tokens
   - completion tokens
   - total tokens
   - estimated/requested cost
   - actual billed cost（若 provider 可提供）
2. 支持 budget policy：
   - tenant budget
   - project budget
   - key budget
3. 超预算行为：
   - hard reject
   - soft alert
   - grace mode
4. cost / usage 查询必须支持：
   - by provider
   - by model
   - by project
   - by user
   - by key

**验收**

1. 至少支持日、周、月聚合。
2. 超预算拒绝会返回明确错误而不是泛型 500。
3. admin API 能拉出费用视图。

### P0-4. Runtime Routing Upgrade

**问题**

当前路由是 `ruleEngine -> ranker -> strategy`，但 `ml_rank` 还只是占位，provider capability 也未显式建模。

**要求**

1. 保留当前三段式，但升级输入：
   - capability-aware filtering
   - health-aware filtering
   - budget-aware routing
2. 增加路由原因记录：
   - 为什么命中该 provider
   - 是否发生 retry/fallback
   - 是否因为 capability / budget / health 过滤掉其他 provider
3. 增加 provider quarantine / drain 模式。

**验收**

1. 路由结果可解释。
2. fallback 不只是发生了，还能看出发生原因。
3. 坏 provider 能从默认路由池自动剔除。

### P0-5. Responses Semantics Unification

**问题**

虽然 `/v1/responses` 已可用，但当前某些 provider 通过 responses surface 返回时，仍残留 chat 语义痕迹。

**要求**

1. 所有 `/v1/responses` outward object 统一为 response-first 语义。
2. 对 chat-only upstream 要做彻底 outward normalization。
3. `GET /v1/responses/:id` 的持久化结构必须与实际返回结构一致。
4. stream 和 non-stream 的 object / event / usage 语义保持一致。

**验收**

1. 同一 provider 经 `/v1/responses` 与 `/v1/chat/completions` 返回不同 surface，但内部同源。
2. 不再出现 responses surface outward `object=chat.completion` 这类残留。

### P0-6. Admin Control Plane API

**问题**

当前有基础 admin，但距离“平台治理面”还差很多。

**要求**

新增或增强 admin API：

1. provider registry 管理
2. provider health / drain / disable
3. project / key / budget 管理
4. request trace / route trace 查询
5. usage / cost 聚合查询
6. key rotate / revoke / audit

**验收**

1. 不改配置文件也能完成大部分运行时治理。
2. 新增 provider 至少支持 DB-backed metadata 管理。

---

## 7.2 P1 应做

### P1-1. Service Abstraction

这是从 APIPark 吸收的第一块，但只做轻量版，不做完整 portal。

**要求**

1. 引入 service 对象：
   - service_id
   - request_prefix
   - default_provider
   - default_model
   - team/project ownership
2. 一个 service 下可挂多个 API surface。
3. 支持统一 publish / enable / disable。

**目标**

把“gateway 只是 provider 列表”升级成“gateway 管理对外服务单元”。

### P1-2. Prompt-to-API

从 APIPark 吸收，但保持轻量：

1. 支持 prompt template + variables
2. 把 prompt API 映射成标准 REST/LLM endpoint
3. 保留 provider fallback / budget / logging

**注意**

这不是做一个 prompt playground，而是做一个可治理的 service capability。

### P1-3. Versioned Publish Workflow

1. 对 service 或 API 配置变更引入 version
2. 支持 staged publish
3. 支持 rollback

### P1-4. Consumer / Subscription Review

从 APIPark 吸收，但只做内部门户版：

1. consumer 对 service 申请访问
2. service owner 审批
3. 批准后生成 scoped key

---

## 7.3 P2 可选

### P2-1. Developer Portal

如果要继续向 APIPark 靠拢，再做：

1. portal 展示 service catalog
2. 文档、示例、调用方式
3. subscription self-service

### P2-2. Guardrails / Policy Plugins

向 LiteLLM 靠拢：

1. per-project guardrails
2. request / response moderation hooks
3. redact / DLP / allowlist / blocklist

### P2-3. Caching

只有在明确知道目标之后再做：

1. prefix-cache aware metrics
2. semantic cache / response cache（仅在场景真实需要时）

---

## 8. 明确不做

以下内容不应进入 P0/P1：

1. 一开始就做 100+ provider 全覆盖
2. 一开始就做完整 public marketplace
3. 把精力优先投入前端 portal 视觉层
4. 做复杂 ML route ranker 而没有先做 capability + observability
5. 做“平台很大”但没有把 response-first 内核收干净

---

## 9. 关键用户故事

### 用户故事 1：平台管理员接入新 provider

作为平台管理员，我希望：

1. 注册一个 provider
2. 标注其 capabilities
3. 设默认 health check
4. 决定它属于哪个 tenant/project 路由池

以便：

- 不改代码就能调整模型接入策略

### 用户故事 2：项目负责人为团队设置预算

作为项目负责人，我希望：

1. 给 project 配置月预算
2. 给关键业务 key 配置单日上限
3. 超预算时自动拒绝或告警

以便：

- 避免模型费用不可控

### 用户故事 3：应用开发者只接一个统一 endpoint

作为应用开发者，我希望：

1. 只接统一 API
2. 不关心 OpenAI / Anthropic / self-hosted 差异
3. 还能获得稳定的错误、usage、trace 信息

### 用户故事 4：SRE 排查失败请求

作为 SRE，我希望：

1. 看到请求最终打到了哪个 provider
2. 为何没选其他 provider
3. 是否发生了 retry / fallback
4. 每次失败是 gateway parser 问题还是 upstream 问题

---

## 10. 核心 API / 数据要求

### 10.1 Provider Metadata

```json
{
  "name": "longcat-primary",
  "type": "openai",
  "health_status": "healthy",
  "enabled": true,
  "drain": false,
  "capabilities": {
    "chat": true,
    "responses": false,
    "messages": false,
    "stream": true,
    "tools": true,
    "images": true,
    "structured_output": false
  },
  "routing_weight": 10
}
```

### 10.2 Project

```json
{
  "project_id": "proj_marketing",
  "tenant_id": "tenant_a",
  "name": "marketing",
  "budget_monthly_usd": 500,
  "allowed_models": ["LongCat-Flash-Thinking", "glm-5.1"]
}
```

### 10.3 Route Trace

```json
{
  "request_id": "req_xxx",
  "surface": "responses",
  "requested_model": "LongCat-Flash-Thinking",
  "candidate_providers": ["longcat-primary", "glm"],
  "filtered_out": [
    {
      "provider": "glm",
      "reason": "capability_mismatch"
    }
  ],
  "selected_provider": "longcat-primary",
  "retries": 1,
  "fallbacks": 0
}
```

---

## 11. 非功能要求

### 性能

1. gateway 自身引入的中位延迟应尽量控制在低双位数毫秒以内
2. routing / auth / persistence 不应成为明显热点

### 可靠性

1. provider failure 不得导致全局雪崩
2. cancelled stream 必须写终态
3. retry / fallback 必须可解释

### 可运维性

1. 所有决策都能追溯
2. 错误必须能分层：
   - client error
   - gateway parse error
   - auth error
   - budget error
   - upstream 4xx/5xx

### 安全

1. key 不以明文长期出现在 repo 中
2. key / budget / project / consumer 操作必须有审计日志
3. consumer/project/key 至少需要最基础的 scoped permissions

---

## 12. 里程碑建议

### Milestone 1: Gateway Kernel Hardening

目标：

- 把现有运行内核从“可跑”提到“稳定可治理”

交付：

- response semantics 收口
- provider capability registry
- route trace
- provider health / drain

### Milestone 2: Governance Plane

交付：

- project
- virtual keys
- budget
- spend reporting

### Milestone 3: Service Layer

交付：

- service abstraction
- prompt-to-API
- versioned publish

### Milestone 4: Internal Developer Portal

交付：

- consumer
- subscription review
- internal portal

---

## 13. 成功指标

### 功能指标

1. 新 provider 从接入到进入路由池不需要改代码
2. 业务侧只用统一 API 即可切换 provider
3. project / key / tenant 三层 usage 和 spend 可查

### 质量指标

1. response/chat/messages 三类 surface 在主 provider 上稳定通过 live matrix
2. 坏 provider 不会污染默认路由成功率
3. fallback 与 route trace 可观测

### 产品指标

1. 应用开发者接入时间显著下降
2. 平台团队能控制预算与权限
3. 运维排障不再依赖猜 provider 行为

---

## 14. 最终产品决策

### 结论

Gateyes vNext 不应该直接做成：

- APIPark 的完整复制品
- LiteLLM 的 provider matrix 复制品

它应该做成：

> **以 Go 实现为核心、面向企业内部平台团队的 AI Gateway + Lightweight Governance Plane**

### 对 APIPark 的吸收

吸收：

- service abstraction
- publish/version
- consumer/subscription review

暂不吸收：

- 完整 public developer portal
- 全套 API marketplace/productization

### 对 LiteLLM 的吸收

必须吸收：

- virtual keys
- budgets / spend governance
- provider breadth 扩展策略
- routing / fallback / observability 产品化

### 对 Gateyes 自身的坚持

保留：

- provider-owned protocol core
- Go data plane
- 简洁 request path
- response-first 内核

---

## Sources

[1] APIPark GitHub README, official repository: https://github.com/APIParkLab/APIPark  
[2] APIPark official docs, AI Services (AI Gateway): https://docs.apipark.com/docs/services/ai_services  
[3] LiteLLM official docs homepage / getting started / proxy overview: https://docs.litellm.ai/  
[4] LiteLLM GitHub README, official repository: https://github.com/BerriAI/litellm
