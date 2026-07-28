# Gateyes Admin Frontend — Technical Implementation Plan

## 1. Goal

基于 `spec.md` 输出可运行的工程骨架，包含：

- Vite + React 19 + TypeScript 项目
- Tailwind CSS v4 + shadcn/ui 组件库
- TanStack Query + TanStack Router + Zustand
- 带 auth 拦截的 API client
- 登录页 + Dashboard 占位 + 基础布局 + 侧边栏导航

## 2. Tech Stack & Versions

| 依赖 | 版本 | 说明 |
|------|------|------|
| react | ^19.0.0 | UI 框架 |
| react-dom | ^19.0.0 | DOM 渲染 |
| vite | ^6.x | 构建工具 |
| typescript | ^5.7 | 类型系统 |
| tailwindcss | ^4.0 | 原子 CSS |
| @tailwindcss/vite | ^4.0 | Vite 插件 |
| @tanstack/react-query | ^5.x | 服务端状态管理 |
| @tanstack/react-router | ^1.x | 类型安全路由 |
| zustand | ^5.x | 客户端全局状态 |
| axios | ^1.x | HTTP client（fetch 也可以，axios 拦截器更成熟） |
| recharts | ^2.x | 图表 |
| lucide-react | latest | 图标 |
| shadcn/ui | latest CLI | 组件脚手架 |
| zod | ^3.x | Schema 校验 |

Dev tools:

- `eslint` + `@eslint/js` + `typescript-eslint` + `eslint-plugin-react-hooks`
- `prettier` + `prettier-plugin-tailwindcss`
- `vite-plugin-checker`（可选，开发时 TS 类型检查）

## 3. Project Structure

```
web/
├── public/                  # 静态资源
├── src/
│   ├── api/                 # API client + 各模块 API
│   │   ├── client.ts        # axios 实例 + auth 拦截器
│   │   ├── auth.ts          # 登录相关（未来）
│   │   ├── dashboard.ts     # Dashboard / usage / budgets
│   │   ├── providers.ts     # Provider CRUD
│   │   ├── keys.ts          # API Key / Virtual Key
│   │   ├── projects.ts      # Project
│   │   ├── users.ts         # User
│   │   ├── tenants.ts       # Tenant
│   │   ├── services.ts      # Service
│   │   ├── responses.ts     # Response / trace
│   │   └── audit.ts         # Audit log
│   ├── components/          # 共享组件
│   │   └── ui/              # shadcn/ui 组件
│   ├── hooks/               # 自定义 hooks
│   │   └── use-auth.ts
│   ├── lib/                 # 工具函数
│   │   └── utils.ts
│   ├── pages/               # 页面组件
│   │   ├── login.tsx
│   │   ├── dashboard.tsx
│   │   ├── providers.tsx
│   │   ├── keys.tsx
│   │   ├── virtual-keys.tsx
│   │   ├── projects.tsx
│   │   ├── users.tsx
│   │   ├── tenants.tsx
│   │   ├── services.tsx
│   │   ├── responses.tsx
│   │   ├── audit.tsx
│   │   └── settings.tsx
│   ├── routes/              # TanStack Router 路由定义
│   │   └── __root.tsx
│   ├── stores/              # Zustand stores
│   │   └── auth-store.ts
│   ├── types/               # TypeScript 类型
│   │   ├── api.ts           # 通用响应/分页类型
│   │   ├── auth.ts
│   │   ├── dashboard.ts
│   │   ├── provider.ts
│   │   └── ...
│   ├── App.tsx
│   ├── main.tsx
│   └── index.css
├── index.html
├── package.json
├── tsconfig.json
├── tsconfig.app.json
├── tsconfig.node.json
├── vite.config.ts
├── eslint.config.js
├── prettier.config.js
└── components.json          # shadcn/ui 配置
```

## 4. API Client Design

### 4.1 基础配置

```ts
// src/api/client.ts
import axios from 'axios';

const client = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/admin/v1',
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' },
});

client.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

client.interceptors.response.use(
  (res) => res.data?.data ?? res.data,
  (err) => {
    const message = err.response?.data?.message || err.message;
    if (err.response?.status === 401) {
      useAuthStore.getState().logout();
      window.location.href = '/login';
    }
    return Promise.reject(new Error(message));
  }
);
```

### 4.2 Admin API 响应格式

后端返回统一结构：

```json
{
  "code": 0,
  "success": true,
  "message": "ok",
  "data": { ... }
}
```

API client 默认提取 `data.data`。

列表返回：

```json
{
  "data": [...],
  "total": 100
}
```

## 5. Routing

使用 TanStack Router，文件式路由（file-based routing）可选。为减少复杂度，首版采用代码式路由：

```tsx
const rootRoute = createRootRoute({
  component: () => (
    <>
      <Outlet />
      <TanStackRouterDevtools />
    </>
  ),
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
});

const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: DashboardPage,
});
```

受保护路由通过 `beforeLoad` 检查 `authStore.token`。

## 6. Auth State

```ts
// src/stores/auth-store.ts
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface AuthState {
  token: string | null;
  identity: Identity | null;
  setToken: (token: string) => void;
  setIdentity: (identity: Identity) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      identity: null,
      setToken: (token) => set({ token }),
      setIdentity: (identity) => set({ identity }),
      logout: () => set({ token: null, identity: null }),
    }),
    { name: 'gateyes-auth' }
  )
);
```

**注意：** token 持久化在 localStorage 中，生产环境建议通过 `httpOnly` cookie 或更安全的存储。本阶段为本地开发便利使用 localStorage。

## 7. Layout

```tsx
// src/components/layout.tsx
export function Layout() {
  return (
    <div className="flex h-screen">
      <Sidebar />
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
```

Sidebar 菜单按权限展示（首版先做可见性控制，后端再做最终鉴权）。

## 8. Dev & Build

```bash
# 安装依赖
cd web && npm install

# 启动开发服务器（代理到本地 Gateway）
npm run dev

# 类型检查
npm run typecheck

# 构建
npm run build

# 代码格式化
npm run format
```

### Vite Proxy 配置

```ts
// vite.config.ts
server: {
  proxy: {
    '/admin': {
      target: 'http://localhost:8080',
      changeOrigin: true,
    },
  },
}
```

本地 Gateway 默认监听 `8080`，开发时前端请求 `/admin/v1/*` 自动代理。

## 9. First Milestone Scope

M1 目标是「能登录、能看 Dashboard、能进各页面」：

- [x] 工程骨架
- [ ] 登录页（输入 key:secret，写入 authStore）
- [ ] Dashboard 占位（调用 `/admin/v1/dashboard` 展示原始 JSON）
- [ ] 侧边栏导航
- [ ] Provider / Keys / Projects / Users / Responses / Audit 等空页面占位
- [ ] API client 骨架（含 auth 拦截器）
- [ ] 统一错误提示（Toast）

后续里程碑再逐个页面填充表单、表格、图表。

## 10. Risks

| 风险 | 缓解 |
|------|------|
| shadcn/ui CLI 与 Tailwind v4 兼容性 | 使用最新版 shadcn CLI，必要时手动初始化 |
| Admin API 无 CORS | 开发通过 Vite proxy 绕过；生产前端与 Gateway 同域部署 |
| 后端返回结构不一致 | API client 统一处理 `data.data`，逐个模块对接时校验 |
| 类型漂移 | 用 Zod 解析关键响应，避免运行时结构变化导致崩溃 |
