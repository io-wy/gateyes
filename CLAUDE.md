# Go Backend Starter - 项目级 AI Coding 约束

> 本文件是项目约束体系的第一层（项目宪法）。所有 AI 在本项目中工作必须遵守。

## 上下文体系

本项目的上下文分四层递进，按需加载：

| 层级 | 位置 | 内容 | 加载时机 |
|------|------|------|----------|
| L1 项目宪法 | `CLAUDE.md`（本文件） | 操作原则、编码约束、流程约束 | 每次会话 |
| L2 全局规则 | `~/.claude/rules/*.md` | 不可变性、测试要求、性能策略 | 每次会话 |
| L3 项目领域知识 | `docs/` | 架构决策、API 契约、运行时机制 | 涉及对应模块时 |
| L4 专业 Skill | `.claude/skills/` | Go 审查、变更影响扫描、协议开发、踩坑进化 | 任务级匹配 |

**加载规则**：遇到不确定的实现细节时，按 L3→L4 顺序查找，禁止凭训练记忆编造。

## 编码约束（反例免疫格式）

每条约束三要素：错误示例 + 正确示例 + Why。AI 看到边界，而不是理解期望。

### C-01：错误处理——禁止吞错误

```go
// WRONG: 吞掉错误
result, _ := db.Exec(query, args...)

// CORRECT: 显式处理并包装上下文
result, err := db.Exec(query, args...)
if err != nil {
    return fmt.Errorf("create record: %w", err)
}
```
Why: 吞错误会让故障定位从分钟级变成小时级。后端服务常驻运行，隐藏的错误会累积爆发。

### C-02：context 传递——禁止创建孤儿 context

```go
// WRONG: 忽略上游取消信号
ctx := context.Background()

// CORRECT: 传递上游 context，保证取消/超时链路完整
func (s *Service) Process(ctx context.Context) error {
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    // ...
}
```
Why: 用户断开连接后必须立即停止下游请求，否则浪费连接资源和计算。

### C-03：并发安全——禁止对共享状态裸读写

```go
// WRONG: 多 goroutine 裸读写 map
stats[name] = value

// CORRECT: 使用 sync.Mutex 或 sync.Map
s.mu.Lock()
stats[name] = value
s.mu.Unlock()
```
Why: 并发 bug 难复现难定位，在线上表现为偶发的数据竞争或 panic。

### C-04：资源释放——每个 Open/Close 必须配对

```go
// WRONG: 打开不关
resp, _ := http.Post(url, ct, body)

// CORRECT: defer 确保释放
resp, err := http.Post(url, ct, body)
if err != nil {
    return err
}
defer resp.Body.Close()
```
Why: 后端长运行，资源泄漏随时间累积最终 OOM。insert 必须有 cleanup，open 必须有 close。

### C-05：外部数据——所有入参必须校验

```go
// WRONG: 直接使用用户输入拼接 SQL
query := fmt.Sprintf("SELECT * FROM users WHERE id = %s", userID)

// CORRECT: 参数化查询 + 校验
if userID == "" {
    return ErrInvalidInput
}
row := db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = ?", userID)
```
Why: OWASP Top 1 注入攻击。所有来自 HTTP 请求的数据都不可信。

### C-06：魔法数字——必须提取为命名常量或配置

```go
// WRONG: 硬编码超时
client := &http.Client{Timeout: 60 * time.Second}

// CORRECT: 提取常量，最好从配置读取
const defaultTimeout = 60 * time.Second
// 或从 config: cfg.Server.Timeout
```
Why: 3 个月后你不知道 60 是什么。超时、限流阈值、重试次数等都是敏感参数。

### C-07：日志——热路径只打 Warn/Error

```go
// WRONG: 每个请求都打 Debug
log.Printf("processing request: %v", req)

// CORRECT: 热路径只打 Warn/Error，详细信息用 requestID 关联
if err != nil {
    slog.Error("request failed", "requestID", reqID, "error", err)
}
```
Why: 后端 QPS 高时，过多日志 I/O 成为性能瓶颈。用 requestID + 结构化日志替代全量打印。

### C-08：依赖引入——标准库 > 已有依赖 > 新依赖

引入新依赖前必须检查：
1. Go 标准库是否已有等价能力？
2. go.mod 中已有依赖是否已提供？
3. 如果必须引入，评估：维护活跃度、许可证、已知 CVE。

### C-09：测试——公共函数必须有测试

```go
// WRONG: 新增公共函数不写测试
func CalculatePrice(base float64, discount float64) float64 {

// CORRECT: 先写测试，再写实现
func TestCalculatePrice(t *testing.T) {
    got := CalculatePrice(100, 0.2)
    if got != 80 {
        t.Errorf("expected 80, got %f", got)
    }
}
```
Why: 没有测试的代码下次修改时无法确认行为是否正确。

### C-10：协议实现——必须对照参考文档，禁止漂移

```go
// WRONG: 凭记忆写协议字段
type Response struct {
    ID      string `json:"id"`
    Output  string `json:"output"`
    // 遗漏了 status, model, usage 等必需字段
}

// CORRECT: 逐字段对照参考文档
type Response struct {
    ID      string    `json:"id"`      // doc: unique identifier
    Object  string    `json:"object"`  // doc: always "response"
    Status  string    `json:"status"`  // doc: completed|failed|in_progress
    Model   string    `json:"model"`   // doc: model used
    Output  []Output  `json:"output"`  // doc: array of output items
    Usage   Usage     `json:"usage"`   // doc: token usage
    Created int64     `json:"created"` // doc: unix timestamp
}
```
Why: 协议漂移是最危险的 bug——上游/下游都按文档实现，你却偷偷改了行为，所有调用方都会出问题。详见 `protocol-driven-development` Skill。

## 流程阻塞约束

以下顺序不可跳过、不可并行：

```
功能实现 → 单元测试 → 代码审查 → 提交
```

- 功能 Task 完成前，不得开始测试 Task
- 测试未通过前，不得标记功能 Task 完成
- 代码审查未通过前，不得提交 commit
- AI 不可自主跳过任何阶段
- io-wy 明确要求跳过时，记录原因后执行

## 改不全预防

每次修改代码时必须执行：

1. **调用点扫描**：修改了函数签名/接口 → Grep 所有调用点，列影响范围后再改
2. **正反操作配对**：新增了 Open/Create/Insert/Start → 确认有对应的 Close/Destroy/Delete/Stop
3. **配置同步**：修改了代码中的配置项 → 检查 config struct、配置文件 example、docs 是否同步
4. **测试同步**：修改了实现逻辑 → 检查对应测试是否需要更新
5. **文档同步**：修改了公共接口 → 检查 API 文档、README 是否需要更新
6. **Migration 同步**：修改了数据模型 → 检查是否需要新增数据库 migration

执行方式：
- 单文件修改：自主检查，结果随 commit 输出
- 多文件修改：先列影响范围，io-wy 确认后再改
- 接口变更：Plan 模式，先出影响分析报告

## 不确定性声明

以下情况必须明确告知 io-wy「私密马赛，io-wy，我还有东西不知道」：

- API/函数签名：查证链（Grep→go.mod→Context7→WebSearch）走完仍无结果
- Go 版本兼容性：无法确认目标版本是否支持该 API
- 框架用法：无法确认当前版本行为
- 性能数据：没有实测数据支撑的性能断言
- 安全判断：无法确认某个模式是否存在安全风险

禁止行为：
- 禁止编造函数签名或方法
- 禁止编造 Go 标准库行为
- 禁止在无源码时声称「已分析源码」
- 禁止跳过实测直接用模式匹配推断行为
- 禁止编造 benchmark 数据

## 测试约束

- 回归口径：`go test ./...`
- 新增公共函数必须有对应测试
- HTTP handler 变更：用 `httptest.NewRecorder` 写集成测试
- 数据库变更：用 SQLite in-memory 或 mock 写测试
- 外部服务调用：mock 测试，不依赖真实上游
- 代码审查前必须 `go test ./...` 通过

## 文档生成约束

- AI 生成的文档/报告文件，单次任务不超过 5 个
- 超过 5 个文件时先告知 io-wy，征得同意后再生成
- 每个文件生成后展示路径和大小
- 临时分析文件放 `tmp/` 目录

## 多模型对抗式代码审查

触发条件（满足任一）：
- 变更涉及核心业务逻辑
- 变更文件数 >= 5
- 变更行数 >= 200
- io-wy 明确要求

流程：
1. 主模型（当前模型）自审一轮
2. 调用 `/codex-review` 进行独立审查
3. 交叉验证：两个模型都发现 → 高置信度
4. 仅单模型发现 → 标注「需 io-wy 确认」
5. 输出合并报告，按 Critical / Major / Minor / Suggestion 分级

## 踩坑进化闭环

触发条件：
- AI 生成了错误代码且被 io-wy 纠正
- AI 遗漏了边界条件导致 bug
- AI 使用了不存在的 API
- AI 改不全
- AI 在不确定时编造了信息

闭环步骤：
1. 记录 PIT-xxx 到 `.claude/skills/pitfall-journal.md`
2. 同类 PIT 出现 >= 2 次 → 提炼为本文件的新约束规则
3. 约束验证有效 >= 2 次 → 考虑固化为独立 Skill
4. 效果有限 → 标注「通用知识可覆盖」
