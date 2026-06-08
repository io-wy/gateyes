---
name: false-positive-tracking
version: 1.0.0
user-invocable: false
description: 误报追踪与 Review 质量度量。记录 Code Review 中的误报，计算真阳性/假阳性比率，定期调优审查规则。Auto-triggered after adversarial-review completes.
---

# False Positive Tracking

> 核心：Review 发现的问题不都是真问题。追踪误报率才能持续提升 Review 质量，避免噪音淹没信号。

## 触发条件

- `adversarial-review` 完成后自动触发（记录审查结果）
- io-wy 说「误报」「false positive」「review quality」「审查质量」「调优审查规则」
- 定期触发（建议每月一次审查误报率）

## 核心概念

| 术语 | 定义 |
|------|------|
| **True Positive (TP)** | Review 发现的问题确实是问题，需要修复 |
| **False Positive (FP)** | Review 发现的问题实际不是问题，误判 |
| **True Negative (TN)** | Review 没有标记，确实没问题（无法直接观测） |
| **False Negative (FN)** | Review 没有发现，但实际存在的问题（上线后暴露） |
| **精确率 Precision** | TP / (TP + FP)，标记为问题的有多少是真问题 |
| **召回率 Recall** | TP / (TP + FN)，真正的 bug 有多少被发现了 |

## 执行流程

### Step 1：审查结果记录

每次 `adversarial-review` 完成后，在审查报告末尾追加判定结果：

```markdown
## 误报判定

| 问题 ID | 级别 | 分类 | 判定 | 判定依据 |
|---------|------|------|------|----------|
| C1 | Critical | 并发 | TP | 确实存在 data race |
| M3 | Major | 性能 | FP | 该路径不在热路径上 |
| S1 | Suggestion | 风格 | FP | 个人偏好，非实际缺陷 |

### 统计
- TP: 2 / FP: 2 / 精确率: 50%
```

**判定者**：io-wy。AI 标记问题，io-wy 判定是否为误报。

### Step 2：PIT 记录

当误报率 > 40% 时，在 `pitfall-journal.md` 中记录：

```markdown
### PIT-XXX: 审查误报——[具体分类]

- **日期**: YYYY-MM-DD
- **类型**: review-false-positive
- **严重度**: Medium
- **场景**: 对 [模块] 进行对抗式审查时
- **现象**: 发现 N 个 Major+ 问题，其中 M 个为误报
- **根因**: 审查规则过于宽泛 / 对项目上下文理解不足 / 分类边界模糊
- **修复**: 调整审查规则 [具体调整]
- **规则提炼**: 在 CLAUDE.md 或 adversarial-review Skill 中明确 [具体规则]
- **状态**: 新增
```

### Step 3：规则调优

当同一类误报出现 >= 2 次时，执行规则调优：

| 误报类型 | 调优动作 |
|----------|----------|
| 热路径误判（非热路径标记为性能问题） | 在 CLAUDE.md 或项目 docs/ 中明确热路径范围 |
| 风格偏好误标为缺陷 | 在 CLAUDE.md 中明确风格规则 vs 缺陷规则 |
| 历史遗留误归为本次引入 | 强化 adversarial-review 的 diff 边界判定 |
| 项目特定设计被误判为错误 | 在 docs/ 中补充设计决策（ADR） |
| 过于严格的并发审查 | 明确哪些路径是单线程安全的 |

调优后的规则写入对应位置：
- 编码约束 → `CLAUDE.md`
- 审查规则 → `adversarial-review.md`
- 设计决策 → `docs/adr/`

### Step 4：定期 Review

建议节奏：每 5 次对抗式审查后做一次误报率分析。

```markdown
# 误报率分析报告

## 统计周期
- 起止日期：
- 审查次数：
- 总问题数：
- TP / FP：

## 误报分布
| 分类 | TP | FP | 精确率 |
|------|----|----|--------|
| 并发 | 3 | 1 | 75% |
| 性能 | 1 | 4 | 20% |
| 安全 | 2 | 0 | 100% |
| 错误处理 | 4 | 1 | 80% |

## 趋势
- 精确率变化：[上次]% → [本次]%
- 改善措施生效情况：...

## 下一步
- 性能类误报率最低 → 需要明确热路径定义
- ...
```

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 不记录误报 | 误报率持续高，Review 噪音大 | adversarial-review 后强制记录 |
| 追求 100% 精确率 | 召回率下降，漏报增加 | 接受 70-80% 精确率为健康水平 |
| 只看精确率不看召回率 | 漏报被忽视 | 上线后 bug 也需要回溯 FN |
| 一次性调太多规则 | 不知道哪条调优有效 | 每次只调 1-2 条规则 |

## 验证清单

- [ ] 审查报告包含误报判定表
- [ ] 误报率已计算
- [ ] 误报率 > 40% 时已记录 PIT
- [ ] 同类误报 >= 2 次时已执行规则调优
- [ ] 调优后的规则已写入对应位置
