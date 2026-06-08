---
name: harness-go
version: 1.0.0
description: Harness Engineering for Go projects. Provides layer dependency checking, code quality linting, pre-verify action validation, and project audit scoring. Use when setting up harness infrastructure, checking layer violations, running quality lint, pre-verifying structural changes, or auditing project harness coverage. Triggered by "harness", "lint-deps", "lint-quality", "verify-action", "harness-audit", "layer check", "架构检查", "层级检查".
---

# Harness Go

> 核心：把 Go 项目的架构约束编码为可执行脚本，让 Agent 在写代码前/后都能自动验证，而不是靠 prompt 记忆。

## 触发条件

- 搭建新项目 Harness 基础设施
- 检查层级依赖方向是否合法（lint-deps）
- 检查代码质量（lint-quality）
- 结构性操作前预验证（verify-action）
- 项目 Harness 健康度审计（harness-audit）
- io-wy 说「harness」「lint-deps」「层级检查」「架构检查」「verify」「审计」

## 与现有体系的关系

```
CLAUDE.md    → L1 项目宪法（已有，不替代）
harness-go   → L4 Skill，提供机械执法工具
README.md    → 领域约束文档（在项目文档中说明 Harness 用法）
scripts/     → 可执行脚本层
```

Harness 不替代 CLAUDE.md，而是给它配一套自动验证工具。

## 脚本清单

| 脚本 | 用途 | 命令 |
|------|------|------|
| `.claude/skills/harness-go/scripts/lint-deps.go` | 层级依赖方向检查 | `go run .claude/skills/harness-go/scripts/lint-deps.go <module> [harness.json]` |
| `.claude/skills/harness-go/scripts/lint-quality.go` | 代码质量检查 | `go run .claude/skills/harness-go/scripts/lint-quality.go` |
| `.claude/skills/harness-go/scripts/verify-action.go` | 结构性操作预验证 | `go run .claude/skills/harness-go/scripts/verify-action.go --action "..." <module>` |
| `.claude/skills/harness-go/scripts/harness-audit.go` | 项目 Harness 健康度审计 | `go run .claude/skills/harness-go/scripts/harness-audit.go [module]` |

## Layer 层级规则（默认）

```
Layer 0: types/ domain/          → 纯类型定义，无内部依赖
Layer 1: utils/                  → 工具函数，仅依赖 Layer 0
Layer 2: config/                 → 配置，依赖 Layer 0-1
Layer 3: core/ service/ usecase/ → 业务逻辑，依赖 Layer 0-2
Layer 4: handler/ api/ cmd/ cli/ → 接口层，依赖 Layer 0-3
```

规则：高层可 import 低层，低层 **绝对不能** import 高层。Layer 0 不能有内部依赖。

自定义层级：运行 `go run .claude/skills/harness-go/scripts/lint-deps.go --init` 生成 `harness.json` 模板，编辑后所有脚本自动读取。

## 验证管道

```
build → lint-arch → test → verify
  │        │         │       │
  │        │         │       └─ scripts/verify/ 端到端功能验证
  │        │         └─ go test ./...
  │        └─ lint-deps + lint-quality
  └─ go build ./...
```

## 使用场景

### 场景 1：新项目初始化 Harness

```bash
# 1. 生成 harness.json
go run .claude/skills/harness-go/scripts/lint-deps.go --init

# 2. 编辑 harness.json 匹配项目结构

# 3. 审计当前状态
go run .claude/skills/harness-go/scripts/harness-audit.go

# 4. 跑 lint 检查
go run .claude/skills/harness-go/scripts/lint-deps.go github.com/your-org/your-project
go run .claude/skills/harness-go/scripts/lint-quality.go
```

### 场景 2：Agent 写代码前预验证

涉及「在新位置创建文件」或「添加跨包 import」时，先验证：

```bash
# 验证创建文件
go run .claude/skills/harness-go/scripts/verify-action.go --action "create file internal/types/user.go" github.com/example/proj

# 验证 import 方向
go run .claude/skills/harness-go/scripts/verify-action.go --action "import internal/app/config from internal/types" github.com/example/proj
```

### 场景 3：CI 管道集成

```bash
# Makefile 中添加
lint-arch:
	go run .claude/skills/harness-go/scripts/lint-deps.go $(MODULE)
	go run .claude/skills/harness-go/scripts/lint-quality.go

audit:
	go run .claude/skills/harness-go/scripts/harness-audit.go $(MODULE)
```

## 常驻监控建议

Agent 在以下时机应主动跑 Harness 检查：

1. **新会话启动时**：跑 `harness-audit` 了解项目 Harness 成熟度
2. **创建新文件/目录时**：跑 `verify-action` 预验证
3. **添加跨包 import 时**：跑 `verify-action` 验证方向
4. **Task 完成后**：跑 `lint-deps + lint-quality` 确认无违规
5. **定期（每 5-10 个 Task）**：跑 `harness-audit` 跟踪改进

不是每次修改都要跑全量——改函数体不需要，结构性操作才需要。

## lint-deps 错误信息规范

错误信息必须包含三要素：

```
LAYER VIOLATION: internal/types/user.go
  internal/types (types, Layer 0) imports internal/app/config (config, Layer 2)
  Rule: Layer 0 packages must have NO internal dependencies.
  Fix: Move config-dependent logic to a higher layer, or pass the value as parameter.
```

1. **什么规则违反了** — 具体说明层级关系和冲突
2. **为什么是问题** — 解释架构约束的意图
3. **怎么修** — 给出明确的修复方向

## lint-quality 检查项

| 检查项 | 说明 |
|--------|------|
| file-too-large | 单文件超过 500 行 |
| logging | 使用 fmt.Println/log.Println 代替结构化日志 |
| hardcoded-timeout | 硬编码 time.Second/time.Minute |
| hardcoded-url | 硬编码 localhost URL |
| error-swallowing | `_ =` 吞掉错误 |

## harness-audit 评分维度

| 维度 | 分值 | 检查项 |
|------|------|--------|
| Documentation | 20 | CLAUDE.md/AGENTS.md, docs/, README.md |
| Layer Config | 20 | harness.json 存在且有效 |
| Lint Scripts | 20 | lint-deps.go, lint-quality.go |
| Verify Infra | 20 | scripts/verify/ 目录及脚本数量 |
| Test Coverage | 20 | _test.go 存在, Makefile 含 test/lint |

评分等级：
- 80-100：Healthy
- 50-79：Needs Improvement
- 20-49：Basic
- 0-19：Barely Started

## 与 pre-verify / e2e-verify Skill 的关系

```
harness-go      → 提供 Go 专用的机械执法脚本（lint-deps, lint-quality）
pre-verify      → AI 行为约束：结构性操作前必须验证
change-impact-scan → AI 行为约束：修改后扫描影响范围
e2e-verify      → AI 行为约束：验证管道定义（build→lint→test→verify）
```

harness-go 是「工具层」，pre-verify/e2e-verify 是「行为层」。行为层说"你应该做"，工具层提供"怎么做"。

## 验证清单

- [ ] harness.json 已创建且匹配项目结构
- [ ] lint-deps 能正确检测跨层 import 违规
- [ ] lint-quality 能检测质量违规
- [ ] verify-action 能验证创建文件和 import 操作
- [ ] harness-audit 能输出 0-100 分
- [ ] CI/Makefile 已集成 lint-arch 目标
