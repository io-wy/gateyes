# docs/docs-tmp/ — 临时缓存(不进 git)

> Skill 自动写入的临时文档缓存。整目录默认 .gitignore,只保留本 README 和 .gitignore。

## 用途

Skill 在执行过程中产出的临时文档落地点。包括:
- 三方协议/API 文档 fetch 缓存
- 调研报告(竞品分析、技术选型对比)
- 代码审查/分析报告
- 临时计划、思考笔记

## 子目录约定(Skill 必须遵守)

| 子目录 | 内容 | 命名 |
|--------|------|------|
| `protocols/` | 协议/三方 API 文档 fetch | `<api-name>.md`,如 `openai-responses.md` |
| `research/` | 调研报告 | `<topic>-<YYYY-MM-DD>.md` |
| `analysis/` | 代码审查/影响分析 | `<task-id>-<YYYY-MM-DD>.md` |
| `plans/` | 临时计划/思考 | `<task>.md` |

完整路径表见 `/CLAUDE.md` → 「目录与输出路径约定」。

## 清理策略

- 不进 git,不会被 push
- Skill 完成任务后**不必**主动清理(io-wy 可保留对照)
- io-wy 周期性清理:`rm -rf docs/docs-tmp/*` 或保留近 30 天

## 不该放这里的内容

- 项目自己产出的正式文档 → 放 `docs/`
- starter 带出去的通用参考 → 放 `docs/docs-ref/`
- Skill 内部维护的演进记录 → 放 `.claude/skills/<skill>/references/`
