# Provider 协议抽象

本文档记录 Gateyes 当前实际落地的 provider canonical protocol。这里写的是代码真相，不是目标设计稿。

## 1. 总体链路

Gateyes 不直接把上游协议透传给客户端，而是先收敛到内部 canonical model，再按客户端协议重新编码：

```text
HTTP request (OpenAI / Anthropic)
  -> internal/protocol/apicompat
  -> provider.ResponseRequest
  -> provider.Provider
  -> provider.ResponseEvent / provider.Response
  -> handler + apicompat encoder
  -> HTTP response (OpenAI / Anthropic)
```

这条链路的边界是：

- `apicompat` 负责外部协议 <-> canonical model
- `provider` 负责 canonical model <-> 上游 provider 协议
- `handler` 只负责 surface 选择、SSE 输出和错误包装

## 2. Canonical Request Model

当前内部请求类型是 `internal/service/provider/types.go` 的 `ResponseRequest`：

```go
type ResponseRequest struct {
    Model           string
    Input           any
    Messages        []Message
    Stream          bool
    MaxOutputTokens int
    MaxTokens       int
    Tools           []any
    OutputFormat    *OutputFormat
    Extra           map[string]any
}
```

关键点：

- `Input` 是原始入口载荷，可能来自 `/v1/responses`、`/v1/chat/completions` 或 `/v1/messages`
- `Messages` 是规范化后的 canonical message 列表
- `OutputFormat` 是 canonical `response_format`
- `Extra` 存放当前不适合直接放入公共字段的协议扩展，目前主要是 `system`、`thinking`、`cache_control`
- `responses.Service.buildUpstreamRequest()` 现在会保留 `OutputFormat` 和 `Extra`，不会在 service 主链路丢失

### 2.1 Canonical Message / Content

内部消息类型：

```go
type Message struct {
    Role       string
    Content    []ContentBlock
    Type       string
    Name       string
    ToolCallID string
    ToolCalls  []ToolCall
}
```

`Message.Content` 已经不是 `any`，而是显式的 `[]ContentBlock`。当前 canonical block 类型如下：

| Block type | 含义 | 主要字段 |
| --- | --- | --- |
| `text` | 普通可见文本输入 | `Text` |
| `thinking` | 非最终可见推理内容 | `Thinking`, `Signature` |
| `refusal` | 拒答内容 | `Refusal` |
| `image` | 图片输入 | `Image` |
| `structured_output` | 结构化 JSON 负载 | `Structured` |

辅助结构：

```go
type ContentImage struct {
    SourceType string
    URL        string
    MediaType  string
    Data       string
    Detail     string
}

type StructuredContent struct {
    Format string
    Data   map[string]any
    Raw    json.RawMessage
}
```

### 2.2 规范化规则

`NormalizeMessageContent()` / `Message.UnmarshalJSON()` 会把不同外部协议的 `content` 统一成 `[]ContentBlock`：

- `string` -> `[]ContentBlock{{Type:"text"}}`
- OpenAI chat `[{type:"text"}, {type:"image_url"}]` -> `text` + `image`
- OpenAI responses `input_text` / `output_text` -> `text`
- `thinking` -> `thinking`
- `refusal` -> `refusal`
- `structured_output` / `json` -> `structured_output`
- `image_url` / `image` -> `image`
- `function_call` / `function_call_output` 不进入 `Content`，而是进 `ToolCalls` / `ToolCallID`

这意味着：

- 外部 wire format 仍然可以是 `string`、`object`、`array`
- 内部核心路径只看 `[]ContentBlock`

## 3. Canonical Output Model

内部响应类型：

```go
type Response struct {
    ID      string
    Object  string
    Created int64
    Model   string
    Status  string
    Output  []ResponseOutput
    Usage   Usage
}

type ResponseOutput struct {
    ID      string
    Type    string
    Role    string
    Status  string
    Content []ResponseContent
    CallID  string
    Name    string
    Args    string
}
```

`ResponseOutput.Type` 当前主类型：

- `message`
- `function_call`

`ResponseContent` 与 canonical block 对齐：

```go
type ResponseContent struct {
    Type       string
    Text       string
    Thinking   string
    Signature  string
    Refusal    string
    Image      *ContentImage
    Structured *StructuredContent
}
```

当前实现上的真实语义：

- `Response.OutputText()` 只拼接最终可见文本：`Text` + `Refusal`
- `thinking` 不会被当成可见文本返回给 OpenAI chat 兼容层
- `function_call` 统一收敛到 `ResponseOutput{Type:"function_call"}`

## 4. Canonical Output Format

`response_format` 的内部模型是：

```go
type OutputFormat struct {
    Type   string
    Name   string
    Strict bool
    Schema map[string]any
    Raw    map[string]any
}
```

当前行为：

- OpenAI-compatible `response_format` 会先规范化为 `OutputFormat`
- `json_schema` 会提取 `name` / `strict` / `schema`
- 原始 payload 仍保留在 `Raw`
- OpenAI provider 发上游请求时直接透传 `Raw`
- Anthropic provider 当前不消费 `OutputFormat`

## 5. Canonical Event Model

内部流式事件不再使用旧的 `response.output_text.delta` / `chat.delta` 作为核心事件名，而是统一成：

| Canonical event | 含义 |
| --- | --- |
| `response_started` | 流启动，携带初始 `Response` 壳 |
| `content_delta` | 文本增量，或携带同 chunk 的 tool call |
| `tool_call_done` | 单个工具调用完整完成 |
| `response_completed` | 流结束，携带完整 `Response` |
| `thinking_delta` | 预留事件，当前主链路未正式输出 |

对应类型：

```go
type ResponseEvent struct {
    Type         string
    Delta        string
    Response     *Response
    Output       *ResponseOutput
    ToolCalls    []ToolCall
    FinishReason string
    Usage        *Usage
}
```

### 5.1 对外协议映射

对外 SSE 仍然保持客户端协议原生形状：

- `/v1/responses`：
  - `response_started` -> `response.created`
  - `content_delta.Delta` -> `response.output_text.delta`
  - `content_delta.ToolCalls` / `tool_call_done` -> `response.output_item.done`
  - `response_completed` -> `response.completed`
- `/v1/chat/completions`：
  - 通过 `ChatStreamEncoder` 重新编码成 `chat.completion.chunk`
  - `role: assistant` 只会发送一次
  - `content` 与 `tool_calls` 不在同一 chunk 混发
- `/v1/messages`：
  - 通过 `AnthropicStreamEncoder` 重新编码成 `message_start` / `content_block_*` / `message_delta` / `message_stop`

## 6. Provider 适配真相

### 6.1 OpenAI Provider

文件：`internal/service/provider/openai.go`

当前支持两种上游入口：

- `Endpoint=responses` -> `POST /responses`
- `Endpoint=chat` 或自定义 OpenAI-compatible chat path -> `POST /v1/chat/completions`

请求映射：

| Canonical block | OpenAI responses input | OpenAI chat input |
| --- | --- | --- |
| `text` | `input_text` | `text` 或纯字符串 |
| `thinking` | 降级为 `input_text` | 降级为文本 |
| `refusal` | 降级为 `input_text` | 降级为文本 |
| `image` | `input_image` | `image_url` |
| `structured_output` | 降级为 JSON 字符串文本 | 降级为文本 |

当前实现还会：

- 在 `responses` 和 `chat` 两条 OpenAI-compatible 路径都透传 `response_format`
- 在 `responses` 和 `chat` 路径都透传 `tools`
- 自动识别上游返回是 chat completions 还是 responses API

响应解析：

- Responses API：
  - `message.content[].type=output_text` -> `ResponseContent{Type:"output_text"}`
  - `message.content[].type=refusal` -> `ResponseContent{Type:"refusal"}`
  - `function_call` -> `ResponseOutput{Type:"function_call"}`
- Chat Completions：
  - 主要解析 `message.content` / `delta.content`
  - `tool_calls` 会规范化成 canonical tool call

### 6.2 Anthropic Provider

文件：`internal/service/provider/anthropic.go`

请求映射：

- `system` / `developer` message 会被提取到顶层 `system`
- `Extra["thinking"]` / `Extra["cache_control"]` 会传给 Anthropic params
- OpenAI 风格 `tools` 会转换成 Anthropic `tools`
- `tool` role 会转成 `tool_result`
- Anthropic 入站 `content[].type=tool_result` 会 canonicalize 成内部 `role=tool + tool_call_id`

内容块映射：

| Canonical block | Anthropic messages block |
| --- | --- |
| `text` | `text` |
| `thinking` | `thinking` |
| `image` | `image` |
| `refusal` | 当前降级为普通 `text` |
| `structured_output` | 当前降级为 JSON 字符串 `text` |

响应解析现状：

- 非流式 `message.content[].type=thinking` 会保留成 `ResponseContent{Type:"thinking"}`
- `tool_use` 会保留成 `function_call`
- 流式主路径当前稳定处理的是 `text` 和 `tool_use`
- `thinking_delta` 常量已保留，但 handler / encoder 还没有把它作为独立 SSE 事件输出

## 7. 当前边界

这几个边界需要明确写出来：

- canonical content model 已经显式化，但并不是每个上游协议都原生支持所有 block type
- 协议没有原生 typed block 的地方，当前实现会降级成文本，而不是造 provider-specific 内部字段
- `thinking_delta` 目前是保留位，不应在文档里写成已稳定对外协议
- `OutputFormat` 目前只有 OpenAI-compatible provider 会真正消费

## 8. 当前回归覆盖

本轮协议回归测试重点卡的是这些面：

- `internal/protocol/apicompat/protocol_regression_test.go`
  - OpenAI chat image input -> canonical `image`
  - `response_format=json_schema` -> canonical `OutputFormat`
  - refusal block -> chat compatibility 输出
  - thinking block -> anthropic compatibility 输出
- `internal/service/provider/provider_extra_test.go`
  - OpenAI 上游请求体中的 image input / `response_format`
  - OpenAI responses refusal block 解析
  - Anthropic non-stream thinking block 解析
- `internal/service/responses/service_test.go`
  - service 主链路不会丢 `OutputFormat` / `Extra`
