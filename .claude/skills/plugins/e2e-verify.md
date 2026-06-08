---
name: e2e-verify
version: 1.0.0
user-invocable: false
description: 端到端验证管道。build → lint-arch → test → verify 完整验证。verify 走真实 HTTP 请求/CLI 命令验证功能正确性。Auto-triggered after execute-with-review Step 6 (test validation).
---

# E2E Verify

> 核心：build + lint + test 能覆盖代码层面，但验证不了"用户执行此操作，最终结果对不对"。verify 补最后一环。

## 验证管道

```
build → lint-arch → test → verify
  │        │         │       │
  │        │         │       └─ 端到端功能验证（项目级）
  │        │         └─ go test ./...
  │        └─ lint-deps + lint-quality
  └─ go build ./...
```

## Verify vs Test

| | Test | Verify |
|--|------|--------|
| 层级 | 函数/模块 | 项目/系统 |
| 验证什么 | 函数返回值对不对 | 用户操作最终结果对不对 |
| 执行方式 | 内存中运行 | 实际运行 CLI / 发 HTTP 请求 |
| 覆盖范围 | 代码路径 | 完整用户流程 |

## 验证失败修复循环

验证失败 → 分析错误 → 修改代码 → 重新验证（一般 1-3 轮收敛）
同一错误 3 轮没过 → 停止，交给人。

## 常见错误
| 错误 | 预防 |
|------|------|
| 跳过 verify 只跑 test | verify 强制步骤 |
| 用 mock 做 verify | verify 走真实网络/进程 |
| 验证失败循环 >3 轮 | 3 轮后交给人 |
