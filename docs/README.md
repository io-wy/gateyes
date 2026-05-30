# Gateyes 文档中心

> **定位**：融合本地 GPU 模型与云上 LLM API 的智能网关
> **核心能力**：智能路由、负载均衡、性能成本优化

---

## 快速开始

| 文档 | 适合谁 | 内容 |
|---|---|---|
| [快速开始](./quickstart.md) | 第一次使用 | Docker Compose 一键部署、首次请求示例 |
| [部署指南](./deployment.md) | 运维 / SRE | K8s Helm 部署、配置热重载、Ingress 部署 |

## 架构与设计

| 文档 | 内容 |
|---|---|
| [架构总览](./architecture.md) | 系统架构、分层设计、数据流、部署拓扑 |
| [Provider 配置](./provider-configuration.md) | 本地 GPU 模型（vLLM/SGLang）与云上 API 的统一接入配置 |
| [智能路由](./routing.md) | 路由策略、规则引擎、亲和层、健康检查与熔断 |
| [性能成本优化](./cost-optimization.md) | L1 响应缓存、预算治理、成本追踪、性价比路由 |

## 核心机制

面向维护者和进阶用户，解释"代码现在到底在做什么"。

| 文档 | 深度 | 内容 |
|---|---|---|
| [运行时机制](./runtime-mechanisms.md) | 总览 | 鉴权、限流、路由、权限模型、预算治理、监控 |
| [L1 缓存](./cache.md) | 深入 | 精确匹配响应缓存：Redis + 内存 LRU fallback |
| [亲和层](./affinity.md) | 深入 | SessionAffinity + PrefixAffinity 软固定层 |
| [限流器](./limiter.md) | 深入 | 多维度令牌桶：内存/Redis 双后端、Fail-Open |
| [监控](./monitoring.md) | 深入 | 14 个 Prometheus 指标、PromQL 推荐、Grafana 基线 |
| [测试策略](./testing.md) | 参考 | 覆盖率、Benchmark、CI 检查项 |

## 运维手册

| 文档 | 场景 |
|---|---|
| [Runbook](./operations/runbook.md) | On-call 排障 |
| [升级](./operations/upgrade.md) | 版本升级 |
| [回滚](./operations/rollback.md) | 紧急回滚 |
| [备份恢复](./operations/backup-and-restore.md) | 数据备份与恢复 |
| [密钥与配置](./operations/secrets-and-config.md) | 环境变量、Secret 分层 |
| [CI/CD](./operations/ci-cd.md) | 发布流程 |

## 基线资产

| 文件 | 用途 |
|---|---|
| [openapi.json](./assets/openapi.json) | OpenAPI 3.0 规范 |
| [grafana-dashboard.json](./assets/grafana-dashboard.json) | Grafana Dashboard 基线 |
| [prometheus-alerts.yml](./assets/prometheus-alerts.yml) | Prometheus 告警规则基线 |
