export interface Tenant {
  id: string
  slug: string
  name: string
  status: string
  budget_usd: number
  spent_usd: number
  budget_policy?: string
  policy?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export interface TenantDetail {
  tenant: Tenant
  providers: string[]
}

export interface CreateTenantRequest {
  id?: string
  slug: string
  name: string
  budget_usd?: number
  policy?: Record<string, unknown>
  status?: string
}

export interface UpdateTenantRequest {
  name?: string
  status?: string
  budget_usd?: number
  budget_policy?: string
  policy?: Record<string, unknown>
}

export interface ReplaceTenantProvidersRequest {
  providers: string[]
}
