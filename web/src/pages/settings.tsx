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

export function SettingsPage() {
  const token = useAuthStore((state) => state.token)

  const reloadMutation = useMutation({
    mutationFn: settingsApi.reloadConfig,
    onSuccess: () => {
      toast.success('配置已重载')
    },
  })

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">系统设置</h1>

      <Card>
        <CardHeader>
          <CardTitle>当前身份</CardTitle>
          <CardDescription>当前登录使用的 Admin API Key</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <div className="text-sm">
            <span className="font-medium">Token: </span>
            <span className="text-muted-foreground font-mono">{token}</span>
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
