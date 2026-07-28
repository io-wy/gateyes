import client from './client'
import type {
  DashboardSummary,
  CacheSummary,
  UsageSummary,
  UsageBreakdown,
  UsageTrend,
  Budget,
} from '@/types/dashboard'

export const dashboardApi = {
  getSummary: () => client.get<DashboardSummary>('/dashboard'),
  getCacheSummary: () => client.get<CacheSummary>('/cache/summary'),
  getUsageSummary: (params?: { period?: string }) =>
    client.get<UsageSummary>('/usage/summary', { params }),
  getUsageBreakdown: (params?: {
    start_time?: string
    end_time?: string
    group_by?: string
  }) => client.get<UsageBreakdown[]>('/usage/breakdown', { params }),
  getUsageTrend: (params?: {
    start_time?: string
    end_time?: string
    interval?: string
  }) => client.get<UsageTrend[]>('/usage/trend', { params }),
  getBudgets: () => client.get<Budget[]>('/budgets'),
}
