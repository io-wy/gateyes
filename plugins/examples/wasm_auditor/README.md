# WASM Auditor 插件示例

一个最小的 Gateyes WASM GatewayPlugin 示例，记录请求生命周期中的所有事件。

## 功能

订阅三个阶段：
- `pre_upstream` —— 请求发往上游前记录
- `post_upstream` —— 收到响应后记录
- `audit` —— 记录最终审计数据

## 构建

需要安装 [TinyGo](https://tinygo.org/getting-started/install/)：

```bash
tinygo build -o wasm_auditor.wasm -target=wasi -no-debug -opt=z .
```

## 网关配置

添加到你的 `config.yaml`：

```yaml
wasmPlugins:
  - name: wasm-auditor
    path: ./plugins/examples/wasm_auditor/wasm_auditor.wasm
    phases:
      - pre_upstream
      - post_upstream
      - audit
```

## 实现你自己的插件

1. 引入 SDK：`github.com/gateyes/gateway/plugins/sdk/gateyes`
2. 导出 `evaluate_gateway` 函数
3. 使用 `gateyes.ReadGatewayEvent` 解析输入
4. 使用 `gateyes.WriteGatewayCommand` 返回命令
5. 处理你关心的 phase，返回 `AllowGateway()` 以继续执行
