import client from './client'
import type { ListData } from '@/types/api'

export interface AuditLog {
  id: string
  tenant_id: string
  actor_user_id?: string
  actor_api_key_id?: string
  actor_role?: string
  action: string
  resource_type: string
  resource_id?: string
  request_id?: string
  ip_address?: string
  payload?: Record<string, unknown>
  created_at?: string
}

export const auditApi = {
  list: (params?: {
    action?: string
    resource_type?: string
    resource_id?: string
    actor_user_id?: string
    start_time?: string
    end_time?: string
    limit?: number
  }) => client.get<ListData<AuditLog>>('/audit', { params }),
}
