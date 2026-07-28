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

export interface UsageSummary {
  totalTokens: number
  totalRequests: number
  totalCost?: number
  period: string
}

export interface UsageBreakdown {
  provider: string
  model: string
  requests: number
  tokens: number
}

export interface UsageTrend {
  timestamp: string
  requests: number
  tokens: number
}

export interface Budget {
  id: string
  name: string
  limit: number
  used: number
  currency: string
  status: string
}
