import type { CreateProviderRequest } from '@/types/provider'

export type ProviderCapabilityKey =
  | 'supports_chat'
  | 'supports_responses'
  | 'supports_messages'
  | 'supports_stream'
  | 'supports_tools'
  | 'supports_images'
  | 'supports_structured_output'
  | 'supports_long_context'
  | 'supports_embeddings'

export interface ProviderCatalogPreset {
  id: string
  label: string
  description: string
  type: string
  vendor: string
  base_url: string
  endpoint: string
  model: string
  timeout: number
  max_tokens: number
  routing_weight: number
  headers?: Record<string, string>
  extra_body?: Record<string, unknown>
  capabilities: Partial<Record<ProviderCapabilityKey, boolean>>
}

const BASE_CAPABILITIES = {
  supports_chat: true,
  supports_responses: true,
  supports_messages: true,
  supports_stream: true,
  supports_tools: true,
  supports_images: true,
  supports_structured_output: true,
  supports_long_context: true,
  supports_embeddings: false,
} satisfies Record<ProviderCapabilityKey, boolean>

export const providerCatalog: ProviderCatalogPreset[] = [
  {
    id: 'openai-chat',
    label: 'OpenAI Chat',
    description: 'OpenAI / OpenAI-compatible chat completions.',
    type: 'openai',
    vendor: 'openai',
    base_url: 'https://api.openai.com/v1',
    endpoint: 'chat',
    model: 'gpt-4.1-mini',
    timeout: 60,
    max_tokens: 128000,
    routing_weight: 1,
    capabilities: {
      ...BASE_CAPABILITIES,
    },
  },
  {
    id: 'openai-responses',
    label: 'OpenAI Responses',
    description: 'OpenAI Responses API with streaming and tool support.',
    type: 'openai',
    vendor: 'openai',
    base_url: 'https://api.openai.com/v1',
    endpoint: 'responses',
    model: 'gpt-4.1',
    timeout: 60,
    max_tokens: 128000,
    routing_weight: 1,
    capabilities: {
      ...BASE_CAPABILITIES,
    },
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    description: 'Claude Messages API.',
    type: 'anthropic',
    vendor: 'anthropic',
    base_url: 'https://api.anthropic.com/v1',
    endpoint: '',
    model: 'claude-sonnet-4-20250514',
    timeout: 60,
    max_tokens: 200000,
    routing_weight: 1,
    headers: {
      'anthropic-version': '2023-06-01',
    },
    capabilities: {
      supports_chat: true,
      supports_responses: true,
      supports_messages: true,
      supports_stream: true,
      supports_tools: true,
      supports_images: true,
      supports_structured_output: false,
      supports_long_context: true,
      supports_embeddings: false,
    },
  },
  {
    id: 'deepseek',
    label: 'DeepSeek',
    description: 'OpenAI-compatible DeepSeek endpoint.',
    type: 'openai',
    vendor: 'deepseek',
    base_url: 'https://api.deepseek.com/v1',
    endpoint: 'chat',
    model: 'deepseek-chat',
    timeout: 60,
    max_tokens: 64000,
    routing_weight: 1,
    capabilities: {
      supports_chat: true,
      supports_responses: true,
      supports_messages: false,
      supports_stream: true,
      supports_tools: true,
      supports_images: false,
      supports_structured_output: true,
      supports_long_context: true,
      supports_embeddings: false,
    },
  },
  {
    id: 'vllm',
    label: 'vLLM / Local',
    description: 'Local OpenAI-compatible deployment.',
    type: 'openai',
    vendor: 'vllm',
    base_url: 'http://localhost:8000/v1',
    endpoint: 'chat',
    model: 'local-model',
    timeout: 120,
    max_tokens: 32000,
    routing_weight: 1,
    capabilities: {
      supports_chat: true,
      supports_responses: true,
      supports_messages: false,
      supports_stream: true,
      supports_tools: true,
      supports_images: false,
      supports_structured_output: true,
      supports_long_context: true,
      supports_embeddings: false,
    },
  },
]

export function findProviderCatalogPreset(
  provider: Pick<CreateProviderRequest, 'type' | 'vendor' | 'base_url' | 'endpoint'>
) {
  const normalized = {
    type: provider.type.trim().toLowerCase(),
    vendor: provider.vendor.trim().toLowerCase(),
    base_url: provider.base_url.trim().toLowerCase(),
    endpoint: provider.endpoint.trim().toLowerCase(),
  }

  return (
    providerCatalog.find((preset) => {
      if (preset.type.toLowerCase() !== normalized.type) return false
      if (preset.vendor.toLowerCase() !== normalized.vendor) return false
      if (normalized.base_url && preset.base_url.toLowerCase() !== normalized.base_url) {
        return false
      }
      if (normalized.endpoint && preset.endpoint.toLowerCase() !== normalized.endpoint) {
        return false
      }
      return true
    }) ?? null
  )
}

export function applyProviderCatalogPreset(
  preset: ProviderCatalogPreset
): Partial<CreateProviderRequest> {
  return {
    type: preset.type,
    vendor: preset.vendor,
    base_url: preset.base_url,
    endpoint: preset.endpoint,
    model: preset.model,
    timeout: preset.timeout,
    routing_weight: preset.routing_weight,
    max_tokens: preset.max_tokens,
    headers: preset.headers ?? {},
    extra_body: preset.extra_body ?? {},
    supports_chat: preset.capabilities.supports_chat ?? false,
    supports_responses: preset.capabilities.supports_responses ?? false,
    supports_messages: preset.capabilities.supports_messages ?? false,
    supports_stream: preset.capabilities.supports_stream ?? false,
    supports_tools: preset.capabilities.supports_tools ?? false,
    supports_images: preset.capabilities.supports_images ?? false,
    supports_structured_output:
      preset.capabilities.supports_structured_output ?? false,
    supports_long_context: preset.capabilities.supports_long_context ?? false,
    supports_embeddings: preset.capabilities.supports_embeddings ?? false,
  }
}
