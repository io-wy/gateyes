# Anthropic Messages API 测试文档

## 概述

本文档记录 Gateway 对 Anthropic Messages API (`/v1/messages`) 的测试验证。

## 测试环境

- Gateway 地址：`http://localhost:8083`
- 测试 Provider：MiniMax-M2.5
- API Key：`test-key-001:test-secret`

## API 接口

### 1. OpenAI 兼容接口

| 接口 | 方法 | 路径 |
|------|------|------|
| Chat Completions | POST | `/v1/chat/completions` |
| Responses | POST | `/v1/responses` |

### 2. Anthropic 兼容接口

| 接口 | 方法 | 路径 |
|------|------|------|
| Messages | POST | `/v1/messages` |

## 测试用例

### 1. OpenAI Chat Completions - 非流式

```bash
curl -s http://localhost:8083/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "say hi"}],
    "max_tokens": 50
  }'
```

**预期响应：**
```json
{
  "id": "...",
  "object": "chat.completion",
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "Hi! How can I help you today?"
    },
    "finish_reason": "stop"
  }]
}
```

**状态：** ✅ 通过

---

### 2. OpenAI Chat Completions - 流式

```bash
curl -s http://localhost:8083/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "say hi"}],
    "max_tokens": 50,
    "stream": true
  }'
```

**预期响应：**
```
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{}}]}
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hi!"}}]}
data: {"id":"...","object":"chat.completion.chunk","choices":[{"finish_reason":"stop"}]}
data: [DONE]
```

**状态：** ✅ 通过

---

### 3. Anthropic Messages - 非流式

```bash
curl -s http://localhost:8083/v1/messages \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "say hi"}],
    "max_tokens": 50
  }'
```

或使用 `X-Api-Key` header（Anthropic SDK 兼容）：

```bash
curl -s http://localhost:8083/v1/messages \
  -H "X-Api-Key: test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "say hi"}],
    "max_tokens": 50
  }'
```

**预期响应：**
```json
{
  "id": "...",
  "type": "message",
  "role": "assistant",
  "content": [
    {"type": "thinking"},
    {"type": "text", "text": "Hi! How can I help you today?"}
  ],
  "model": "MiniMax-M2.5",
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 43, "output_tokens": 39}
}
```

**状态：** ✅ 通过

---

### 4. Anthropic Messages - 流式

```bash
curl -s http://localhost:8083/v1/messages \
  -H "X-Api-Key: test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "say hi"}],
    "max_tokens": 50,
    "stream": true
  }'
```

**预期响应：**
```
data: {"type":"message_start","message":{...}}
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi!"}}
data: {"type":"message_delta","delta":"end_turn",...}
data: [DONE]
```

**注意：** `delta` 字段格式为对象 `{"type":"text_delta","text":"..."}`，符合 Anthropic SSE 规范。

**状态：** ✅ 通过

---

### 5. Tool Calling (函数调用)

```bash
curl -s http://localhost:8083/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [{"role": "user", "content": "What is 2+2?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "calculator",
        "description": "A calculator",
        "parameters": {
          "type": "object",
          "properties": {
            "expression": {"type": "string"}
          },
          "required": ["expression"]
        }
      }
    }]
  }'
```

**预期响应：**
```json
{
  "choices": [{
    "message": {
      "tool_calls": [{
        "id": "call_...",
        "type": "function",
        "function": {
          "name": "calculator",
          "arguments": "{\"expression\":\"2+2\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

**状态：** ✅ 通过

---

### 6. Multi-turn (多轮对话)

```bash
curl -s http://localhost:8083/v1/chat/completions \
  -H "Authorization: Bearer test-key-001:test-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MiniMax-M2.5",
    "messages": [
      {"role": "user", "content": "My name is Alice"},
      {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
      {"role": "user", "content": "What is my name?"}
    ],
    "max_tokens": 50
  }'
```

**预期响应：**
```json
{
  "choices": [{
    "message": {
      "content": "Your name is Alice, as you just told me!"
    }
  }]
}
```

**状态：** ✅ 通过

---

## 认证方式

Gateway 支持两种认证 header：

| Header | 格式 | 示例 |
|--------|------|------|
| `Authorization` | `Bearer key:secret` | `Authorization: Bearer test-key-001:test-secret` |
| `X-Api-Key` | `key:secret` | `X-Api-Key: test-key-001:test-secret` |

**说明：**
- `X-Api-Key` 用于兼容 Anthropic SDK
- 优先使用 `X-Api-Key`（避免代理环境变量干扰）

## SSE 格式说明

### Anthropic 流式响应格式

```json
{
  "type": "message_start",
  "message": {...}
}

{
  "type": "content_block_delta",
  "index": 0,
  "delta": {
    "type": "text_delta",
    "text": "Hello"
  }
}

{
  "type": "message_delta",
  "delta": "end_turn",
  "message": {
    "stop_reason": "end_turn",
    "usage": {"output_tokens": 10}
  }
}

{
  "type": "message_stop"
}

data: [DONE]
```

## 已知问题

### Python httpx 代理问题

使用 Anthropic Python SDK 时，如果设置了 `HTTP_PROXY` 或 `HTTPS_PROXY` 环境变量，httpx 会使用代理。代理可能会修改 Authorization header 为 `Bearer PROXY_MANAGED`，导致认证失败。

**解决方案：**
1. 清除代理环境变量：`unset HTTP_PROXY HTTPS_PROXY`
2. 或在代码中禁用代理：
```python
import httpx
client = anthropic.Anthropic(
    api_key='test-key-001:test-secret',
    http_client=httpx.Client(proxies={"http://": None, "https://": None})
)
```

---

## 测试结果汇总

| 测试项 | 状态 |
|--------|------|
| OpenAI Chat Completions 非流式 | ✅ |
| OpenAI Chat Completions 流式 | ✅ |
| Anthropic Messages 非流式 | ✅ |
| Anthropic Messages 流式 | ✅ |
| Tool Calling | ✅ |
| Multi-turn 对话 | ✅ |
