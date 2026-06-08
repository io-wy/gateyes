# Workflow Bridge: v1 vs v2

> 本文档是两套工作流体系的选择指南。它们不是竞争对手——服务不同复杂度场景。

## 两套体系一览

### v1: Brainstorming → Plan → Execute

**最佳适用:** 小到中型功能、Bug 修复、快速迭代、日常开发
**心智模型:** 线性流水线，带检查点

```
brainstorming-with-context
        ↓
    plan-to-tasks
        ↓
  execute-with-review
        ↓
 change-impact-scan
```

**触发方式:** 任何编码任务（"修 bug"、"加功能"、"重构"）。

### v2: Spec → Spec-Check → Spec-Do

**最佳适用:** 大型功能、Epic、跨模块变更、架构工作、需要正式文档的场景
**心智模型:** 结构化规格工件，按复杂度分级（L1 Patch / L2 Feature / L3 Epic）

```
/spec <feature>
      ↓
/spec-check <slug>
      ↓
/spec-do <slug>
```

**触发方式:** "写规格"、"new spec"、"spec-do"。

---

## 选择对照表

| 场景 | 推荐体系 | 理由 |
|------|---------|------|
| 快速修 bug（≤2 文件） | v1 | 开箱即用，无 ceremony |
| 单模块新功能（3-8 文件） | v1 完整流水线 | 轻量但有结构 |
| 跨模块 / 破坏变更 | v2 (L3 Epic) | 需要 requirements + design + tasks 三文件 |
| 需要正式需求文档 | v2 | 产出可交付的规格工件 |
| 团队交接 / 多人协作 | v2 | spec 工件是双向契约，可交接 |
| 从零设计新模块 | v2 | 需要 Options Considered + Affected Modules |
| 日常维护、小调整 | v1 | 不增加文档负担 |

## 复杂度判断速查

```
变更文件数
  ≤2  → v1 (或 v2 L1 Patch)
  3-8 → v1 完整流水线 或 v2 L2 Feature
  >8  → v2 L3 Epic

是否需要以下之一？
  ✓ 正式需求文档
  ✓ 设计决策记录（DD-*）
  ✓ 团队交接
  ✓ 可追溯性（FR→AC→DD→T）
  → 用 v2

是否只是快速迭代？
  ✓ 修 bug
  ✓ 加字段
  ✓ 改配置
  → 用 v1
```

## 互操作性

- **v2 的 spec 可以引用 v1 的输出**
  - 例如：spec 的 Context 部分可以链接到 brainstorming 产出的方案文档
- **v1 的 change-impact-scan 应在 v2 的 spec-do 之后运行**
  - spec-do 完成代码实现后，同样需要扫描影响范围
- **v1 的 adversarial-review 适用于任何体系的代码产出**
  - 代码审查与使用哪个工作流无关
- **两套体系共享同一套约束**
  - 都遵守 CLAUDE.md C-01~C-10 编码约束
  - 都使用 harness-go 的 lint 脚本
  - 都遵循 pitfall-journal 的踩坑进化闭环

## 渐进式迁移路径

已经在用 v1 的项目，可以逐步引入 v2：

1. **第 1 阶段**: 大型功能开始用 `/spec`（其余保持 v1）
2. **第 2 阶段**: 团队习惯后，中型功能也开始用 v2 L2
3. **第 3 阶段**: v2 成为新功能默认，v1 保留给快速修复

不需要一次性切换——两套体系可以长期共存。

## Skill 索引对照

| 能力 | v1 Skill | v2 Skill |
|------|----------|----------|
| 需求澄清+方案设计 | `brainstorming-with-context` | `spec` (含 Context + Goals) |
| 拆任务 | `plan-to-tasks` | `spec` (L2/L3 Tasks section) |
| 执行代码 | `execute-with-review` | `spec-do` |
| 质量检查 | `spec-checkpoint` (CP 门禁) | `spec-check` (lint + review) |
| 影响扫描 | `change-impact-scan` | `change-impact-scan` (共用) |
| 代码审查 | `adversarial-review` | `adversarial-review` (共用) |
| 测试策略 | `test-strategy` | `test-strategy` (共用) |
