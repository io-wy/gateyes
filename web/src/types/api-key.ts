export interface APIKey {
  id: string
  tenant_id: string
  tenant_slug?: string
  user_id: string
  user_name?: string
  user_email?: string
  project_id?: string
  project_slug?: string
  api_key: string
  status: string
  budget_usd: number
  spent_usd: number
  budget_policy?: string
  rate_limit_qps: number
  allowed_models?: string[]
  allowed_providers?: string[]
  allowed_services?: string[]
  created_at?: string
  updated_at?: string
  last_used_at?: string
  revoked_at?: string
  // 仅在创建/轮换时返回一次
  api_secret?: string
  token?: string
}

export interface CreateAPIKeyRequest {
  user_id: string
  project_id?: string
  budget_usd?: number
  rate_limit_qps?: number
  allowed_models?: string[]
  allowed_providers?: string[]
  allowed_services?: string[]
  status?: string
}

export interface UpdateAPIKeyRequest {
  project_id?: string
  status?: string
  budget_usd?: number
  budget_policy?: string
  rate_limit_qps?: number
  allowed_models?: string[]
  allowed_providers?: string[]
  allowed_services?: string[]
}
