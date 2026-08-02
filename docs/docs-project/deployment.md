# Deployment

## 0. Quickstart (Docker Compose)

最简单的方式，适合开发和试玩：

```bash
# 1. 克隆
git clone https://github.com/io-wy/gateyes.git && cd gateyes

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，至少填写一个 provider 的 API key（如 OPENAI_API_KEY）

# 3. 启动（网关 + PostgreSQL + Redis + Prometheus + Grafana）
docker compose up --build -d

# 4. 验证
curl http://127.0.0.1:8028/health
```

### 首次使用

```bash
# 1. 创建租户
curl -X POST http://127.0.0.1:8028/admin/v1/tenants \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"slug":"my-team","name":"My Team"}'

# 2. 创建用户（返回 api_key 和 api_secret）
curl -X POST http://127.0.0.1:8028/admin/v1/users \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"<tenant_id>","name":"alice","role":"tenant_user"}'

# 3. 用返回的凭据请求
curl -X POST http://127.0.0.1:8028/v1/chat/completions \
  -H "Authorization: Bearer <api_key>:<api_secret>" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}'
```

## 1. Local

### Prerequisites

1. Copy `.env.example` to `.env`
2. Fill provider secrets
3. Review `configs/config.example.yaml`

### Run with Docker Compose

```bash
docker compose up --build
```

Exposed endpoints:

1. Gateway: `http://127.0.0.1:8028`
2. Prometheus: `http://127.0.0.1:9090`
3. Grafana: `http://127.0.0.1:3000`
4. Redis: `localhost:6379`

Redis 用于分布式限流和告警去重，不部署 Redis 时自动降级为内存模式。

## 2. Kubernetes

Prerequisites:

1. Redis instance reachable from the cluster (required for distributed rate limiting and alert dedup)

Helm chart path:

```text
deploy/helm/gateyes
```

### Install dev

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-dev.yaml
```

### Install staging

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes-staging --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-staging.yaml
```

### Install prod

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes-prod --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-prod.yaml
```

## 3. Migration job

Helm chart includes a pre-install/pre-upgrade migration job:

1. Config mounted from ConfigMap
2. Secrets injected from Secret
3. Command: `/app/gateway-migrate -config /app/configs/config.yaml -action up`

## 4. Health probes

Deployment assets already wire:

1. readiness: `/ready`
2. liveness: `/health`

## 5. CRD / Operator 部署

Gateyes 的 Kubernetes 形态分成 gateway 数据面和可选 operator 控制面。gateway 仍然是纯 AI gateway，不在请求热路径中查询 Kubernetes API；operator 负责监听 CRD，并把声明式资源同步到 Gateyes Admin API、数据库或运行时配置缓存。

Helm chart 已包含 CRD manifests：

```text
deploy/helm/gateyes/crds/gateyes.io_crds.yaml
```

当前声明式资源包括：

| CRD | 作用 |
| --- | --- |
| `GateyesGateway` | 声明 gateway / MaaS / operator / full 部署形态 |
| `ModelEndpoint` | 声明 OpenAI、Anthropic、Azure、vLLM、SGLang、KServe 或 external 上游 |
| `RoutePolicy` | 声明租户、模型、标签、header、亲和和路由策略 |
| `BudgetPolicy` | 声明租户、项目、API key、virtual key、service 的预算和限流 |
| `InferenceAutoscalePolicy` | 声明 observe / recommend / enforce 扩缩容策略 |
| `InferenceService` | 声明自托管 vLLM / SGLang / KServe / external 推理服务 |

### 5.1 安装 CRD

Helm 会在安装 chart 时先安装 `crds/` 目录下的 CRD。也可以单独安装：

```bash
kubectl apply -f deploy/helm/gateyes/crds/gateyes.io_crds.yaml
```

### 5.2 平台模式

`values.yaml` 提供 `platform.mode` 作为部署意图：

```yaml
platform:
  mode: gateway # gateway | maas | operator | full
  crds:
    install: true
  operator:
    enabled: false
```

当前 chart 的 gateway 数据面仍由 `Deployment` 管理；operator deployment 作为独立控制面运行，使用 `client-go` dynamic client 读取 Gateyes CRD，并通过 Admin sync API 同步 provider、router 和 budget。Kubernetes 依赖只存在于 `/app/gateway-operator`，不得引入 `cmd/gateway` 请求热路径。

### 5.3 启用 operator skeleton

当前镜像包含三个二进制：

| Binary | 作用 |
| --- | --- |
| `/app/gateway` | Gateway 数据面和 Admin API |
| `/app/gateway-migrate` | 数据库迁移 |
| `/app/gateway-operator` | CRD/operator 控制面入口 |

启用 operator：

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  --set platform.mode=full \
  --set platform.operator.enabled=true
```

operator 默认以 dry-run 运行，会读取 `ModelEndpoint`、`RoutePolicy`、`BudgetPolicy` 和 `InferenceAutoscalePolicy` 并输出同步计划计数。设置 `platform.operator.dryRun=false` 后，operator 会调用 Gateyes Admin API 应用 provider、router 和 budget 变更。`platform.operator.namespace` 为空时 chart 生成 `ClusterRole/ClusterRoleBinding`，指定 namespace 时生成 `Role/RoleBinding`。

## 6. Production notes

1. Prefer Postgres/MySQL in staging/prod
2. Do not keep provider keys in repo or values files
3. Use external secret manager or pre-created K8s Secret for production
4. CRD/operator 模式下，operator 必须使用独立 ServiceAccount 和最小 RBAC 权限
