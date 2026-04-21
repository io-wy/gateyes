# Deployment

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

1. Gateway: `http://127.0.0.1:8083`
2. Prometheus: `http://127.0.0.1:9090`
3. Grafana: `http://127.0.0.1:3000`

## 2. Kubernetes

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

## 5. Production notes

1. Prefer Postgres/MySQL in staging/prod
2. Do not keep provider keys in repo or values files
3. Use external secret manager or pre-created K8s Secret for production
