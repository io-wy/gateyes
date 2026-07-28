export interface User {
  id: string
  tenant_id: string
  tenant_slug?: string
  api_key: string
  project_id?: string
  name: string
  email?: string
  role: string
  quota: number
  used: number
  remaining: number
  qps: number
  key_budget_usd: number
  key_spent_usd: number
  models?: string[]
  status: string
  created_at?: string
  updated_at?: string
  // 仅在创建时返回一次
  api_secret?: string
  token?: string
}

export interface CreateUserRequest {
  tenant_id?: string
  project_id?: string
  name: string
  email?: string
  role?: string
  quota?: number
  qps?: number
  key_budget_usd?: number
  models?: string[]
  status?: string
}

export interface UpdateUserRequest {
  role?: string
  quota?: number
  qps?: number
  project_id?: string
  key_budget_usd?: number
  models?: string[]
  status?: string
}

export interface UserUsage {
  user: {
    id: string
    name: string
    quota: number
    used: number
    remaining: number
    usage_percent: number
    project_id?: string
    key_budget_usd: number
    key_spent_usd: number
  }
  trend: {
    date: string
    requests: number
    tokens: number
    cost_usd: number
  }[]
}
