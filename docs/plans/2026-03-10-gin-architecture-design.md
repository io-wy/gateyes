# Gateyes Gin Architecture Refactor

## Goal

将当前 `main -> router -> proxy` 的耦合启动链收敛成更常见的 Gin 分层结构，并把 provider 做成可扩展抽象层，为后续接入更多模型供应商预留稳定扩展点。

## Target Structure

```text
cmd/
  gateyes/

internal/
  bootstrap/        # 组装应用依赖和启动链
  handler/          # Gin HTTP 入口
  router/           # 只负责路由注册
  middleware/       # 中间件
  service/
    gateway/        # 网关服务入口
    proxy/          # 兼容层与现有核心逻辑
  provider/
    factory/        # provider factory / registry 装配
    openai/         # OpenAI-compatible adapter
    upstream/       # 通用 upstream transport
```

## Decisions

### 1. Router 只做路由绑定

- `router.New` 不再创建 middleware、provider、proxy service。
- 中间件链和代理路由在 `bootstrap` 中组装后注入。

### 2. Bootstrap 负责应用装配

- `cmd/gateyes/main.go` 只做配置加载、信号处理和 server 生命周期。
- `internal/bootstrap/app.go` 统一创建 metrics、middleware、gateway service、router、server。

### 3. Provider 使用 registry + adapter

- 新增 `internal/provider/provider.go` 定义统一 `ModelProvider` 接口。
- 新增 `internal/provider/factory` 根据 `providers.<name>.type` 创建具体 adapter。
- 当前默认 `type` 为 `openai`，对应 OpenAI-compatible 上游。

### 4. 兼容迁移优先

- 保留 `internal/service/proxy` 现有核心逻辑，避免一次性大搬迁带来测试回归。
- 通过 `internal/service/gateway` 暴露更清晰的服务入口，后续可以逐步把 `proxy` 实现继续下沉。

## Trade-offs

- 这次没有一次性把全部 proxy 内核完全迁移到 `service/gateway`，而是先完成启动层、provider 抽象和路由职责切分。
- 这样风险更低，现有测试可直接兜底，后续新增供应商时已经不需要再修改启动链和路由骨架。

## Verification

- `go test ./...`
