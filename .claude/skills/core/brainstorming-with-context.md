---
name: brainstorming-with-context
version: 1.1.0
description: 结构化需求澄清与方案设计。You MUST use this before any new feature, bug fix, refactoring, or interface change — explores codebase first, clarifies requirements, then produces design options with trade-offs before implementation. Triggered when user says "帮我做/实现/修/加功能/new feature/bug fix/refactor".
---

# Brainstorming with Context

> 核心：AI 不闭门造车。先理解代码现状，再理解需求边界，最后才出方案。

## 触发条件

- 新功能开发、bug 修复、重构、接口变更
- io-wy 说「帮我做 xxx」「实现 xxx」「修 xxx」
- 收到任何需要写代码的任务时，在动手之前触发

## 执行流程

### Phase 1：代码探索（不可跳过）

根据任务关键词搜索并阅读相关代码。目标：理解现状，不是设计方案。

必须完成：

1. **定位入口**：handler → service → repository，沿调用链读文件
2. **读完整实现**：不是只看函数签名，而是读完整个函数体
   - 理解数据流：输入从哪来 → 中间经过什么处理 → 输出到哪去
   - 识别配置项、环境变量、数据库表
3. **查看测试**：读已有测试，理解当前行为的预期
4. **检查依赖**：该模块被谁调用、调用了谁

完成后向 io-wy 汇报理解结果（1-3 句话），确认理解正确。

### Phase 2：需求澄清（至少确认以下维度）

向 io-wy 提问，不假设答案：

1. **边界条件**：输入为空/极大/非法时怎么处理？
2. **兼容性**：需要向后兼容吗？有无调用方依赖当前行为？
3. **性能**：预期 QPS/数据量？延迟要求？
4. **错误处理**：失败时重试/降级/直接报错？
5. **可观测性**：需要加日志/指标/告警？
6. **配置**：需要新增配置项？默认值？

提问策略：二选一带推荐，单次最多 2 问。

### Phase 3：方案设计

1. **至少 2 种方案**，每种列优缺点
2. **推荐方案 + 理由**
3. **变更文件清单**（精确到文件路径）
4. **变更类型标注**：新增/修改/删除
5. **影响范围标注**：哪些 API/模块受影响

保存到 `docs/plans/`，告知 io-wy 路径。

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 未读代码就出方案 | 方案脱离实际，遗漏已有逻辑 | Phase 1 不可跳过 |
| 不提问直接做 | 理解偏差导致返工 | Phase 2 至少问一轮 |
| 只给一种方案 | 没有对比无法判断优劣 | Phase 3 至少 2 种 |
| 方案不列文件清单 | 执行时发现遗漏依赖 | Phase 3 必须列清单 |

## 验证清单

- [ ] 已读取所有涉及文件的完整内容
- [ ] 已向 io-wy 确认理解无误
- [ ] 已提出至少 1 个澄清问题
- [ ] 已给出至少 2 种方案
- [ ] 方案包含精确的文件变更清单

## 链式触发

方案被 io-wy 确认后，自动链式触发 `plan-to-tasks`。
