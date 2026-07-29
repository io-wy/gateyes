import {
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
  Outlet,
} from '@tanstack/react-router'
import { AppLayout } from '@/components/layout/app-layout'
import {
  DashboardPage,
  LoginPage,
  OIDCCallbackPage,
  PlaygroundPage,
  ProvidersPage,
  KeysPage,
  VirtualKeysPage,
  ProjectsPage,
  UsersPage,
  TenantsPage,
  ServicesPage,
  PluginsPage,
  ResponsesPage,
  AuditPage,
  SettingsPage,
} from '@/pages'
import { useAuthStore } from '@/stores/auth-store'

const rootRoute = createRootRoute({
  component: () => <Outlet />,
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
  beforeLoad: () => {
    const token = useAuthStore.getState().token
    if (token) {
      throw redirect({ to: '/' })
    }
  },
})

const oidcCallbackRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/oidc/callback',
  component: OIDCCallbackPage,
})

const authLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'auth-layout',
  component: AppLayout,
  beforeLoad: () => {
    const token = useAuthStore.getState().token
    if (!token) {
      throw redirect({ to: '/login' })
    }
  },
})

const dashboardRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/',
  component: DashboardPage,
})

const providersRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/providers',
  component: ProvidersPage,
})

const playgroundRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/playground',
  component: PlaygroundPage,
})

const keysRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/keys',
  component: KeysPage,
})

const virtualKeysRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/virtual-keys',
  component: VirtualKeysPage,
})

const projectsRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/projects',
  component: ProjectsPage,
})

const usersRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/users',
  component: UsersPage,
})

const tenantsRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/tenants',
  component: TenantsPage,
})

const servicesRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/services',
  component: ServicesPage,
})

const pluginsRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/plugins',
  component: PluginsPage,
})

const responsesRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/responses',
  component: ResponsesPage,
})

const auditRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/audit',
  component: AuditPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => authLayoutRoute,
  path: '/settings',
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  oidcCallbackRoute,
  authLayoutRoute.addChildren([
    dashboardRoute,
    playgroundRoute,
    providersRoute,
    keysRoute,
    virtualKeysRoute,
    projectsRoute,
    usersRoute,
    tenantsRoute,
    servicesRoute,
    pluginsRoute,
    responsesRoute,
    auditRoute,
    settingsRoute,
  ]),
])

export const router = createRouter({ routeTree })

// Type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
