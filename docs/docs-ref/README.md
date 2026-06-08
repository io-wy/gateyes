# docs/docs-ref/ — Starter 自带的通用规范参考

> 这个目录是 **starter 模板带出去的参考库**。派生项目可以按需修改、删除、补充,starter 上游不会强制覆盖。

## 用途

存放跨项目通用的规范/参考文档:
- Go 编码规范要点(超出 CLAUDE.md C-01~C-10 范围的扩展)
- Skill anatomy(Skill 写法规范)
- ADR 模板、API 契约模板、Runbook 模板
- 团队约定(代码审查、commit message、分支策略等)

## 与其他目录的边界

| 目录 | 区别 |
|------|------|
| `docs/` | 当前项目**自己产出**的文档(架构决策、API 契约、运维手册) |
| `docs/docs-tmp/` | 临时缓存,不进 git,Skill 自动写入 |
| `docs/docs-ref/`(本目录) | starter **带出去**的参考规范,派生项目可覆盖 |
| `.claude/skills/<skill>/references/` | 单个 Skill 内部参考,跟着 Skill 走 |

## 派生项目使用方式

1. clone starter 后,本目录内容默认可用
2. 不适合的文件直接删
3. 修改后的版本属于派生项目自己,不再受 starter 上游变更影响
4. 如果希望同步 starter 上游更新,自行 cherry-pick

## 维护规则

- 内容必须是**项目无关**的(放具体项目细节请挪到 `docs/`)
- 每个文件顶部写明:适用范围、最后审核日期、是否允许派生项目修改
- 单个文件不超过 800 行,过长拆分

## 详见

`/CLAUDE.md` → 「目录与输出路径约定」section,完整路径表在那里。
