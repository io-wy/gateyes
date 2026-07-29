import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, ShieldAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { useAuthStore } from '@/stores/auth-store'
import { dashboardApi } from '@/api/dashboard'
import { toast } from 'sonner'

function parseHashParams(hash: string): Record<string, string> {
  const params: Record<string, string> = {}
  if (!hash) return params
  const cleaned = hash.startsWith('#') ? hash.slice(1) : hash
  const searchParams = new URLSearchParams(cleaned)
  searchParams.forEach((value, key) => {
    params[key] = value
  })
  return params
}

export function OIDCCallbackPage() {
  const navigate = useNavigate()
  const setOIDCTokens = useAuthStore((state) => state.setOIDCTokens)
  const [processed, setProcessed] = useState(false)

  const params = useMemo(() => parseHashParams(window.location.hash), [])
  const errorParam = params.error
  const accessToken = params.access_token
  const refreshToken = params.refresh_token

  const errorMessage = useMemo(() => {
    if (errorParam) {
      return decodeURIComponent(errorParam) || 'OIDC 登录失败'
    }
    if (!accessToken || !refreshToken) {
      return '未从身份提供商获取到令牌'
    }
    return null
  }, [errorParam, accessToken, refreshToken])

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (processed || errorMessage) return

    setOIDCTokens(accessToken, refreshToken)
    setProcessed(true)

    dashboardApi
      .getSummary()
      .then(() => {
        toast.success('登录成功')
        navigate({ to: '/' })
      })
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : '令牌验证失败')
      })
  }, [processed, errorMessage, accessToken, refreshToken, setOIDCTokens, navigate])
  /* eslint-enable react-hooks/set-state-in-effect */

  if (errorMessage) {
    return (
      <div className="bg-muted/50 flex min-h-screen items-center justify-center p-4">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center">
            <div className="bg-destructive text-destructive-foreground mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full">
              <ShieldAlert className="h-6 w-6" />
            </div>
            <CardTitle className="text-2xl">登录失败</CardTitle>
            <CardDescription>{errorMessage}</CardDescription>
          </CardHeader>
          <CardContent>
            <Button className="w-full" onClick={() => navigate({ to: '/login' })}>
              返回登录页
            </Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="bg-muted/50 flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <Loader2 className="mx-auto mb-4 h-10 w-10 animate-spin text-primary" />
          <CardTitle className="text-2xl">正在登录</CardTitle>
          <CardDescription>请稍候，正在完成 OIDC 登录...</CardDescription>
        </CardHeader>
      </Card>
    </div>
  )
}
