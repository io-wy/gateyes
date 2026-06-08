---
name: pre-verify
version: 1.0.0
user-invocable: false
description: 预验证。执行结构性操作前（创建文件、跨包 import），先验证层级合法性。Auto-triggered before creating files in new locations or adding cross-package imports.
---

# Pre-Verify

> 核心：与其写 50 行代码后被 linter 拦住再撤销，不如在动手前花 2 次交互确认合法性。

## 触发条件

- 在新目录创建文件
- 添加跨包 import（特别是跨层 import）
- 修改公共接口/函数签名

## 不需要预验证

- 修改函数体（不新增 import）
- 同目录加测试文件
- 改 typo / 日志 / 纯文档

## 预验证流程

### Step 1: 识别操作类型
- 创建文件 → 检查目标目录层级 + 命名规范
- 添加 import → 检查方向（高层→低层合法，反向非法）

### Step 2: 执行验证

```
操作: internal/transport/http/handler 创建文件并 import internal/core
验证: handler(L4) → core(L3) → ✓ 合法

操作: internal/types import internal/app/config
验证: types(L0) → config(L2) → ✗ 非法
  Fix: 将 config 依赖逻辑移到更高层，或作为参数传入
```

### Step 3: 错误信息必须包含三要素
1. 什么规则违反了 2. 为什么是问题 3. 怎么修

## 与 change-impact-scan 的关系
pre-verify（事前）防违规发生，change-impact-scan（事后）扫描已发生的变更影响。
