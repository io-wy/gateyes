# Skills 索引

> 17 个 skill（8 core + 9 plugin）自动加载；7 个已隐藏（`user-invocable: false`）；2 个轻量 slash-command；6 个全局 skill 建议装到 `~/.claude/skills/`。

## Slash-Command 速查

打 `/` 就能看到所有可用命令。**你只需要记这些：**

| 命令 | 做什么 |
|------|--------|
| `/brainstorming` | 新功能/修bug/重构 前先探代码出方案 |
| `/review` 或 `/adversarial-review` | 代码审查（双模型交叉验证） |
| `/test` 或 `/test-strategy` | 写测试 / 测试策略 |
| `/spec` `<name>` | 创建规格工件（L1/L2/L3） |
| `/spec-do` `<slug>` | 实现规格中的任务 |
| `/spec-check` `<slug>` | 检查规格质量 |
| `/protocol` | 协议驱动开发（API/数据格式） |
| `/delivery` | 部署/CI/Docker/监控 |
| `/harness` | 一键架构检查 + 健康度评分 |
| `/init` `[all\|docker\|cicd\|docs\|internal]` | 展开骨架模板 |

**隐藏命令**（自动触发，不在菜单出现）：`plan-to-tasks` `execute-with-review` `spec-checkpoint` `pre-verify` `e2e-verify` `false-positive-tracking` `knowledge-loop`

---

## Core（始终加载·8 个）

日常开发工作流，项目级强制。

| Skill | 触发 | 一句话 |
|-------|------|--------|
| `brainstorming-with-context` | 任何编码任务前（Enforcement） | 先探代码→澄清需求→出方案 |
| `plan-to-tasks` | brainstorming 后自动 | 拆解为 7 要素 Task |
| `execute-with-review` | plan 确认后自动 | 9 步流程：读→实现→审查→构建→测试→提交 |
| `adversarial-review` | 核心变更/≥5 文件/≥200 行 | 双模型交叉审查 |
| `change-impact-scan` | 改接口/模型/配置后 | Grep 全部调用点 |
| `pitfall-journal` | AI 犯错被纠正时 | 记录 PIT→提炼规则 |
| `knowledge-snapshot` | 不确定 API 时 | 走查证链，禁止编造 |
| `test-strategy` | 写测试/覆盖率低时 | 单元 70/集成 20/E2E 10 |

---

## Plugins（按需加载·11 个）

`.claude/skills/plugins/` 下，在任务匹配时自动触发。

| Skill | 触发 | 一句话 |
|-------|------|--------|
| `spec` | 写规格 / new spec | L1/L2/L3 复杂度评估 + 工件生成 |
| `spec-check` | check spec | 结构 lint + 质量 review |
| `spec-do` | spec-do/实现规格 | 读取规格→实现→写回 Evidence |
| `spec-checkpoint` | v1 工作流阶段切换时 | 7 个门禁等放行 |
| `pre-verify` | 结构性操作前 | 预验证层级合法性 |
| `e2e-verify` | execute 测试通过后 | build→lint→test→verify |
| `protocol-driven-development` | 写协议/API 代码时 | 参考文档→逐字段→交叉验证→6 类测试 |
| `project-delivery` | 部署/CI/Docker | 生产交付全流程基线 |
| `false-positive-tracking` | adversarial-review 后 | 误报追踪，调优审查规则 |
| `knowledge-loop` | PIT 审查/skill 进化时 | PIT→规则→Skill→自动化 五级进化 |
| `harness-go` | 层级检查/架构检查 | lint-deps + lint-quality + verify-action + audit |

---

## Global Skills（全局安装·6 个）

跨项目通用能力，建议安装到 `~/.claude/skills/`。见 `docs/docs-ref/docs/docs-ref/templates/global-skills/`。

```bash
cp docs/docs-ref/docs/docs-ref/templates/global-skills/deep-thinking.md ~/.claude/skills/
cp docs/docs-ref/docs/docs-ref/templates/global-skills/prompt-engineering.md ~/.claude/skills/
cp docs/docs-ref/docs/docs-ref/templates/global-skills/technical_writing.md ~/.claude/skills/
cp docs/docs-ref/docs/docs-ref/templates/global-skills/devex-tooling.md ~/.claude/skills/
cp docs/docs-ref/docs/docs-ref/templates/global-skills/coordinator-delegation.md ~/.claude/skills/
cp docs/docs-ref/docs/docs-ref/templates/global-skills/competitive-analysis.md ~/.claude/skills/
```

| Skill | 触发 | 一句话 |
|-------|------|--------|
| `deep-thinking` | 复杂调试/架构决策 | 5 步结构化推理 |
| `prompt-engineering` | prompt 设计/优化 | Prompt 模板 + Few-shot + 防御注入 |
| `technical_writing` | 写文档/API docs | Diátaxis 框架文档体系 |
| `devex-tooling` | 工具链/lint/monorepo | 评估搭建开发工具链 |
| `coordinator-delegation` | 委派/子代理 | 协调者不写代码，委派子代理 |
| `competitive-analysis` | 竞品分析/调研 | 多子代理调研业界实现 |

---

## io-wy 实际要记的

打 `/` 然后选就行。实在要记，就 3 个：

```
/brainstorming → 新功能/修bug
/review        → 代码审查
/harness       → 架构检查
```

---

## 维护规则

- 新增 Skill → 先判断属于 Core/Plugins/Global，写入本表
- Core: 日常开发必须，<10 个
- Plugins: 按需加载，放在 `plugins/` 子目录
- Global: 跨项目通用，放在 `docs/docs-ref/docs/docs-ref/templates/global-skills/`，建议用户全局安装
