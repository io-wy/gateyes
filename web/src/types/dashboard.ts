export interface DashboardSummary {
  totalRequests: number
  successRate: number
  avgLatencyMs: number
  activeProviders: number
  healthyBudgets: number
  totalBudgets: number
}

export interface CacheLookupSummary {
  hit: number
  miss: number
  error: number
  skip: number
  total: number
}

export interface CacheWriteSummary {
  success: number
  error: number
  total: number
}

export interface CacheLayerSummary {
  layer: string
  lookups: CacheLookupSummary
  writes: CacheWriteSummary
  hit_rate: number
  lookup_avg_ms: number
  value_avg_bytes: number
}

export interface CacheSummary {
  enabled: boolean
  layers: CacheLayerSummary[]
  totals: CacheLayerSummary
}

export interface UsageFilterResponse {
  tenant_id: string
  project_id: string
  user_id: string
  api_key_id: string
  provider: string
  model: string
  start_time?: string
  end_time?: string
}

export interface UsageStats {
  total_requests: number
  success_requests: number
  failed_requests: number
  total_tokens: number
  total_cost_usd: number
  avg_latency_ms: number
}

export interface UsageSummaryResponse {
  filter: UsageFilterResponse
  summary: UsageStats
}

export interface UsageBreakdownRow {
  dimension: string
  total_requests: number
  success_requests: number
  failed_requests: number
  total_tokens: number
  total_cost_usd: number
  avg_latency_ms: number
}

export interface UsageBreakdownResponse {
  filter: UsageFilterResponse
  dimension: string
  rows: UsageBreakdownRow[]
}

export interface UsageTimeBucket {
  bucket: string
  total_requests: number
  success_requests: number
  failed_requests: number
  total_tokens: number
  total_cost_usd: number
  avg_latency_ms: number
}

export interface UsageTrendResponse {
  filter: UsageFilterResponse
  period: string
  rows: UsageTimeBucket[]
}

export interface BudgetStatus {
  scope: string
  id: string
  budget_usd: number
  spent_usd: number
  policy: string
  utilization: number
  is_exhausted: boolean
}
