import { Outlet, Link, useLocation } from '@tanstack/react-router'
import {
  LayoutDashboard,
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
} from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'

const navItems = [
  { name: 'Dashboard', path: '/', icon: LayoutDashboard },
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

export function AppLayout() {
  const location = useLocation()
  const logout = useAuthStore((state) => state.logout)

  return (
    <div className="bg-background flex h-screen">
      <aside className="bg-card flex w-60 flex-col border-r">
        <div className="flex h-14 items-center px-4 font-semibold">
          Gateyes 控制台
        </div>
        <Separator />
        <nav className="flex-1 overflow-auto px-2 py-3">
          <ul className="space-y-1">
            {navItems.map((item) => {
              const Icon = item.icon
              const active = location.pathname === item.path
              return (
                <li key={item.path}>
                  <Link
                    to={item.path}
                    className={`flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
                      active
                        ? 'bg-primary text-primary-foreground'
                        : 'hover:bg-muted'
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
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  )
}
