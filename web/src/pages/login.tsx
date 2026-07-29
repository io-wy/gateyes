import { useEffect, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Eye, EyeOff, Shield, KeyRound, Lock } from 'lucide-react'
import axios from 'axios'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuthStore } from '@/stores/auth-store'
import { dashboardApi } from '@/api/dashboard'
import { toast } from 'sonner'

type AuthMode = 'apikey' | 'oidc'

interface OIDCStatusResponse {
  success: boolean
  enabled?: boolean
}

interface OIDCLoginResponse {
  success: boolean
  auth_url?: string
  state?: string
}

export function LoginPage() {
  const navigate = useNavigate()
  const [mode, setMode] = useState<AuthMode>('apikey')
  const [credential, setCredential] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [loading, setLoading] = useState(false)
  const [oidcEnabled, setOidcEnabled] = useState(false)
  const [oidcLoading, setOidcLoading] = useState(false)
  const setAPIKeyToken = useAuthStore((state) => state.setAPIKeyToken)

  useEffect(() => {
    axios
      .get<OIDCStatusResponse>('/admin/auth/oidc/status', {
        withCredentials: true,
      })
      .then((res) => {
        setOidcEnabled(res.data.enabled ?? false)
      })
      .catch(() => {
        setOidcEnabled(false)
      })
  }, [])

  const handleApiKeySubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setLoading(true)
    try {
      const trimmed = credential.trim()
      if (!trimmed.includes(':')) {
        throw new Error('格式错误，请使用 key:secret 格式')
      }
      setAPIKeyToken(trimmed)
      await dashboardApi.getSummary()
      navigate({ to: '/' })
    } catch (err) {
      setAPIKeyToken('')
      toast.error(err instanceof Error ? err.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  const handleOIDCLogin = async () => {
    setOidcLoading(true)
    try {
      const res = await axios.get<OIDCLoginResponse>('/admin/auth/oidc/login', {
        withCredentials: true,
      })
      const authURL = res.data.auth_url
      if (!authURL) {
        throw new Error('未获取到 OIDC 授权地址')
      }
      window.location.href = authURL
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'OIDC 登录启动失败')
    } finally {
      setOidcLoading(false)
    }
  }

  return (
    <div className="bg-muted/50 flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <div className="bg-primary text-primary-foreground mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Shield className="h-6 w-6" />
          </div>
          <CardTitle className="text-2xl">Gateyes 控制台</CardTitle>
          <CardDescription>选择登录方式</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-2">
            <Button
              type="button"
              variant={mode === 'apikey' ? 'default' : 'outline'}
              onClick={() => setMode('apikey')}
              className="w-full"
            >
              <KeyRound className="mr-2 h-4 w-4" />
              API Key
            </Button>
            <Button
              type="button"
              variant={mode === 'oidc' ? 'default' : 'outline'}
              onClick={() => setMode('oidc')}
              disabled={!oidcEnabled}
              className="w-full"
            >
              <Lock className="mr-2 h-4 w-4" />
              OIDC
            </Button>
          </div>

          {mode === 'apikey' ? (
            <form onSubmit={handleApiKeySubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="credential">API Key</Label>
                <div className="relative">
                  <Input
                    id="credential"
                    type={showSecret ? 'text' : 'password'}
                    placeholder="admin-key-001:your-secret"
                    value={credential}
                    onChange={(e) => setCredential(e.target.value)}
                    required
                  />
                  <button
                    type="button"
                    onClick={() => setShowSecret(!showSecret)}
                    className="text-muted-foreground absolute top-1/2 right-2 -translate-y-1/2"
                  >
                    {showSecret ? (
                      <EyeOff className="h-4 w-4" />
                    ) : (
                      <Eye className="h-4 w-4" />
                    )}
                  </button>
                </div>
                <p className="text-muted-foreground text-xs">
                  格式：key:secret，例如 admin-key-001:xxx
                </p>
              </div>
              <Button type="submit" className="w-full" disabled={loading}>
                {loading ? '登录中...' : '登录'}
              </Button>
            </form>
          ) : (
            <div className="space-y-4">
              <p className="text-muted-foreground text-sm">
                点击按钮跳转至企业身份提供商完成登录。
              </p>
              <Button
                type="button"
                className="w-full"
                onClick={handleOIDCLogin}
                disabled={oidcLoading || !oidcEnabled}
              >
                {oidcLoading ? '跳转中...' : '使用 OIDC 登录'}
              </Button>
              {!oidcEnabled && (
                <p className="text-destructive text-xs">
                  后端未启用 OIDC，请联系管理员开启。
                </p>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
