---
name: plan-to-tasks
version: 1.1.0
user-invocable: false
description: 将设计方案拆分为可执行 Task 列表。每个 Task 包含 7 要素（文件路径、变更描述、验证标准、阻塞关系、复杂度、关联指标、动词+名词标题）。Auto-triggered after brainstorming is confirmed.
---

# Plan to Tasks

> 核心：「优化性能」不是 Task。「在 internal/service/handler.go:45 将超时从硬编码改为配置读取」才是。

## 触发条件

- `brainstorming-with-context` 方案被确认后自动触发
- io-wy 说「拆任务」「出计划」「make a plan」「break down」

## Task 规范（7 要素）

```markdown
### Task N: <动词>+<名词>

**文件路径**: internal/app/config/config.go:45, configs/config.example.yaml:22
**变更描述**:
  - 改之前：Timeout 硬编码为 60s
  - 改之后：Config struct 新增 Timeout time.Duration，YAML 新增 timeout: 10s
**验证标准**: go build ./... 通过 + 单元测试验证默认值和解析
**阻塞关系**: 无前置，blocks [Task N+1]
**预估复杂度**: 简单(<=10行) / 中等(10-50行) / 复杂(>50行)
**关联指标**: 无（或列出需要新增的 Prometheus metric）
```

## 阻塞顺序

强制顺序，不可跳过：

```
功能实现 Task → 单元测试 Task → 代码审查
```

## 输出格式

计划写入 `docs/plans/YYYY-MM-DD-<简述>.md`：

```markdown
# 实施计划: <需求标题>

## 方案概要
<1-3 句话>

## Task 汇总

| Task | 文件 | 复杂度 | 阻塞关系 | 状态 |
|------|------|--------|----------|------|
| 1. xxx | file.go | 简单 | blocks [2] | pending |
| 2. xxx | file_test.go | 中等 | blockedBy [1] | pending |

## 详细 Task
### Task 1: ...
<7 要素>
```

## io-wy 审核通过后才执行

- 确认 → `execute-with-review`
- 需修改 → 调整后重新审核
- 需重来 → 回到 brainstorming Phase 3

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 模糊 Task（「优化性能」） | 执行时不知道具体做什么 | 每条 7 要素齐全 |
| 遗漏测试 Task | 功能无验证 | 阻塞顺序强制包含测试 |
| io-wy 未审核就执行 | 方向错误导致返工 | 必须等确认 |

## 验证清单

- [ ] 每个 Task 都有 7 要素
- [ ] 阻塞关系正确（功能→测试→审查）
- [ ] 文件路径精确到具体文件
- [ ] 验证标准可执行（不是「确认没问题」）
- [ ] io-wy 已审核通过
