# Benchmark

Gateyes 压测与测试工具目录。

## 目录结构

| 目录 | 说明 |
|---|---|
| `cmd/loadtest/` | 单场景阶梯压测工具源码 |
| `cmd/multiscenario/` | 多场景混合压测工具源码 |
| `cmd/mockupstream/` | Mock 上游服务源码（用于零成本测试） |
| `runtime/` | 压测运行时目录（配置、脚本，不提交二进制/日志/数据库） |
| `k6.js` | k6 压测脚本 |

## 使用方式

```bash
# 编译压测工具
go build -o bin/loadtest ./cmd/loadtest
go build -o bin/multiscenario ./cmd/multiscenario
go build -o bin/mockupstream ./cmd/mockupstream

# 运行压测（进入 runtime/ 目录）
cd runtime
../../bin/loadtest -url http://localhost:8083/v1/chat/completions ...
```
