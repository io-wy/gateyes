---
name: coordinator-delegation
version: 1.0.0
description: 协调者-执行者分离。中等复杂度以上任务，协调者（Coordinator）只负责规划、委派、汇总，一行代码都不写；执行者（子代理）每次从干净上下文开始，拿到精确 prompt，干完释放。Triggered when task involves multiple files, structural changes, refactoring, new modules, or when user says "委派/delegate/子代理/sub-agent/协调者/coordinator/拆任务".
---

# Coordinator Delegation

> 核心：如果这篇文章你只记住一件事，记这个——中等复杂度以上的任务，协调者绝不写代码。

## 触发条件

- 涉及 3+ 个文件的修改
- 结构性变更（重构、新模块、改架构）
- 需要做设计决策和权衡
- 需要用清单跟踪改了哪些地方
- io-wy 说「委派」「子代理」「拆任务」「coordinator」

## 复杂度判断

| 复杂度 | 判断标准 | 执行方式 |
|--------|----------|----------|
| 简单 | 一句话描述，不含"和"字 | 主 Agent 直接做 |
| 中等 | 多文件一致性修改 | 委派子代理 |
| 复杂 | 结构性变更，需设计决策 | 子代理 + Git Worktree 隔离 |

```
能用一句话描述且不包含"和"字的 → 直接做
需要清单来跟踪改了哪些地方的 → 委派
需要做设计决策和权衡的 → 委派 + 隔离
```

## 两层分离

### Coordinator（协调者）

**做什么**：
- 理解需求，分解任务
- 制定计划，确定依赖关系
- 委派子代理执行
- 接收结果，汇总报告
- 处理异常和阻塞

**不做什么**：
- ❌ 不写代码（Edit/Write 工具只用于修改计划/报告，不修改源码）
- ❌ 不做具体实现
- ❌ 不跑编译/测试（除非验证子代理结果）

### Executor（执行者/子代理）

**做什么**：
- 从干净的上下文开始
- 拿到精确的 prompt（含需要知道的一切）
- 执行具体编码任务
- 跑验证（build/test/lint）
- 返回结果和摘要

**不做什么**：
- ❌ 不知道之前发生了什么（上下文被隔离）
- ❌ 不做超出 prompt 范围的决策
- ❌ 不与其他子代理直接通信

## 子代理启动规范

### 上下文设计

子代理的 prompt 必须包含：
1. **任务目标** — 精确描述要做什么
2. **架构上下文** — 相关 AGENTS.md / ARCHITECTURE.md 的摘要
3. **约束规则** — 必须遵守的编码约束（C-01 到 C-10）
4. **输入数据** — 需要修改的文件路径、现有代码
5. **验证标准** — 完成后必须通过的验证
6. **输出格式** — 返回什么格式的结果

```
Agent(
  description="Implement: user authentication middleware",
  prompt="""
  ## Task
  在 internal/transport/http/middleware/auth.go 实现 JWT 认证中间件。

  ## Architecture
  - middleware 属于 Layer 4（接口层）
  - 依赖 internal/core/auth 服务（Layer 3）
  - 不能直接在 middleware 里操作数据库

  ## Constraints
  - C-02: context 必须传递上游 ctx
  - C-04: 资源必须配对（defer Close）
  - C-05: 所有入参必须校验

  ## Input
  - 现有文件: internal/transport/http/middleware/auth.go（当前为空）
  - 依赖服务: internal/core/auth/service.go（已有 ValidateToken 方法）

  ## Validation
  - go build ./...
  - go test ./internal/transport/http/middleware/...
  - 集成测试: 带有效/无效 token 的请求

  ## Output
  - 修改后的文件内容
  - 新增测试文件
  - 验证结果摘要
  """
)
```

### 模型选择策略

不是所有任务都需要最强模型：

| 任务类型 | 推荐模型 | 原因 |
|----------|----------|------|
| 代码检索/定位 | Gemini Flash | 速度快，成本低 |
| 简单修改（改 typo、重命名） | Claude Haiku | 响应快，成本低 |
| 常规开发 | Claude Sonnet | 平衡速度和质量 |
| 复杂重构/架构变更 | Claude Opus / GPT Codex | 深度推理 |
| 交叉 Review | 与编码不同的模型 | 减少思维盲区重叠 |

```
Agent(description="Rename field", model="haiku", prompt="...")
Agent(description="Refactor auth module", model="opus", isolation="worktree", prompt="...")
```

### 执行隔离

| 场景 | 隔离级别 |
|------|----------|
| 简单修改 | 无隔离，直接修改 |
| 中等修改 | 子代理独立上下文 |
| 结构性变更 | Git Worktree 隔离 |

**Git Worktree**：
- 创建临时分支副本
- 子代理在副本上工作
- 成功 → 合并回主分支
- 失败 → 丢掉副本，不污染主分支

## 检查点（Checkpoint）

每完成一个阶段、跑过验证就存档：

```
Phase 1: 类型定义完成
- 文件: internal/types/user.go
- 验证: go build ./internal/types/... ✓
- 架构决策: User 结构体包含 ID, Email, Name 字段
- 检查点: CP-001

Phase 2: 服务层完成
- 文件: internal/core/user/service.go
- 验证: go test ./internal/core/user/... ✓
- 架构决策: 通过接口依赖，不直接操作 DB
- 检查点: CP-002
```

检查点必须携带架构决策：
- 没有架构决策的检查点 → 新 Agent 可能走完全不同的路
- 有架构决策的检查点 → 新 Agent 延续一致的方向

## 信息压缩

子代理完成后，协调者只保留摘要，丢掉详细上下文：

```
子代理详细输出（5000 tokens）
    ↓ 协调者压缩
摘要（200 tokens）:
"完成了 user service 的 CRUD 实现，新增 3 个测试，
build 通过，test 通过。关键决策：使用接口隔离 DB 依赖。"
```

## 常见陷阱

| 陷阱 | 表现 | 预防 |
|------|------|------|
| "只是快速改一下" | 协调者直接 Edit 源码，5次编辑变20次，上下文耗尽 | **绝不例外**，任何源码修改都走子代理 |
| 所有子代理用同一模型 | 浪费钱和时间，简单任务不需要 Opus | 按任务性质选模型 |
| 子代理 prompt 太粗 | 子代理做出与架构矛盾的决策 | prompt 必须含架构约束和验证标准 |
| 不存检查点 | 任务中断后新 Agent 走不同方向 | 每阶段通过后强制存档 |
| 子代理结果不验证 | 子代理说"完成"但实际有 bug | 协调者必须跑验证确认 |

## 与 execute-with-review 的关系

```
coordinator-delegation: 谁来写代码（协调者 vs 子代理）
execute-with-review: 代码怎么写（7步流程 + review）
```

两者正交：协调者委派子代理后，子代理执行时仍然要走 execute-with-review 的 7 步流程。

## 验证清单

- [ ] 复杂度已判断（简单/中等/复杂）
- [ ] 协调者不修改源码（只修改计划/报告）
- [ ] 子代理 prompt 含：目标+架构+约束+输入+验证标准
- [ ] 子代理模型选择匹配任务复杂度
- [ ] 结构性变更使用 Git Worktree 隔离
- [ ] 每阶段完成后存检查点（含架构决策）
- [ ] 子代理结果经过验证（build/test）
- [ ] 协调者只保留摘要，丢掉详细上下文
