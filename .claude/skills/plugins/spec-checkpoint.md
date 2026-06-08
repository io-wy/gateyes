---
name: spec-checkpoint
version: 1.1.0
user-invocable: false
description: 阶段性质量门禁。在 brainstorming→plan→execute 全流程的 7 个关键节点，强制等待 io-wy 确认后才进入下一阶段。Auto-triggered at each phase transition within the v1 workflow.
---

# Spec Checkpoint

> 核心：每个阶段都有 checkpoint，防止跳步。io-wy 是唯一的放行者。

## 触发条件

每个主要阶段完成时自动触发。

## 7 个 Checkpoint

| CP | 阶段 | 产出 | 通过条件 |
|----|------|------|----------|
| 1 | 需求理解 | 理解摘要 + 假设列表 | io-wy 确认无误 |
| 2 | 代码探索 | 模块清单 + 数据流 + 现有行为 | 涉及模块全覆盖 |
| 3 | 方案设计 | >=2 方案 + 推荐 + 变更清单 | 方案被选定 |
| 4 | Task 计划 | Task 列表（7 要素） | 计划被确认 |
| 5 | 代码实现 | spec review + quality review + build 通过 | Critical/Major 清零 |
| 6 | 测试通过 | go test ./... 通过 | 测试全绿 |
| 7 | 提交完成 | commit message + MR 描述 | io-wy 确认 |

## 执行方式

每个 Checkpoint：
1. 展示产出
2. 等待 io-wy：
   - 「通过/ok/确认」 → 下一阶段
   - 「修改 xxx」 → 调整后重新提交
   - 「重来」 → 回上一个 CP
   - 「跳过」 → 记录原因后继续（io-wy 主动说才可）
3. 通过后才进入下一阶段

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 自主跳过 CP | 跳步导致返工 | CP 不可自主跳过 |
| 合并 CP | 问题在后面的阶段才暴露 | 每个 CP 独立过 |
| io-wy 未确认就继续 | 方向错误 | 必须等确认 |

## 验证清单

- [ ] 每个 CP 都独立经过 io-wy 确认
- [ ] 无跳过（除非 io-wy 主动要求）
- [ ] 每阶段的产出完整展示给 io-wy
