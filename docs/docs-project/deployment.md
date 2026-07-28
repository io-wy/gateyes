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

## 5. Ingress Controller 部署

Gateyes 可作为集群 Ingress Controller 运行，与 Nginx Ingress Controller 并存或替代。

### 5.1 启用 Ingress Controller

```yaml
ingressController:
  enabled: true
  class: gateyes
  watchNamespace: ""
  tlsEnabled: false
  proxy:
    connectTimeout: 5
    readTimeout: 60
    maxIdleConns: 100
  discovery:
    type: kubernetes
```

Helm 会自动创建 `IngressClass` 资源并扩展 RBAC 权限。

### 5.2 使用 Gateyes 接管 Ingress

将现有 Ingress 的 `ingressClassName` 改为 `gateyes`：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
spec:
  ingressClassName: gateyes
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-svc
                port:
                  number: 8080
```

## 6. Production notes

1. Prefer Postgres/MySQL in staging/prod
2. Do not keep provider keys in repo or values files
3. Use external secret manager or pre-created K8s Secret for production
4. Ingress Controller 多副本时建议开启 leader election（controller-runtime 默认未启用）
