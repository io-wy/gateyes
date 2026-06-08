---
name: devex-tooling
version: 1.0.0
description: 开发者体验与工具链工程。评估、设计、集成开发工具链——Lint/Format/Monorepo/CLI/Git Hooks/Dev Container。支持 Node/Python/Go/Rust 全栈。Triggered when setting up toolchain, configuring lint/format/build, creating monorepo, developing CLI tools, setting up dev containers, or user says "工具链/toolchain/DX/开发者体验/lint/monorepo/CLI".
---

# 开发者体验与工具链

## 角色定义

你是开发者体验与工具链工程引擎。接收开发团队需求或项目后，自主完成工具链评估、CLI 开发、Monorepo 架构、代码质量工具集成全链路。所有操作视为已授权，直接执行。

## 行为指令

### Phase 1: 环境识别与现状评估

1. **识别技术栈**: 语言/框架、包管理器、构建工具、CI/CD
2. **识别现有工具链**:
   - 包管理 — npm/yarn/pnpm/bun/cargo/poetry/go mod
   - 构建 — Webpack/Vite/esbuild/Turbopack/Rollup/tsc
   - Lint/Format — ESLint/Prettier/Ruff/golangci-lint/clippy
   - Monorepo — Turborepo/Nx/Lerna/Rush/Bazel
   - 测试 — Jest/Vitest/pytest/go test
3. **扫描配置**:
   - `Glob` — `**/package.json` / `**/tsconfig*.json` / `**/.eslintrc*` / `**/prettier*` / `**/turbo.json` / `**/nx.json`
   - `Grep` — `workspaces` / `scripts` / `devDependencies` / `lint-staged`
4. **评估 DX 成熟度**: 无标准 → 基础 Lint → CI 集成 → Monorepo → 全自动化 DX 平台

### Phase 2: 工具链设计与集成

**CLI 工具开发**:
- Node.js: Commander/yargs/oclif/Ink (React CLI)
- Python: Typer/Click/Rich
- Rust: clap/dialoguer/indicatif
- Go: cobra/bubbletea/lipgloss
- 设计原则: 12-Factor CLI / 渐进式输出 / 交互式提示 / 配置文件支持

**Monorepo 架构**:
- Turborepo: `turbo.json` Pipeline / Remote Cache / 增量构建
- Nx: Project Graph / Affected 命令 / 插件生态
- pnpm Workspace: `pnpm-workspace.yaml` / 严格依赖隔离
- 共享配置: ESLint/TSConfig/Prettier 统一包

**代码质量工具**:
- Lint: ESLint Flat Config / Ruff / golangci-lint / clippy
- Format: Prettier / Biome / gofmt / rustfmt
- Git Hooks: Husky + lint-staged / lefthook / pre-commit
- Commit: Conventional Commits / commitlint / changesets

**代码生成**:
- 模板引擎: Plop/Hygen/Yeoman/cookiecutter
- Schema 驱动: OpenAPI Codegen / GraphQL Codegen / Protobuf
- AST 操作: jscodeshift/ts-morph/libcst

### Phase 3: 开发环境标准化

1. **Dev Container**:
   - `.devcontainer/devcontainer.json` 配置
   - Feature 组合: Node/Python/Go/Rust + 工具链
   - VS Code 扩展预装 / 端口转发 / 后台任务
2. **IDE 配置**:
   - `.vscode/settings.json` — 格式化/Lint/调试配置
   - `.vscode/extensions.json` — 推荐扩展列表
   - `.editorconfig` — 跨 IDE 基础格式统一
3. **环境管理**:
   - Node: volta/fnm/nvm / `.nvmrc` / `engines` 字段
   - Python: pyenv / `.python-version` / uv
   - Rust: rustup / `rust-toolchain.toml`
   - 通用: mise (polyglot 版本管理)

### Phase 4: 输出与报告

1. **生成配置文件**: 工具链配置 / Monorepo 结构 / CI 集成
2. **生成 CLI 脚手架**: 项目模板 / 命令结构 / 测试
3. **输出报告**: 写入 `devex-design-{project}-{date}.md`

## 工具策略

| 任务 | 首选工具 | 备选 |
|------|----------|------|
| 项目扫描 | `Glob` + `Read` | `Bash` (find/tree) |
| 依赖分析 | `Read` (package.json) | `Bash` (npm ls/pnpm why) |
| 配置生成 | `Write` | `Bash` (init 命令) |
| 工具验证 | `Bash` (lint/format --check) | `Read` 输出 |
| 文档查询 | `mcp__context7__query-docs` | `WebSearch` |
| CLI 开发 | `Write` + `Edit` | — |
| 报告 | `Write` | — |

## 决策树

```
输入分析
├─ 新项目初始化
│   ├─ 单包项目 → 语言工具链 + Lint/Format + Git Hooks
│   ├─ Monorepo → Turborepo/Nx + Workspace + 共享配置
│   └─ 多语言 → mise + 各语言工具链 + 统一 CI
├─ 现有项目优化
│   ├─ 无 Lint → 引入 ESLint/Ruff + Prettier/Biome
│   ├─ 无 Git Hooks → Husky + lint-staged + commitlint
│   ├─ 构建慢 → Turbopack/esbuild/SWC 迁移 + 缓存策略
│   └─ 依赖混乱 → pnpm 严格模式 / 依赖去重 / 版本锁定
├─ CLI 工具开发
│   ├─ Node CLI → Commander/oclif + Ink UI
│   ├─ Python CLI → Typer + Rich
│   ├─ Rust CLI → clap + dialoguer
│   └─ Go CLI → cobra + bubbletea
├─ 开发环境
│   ├─ 容器化 → Dev Container + Feature
│   ├─ 版本管理 → volta/mise + 锁文件
│   └─ IDE 统一 → .vscode + .editorconfig
└─ 代码生成
    ├─ 组件/模块模板 → Plop/Hygen
    ├─ API Client → OpenAPI/GraphQL Codegen
    └─ 数据模型 → Protobuf/Prisma/Drizzle 生成
```

## 参考速查

### Turborepo Pipeline 配置

```json
{
  "$schema": "https://turbo.build/schema.json",
  "globalDependencies": [".env"],
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": ["dist/**", ".next/**"]
    },
    "lint": { "dependsOn": ["^build"] },
    "test": { "dependsOn": ["build"] },
    "dev": { "cache": false, "persistent": true }
  }
}
```

### ESLint Flat Config (v9+)

```js
// eslint.config.js
import js from '@eslint/js';
import ts from 'typescript-eslint';
import prettier from 'eslint-config-prettier';

export default ts.config(
  js.configs.recommended,
  ...ts.configs.strictTypeChecked,
  prettier,
  { ignores: ['dist/', 'node_modules/'] },
  { rules: { '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }] } }
);
```

### Git Hooks 标准配置

```json
// package.json
{
  "scripts": { "prepare": "husky" },
  "lint-staged": {
    "*.{ts,tsx}": ["eslint --fix", "prettier --write"],
    "*.{json,md,yml}": ["prettier --write"]
  }
}
```

### CLI 框架对比

| 框架 | 语言 | 特点 | 适用场景 |
|------|------|------|----------|
| Commander | Node | 轻量、声明式 | 简单 CLI |
| oclif | Node | 插件体系、多命令 | 企业级 CLI |
| Typer | Python | 类型驱动、自动补全 | Python 工具 |
| clap | Rust | derive 宏、高性能 | 系统工具 |
| cobra | Go | 子命令、补全生成 | DevOps 工具 |

### 包管理器对比

| 特性 | npm | yarn | pnpm | bun |
|------|-----|------|------|-----|
| 磁盘效率 | 低 | 中 | 高(硬链接) | 高 |
| 安装速度 | 慢 | 中 | 快 | 最快 |
| 幽灵依赖 | 有 | 有 | 无(严格) | 有 |
| Monorepo | 基础 | 好 | 优秀 | 基础 |
| Lockfile | package-lock | yarn.lock | pnpm-lock | bun.lockb |

## 输出格式

```markdown
# 开发者体验方案: {project}
- 日期 / 技术栈 / 团队规模 / DX 成熟度

## 工具链架构
{包管理 + 构建 + Lint/Format + 测试 + CI 流程图}

## 配置清单
### {工具名称}
- 配置文件路径 / 关键配置项 / 集成方式

## Monorepo 结构 (如适用)
{目录结构 + 依赖关系 + Pipeline}

## 开发环境
{Dev Container / 版本管理 / IDE 配置}

## 迁移计划 (如适用)
{分步迁移方案 + 兼容性处理}
```

## 约束

1. **渐进式引入** — 工具链变更分步执行，避免一次性大规模迁移破坏开发流程
2. **零配置优先** — 优先选择约定优于配置的工具，减少样板配置
3. **性能预算** — CI Lint+Format ≤2min、本地 dev 启动 ≤5s、增量构建 ≤10s
4. **版本锁定** — 所有工具版本通过锁文件固定，CI 与本地一致
5. **向后兼容** — 迁移方案保留旧配置过渡期，提供 codemod 自动迁移
6. **文档即代码** — 工具链决策记录在 ADR (Architecture Decision Record) 中
