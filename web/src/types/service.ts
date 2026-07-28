export interface GuardrailRuleSet {
  allow_models?: string[]
  block_models?: string[]
  block_terms?: string[]
  block_regex?: string[]
  redact_terms?: string[]
  max_input_chars?: number
  max_output_chars?: number
}

export interface ServicePolicyConfig {
  enabled?: boolean
  request?: GuardrailRuleSet
  response?: GuardrailRuleSet
}

export interface PromptTemplateVariable {
  name: string
  default?: string
  required?: boolean
  description?: string
}

export interface PromptTemplateConfig {
  system_template?: string
  user_template?: string
  variables?: PromptTemplateVariable[]
}

export interface ServiceConfig {
  surfaces?: string[]
  prompt_template?: PromptTemplateConfig
  policy?: ServicePolicyConfig
  metadata?: Record<string, unknown>
}

export interface Service {
  id: string
  tenant_id: string
  project_id?: string
  project_slug?: string
  name: string
  request_prefix: string
  description?: string
  default_provider?: string
  default_model?: string
  publish_status: string
  published_version_id?: string
  staged_version_id?: string
  enabled: boolean
  config?: ServiceConfig
  created_at?: string
  updated_at?: string
}

export interface ServiceVersion {
  id: string
  service_id: string
  tenant_id: string
  version: number
  status: string
  snapshot?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export interface ServiceDetail {
  service: Service
  versions: ServiceVersion[]
}

export interface CreateServiceRequest {
  tenant_id?: string
  project_id?: string
  name: string
  request_prefix: string
  description?: string
  default_provider?: string
  default_model?: string
  enabled?: boolean
  config?: ServiceConfig
  auto_publish?: boolean
}

export interface UpdateServiceRequest {
  project_id?: string
  name?: string
  request_prefix?: string
  description?: string
  default_provider?: string
  default_model?: string
  enabled?: boolean
  config?: ServiceConfig
}

export interface ServiceSubscription {
  id: string
  tenant_id: string
  service_id: string
  project_id?: string
  project_slug?: string
  consumer_name: string
  consumer_email?: string
  consumer_user_id?: string
  status: string
  requested_budget_usd: number
  requested_rate_limit_qps: number
  allowed_surfaces?: string[]
  approved_api_key_id?: string
  approved_user_id?: string
  review_note?: string
  approved_at?: string
  created_at?: string
  updated_at?: string
}

export interface CreateServiceSubscriptionRequest {
  project_id?: string
  consumer_name: string
  consumer_email?: string
  consumer_user_id?: string
  requested_budget_usd?: number
  requested_rate_limit_qps?: number
  allowed_surfaces?: string[]
}

export interface ReviewServiceSubscriptionRequest {
  decision: string
  review_note?: string
}
