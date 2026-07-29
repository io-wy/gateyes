import { useMutation } from '@tanstack/react-query'
import { RotateCcw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { useAuthStore } from '@/stores/auth-store'
import { settingsApi } from '@/api/settings'
import { toast } from 'sonner'

function maskToken(token: string | null): string {
  if (!token) return '未登录'
  if (token.length <= 12) return '*'.repeat(token.length)
  return `${token.slice(0, 6)}...${token.slice(-6)}`
}

export function SettingsPage() {
  const token = useAuthStore((state) => state.token)
  const authMethod = useAuthStore((state) => state.authMethod)

  const reloadMutation = useMutation({
    mutationFn: settingsApi.reloadConfig,
    onSuccess: () => {
      toast.success('配置已重载')
    },
  })

  const methodLabel =
    authMethod === 'oidc' ? 'OIDC' : authMethod === 'apikey' ? 'API Key' : '未知'

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">系统设置</h1>

      <Card>
        <CardHeader>
          <CardTitle>当前身份</CardTitle>
          <CardDescription>当前登录方式及凭证</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <div className="text-sm">
            <span className="font-medium">登录方式：</span>
            <span className="text-muted-foreground">{methodLabel}</span>
          </div>
          <div className="text-sm">
            <span className="font-medium">Token：</span>
            <span className="text-muted-foreground font-mono">{maskToken(token)}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>配置重载</CardTitle>
          <CardDescription>
            重新加载运行时配置（需要 config_write 权限）
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            variant="outline"
            onClick={() => reloadMutation.mutate()}
            disabled={reloadMutation.isPending}
          >
            <RotateCcw className="mr-2 h-4 w-4" />
            {reloadMutation.isPending ? '重载中...' : 'Reload Config'}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
