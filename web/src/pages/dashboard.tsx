import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useQuery } from '@tanstack/react-query'
import { dashboardApi } from '@/api/dashboard'
import { useAuthStore } from '@/stores/auth-store'

export function DashboardPage() {
  const token = useAuthStore((state) => state.token)
  const { data, isLoading } = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => dashboardApi.getSummary(),
    enabled: !!token,
  })
  const { data: cacheData, isLoading: isCacheLoading } = useQuery({
    queryKey: ['cache-summary'],
    queryFn: () => dashboardApi.getCacheSummary(),
    enabled: !!token,
  })

  const cacheTotals = cacheData?.totals
  const formatRate = (value?: number) => `${((value ?? 0) * 100).toFixed(1)}%`
  const formatMs = (value?: number) => `${(value ?? 0).toFixed(2)} ms`
  const formatBytes = (value?: number) => {
    const safe = value ?? 0
    if (safe >= 1024) {
      return `${(safe / 1024).toFixed(1)} KiB`
    }
    return `${safe.toFixed(0)} B`
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">总请求数</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isLoading ? '-' : (data?.totalRequests ?? 0)}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">成功率</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isLoading
                ? '-'
                : `${((data?.successRate ?? 0) * 100).toFixed(1)}%`}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">平均延迟</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isLoading ? '-' : `${data?.avgLatencyMs ?? 0} ms`}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">活跃 Provider</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isLoading ? '-' : (data?.activeProviders ?? 0)}
            </div>
          </CardContent>
        </Card>
      </div>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">缓存命中率</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isCacheLoading ? '-' : formatRate(cacheTotals?.hit_rate)}
            </div>
            <div className="text-muted-foreground mt-1 text-xs">
              {cacheData?.enabled ? '已启用' : '未启用'}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">缓存命中 / 未命中</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isCacheLoading
                ? '-'
                : `${cacheTotals?.lookups.hit ?? 0} / ${cacheTotals?.lookups.miss ?? 0}`}
            </div>
            <div className="text-muted-foreground mt-1 text-xs">
              lookup {cacheTotals?.lookups.total ?? 0}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">缓存读取延迟</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isCacheLoading ? '-' : formatMs(cacheTotals?.lookup_avg_ms)}
            </div>
            <div className="text-muted-foreground mt-1 text-xs">
              平均 lookup
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">缓存体积</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {isCacheLoading ? '-' : formatBytes(cacheTotals?.value_avg_bytes)}
            </div>
            <div className="text-muted-foreground mt-1 text-xs">
              平均 value
            </div>
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>原始数据</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="bg-muted overflow-auto rounded-md p-4 text-xs">
            {JSON.stringify({ dashboard: data, cache: cacheData }, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </div>
  )
}
