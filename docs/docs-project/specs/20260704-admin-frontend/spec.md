# Gateyes Admin Frontend — Baseline Specification

## 1. Overview

为 Gateyes LLM API Gateway 构建一个 Web 管理控制台（Admin Dashboard）。该前端面向运维人员、平台管理员和租户管理员，提供对网关配置、用量、审计、Provider、API Key、项目/用户/服务等资源的可视化管理能力。

## 2. Why

当前 Gateyes 仅暴露 Admin REST API (`/admin/v1/*`)，所有管理操作需通过 curl/脚本完成：

- 配置 Provider、API Key、Virtual Key 容易出错且无法实时验证。
- 用量、预算、审计数据分散在不同 endpoint，缺乏统一视图。
- 故障排查时需要手动拼接 `/admin/v1/responses/:id/trace`、Prometheus 和日志。
- 非技术用户难以使用网关。

一个官方 Admin Frontend 可以降低运营门槛、减少误操作、提升可观测性。

## 3. Target Users

| 角色 | 权限范围 | 典型场景 |
|------|---------|---------|
| 超级管理员 (SuperAdmin) | 跨租户管理 | 创建租户、配置全局 Provider、查看平台级用量 |
| 租户管理员 (TenantAdmin) | 本租户内 | 管理 Project、API Key、Virtual Key、查看本租户用量 |
| 运维/只读用户 (ReadOnly) | 按权限只读 | 查看 Dashboard、审计日志、响应记录、Provider 状态 |

## 4. Functional Requirements

### 4.1 认证与登录

- 使用 Admin API Key（`Authorization: Bearer {key}:{secret}`）登录。
- 前端本地存储 token（优先 `httpOnly` cookie 或 secure localStorage）。
- 登录成功后调用 `/admin/v1/dashboard` 或读取身份上下文确认权限。
- 无权限页面隐藏入口；越权请求由后端返回 403，前端统一处理。

### 4.2 Dashboard 首页

- 展示核心指标卡片：总请求数、成功率、平均延迟、当前活跃 Provider 数、预算健康度。
- 调用 `/admin/v1/dashboard`、`/admin/v1/usage/summary`、`/admin/v1/budgets`。
- 支持时间范围选择（近 1h / 24h / 7d / 30d）。
- 图表：请求趋势、Top Models、Top Providers、错误分布。

### 4.3 Provider 管理

- Provider 列表：名称、类型、模型、状态、健康状态、最近检查时间。
- 创建/编辑 Provider：表单映射 `admin_provider.go` 中的字段。
- 健康检查：调用 `POST /admin/v1/providers/check`。
- Provider 统计：点击跳转 `/admin/v1/providers/:name/stats`。

### 4.4 API Key & Virtual Key 管理

- API Key 列表、创建、编辑、Rotate、Revoke。
- Virtual Key 列表、创建、编辑、删除。
- 创建成功后仅展示一次完整 secret，并提供复制按钮。

### 4.5 Project / User / Tenant 管理

- Project：CRUD、查看用量。
- User：CRUD、重置用量、查看用量。
- Tenant：仅超级管理员可见，CRUD、替换 Provider 绑定。

### 4.6 Service & Subscription（如启用 catalog 功能）

- Service 列表、版本管理、发布/回滚。
- Subscription 审批流程。

### 4.7 响应记录与审计

- 响应记录列表：支持按 provider、model、status、project、api_key、user、时间过滤。
- 单条响应详情 + Trace 可视化。
- 审计日志列表：action、resource_type、operator、时间。

### 4.8 配置重载

- 提供「Reload Config」按钮，调用 `POST /admin/v1/reload`。
- 操作前二次确认，操作后展示结果。

## 5. Out of Scope (MVP)

- 多语言 i18n（首版仅中文）。
- 复杂 RBAC 配置界面（复用后端权限，前端按角色隐藏菜单）。
- 实时监控告警配置（仅展示，告警仍通过 config 文件配置）。
- WASM/gRPC plugin 在线开发环境（仅展示已注册插件）。
- Electron / Tauri 等桌面壳（首版只做 Web，后续如确有强需求再考虑）。

## 6. Non-Functional Requirements

### 6.1 架构

- 独立前端工程，不耦合 Go 后端编译流程。
- 开发服务器通过 proxy 将 `/admin/v1` 请求转发到本地 Gateway。
- 生产构建为静态资源，可单独部署或嵌入到 gateway 二进制中（可选）。

### 6.2 性能

- 首屏加载时间 < 2s（压缩后 JS < 500KB）。
- 列表页支持分页/虚拟滚动，默认 limit 50。
- 图表数据按需聚合，避免一次性拉取过长时间范围原始点。

### 6.3 安全

- Admin token 不硬编码，通过登录页输入。
- 所有请求通过 HTTPS（生产）。
- 对表单输入做基本校验，敏感操作需二次确认。
- 不将 API Key secret 持久化到前端状态管理。

### 6.4 可访问性

- 遵循基本 a11y 标准（语义化标签、键盘导航、颜色对比度）。

## 7. Tech Stack Recommendation

**推荐：React 19 + TypeScript + Vite + TanStack Query + TanStack Router + shadcn/ui + Recharts**

| 层级 | 选型 | 理由 |
|------|------|------|
| 框架 | React 19 + Vite | 生态成熟，构建快，团队招聘友好 |
| 语言 | TypeScript | 与 Admin API 强类型对接，减少协议漂移 |
| 路由 | TanStack Router | 类型安全路由，适合管理后台 |
| 数据获取 | TanStack Query | 缓存、重试、分页、乐观更新 |
| UI 组件 | shadcn/ui + Tailwind CSS | 无运行时依赖，可高度定制 |
| 图表 | Recharts | 与 React 集成好，满足 Dashboard 需求 |
| 状态管理 | Zustand（轻量） | 仅需管理全局用户/权限/主题 |
| HTTP Client | fetch/axios | 标准或轻量，足够 |
| 构建产物 | 静态 SPA | 可部署到任意静态托管或 gateway 静态文件服务 |

备选方案：
- Vue 3 + Nuxt：如果团队更熟悉 Vue。
- Next.js：如需 SSR/SEO，但管理后台多为内部工具，SSR 收益有限。

## 8. API Integration

核心 Admin API 映射（详见 `internal/handler/server.go`）：

| 功能 | Endpoint | Method |
|------|----------|--------|
| Dashboard | `/admin/v1/dashboard` | GET |
| 用量摘要 | `/admin/v1/usage/summary` | GET |
| 用量明细 | `/admin/v1/usage/breakdown` | GET |
| 用量趋势 | `/admin/v1/usage/trend` | GET |
| 预算 | `/admin/v1/budgets` | GET |
| Provider | `/admin/v1/providers` | CRUD |
| Provider Stats | `/admin/v1/providers/:name/stats` | GET |
| Provider Check | `/admin/v1/providers/check` | POST |
| API Key | `/admin/v1/keys` | CRUD + rotate/revoke |
| Virtual Key | `/admin/v1/virtual-keys` | CRUD |
| Project | `/admin/v1/projects` | CRUD + usage |
| User | `/admin/v1/users` | CRUD + usage/reset |
| Tenant | `/admin/v1/tenants` | CRUD + providers |
| Service | `/admin/v1/services` | CRUD + versions/subscriptions |
| Response | `/admin/v1/responses` | GET + trace |
| Audit | `/admin/v1/audit` | GET |
| Config Reload | `/admin/v1/reload` | POST |

统一错误处理：后端返回 `{code, success, message, data}`，前端根据 `success` 和 HTTP status 展示 Toast/Modal。

## 9. Page / Navigation Structure

```
/                      Dashboard 首页
/providers             Provider 管理
/keys                  API Key 管理
/virtual-keys          Virtual Key 管理
/projects              Project 管理
/users                 User 管理
/tenants               Tenant 管理（SuperAdmin 可见）
/services              Service 管理
/responses             响应记录查询
/audit                 审计日志
/settings              系统设置 / Config Reload
```

## 10. Data Model Mapping (Frontend)

基于后端 `repository` 包与 `admin_*.go` 返回结构，前端定义 TypeScript interfaces：

- `Provider`, `ProviderStats`
- `APIKey`, `VirtualKey`
- `Project`, `User`, `Tenant`
- `Service`, `ServiceVersion`, `Subscription`
- `ResponseRecord`, `ResponseTrace`
- `AuditLog`, `UsageSummary`, `Budget`

## 11. Milestones

| 阶段 | 目标 | 预计周期 |
|------|------|---------|
| M1 | 工程初始化、登录、Dashboard、Provider 管理 | 1 周 |
| M2 | API Key / Virtual Key / Project / User 管理 | 1 周 |
| M3 | Tenant / Service / Response / Audit | 1 周 |
| M4 | 图表优化、权限细化、E2E 测试、文档 | 1 周 |

## 12. Open Questions

1. 前端是否作为独立仓库，还是放在当前 `Gateyes` 仓库的子目录（如 `web/`）？
2. 首版是否优先支持中文 UI，还是英文为主？
3. 是否需要将生产构建产物嵌入 gateway 二进制（通过 `embed.FS` 提供 `/` 静态服务）？
