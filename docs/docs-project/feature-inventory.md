# Gateyes 功能盘点

> 更新时间：2026-07-28

本文档记录当前仓库里已经落地的功能，作为 README 和面试材料之外的工程事实索引。

## 1. API 兼容层

Gateyes 对外提供多种 LLM API surface，并在内部收敛到 `provider.ResponseRequest`：

| Surface | 路由 | 说明 |
| --- | --- | --- |
| OpenAI Responses | `POST /v1/responses` | 主路径，支持 JSON 和 SSE streaming |
| OpenAI Chat Completions | `POST /v1/chat/completions` | 兼容 OpenAI SDK |
| Anthropic Messages | `POST /v1/messages` | 兼容 Anthropic SDK，保留 thinking/cache_control 语义 |
| Embeddings | `POST /v1/embeddings` | 选择支持 embeddings 的 provider |
| Images | `POST /v1/images/generations` | 选择支持 images 的 provider |
| Service runtime | `POST /service/:prefix/*` | 基于 prompt/service catalog 的业务级入口 |

`GET /v1/models` 会返回当前租户可用模型、provider、健康状态和能力 catalog。

## 2. Provider 与路由

- Provider 类型：OpenAI-compatible、Anthropic-compatible、Azure-compatible。
- Provider registry：记录 enabled/drain/health/capabilities/pricing/routing weight。
- Model aliases：允许客户端使用 `claude-sonnet-*` 等外部模型名，由 provider 转成实际上游模型。
- 路由策略：round-robin、least-load、least-TPM、cost-based、sticky、rule engine。
- 路由过滤：provider scope、model scope、surface capability、stream/tools/images/structured-output capability、health/drain。
- Fallback/retry/circuit breaker：provider 调用失败时按候选列表重试和切换。

## 3. 认证、权限与租户治理

- API Key：`Authorization: Bearer <key>:<secret>`，secret 哈希存储。
- Virtual Key：支持项目/服务侧细分 key，继承父 API Key 权限边界。
- RBAC：内置角色 + 自定义角色/权限，admin API 使用 permission middleware。
- OIDC：后台管理支持登录、callback、refresh、logout。
- Tenant/Project/User：支持 CRUD、状态、quota 和用量查询。

## 4. 预算、限流与成本

- 多级预算链：virtual key -> API key -> project -> tenant。
- 预算策略：hard reject、soft alert、grace。
- 限流维度：全局、租户、用户、provider、model，支持 Redis Lua 分布式限流和内存 fallback。
- 成本计算：优先 provider 配置价格，其次 pricing feed；响应完成后写 usage 和扣减预算。

## 5. 缓存与 token 优化

- L1 response cache：支持 memory / Redis fallback，非流式与流式分层。
- Cache hints：支持请求头跳过缓存、设置 bucket、设置 TTL。
- Singleflight：相同 cache key 的并发 miss 只打一笔上游请求。
- Prompt rewrite：参考 `gt_ai_gateway` 的缓存优化思路，内置清理动态 prompt 干扰：
  - Anthropic system prompt 中 `x-anthropic-billing-header` 的 `cch` 值归一化。
  - Claude Code date marker 格式稳定化。
  - OpenAI Chat/Responses 自动注入稳定 `prompt_cache_key`，除非客户端已经提供。
- 可观测：响应头、route trace、Dashboard cache summary、Playground cache badge 都能看到缓存状态。

## 6. 可观测性与审计

- Prometheus：请求数、延迟、TTFT、stream duration、tokens、cached tokens、retry/fallback、cache lookup/write、provider cache metrics。
- Tracing：W3C `traceparent` 传播，OTLP exporter。
- Audit log：admin 关键写操作落库，可在后台查询。
- Provider runtime stats：provider 当前负载、TPM、错误率、健康状态。

## 7. 插件与扩展

- WASM Gateway plugin：轻量请求/响应过滤、转换、cache hit 注入。
- gRPC Gateway plugin：跨进程插件，适合外部依赖和复杂逻辑。
- gRPC Router plugin：可替换 provider ordering。
- 生命周期 phase：`pre_route`、`post_route`、`pre_upstream`、`post_upstream`、`audit`。

## 8. Admin Frontend

`web/` 是 Vite + React + TanStack Router 的管理控制台，当前页面包括：

- Dashboard：总览、cache summary。
- Playground：Responses/Chat/Messages/Invoke 测试，支持 JSON/stream viewer、cache badge、rewrite/prompt cache key 标识。
- Providers：provider catalog、能力、健康、配置 CRUD。
- Keys / Virtual Keys：API key 与 virtual key 管理。
- Projects / Tenants / Users：多租户资源管理。
- Services：prompt service、版本、发布、回滚、订阅。
- Responses：响应详情、usage、route trace。
- Audit / Settings / Plugins：审计、系统配置入口、插件管理。

## 9. 运维交付

- 配置：`configs/config.yaml` / `configs/config.example.yaml`。
- 数据库迁移：`internal/repository/db/migrations`。
- 部署：Docker Compose、Helm chart、ingress template。
- Runbook：备份恢复、升级、回滚、CI/CD、secrets/config、vLLM cache experiment。

## 10. 当前边界

- L1 cache 是 exact-match response cache，不是 semantic cache。
- Embeddings 和 images 目前走 provider 选择，不走 responses.Service 的 L1 response cache。
- Provider 侧 prefix/KV cache 指标依赖上游暴露 `/metrics`。
- `pre_route` / `post_route` gateway plugin phase 当前只在流式路径完整触发；非流式路径主要使用 `pre_upstream` / `post_upstream` / `audit`。
