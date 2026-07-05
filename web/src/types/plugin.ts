export interface Plugin {
  id: string
  tenant_id: string
  name: string
  type: 'wasm' | 'grpc'
  description?: string
  author?: string
  phases: string[]
  file_path?: string
  address?: string
  timeout_ms?: number
  memory_pages?: number
  enabled: boolean
  source: string
  config?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export interface CreatePluginRequest {
  name: string
  type: 'wasm' | 'grpc'
  description?: string
  author?: string
  phases?: string[]
  address?: string
  timeout_ms?: number
  memory_pages?: number
  enabled?: boolean
  source?: string
  config?: Record<string, unknown>
}

export interface UpdatePluginRequest {
  name?: string
  description?: string
  author?: string
  phases?: string[]
  address?: string
  timeout_ms?: number
  memory_pages?: number
  enabled?: boolean
  source?: string
  config?: Record<string, unknown>
}
