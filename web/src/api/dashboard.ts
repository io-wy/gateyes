import client from './client'
import type {
  DashboardSummary,
  CacheSummary,
  UsageSummaryResponse,
  UsageBreakdownResponse,
  UsageTrendResponse,
  BudgetStatus,
} from '@/types/dashboard'

export const dashboardApi = {
  getSummary: () => client.get<DashboardSummary>('/dashboard'),
  getCacheSummary: () => client.get<CacheSummary>('/cache/summary'),
  getUsageSummary: (params?: {
    days?: number
    provider?: string
    model?: string
    project_id?: string
    user_id?: string
    api_key_id?: string
    start_time?: string
    end_time?: string
  }) => client.get<UsageSummaryResponse>('/usage/summary', { params }),
  getUsageBreakdown: (params?: {
    days?: number
    provider?: string
    model?: string
    project_id?: string
    user_id?: string
    api_key_id?: string
    start_time?: string
    end_time?: string
    dimension?: string
  }) => client.get<UsageBreakdownResponse>('/usage/breakdown', { params }),
  getUsageTrend: (params?: {
    days?: number
    provider?: string
    model?: string
    project_id?: string
    user_id?: string
    api_key_id?: string
    start_time?: string
    end_time?: string
    period?: string
    limit?: number
  }) => client.get<UsageTrendResponse>('/usage/trend', { params }),
  getBudgets: (params?: { project_id?: string; api_key_id?: string }) =>
    client.get<BudgetStatus[]>('/budgets', { params }),
}
