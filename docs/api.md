# AI Gateway API 文档

文档版本: `v2-gateway-only`  
更新时间: `2026-03-05`  
状态: `Active`

## 1. 概述

当前仅保留网关链路，不包含账号管理、后台管理或登录能力。

- 健康检查：`/healthz`
- OpenAI 兼容网关：`/v1/*`

## 2. 通用约定

### 2.1 Base URL

- 默认: `http://localhost:8080`
- 网关接口前缀: `/v1`

### 2.2 认证

网关认证（可由环境变量控制是否启用）:

- `Authorization: Bearer <api_key>`
- 或 `x-api-key: <api_key>`

### 2.3 通用 Header

- `Content-Type: application/json`
- `x-session-id: <string>`（可选，缺省时网关生成并回传）
- `x-request-id: <string>`（可选，缺省时网关生成并回传）

## 3. 统一错误响应

所有失败响应结构：

```json
{
  "error": {
    "message": "错误描述",
    "type": "error_type",
    "code": "optional_error_code"
  }
}
```

常见 `type`：

- `invalid_request_error`
- `authentication_error`
- `rate_limit_error`
- `service_unavailable`
- `internal_error`

## 4. 健康检查

`GET /healthz`

响应示例：

```json
{
  "status": "ok",
  "time": "2026-03-05T12:00:00Z",
  "build": {
    "version": "",
    "commit": "unknown",
    "date": "unknown",
    "build_type": "source"
  }
}
```

## 5. Gateway API（OpenAI 兼容）

### 5.1 Models

`GET /v1/models`

### 5.2 Chat Completions

`POST /v1/chat/completions`

请求示例：

```json
{
  "model": "gpt-4o-mini",
  "messages": [
    { "role": "user", "content": "Hello" }
  ],
  "stream": false
}
```

### 5.3 Embeddings

`POST /v1/embeddings`

请求示例：

```json
{
  "model": "text-embedding-3-small",
  "input": ["Hello world"]
}
```
