import { useMemo, type ComponentType } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  BarChart3,
  Clock3,
  Database,
  Gauge,
  Layers3,
  RefreshCw,
  Server,
  ShieldCheck,
  WalletCards,
} from 'lucide-react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { dashboardApi } from '@/api/dashboard'
import { useAuthStore } from '@/stores/auth-store'
import { isAdminIdentity } from '@/lib/authz'

const numberFormatter = new Intl.NumberFormat('en-US')
const compactFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
})
const currencyFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 2,
})

function formatNumber(value?: number) {
  return numberFormatter.format(value ?? 0)
}

function formatCompact(value?: number) {
  return compactFormatter.format(value ?? 0)
}

function formatRate(value?: number) {
  return `${((value ?? 0) * 100).toFixed(1)}%`
}

function formatMs(value?: number) {
  return `${(value ?? 0).toFixed(1)} ms`
}

function formatBytes(value?: number) {
  const safe = value ?? 0
  if (safe >= 1024 * 1024) return `${(safe / 1024 / 1024).toFixed(1)} MiB`
  if (safe >= 1024) return `${(safe / 1024).toFixed(1)} KiB`
  return `${safe.toFixed(0)} B`
}

function formatBucket(bucket: string) {
  if (!bucket) return '-'
  const date = new Date(bucket)
  if (Number.isNaN(date.getTime())) return bucket
  return `${date.getMonth() + 1}/${date.getDate()}`
}

function MetricTile({
  label,
  value,
  helper,
  icon: Icon,
}: {
  label: string
  value: string
  helper?: string
  icon: ComponentType<{ className?: string }>
}) {
  return (
    <div className="bg-card min-h-28 rounded-xl border p-4 shadow-sm">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-muted-foreground text-xs font-medium">
            {label}
          </div>
          <div className="mt-2 truncate text-2xl font-semibold">{value}</div>
          {helper && (
            <div className="text-muted-foreground mt-1 truncate text-xs">
              {helper}
            </div>
          )}
        </div>
        <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
          <Icon className="h-4 w-4" />
        </div>
      </div>
    </div>
  )
}

export function DashboardPage() {
  const token = useAuthStore((state) => state.token)
  const identity = useAuthStore((state) => state.identity)
  const isAdmin = isAdminIdentity(identity)

  const {
    data,
    isLoading,
    refetch: refetchDashboard,
    isFetching: isFetchingDashboard,
  } = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => dashboardApi.getSummary(),
    enabled: !!token,
  })
  const {
    data: usageSummary,
    isLoading: isUsageLoading,
    refetch: refetchUsage,
    isFetching: isFetchingUsage,
  } = useQuery({
    queryKey: ['usage-summary', 30],
    queryFn: () => dashboardApi.getUsageSummary({ days: 30 }),
    enabled: !!token,
  })
  const {
    data: usageTrend,
    refetch: refetchTrend,
    isFetching: isFetchingTrend,
  } = useQuery({
    queryKey: ['usage-trend', 'day', 14],
    queryFn: () =>
      dashboardApi.getUsageTrend({ days: 30, period: 'day', limit: 14 }),
    enabled: !!token,
  })
  const {
    data: usageBreakdown,
    refetch: refetchBreakdown,
    isFetching: isFetchingBreakdown,
  } = useQuery({
    queryKey: ['usage-breakdown', 'provider', 30],
    queryFn: () =>
      dashboardApi.getUsageBreakdown({ days: 30, dimension: 'provider' }),
    enabled: !!token,
  })
  const {
    data: budgets,
    refetch: refetchBudgets,
    isFetching: isFetchingBudgets,
  } = useQuery({
    queryKey: ['budgets'],
    queryFn: () => dashboardApi.getBudgets(),
    enabled: !!token && isAdmin,
  })
  const {
    data: cacheData,
    isLoading: isCacheLoading,
    refetch: refetchCache,
    isFetching: isFetchingCache,
  } = useQuery({
    queryKey: ['cache-summary'],
    queryFn: () => dashboardApi.getCacheSummary(),
    enabled: !!token && isAdmin,
  })

  const usage = usageSummary?.summary
  const cacheTotals = cacheData?.totals
  const exhaustedBudgets =
    budgets?.filter((budget) => budget.is_exhausted).length ?? 0
  const healthyBudgets = (budgets?.length ?? 0) - exhaustedBudgets

  const trendRows = useMemo(
    () =>
      (usageTrend?.rows ?? []).map((row) => ({
        bucket: formatBucket(row.bucket),
        requests: row.total_requests,
        tokens: row.total_tokens,
      })),
    [usageTrend]
  )

  const breakdownRows = useMemo(
    () =>
      (usageBreakdown?.rows ?? []).slice(0, 6).map((row) => ({
        name: row.dimension || '-',
        requests: row.total_requests,
        tokens: row.total_tokens,
        cost: row.total_cost_usd,
      })),
    [usageBreakdown]
  )

  const handleRefresh = () => {
    refetchDashboard()
    refetchUsage()
    refetchTrend()
    refetchBreakdown()
    if (isAdmin) {
      refetchBudgets()
      refetchCache()
    }
  }

  const isRefreshing =
    isFetchingDashboard ||
    isFetchingUsage ||
    isFetchingTrend ||
    isFetchingBreakdown ||
    (isAdmin && (isFetchingBudgets || isFetchingCache))

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Dashboard</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            请求、用量、预算和缓存状态的 30 天概览。
          </p>
        </div>
        <Button
          variant="outline"
          onClick={handleRefresh}
          disabled={isRefreshing}
          className="w-full sm:w-auto"
        >
          <RefreshCw
            className={`mr-2 h-4 w-4 ${isRefreshing ? 'animate-spin' : ''}`}
          />
          {isRefreshing ? '刷新中...' : '刷新'}
        </Button>
      </div>

      <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
        <MetricTile
          label="总请求数"
          value={isLoading ? '-' : formatNumber(data?.totalRequests)}
          helper="全量请求"
          icon={Activity}
        />
        <MetricTile
          label="成功率"
          value={isLoading ? '-' : formatRate(data?.successRate)}
          helper={`${formatNumber(usage?.success_requests)} success / ${formatNumber(usage?.failed_requests)} failed`}
          icon={ShieldCheck}
        />
        <MetricTile
          label="平均延迟"
          value={isLoading ? '-' : formatMs(data?.avgLatencyMs)}
          helper="请求平均耗时"
          icon={Clock3}
        />
        <MetricTile
          label="活跃 Provider"
          value={isLoading ? '-' : formatNumber(data?.activeProviders)}
          helper="健康可路由节点"
          icon={Server}
        />
        <MetricTile
          label="Token"
          value={isUsageLoading ? '-' : formatCompact(usage?.total_tokens)}
          helper="近 30 天"
          icon={Gauge}
        />
        <MetricTile
          label="成本"
          value={
            isUsageLoading
              ? '-'
              : currencyFormatter.format(usage?.total_cost_usd ?? 0)
          }
          helper="近 30 天"
          icon={WalletCards}
        />
      </section>

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(320px,1fr)]">
        <div className="bg-card rounded-xl border p-4 shadow-sm">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="font-semibold">用量趋势</h2>
              <p className="text-muted-foreground mt-1 text-sm">
                最近 14 个时间桶的请求与 token。
              </p>
            </div>
            <Badge variant="outline">{usageTrend?.period ?? 'day'}</Badge>
          </div>
          <div className="mt-4 h-72">
            {trendRows.length === 0 ? (
              <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
                暂无趋势数据
              </div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <LineChart
                  data={trendRows}
                  margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
                >
                  <CartesianGrid
                    stroke="var(--border)"
                    strokeDasharray="3 3"
                    vertical={false}
                  />
                  <XAxis
                    dataKey="bucket"
                    tickLine={false}
                    axisLine={false}
                    tick={{
                      fontSize: 12,
                      fill: 'var(--muted-foreground)',
                    }}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    tick={{
                      fontSize: 12,
                      fill: 'var(--muted-foreground)',
                    }}
                    width={42}
                  />
                  <Tooltip />
                  <Line
                    type="monotone"
                    dataKey="requests"
                    name="Requests"
                    stroke="var(--primary)"
                    strokeWidth={2}
                    dot={false}
                  />
                  <Line
                    type="monotone"
                    dataKey="tokens"
                    name="Tokens"
                    stroke="var(--chart-3)"
                    strokeWidth={2}
                    dot={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            )}
          </div>
        </div>

        <div className="bg-card rounded-xl border p-4 shadow-sm">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="font-semibold">Provider 分布</h2>
              <p className="text-muted-foreground mt-1 text-sm">
                按 provider 聚合近 30 天请求。
              </p>
            </div>
            <BarChart3 className="text-muted-foreground h-4 w-4" />
          </div>
          <div className="mt-4 h-72">
            {breakdownRows.length === 0 ? (
              <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
                暂无分布数据
              </div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={breakdownRows}
                  layout="vertical"
                  margin={{ top: 8, right: 8, left: 8, bottom: 0 }}
                >
                  <CartesianGrid
                    stroke="var(--border)"
                    strokeDasharray="3 3"
                    horizontal={false}
                  />
                  <XAxis
                    type="number"
                    tickLine={false}
                    axisLine={false}
                    tick={{
                      fontSize: 12,
                      fill: 'var(--muted-foreground)',
                    }}
                  />
                  <YAxis
                    dataKey="name"
                    type="category"
                    tickLine={false}
                    axisLine={false}
                    tick={{
                      fontSize: 12,
                      fill: 'var(--muted-foreground)',
                    }}
                    width={84}
                  />
                  <Tooltip />
                  <Bar
                    dataKey="requests"
                    name="Requests"
                    fill="var(--primary)"
                    radius={[0, 4, 4, 0]}
                  />
                </BarChart>
              </ResponsiveContainer>
            )}
          </div>
        </div>
      </section>

      {isAdmin && (
        <section className="grid gap-6 xl:grid-cols-2">
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="font-semibold">预算状态</h2>
                <p className="text-muted-foreground mt-1 text-sm">
                  {healthyBudgets} healthy / {budgets?.length ?? 0} total
                </p>
              </div>
              <Layers3 className="text-muted-foreground h-4 w-4" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {budgets?.length === 0 && (
                <div className="text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm sm:col-span-2">
                  暂无预算记录
                </div>
              )}
              {budgets?.slice(0, 6).map((budget) => {
                const utilization = Math.max(
                  0,
                  Math.min(1, budget.utilization ?? 0)
                )
                return (
                  <div
                    key={`${budget.scope}-${budget.id}`}
                    className="bg-background rounded-lg border p-3"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">
                          {budget.scope}
                        </div>
                        <div className="text-muted-foreground truncate font-mono text-xs">
                          {budget.id}
                        </div>
                      </div>
                      <Badge
                        variant={
                          budget.is_exhausted ? 'destructive' : 'outline'
                        }
                      >
                        {formatRate(utilization)}
                      </Badge>
                    </div>
                    <div className="bg-muted mt-3 h-2 overflow-hidden rounded-full">
                      <div
                        className={`h-full rounded-full ${
                          budget.is_exhausted ? 'bg-destructive' : 'bg-primary'
                        }`}
                        style={{ width: `${utilization * 100}%` }}
                      />
                    </div>
                    <div className="text-muted-foreground mt-2 text-xs">
                      {currencyFormatter.format(budget.spent_usd ?? 0)} /{' '}
                      {budget.budget_usd > 0
                        ? currencyFormatter.format(budget.budget_usd)
                        : 'unlimited'}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="font-semibold">缓存概览</h2>
                <p className="text-muted-foreground mt-1 text-sm">
                  {cacheData?.enabled ? '缓存已启用' : '缓存未启用'}
                </p>
              </div>
              <Database className="text-muted-foreground h-4 w-4" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <MetricTile
                label="命中率"
                value={isCacheLoading ? '-' : formatRate(cacheTotals?.hit_rate)}
                helper={`${formatNumber(cacheTotals?.lookups.hit)} hit / ${formatNumber(cacheTotals?.lookups.miss)} miss`}
                icon={Database}
              />
              <MetricTile
                label="读取延迟"
                value={
                  isCacheLoading ? '-' : formatMs(cacheTotals?.lookup_avg_ms)
                }
                helper="平均 lookup"
                icon={Clock3}
              />
              <MetricTile
                label="写入总数"
                value={
                  isCacheLoading ? '-' : formatNumber(cacheTotals?.writes.total)
                }
                helper={`${formatNumber(cacheTotals?.writes.success)} success`}
                icon={Activity}
              />
              <MetricTile
                label="平均体积"
                value={
                  isCacheLoading
                    ? '-'
                    : formatBytes(cacheTotals?.value_avg_bytes)
                }
                helper="平均 value"
                icon={Gauge}
              />
            </div>
          </div>
        </section>
      )}

      <details className="bg-card rounded-xl border p-4 shadow-sm">
        <summary className="cursor-pointer text-sm font-medium">
          原始数据
        </summary>
        <pre className="bg-muted mt-3 overflow-auto rounded-lg p-4 text-xs">
          {JSON.stringify(
            {
              dashboard: data,
              usage: usageSummary,
              trend: usageTrend,
              breakdown: usageBreakdown,
              budgets,
              cache: cacheData,
            },
            null,
            2
          )}
        </pre>
      </details>
    </div>
  )
}
