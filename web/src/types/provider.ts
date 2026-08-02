export interface Provider {
  name: string
  type: string
  vendor: string
  model: string
  base_url: string
  endpoint: string
  enabled: boolean
  drain: boolean
  health_status: string
  routing_weight: number
  supports_chat: boolean
  supports_responses: boolean
  supports_messages: boolean
  supports_stream: boolean
  supports_tools: boolean
  supports_images: boolean
  supports_structured_output: boolean
  supports_long_context: boolean
  supports_embeddings: boolean
  created_at?: string
  updated_at?: string
  // runtime config
  timeout?: number
  max_tokens?: number
  price_input?: number
  price_output?: number
  headers?: Record<string, string>
  extra_body?: Record<string, unknown>
  labels?: Record<string, string>
  has_api_key?: boolean
  // usage stats (from list endpoint)
  status?: string
  current_load?: number
  total_requests?: number
  success_requests?: number
  failed_requests?: number
  total_tokens?: number
  total_cost_usd?: number
  avg_latency_ms?: number
  error_rate?: number
}

export interface ProviderStats {
  name: string
  total_requests: number
  success_requests: number
  failed_requests: number
  avg_latency_ms: number
}

export interface CreateProviderRequest {
  name: string
  type: string
  vendor: string
  model: string
  base_url: string
  endpoint: string
  api_key: string
  routing_weight: number
  price_input: number
  price_output: number
  max_tokens: number
  timeout: number
  enabled: boolean
  headers?: Record<string, string>
  extra_body?: Record<string, unknown>
  labels?: Record<string, string>
  supports_chat?: boolean
  supports_responses?: boolean
  supports_messages?: boolean
  supports_stream?: boolean
  supports_tools?: boolean
  supports_images?: boolean
  supports_structured_output?: boolean
  supports_long_context?: boolean
  supports_embeddings?: boolean
}

export type UpdateProviderRequest = Partial<CreateProviderRequest>
