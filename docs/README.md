# Gateyes 文档中心

> 本文档按受众和场景组织，按需阅读。

---

## 快速开始

| 文档 | 适合谁 | 内容 |
|---|---|---|
| [Deployment](./deployment.md) | 运维 / SRE | Docker Compose 一键部署、K8s Helm 部署、首次使用 curl 示例 |
| [Secrets and Config](./secrets-and-config.md) | 运维 / 安全 | 环境变量契约、密钥分层、生产 secret backend 建议 |

## API 规范

| 文档 | 说明 |
|---|---|
| [openapi.json](./openapi.json) | OpenAPI 3.0 规范，覆盖全部 50+ endpoint，可导入 Postman / Swagger UI / Stoplight |

## 架构与核心机制

面向维护者和进阶用户，解释"代码现在到底在做什么"。

| 文档 | 深度 | 内容 |
|---|---|---|
| [Runtime Mechanisms](./runtime-mechanisms.md) | 总览 | 鉴权、限流、路由、权限模型、预算治理、监控六大机制的完整实现说明 |
| [Provider Protocol](./provider-protocol.md) | 总览 | Provider 协议边界设计、内部统一协议、新增 adapter 指南 |
| [L1 Cache](./cache.md) | 深入 | 精确匹配响应缓存：Redis 主缓存 + 内存 LRU fallback、Cache Key 设计、Fail-Open、分布式一致性 |
| [Affinity Layer](./affinity.md) | 深入 | SessionAffinity + PrefixAffinity 软固定层：加权一致哈希、prefix-cache 优化、向后兼容 |
| [Limiter](./limiter.md) | 深入 | 多维度令牌桶算法的完整实现：内存/Redis 双后端、Lua 脚本、队列行为、Fail-Open |
| [Monitoring](./monitoring.md) | 深入 | 14 个 Prometheus 指标口径、label 语义、埋点边界、推荐 PromQL |
| [Testing](./testing.md) | 参考 | 测试策略、覆盖率报告、Benchmark 体系、CI 检查项、调试指南 |
**阅读建议**：先看 `runtime-mechanisms.md` 建立整体认知，再根据需求深入 `limiter.md` 或 `monitoring.md`。

## 运维手册

| 文档 | 场景 |
|---|---|
| [Runbook](./runbook.md) | On-call 排障：健康检查失败、错误率飙升、TTFT 延迟、Redis 故障 |
| [Upgrade](./upgrade.md) | 版本升级：pre-check、Helm upgrade、验证步骤 |
| [Rollback](./rollback.md) | 紧急回滚：Helm rollback、DB restore、checklist |
| [Backup and Restore](./backup-and-restore.md) | 数据备份与恢复策略：SQLite / Postgres / MySQL |
| [CI/CD](./ci-cd.md) | CI 检查项、Release 流程、Live provider 矩阵测试 |

## 基线资产

开箱即用的监控配置，直接导入即可使用。

| 文件 | 用途 |
|---|---|
| [grafana-dashboard.json](./grafana-dashboard.json) | Grafana Dashboard 基线 |
| [grafana-dashboard.example.json](./grafana-dashboard.example.json) | Dashboard 最小样例 |
| [prometheus-alerts.yml](./prometheus-alerts.yml) | Prometheus 告警规则基线 |
| [prometheus-alerts.example.yml](./prometheus-alerts.example.yml) | 告警规则最小样例 |

## 报告与评估

| 文档 | 内容 |
|---|---|
| [Tech Highlights Report](./tech-highlights-report.md) | 技术亮点、benchmark 实测数据、与同类项目对比 |

## 项目演进（内部设计文档）

以下文档位于仓库根目录 `design/`，面向核心贡献者和维护者：

| 文档 | 内容 |
|---|---|
| [design/vnext-prd.md](../design/vnext-prd.md) | vNext 产品定位与严格能力差距分析（对标 APIPark / LiteLLM） |
| [design/capability-gap-prd.md](../design/capability-gap-prd.md) | 20 条真实痛点 PRD：已做 / 部分做 / 未做 / 优先级收敛 |
| [design/plans/](../design/plans/) | 已实现的功能切片实施计划（provider registry、project budget） |
