---
name: change-impact-scan
version: 1.1.0
description: 变更影响扫描。修改接口/数据模型/配置/公共模块时，自动 Grep 所有调用点和关联文件，防止改不全。Triggered when modifying function signatures, interfaces, data models, config structs, or shared modules, or when user says "改影响/影响范围/改不全/impact scan/调用点/影响分析/change impact/改接口/改模型".
---

# Change Impact Scan

> 核心：「改不全」是 AI Coding 高频问题——局部视角，遗忘全局影响。本 Skill 强制全局扫描。

## 触发条件

- 修改函数签名或接口
- 修改数据模型/Schema
- 修改配置结构
- 修改公共模块（被 3+ 文件引用）
- 修改 API 路由或请求/响应结构
- io-wy 说「扫描影响」「影响分析」「impact scan」

## 执行流程

### Step 1：识别变更类型

| 类型 | 判断依据 |
|------|----------|
| 签名变更 | 参数、返回值、接口方法变化 |
| 数据模型 | struct 字段增删改 |
| 配置变更 | Config struct 字段增删改 |
| 路由变更 | API 路径、HTTP 方法、中间件链变化 |

### Step 2：执行 Grep 扫描

**签名变更**：
```bash
grep -rn "funcName\|InterfaceName" internal/
```

**数据模型变更**：
```bash
grep -rn "StructName{" internal/
grep -rn "StructName\." internal/
```

**配置变更**：
```bash
grep -rn "config\.Field\|cfg\.Field" internal/
```

### Step 3：生成影响报告

```markdown
# 变更影响报告

## 变更概述
- 类型: 签名变更
- 文件: internal/service/service.go:45

## 直接影响（必须同步修改）
| 文件 | 行号 | 说明 | 需修改 |
|------|------|------|--------|
| handler.go:67 | 调用点 | 参数变化 | 是 |

## 间接影响（可能需修改）
| 文件 | 行号 | 说明 | 需修改 |
|------|------|------|--------|
| router.go:12 | 间接调用 | 需确认 | 待确认 |

## 配置/文档同步
| 文件 | 需同步 |
|------|--------|
| config.example.yaml | 否 |
```

### Step 4：io-wy 确认后才动手改

影响报告展示给 io-wy，确认后再开始修改。

## 常见错误

| 错误 | 后果 | 预防 |
|------|------|------|
| 只改函数不改调用点 | 编译报错或运行时 panic | Step 2 Grep 扫描 |
| 改了代码不改配置 | 配置漂移 | Step 3 配置同步检查 |
| 全局扫描未完就动手 | 遗漏关联修改 | Step 4 先确认再改 |

## 验证清单

- [ ] 变更类型已识别
- [ ] Grep 扫描已完成
- [ ] 影响报告已输出
- [ ] io-wy 已确认影响范围
