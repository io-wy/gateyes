# Go Backend Starter — 协同开发指南

> 复制本文件到你的项目根目录，作为你和 Claude Code 的协作说明书。
> 建议改名为 `README.md` 或保留为 `claude.template.md` 参考。

---

## 一、这是什么

一套 **AI 辅助 Go 后端开发** 的 starter 模板。它不是框架——不给你 ORM、不给你 router、不限定架构。它给的是：

- **10 条编码约束**（反例免疫格式，AI 看到边界而不是期望）
- **8 个核心 Skill**（brainstorming → plan → execute → review 完整工作流）
- **Harness 机械执法**（操作前预验证 + 操作后影响扫描 + 层级方向检查）
- **模板生成系统**（`make init-*` 按需展开，不塞给你不需要的东西）

---

## 二、5 分钟上手

### 2.1 复制 starter

```bash
cp -r go_backend_starter my-project && cd my-project
rm -rf .git && git init
```

此时你的项目只有骨骼：

```
CLAUDE.md    Makefile    go.mod    harness.json    .gitignore
```

### 2.2 配置本地环境

复制并编辑 `claude.local.md`：

```bash
cp claude.local.md.example claude.local.md   # 如果存在 example
# 或者自己写一个
```

填入你的 Go 路径、模块名、个人偏好。这个文件不进 git。

### 2.3 展开项目骨架

```bash
make init-internal    # cmd/ + internal/ + configs/
make init-docker      # Dockerfile + docker-compose.yml
make init-cicd        # .github/workflows/ci.yml

# 或一键全部
make init-all
```

### 2.4 安装全局 Skills（一次，所有项目共享）

```bash
cp docs/docs-ref/templates/global-skills/deep-thinking.md ~/.claude/skills/
cp docs/docs-ref/templates/global-skills/coordinator-delegation.md ~/.claude/skills/
# ... 等 6 个
```

### 2.5 开始编码

现在打开 Claude Code，说：

> "帮我加一个用户注册接口"

AI 会自动走 **brainstorming → plan → execute → review** 完整流程。

---

## 三、工作流全景

```
你说话 → brainstorming（探代码+澄清需求+出方案）
              ↓
         plan（拆 Task，L1/L2/L3 分级）
              ↓
         execute（9 步：读→实现→审查→构建→测试→verify→提交）
              ↓
         review（双模型交叉审查，核心变更时自动触发）
```

### 什么时候用哪个

| 场景 | 走哪条路 |
|------|---------|
| 修 bug、加小功能（≤2 文件） | brainstorming → plan(L1) → execute |
| 单模块功能（3-8 文件） | 同上，plan(L2) |
| 大型功能 / Epic / 跨模块 | plan(L3 spec 三文件) → spec-do |
| 只需要规划不想实现 | 只到 plan，不触发 execute |

更细的选择指南见 `docs/docs/docs-ref/workflow-bridge.md`。

---

## 四、Harness —— 你的架构不会悄悄烂掉

Harness 是"机械执法"系统——在你写代码前/后自动检查架构约束，而不是靠 AI 自觉。

### 层级规则

```
Layer 0: types/         纯类型，零依赖
Layer 1: utils/         工具函数，只依赖 L0
Layer 2: config/        配置，只依赖 L0-1
Layer 3: core/service/  业务逻辑，只依赖 L0-2
Layer 4: handler/api/   接口层，只依赖 L0-3
```

高层可 import 低层。反过来就是违规。

### 什么时候触发

| 时刻 | 触发什么 | 做什么 |
|------|---------|--------|
| 创建新文件/加 import 前 | pre-verify | 检查层级方向 |
| 改完函数签名/接口/模型 | change-impact-scan | Grep 全部调用点 |
| 每 5-10 个 Task 后 | harness-audit | 健康度 0-100 分 |
| CI 中 | lint-arch | lint-deps + lint-quality |

### 手动运行

```bash
make lint-arch      # 层级 + 质量检查
make harness-audit  # 健康度评分
```

---

## 五、Skill 是怎么工作的

你不需要记住 19 个 Skill。Claude Code 会根据你的话自动匹配。

**你只说关键词**，AI 会调起对应 Skill：

| 你说 | AI 自动触发 |
|------|-----------|
| "加功能" "修 bug" "重构" | brainstorming |
| "写测试" "覆盖率" | test-strategy |
| "审查" "review" | adversarial-review |
| "写协议" "API 契约" | protocol-driven-development |
| "部署" "CI/CD" "Docker" | project-delivery |
| "层级检查" "harness" | harness-go |

**不需要记全名，不需要记参数。** 就像你不会记每个 Linux 命令的 man page——需要时 man 一下就行。

---

## 六、claude.local.md 怎么用

`claude.local.md` 是你个人的"本地覆盖文件"。放在项目根目录，不进 git。

```yaml
# 例：告诉 AI 你的 Go 装在非标准路径
go_bin: /opt/go/bin/go

# 例：提交前展示 diff
confirm_before_commit: true

# 例：个人模型偏好
default_model: sonnet
```

CLAUDE.md 会在会话启动时自动查找并加载它。local 的优先级高于 CLAUDE.md 默认值。

---

## 七、常见问题

### Q: 为什么不用 Gin/Echo/Fiber？

Starter 不绑定框架。你可以 `make init-internal` 之后自己加。约束体系是框架无关的。

### Q: 25 个 Skill 怎么记得住？

记不住。你只需要记 4 个关键词：**加功能 → brainstorming、审查 → review、写测试 → test、层级 → harness**。其他 AI 自己会匹配。

### Q: make init-* 展开之后还能改吗？

当然。模板只是起点，展开之后就是你的代码，随便改。

### Q: 怎么知道 harness 过没过？

```bash
make lint-arch     # 绿色 = 过
make harness-audit # 目标 ≥ 80 分
```

### Q: Global Skills 和项目 Skills 有什么区别？

Global Skills（deep-thinking、prompt-engineering 等）是跨项目通用的 AI 能力——装一次，所有项目共享。项目 Skills 是这个 starter 特有的工作流约束。

### Q: 我不想要某个 Skill 怎么办？

直接删掉对应的 `.md` 文件即可。Skill 体系是可选可裁剪的。
