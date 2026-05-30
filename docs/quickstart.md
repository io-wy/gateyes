# 快速开始

5 分钟让 Gateyes 跑起来。

---

## Docker Compose 一键启动

```bash
# 1. 克隆
git clone https://github.com/io-wy/gateyes.git && cd gateyes

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，至少填写一个 provider 的 API key

# 3. 启动（网关 + PostgreSQL + Redis + Prometheus + Grafana）
docker compose up --build -d

# 4. 验证
curl http://127.0.0.1:8083/health
```

### 首次使用

```bash
# 1. 创建租户
curl -X POST http://127.0.0.1:8083/admin/tenants \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"slug":"my-team","name":"My Team"}'

# 2. 创建用户（返回 api_key 和 api_secret）
curl -X POST http://127.0.0.1:8083/admin/users \
  -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"name":"alice","role":"tenant_user"}'

# 3. 用返回的凭据请求
# 云上 API
curl -X POST http://127.0.0.1:8083/v1/chat/completions \
  -H "Authorization: Bearer <api_key>:<api_secret>" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}'

# 本地 GPU 模型（假设已通过 vLLM 部署）
curl -X POST http://127.0.0.1:8083/v1/chat/completions \
  -H "Authorization: Bearer <api_key>:<api_secret>" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3-local","messages":[{"role":"user","content":"hello"}]}'
```

---

## 本地开发运行

```bash
# 1. 复制配置
cp configs/config.example.yaml configs/config.yaml

# 2. 编辑 configs/config.yaml，配置 provider

# 3. 运行
go run ./cmd/gateway
```

---

## 暴露的服务端口

| 服务 | 地址 | 说明 |
|------|------|------|
| Gateway | `http://127.0.0.1:8083` | API 入口 |
| Prometheus | `http://127.0.0.1:9090` | 指标采集 |
| Grafana | `http://127.0.0.1:3000` | 监控面板 |
| Redis | `localhost:6379` | 分布式限流 + 缓存 |
