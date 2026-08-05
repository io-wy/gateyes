# Gateyes

Gateyes 是一个面向生产环境的 LLM 推理控制面。它提供 OpenAI 兼容和 Anthropic 兼容接口，统一本地 GPU 推理集群与云端 LLM API，并集中管理模型发现、智能路由、自动扩缩容、响应缓存、鉴权、配额、预算、追踪、指标和插件执行。

## 能提供什么

- 统一 API 接口：`/v1/responses`、`/v1/chat/completions`、`/v1/messages`、`/v1/embeddings`、`/v1/images/generations`
- 多租户鉴权、RBAC、OIDC 管理登录、API key 和 virtual key
- 带 fallback、重试、熔断和健康检查的 provider 路由
- 基于 Redis 的分布式限流，带内存降级
- 面向 project、tenant、API key 和 virtual key 的预算与限额控制
- 带 Redis + 内存降级的 L1 响应缓存、singleflight、cache hint 和 prompt rewrite 优化
- 基于 Kafka 的持久化 eventbus 和异步 batch 推理任务
- Prometheus 指标、OTLP tracing、审计日志和 provider 运行态统计
- WASM 和 gRPC 插件支持，覆盖 gateway 生命周期各阶段
- React 管理后台：dashboard、providers、keys、services、responses、audit 和 API playground
- Kubernetes operator 模式，支持 `InferenceService`、`ModelEndpoint`、`RoutePolicy`、`BudgetPolicy` 和 `InferenceAutoscalePolicy`

## 部署模式

| 模式 | 含义 |
| --- | --- |
| `gateway` | 仅数据面和管理 API |
| `maas` | gateway 加租户侧 MaaS 能力 |
| `operator` | 仅 CRD 和 operator 控制面 |
| `full` | gateway、MaaS、CRD 和 operator 全部启用 |

Helm chart 使用 `platform.mode` 表示部署意图。

## 仓库布局

```text
cmd/gateway/               gateway 二进制和 admin API
cmd/operator/              Kubernetes CRD 读取与 reconcile 循环
configs/                   运行配置示例和本地配置入口
deploy/                    Docker 与 Helm 部署资产、Grafana/Prometheus
docs/docs-project/         项目文档、runbook 和运维说明
internal/app/config/       配置加载、校验和热更新编排
internal/handler/          HTTP handler、中间件、指标和服务装配
internal/pkg/              内部通用库：db/redis、trace、eventbus、logging
internal/service/          业务服务：routing、provider、limiter、responses、auth、platform sync
internal/service/platform/  CRD 形状的平台资源、autoscale 和 workload 规划
plugins/                   插件 SDK 和示例
proto/plugin/v1/           插件协议源码
web/                       React 管理后台和 API playground
```

## 快速开始

Gateyes 通过 Viper 读取 YAML 配置。`.env` 会先于 `${VAR}` 占位符解析加载，真实环境变量优先于 `.env`。

```bash
cp .env.example .env
cp configs/config.example.yaml configs/config.yaml
# 编辑 .env：数据库 DSN、provider key、bootstrap secret
```

本地运行：

```bash
go run ./cmd/gateway -config configs/config.yaml
```

演示配置：

```bash
go run ./cmd/gateway -config configs/demo-mock.yaml
```

## Kubernetes / Operator

Gateyes 的 Kubernetes 形态分成 gateway 数据面和可选 operator 控制面。gateway 仍然是纯请求路径进程，不在推理流量中查询 Kubernetes API。operator 读取 Gateyes CRD，并把声明式资源同步到 Admin API 状态和 Kubernetes 工作负载。

chart 会安装这些 CRD：

- `GateyesGateway`
- `ModelEndpoint`
- `RoutePolicy`
- `BudgetPolicy`
- `InferenceAutoscalePolicy`
- `InferenceService`

启用 operator：

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  --set platform.mode=full \
  --set platform.operator.enabled=true
```

默认情况下 operator 运行在 dry-run 模式。它会读取上面的 CRD，并输出 provider、router、budget、autoscale decision 和 workload 的计数。设置 `platform.operator.dryRun=false` 后，它会先把 `InferenceService` reconcile 成 Kubernetes `Deployment` / `Service`，再通过 Admin API 同步 provider、router 和 budget 变更。

`InferenceService.spec.exposeAsModelEndpoint` 默认为 true，因此自托管服务也会被暴露成一个 `ModelEndpoint`，指向 `http://<service>.<namespace>.svc:<port>/<openAIPath>`。`routeLabels` 会复制到 provider labels，供 `RoutePolicy.spec.endpointSelector` 使用。`InferenceAutoscalePolicy` 在 `mode=enforce` 时会在规划阶段调整 workload replicas。

`platform.operator.namespace` 为空表示使用集群级 RBAC；如果设置为具体 namespace，就只在该 namespace 内运行。

## 冷启动（共享 PostgreSQL）

本地开发使用共享 PostgreSQL 实例时（容器名 `postgres`，端口 `5432`）：

```bash
# 1. 配置密钥（如果还没有）
cp .env.example .env
# 编辑 .env：GATEYES_ADMIN_BOOTSTRAP_SECRET、GATEYES_DEMO_SECRET、provider key

# 2. 一键：基础设施 + DB + gateway + admin 验证
make give-me-an-admin
```

这个命令会：
1. 如果不存在，就启动 `postgres:16-alpine` 和 `redis:7-alpine`
2. 创建 `gateyes` role 和 database，并修正 PostgreSQL 15+ `public` schema 的 owner
3. 启动 gateway，等待 `/ready` 返回 200
4. 验证 bootstrap admin（`admin-key-001`）可以正常认证

完成后可以使用：

- **Admin**：`Authorization: Bearer admin-key-001:$GATEYES_ADMIN_BOOTSTRAP_SECRET`
- **Demo user**：`Authorization: Bearer demo-key-001:$GATEYES_DEMO_SECRET`

手动等价操作：

```bash
make provision-db
make run
```

## 告警

Gateyes 可以在预算耗尽、provider 状态变化或配额跨阈值时发送告警。

### Webhook（默认）

在 `config.yaml` 中配置 `alert.webhookURL` / `webhookSecret`，或使用 `alert.channels` 进行多 webhook 和标签路由。

### 飞书（Lark）

使用官方 Feishu SDK channel 向用户或群聊发送文本或交互卡片消息。

1. 创建飞书应用，并授予 `im:chat:send` / `im:message:send` 权限
2. 在 `configs/config.yaml` 中加入：

```yaml
alert:
  enabled: true
  channels:
    - name: feishu-ops
      type: feishu
      feishuAppId: ${FEISHU_APP_ID}
      feishuAppSecret: ${FEISHU_APP_SECRET}
      feishuReceiveType: chat_id
      feishuReceiveId: ${FEISHU_CHAT_ID}
      feishuMsgType: interactive
      labels:
        severity: critical
```

3. 在 `.env` 中设置：

```bash
FEISHU_APP_ID=cli_xxx
FEISHU_APP_SECRET=xxx
FEISHU_CHAT_ID=oc_xxx
```

## 模型别名

provider 可以声明 alias，让客户端继续使用原生模型名，而 gateway 在上游转发时映射成 provider-specific 名称。

```yaml
providers:
  - name: deepseek
    type: openai
    model: deepseek-v4-flash
    modelAliases:
      claude-sonnet-4-6: deepseek-v4-flash
      claude-opus-4-6: deepseek-v4-pro
      gpt-5.4: deepseek-v4-flash
```

## API 接口

| 接口 | 兼容性 | 用途 |
| --- | --- | --- |
| `/v1/responses` | OpenAI Responses | 主请求路径 |
| `/v1/chat/completions` | OpenAI Chat Completions | 兼容现有 OpenAI SDK |
| `/v1/messages` | Anthropic Messages | 兼容 Anthropic SDK |
| `/v1/embeddings` | OpenAI Embeddings | 文本向量 |
| `/v1/images/generations` | OpenAI Images | 图片生成 |

所有请求接口共享 provider 选择、重试/fallback、持久化、预算、限流、追踪和指标。

## 插件

插件协议源码在 `proto/plugin/v1/`；生成后的 Go 代码在 `pkg/plugin/v1/`。

```bash
make proto
```

- WASM 插件使用 `plugins/sdk/gateyes`
- gRPC 插件 import `github.com/gateyes/gateway/pkg/plugin/v1`
- 运行时适配器在 `internal/extension/plugin`

完整指南：[docs/docs-project/plugin-development.md](./docs/docs-project/plugin-development.md)

## 开发

```bash
make fmt
make test
make vet
make lint-arch
make proto
make run
```

默认回归：

```bash
go test ./...
```

聚焦 gateway 兼容性：

```bash
go test ./internal/service/provider ./internal/service/responses ./internal/handler
```

## 文档

| 文档 | 用途 |
| --- | --- |
| [feature-inventory.md](./docs/docs-project/feature-inventory.md) | 当前已实现能力清单 |
| [architecture.md](./docs/docs-project/architecture.md) | 架构和职责边界 |
| [runtime-mechanisms.md](./docs/docs-project/runtime-mechanisms.md) | 鉴权、路由、限额、缓存、预算机制 |
| [api-reference.md](./docs/docs-project/api-reference.md) | HTTP API 请求体和响应体参考 |
| [database-ddl.md](./docs/docs-project/database-ddl.md) | 数据库最终表结构 DDL |
| [cache.md](./docs/docs-project/cache.md) | L1 cache、singleflight、prompt rewrite 和可观测性 |
| [kafka-batch-inference.md](./docs/docs-project/kafka-batch-inference.md) | Kafka eventbus 和 batch 推理任务 |
| [provider-configuration.md](./docs/docs-project/provider-configuration.md) | provider 配置 |
| [plugin-development.md](./docs/docs-project/plugin-development.md) | WASM 与 gRPC 插件指南 |
| [deployment.md](./docs/docs-project/deployment.md) | Docker Compose、Helm、生产说明和 operator 模式 |
| [monitoring.md](./docs/docs-project/monitoring.md) | 指标和告警 |
| [operations/runbook.md](./docs/docs-project/operations/runbook.md) | 运维 runbook |

## 部署

Docker Compose：

```bash
docker compose up --build -d
```

Helm：

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-prod.yaml
```

本地开发用 `.env` 保存密钥。生产环境请使用 Kubernetes Secret 或外部密钥管理系统，不要把 provider key 或 bootstrap secret 提交到仓库。
