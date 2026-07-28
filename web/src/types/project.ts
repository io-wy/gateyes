export interface Project {
  id: string
  tenant_id: string
  tenant_slug?: string
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

export interface CreateProjectRequest {
  tenant_id?: string
  slug: string
  name: string
  budget_usd?: number
  policy?: Record<string, unknown>
  status?: string
}

export interface UpdateProjectRequest {
  name?: string
  status?: string
  budget_usd?: number
  budget_policy?: string
  policy?: Record<string, unknown>
}

export interface ProjectUsage {
  project: Project
  summary: {
    total_requests: number
    total_tokens: number
    total_cost_usd: number
  }
  trend: {
    date: string
    requests: number
    tokens: number
    cost_usd: number
  }[]
}
