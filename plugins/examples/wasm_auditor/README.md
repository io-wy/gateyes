# WASM Auditor Plugin Example

A minimal example of a WASM GatewayPlugin for Gateyes that logs all request lifecycle events.

## What it does

Subscribes to three phases:
- `pre_upstream` — logs before request goes upstream
- `post_upstream` — logs after response received
- `audit` — logs final audit data

## Build

Requires [TinyGo](https://tinygo.org/getting-started/install/):

```bash
tinygo build -o wasm_auditor.wasm -target=wasi -no-debug -opt=z .
```

## Gateway config

Add to your `config.yaml`:

```yaml
wasmPlugins:
  - name: wasm-auditor
    path: ./plugins/examples/wasm_auditor/wasm_auditor.wasm
    phases:
      - pre_upstream
      - post_upstream
      - audit
```

## Implement your own

1. Import the SDK: `github.com/gateyes/gateway/plugins/sdk/gateyes`
2. Export `evaluate_gateway` function
3. Use `gateyes.ReadGatewayEvent` to parse input
4. Use `gateyes.WriteGatewayCommand` to return commands
5. Handle phases you care about, return `AllowGateway()` to continue
