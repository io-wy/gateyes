# Gateyes

生产级 LLM API Gateway，统一接入 OpenAI / Anthropic / vLLM / SGLang，提供多租户隔离、智能路由、限流熔断、预算治理和完整可观测性。

```
Client (OpenAI/Anthropic SDK)
    |
    v
+----------------------------------+
|  Gateyes Gateway                 |
|  - Auth / RBAC / 多租户          |
|  - 限流 / 熔断 / 缓存            |
|  - 智能路由 / 预算治理            |
|  - 插件系统 (WASM + gRPC)        |
|  - Metrics / Trace / Audit       |
+----------------------------------+
    |                  |         |
    v                  v         v
OpenAI API      Anthropic    vLLM/SGLang
```

---

## 内置功能

### API 接入（4 路统一）

| 接口 | 协议 | 说明 |
|------|------|------|
| `/v1/responses` | OpenAI Responses API | 内部主链路 |
| `/v1/chat/completions` | OpenAI Chat Completions | 存量零改动接入 |
| `/v1/messages` | Anthropic Messages API | Anthropic 生态直连 |
| `/v1/embeddings` | OpenAI Embeddings API | 文本向量化 |

四路共享同一套业务编排：provider 选择、重试、熔断、流式处理、usage 记录。

### 多租户与权限

- **鉴权链路**：`api_key:api_secret -> user -> tenant -> role`
- **四级角色**：`super_admin` / `tenant_admin` / `tenant_user`
- **Provider 可见性**：按 tenant 绑定，未绑定即不可见
- **模型白名单**：user + api_key + virtual_key 三级 AND 控制

### 路由（6 种策略）

| 策略 | 说明 |
|------|------|
| `round_robin` | 简单轮询 |
| `least_load` | 实时并发最小负载 |
| `cost_based` | 按价格优先低成本 |
| `sticky` | 同 session 命中同一 provider |
| `ruleEngine` | 按 prompt 长度、工具调用等分流 |
| `prefix_affinity` | 同前缀命中同一 provider（提升 prefix-cache 命中率） |

### 限流（Redis Lua Token Bucket）

多维度独立判定：全局 QPS/TPM、租户 TPM/RPM、Provider TPM/RPM、模型 QPS。无 Redis 时自动降级为内存模式。

### 预算治理

四级预算（virtual_key -> api_key -> project -> tenant），三种策略：
- `hard_reject`：耗尽即拒
- `soft_alert`：告警但不阻断
- `grace`：宽限期，超支后续记账

### 熔断

三态模型（healthy / degraded / unhealthy），定时探活 + 手动触发，状态变更持久化并告警。

### 缓存

L1 精确匹配缓存（Redis + 内存 LRU），命中时直接复用响应，不调用上游。

### 可观测性

- **14 个 Prometheus 指标**：请求/延迟/上游延迟/TTFT/流式时长/token/错误/重试/熔断
- **OTLP 链路追踪**：W3C traceparent 传播
- **审计日志**：admin 关键写操作全记录

### 插件系统

支持 **WASM (TinyGo)** 和 **gRPC** 两种插件，覆盖 5 个生命周期阶段：

| Phase | 触发时机 | 典型用途 |
|-------|---------|---------|
| `pre_route` | 路由前 | 改写路由目标 |
| `post_route` | 路由后 | 审计路由决策 |
| `pre_upstream` | 发请求到 provider 前 | 拦截、改写请求、缓存命中 |
| `post_upstream` | 收到 provider 响应后 | 改写响应、内容审核 |
| `audit` | 响应已写入客户端后 | 异步日志、计费、监控 |

---

## 快速开始

### Docker Compose（推荐）

```bash
git clone https://github.com/io-wy/gateyes.git && cd gateyes
cp .env.example .env
# 编辑 .env，填写 OPENAI_API_KEY
docker compose up --build -d

curl http://127.0.0.1:8083/health
```

暴露服务：
- Gateway: `http://127.0.0.1:8083`
- Prometheus: `http://127.0.0.1:9090`
- Grafana: `http://127.0.0.1:3000`

### 本地开发

```bash
cp configs/config.example.yaml configs/config.yaml
# 编辑 configs/config.yaml，配置 provider
go run ./cmd/gateway
```

### 零成本体验（内置 Mock）

```bash
go run ./benchmark/cmd/mockupstream/main.go -port 19999
./bin/gateway -config configs/demo-mock.yaml

curl -X POST http://localhost:8083/v1/responses \
  -H "Authorization: Bearer demo-key-001:demo-secret-001" \
  -H "Content-Type: application/json" \
  -d '{"model": "mock-model", "input": "hello"}'
```

---

## 插件开发

Gateyes 支持 **WASM (TinyGo)** 和 **gRPC** 两种插件。选择建议：

| 维度 | WASM | gRPC |
|------|------|------|
| 延迟 | < 1ms | ~5-50ms |
| 沙箱 | 完全隔离 | 独立进程 |
| 外部依赖 | 不能访问 | 可以访问网络/数据库 |
| 适用场景 | 轻量过滤、关键词拦截 | 复杂路由、模型调用 |

### WASM 插件（TinyGo）

```go
package main

import "github.com/gateyes/gateway/plugins/sdk/gateyes"

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
    ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)

    switch ev.Phase {
    case "pre_upstream":
        // 检查请求，拦截敏感词
        if shouldBlock(ev.Payload) {
            return gateyes.WriteGatewayCommand(outputPtr,
                gateyes.BlockGateway("blocked by policy"))
        }
    }

    return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
}

func main() {}
```

构建：
```bash
tinygo build -o my_plugin.wasm -target=wasi -no-debug -opt=z .
```

配置：
```yaml
wasmPlugins:
  - name: my-filter
    path: ./my_plugin.wasm
    phases: [pre_upstream, post_upstream]
    timeoutMs: 50
    memoryPages: 1
```

### gRPC 插件（Go）

```go
func (s *server) Process(stream pluginv1.GatewayPlugin_ProcessServer) error {
    for {
        ev, err := stream.Recv()
        if err == io.EOF { return nil }

        switch ev.GetPhase() {
        case pluginv1.Phase_PHASE_PRE_UPSTREAM:
            stream.Send(&pluginv1.Command{
                Action: pluginv1.Action_ACTION_ALLOW,
            })
        }
    }
}
```

配置：
```yaml
grpcPlugins:
  - name: my-auditor
    type: gateway
    address: localhost:50052
    timeout: 100
    phases:
      - pre_upstream
      - post_upstream
      - audit
```

完整开发指南（ABI 规范、TRANSFORM 用法、调试技巧、常见问题）见 [`docs/plugin-development.md`](./docs/plugin-development.md)。

---

## 部署与维护

### Kubernetes（Helm）

```bash
helm upgrade --install gateyes ./deploy/helm/gateyes \
  -n gateyes --create-namespace \
  -f ./deploy/helm/gateyes/values.yaml \
  -f ./deploy/helm/gateyes/values-prod.yaml
```

Helm chart 包含：
- Gateway Deployment（无状态，水平扩缩）
- PostgreSQL / Redis 依赖（可外置）
- Prometheus + Grafana
- Migration Job（pre-install/pre-upgrade）
- Health probes（`/ready`, `/health`）

### 配置热重载

```bash
curl -X POST http://localhost:8083/admin/reload \
  -H "Authorization: Bearer admin-key-001:admin-secret-001"
```

### 生产检查清单

1. **数据库**：生产用 PostgreSQL，开发用 SQLite
2. **Redis**：推荐部署，用于分布式限流；不部署时自动降级内存模式
3. **密钥管理**：Provider API key 不放仓库，用 K8s Secret 或外部密钥管理器
4. **监控**：Prometheus + Grafana 基线 dashboard 开箱即用
5. **追踪**：OTLP exporter 配置到 Jaeger / Tempo

详细部署指南见 [`docs/deployment.md`](./docs/deployment.md)。

### 运维常用操作

```bash
# 查看 provider 状态
curl -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  http://localhost:8083/admin/providers

# 查看指标
curl http://localhost:8083/metrics

# 查看 trace
curl -H "Authorization: Bearer admin-key-001:admin-secret-001" \
  http://localhost:8083/admin/traces

# 全量测试回归
go test ./...
```

---

## 文档导航

| 文档 | 内容 |
|------|------|
| [`docs/plugin-development.md`](./docs/plugin-development.md) | WASM / gRPC 插件完整开发指南 |
| [`docs/deployment.md`](./docs/deployment.md) | Docker Compose / K8s Helm 部署 |
| [`docs/architecture.md`](./docs/architecture.md) | 架构总览、职责边界 |
| [`docs/runtime-mechanisms.md`](./docs/runtime-mechanisms.md) | 鉴权、限流、路由、预算、缓存实现细节 |
| [`docs/provider-configuration.md`](./docs/provider-configuration.md) | 本地 GPU / 云上 API 接入配置 |
| [`docs/routing.md`](./docs/routing.md) | 路由策略详解 |
| [`docs/cache.md`](./docs/cache.md) | L1 缓存机制 |
| [`docs/monitoring.md`](./docs/monitoring.md) | 监控指标与告警 |

---

## License

MIT
