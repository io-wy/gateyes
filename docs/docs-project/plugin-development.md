# Gateyes 插件开发指南

## 两种插件怎么选

| 维度 | WASM (TinyGo) | gRPC (Go/Python/...) |
|------|--------------|---------------------|
| 延迟 | < 1ms | ~5-50ms (本机) |
| 沙箱 | 完全隔离，无网络 | 独立进程，可访问网络 |
| 状态 | 无状态（每次调用新实例） | 可有状态（连接保持） |
| 外部依赖 | 不能访问 | 可以访问数据库/API |
| 部署 | 单个 `.wasm` 文件 | 独立服务 |
| 适用场景 | 轻量过滤、关键词拦截、PII 检测 | 复杂路由、模型调用、外部审核 |

**推荐**：先写 WASM，遇到需要网络访问时再切 gRPC。

---

## 接口规范（这是合约，必须遵守）

### Phase（生命周期阶段）

| Phase | 触发时机 | payload 内容 | 可用 Action |
|-------|---------|-------------|------------|
| `pre_route` | 路由前（guardrail 之后，`planCandidates` 之前） | `{"request": {...}}` | `BLOCK`, `TRANSFORM` |
| `post_route` | 路由后（`planCandidates` 之后） | `{"request": {...}, "candidates": ["p1", "p2"]}` | `BLOCK`, `TRANSFORM` |
| `pre_upstream` | 发请求到 provider 之前 | `{"request": {...}}` | `BLOCK`, `TRANSFORM`, `CACHE_HIT` |
| `post_upstream` | 收到 provider 响应之后 | `{"response": {...}}` | `BLOCK`, `TRANSFORM` |
| `audit` | 响应已写入客户端后（异步） | `{"request": "...", "response": {...}, "usage": {...}, "provider": "...", "latency": 123}` | 只读，建议 `ALLOW` |

**注意**：
- `pre_route` / `post_route` **只在流式路径**（`StreamResponse`）中调用。非流式路径（`CreateResponse`）目前没有这两个 phase。
- `audit` 是异步的，gateway 不等待 plugin 返回。

### Action（命令类型）

| Action | 行为 | 需要 payload |
|--------|------|-------------|
| `ALLOW` | 继续处理 | 否 |
| `BLOCK` | 终止请求，返回错误 | 否（`reason` 必填） |
| `TRANSFORM` | 用 payload 替换当前请求/响应 | 是（JSON 格式） |
| `CACHE_HIT` | 直接返回 payload 作为响应（仅 `pre_upstream`） | 是（完整响应 JSON） |
| `SKIP` | 跳过本阶段剩余插件 | 否 |

**Fail-Open**：插件崩溃、超时、返回坏 JSON 都视为 `ALLOW`，不会阻塞网关。

---

## WASM 插件开发（TinyGo）

### 1. 环境准备

```bash
# macOS
brew install tinygo

# 验证
tinygo version  # 需要 >= 0.31
```

### 2. 项目结构

```
my_wasm_plugin/
├── go.mod
└── main.go
```

### 3. go.mod

```go
module my_wasm_plugin

go 1.23

require github.com/gateyes/gateway v0.0.0

replace github.com/gateyes/gateway => /path/to/gateyes
```

### 4. 完整示例：关键词拦截插件

```go
package main

import (
	"strings"
	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

var blockedKeywords = []string{
	"credit_card", "ssn", "password",
}

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)

	switch ev.Phase {
	case "pre_upstream":
		// 从 payload 中提取请求文本
		body := extractText(ev.Payload)
		lower := strings.ToLower(body)

		for _, kw := range blockedKeywords {
			if strings.Contains(lower, kw) {
				return gateyes.WriteGatewayCommand(outputPtr, gateyes.BlockGateway(
					"request contains prohibited keyword: " + kw,
				))
			}
		}

	case "post_upstream":
		// 检查响应中的敏感词
		body := extractResponseText(ev.Payload)
		lower := strings.ToLower(body)

		for _, kw := range blockedKeywords {
			if strings.Contains(lower, kw) {
				return gateyes.WriteGatewayCommand(outputPtr, gateyes.BlockGateway(
					"response contains prohibited keyword: " + kw,
				))
			}
		}
	}

	return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
}

func extractText(payload map[string]any) string {
	// payload 结构: {"request": {"input": "...", "messages": [...]}}
	if req, ok := payload["request"].(map[string]any); ok {
		if input, ok := req["input"].(string); ok && input != "" {
			return input
		}
		// 尝试从 messages 提取
		if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
			if last, ok := msgs[len(msgs)-1].(map[string]any); ok {
				if content, ok := last["content"].(string); ok {
					return content
				}
			}
		}
	}
	return ""
}

func extractResponseText(payload map[string]any) string {
	// payload 结构: {"response": {"output": [{"content": [{"text": "..."}]}]}}
	if resp, ok := payload["response"].(map[string]any); ok {
		if outputs, ok := resp["output"].([]any); ok && len(outputs) > 0 {
			if out, ok := outputs[0].(map[string]any); ok {
				if contents, ok := out["content"].([]any); ok && len(contents) > 0 {
					if c, ok := contents[0].(map[string]any); ok {
						if text, ok := c["text"].(string); ok {
							return text
						}
					}
				}
			}
		}
	}
	return ""
}

func main() {}
```

### 5. SDK 工具函数

`github.com/gateyes/gateway/plugins/sdk/gateyes` 提供：

```go
// 读取 gateway 传入的事件
ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)
// ev.Phase: string ("pre_upstream" 等)
// ev.Context: {TraceID, TenantID, UserID, Model, Stream}
// ev.Payload: map[string]any

// 写回命令
return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
return gateyes.WriteGatewayCommand(outputPtr, gateyes.BlockGateway("reason"))

// TRANSFORM 请求（pre_upstream）
transformedReq := gateyes.TransformRequestGateway(modifiedRequest)
return gateyes.WriteGatewayCommand(outputPtr, transformedReq)

// TRANSFORM 响应（post_upstream）
transformedResp := gateyes.TransformResponseGateway(modifiedResponse)
return gateyes.WriteGatewayCommand(outputPtr, transformedResp)
```

### 6. 构建

```bash
cd my_wasm_plugin
tinygo build -o my_plugin.wasm -target=wasi -no-debug -opt=z .
```

### 7. 配置到 gateway

```yaml
plugins:
  enabled: true
  directory: "./plugins"
  autoReload: true

wasmPlugins:
  - name: keyword-filter
    path: ./plugins/my_plugin.wasm
    phases:
      - pre_upstream
      - post_upstream
    timeoutMs: 50
    memoryPages: 1
```

---


## gRPC 协议文件位置

插件协议源码统一放在 `proto/plugin/v1/`：

- `plugin.proto`：通用插件健康检查与能力声明
- `router.proto`：RouterPlugin 排序协议
- `gateway.proto`：GatewayPlugin 生命周期事件/命令协议

Go 生成代码统一放在 `pkg/plugin/v1/`，作为跨进程插件合约包被 gateway 和外部插件服务引用。修改 proto 后运行 `make proto`，不要手写 `pkg/plugin/v1/*.pb.go`。

---

## gRPC 插件开发（Go）

### 1. 项目结构

```
my_grpc_plugin/
├── go.mod
└── main.go
```

### 2. go.mod

```go
module my_grpc_plugin

go 1.23

require (
	github.com/gateyes/gateway v0.0.0
	google.golang.org/grpc v1.81.0
)

replace github.com/gateyes/gateway => /path/to/gateyes
```

### 3. 完整示例：内容审核插件

```go
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

type gatewayPluginServer struct {
	pluginv1.UnimplementedGatewayPluginServer
}

func (s *gatewayPluginServer) Process(stream pluginv1.GatewayPlugin_ProcessServer) error {
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		phase := ev.GetPhase()
		ctx := ev.GetContext()
		log.Printf("[%s] trace=%s model=%s", phase.String(), ctx.GetTraceId(), ctx.GetModel())

		var cmd *pluginv1.Command

		switch phase {
		case pluginv1.Phase_PHASE_PRE_UPSTREAM:
			// 解析请求 payload
			var payload struct {
				Request map[string]any `json:"request"`
			}
			_ = json.Unmarshal(ev.GetPayload(), &payload)

			if shouldBlockRequest(payload.Request) {
				cmd = &pluginv1.Command{
					Action: pluginv1.Action_ACTION_BLOCK,
					Reason: "request blocked by content policy",
				}
			}

		case pluginv1.Phase_PHASE_POST_UPSTREAM:
			var payload struct {
				Response map[string]any `json:"response"`
			}
			_ = json.Unmarshal(ev.GetPayload(), &payload)

			if shouldBlockResponse(payload.Response) {
				cmd = &pluginv1.Command{
					Action: pluginv1.Action_ACTION_BLOCK,
					Reason: "response blocked by content policy",
				}
			}

		case pluginv1.Phase_PHASE_AUDIT:
			// 异步审计，只记录不拦截
			var payload map[string]any
			_ = json.Unmarshal(ev.GetPayload(), &payload)
			log.Printf("[AUDIT] latency=%v provider=%v",
				payload["latency"], payload["provider"])
		}

		if cmd != nil {
			if err := stream.Send(cmd); err != nil {
				return err
			}
		} else {
			if err := stream.Send(&pluginv1.Command{
				Action: pluginv1.Action_ACTION_ALLOW,
			}); err != nil {
				return err
			}
		}
	}
}

func shouldBlockRequest(req map[string]any) bool {
	if req == nil {
		return false
	}
	s := jsonString(req)
	// 简单示例：检查是否包含敏感词
	return containsAny(s, []string{"BLOCK_PRE", "FORBIDDEN"})
}

func shouldBlockResponse(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	s := jsonString(resp)
	return containsAny(s, []string{"BLOCK_POST", "HARMFUL"})
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if contains(s, p) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "50052"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pluginv1.RegisterGatewayPluginServer(grpcServer, &gatewayPluginServer{})

	hs := health.NewServer()
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, hs)
	reflection.Register(grpcServer)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		grpcServer.GracefulStop()
	}()

	log.Printf("gateway plugin listening on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
```

### 4. 关键注意事项

**Phase 和 Action 使用枚举值**：

```go
// Phase 枚举
pluginv1.Phase_PHASE_PRE_ROUTE
pluginv1.Phase_PHASE_POST_ROUTE
pluginv1.Phase_PHASE_PRE_UPSTREAM
pluginv1.Phase_PHASE_POST_UPSTREAM
pluginv1.Phase_PHASE_AUDIT

// Action 枚举
pluginv1.Action_ACTION_ALLOW
pluginv1.Action_ACTION_BLOCK
pluginv1.Action_ACTION_TRANSFORM
pluginv1.Action_ACTION_CACHE_HIT
pluginv1.Action_ACTION_SKIP
```

**不要用字符串字面量**，gateway 内部会自动转换（如 `"pre_route"` ↔ `"PHASE_PRE_ROUTE"`）。

### 5. 构建和运行

```bash
cd my_grpc_plugin
go mod tidy
go build -o my_plugin .
./my_plugin
# 2024/xx/xx gateway plugin listening on :50052
```

### 6. 配置到 gateway

```yaml
grpcPlugins:
  - name: content-auditor
    type: gateway
    address: localhost:50052
    timeout: 100
    phases:
      - pre_upstream
      - post_upstream
      - audit
```

---

## TRANSFORM 用法（修改请求/响应）

### WASM

```go
// 修改请求模型
case "pre_upstream":
    ev.Payload["request"]["model"] = "gpt-4o"
    return gateyes.WriteGatewayCommand(outputPtr,
        gateyes.TransformRequestGateway(ev.Payload["request"]))

// 修改响应内容
case "post_upstream":
    ev.Payload["response"]["output"][0]["content"][0]["text"] = "[FILTERED]"
    return gateyes.WriteGatewayCommand(outputPtr,
        gateyes.TransformResponseGateway(ev.Payload["response"]))
```

### gRPC

```go
case pluginv1.Phase_PHASE_PRE_UPSTREAM:
    var payload struct {
        Request map[string]any `json:"request"`
    }
    json.Unmarshal(ev.GetPayload(), &payload)

    // 修改模型
    payload.Request["model"] = "gpt-4o"
    transformed, _ := json.Marshal(payload.Request)

    cmd = &pluginv1.Command{
        Action:  pluginv1.Action_ACTION_TRANSFORM,
        Payload: transformed,
    }
```

---

## 调试技巧

### 1. 看 gateway 日志

```bash
./gateway -config configs/config.yaml 2>&1 | grep -i plugin
```

关键日志：
- `grpc plugin became healthy` — 插件连接成功
- `grpc plugin health check failed` — 插件连不上
- `gateway plugin process stream failed` — 调用失败

### 2. 确认 phase 被调用

在 plugin 里打日志：

```go
log.Printf("[%s] trace=%s", phase.String(), ctx.GetTraceId())
```

如果看不到日志，检查：
- `phases` 配置是否正确
- 请求路径是否经过对应 phase（如 `pre_route` 只在流式路径）

### 3. 确认 BLOCK 生效

如果 plugin 返回 BLOCK 但请求仍然通过，检查：
- Action 是否用了正确的枚举（`ACTION_BLOCK` 不是 `"BLOCK"`）
- `phases` 配置是否包含该 phase
- gateway 日志是否有 `gateway plugin recv failed` 错误

### 4. 用 grpcurl 测试 gRPC plugin

```bash
# 列出服务
grpcurl -plaintext localhost:50052 list

# 健康检查
grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check
```

---

## 常见问题

### Q: 为什么我的 WASM plugin 不生效？

1. 确认 `plugins.enabled: true`
2. 确认 `wasmPlugins` 中 `phases` 包含正确的 phase
3. 确认 `.wasm` 文件路径正确
4. 确认 `evaluate_gateway` 函数被 `//export` 导出

### Q: 为什么 gRPC plugin 连不上？

1. 确认 plugin 先启动，gateway 后启动
2. 确认 `address` 和实际监听端口一致
3. 确认注册了 gRPC health check（`grpc_health_v1.RegisterHealthServer`）
4. 查看 gateway 日志中的连接错误

### Q: `pre_route` / `post_route` 为什么没触发？

这两个 phase **只在流式路径**（`stream: true`）中触发。非流式请求（`Create`）不走这两个 phase。

### Q: TRANSFORM 后请求格式不对？

TRANSFORM 的 payload 必须是完整的 `ResponseRequest`（pre_upstream）或 `Response`（post_upstream）JSON。不能是部分字段。

---

## 参考示例

| 示例 | 路径 | 说明 |
|------|------|------|
| WASM keyword block | `plugins/examples/keyword_block/` | 旧 guardrail 接口（deprecated） |
| WASM auditor | `plugins/examples/wasm_auditor/` | 新 GatewayPlugin 接口 |
| gRPC router | `plugins/examples/grpc_router/` | 加权随机路由 |
| gRPC auditor | `plugins/examples/grpc_auditor/` | 简单审计 |
| PII WASM guardrail | `plugins/examples/pii_guard/` | SSN 检测 + redaction |
