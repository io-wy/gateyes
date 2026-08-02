import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { Textarea } from '@/components/ui/textarea'
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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { Provider, CreateProviderRequest } from '@/types/provider'
import {
  applyProviderCatalogPreset,
  findProviderCatalogPreset,
  providerCatalog,
} from '@/lib/provider-catalog'

interface ProviderFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Provider | null
  onSubmit: (data: CreateProviderRequest) => void
  loading?: boolean
}

const capabilityFields = [
  { key: 'supports_chat', label: 'Chat Completions' },
  { key: 'supports_responses', label: 'Responses API' },
  { key: 'supports_messages', label: 'Anthropic Messages' },
  { key: 'supports_stream', label: 'Streaming' },
  { key: 'supports_tools', label: 'Tools' },
  { key: 'supports_images', label: 'Images' },
  { key: 'supports_structured_output', label: 'Structured Output' },
  { key: 'supports_long_context', label: 'Long Context' },
  { key: 'supports_embeddings', label: 'Embeddings' },
] as const

function buildInitialForm(initial?: Provider | null): CreateProviderRequest {
  const defaults = {
    name: '',
    type: '',
    vendor: '',
    model: '',
    base_url: '',
    endpoint: '',
    api_key: '',
    routing_weight: 1,
    price_input: 0,
    price_output: 0,
    max_tokens: 0,
    timeout: 0,
    enabled: true,
    supports_chat: false,
    supports_responses: false,
    supports_messages: false,
    supports_stream: false,
    supports_tools: false,
    supports_images: false,
    supports_structured_output: false,
    supports_long_context: false,
    supports_embeddings: false,
    labels: {},
  }

  if (!initial) return defaults

  return {
    name: initial.name,
    type: initial.type || '',
    vendor: initial.vendor || '',
    model: initial.model,
    base_url: initial.base_url || '',
    endpoint: initial.endpoint || '',
    api_key: '',
    routing_weight: initial.routing_weight ?? 1,
    price_input: initial.price_input ?? 0,
    price_output: initial.price_output ?? 0,
    max_tokens: initial.max_tokens ?? 0,
    timeout: initial.timeout ?? 0,
    enabled: initial.enabled ?? true,
    supports_chat: initial.supports_chat ?? false,
    supports_responses: initial.supports_responses ?? false,
    supports_messages: initial.supports_messages ?? false,
    supports_stream: initial.supports_stream ?? false,
    supports_tools: initial.supports_tools ?? false,
    supports_images: initial.supports_images ?? false,
    supports_structured_output: initial.supports_structured_output ?? false,
    supports_long_context: initial.supports_long_context ?? false,
    supports_embeddings: initial.supports_embeddings ?? false,
    labels: initial.labels || {},
  }
}

function buildPresetId(initial?: Provider | null) {
  if (!initial) return ''
  return (
    findProviderCatalogPreset({
      type: initial.type || '',
      vendor: initial.vendor || '',
      base_url: initial.base_url || '',
      endpoint: initial.endpoint || '',
    })?.id || ''
  )
}

export function ProviderFormDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  loading,
}: ProviderFormDialogProps) {
  const [form, setForm] = useState<CreateProviderRequest>(() =>
    buildInitialForm(initial)
  )
  const [headersText, setHeadersText] = useState(() =>
    JSON.stringify(initial?.headers || {}, null, 2)
  )
  const [extraBodyText, setExtraBodyText] = useState(() =>
    JSON.stringify(initial?.extra_body || {}, null, 2)
  )
  const [labelsText, setLabelsText] = useState(() =>
    JSON.stringify(initial?.labels || {}, null, 2)
  )
  const [jsonError, setJsonError] = useState<string | null>(null)
  const [presetId, setPresetId] = useState(() => buildPresetId(initial))

  const isEdit = !!initial
  const selectedPreset = useMemo(
    () => providerCatalog.find((item) => item.id === presetId) ?? null,
    [presetId]
  )

  const updateField = <K extends keyof CreateProviderRequest>(
    key: K,
    value: CreateProviderRequest[K]
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const handlePresetChange = (value: string | null) => {
    const nextPresetId = value ?? ''
    setPresetId(nextPresetId)
    const preset = providerCatalog.find((item) => item.id === nextPresetId)
    if (!preset || isEdit) return
    setForm((prev) => ({
      ...prev,
      ...applyProviderCatalogPreset(preset),
    }))
    setHeadersText(JSON.stringify(preset.headers || {}, null, 2))
    setExtraBodyText(JSON.stringify(preset.extra_body || {}, null, 2))
    setJsonError(null)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    let headers: Record<string, string> | undefined
    let extraBody: Record<string, unknown> | undefined
    let labels: Record<string, string> | undefined

    try {
      headers = JSON.parse(headersText)
      extraBody = JSON.parse(extraBodyText)
      labels = JSON.parse(labelsText)
      setJsonError(null)
    } catch {
      setJsonError('Headers、Extra Body 或 Labels 不是合法 JSON')
      return
    }

    onSubmit({
      ...form,
      headers,
      extra_body: extraBody,
      labels,
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-auto">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? '编辑 Provider' : '创建 Provider'}
          </DialogTitle>
          <DialogDescription>
            {isEdit
              ? '修改 Provider 配置，留空 api_key 表示不更新。'
              : '填写以下信息创建新的 Provider。'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-6">
          {!isEdit && (
            <div className="space-y-2">
              <Label htmlFor="catalog">模板</Label>
              <Select value={presetId} onValueChange={handlePresetChange}>
                <SelectTrigger id="catalog">
                  <SelectValue placeholder="选择 provider 模板" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">自定义</SelectItem>
                  {providerCatalog.map((item) => (
                    <SelectItem key={item.id} value={item.id}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {selectedPreset && (
                <p className="text-muted-foreground text-xs">
                  {selectedPreset.description}
                </p>
              )}
            </div>
          )}

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="name">名称 *</Label>
              <Input
                id="name"
                value={form.name}
                onChange={(e) => updateField('name', e.target.value)}
                required
                disabled={isEdit}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="model">模型 *</Label>
              <Input
                id="model"
                value={form.model}
                onChange={(e) => updateField('model', e.target.value)}
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="type">类型</Label>
              <Input
                id="type"
                value={form.type}
                onChange={(e) => updateField('type', e.target.value)}
                placeholder="openai / anthropic / ..."
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="vendor">厂商</Label>
              <Input
                id="vendor"
                value={form.vendor}
                onChange={(e) => updateField('vendor', e.target.value)}
              />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="base_url">Base URL</Label>
              <Input
                id="base_url"
                value={form.base_url}
                onChange={(e) => updateField('base_url', e.target.value)}
                placeholder="https://api.openai.com/v1"
              />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="endpoint">Endpoint</Label>
              <Input
                id="endpoint"
                value={form.endpoint}
                onChange={(e) => updateField('endpoint', e.target.value)}
              />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="api_key">
                API Key{isEdit && '（留空表示不更新）'}
              </Label>
              <Input
                id="api_key"
                type="password"
                value={form.api_key}
                onChange={(e) => updateField('api_key', e.target.value)}
                required={!isEdit}
              />
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            {[
              { key: 'routing_weight', label: '权重' },
              { key: 'price_input', label: '输入价格' },
              { key: 'price_output', label: '输出价格' },
              { key: 'max_tokens', label: '最大 Token' },
              { key: 'timeout', label: '超时(秒)' },
            ].map((field) => (
              <div key={field.key} className="space-y-2">
                <Label htmlFor={field.key}>{field.label}</Label>
                <Input
                  id={field.key}
                  type="number"
                  value={
                    form[field.key as keyof CreateProviderRequest] as number
                  }
                  onChange={(e) =>
                    updateField(
                      field.key as keyof CreateProviderRequest,
                      parseFloat(e.target.value) || 0
                    )
                  }
                />
              </div>
            ))}
          </div>

          <div className="flex items-center gap-2">
            <Switch
              id="enabled"
              checked={form.enabled}
              onCheckedChange={(checked) => updateField('enabled', checked)}
            />
            <Label htmlFor="enabled">启用</Label>
          </div>

          <div className="space-y-2">
            <Label>能力支持</Label>
            <div className="grid gap-2 md:grid-cols-3">
              {capabilityFields.map((cap) => (
                <div key={cap.key} className="flex items-center gap-2">
                  <Checkbox
                    id={cap.key}
                    checked={
                      (form[
                        cap.key as keyof CreateProviderRequest
                      ] as boolean) ?? false
                    }
                    onCheckedChange={(checked) =>
                      updateField(
                        cap.key as keyof CreateProviderRequest,
                        !!checked
                      )
                    }
                  />
                  <Label htmlFor={cap.key} className="text-sm font-normal">
                    {cap.label}
                  </Label>
                </div>
              ))}
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="headers">Headers (JSON)</Label>
              <Textarea
                id="headers"
                value={headersText}
                onChange={(e) => setHeadersText(e.target.value)}
                rows={4}
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="extra_body">Extra Body (JSON)</Label>
              <Textarea
                id="extra_body"
                value={extraBodyText}
                onChange={(e) => setExtraBodyText(e.target.value)}
                rows={4}
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="labels">Labels (JSON)</Label>
              <Textarea
                id="labels"
                value={labelsText}
                onChange={(e) => setLabelsText(e.target.value)}
                rows={3}
                className="font-mono text-sm"
                placeholder='{"accelerator":"h100","runtime":"vllm"}'
              />
            </div>
          </div>

          {jsonError && <p className="text-destructive text-sm">{jsonError}</p>}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={loading}
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
