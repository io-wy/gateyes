import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { providersApi } from '@/api/providers'
import type {
  Service,
  CreateServiceRequest,
  UpdateServiceRequest,
  ServiceConfig,
  PromptTemplateVariable,
  GuardrailRuleSet,
} from '@/types/service'

const SURFACES = ['responses', 'chat', 'messages', 'invoke'] as const

const TABS = [
  { id: 'basic', label: '基本信息' },
  { id: 'provider', label: 'Provider & Model' },
  { id: 'surfaces', label: 'Surfaces' },
  { id: 'prompt', label: 'Prompt Template' },
  { id: 'policy', label: 'Policy' },
] as const

const defaultConfig: ServiceConfig = {
  surfaces: [],
  prompt_template: {
    system_template: '',
    user_template: '',
    variables: [],
  },
  policy: {
    enabled: false,
    request: {
      allow_models: [],
      block_models: [],
      block_terms: [],
      block_regex: [],
      redact_terms: [],
      max_input_chars: 0,
    },
    response: {
      allow_models: [],
      block_models: [],
      block_terms: [],
      block_regex: [],
      redact_terms: [],
      max_output_chars: 0,
    },
  },
  metadata: {},
}

function arrayToText(arr?: string[]): string {
  return (arr || []).join('\n')
}

function textToArray(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
}

function ensureConfig(config?: ServiceConfig | null): ServiceConfig {
  if (!config) return defaultConfig
  return {
    surfaces: config.surfaces || [],
    prompt_template: {
      system_template: config.prompt_template?.system_template || '',
      user_template: config.prompt_template?.user_template || '',
      variables: config.prompt_template?.variables || [],
    },
    policy: {
      enabled: config.policy?.enabled || false,
      request: {
        allow_models: config.policy?.request?.allow_models || [],
        block_models: config.policy?.request?.block_models || [],
        block_terms: config.policy?.request?.block_terms || [],
        block_regex: config.policy?.request?.block_regex || [],
        redact_terms: config.policy?.request?.redact_terms || [],
        max_input_chars: config.policy?.request?.max_input_chars || 0,
      },
      response: {
        allow_models: config.policy?.response?.allow_models || [],
        block_models: config.policy?.response?.block_models || [],
        block_terms: config.policy?.response?.block_terms || [],
        block_regex: config.policy?.response?.block_regex || [],
        redact_terms: config.policy?.response?.redact_terms || [],
        max_output_chars: config.policy?.response?.max_output_chars || 0,
      },
    },
    metadata: config.metadata || {},
  }
}

interface ServiceFormState {
  name: string
  request_prefix: string
  description: string
  enabled: boolean
  tenant_id: string
  project_id: string
  default_provider: string
  default_model: string
  config: ServiceConfig
}

function buildInitialForm(service: Service | null): ServiceFormState {
  if (service) {
    return {
      name: service.name,
      request_prefix: service.request_prefix,
      description: service.description || '',
      enabled: service.enabled,
      tenant_id: service.tenant_id,
      project_id: service.project_id || '',
      default_provider: service.default_provider || '',
      default_model: service.default_model || '',
      config: ensureConfig(service.config),
    }
  }
  return {
    name: '',
    request_prefix: '',
    description: '',
    enabled: true,
    tenant_id: '',
    project_id: '',
    default_provider: '',
    default_model: '',
    config: defaultConfig,
  }
}

interface ServiceFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingService: Service | null
  onSubmit: (data: CreateServiceRequest | UpdateServiceRequest) => void
  loading?: boolean
}

export function ServiceFormDialog({
  open,
  onOpenChange,
  editingService,
  onSubmit,
  loading,
}: ServiceFormDialogProps) {
  const [activeTab, setActiveTab] = useState('basic')
  const [form, setForm] = useState<ServiceFormState>(() =>
    buildInitialForm(editingService)
  )

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => providersApi.list(),
    enabled: open,
  })

  const selectedProvider = providers?.find(
    (p) => p.name === form.default_provider
  )
  const modelOptions = selectedProvider ? [selectedProvider.model] : []

  const updateForm = (patch: Partial<ServiceFormState>) => {
    setForm((prev) => ({ ...prev, ...patch }))
  }

  const updateConfig = (patch: Partial<ServiceConfig>) => {
    setForm((prev) => ({ ...prev, config: { ...prev.config, ...patch } }))
  }

  const updatePromptTemplate = (
    patch: Partial<ServiceConfig['prompt_template']>
  ) => {
    updateConfig({
      prompt_template: { ...form.config.prompt_template, ...patch },
    })
  }

  const updatePolicy = (patch: Partial<ServiceConfig['policy']>) => {
    updateConfig({
      policy: { ...form.config.policy, ...patch },
    })
  }

  const updateRuleSet = (
    kind: 'request' | 'response',
    patch: Partial<GuardrailRuleSet>
  ) => {
    updateConfig({
      policy: {
        ...form.config.policy,
        [kind]: { ...form.config.policy?.[kind], ...patch },
      },
    })
  }

  const addVariable = () => {
    const variables = [...(form.config.prompt_template?.variables || [])]
    variables.push({ name: '', default: '', required: false, description: '' })
    updatePromptTemplate({ variables })
  }

  const updateVariable = (
    index: number,
    patch: Partial<PromptTemplateVariable>
  ) => {
    const variables = [...(form.config.prompt_template?.variables || [])]
    variables[index] = { ...variables[index], ...patch }
    updatePromptTemplate({ variables })
  }

  const removeVariable = (index: number) => {
    const variables = [...(form.config.prompt_template?.variables || [])]
    variables.splice(index, 1)
    updatePromptTemplate({ variables })
  }

  const toggleSurface = (surface: string, checked: boolean) => {
    const surfaces = new Set(form.config.surfaces || [])
    if (checked) {
      surfaces.add(surface)
    } else {
      surfaces.delete(surface)
    }
    updateConfig({ surfaces: Array.from(surfaces) })
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const payload: CreateServiceRequest | UpdateServiceRequest = {
      name: form.name,
      request_prefix: form.request_prefix,
      description: form.description,
      default_provider: form.default_provider,
      default_model: form.default_model,
      enabled: form.enabled,
      project_id: form.project_id,
      config: form.config,
    }
    if (editingService) {
      onSubmit(payload as UpdateServiceRequest)
    } else {
      onSubmit({ ...payload, tenant_id: form.tenant_id } as CreateServiceRequest)
    }
  }


  const renderBasicTab = () => (
    <div className="grid gap-4 md:grid-cols-2">
      <div className="space-y-2">
        <Label htmlFor="name">名称 *</Label>
        <Input
          id="name"
          value={form.name}
          onChange={(e) => updateForm({ name: e.target.value })}
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="request_prefix">Request Prefix *</Label>
        <Input
          id="request_prefix"
          value={form.request_prefix}
          onChange={(e) => updateForm({ request_prefix: e.target.value })}
          required
          disabled={!!editingService}
        />
      </div>
      {!editingService && (
        <>
          <div className="space-y-2">
            <Label htmlFor="tenant_id">Tenant ID（超级管理员）</Label>
            <Input
              id="tenant_id"
              value={form.tenant_id}
              onChange={(e) => updateForm({ tenant_id: e.target.value })}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="project_id">Project ID</Label>
            <Input
              id="project_id"
              value={form.project_id}
              onChange={(e) => updateForm({ project_id: e.target.value })}
            />
          </div>
        </>
      )}
      {editingService && (
        <div className="space-y-2">
          <Label htmlFor="project_id">Project ID</Label>
          <Input
            id="project_id"
            value={form.project_id}
            onChange={(e) => updateForm({ project_id: e.target.value })}
          />
        </div>
      )}
      <div className="col-span-full space-y-2">
        <Label htmlFor="description">描述</Label>
        <Textarea
          id="description"
          value={form.description}
          onChange={(e) => updateForm({ description: e.target.value })}
          rows={3}
        />
      </div>
      <div className="col-span-full flex items-center gap-2">
        <Switch
          id="enabled"
          checked={form.enabled}
          onCheckedChange={(checked) => updateForm({ enabled: checked })}
        />
        <Label htmlFor="enabled">启用</Label>
      </div>
    </div>
  )

  const renderProviderTab = () => (
    <div className="grid gap-4 md:grid-cols-2">
      <div className="space-y-2">
        <Label htmlFor="default_provider">默认 Provider</Label>
        <Select
          value={form.default_provider}
          onValueChange={(value) => {
            const v = value || ''
            const provider = providers?.find((p) => p.name === v)
            updateForm({
              default_provider: v,
              default_model: provider?.model || '',
            })
          }}
        >
          <SelectTrigger id="default_provider">
            <SelectValue placeholder="选择 Provider" />
          </SelectTrigger>
          <SelectContent>
            {providers?.map((provider) => (
              <SelectItem key={provider.name} value={provider.name}>
                {provider.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label htmlFor="default_model">默认模型</Label>
        <Select
          value={form.default_model}
          onValueChange={(value) => updateForm({ default_model: value || '' })}
        >
          <SelectTrigger id="default_model">
            <SelectValue placeholder="选择模型" />
          </SelectTrigger>
          <SelectContent>
            {modelOptions.map((model) => (
              <SelectItem key={model} value={model}>
                {model}
              </SelectItem>
            ))}
            {modelOptions.length === 0 && (
              <SelectItem value="">未选择 Provider</SelectItem>
            )}
          </SelectContent>
        </Select>
      </div>
    </div>
  )

  const renderSurfacesTab = () => (
    <div className="space-y-4">
      <Label>支持的调用方式</Label>
      <div className="grid grid-cols-2 gap-4">
        {SURFACES.map((surface) => (
          <div key={surface} className="flex items-center gap-2">
            <Checkbox
              id={`surface-${surface}`}
              checked={(form.config.surfaces || []).includes(surface)}
              onCheckedChange={(checked) =>
                toggleSurface(surface, checked as boolean)
              }
            />
            <Label htmlFor={`surface-${surface}`}>{surface}</Label>
          </div>
        ))}
      </div>
    </div>
  )

  const renderPromptTab = () => (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="system_template">System Template</Label>
        <Textarea
          id="system_template"
          value={form.config.prompt_template?.system_template || ''}
          onChange={(e) => updatePromptTemplate({ system_template: e.target.value })}
          rows={4}
          placeholder="例如：You are a helpful assistant."
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="user_template">User Template</Label>
        <Textarea
          id="user_template"
          value={form.config.prompt_template?.user_template || ''}
          onChange={(e) => updatePromptTemplate({ user_template: e.target.value })}
          rows={4}
          placeholder="例如：Say hello to {{name}}"
        />
      </div>
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label>模板变量</Label>
          <Button type="button" variant="outline" size="sm" onClick={addVariable}>
            <Plus className="mr-1 h-3 w-3" />
            添加变量
          </Button>
        </div>
        {(form.config.prompt_template?.variables || []).length === 0 && (
          <p className="text-muted-foreground text-sm">暂无变量</p>
        )}
        <div className="space-y-2">
          {(form.config.prompt_template?.variables || []).map((variable, index) => (
            <div
              key={index}
              className="grid grid-cols-12 items-end gap-2 rounded-md border p-2"
            >
              <div className="col-span-3 space-y-1">
                <Label className="text-xs">名称</Label>
                <Input
                  value={variable.name}
                  onChange={(e) =>
                    updateVariable(index, { name: e.target.value })
                  }
                  placeholder="name"
                />
              </div>
              <div className="col-span-3 space-y-1">
                <Label className="text-xs">默认值</Label>
                <Input
                  value={variable.default || ''}
                  onChange={(e) =>
                    updateVariable(index, { default: e.target.value })
                  }
                />
              </div>
              <div className="col-span-4 space-y-1">
                <Label className="text-xs">描述</Label>
                <Input
                  value={variable.description || ''}
                  onChange={(e) =>
                    updateVariable(index, { description: e.target.value })
                  }
                />
              </div>
              <div className="col-span-1 flex items-center gap-1 pb-2">
                <Checkbox
                  id={`var-required-${index}`}
                  checked={variable.required}
                  onCheckedChange={(checked) =>
                    updateVariable(index, { required: checked as boolean })
                  }
                />
                <Label
                  htmlFor={`var-required-${index}`}
                  className="text-xs"
                >
                  必填
                </Label>
              </div>
              <div className="col-span-1 pb-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={() => removeVariable(index)}
                >
                  <Trash2 className="text-destructive h-4 w-4" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )

  const renderRuleSet = (kind: 'request' | 'response') => {
    const rules = form.config.policy?.[kind] || {}
    const isRequest = kind === 'request'
    return (
      <div className="space-y-3 rounded-md border p-3">
        <h4 className="text-sm font-medium">
          {isRequest ? 'Request Guardrails' : 'Response Guardrails'}
        </h4>
        <div className="grid gap-3 md:grid-cols-2">
          <div className="space-y-1">
            <Label className="text-xs">Allow Models（每行一个）</Label>
            <Textarea
              value={arrayToText(rules.allow_models)}
              onChange={(e) =>
                updateRuleSet(kind, { allow_models: textToArray(e.target.value) })
              }
              rows={2}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">Block Models（每行一个）</Label>
            <Textarea
              value={arrayToText(rules.block_models)}
              onChange={(e) =>
                updateRuleSet(kind, { block_models: textToArray(e.target.value) })
              }
              rows={2}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">Block Terms（每行一个）</Label>
            <Textarea
              value={arrayToText(rules.block_terms)}
              onChange={(e) =>
                updateRuleSet(kind, { block_terms: textToArray(e.target.value) })
              }
              rows={2}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">Block Regex（每行一个）</Label>
            <Textarea
              value={arrayToText(rules.block_regex)}
              onChange={(e) =>
                updateRuleSet(kind, { block_regex: textToArray(e.target.value) })
              }
              rows={2}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">Redact Terms（每行一个）</Label>
            <Textarea
              value={arrayToText(rules.redact_terms)}
              onChange={(e) =>
                updateRuleSet(kind, { redact_terms: textToArray(e.target.value) })
              }
              rows={2}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">
              {isRequest ? 'Max Input Chars' : 'Max Output Chars'}
            </Label>
            <Input
              type="number"
              min={0}
              value={
                isRequest
                  ? rules.max_input_chars || 0
                  : rules.max_output_chars || 0
              }
              onChange={(e) => {
                const value = parseInt(e.target.value, 10)
                if (isRequest) {
                  updateRuleSet(kind, { max_input_chars: isNaN(value) ? 0 : value })
                } else {
                  updateRuleSet(kind, {
                    max_output_chars: isNaN(value) ? 0 : value,
                  })
                }
              }}
            />
          </div>
        </div>
      </div>
    )
  }

  const renderPolicyTab = () => (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Switch
          id="policy-enabled"
          checked={form.config.policy?.enabled || false}
          onCheckedChange={(checked) => updatePolicy({ enabled: checked })}
        />
        <Label htmlFor="policy-enabled">启用 Policy</Label>
      </div>
      {form.config.policy?.enabled && (
        <div className="space-y-4">
          {renderRuleSet('request')}
          {renderRuleSet('response')}
        </div>
      )}
    </div>
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-3xl overflow-auto">
        <DialogHeader>
          <DialogTitle>
            {editingService ? '编辑 Service' : '创建 Service'}
          </DialogTitle>
        </DialogHeader>
        <div className="flex gap-1 border-b">
          {TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              className={`px-3 py-2 text-sm transition-colors ${
                activeTab === tab.id
                  ? 'border-b-2 border-primary font-medium'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
              onClick={() => setActiveTab(tab.id)}
            >
              {tab.label}
            </button>
          ))}
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          {activeTab === 'basic' && renderBasicTab()}
          {activeTab === 'provider' && renderProviderTab()}
          {activeTab === 'surfaces' && renderSurfacesTab()}
          {activeTab === 'prompt' && renderPromptTab()}
          {activeTab === 'policy' && renderPolicyTab()}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? '保存中...' : '保存'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
