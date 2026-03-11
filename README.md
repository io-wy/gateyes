# Gateyes

Gateyes 是一个面向 OpenAI-compatible 接口的轻量 API Gateway，重点解决：

- 多 provider 统一入口
- virtual key 与 provider 子池绑定
- retry / fallback / circuit breaker
- rate limit / quota / cache / metrics 按需开启

## 快速开始

### 1. 准备配置

- 示例模板：`config/gateyes.example.json`
- 本地运行：`config/gateyes.json`
- 未显式配置的字段会使用 `internal/config/config.go` 里的默认值

### 2. 启动

```bash
go run ./cmd/gateyes -config config/gateyes.json
```

### 3. 健康检查

```bash
curl http://localhost:8080/healthz
```

### 4. 发送请求

当配置了 `auth.virtual_keys` 时：

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer vk-local" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role":"user","content":"hello"}]
  }'
```

如果你不需要认证，删除配置里的 `auth` 节点即可。

## 最小配置原则

- 必填：至少一个 `providers.<name>.base_url`
- 默认 provider 类型：`providers.<name>.type = "openai"`
- 常用：`providers.<name>.api_key`
- 建议保留：`gateway.default_provider`
- 需要认证时再加：`auth.virtual_keys`
- 需要限流、配额、缓存时再加对应节点

## 当前目录分层

- `cmd/gateyes`：进程入口
- `internal/bootstrap`：应用装配
- `internal/router`：路由注册
- `internal/handler`：HTTP 入口处理
- `internal/service/gateway`：网关服务入口
- `internal/provider`：provider 抽象、factory 和 upstream adapter

## 默认行为

- 默认监听：`:8080`
- 默认 OpenAI 路径前缀：`/v1`
- 默认 metrics 路径：`/metrics`
- `rate_limit` / `quota` / `cache` 默认关闭
- 配置了 `auth.virtual_keys` 时，即使 `auth.enabled=false`，仍会要求有效 virtual key

## 常用端点

- `GET /healthz`
- `GET /metrics`
- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/responses`
- `GET /v1/models`
- `GET /v1/models/{id}`

## 开发

```bash
go test ./...
```
