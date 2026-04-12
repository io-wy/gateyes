# Ollama / vLLM Provider 扩展设计

## 状态

Accepted

## 背景

Gateyes 当前 provider 抽象已经收敛到两类协议适配器：

- `openai`
- `anthropic`

主链路边界已经比较清晰：

- `handler` 只负责 HTTP / SSE surface
- `internal/protocol/apicompat` 负责外部协议转 canonical request / response
- `internal/service/responses` 负责编排、fallback、持久化、usage
- `internal/service/provider` 负责 canonical protocol 与上游 provider 协议互转

现在要扩展：

- `Ollama`
- `vLLM`

要求是继续保持“协议抽象优先”，而不是把 gateway 做成“按厂商品牌分叉”的转发器。

## 目标

1. 以最小架构代价接入 `Ollama` 与 `vLLM`
2. 不把 vendor-specific 逻辑泄漏到 `handler` / `responses.Service`
3. 尽量复用现有 `openai` / `anthropic` canonical adapter
4. 为后续 `lmstudio` / `sglang` / `xinference` 一类兼容 API 留出扩展位

## 非目标

1. 第一阶段不做 upstream 模型动态发现
2. 第一阶段不做 `Ollama native /api/chat` / `/api/generate` 全量支持
3. 第一阶段不把 provider 选择从“单 provider 单 model”改成“单 provider 多 model catalog”
4. 第一阶段不引入新的 handler surface

## 结论

推荐采用：

```text
Provider = protocol adapter + vendor profile
```

而不是：

```text
Provider = vendor brand
```

也就是：

- `vLLM` 先作为 `openai` provider 的一个 profile
- `Ollama` 先作为 `openai` / `anthropic` provider 的 profile
- 只有当未来要支持 `Ollama native API` 独有能力时，才新增 `ollama_native`

## 为什么这样更合理

### 1. vLLM 本质是 OpenAI-compatible server

vLLM 官方主入口就是 OpenAI-compatible API。对 Gateyes 来说，它不是一套新协议，而是：

- `openai + vendor=vllm`

所以不应该复制一个 `vllmProvider` 去重写：

- request 构建
- stream 解析
- responses/chat fallback
- tool calling
- `response_format`

### 2. Ollama 同时有 OpenAI / Anthropic compatibility

Ollama 不是单一协议。它既提供 native API，也提供：

- OpenAI compatibility
- Anthropic compatibility

因此第一阶段最合理的是：

- `type=openai, vendor=ollama`
- `type=anthropic, vendor=ollama`

而不是直接做单一 `type=ollama`

### 3. 当前 repo 的层次已经决定了 vendor 差异应该留在 provider 层

如果把 `vllm` / `ollama` 做成品牌型 provider class，很容易导致这些问题：

- `handler` 开始知道哪些 provider 支持哪些 surface
- `responses.Service` 开始知道某些 provider 的 fallback 规则
- `canonical content / event / output format` 在不同 provider 中重复实现

这和当前 repo 的架构方向冲突。

## 设计方案

### 1. provider 类型保持协议优先

第一阶段只保留：

- `openai`
- `anthropic`

预留但暂不实现：

- `ollama_native`

`internal/service/provider/manager.go` 的工厂分发原则不变：

- `Type` 决定走哪个协议 adapter
- `Vendor` 只影响 adapter 内的 profile 行为

### 2. ProviderConfig 扩展为 profile 配置

建议把 `internal/config/config.go` 的 `ProviderConfig` 扩展为：

```go
type ProviderConfig struct {
    Name        string            `yaml:"name"`
    Type        string            `yaml:"type"`      // openai | anthropic | ollama_native(未来)
    Vendor      string            `yaml:"vendor"`    // generic | openai | azure | vllm | ollama | minimax
    BaseURL     string            `yaml:"baseURL"`
    Endpoint    string            `yaml:"endpoint"`  // chat | responses | messages
    APIKey      string            `yaml:"apiKey"`
    Model       string            `yaml:"model"`
    Headers     map[string]string `yaml:"headers"`
    ExtraBody   map[string]any    `yaml:"extraBody"`
    PriceInput  float64           `yaml:"priceInput"`
    PriceOutput float64           `yaml:"priceOutput"`
    MaxTokens   int               `yaml:"maxTokens"`
    Timeout     int               `yaml:"timeout"`
    Enabled     bool              `yaml:"enabled"`
}
```

字段职责：

- `Type`: 协议适配器选择
- `Vendor`: vendor profile 选择
- `Headers`: provider-specific header 注入
- `ExtraBody`: provider-specific body passthrough

### 3. 新增 provider profile hook，不新增 handler 分支

建议新增：

- `internal/service/provider/profile.go`
- `internal/service/provider/profile_openai.go`
- `internal/service/provider/profile_anthropic.go`

对现有 adapter 只加 profile hook：

```go
func applyOpenAIProfile(cfg config.ProviderConfig, payload map[string]any, req *ResponseRequest)
func applyAnthropicProfile(cfg config.ProviderConfig, payload map[string]any, req *ResponseRequest)
func applyProviderHeaders(cfg config.ProviderConfig, httpReq *http.Request)
```

规则：

- `openai.go` 在构造 payload 后统一调用 `applyOpenAIProfile`
- `anthropic.go` 在构造 params 后统一调用 `applyAnthropicProfile`
- 所有 vendor-specific 小差异都收敛在这些 hook

### 4. 第一阶段仍然采用“一条 provider 配一个 model”

当前 `router` 是按 `Provider.Model()` 做优先匹配的。

因此第一阶段保持：

- 一个 upstream model 对应一条 provider 配置
- 同一个 `vLLM` / `Ollama` 实例托多个模型时，配置多条 provider，共享 `baseURL`

这样能避免立即改动：

- router
- `/v1/models`
- tenant provider ACL

## 各 provider 的接入方式

### A. vLLM

推荐接法：

```yaml
- name: vllm-qwen72b
  type: openai
  vendor: vllm
  baseURL: http://vllm:8000/v1
  endpoint: chat
  apiKey: token-abc123
  model: Qwen/Qwen2.5-72B-Instruct
  timeout: 120
  extraBody:
    parallel_tool_calls: false
  enabled: true
```

第一阶段支持面：

- `/v1/chat/completions`
- `/v1/responses`
- 非流 / 流式
- tool calling
- `response_format`

第一阶段不做：

- vLLM 专有 sampling / beam / guided decoding 的完整 typed config 抽象
- 模型自动发现

`vLLM` 相关 profile 建议：

1. 允许通过 `ExtraBody` 注入专有参数
2. 给常见 structured output 参数留 passthrough 能力
3. 对 “模型未配置 chat template” 这类错误做更可读的错误包装

### B. Ollama OpenAI compatibility

推荐接法：

```yaml
- name: ollama-qwen-openai
  type: openai
  vendor: ollama
  baseURL: http://ollama:11434/v1
  endpoint: chat
  apiKey: ollama
  model: qwen3-coder
  timeout: 120
  enabled: true
```

第一阶段支持面：

- `/v1/chat/completions`
- 能支持时再支持 `/responses`

注意：

- 官方文档里 OpenAI compatibility 有覆盖面，但 capability 不一定和 OpenAI 官方等价
- `response_format` / multimodal / tool calling 需要按 Ollama 版本与模型实测

### C. Ollama Anthropic compatibility

推荐接法：

```yaml
- name: ollama-qwen-anthropic
  type: anthropic
  vendor: ollama
  baseURL: http://ollama:11434
  endpoint: messages
  apiKey: ollama
  model: qwen3-coder
  timeout: 120
  enabled: true
```

第一阶段支持面：

- `/v1/messages`
- 非流 / 流式
- tool use / tool result

注意：

- 要按实际 Ollama 版本复测 `thinking`、tool use、streaming event 形状

### D. 未来的 Ollama native API

只在以下需求成立时再做：

1. 必须接 Ollama 独有能力
2. OpenAI / Anthropic compatibility 不能满足
3. 这些能力值得引入新的 canonical 映射

那时再加：

- `type: ollama_native`
- `internal/service/provider/ollama_native.go`

并明确写清：

- 它服务的是 Ollama native surface，不是现有 OpenAI / Anthropic surface 的变种

## 代码改造点

### 第一批

1. `internal/config/config.go`
   - 扩展 `ProviderConfig`

2. `internal/service/provider/manager.go`
   - `newProvider()` 仍按 `Type` 分发
   - 不新增 `vllmProvider`
   - 不新增 `ollamaProvider`

3. `internal/service/provider/openai.go`
   - merge `cfg.Headers`
   - merge `cfg.ExtraBody`
   - 调用 `applyOpenAIProfile()`

4. `internal/service/provider/anthropic.go`
   - merge `cfg.Headers`
   - merge `cfg.ExtraBody`
   - 调用 `applyAnthropicProfile()`

5. `docs/provider-protocol.md`
   - 增补 “provider = protocol adapter + vendor profile” 说明

### 第二批

6. 回归测试
   - `provider_extra_test.go`
   - `provider_integration_test.go`
   - `handler/e2e_test.go`

## 测试矩阵

### Phase 1 必测

#### vLLM

- chat non-stream
- chat stream
- tool call roundtrip
- json_schema / structured output
- 长文本

#### Ollama OpenAI compatibility

- chat non-stream
- chat stream
- tool call roundtrip
- 长文本

#### Ollama Anthropic compatibility

- messages non-stream
- messages stream
- tool_use -> tool_result -> final answer

### Phase 1 可接受边界

- `/v1/models` 仍然是配置视图，不是 upstream 发现
- 某些 multimodal / structured output capability 取决于模型与 upstream 版本，不做虚假承诺

## 风险与缓解

### 风险 1：把品牌差异写进 handler / service

后果：

- gateway 失去统一协议抽象
- 后续品牌越多越乱

缓解：

- 所有 vendor 分支只允许出现在 `internal/service/provider/profile*.go`

### 风险 2：把 vLLM / Ollama 误做成单独 provider type

后果：

- 重复实现 OpenAI / Anthropic 解析
- 测试矩阵膨胀

缓解：

- 第一阶段禁止新增 `vllmProvider`
- 第一阶段禁止新增 `ollamaProvider`

### 风险 3：过早做动态模型发现

后果：

- 要改 router / ACL / admin / `/v1/models`
- 范围失控

缓解：

- 第一阶段坚持“一 provider 一 model”

### 风险 4：provider-specific body 参数不断散落

后果：

- adapter 中出现大量 `if vendor == ...`

缓解：

- 用 `ExtraBody`
- 用 profile hook
- 不把特殊参数直接塞进 canonical `ResponseRequest`

## 实施顺序

### Phase 1

1. 扩展 `ProviderConfig`
2. 增加 profile hook
3. 支持 `type=openai, vendor=vllm`
4. 支持 `type=openai, vendor=ollama`
5. 补 OpenAI-compatible e2e regression

### Phase 2

6. 支持 `type=anthropic, vendor=ollama`
7. 补 Anthropic-compatible e2e regression

### Phase 3

8. 视需要评估 `ollama_native`
9. 视需要评估动态模型发现

## 验收标准

### 架构层

1. `handler` 不出现 `vllm` / `ollama` 条件分支
2. `responses.Service` 不出现 `vllm` / `ollama` 条件分支
3. provider 工厂仍以协议类型为主，而不是品牌类型为主

### 功能层

1. vLLM 通过 OpenAI-compatible 路径完成：
   - non-stream
   - stream
   - tool calling
   - structured output

2. Ollama 通过 OpenAI-compatible 路径完成：
   - non-stream
   - stream
   - tool calling

3. Ollama 通过 Anthropic-compatible 路径完成：
   - non-stream
   - stream
   - tool_use / tool_result 两轮闭环

## 推荐

直接按这个方案落地：

- 先做 `provider profile`
- 再做 `vllm + openai`
- 再做 `ollama + openai`
- 再做 `ollama + anthropic`

不要先做：

- `vllmProvider`
- `ollamaProvider`
- `ollama native API`
- 动态模型发现

这是当前 Gateyes 这棵树最稳、最不容易把协议抽象做坏的路径。
