export interface VirtualKey {
  id: string
  tenant_id: string
  project_id?: string
  user_id: string
  api_key_id: string
  name: string
  key: string
  status: string
  budget_usd: number
  spent_usd: number
  budget_policy?: string
  rate_limit_qps: number
  allowed_models?: string[]
  allowed_providers?: string[]
  metadata?: Record<string, unknown>
  callback_url?: string
  created_at?: string
  updated_at?: string
  expires_at?: string
  revoked_at?: string
  // 仅在创建时返回一次
  secret?: string
  token?: string
}

export interface CreateVirtualKeyRequest {
  user_id: string
  api_key_id: string
  project_id?: string
  name?: string
  budget_usd?: number
  budget_policy?: string
  rate_limit_qps?: number
  allowed_models?: string[]
  allowed_providers?: string[]
  callback_url?: string
  status?: string
}

export interface UpdateVirtualKeyRequest {
  name?: string
  status?: string
  budget_usd?: number
  budget_policy?: string
  rate_limit_qps?: number
  allowed_models?: string[]
  allowed_providers?: string[]
  callback_url?: string
}
