import client from './client'
import type { ListData } from '@/types/api'

export interface ResponseRecord {
  id: string
  tenant_id: string
  project_id?: string
  user_id?: string
  api_key_id?: string
  provider_name: string
  model: string
  status: string
  created_at?: string
  updated_at?: string
}

export interface ResponseDetail {
  id: string
  tenant_id: string
  project_id?: string
  user_id?: string
  api_key_id?: string
  provider_name: string
  model: string
  status: string
  request_body: unknown
  response_body: unknown
  route_trace: unknown
  created_at?: string
  updated_at?: string
}

export interface ResponseTrace {
  response_id: string
  trace: Record<string, unknown>
}

export const responsesApi = {
  list: (params?: {
    provider_name?: string
    model?: string
    status?: string
    project_id?: string
    api_key_id?: string
    user_id?: string
    q?: string
    start_time?: string
    end_time?: string
    limit?: number
    offset?: number
  }) => client.get<ListData<ResponseRecord>>('/responses', { params }),
  detail: (id: string) => client.get<ResponseDetail>(`/responses/${id}`),
  trace: (id: string) => client.get<ResponseTrace>(`/responses/${id}/trace`),
}
