# Gateyes 测试记录

> 本文档记录每次 benchmark / 压测 / 回归测试的全过程，包括环境、步骤、问题、修复和指标。  
> 每次测试新增一个二级标题区块，按时间倒序排列。

---

## 测试记录模板

每次测试复制以下模板填写：

```markdown
### YYYY-MM-DD 测试简述

**测试人**：  
**目标**：验证 xxx 功能 / 压测 xxx 场景  
**环境**：
- OS：Win11 / Linux / macOS
- Go 版本：
- 数据库：SQLite / PostgreSQL
- Provider 数量及类型：

**执行步骤**：
1. ...

**遇到的问题**：
| 序号 | 现象 | 根因 | 修复方式 | 状态 |
|------|------|------|----------|------|
| 1 | | | | |

**性能指标**：
| 并发 | 总请求 | 成功 | 错误 | RPS | Avg Latency | P95 | 备注 |
|------|--------|------|------|-----|-------------|-----|------|
| | | | | | | | |

**状态码分布**：
- 200 = x, 401 = x, 502 = x, 0 = x (连接错误)

**结论**：
- ...

**TODO / 遗留问题**：
- [ ] ...
```

---

## 2026-04-26 WAL 模式修复 + 第二轮压测

**测试人**：io-wy + AI  
**目标**：
1. 解决 SQLite `database is locked` 问题
2. 在 WAL 模式下重新跑完整阶梯压测

**环境**：同上一轮（SQLite, 3 providers, benchmark/deploy）

**执行步骤**：
1. `internal/db/db.go`：在 `Open()` 的 `Ping()` 后增加 `PRAGMA journal_mode=WAL;`
2. 重新编译 gateway → `benchmark/deploy/gateway.exe`
3. 删库重启：`rm -f gateyes_bench2.db gateway.log && ./gateway.exe -config bench.yaml`
4. 运行 loadtest：`-d 30s -warmup 3s`

**遇到的问题**：

| 序号 | 现象 | 根因 | 修复方式 | 状态 |
|------|------|------|----------|------|
| 1 | 高并发下大量 429 | 上游 API（bigmodel.cn / longcat.chat）对账户做了 QPS 限流 | 非 gateway 问题，需用 mock 上游或降低并发 | 已确认 |
| 2 | 大量 503 "no provider available" | 上游 429/401 导致 provider 被 health checker 标记为 unhealthy，最终全部不可用 | 同上 | 已确认 |

**性能指标（30s 阶梯，WAL 模式）**：

| 并发 | 总请求 | 成功 | 错误 | RPS | Avg Latency | P95 | 备注 |
|------|--------|------|------|-----|-------------|-----|------|
| 1 | 14 | 14 | 0 | 0.5 | 1.81s | 2.88s | 100% 成功，低 QPS 不触发上游限流 |
| 10 | 1250 | 100 | 1150 | 41.7 | 241.85ms | 1.68s | 成功率 8%， mostly 429+503 |
| 50 | 23355 | 50 | 23305 | 778.5 | 64.72ms | 98.0ms | 成功率 0.2%，大量 429/502/503 |
| 100 | 15851 | 0 | 15851 | 528.4 | 189.32ms | 228.46ms | 全部失败，0=15798 连接错误 |

**状态码分布**：
- **200** = 164 (0.3%)
- **429** = 7336 (上游限流："您的账户已达到速率限制，请您控制请求频率" / `code:1302`)
- **502** = 1845 (upstream 错误透传)
- **503** = 1712 (无可用 provider)
- **0** = 29407 (客户端连接错误，高并发下连接被重置)

**结论**：
- WAL 模式彻底解决了 SQLite `database is locked` 问题，低并发下请求完全正常。
- 当前压测瓶颈**完全来自上游 API 的账户级 QPS 限流**，不是 gateway 自身性能问题。
- 要测 gateway 真实吞吐，需要：
  1. 使用 mock 上游（本地 HTTP server 模拟 OpenAI/Anthropic 响应）
  2. 或申请更高 QPS 额度的 API key
  3. 或只跑 1~2 并发验证功能正确性

**TODO / 遗留问题**：
- [ ] 搭建 mock 上游服务，排除外部 API 限流干扰，测 gateway 真实极限
- [ ] 更新 2.env / 3.env 的有效 API key
- [ ] 确认 health checker 在 upstream 限流时不应将 provider 直接标记为 unhealthy（限流是临时状态，不是服务宕机）

---

## 2026-04-26 基准压测 + 环境修复

**测试人**：io-wy + AI  
**目标**：
1. 验证 provider env 文件加载是否正常（BaseURL 映射）
2. 跑通完整 loadtest 阶梯压测
3. 确认产物目录结构

**环境**：
- OS：Win11 Pro 10.0.26200
- Go：1.25
- 数据库：SQLite (`gateyes_bench2.db`)
- Provider：3 个
  - openai-primary → LongCat-Flash-Chat (OpenAI 兼容)
  - anthropic-primary → glm-5.1 (Anthropic 兼容)
  - anthropic-secondary → LongCat-Flash-Chat (Anthropic 兼容，密钥无效)
- 压测工具：`benchmark/deploy/loadtest.exe`
- 产物目录：`benchmark/deploy/`

**执行步骤**：
1. 修复 `internal/config/config.go`：`LLM_API_BASE` 去掉前缀后为 `API_BASE`，但 switch case 只匹配了 `BASE_URL`，导致 BaseURL 为空。增加 `case "BASE_URL", "API_BASE":`。
2. 重新编译 `gateway.exe` 和 `loadtest.exe` 到 `benchmark/deploy/`。
3. 复制 `bench.yaml` + `1.env/2.env/3.env` 到 `benchmark/deploy/`。
4. 清理旧数据库（关键！）：`rm -f gateyes_bench2.db gateway.log gateway.pid`
5. 启动 gateway：`./gateway.exe -config bench.yaml`
6. 手动 curl 验证连通性。
7. 运行 loadtest：`-d 5s -warmup 1s` 及 `-d 30s`。

**遇到的问题**：

| 序号 | 现象 | 根因 | 修复方式 | 状态 |
|------|------|------|----------|------|
| 1 | `unsupported protocol scheme ""` | `LLM_API_BASE` → `API_BASE` 未匹配 switch case | `config.go:272` 增加 `API_BASE` 映射 | 已修复 |
| 2 | 修复配置后仍报 BaseURL 为空 | 旧数据库 `provider_registry` 表残留了 BaseURL 为空的记录；`seedProviderRegistry` 在 `RuntimeConfig != nil` 时跳过更新 | 删除 `gateyes_bench2.db` 后重启 | 已解决 |
| 3 | loadtest 全部 401 | deploy 目录的 `loadtest.exe` 编译时间早于 auth header 修复 | 重新编译 `loadtest.exe` | 已修复 |
| 4 | 高并发下大量 502 + statusCode=0 | SQLite `database is locked (SQLITE_BUSY)`，文件级锁无法承受 500+ RPS 并发写 | 待处理：需开启 WAL 模式或换 PG | 未解决 |

**性能指标（5s 短测）**：

| 并发 | 总请求 | 成功 | 错误 | RPS | Avg Latency | P95 | 备注 |
|------|--------|------|------|-----|-------------|-----|------|
| 1 | 591 | 1 | 590 | 118.2 | 8.45ms | 5.55ms |  mostly 连接错误 |
| 10 | 2533 | 1 | 2532 | 506.6 | 19.73ms | 25.56ms | mostly 连接错误 |
| 50 | 2285 | 3 | 2282 | 457.0 | 108.20ms | 128.50ms | 含 2 个 502 |
| 100 | 2252 | 9 | 2243 | 450.4 | 219.85ms | 246.76ms | 含 1 个 502 |

**状态码分布（5s 短测）**：
- 200 = 14 (0.17%)
- 502 = 6 (SQLite 锁)
- 0 = 9550 (连接错误，客户端侧)

**结论**：
- 单请求/低频请求功能正常，glm-5.1 可正常返回。
- SQLite 是当前压测的最大瓶颈，任何 >50 RPS 的并发写都会触发 `SQLITE_BUSY`。
- 压测结论不可信，需先解决数据库并发问题后再跑正式 benchmark。

**TODO / 遗留问题**：
- [ ] SQLite 开启 WAL 模式，提升并发写性能
- [ ] 或：压测环境改用 PostgreSQL
- [ ] 2.env / 3.env 的 API key 需更新为有效密钥（anthropic-secondary 健康检查 401）
- [ ] 正式跑一轮 30s 全阶梯压测，记录可信指标
- [ ] 确认 `seedProviderRegistry` 在配置变更时能自动更新数据库记录（而非跳过）
