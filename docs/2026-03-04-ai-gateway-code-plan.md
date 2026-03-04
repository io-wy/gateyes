# AI Gateway Code Plan（详细版）

日期: `2026-03-04`  
状态: `Planned`  
范围: `仅规划，不含本次代码实现`

## 1. 项目目标

建设一个可生产部署的 AI 网关，满足以下核心目标：

- 对外提供 OpenAI 兼容 API（`/v1/*`）
- 支持多上游、多渠道调度与弹性回退
- 具备稳定的并发与限流控制能力
- 提供完整可观测性（Prometheus + Grafana + 告警）
- 与 `sub2api` 的成熟实践保持架构一致性（缩小版实现）

## 2. 本期范围与非目标

本期范围（In Scope）：

- 网关请求主链路稳定化
- 调度、并发、失败切换机制完善
- 管理 API 与配置治理
- 指标、日志、追踪最小可用落地
- Grafana 看板与基础告警
- 压测与发布基线

非目标（Out of Scope）：

- 多租户复杂计费系统
- 完整运营后台前端
- 高阶风控/反欺诈能力
- 全链路分布式追踪平台自建（可先接 OpenTelemetry exporter）

## 3. 成功标准（Success Criteria）

- 可用性：核心转发链路 7x24 稳定，支持灰度发布
- 性能：目标并发下错误率 < 1%，p95 延迟可控
- 可观测：关键 RED 指标全量可见，告警可触发且可处理
- 可运维：问题可定位到 token / channel / upstream
- 可演进：新增上游渠道不需要大规模重构

## 4. 目标架构（对齐 sub2api 思路）

请求路径（目标态）：

1. 网关鉴权（Token、模型权限、IP 白名单）
2. 请求上下文构建（`request_id`、`session_id`、`group`、`model`）
3. 并发入口控制（全局 + token 等）
4. 调度选路（Sticky -> Priority/Weight -> WaitPlan）
5. 上游转发（含重试、超时、错误分类）
6. 流式/非流式响应透传
7. 异步记录（请求统计、token 消耗、错误分类）
8. 指标暴露（Prometheus）+ 管理查询（Admin Metrics）

关键设计原则：

- 控制面与数据面分离：Admin API 不影响主转发链路
- 所有限制策略可配置：并发、重试、粘性 TTL、回退等待
- 避免高基数标签污染指标系统
- 所有关键动作打结构化日志并带 `request_id`

## 5. 代码实施分阶段计划

## Phase 0：基线与契约冻结（0.5~1 天）

目标：

- 明确 API 契约和技术边界，避免后续反复返工。

任务：

- 冻结 `docs/api.md`（网关契约、错误语义、监控契约）
- 明确环境变量规范和默认值
- 固化模块边界（`handler` / `middleware` / `scheduler` / `concurrency`）

交付物：

- API 文档（已更新）
- 本 code plan 文档

验收标准：

- 团队对接口与行为无歧义
- 开发与测试可据此编写用例

---

## Phase 1：工程稳定化与编译基线（1~2 天）

目标：

- 建立“可编译、可启动、可基础联调”的稳定骨架。

任务：

- 统一包结构（消除同目录多 package 冲突）
- 修复明显绑定/参数处理缺陷
- 补齐依赖锁定（`go.sum`、构建缓存策略）
- 统一错误响应格式工具函数

交付物：

- 编译通过的服务二进制
- 基础健康检查可用

验收标准：

- `go build ./...` 成功
- 本地最小启动路径成功（DB/Redis 可连通）

---

## Phase 2：网关主链路完善（2~3 天）

目标：

- 让 `/v1/chat/completions`、`/v1/embeddings`、`/v1/models` 行为稳定一致。

任务：

- 统一请求解析与模型提取逻辑（避免 body 重复消费）
- 完善模型映射与 provider 识别
- 标准化流式响应透传（SSE、断流处理）
- 补充上游超时与上下文取消处理

交付物：

- 稳定的 OpenAI 兼容入口
- 标准错误映射（参数错、鉴权错、上游错）

验收标准：

- 网关端到端用例通过（流式/非流式）
- 响应格式对主流 OpenAI SDK 可兼容

---

## Phase 3：调度与并发控制强化（2~3 天）

目标：

- 完成可配置、可解释、可观测的调度与并发机制。

任务：

- Sticky Session（TTL、失效、回收）
- Priority + Weight 选择与候选重试
- WaitPlan 机制（返回 202 + 建议等待）
- 并发控制三层：global / channel / token
- Redis 原子操作完善（防计数漂移）

交付物：

- 可配置调度与并发策略
- 并发状态查询 API

验收标准：

- 并发超限场景行为稳定（无明显负值计数/泄漏）
- 压测下渠道负载符合优先级与权重预期

---

## Phase 4：上游可靠性与失败切换（2 天）

目标：

- 上游异常不放大，具备自动恢复与切换能力。

任务：

- 上游错误分类（429/5xx/网络超时/协议错误）
- 指数退避 + 抖动重试（仅可重试错误）
- 账户/渠道故障短时降权或熔断（可配置）
- 限流头解析（如 `Retry-After`）并用于调度决策

交付物：

- 可控重试策略
- 失败切换策略与参数化配置

验收标准：

- 人为注入上游故障时，网关仍可维持可用
- 错误不会无限重试或形成风暴

---

## Phase 5：可观测性与 Grafana 落地（2~3 天）

目标：

- 实现“可看见、可告警、可定位”的最小可用观测体系。

任务：

- 结构化日志（JSON）+ 关键字段统一
- Prometheus `/metrics` 指标暴露
- 指标覆盖：RED + 调度 + 并发 + 上游
- Grafana Dashboard（3 套：Gateway/Upstream/Capacity）
- Prometheus Alert Rules（高错误率、高延迟、无可用渠道、依赖不可用）

交付物：

- `monitoring/prometheus.yml`
- `monitoring/alerts.yml`
- `monitoring/grafana-dashboard-*.json`

验收标准：

- 看板可直接导入并展示数据
- 告警规则可在演练中触发并恢复

---

## Phase 6：管理面与治理能力（1~2 天）

目标：

- 让 Admin API 具备生产可用的配置治理体验。

任务：

- 渠道/Token/调度/并发配置接口补齐一致性校验
- 配置变更审计日志
- 敏感字段脱敏返回（API key/token）
- 管理接口鉴权与访问控制（最小权限）

交付物：

- 稳定管理 API
- 审计日志规范

验收标准：

- 非法参数与越权访问被正确拒绝
- 配置变更可追踪可回滚

---

## Phase 7：测试、压测、发布（2~3 天）

目标：

- 以可回滚方式上线，保证稳定运行。

任务：

- 单元测试（调度、并发、错误映射）
- 集成测试（DB + Redis + 上游 mock）
- 压测（k6/vegeta）：稳态、突增、故障注入
- 发布策略：灰度 + 回滚开关 + SLO 观察窗口

交付物：

- 测试报告
- 压测报告
- 发布 Runbook

验收标准：

- 达到既定 SLO 目标
- 回滚流程演练通过

## 6. 监控实施清单（详细）

指标最小集：

- 请求：`http_requests_total`, `http_request_duration_seconds`, `http_inflight_requests`
- 上游：`upstream_requests_total`, `upstream_errors_total`, `upstream_request_duration_seconds`
- 调度：`scheduler_selected_total`, `scheduler_wait_plan_total`
- 并发：`concurrency_inuse`, `concurrency_limit`
- 业务：`tokens_consumed_total`

日志字段最小集：

- `timestamp`, `level`, `request_id`, `session_id`
- `token_id_masked`, `channel_id`, `provider`, `model`
- `latency_ms`, `status_code`, `error_type`

Dashboard 最小面板：

- 概览：RPS、错误率、p95、inflight
- 路由：各接口成功/失败趋势
- 调度：layer 命中占比、wait plan 次数
- 上游：provider 成功率、429/5xx 趋势
- 容量：global/channel/token 并发占用率

告警初始阈值（可调）：

- 错误率 > 5%（5 分钟）
- p95 > 2 秒（10 分钟）
- wait_plan 比例 > 20%（10 分钟）
- 可用渠道 = 0（1 分钟）
- Redis/DB 健康检查失败（1 分钟）

## 7. 风险与缓解

风险 1：并发计数漂移导致“假超限”或“假空闲”

- 缓解：原子脚本 + 定期修正 + 连接中断补偿

风险 2：高基数指标拖垮 Prometheus

- 缓解：限制标签基数，token 使用聚合维度

风险 3：上游异常引发重试风暴

- 缓解：限制重试次数 + 熔断窗口 + 指数退避

风险 4：管理接口安全薄弱

- 缓解：强制管理员鉴权 + 内网暴露 + 审计日志

## 8. 里程碑建议

- M1（第 1 周）：Phase 0~2 完成，可稳定转发
- M2（第 2 周）：Phase 3~5 完成，可观测性上线
- M3（第 3 周）：Phase 6~7 完成，具备上线条件

## 9. 完成定义（Definition of Done）

- API 行为与文档一致
- 关键路径测试与压测通过
- Grafana 看板和告警可用
- 运行手册与回滚策略完备
- 上线后可在 5 分钟内定位大多数核心故障

## 10. 执行顺序建议

建议严格按以下顺序推进：

1. 先完成 Phase 1（工程基线），避免后续反复修补
2. 再完成 Phase 2~4（核心功能可靠性）
3. 再完成 Phase 5（监控）并进行告警演练
4. 最后推进 Phase 6~7（治理、测试、发布）
