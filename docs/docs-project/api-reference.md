# Gateyes API 请求体 / 响应体参考

本文档按实际路由注册整理。除特别说明外，管理接口推荐使用 `/admin/v1/*`；`/admin/*` 是兼容别名，路径和请求/响应体一致。

## 1. 通用约定

### 鉴权

业务接口和大多数管理接口使用：

```http
Authorization: Bearer <key>:<secret>
Content-Type: application/json
```

OIDC 登录、回调、refresh 状态接口按各自说明使用。

### 通用成功响应

```json
{
  "success": true,
  "data": {}
}
```

列表接口通常返回：

```json
{
  "success": true,
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 100
  }
}
```

### 通用错误响应

```json
{
  "success": false,
  "error": {
    "code": "bad_request",
    "message": "invalid request"
  }
}
```

## 2. 健康检查与指标

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/health` | 无 | `{"status":"ok"}` |
| `GET` | `/ready` | 无 | `{"status":"ready"}` |
| `GET` | `/metrics` | 无 | Prometheus text exposition |

## 3. LLM 兼容接口

### `POST /v1/responses`

请求体：

```json
{
  "model": "gpt-4o-mini",
  "input": "hello",
  "instructions": "optional system instruction",
  "stream": false,
  "temperature": 0.2,
  "max_output_tokens": 1024,
  "metadata": {
    "trace": "demo"
  }
}
```

响应体：

```json
{
  "id": "resp_xxx",
  "object": "response",
  "created_at": 1720000000,
  "model": "gpt-4o-mini",
  "status": "completed",
  "output": [
    {
      "type": "message",
      "role": "assistant",
      "content": [
        {
          "type": "output_text",
          "text": "hello"
        }
      ]
    }
  ],
  "usage": {
    "input_tokens": 12,
    "output_tokens": 8,
    "total_tokens": 20
  }
}
```

### `GET /v1/responses/:id`

请求体：无

响应体：

```json
{
  "id": "resp_xxx",
  "object": "response",
  "status": "completed",
  "model": "gpt-4o-mini",
  "output": []
}
```

### `POST /v1/chat/completions`

请求体：

```json
{
  "model": "gpt-4o-mini",
  "messages": [
    {
      "role": "user",
      "content": "hello"
    }
  ],
  "stream": false,
  "temperature": 0.2,
  "max_tokens": 1024,
  "tools": []
}
```

响应体：

```json
{
  "id": "chatcmpl_xxx",
  "object": "chat.completion",
  "created": 1720000000,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "hello"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 8,
    "total_tokens": 20
  }
}
```

### `POST /v1/messages`

请求体：

```json
{
  "model": "claude-3-5-sonnet-latest",
  "max_tokens": 1024,
  "system": "optional system prompt",
  "messages": [
    {
      "role": "user",
      "content": "hello"
    }
  ],
  "stream": false,
  "tools": []
}
```

响应体：

```json
{
  "id": "msg_xxx",
  "type": "message",
  "role": "assistant",
  "model": "claude-3-5-sonnet-latest",
  "content": [
    {
      "type": "text",
      "text": "hello"
    }
  ],
  "stop_reason": "end_turn",
  "usage": {
    "input_tokens": 12,
    "output_tokens": 8
  }
}
```

### `POST /v1/embeddings`

请求体：

```json
{
  "model": "text-embedding-3-small",
  "input": "hello"
}
```

响应体：

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.01, 0.02]
    }
  ],
  "model": "text-embedding-3-small",
  "usage": {
    "prompt_tokens": 2,
    "total_tokens": 2
  }
}
```

### `POST /v1/images/generations`

请求体：

```json
{
  "model": "gpt-image-1",
  "prompt": "a small red cube",
  "size": "1024x1024",
  "n": 1,
  "response_format": "url"
}
```

响应体：

```json
{
  "created": 1720000000,
  "data": [
    {
      "url": "https://example.com/image.png"
    }
  ]
}
```

### `GET /v1/models`

请求体：无

查询参数：

| 参数 | 说明 |
| --- | --- |
| `surface` | 可选：`responses`、`chat`、`messages`、`embeddings`、`images` |
| `stream` | 可选：`true` 时只返回支持 stream 的模型/provider |

响应体：

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "owned_by": "openai",
      "provider": "openai-primary",
      "capabilities": {
        "responses": true,
        "stream": true
      },
      "labels": {
        "runtime": "vllm"
      }
    }
  ]
}
```

## 4. Service 前缀接口

这些接口使用 `/service/:prefix/*`，先按 `request_prefix` 找到已发布 service，再复用 LLM 请求链路。

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `POST` | `/service/:prefix/responses` | 同 `/v1/responses`，可省略 `model` | 同 `/v1/responses` |
| `POST` | `/service/:prefix/chat/completions` | 同 `/v1/chat/completions`，可省略 `model` | 同 `/v1/chat/completions` |
| `POST` | `/service/:prefix/messages` | 同 `/v1/messages`，可省略 `model` | 同 `/v1/messages` |
| `POST` | `/service/:prefix/invoke` | service config 定义的自由 JSON | service config 定义的自由 JSON |

## 5. Batch API

### `POST /v1/batches`

请求体：

```json
{
  "endpoint": "/v1/responses",
  "model": "gpt-4o-mini",
  "completion_window": "24h",
  "metadata": {
    "job": "nightly"
  },
  "requests": [
    {
      "custom_id": "item-1",
      "body": {
        "input": "hello"
      }
    }
  ]
}
```

响应体：

```json
{
  "success": true,
  "data": {
    "id": "batch_xxx",
    "status": "queued",
    "endpoint": "/v1/responses",
    "model": "gpt-4o-mini",
    "total_items": 1,
    "completed_items": 0,
    "failed_items": 0
  }
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/v1/batches` | 无 | batch job 列表 |
| `GET` | `/v1/batches/:id` | 无 | 单个 batch job |
| `POST` | `/v1/batches/:id/cancel` | 无 | 更新后的 batch job |
| `GET` | `/v1/batches/:id/items` | 无 | batch item 列表 |

## 6. Admin API

### Dashboard / Catalog / Cache / Audit

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/dashboard` | 无 | usage、provider、budget 汇总 |
| `GET` | `/admin/v1/catalog` | 无 | 已发布 service catalog |
| `GET` | `/admin/v1/cache/summary` | 无 | cache layer summary |
| `GET` | `/admin/v1/audit` | 无 | audit log 列表 |

### Provider

创建请求体：

```json
{
  "name": "openai-primary",
  "type": "openai",
  "vendor": "openai",
  "base_url": "https://api.openai.com/v1",
  "endpoint": "responses",
  "api_key": "sk-***",
  "model": "gpt-4o-mini",
  "routing_weight": 10,
  "price_input": 0.000005,
  "price_output": 0.000015,
  "timeout": 60,
  "enabled": true,
  "labels": {
    "tier": "fast"
  },
  "supports_responses": true,
  "supports_stream": true
}
```

更新请求体：同创建请求体，但所有字段可选；额外支持 `drain`、`health_status`。

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/providers` | 无 | provider 列表 |
| `POST` | `/admin/v1/providers/check` | 无 | provider 健康检查结果 |
| `POST` | `/admin/v1/providers` | provider 创建请求体 | provider |
| `GET` | `/admin/v1/providers/:name` | 无 | provider |
| `GET` | `/admin/v1/providers/:name/stats` | 无 | provider runtime stats |
| `PUT` | `/admin/v1/providers/:name` | provider 更新请求体 | provider |
| `DELETE` | `/admin/v1/providers/:name` | 无 | 删除结果 |

### Tenant

创建请求体：

```json
{
  "id": "tenant-a",
  "slug": "tenant-a",
  "name": "Tenant A",
  "budget_usd": 100,
  "policy": {
    "enabled": true
  }
}
```

更新请求体：

```json
{
  "name": "Tenant A Updated",
  "status": "active",
  "budget_usd": 200,
  "budget_policy": "hard_reject",
  "policy": {
    "enabled": true
  }
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/tenants` | 无 | tenant 列表 |
| `POST` | `/admin/v1/tenants` | tenant 创建请求体 | tenant |
| `GET` | `/admin/v1/tenants/:id` | 无 | tenant |
| `PUT` | `/admin/v1/tenants/:id` | tenant 更新请求体 | tenant |
| `DELETE` | `/admin/v1/tenants/:id` | 无 | 删除结果 |
| `POST` | `/admin/v1/tenants/:id/providers` | `{"providers":["openai-primary"]}` | tenant provider 绑定结果 |

### User / API Key / Virtual Key

创建 user：

```json
{
  "tenant_id": "tenant-a",
  "project_id": "project-a",
  "name": "alice",
  "email": "alice@example.com",
  "role": "tenant_user",
  "quota": 1000000,
  "qps": 10,
  "key_budget_usd": 20,
  "models": ["gpt-4o-mini"]
}
```

创建 API key：

```json
{
  "user_id": "user_xxx",
  "project_id": "project_xxx",
  "budget_usd": 20,
  "rate_limit_qps": 10,
  "allowed_models": ["gpt-4o-mini"],
  "allowed_providers": ["openai-primary"],
  "allowed_services": ["svc-a"],
  "expires_at": "2026-12-31T23:59:59Z"
}
```

创建 virtual key：

```json
{
  "user_id": "user_xxx",
  "api_key_id": "key_xxx",
  "project_id": "project_xxx",
  "name": "demo-vk",
  "budget_usd": 10,
  "budget_policy": "hard_reject",
  "rate_limit_qps": 5,
  "allowed_models": ["gpt-4o-mini"],
  "allowed_providers": ["openai-primary"],
  "callback_url": "https://example.com/callback"
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/users` | 无 | user 列表 |
| `POST` | `/admin/v1/users` | user 创建请求体 | user |
| `GET` | `/admin/v1/users/:id` | 无 | user |
| `PUT` | `/admin/v1/users/:id` | user 更新请求体，字段可选 | user |
| `DELETE` | `/admin/v1/users/:id` | 无 | 删除结果 |
| `POST` | `/admin/v1/users/:id/reset` | 无 | reset 结果 |
| `GET` | `/admin/v1/users/:id/usage` | 无 | user usage |
| `GET` | `/admin/v1/keys` | 无 | API key 列表 |
| `POST` | `/admin/v1/keys` | API key 创建请求体 | API key，包含明文 secret，仅返回一次 |
| `GET` | `/admin/v1/keys/:id` | 无 | API key |
| `PUT` | `/admin/v1/keys/:id` | API key 更新请求体，字段可选 | API key |
| `POST` | `/admin/v1/keys/:id/rotate` | 无 | 新 key/secret |
| `POST` | `/admin/v1/keys/:id/revoke` | 无 | revoked key |
| `GET` | `/admin/v1/virtual-keys` | 无 | virtual key 列表 |
| `POST` | `/admin/v1/virtual-keys` | virtual key 创建请求体 | virtual key，包含明文 secret，仅返回一次 |
| `GET` | `/admin/v1/virtual-keys/:id` | 无 | virtual key |
| `PUT` | `/admin/v1/virtual-keys/:id` | virtual key 更新请求体，字段可选 | virtual key |
| `DELETE` | `/admin/v1/virtual-keys/:id` | 无 | 删除结果 |

### Project

创建请求体：

```json
{
  "tenant_id": "tenant-a",
  "slug": "project-a",
  "name": "Project A",
  "budget_usd": 100,
  "policy": {
    "enabled": true
  }
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/projects` | 无 | project 列表 |
| `POST` | `/admin/v1/projects` | project 创建请求体 | project |
| `GET` | `/admin/v1/projects/:id` | 无 | project |
| `GET` | `/admin/v1/projects/:id/usage` | 无 | project usage |
| `PUT` | `/admin/v1/projects/:id` | project 更新请求体，字段可选 | project |
| `DELETE` | `/admin/v1/projects/:id` | 无 | 删除结果 |

### Service / Subscription

创建 service：

```json
{
  "tenant_id": "tenant-a",
  "project_id": "project-a",
  "name": "coder",
  "request_prefix": "coder",
  "description": "coding service",
  "default_provider": "openai-primary",
  "default_model": "gpt-4o-mini",
  "enabled": true,
  "auto_publish": true,
  "config": {
    "surfaces": ["responses", "chat"],
    "metadata": {}
  }
}
```

发布请求体：

```json
{
  "version_id": "ver_xxx",
  "mode": "published"
}
```

订阅请求体：

```json
{
  "project_id": "project-a",
  "consumer_name": "team-a",
  "consumer_email": "team@example.com",
  "consumer_user_id": "user_xxx",
  "requested_budget_usd": 20,
  "requested_rate_limit_qps": 5,
  "allowed_surfaces": ["responses"]
}
```

订阅审核请求体：

```json
{
  "decision": "approve",
  "review_note": "ok"
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/services` | 无 | service 列表 |
| `POST` | `/admin/v1/services` | service 创建请求体 | service |
| `GET` | `/admin/v1/services/:id` | 无 | service |
| `PUT` | `/admin/v1/services/:id` | service 更新请求体，字段可选 | service |
| `DELETE` | `/admin/v1/services/:id` | 无 | 删除结果 |
| `GET` | `/admin/v1/services/:id/versions` | 无 | version 列表 |
| `POST` | `/admin/v1/services/:id/versions` | 无 | 新 version |
| `POST` | `/admin/v1/services/:id/publish` | 发布请求体 | service/version |
| `POST` | `/admin/v1/services/:id/promote` | 无 | promote 结果 |
| `POST` | `/admin/v1/services/:id/rollback` | `{"version_id":"ver_xxx"}` | rollback 结果 |
| `GET` | `/admin/v1/services/:id/subscriptions` | 无 | subscription 列表 |
| `POST` | `/admin/v1/services/:id/subscriptions` | 订阅请求体 | subscription |
| `GET` | `/admin/v1/subscriptions/:id` | 无 | subscription |
| `POST` | `/admin/v1/subscriptions/:id/review` | 订阅审核请求体 | subscription |

### Plugin

创建请求体：

```json
{
  "name": "audit-filter",
  "type": "wasm",
  "description": "audit request",
  "author": "gateyes",
  "phases": ["request", "response"],
  "address": "",
  "timeout_ms": 50,
  "memory_pages": 1,
  "enabled": true,
  "source": "custom",
  "config": {}
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/plugins` | 无 | plugin 列表 |
| `POST` | `/admin/v1/plugins` | plugin 创建请求体 | plugin |
| `POST` | `/admin/v1/plugins/upload` | multipart/form-data | plugin |
| `GET` | `/admin/v1/plugins/:id` | 无 | plugin |
| `PUT` | `/admin/v1/plugins/:id` | plugin 更新请求体，字段可选 | plugin |
| `DELETE` | `/admin/v1/plugins/:id` | 无 | 删除结果 |

### Response / Usage / Budget

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/responses` | 无 | response summary 列表 |
| `GET` | `/admin/v1/responses/:id` | 无 | response detail，含 request/response/route_trace |
| `GET` | `/admin/v1/responses/:id/trace` | 无 | route trace |
| `GET` | `/admin/v1/budgets` | 无 | tenant/project/key budget 列表 |
| `GET` | `/admin/v1/usage/summary` | 无 | usage 汇总 |
| `GET` | `/admin/v1/usage/breakdown` | 无 | 按 provider/model/project 等维度聚合 |
| `GET` | `/admin/v1/usage/trend` | 无 | 时间序列趋势 |

### Operator Sync

`POST /admin/v1/sync/router`

```json
{
  "strategy": "least_gpu_cache",
  "ruleEngine": {
    "enabled": true,
    "rules": []
  }
}
```

`POST /admin/v1/sync/budget`

```json
{
  "subject_kind": "tenant",
  "subject_name": "tenant-a",
  "budget_usd": 100,
  "budget_policy": "hard_reject",
  "rate_limit_qps": 10,
  "monthly_tokens": 1000000,
  "alert_thresholds": [0.5, 0.8, 0.95]
}
```

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `POST` | `/admin/v1/reload` | 无 | reload 结果 |
| `POST` | `/admin/v1/sync/router` | router config | sync 结果 |
| `POST` | `/admin/v1/sync/budget` | budget sync 请求体 | sync 结果 |

### RBAC

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/v1/roles` | 无 | role 列表 |
| `POST` | `/admin/v1/roles` | `{"name":"ops","description":"ops","permissions":["provider:read"]}` | role |
| `GET` | `/admin/v1/roles/:id` | 无 | role |
| `PUT` | `/admin/v1/roles/:id` | `{"name":"ops","description":"ops","permissions":["provider:read"]}` | role |
| `DELETE` | `/admin/v1/roles/:id` | 无 | 删除结果 |
| `GET` | `/admin/v1/permissions` | 无 | permission 列表 |

## 7. Admin OIDC

| 方法 | 路径 | 请求体 | 响应体 |
| --- | --- | --- | --- |
| `GET` | `/admin/auth/oidc/status` | 无 | OIDC 开关和 issuer/client 信息 |
| `GET` | `/admin/auth/oidc/login` | 无 | 跳转到 OIDC provider |
| `GET` | `/admin/auth/callback` | query: `code`、`state` | 登录结果或跳转 |
| `POST` | `/admin/auth/refresh` | `{"refresh_token":"token"}` | 新 token |
| `POST` | `/admin/auth/logout` | 无 | logout 结果 |
