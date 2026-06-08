---
name: protocol-driven-development
version: 1.0.0
description: 协议驱动开发。写任何协议相关代码（API 契约、数据格式、通信协议、接口兼容层）时必须先找到并读取参考文档，逐字段对照实现，写完后与参考文档交叉验证，并产出覆盖完整字段和边界条件的测试文件。防止协议漂移。Triggered when implementing protocol/API/data-format code, interface compatibility layers, or when user says "协议开发/protocol/接口对接/API 契约/数据格式/通信协议/对照文档/协议漂移/protocol driven".
---

# Protocol Driven Development

> 核心：协议是最危险的代码——上游和下游都按文档实现，你如果凭记忆偷偷改了行为，所有调用方都会出问题。

## 触发条件

本 Skill 在以下场景自动触发：

- 新增或修改 API 接口（REST/gRPC/GraphQL）
- 新增或修改数据序列化格式（JSON/YAML/Protobuf）
- 新增或修改外部服务兼容层（如 OpenAI 兼容、Anthropic 兼容）
- 新增或修改消息协议（SSE 事件、WebSocket 帧、gRPC message）
- 新增或修改请求/响应结构体
- io-wy 提到「写接口」「协议」「API」「兼容」「adapter」「contract」

## 执行流程

### Phase 1：获取参考文档（不可跳过）

**在写任何代码之前**，必须先找到并读取权威参考文档。

#### 1.1 确定参考文档来源

| 协议类型 | 参考文档来源 | 获取方式 |
|----------|------------|----------|
| 公开 API（OpenAI/Anthropic/GitHub 等） | 官方 API 文档 | Context7 / WebFetch 官方文档页 |
| gRPC/Protobuf | `.proto` 文件 | Read 项目内的 proto 文件 |
| HTTP API | OpenAPI spec / Swagger | Read `docs/openapi.json` |
| 内部协议 | 项目 docs | Read `docs/provider-protocol.md` 等对应文件 |
| 数据格式 | JSON Schema / Go struct tag + 文档 | Read 定义文件 |
| 标准协议（HTTP/RFC） | RFC 原文 | Context7 / WebFetch |

#### 1.2 读取并记录

```
参考文档: [文档名称 + URL/路径]
读取时间: [时间]
关键字段数: [N]
必需字段: [列出]
可选字段: [列出]
枚举值: [列出所有合法值]
边界条件: [列出文档中提到的边界情况]
```

**禁止行为**：
- 禁止在未读取参考文档的情况下开始写协议代码
- 禁止凭训练记忆编写协议字段——训练数据可能过时
- 禁止只读了部分字段就开始实现

### Phase 2：逐字段实现

#### 2.1 实现约束

```markdown
### 协议实现约束

- WRONG: 凭记忆写字段名和类型
- CORRECT: 逐字段从参考文档复制，确认名称、类型、是否必需
- Why: 字段名大小写差一个字符就是 bug

- WRONG: 只实现当前需要的字段，忽略文档中的其他字段
- CORRECT: 文档中标记为 required 的字段必须全部实现；optional 字段至少声明为零值
- Why: 上游/下游依赖完整协议，缺少字段会导致解析失败

- WRONG: 自己发明文档中没有的字段
- CORRECT: 只实现文档中定义的字段，如需扩展用明确的 extension 字段
- Why: 额外字段可能和未来版本冲突

- WRONG: 用 string 代替枚举类型
- CORRECT: 定义 const 枚举，列出所有合法值
- Why: 枚举防止非法值传入，编译器帮你检查

- WRONG: 忽略文档中的 deprecated 标记
- CORRECT: 实现 deprecated 字段但标注 Go doc，新字段优先使用
- Why: 上游可能还在用旧字段，删除会破坏兼容性
```

#### 2.2 实现检查清单

对每个字段逐一确认：

- [ ] 字段名与参考文档完全一致（大小写）
- [ ] Go 类型与文档类型对应正确（string/int/float/bool/array/object）
- [ ] JSON tag 与文档字段名一致
- [ ] 必需/可选与文档一致
- [ ] 默认值与文档一致
- [ ] 枚举值完整覆盖文档中的所有合法值
- [ ] 嵌套结构体的字段也逐一确认

### Phase 3：交叉验证

实现完成后，**逐字段与参考文档对照**。

#### 3.1 验证方式

```
输出对照表：

| # | 文档字段 | 文档类型 | 代码字段 | 代码类型 | JSON tag | 匹配 |
|---|---------|---------|---------|---------|----------|------|
| 1 | id | string | ID | string | "id" | OK |
| 2 | object | string | Object | string | "object" | OK |
| 3 | status | enum | Status | string | "status" | MISMATCH: 缺少 "cancelled" 枚举值 |
```

#### 3.2 漂移检测

以下情况标记为漂移：

| 漂移类型 | 检测方法 |
|----------|----------|
| 字段缺失 | 文档有但代码没有 |
| 字段多余 | 代码有但文档没有（且不是 extension） |
| 类型不匹配 | 文档是 int 代码是 string |
| 名称不匹配 | JSON tag 与文档字段名不一致 |
| 枚举不完整 | 代码缺少文档中的某个枚举值 |
| 必需性不匹配 | 文档 required 但代码 optional，或反过来 |

**任何漂移都必须修复后再继续**，除非 io-wy 明确说「这个漂移是有意的」。

### Phase 4：编写测试文件

#### 4.1 测试覆盖要求

协议代码必须有独立测试文件，覆盖以下维度：

**A. 结构完整性测试**——验证所有必需字段存在

```go
func TestResponseStructCompleteness(t *testing.T) {
    // 逐字段检查：参考文档列出的 required 字段在 struct 中都存在
    r := Response{}
    _ = r.ID        // doc: required
    _ = r.Object    // doc: required
    _ = r.Status    // doc: required
    _ = r.Model     // doc: required
    _ = r.Output    // doc: required
    _ = r.Usage     // doc: required
    _ = r.Created   // doc: required
}
```

**B. JSON 序列化/反序列化测试**——验证 JSON tag 正确

```go
func TestResponseJSONRoundTrip(t *testing.T) {
    original := Response{
        ID:      "resp_001",
        Object:  "response",
        Status:  "completed",
        Model:   "gpt-4o",
        Created: 1713926400,
    }
    data, err := json.Marshal(original)
    require.NoError(t, err)

    var decoded Response
    err = json.Unmarshal(data, &decoded)
    require.NoError(t, err)

    assert.Equal(t, original.ID, decoded.ID)
    assert.Equal(t, original.Object, decoded.Object)
    assert.Equal(t, original.Status, decoded.Status)
}
```

**C. 枚举值测试**——验证所有合法值可解析

```go
func TestResponseStatusEnum(t *testing.T) {
    // 从参考文档列出所有合法值
    validStatuses := []string{"completed", "failed", "in_progress", "cancelled"}
    for _, status := range validStatuses {
        t.Run(status, func(t *testing.T) {
            jsonStr := fmt.Sprintf(`{"status": "%s"}`, status)
            var r Response
            err := json.Unmarshal([]byte(jsonStr), &r)
            require.NoError(t, err)
            assert.Equal(t, status, r.Status)
        })
    }
}
```

**D. 必需字段缺失测试**——验证缺失 required 字段时报错

```go
func TestResponseMissingRequiredFields(t *testing.T) {
    // 空对象应该无法通过业务校验
    jsonStr := `{}`
    var r Response
    err := json.Unmarshal([]byte(jsonStr), &r)
    // 如果用了 validator:
    err = r.Validate()
    assert.Error(t, err, "empty response should fail validation")
}
```

**E. 未知字段处理测试**——验证额外字段不会 panic

```go
func TestResponseUnknownFields(t *testing.T) {
    jsonStr := `{"id": "resp_001", "future_field": "value"}`
    var r Response
    err := json.Unmarshal([]byte(jsonStr), &r)
    require.NoError(t, err)
    assert.Equal(t, "resp_001", r.ID)
}
```

**F. 边界值测试**——验证空值/极大值/特殊字符

```go
func TestResponseBoundaryValues(t *testing.T) {
    tests := []struct {
        name string
        json string
    }{
        {"empty strings", `{"id":"","object":"","status":"completed"}`},
        {"long string", fmt.Sprintf(`{"id":"%s","status":"completed"}`, strings.Repeat("a", 10000))},
        {"unicode", `{"id":"中文测试","status":"completed"}`},
        {"null fields", `{"id":null,"status":null}`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var r Response
            err := json.Unmarshal([]byte(tt.json), &r)
            require.NoError(t, err, "should not panic on boundary input")
        })
    }
}
```

#### 4.2 测试命名规范

```
Test[StructName][TestCategory]

TestCategory:
- StructCompleteness    — 结构完整性
- JSONRoundTrip         — JSON 往返
- Enum                  — 枚举值覆盖
- MissingRequired       — 必需字段缺失
- UnknownFields         — 未知字段处理
- BoundaryValues        — 边界值
- Compatibility         — 兼容性（如同时支持新旧格式）
```

#### 4.3 测试文件位置

```
internal/service/protocol/
├── types.go              — 协议类型定义
├── types_test.go         — 结构完整性 + JSON 往返
├── enum_test.go          — 枚举值覆盖
├── boundary_test.go      — 边界值 + 未知字段
└── compatibility_test.go — 兼容性测试（如适用）
```

### Phase 5：兼容性声明

如果本次实现是现有协议的变更，必须输出兼容性声明：

```markdown
## 协议变更兼容性声明

### 新增字段
- `field_name`（可选）— 新增，不影响旧客户端

### 修改字段
- `field_name`（类型从 A 改为 B）— 破坏性变更，需要迁移

### 删除字段
- 无

### 默认值变更
- 无

### 影响范围
- 受影响的调用方：[列出]
- 需要同步修改的测试：[列出]
- 需要更新的文档：[列出]
```

**破坏性变更必须先经过 io-wy 确认，禁止自主执行。**

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 字段名拼写错误 | 序列化后对端无法识别 | Phase 3 交叉验证 |
| 遗漏 required 字段 | 对端解析报错 | Phase 2 检查清单 |
| 枚举值不完整 | 合法值被拒绝 | Phase 4C 枚举测试 |
| 类型不匹配 | 运行时 panic 或静默丢数据 | Phase 2 逐字段对照 |
| 凭记忆而非文档 | 训练数据过时导致实现过时 | Phase 1 必须读文档 |
| 只写正常路径测试 | 边界输入导致 panic | Phase 4F 边界值测试 |

## 禁止行为

- 禁止在未读取参考文档的情况下编写协议代码
- 禁止跳过交叉验证直接提交
- 禁止只写 happy path 测试
- 禁止自主引入文档中没有的 breaking change
- 禁止把「我之前实现过类似的」当作参考——每次都必须读当前版本的文档
