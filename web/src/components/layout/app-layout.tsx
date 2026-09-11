import { useEffect } from 'react'
import { Outlet, Link, useLocation } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  LayoutDashboard,
  FlaskConical,
  Server,
  Key,
  KeyRound,
  FolderKanban,
  Users,
  Building2,
  Boxes,
  Puzzle,
  MessageSquareReply,
  ScrollText,
  Settings,
  LogOut,
  Store,
} from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { authApi } from '@/api/auth'
import { hasAnyPermission, isAdminIdentity, isTenantUser } from '@/lib/authz'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'

const adminNavItems = [
  { name: 'Dashboard', path: '/', icon: LayoutDashboard },
  { name: 'Playground', path: '/playground', icon: FlaskConical },
  { name: 'Provider', path: '/providers', icon: Server },
  { name: 'API Key', path: '/keys', icon: Key },
  { name: 'Virtual Key', path: '/virtual-keys', icon: KeyRound },
  { name: 'Project', path: '/projects', icon: FolderKanban },
  { name: 'User', path: '/users', icon: Users },
  { name: 'Tenant', path: '/tenants', icon: Building2 },
  { name: 'Service', path: '/services', icon: Boxes },
  { name: 'Plugin', path: '/plugins', icon: Puzzle },
  { name: 'Response', path: '/responses', icon: MessageSquareReply },
  { name: 'Audit', path: '/audit', icon: ScrollText },
  { name: 'Settings', path: '/settings', icon: Settings },
]

const userNavItems = [
  {
    name: 'Playground',
    path: '/playground',
    icon: FlaskConical,
    permissions: ['service:read'],
  },
  { name: 'API Key', path: '/keys', icon: Key, permissions: ['api_key:read'] },
  {
    name: 'Virtual Key',
    path: '/virtual-keys',
    icon: KeyRound,
    permissions: ['virtual_key:read'],
  },
  {
    name: '服务目录',
    path: '/catalog',
    icon: Store,
    permissions: ['service:read'],
  },
  {
    name: '调用记录',
    path: '/responses',
    icon: MessageSquareReply,
    permissions: ['response:read'],
  },
  {
    name: '用量',
    path: '/',
    icon: LayoutDashboard,
    permissions: ['usage:read'],
  },
  { name: 'Settings', path: '/settings', icon: Settings },
]

export function AppLayout() {
  const location = useLocation()
  const logout = useAuthStore((state) => state.logout)
  const token = useAuthStore((state) => state.token)
  const identity = useAuthStore((state) => state.identity)
  const setIdentity = useAuthStore((state) => state.setIdentity)

  const { data: loadedIdentity } = useQuery({
    queryKey: ['auth-me'],
    queryFn: () => authApi.me(),
    enabled: !!token && !identity,
    retry: false,
  })

  useEffect(() => {
    if (loadedIdentity) {
      setIdentity(loadedIdentity)
    }
  }, [loadedIdentity, setIdentity])

  const navItems = isTenantUser(identity)
    ? userNavItems.filter(
        (item) =>
          !item.permissions || hasAnyPermission(identity, item.permissions)
      )
    : isAdminIdentity(identity)
      ? adminNavItems
      : userNavItems.filter(
          (item) =>
            !item.permissions || hasAnyPermission(identity, item.permissions)
        )

  return (
    <div className="bg-muted/30 flex h-screen">
      <aside className="bg-card/95 flex w-64 flex-col border-r shadow-sm">
        <div className="flex h-14 items-center px-4 font-semibold">
          Gateyes 控制台
        </div>
        <Separator />
        <nav className="flex-1 overflow-auto px-3 py-4">
          <ul className="space-y-1">
            {navItems.map((item) => {
              const Icon = item.icon
              const active = location.pathname === item.path
              return (
                <li key={item.path}>
                  <Link
                    to={item.path}
                    className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
                      active
                        ? 'bg-primary text-primary-foreground'
                        : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    {item.name}
                  </Link>
                </li>
              )
            })}
          </ul>
        </nav>
        <Separator />
        <div className="p-2">
          <Button
            variant="ghost"
            className="w-full justify-start gap-2"
            onClick={logout}
          >
            <LogOut className="h-4 w-4" />
            退出登录
          </Button>
        </div>
      </aside>
      <main className="min-w-0 flex-1 overflow-auto p-4 sm:p-6 lg:p-8">
        <Outlet />
      </main>
    </div>
  )
}
