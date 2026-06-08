---
name: execute-with-review
version: 1.1.0
user-invocable: false
description: 按 Task 列表执行开发，每个 Task 走 9 步流程（读取→实现→Spec 审查→质量审查→编译→测试→提交），完成后自动执行 change-impact-scan 防改不全。Auto-triggered after plan is confirmed.
---

# Execute with Review

> 核心：每个 Task 不是「做完了」，而是经过 spec review + quality review 双重验证才算「做好了」。

## 触发条件

- `plan-to-tasks` 计划被 io-wy 确认后自动触发
- io-wy 说「开始执行」「跑任务」「execute」「run tasks」

## 执行模式

| 情况 | 模式 |
|------|------|
| 1-2 个简单 Task | 主 Agent 顺序执行 |
| 3-5 个 Task | 2-3 个子 Agent 并行（独立 Task 并行，有依赖的顺序执行） |
| >5 个 Task | 分批，每批 3-5 个，批间 compact |

## 每个 Task 的 7 步流程

### Step 1：读取当前状态

- Read 目标文件（重新读确认最新，不凭之前的记忆）
- 确认前置 Task 已完成、阻塞关系满足

### Step 2：实施变更

- 按计划精确修改，遵守 CLAUDE.md C-01 到 C-10
- 实际代码与计划不符（行号偏移等）时先确认再改

### Step 3：Spec Review（不可跳过）

逐条对照计划的变更描述：

- [ ] 每个变更点都已实现
- [ ] 无遗漏
- [ ] 行为符合验证标准

结果：PASS → Step 4 / PARTIAL → 补齐后重审 / FAIL → 回 Step 2

### Step 4：Code Quality Review（不可跳过）

**Critical**（必须修）：
- [ ] 错误处理完整，无 `_ =`
- [ ] 资源配对：Open/Close、Lock/Unlock
- [ ] 并发安全：共享状态有保护
- [ ] 注入风险：用户输入有校验

**Major**（建议修）：
- [ ] context 传递完整
- [ ] 无魔法数字
- [ ] 函数 <=80 行，嵌套 <=4 层

**Minor**（列出供 io-wy 决定）：
- [ ] Go 命名惯用法
- [ ] import 分组
- [ ] 公开函数有 godoc

### Step 5：编译验证

```bash
go build ./... && go vet ./...
```

不通过则修复后回 Step 3。

### Step 6：测试验证

```bash
go test ./...
```

- 本次 Task 引起的失败 → 修复代码回 Step 3
- 已有测试问题 → 告知 io-wy

### Step 7：提交

Conventional Commits 格式（feat/fix/refactor/chore/docs/test），展示 message 给 io-wy 确认后提交。

## 改不全检查

每个 Task 完成后额外检查：

1. 修改了函数签名？→ `grep -rn "funcName" internal/` 扫描调用点
2. 新增了资源分配？→ 确认有对应释放
3. 修改了配置结构？→ 检查 config YAML example 同步
4. 修改了 API 行为？→ 检查文档同步
5. 修改了数据模型？→ 检查 migration 需要

## 输出

每个 Task 完成后输出一行：

```
Task N: <名称> | Spec: PASS | Quality: PASS (0C/0M/2m) | Build: OK | Test: OK | Commit: abc1234
```

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 跳过 spec review | 实现偏离计划 | Step 3 不可跳过 |
| 跳过 quality review | 引入并发 bug/资源泄漏 | Step 4 不可跳过 |
| 不跑测试就提交 | CI 红了才发现 | Step 6 强制 go test |

## 验证清单

- [ ] 每个 Task 都走完 7 步
- [ ] Spec review 全部 PASS
- [ ] Quality review 无 Critical/Major 残留
- [ ] go build + go test 通过
- [ ] 改不全检查完成
