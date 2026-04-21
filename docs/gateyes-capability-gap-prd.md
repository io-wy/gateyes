# Gateyes Capability Gap PRD

对标对象：LiteLLM、APIPark、Portkey、Dify  
更新时间：2026-04-21

## 1. 文档目标

这份文档不是重写 [`gateyes-vnext-prd-against-apipark-litellm.md`](./gateyes-vnext-prd-against-apipark-litellm.md)，而是把一组更贴近真实采购和使用决策的 20 条痛点，整理成一份可执行 PRD：

1. 明确 **哪些已经做了**
2. 明确 **哪些只是部分做了**
3. 明确 **哪些还没做**
4. 明确 **哪些值得现在做，哪些不值得现在做**

本文的判断基于当前仓库代码、README、runtime 文档与现有 admin/runtime surface，而不是产品宣传口径。

---

## 2. 当前基线

在这份补充 PRD 编写时，Gateyes 已经具备以下基线能力：

1. `responses/chat/messages` 三类文本生成入口
2. 多租户、RBAC、DB-backed auth
3. provider registry、health/drain、route trace、usage/cost summary
4. API key lifecycle、project/key/tenant budget 基础能力
5. service abstraction、prompt-to-API、publish/version、subscription review
6. service-level guardrails/policy 基础能力

当前仍然是：

> **Go-based AI gateway kernel + lightweight governance plane**

它还不是：

1. 完整 provider matrix 平台
2. 完整 API portal 产品
3. 完整 agent runtime / workflow 平台

---

## 3. 状态结论总览

### 3.1 已做 / 部分做 / 未做

| 编号 | 能力 | 当前状态 | 结论 |
|---|---|---|---|
| 1 | Embeddings / Images / Audio 统一接入 | 未做 | 只有 text generation |
| 2 | Function Calling 多轮编排 | 未做 | 只有 `tool_calls` 透传 |
| 3 | Batch API | 未做 | 没有 batch job 模型 |
| 4 | Fine-tuning / Model Management | 未做 | 没有 fine-tuning control plane |
| 5 | 精确 token 计数 | 部分做 | runtime usage 优先取上游，admission 和兜底仍粗糙 |
| 6 | 官方 SDK / Client Library | 未做 | 只有 REST API |
| 7 | Admin Dashboard / Web UI | 未做 | 只有 admin API |
| 8 | 主动健康检查 | 未做 | 只有被动 circuit breaker |
| 9 | 审计日志 | 未做 | 没有资源操作审计流 |
| 10 | Provider 动态管理 | 部分做 | metadata 可热改，provider 实例生命周期还不完整 |
| 11 | Webhook / Callback 体系 | 部分做 | 只有 quota/budget 告警 webhook 基础能力 |
| 12 | 更细粒度限流 | 部分做 | 有全局 TPM 与 key QPS，没有 tenant/provider/model/RPM |
| 13 | 配置热更新 | 未做 | 仍以启动装配为主 |
| 14 | 请求/响应日志检索 | 部分做 | 有持久化和聚合，没有 operator 级搜索面 |
| 15 | Provider 适配广度 | 未做 | 仍以 `openai/anthropic/grpc-vllm` 为主 |
| 16 | Guardrails / 内容安全 | 部分做 | 已有 service-level policy，不是完整插件化 guardrails |
| 17 | 语义缓存 | 未做 | 没有 semantic cache |
| 18 | OpenTelemetry tracing | 未做 | 只有 `traceparent` / request correlation |
| 19 | 多实例部署支持 | 未做 | 没有分布式状态同步 |
| 20 | API key 使用者自助服务 | 部分做 | 有 subscription review + scoped key，没有 self-service portal |

### 3.2 值得做程度

| 编号 | 能力 | 值得做程度 | 建议 |
|---|---|---|---|
| 1 | Embeddings / Images / Audio | 高 | 先做 `embeddings`，image/audio 后续分波次 |
| 2 | Function Calling 多轮编排 | 中高 | 值得做，但应放 `service/orchestrator`，不要污染 provider |
| 3 | Batch API | 中 | 有价值，但不是当前最优先 |
| 4 | Fine-tuning / Model Management | 低到中 | 只有在定位转向模型平台时才值得优先做 |
| 5 | 精确 token 计数 | 高 | 直接影响成本信任度 |
| 6 | SDK / Client Library | 中 | API 面稳定一轮后再做 |
| 7 | Admin Dashboard / Web UI | 高 | 对采购和运营体验影响极大 |
| 8 | 主动健康检查 | 高 | 生产网关硬需求 |
| 9 | 审计日志 | 高 | 企业合规硬需求 |
| 10 | Provider 动态管理 | 高 | 生产运维硬需求 |
| 11 | Webhook / Callback 体系 | 中高 | 对集成很有价值，但次于 health/audit |
| 12 | 更细粒度限流 | 高 | 企业治理刚需 |
| 13 | 配置热更新 | 中 | 重要，但优先级低于 health/audit/provider mgmt |
| 14 | 请求/响应日志检索 | 中高 | 排障强需求 |
| 15 | Provider 适配广度 | 高 | 市场门槛能力 |
| 16 | Guardrails / 内容安全 | 高 | 企业治理与合规核心卖点 |
| 17 | 语义缓存 | 低到中 | 成本卖点，但不是当前最卡脖子 |
| 18 | OpenTelemetry tracing | 高 | SRE 场景强需求 |
| 19 | 多实例部署支持 | 高 | 走生产部署必须补 |
| 20 | API key 使用者自助服务 | 中高 | 产品化很重要，但可晚于核心治理面 |

---

## 4. 逐项 PRD 结论

## 4.1 使用者（App Developer / Agent Builder）痛点

### 1. Embeddings / Images / Audio 统一接入

**当前状态**

- 未做
- 当前只覆盖文本生成主链路：`responses/chat/messages`

**为什么值得做**

1. `embeddings` 是最通用的第二能力域
2. 企业内部知识库、RAG、召回、语义检索都依赖 embedding
3. 没有 `embeddings`，就不是完整的“统一 AI gateway”

**产品要求**

第一阶段建议只做：

1. `POST /v1/embeddings`
2. provider 内部统一 embedding request/response 协议
3. usage / cost / route trace / budget / policy 复用现有控制面

`images/audio` 建议放到下一波，而不是这轮一次性做满。

**优先级**

`P0+`

### 2. Function Calling 多轮编排

**当前状态**

- 未做
- 只有 tool call transport，不是 tool execution loop

**为什么值得做**

1. 对 Agent Builder 价值极高
2. 能明显降低应用侧 orchestration 负担
3. 这是从“兼容模型 API”走向“agent-friendly gateway”的关键一步

**边界**

不建议放进 provider。应该放到：

1. `service` 层
2. 或独立 `orchestrator` 层

原因是：

1. provider 层只应该负责协议与上游 adapter
2. tool loop 是业务编排，不是 provider transport

**优先级**

`P1+`

### 3. Batch API

**当前状态**

- 未做

**为什么不是最优先**

1. 有价值，但主要服务离线批处理场景
2. 对当前 Gateyes “生产治理能力”提升不如 health/audit/provider mgmt 直接

**建议**

如果做，应独立为：

1. batch job metadata
2. async queue / worker
3. callback / result fetch API

**优先级**

`P2`

### 4. Fine-tuning / Model Management

**当前状态**

- 未做

**判断**

这条不应该优先进入当前主路线。原因是：

1. 它更接近模型平台，不是网关核心能力
2. 会把产品边界从 gateway 拉向 model ops platform

**优先级**

`Not Now`

### 5. 精确 token 计数

**当前状态**

- 部分做了
- provider 返回的真实 usage 已经会被记录
- admission 和 fallback 场景仍有粗估

**为什么值得做**

1. 直接影响账单和预算信任度
2. 关系到 operator 和开发者是否相信网关口径

**产品要求**

1. 引入主流 tokenizer 计数能力
2. 区分 `admission estimate` 与 `billed usage`
3. 在 API 和 admin 面明确暴露“估算值/真实值”来源

**优先级**

`P0`

### 6. 官方 SDK / Client Library

**当前状态**

- 未做

**判断**

值得做，但应在 API 面和 service surface 再稳定一轮之后再做，否则 SDK 很快会失配。

**优先级**

`P2`

## 4.2 运营者（Platform Engineer / SRE / 管理员）痛点

### 7. Admin Dashboard / Web UI

**当前状态**

- 未做
- 当前只有 admin API

**为什么值得做**

1. 没有 UI，产品化门槛明显更高
2. 团队负责人、财务、运营无法直接使用

**建议**

第一阶段不做完整 portal，只做 operator dashboard：

1. provider 状态
2. spend / usage 视图
3. service / subscription / key 管理
4. request trace / route trace 查看

**优先级**

`P0`

### 8. 主动健康检查

**当前状态**

- 未做

**为什么值得做**

1. 生产网关硬需求
2. 能避免第一个真实业务请求承担 provider 探活成本

**产品要求**

1. 周期性主动 probe
2. health 状态写回 provider registry
3. 与 route filtering 联动
4. 支持管理面强制 quarantine/drain

**优先级**

`P0`

### 9. 审计日志

**当前状态**

- 未做

**为什么值得做**

1. 企业合规要求
2. 能回答“谁改了预算/谁发了 key/谁上线了 provider”

**优先级**

`P0`

### 10. Provider 动态管理

**当前状态**

- 部分做了
- metadata 已经 DB-backed
- 但 provider instance lifecycle 还没有完整 runtime 管理面

**产品要求**

需要做到不重启即可：

1. 注册 provider
2. 修改 endpoint / API key / timeout / headers
3. 上下线 provider
4. 调整 weight / health policy

**优先级**

`P0`

### 11. Webhook / Callback 体系

**当前状态**

- 部分做了
- 当前只有 quota/budget 告警方向的 webhook 基础能力

**建议补充**

1. request completed callback
2. provider state changed callback
3. budget exhausted callback
4. error-rate spike callback

**优先级**

`P1`

### 12. 更细粒度限流

**当前状态**

- 部分做了
- 已有全局 TPM + per-key QPS

**还缺**

1. tenant limit
2. provider limit
3. model limit
4. RPM

**优先级**

`P0`

### 13. 配置热更新

**当前状态**

- 未做

**判断**

重要，但优先级低于：

1. 主动健康检查
2. 审计日志
3. provider 动态管理

因为只要把主要治理项放入 DB-backed control plane，热更新压力会先下降一截。

**优先级**

`P1`

### 14. 请求/响应日志检索

**当前状态**

- 部分做了
- 当前已有 `responses` 与 `usage` 持久化
- 已有 summary/breakdown/trend
- 没有全文检索与 operator 搜索面

**建议**

1. 支持按关键词、时间、status、provider、model、service 检索
2. 提供 request trace / route trace / response body 关联检索

**优先级**

`P1`

## 4.3 市场竞争力维度

### 15. Provider 适配广度

**当前状态**

- 未做
- 仍以 `openai / anthropic / grpc-vllm` 为主

**为什么值得做**

1. 这是直接影响采购的门槛项
2. `Azure OpenAI` 与 `Bedrock` 是企业场景的硬门槛

**建议顺序**

1. Azure OpenAI
2. AWS Bedrock
3. Vertex AI
4. 国内主流厂商

**优先级**

`P0`

### 16. Guardrails / 内容安全

**当前状态**

- 部分做了
- 现在已经有 service-level request/response policy

当前已支持：

1. `allow_models`
2. `block_models`
3. `block_terms`
4. `block_regex`
5. `redact_terms`
6. `max_input_chars`
7. `max_output_chars`

**为什么仍只是部分做了**

1. 还是 service-local policy，不是 project/tenant/global 多层继承
2. 不是插件化 hook 体系
3. 没有真正接外部 moderation / DLP provider

**建议**

继续坚持现在的层次：

1. handler 只做 HTTP 参数解析
2. service 负责 policy 执行
3. provider 不承载 policy 逻辑

**优先级**

`P0`

### 17. 语义缓存

**当前状态**

- 未做

**判断**

它是成本卖点，不是当前最卡脖子的生产治理能力。  
而且从 Gateyes 当前定位看，网关层缓存不应该抢在治理面之前做。

**优先级**

`P3`

### 18. OpenTelemetry tracing

**当前状态**

- 未做
- 当前只有 `traceparent` 和 request correlation

**为什么值得做**

1. 微服务环境下的 SRE 需要完整链路视图
2. 对排障和性能分析很关键

**优先级**

`P0`

### 19. 多实例部署支持

**当前状态**

- 未做

**为什么值得做**

1. 走生产高可用必须补
2. 限流、熔断、sticky 等现在都偏单实例内存口径

**优先级**

`P1`

### 20. API key 使用者自助服务

**当前状态**

- 部分做了
- 已经有 subscription review + scoped key issuance
- 但没有 developer self-service 面

**建议**

第一阶段补最小自助面：

1. 查看自己的 key
2. 查看自己的 usage / budget
3. 自助 rotate 自己的 key
4. 查看自己被授权的 services

**优先级**

`P1`

---

## 5. 优先级收敛

## 5.1 现在最值得做的

### Wave 1：生产可用门槛

1. `15` Provider 广度，先补 `Azure OpenAI + Bedrock`
2. `1` 先补 `embeddings`
3. `8` 主动健康检查
4. `9` 审计日志
5. `10` 完整 provider 动态管理
6. `12` tenant/provider/model/RPM 限流
7. `18` OpenTelemetry tracing
8. `16` guardrails 从 service-level 向 project/tenant 多层继承推进

### Wave 2：产品化增强

1. `7` operator dashboard
2. `14` 请求/响应检索
3. `20` developer self-service
4. `11` webhook / callback 体系

### Wave 3：能力增强但不抢前

1. `2` function calling 多轮编排
2. `6` SDK
3. `13` 配置热更新
4. `19` 多实例一致性

### Wave 4：明确后置

1. `3` batch API
2. `4` fine-tuning / model management
3. `17` semantic cache

---

## 6. 明确不建议现在优先做的

以下能力不是“不值得做”，而是 **不值得现在抢前做**：

1. Batch API
2. Fine-tuning / Model Management
3. Semantic Cache
4. SDK

理由不是它们没有价值，而是：

1. 它们不如 health/audit/provider mgmt/OTel 直接决定生产可用性
2. 它们不如 Azure/Bedrock/embeddings 直接决定市场门槛
3. 它们会把产品边界拉向“更大平台”，而当前最紧的缺口仍是 gateway governance

---

## 7. 最终产品判断

如果目标是让 Gateyes 变成“真实可采购、可运营、可扩展”的企业网关，那么：

### 结论一

当前最值得做的不是继续加更多文本 generation 花样能力，而是先补：

1. provider breadth
2. operator governance
3. observability
4. guardrails

### 结论二

Function Calling 自动编排值得做，但要守住层次：

1. 不放 provider
2. 放 service / orchestrator

### 结论三

语义缓存、fine-tuning、batch 这些能力都可以做，但不应该抢在：

1. Azure/Bedrock
2. embeddings
3. 健康检查
4. 审计日志
5. 动态 provider 管理
6. OTel

之前。

### 结论四

Guardrails / Policy 方向是对的，而且已经开始进入正确层次：

1. handler 解析 HTTP
2. service 承载 policy
3. provider 保持 adapter 职责

这条边界应该继续坚持。
