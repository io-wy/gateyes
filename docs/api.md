# AI Gateway API 文档

文档版本: `v1.1`  
更新时间: `2026-03-04`  
状态: `Draft / Ready for Implementation`

## 1. 概述

AI Gateway 提供 OpenAI 兼容接口，对接多上游（Anthropic / OpenAI / Gemini 等），统一完成：

- API Key 鉴权
- 模型路由与映射
- 三层调度（粘性会话 / 权重选择 / 等待回退）
- 并发控制（全局 / 渠道 / Token）
- 可观测性（Admin Metrics + Prometheus + Grafana）

设计参考: [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)

## 2. 通用约定

### 2.1 Base URL

- 默认: `http://localhost:8080`
- 所有网关接口以 `/v1` 开头
- 所有管理接口以 `/api/v1/admin` 开头

### 2.2 认证

Gateway 认证（必需）:

- `Authorization: Bearer <api_key>`
- 或 `x-api-key: <api_key>`

Admin 认证（生产建议）:

- 当前文档定义了完整 Admin API，但部署时应至少通过反向代理或网关层加管理员鉴权（OIDC / Basic Auth / IP 白名单）。

### 2.3 通用 Header

- `Content-Type: application/json`
- `x-session-id: <string>`（可选，缺省时网关生成并回传）
- `x-request-id: <string>`（可选，建议客户端传入用于链路追踪）

### 2.4 时间与单位

- 时间格式: RFC3339 / ISO8601（如 `2026-03-04T10:00:00Z`）
- 延迟单位: 毫秒（文档说明）/ 秒（Prometheus 度量）
- 计数类字段: 累积值

## 3. 统一错误响应

所有失败响应遵循如下结构：

```json
{
  "error": {
    "message": "错误描述",
    "type": "error_type",
    "code": "optional_error_code"
  }
}
```

常见 `type`:

- `invalid_request_error` 参数错误/格式错误
- `authentication_error` 鉴权失败
- `permission_error` 权限不足
- `rate_limit_error` 限流或并发超限
- `upstream_error` 上游服务错误
- `service_unavailable` 网关无可用渠道
- `wait_plan` 当前建议等待重试

常见状态码：

- `200` 成功
- `201` 创建成功
- `202` 接受请求（等待计划）
- `204` 删除成功
- `400` 参数错误
- `401` 未认证
- `403` 拒绝访问
- `404` 资源不存在
- `409` 资源冲突
- `429` 触发限流/并发限制
- `500` 网关内部错误
- `502` 上游错误
- `503` 暂不可用

## 4. Gateway API（OpenAI 兼容）

## 4.1 Chat Completions

`POST /v1/chat/completions`

请求示例：

```json
{
  "model": "claude-3-5-sonnet-20241022",
  "messages": [
    { "role": "user", "content": "Hello" }
  ],
  "stream": false,
  "max_tokens": 1024,
  "temperature": 0.7
}
```

说明：

- `model` 必填
- `messages` 必填
- `stream=true` 时返回 SSE（`text/event-stream`）
- 网关支持模型映射（客户端模型 -> 上游模型）

非流式响应示例：

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1699000000,
  "model": "claude-3-5-sonnet-20241022",
  "choices": [
    {
      "index": 0,
      "message": { "role": "assistant", "content": "Hello!" },
      "finish_reason": "stop"
    }
  ]
}
```

流式 SSE 示例（示意）：

```text
data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hel"}}]}

data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk","choices":[{"delta":{"content":"lo"}}]}

data: [DONE]
```

## 4.2 Embeddings

`POST /v1/embeddings`

请求示例：

```json
{
  "model": "text-embedding-3-small",
  "input": ["Hello world", "AI gateway"]
}
```

## 4.3 Models

`GET /v1/models`

响应示例：

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-3-5-sonnet-20241022",
      "object": "model",
      "created": 1677610602,
      "owned_by": "anthropic"
    }
  ]
}
```

## 5. Admin API

## 5.1 渠道管理（Channels）

### 创建渠道

`POST /api/v1/admin/channels`

请求示例：

```json
{
  "type": "anthropic",
  "key": "sk-ant-xxx",
  "base_url": "https://api.anthropic.com",
  "models": ["claude-3-5-sonnet-20241022"],
  "model_mapping": ["claude-3-5-sonnet-20241022:claude-3-5-sonnet-20241022"],
  "priority": 1,
  "weight": 100,
  "max_concurrency": 10,
  "enabled": true,
  "response_format": "claude"
}
```

字段说明：

- `type`: 渠道类型，`anthropic` / `openai` / `gemini`
- `key`: 上游 API Key
- `base_url`: 上游 API Base URL（可选）
- `models`: 支持模型列表
- `model_mapping`: 模型映射，格式 `client_model:upstream_model`
- `priority`: 优先级，越小越优先
- `weight`: 同优先级内权重
- `max_concurrency`: 渠道并发上限
- `enabled`: 是否启用
- `response_format`: 响应兼容格式，`openai` / `claude`

### 其他渠道接口

- `GET /api/v1/admin/channels` 列表（支持 `type/status` 过滤）
- `GET /api/v1/admin/channels/:id` 详情
- `PATCH /api/v1/admin/channels/:id` 更新
- `DELETE /api/v1/admin/channels/:id` 删除

## 5.2 Token 管理

- `POST /api/v1/admin/tokens` 创建
- `GET /api/v1/admin/tokens` 列表
- `DELETE /api/v1/admin/tokens/:id` 删除

创建示例：

```json
{
  "name": "my-key",
  "group": "default",
  "rate_limit": 100,
  "max_concurrency": 10,
  "allowed_models": [],
  "ip_whitelist": []
}
```

说明：`key` 仅在创建时返回一次。

## 5.3 调度配置

- `GET /api/v1/admin/scheduling`
- `PATCH /api/v1/admin/scheduling`

配置字段：

- `sticky_session_enabled`
- `sticky_session_ttl`（秒）
- `fallback_wait_timeout`（秒）
- `max_retries`

## 5.4 并发配置

- `GET /api/v1/admin/concurrency/config`
- `PATCH /api/v1/admin/concurrency/config`
- `GET /api/v1/admin/concurrency/status`

并发配置示例：

```json
{
  "global_limit": 1000,
  "per_token_limit": 50,
  "per_channel_limits": {
    "anthropic": 400,
    "openai": 400,
    "gemini": 200
  }
}
```

## 5.5 实时指标（Admin）

`GET /api/v1/admin/metrics`

响应示例：

```json
{
  "timestamp": "2026-03-04T10:00:00Z",
  "total_requests": 1000,
  "active_requests": 50,
  "total_tokens": 500000,
  "by_platform": {
    "anthropic": {
      "requests": 600,
      "tokens": 300000,
      "active_channels": 5,
      "total_channels": 8,
      "avg_load_rate": 45.5
    }
  }
}
```

## 6. 调度与并发语义

## 6.1 三层调度

Layer 1: Sticky Session

- 命中会话绑定渠道
- 验证渠道可用 + 模型可用 + 并发可用
- 成功则直接使用

Layer 2: Priority + Weight

- 过滤可用渠道
- 按 `priority` 从高到低（数值从小到大）
- 同层按 `weight` 随机
- 获取并发槽成功即返回

Layer 3: Fallback Wait Plan

- 无可用即时槽位时返回等待计划（`202` + `wait_plan`）
- 客户端按建议等待后重试

## 6.2 并发控制

推荐三层并发：

- 全局并发
- 渠道并发
- Token 并发

Redis Key 约定：

- `concurrency:global`
- `concurrency:channel:{id}`
- `concurrency:token:{id}`

获取/释放:

- 获取: `INCR`
- 释放: `DECR`
- 超限: `429 Too Many Requests`

## 7. 监控与 Grafana 契约

## 7.1 健康检查

- `GET /health`：进程健康（轻量）
- 规划建议新增：
- `GET /live`：存活探针
- `GET /ready`：就绪探针（检查 DB/Redis）

## 7.2 Prometheus 指标端点（规划）

- `GET /metrics`
- `Content-Type: text/plain; version=0.0.4`

核心指标（建议）：

- `ai_gateway_http_requests_total{method,route,status_class}`
- `ai_gateway_http_request_duration_seconds_bucket{method,route}`
- `ai_gateway_http_inflight_requests`
- `ai_gateway_upstream_requests_total{provider,model,result}`
- `ai_gateway_upstream_request_duration_seconds_bucket{provider,model}`
- `ai_gateway_scheduler_selected_total{layer,provider}`
- `ai_gateway_scheduler_wait_plan_total{provider,model}`
- `ai_gateway_concurrency_inuse{scope,scope_id}`
- `ai_gateway_concurrency_limit{scope,scope_id}`
- `ai_gateway_tokens_consumed_total{provider,model}`

说明：

- 高基数标签（例如完整 token id）不应直接暴露到公开指标，建议聚合或脱敏。

## 7.3 Grafana 看板建议

Dashboard 1: Gateway RED

- RPS（总/分路由）
- 错误率（4xx/5xx）
- 延迟 p50/p95/p99

Dashboard 2: Upstream & Scheduling

- 各 provider 成功率
- 各 provider 延迟分位
- 调度层命中占比（sticky / weight / wait）
- 等待计划次数趋势

Dashboard 3: Capacity

- 全局/渠道/Token 并发占用率
- 近 5 分钟并发峰值
- 资源饱和趋势

## 7.4 告警建议

- `5xx` 错误率 > 5% 持续 5 分钟（Critical）
- p95 延迟 > 2s 持续 10 分钟（Warning）
- `wait_plan` 比例 > 20% 持续 10 分钟（Warning）
- 可用渠道数为 0（Critical）
- Redis 或 PostgreSQL 不可用（Critical）

## 8. 安全建议

- 管理接口必须加鉴权与审计日志
- 永远不在日志中输出上游 API Key、用户 Token 明文
- 生产环境只允许 HTTPS
- 建议配置上游出口域名白名单（防 SSRF）

## 9. 兼容性与演进建议

- 保持 `/v1/*` OpenAI 兼容优先
- 对非兼容能力采用显式扩展路径（如 `/api/v1/admin/*`）
- 对破坏性变更使用新版本前缀（`/v2`）

## 10. 变更记录

- `2026-03-04`: 重构文档结构，补齐调度/并发/监控/Grafana 契约，新增 Prometheus 指标与告警建议。
