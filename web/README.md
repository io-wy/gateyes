# Gateyes Admin Frontend

Gateyes LLM API Gateway 的 Web 管理控制台。

## 技术栈

- React 19 + TypeScript
- Vite 6
- Tailwind CSS 4 + shadcn/ui
- TanStack Query + TanStack Router
- Zustand
- Recharts

## 开发

```bash
cd web
npm install
npm run dev
```

开发服务器默认监听 `http://localhost:5173`，并通过 Vite proxy 将 `/admin`、`/service` 请求转发到 `http://localhost:8028`（与 `configs/config.yaml` 中的 `server.listenAddr` 保持一致）。

## 登录

使用 Gateyes Admin API Key 登录，格式：

```
key:secret
```

例如：`admin-key-001:your-bootstrap-secret`

## 脚本

```bash
npm run dev          # 启动开发服务器
npm run build        # 生产构建
npm run typecheck    # TypeScript 类型检查
npm run lint         # ESLint
npm run lint:fix     # ESLint 自动修复
npm run format       # Prettier 格式化
npm run format:check # Prettier 格式检查
```

## 项目结构

```
src/
  api/           # API client 与各模块 API
  components/    # 共享组件 + shadcn/ui 组件
  hooks/         # 自定义 hooks
  lib/           # 工具函数
  pages/         # 页面组件
  routes/        # TanStack Router 路由定义
  stores/        # Zustand 状态
  types/         # TypeScript 类型
```

## 已实现功能

- [x] 登录（Admin API Key 验证）
- [x] API Test Playground（Service surfaces / stream 事件查看）
- [x] Dashboard 占位 + 原始数据展示
- [x] Provider 管理（列表/创建/编辑/删除/健康检查/默认模板）
- [x] API Key 管理（列表/创建/编辑/轮换/吊销）
- [x] Virtual Key 管理（列表/创建/编辑/删除）
- [x] Project 管理（列表/创建/编辑/删除/用量弹窗）
- [x] User 管理（列表/创建/编辑/删除/重置用量/用量弹窗）
- [x] Tenant 管理（列表/创建/编辑/删除/Provider 绑定）
- [x] Service 管理（列表/创建/编辑/删除/版本发布/回滚/promote）
- [x] Response 查询 + JSON / Trace viewer
- [x] 审计日志
- [x] Config Reload

## 测试

```bash
# E2E 测试（依赖后端跑在 localhost:8028）
npm run test:e2e

# 带 UI 的 E2E 调试
npm run test:e2e:ui
```

E2E 测试默认用 `admin-key-001:local-admin-secret` 登录，可在 `e2e/smoke.spec.ts` 中修改。

## 后续可优化

- [ ] Dashboard 真实图表（Recharts 对接 usage/budgets API）
- [ ] 分页与搜索增强
- [ ] Service Subscription 审批界面
- [ ] 权限控制（当前仅菜单可见，后端已做最终鉴权）
- [ ] 代码分割降低首屏 JS 体积（当前 576KB）
