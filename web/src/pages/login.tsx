import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Eye, EyeOff, Shield } from 'lucide-react'
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

export function LoginPage() {
  const navigate = useNavigate()
  const [credential, setCredential] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [loading, setLoading] = useState(false)
  const setToken = useAuthStore((state) => state.setToken)

  const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setLoading(true)
    try {
      const trimmed = credential.trim()
      if (!trimmed.includes(':')) {
        throw new Error('格式错误，请使用 key:secret 格式')
      }
      setToken(trimmed)
      // 用 dashboard 接口验证 token 是否有效
      await dashboardApi.getSummary()
      navigate({ to: '/' })
    } catch {
      setToken('')
    } finally {
      setLoading(false)
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
          <CardDescription>输入 Admin API Key 登录</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
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
        </CardContent>
      </Card>
    </div>
  )
}
