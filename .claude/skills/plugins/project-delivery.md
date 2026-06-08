---
name: project-delivery
version: 1.1.0
description: 项目部署、生产、交付全流程规范。CI/CD、Docker、构建、发布、灰度、回滚、监控、告警、运维手册。新项目启动时按本 Skill 建立交付基线，已有项目按本 Skill 补齐缺口。Triggered when setting up deployment, CI, Docker, monitoring, runbook, or production readiness, or when user says "部署/deploy/上线/发布/release/CI/CD/Docker/灰度/canary/回滚/rollback/运维/runbook/生产环境/production/交付/delivery".
---

# Project Delivery — 部署、生产、交付全流程规范

> 核心：代码能跑不等于能上线。从构建到生产，每个环节都要有明确的规范和可回退的方案。

---

## 一、项目结构基线

每个 Go 后端项目必须包含以下交付相关文件（缺什么补什么）：

```
project-root/
├── Dockerfile                # 多阶段构建镜像
├── docker-compose.yml        # 本地开发 + 依赖服务编排
├── docker-compose.prod.yml   # 生产编排（可选）
├── Makefile                  # 构建入口，所有常用命令收敛于此
├── .env.example              # 环境变量模板（禁止提交 .env）
├── .gitignore                # 忽略规则
├── .dockerignore             # Docker 构建忽略
├── .golangci.yml             # lint 规则
├── configs/
│   └── config.example.yaml   # 配置文件模板
├── scripts/
│   ├── build.sh              # 构建脚本
│   ├── migrate.sh            # 数据库迁移脚本
│   └── healthcheck.sh        # 健康检查脚本
├── deploy/
│   ├── docker/
│   │   └── prometheus.yml    # 监控配置
│   └── k8s/                  # Kubernetes 编排（如使用）
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── configmap.yaml
│       ├── secret.yaml
│       └── hpa.yaml
├── docs/
│   ├── deployment.md         # 部署文档
│   ├── ci-cd.md              # CI/CD 流水线说明
│   ├── secrets-and-config.md # 配置与密钥管理
│   ├── monitoring.md         # 监控与告警
│   ├── runbook.md            # 运维手册
│   ├── rollback.md           # 回滚方案
│   ├── backup-and-restore.md # 备份与恢复
│   └── upgrade.md            # 升级指南
└── .github/
    └── workflows/
        ├── ci.yml            # 持续集成
        ├── release.yml       # 发布流水线
        └── live-provider.yml # Live 回归（可选）
```

---

## 二、Makefile 规范

Makefile 是开发者的统一入口。所有构建、测试、部署命令必须收敛到 Makefile。

### 必须包含的 target

```makefile
GO ?= go
CONFIG ?= configs/config.example.yaml
APP_NAME ?= $(shell basename $(PWD))
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)

.PHONY: fmt test test-race vet lint vuln build run \
        docker-build docker-push migrate-up migrate-status \
        clean tidy

# --- 代码质量 ---
fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

# --- 质量门禁（CI 用）---
quality: fmt vet lint test
	@echo "quality gate passed"

# --- 构建 ---
build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o ./bin/$(APP_NAME) ./cmd/...

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o ./bin/$(APP_NAME)-linux-amd64 ./cmd/...

# --- 运行 ---
run:
	$(GO) run ./cmd/... -config $(CONFIG)

# --- Docker ---
docker-build:
	docker build -t $(APP_NAME):$(VERSION) -t $(APP_NAME):latest .

docker-push: docker-build
	docker tag $(APP_NAME):$(VERSION) $(REGISTRY)/$(APP_NAME):$(VERSION)
	docker push $(REGISTRY)/$(APP_NAME):$(VERSION)

# --- 数据库 ---
migrate-up:
	$(GO) run ./cmd/migrate -config $(CONFIG) -action up

migrate-down:
	$(GO) run ./cmd/migrate -config $(CONFIG) -action down

migrate-status:
	$(GO) run ./cmd/migrate -config $(CONFIG) -action status

# --- 清理 ---
clean:
	rm -rf bin/ tmp/ coverage.out coverage.html

tidy:
	$(GO) mod tidy
```

---

## 三、Dockerfile 规范

### 多阶段构建模板

```dockerfile
# ========== Stage 1: Build ==========
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# 先复制依赖文件，利用 Docker 缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码
COPY . .

# 构建参数
ARG VERSION=dev
ARG BUILD_TIME

# 静态链接，无 CGO
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -s -w" \
    -o /bin/app ./cmd/...

# ========== Stage 2: Runtime ==========
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

# 从 builder 阶段复制二进制
COPY --from=builder /bin/app .

# 复制配置模板（生产环境用 configmap/env 覆盖）
COPY configs/config.example.yaml ./configs/config.yaml

# 非 root 运行
USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./app"]
CMD ["-config", "configs/config.yaml"]
```

### 约束

```markdown
### Docker 约束

- WRONG: 把源码直接 COPY 到运行镜像
- CORRECT: 多阶段构建，运行镜像只有二进制 + 必要文件
- Why: 运行镜像不含编译器和源码，减小攻击面和镜像体积

- WRONG: 用 root 运行进程
- CORRECT: 创建非特权用户，USER app
- Why: 容器逃逸时攻击者获得 root 权限的后果远大于普通用户

- WRONG: 不设 HEALTHCHECK
- CORRECT: 设置 HEALTHCHECK，编排系统（K8s/Docker Compose）依赖它判断健康
- Why: 没有健康检查，编排系统无法区分"慢启动"和"真的挂了"

- WRONG: 硬编码配置在 Dockerfile 里
- CORRECT: 配置通过环境变量或 configmap 注入，Dockerfile 只放模板
- Why: 镜像应该是环境无关的，同一个镜像能跑 dev/staging/prod

- WRONG: 不加 -s -w 编译参数
- CORRECT: go build -ldflags "-s -w" 去掉 debug 信息
- Why: 生产镜像不需要 debug 符号表，可减小 ~30% 体积
```

---

## 四、docker-compose 规范

### 开发环境

```yaml
# docker-compose.yml — 本地开发用
version: "3.9"

services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "${APP_PORT:-8080}:8080"
    env_file:
      - .env
    volumes:
      - ./configs:/app/configs:ro
      - ./data:/app/data          # SQLite 数据持久化
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped

  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: ${DB_NAME:-app}
      POSTGRES_USER: ${DB_USER:-app}
      POSTGRES_PASSWORD: ${DB_PASSWORD:-devpassword}
    ports:
      - "${DB_PORT:-5432}:5432"
    volumes:
      - db_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER:-app}"]
      interval: 5s
      timeout: 3s
      retries: 5

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "${PROMETHEUS_PORT:-9090}:9090"
    volumes:
      - ./deploy/docker/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    ports:
      - "${GRAFANA_PORT:-3000}:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD:-admin}
    volumes:
      - grafana_data:/var/lib/grafana
    restart: unless-stopped

volumes:
  db_data:
  grafana_data:
```

### 约束

- `.env` 文件在 `.gitignore` 中，`.env.example` 提交到仓库
- 所有端口通过环境变量配置，有默认值
- 依赖服务必须有 `healthcheck`
- 生产环境不使用 docker-compose，使用 Kubernetes 或其他编排系统

---

## 五、CI/CD 流水线规范

### CI 流水线（.github/workflows/ci.yml）

```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  quality:
    name: Quality Gate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      - name: Format
        run: gofmt -d . | tee /dev/stderr | [ $(wc -l) -eq 0 ]

      - name: Vet
        run: go vet ./...

      - name: Lint
        uses: golangci/golangci-lint-action@v6

      - name: Test
        run: go test -race -coverprofile=coverage.out ./...

      - name: Vulnerability Check
        run: go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...

      - name: Build
        run: CGO_ENABLED=0 go build -ldflags "-s -w" ./cmd/...
```

### Release 流水线（.github/workflows/release.yml）

```yaml
name: Release

on:
  push:
    tags: ["v*"]

jobs:
  release:
    name: Build & Push
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      - name: Determine Version
        id: version
        run: echo "version=${GITHUB_REF#refs/tags/}" >> $GITHUB_OUTPUT

      - name: Run Tests
        run: go test -race ./...

      - name: Build Binary
        run: |
          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
            -ldflags "-X main.Version=${{ steps.version.outputs.version }} -s -w" \
            -o ./bin/app-linux-amd64 ./cmd/...

      - name: Build & Push Docker Image
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: |
            ${{ secrets.REGISTRY }}/app:${{ steps.version.outputs.version }}
            ${{ secrets.REGISTRY }}/app:latest
          build-args: |
            VERSION=${{ steps.version.outputs.version }}

      - name: Generate Changelog
        run: |
          git log $(git describe --tags --abbrev=0 HEAD^)..HEAD --oneline > CHANGELOG.md

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: ./bin/app-linux-amd64
          body_path: CHANGELOG.md
```

### 流水线约束

```markdown
### CI/CD 约束

- WRONG: CI 只跑 go build
- CORRECT: fmt + vet + lint + test + vuln 全部通过才算绿色
- Why: 只编译通过不能保证代码质量

- WRONG: PR 合入后才跑 CI
- CORRECT: PR 必须等 CI 绿色后才允许合并
- Why: 合入后再发现问题，修复合入成本远高于修 PR

- WRONG: release 时不跑测试
- CORRECT: release 流水线也要跑完整测试
- Why: 防止 tag 打在未验证的 commit 上

- WRONG: 硬编码镜像仓库地址
- CORRECT: 用 secrets.REGISTRY 或环境变量
- Why: 不同环境（dev/staging/prod）用不同仓库

- WRONG: 不打版本标签
- CORRECT: 用 git tag 触发 release，版本号嵌入二进制
- Why: 生产环境必须能查到运行的是哪个版本
```

---

## 六、配置与密钥管理

### 分层原则

| 层 | 内容 | 管理方式 | 示例 |
|----|------|----------|------|
| 代码内嵌默认值 | 无敏感信息的默认配置 | 写在代码里 | 默认超时 60s |
| 配置文件 | 环境相关但不敏感的配置 | YAML / JSON | listenAddr: ":8080" |
| 环境变量 | 可能有敏感信息的配置 | `.env` / K8s ConfigMap | DB_HOST, REDIS_URL |
| 密钥管理 | 绝对不能泄露的密钥 | K8s Secret / Vault / AWS SM | DB_PASSWORD, API_KEY |

### 约束

```markdown
### 配置约束

- WRONG: 密钥写在 config.yaml 里提交到 Git
- CORRECT: config.yaml 里写 ${ENV_VAR} 占位，运行时从环境变量注入
- Why: 密钥泄露 = 全线事故，Git 历史不可擦除

- WRONG: 所有配置都硬编码
- CORRECT: 可变的配置项通过配置文件/环境变量管理
- Why: 不同环境需要不同配置，硬编码无法区分

- WRONG: 配置改了不通知
- CORRECT: 敏感配置变更写审计日志
- Why: 配置变更常是故障的根源，需要可追溯
```

---

## 七、健康检查与就绪探针

### 必须实现的端点

| 端点 | 用途 | 响应 |
|------|------|------|
| `GET /health` | 存活探针（Liveness） | `{"status": "ok"}` 200 |
| `GET /ready` | 就绪探针（Readiness） | 检查依赖（DB/缓存）是否可用，可用返回 200 |
| `GET /metrics` | Prometheus 指标 | 标准 Prometheus 格式 |

### 实现示例

```go
// /health — 只检查进程是否存活
func (h *Handler) Health(c *gin.Context) {
    c.JSON(200, gin.H{"status": "ok"})
}

// /ready — 检查是否准备好接收流量
func (h *Handler) Ready(c *gin.Context) {
    ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
    defer cancel()

    if err := h.db.PingContext(ctx); err != nil {
        c.JSON(503, gin.H{"status": "not ready", "reason": "database unreachable"})
        return
    }
    c.JSON(200, gin.H{"status": "ready"})
}
```

### 约束

- WRONG: 只实现 /health 不实现 /ready
- CORRECT: 两个都实现，/health 检查进程，/ready 检查依赖
- Why: 进程活着但 DB 连不上时不应该接收流量

---

## 八、监控与告警

### 必须暴露的指标

```go
// 请求维度
requests_total          // Counter{service, method, path, status}
request_duration_seconds // Histogram{service, method, path}

// 依赖维度
db_query_duration_seconds // Histogram{query_type}
db_connections_active     // Gauge
db_connections_idle       // Gauge

// 系统维度
go_goroutines             // Gauge（标准库自带）
process_resident_memory_bytes // Gauge（标准库自带）
```

### 告警规则模板

```yaml
# deploy/docker/prometheus-alerts.yml
groups:
  - name: app
    rules:
      - alert: HighErrorRate
        expr: rate(requests_total{status=~"5.."}[5m]) / rate(requests_total[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Error rate > 5%"

      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(request_duration_seconds_bucket[5m])) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "P95 latency > 2s"

      - alert: ServiceDown
        expr: up{job="app"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Service is down"
```

### 约束

- WRONG: 上线后不设告警
- CORRECT: 上线前配置至少 3 条告警（错误率、延迟、存活）
- Why: 没有告警 = 盲飞，用户投诉才知道出问题

---

## 九、日志规范

### 结构化日志

```go
// WRONG: 非结构化日志
log.Printf("user %s did something", userID)

// CORRECT: 结构化日志，带 requestID
slog.Info("request completed",
    "requestID", reqID,
    "method", "POST",
    "path", "/v1/resource",
    "status", 200,
    "duration", time.Since(start),
    "userID", userID,
)
```

### 约束

- 禁止在日志中输出密钥、token、密码
- 热路径禁止 Debug 级别日志
- 每个请求必须有唯一 requestID
- 错误日志必须包含足够上下文用于定位（不要只打 error message）

---

## 十、数据库 Migration

### 规范

- migration 文件按时间戳排序：`001_create_users.sql`, `002_add_email_to_users.sql`
- 只允许 additive 变更（新增列、新增表），不允许破坏性变更（删列、改类型）
- 破坏性变更必须分多步：先加新列 → 迁移数据 → 再删旧列
- 启动时自动执行 migration（`autoMigrate: true` 可选配置）
- 生产环境的 destructive migration 必须人工确认

### 约束

- WRONG: 直接 ALTER TABLE 删列
- CORRECT: 先加新列 → 迁移数据 → 下个版本再删旧列
- Why: 生产数据库的 destructive 变更不可逆，必须分步

---

## 十一、发布策略

### 版本号规范（Semantic Versioning）

```
vMAJOR.MINOR.PATCH

MAJOR: 不兼容的 API 变更
MINOR: 向后兼容的新功能
PATCH: 向后兼容的 bug 修复
```

### 发布 Checklist

```markdown
## 发布前

- [ ] 所有测试通过（CI 绿色）
- [ ] CHANGELOG.md 已更新
- [ ] 版本号已更新
- [ ] 配置文件模板已同步
- [ ] 数据库 migration 已确认（无破坏性变更，或已有分步方案）
- [ ] 告警规则已更新（如有新指标）

## 发布中

- [ ] 镜像构建并推送
- [ ] 数据库 migration 执行
- [ ] 新版本部署
- [ ] 健康检查通过
- [ ] 烟雾测试通过（核心 API 手动验证）

## 发布后

- [ ] 监控指标正常（错误率、延迟、QPS）
- [ ] 日志无异常错误
- [ ] 回滚方案已确认并可用
```

### 灰度发布

如果使用 Kubernetes：

```yaml
# 金丝雀发布——先部署 1 个 Pod，观察后再全量
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-canary
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
      track: canary
  template:
    metadata:
      labels:
        app: app
        track: canary
    spec:
      containers:
        - name: app
          image: registry/app:v1.2.0
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
```

---

## 十二、回滚方案

### 快速回滚

```bash
# 1. 回滚到上一个版本
kubectl rollout undo deployment/app

# 2. 或指定版本
kubectl set image deployment/app app=registry/app:v1.1.0

# 3. 数据库回滚（如果 migration 已执行）
make migrate-down  # 危险操作，必须人工确认
```

### 回滚约束

- **镜像回滚**：秒级，无数据风险 → 可以快速执行
- **数据库回滚**：有数据风险 → 必须人工确认，最好只回滚代码不回滚数据
- **配置回滚**：重新部署旧版配置 → 检查 ConfigMap 版本

### 约束

- WRONG: 出问题后慌乱中手动改配置
- CORRECT: 按回滚文档执行，每步确认
- Why: 慌乱中的手动操作往往让事情更糟

---

## 十三、运维手册（Runbook）

每个项目必须有 `docs/runbook.md`，内容包含：

```markdown
# Runbook

## 常见故障与处置

### 服务无响应
1. 检查健康端点：curl http://localhost:8080/health
2. 检查进程：ps aux | grep app
3. 检查端口：ss -tlnp | grep 8080
4. 检查日志：journalctl -u app --since "10 minutes ago"
5. 检查资源：top / df -h / free -m
6. 如果无法恢复：回滚到上一版本

### 数据库连接失败
1. 检查数据库端点：nc -zv $DB_HOST $DB_PORT
2. 检查连接数：show processlist;
3. 检查网络：ping $DB_HOST
4. 如果是连接数耗尽：kill 长事务或重启连接池

### 错误率飙升
1. 查看错误分布：Prometheus → requests_total{status=~"5.."}
2. 查看最近部署：kubectl rollout history deployment/app
3. 如果是部署后出现：考虑回滚
4. 如果是外部服务引起：检查上游服务状态

### 内存持续增长
1. 查看内存趋势：process_resident_memory_bytes
2. 导出 pprof：curl http://localhost:8080/debug/pprof/heap > heap.prof
3. 分析：go tool pprof heap.prof
4. 常见原因：goroutine 泄漏、大对象未释放、缓存只增不减
```

---

## 十四、使用本 Skill

### 新项目启动

1. 按「一、项目结构基线」创建文件
2. 按「二、Makefile 规范」配置 Makefile
3. 按「三、Dockerfile 规范」创建 Dockerfile
4. 按「四、docker-compose 规范」创建开发环境
5. 按「五、CI/CD 规范」配置流水线
6. 按「七、健康检查」实现 /health 和 /ready
7. 按「八、监控与告警」配置 Prometheus
8. 按「十三、运维手册」写 runbook

### 已有项目补齐

1. 对比「一、项目结构基线」，缺什么补什么
2. 补齐 Makefile 中缺失的 target
3. 补齐 Dockerfile 中的安全措施（非 root 用户、HEALTHCHECK）
4. 补齐 CI 流水线中的质量门禁
5. 补齐监控告警
6. 补齐 runbook

### 触发条件

- io-wy 说「搭建部署」/「配置 CI」/「写 Dockerfile」/「配监控」/「写 runbook」
- 新项目初始化时
- 部署相关任务
