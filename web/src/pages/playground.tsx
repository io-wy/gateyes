import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Play,
  Square,
  MessageSquare,
  Bot,
  FileCode,
  Sparkles,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { JsonBlock } from '@/components/json-block'
import { servicesApi } from '@/api/services'
import {
  runPlaygroundRequest,
  type PlaygroundCacheTrace,
  type PlaygroundEvent,
  type PlaygroundSurface,
} from '@/api/playground'
import { useAuthStore } from '@/stores/auth-store'

const SURFACES: Array<{
  id: PlaygroundSurface
  label: string
  description: string
  icon: typeof MessageSquare
}> = [
  {
    id: 'responses',
    label: 'Responses',
    description: 'OpenAI Responses / 统一响应格式',
    icon: Sparkles,
  },
  {
    id: 'chat',
    label: 'Chat',
    description: 'OpenAI Chat Completions',
    icon: Bot,
  },
  {
    id: 'messages',
    label: 'Messages',
    description: 'Anthropic Messages',
    icon: MessageSquare,
  },
  {
    id: 'invoke',
    label: 'Invoke',
    description: 'Service prompt template',
    icon: FileCode,
  },
]

function parseJson<T>(text: string, fallback: T) {
  try {
    return JSON.parse(text) as T
  } catch {
    return fallback
  }
}

function buildDefaultPayload(
  surface: PlaygroundSurface,
  model: string,
  systemPrompt: string,
  userPrompt: string,
  variablesText: string,
  toolsText: string,
  maxTokens: number,
  stream: boolean
) {
  const tools = parseJson<unknown[]>(toolsText, [])
  if (surface === 'invoke') {
    return {
      variables: parseJson<Record<string, unknown>>(variablesText, {}),
      stream,
      max_tokens: maxTokens,
      max_output_tokens: maxTokens,
      tools,
    }
  }

  if (surface === 'messages') {
    return {
      model,
      system: systemPrompt,
      messages: [
        {
          role: 'user',
          content: [{ type: 'text', text: userPrompt }],
        },
      ],
      stream,
      max_tokens: maxTokens,
      tools,
    }
  }

  return {
    model,
    messages: [
      ...(systemPrompt
        ? [{ role: 'system', content: systemPrompt }]
        : []),
      { role: 'user', content: userPrompt },
    ],
    stream,
    max_tokens: maxTokens,
    max_output_tokens: maxTokens,
    tools,
  }
}

function cacheBadgeVariant(
  result?: string
): 'default' | 'destructive' | 'secondary' | 'outline' {
  if (result === 'hit') return 'default'
  if (result === 'error') return 'destructive'
  if (result === 'skip') return 'secondary'
  return 'outline'
}

function cacheBadgeText(cache?: PlaygroundCacheTrace | null) {
  if (!cache?.result) {
    return 'cache pending'
  }
  const suffix = cache.layer ? ` / ${cache.layer}` : ''
  if (cache.result === 'skip' && cache.reason) {
    return `cache skip: ${cache.reason}${suffix}`
  }
  if (cache.result === 'error' && cache.reason) {
    return `cache error${suffix}`
  }
  return `cache ${cache.result}${suffix}`
}

export function PlaygroundPage() {
  const token = useAuthStore((state) => state.token)
  const abortRef = useRef<AbortController | null>(null)
  const [surface, setSurface] = useState<PlaygroundSurface>('responses')
  const [serviceId, setServiceId] = useState('')
  const [model, setModel] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('You are a concise assistant.')
  const [userPrompt, setUserPrompt] = useState('Explain this request in one sentence.')
  const [variablesText, setVariablesText] = useState('{\n  "topic": "Gateyes"\n}')
  const [toolsText, setToolsText] = useState('[]')
  const [maxTokens, setMaxTokens] = useState(256)
  const [stream, setStream] = useState(true)
  const [running, setRunning] = useState(false)
  const [durationMs, setDurationMs] = useState<number | null>(null)
  const [status, setStatus] = useState<number | null>(null)
  const [resultData, setResultData] = useState<unknown>(null)
  const [rawResponse, setRawResponse] = useState('')
  const [streamText, setStreamText] = useState('')
  const [eventLog, setEventLog] = useState<PlaygroundEvent[]>([])
  const [cacheTrace, setCacheTrace] = useState<PlaygroundCacheTrace | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [mobileTab, setMobileTab] = useState<'request' | 'response'>('request')
  const [outputTab, setOutputTab] = useState<'stream' | 'json' | 'raw' | 'events'>('stream')

  const { data: services } = useQuery({
    queryKey: ['services'],
    queryFn: () => servicesApi.list(),
  })

  const publishedServices = useMemo(
    () => services?.filter((item) => item.publish_status === 'published') ?? [],
    [services]
  )

  const selectedService = useMemo(
    () => publishedServices.find((item) => item.id === serviceId) ?? publishedServices[0] ?? null,
    [publishedServices, serviceId]
  )
  const activeServiceId = serviceId || selectedService?.id || ''
  const activeModel = model || selectedService?.default_model || ''

  useEffect(() => {
    return () => {
      abortRef.current?.abort()
    }
  }, [])

  const requestBody = useMemo(() => {
    if (!selectedService) return null
    return buildDefaultPayload(
      surface,
      activeModel,
      systemPrompt,
      userPrompt,
      variablesText,
      toolsText,
      maxTokens,
      stream
    )
  }, [
    surface,
    activeModel,
    selectedService,
    systemPrompt,
    userPrompt,
    variablesText,
    toolsText,
    maxTokens,
    stream,
  ])

  const handleRun = async () => {
    if (!selectedService) return
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setRunning(true)
    setError(null)
    setStatus(null)
    setDurationMs(null)
    setResultData(null)
    setRawResponse('')
    setStreamText('')
    setEventLog([])
    setCacheTrace(null)

    const startedAt = performance.now()
    try {
      const result = await runPlaygroundRequest({
        prefix: selectedService.request_prefix,
        surface,
        payload: requestBody as Record<string, unknown>,
        stream,
        signal: controller.signal,
        onEvent: (event) => {
          setEventLog((current) => [...current, event])
          if (event.json && typeof event.json === 'object') {
            const payload = event.json as Record<string, unknown>
            if (surface === 'invoke' && payload.response) {
              setResultData(payload.response)
            } else if (payload.response) {
              setResultData(payload.response)
            } else if (payload.choices || payload.output || payload.data) {
              setResultData(payload)
            }
          }
        },
        onText: (text) => {
          setStreamText((current) => current + text)
        },
      })

      setStatus(result.status)
      setRawResponse(result.raw)
      setCacheTrace(result.cache ?? null)
      if (!stream) {
        setResultData(result.data)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '请求失败')
    } finally {
      setDurationMs(Math.round(performance.now() - startedAt))
      setRunning(false)
    }
  }

  const handleStop = () => {
    abortRef.current?.abort()
    setRunning(false)
  }

  return (
    <div className="flex h-[calc(100vh-6rem)] flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold sm:text-2xl">API Test Playground</h1>
        <p className="text-muted-foreground text-sm">
          测试 {selectedService?.name || 'service'} 的 `/service/:prefix/*` 路由，支持流式输出与事件查看。
        </p>
      </div>

      <div className="flex flex-col gap-3 rounded-lg border bg-card p-3 shadow-sm sm:flex-row sm:items-end sm:justify-between">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:flex-wrap">
          <div className="min-w-[180px] space-y-1.5">
            <Label className="text-xs">Service</Label>
            {publishedServices.length === 0 ? (
              <div className="text-muted-foreground text-sm">
                暂无已发布 Service，去{' '}
                <a href="/services" className="text-primary underline">
                  Service 页面
                </a>{' '}
                创建
              </div>
            ) : (
              <Select value={activeServiceId} onValueChange={(value) => setServiceId(value ?? '')}>
                <SelectTrigger className="h-9">
                  <SelectValue placeholder="选择 service">
                    {selectedService
                      ? `${selectedService.name} / ${selectedService.request_prefix}`
                      : '选择 service'}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {publishedServices.map((service) => (
                    <SelectItem key={service.id} value={service.id}>
                      {service.name} / {service.request_prefix}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          <div className="min-w-[140px] space-y-1.5">
            <Label className="text-xs">模型</Label>
            <Input
              className="h-9"
              value={surface === 'invoke' ? '' : activeModel}
              onChange={(e) => setModel(e.target.value)}
              placeholder={selectedService?.default_model || 'model'}
              disabled={surface === 'invoke'}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">Endpoint</Label>
            <div className="inline-flex rounded-md border p-1">
              {SURFACES.map((item) => {
                const Icon = item.icon
                const active = surface === item.id
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => setSurface(item.id)}
                    className={`flex items-center gap-1.5 rounded px-2.5 py-1.5 text-sm font-medium transition-colors ${
                      active
                        ? 'bg-primary text-primary-foreground'
                        : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                    title={item.description}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    <span className="hidden sm:inline">{item.label}</span>
                    <span className="sm:hidden">{item.label.slice(0, 2)}</span>
                  </button>
                )
              })}
            </div>
          </div>

          <div className="flex items-end gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs">Streaming</Label>
              <div className="flex h-9 items-center gap-2 rounded-md border px-3">
                <Switch
                  id="stream"
                  checked={stream}
                  onCheckedChange={setStream}
                />
                <span className="text-sm text-muted-foreground">
                  {stream ? '启用' : '关闭'}
                </span>
              </div>
            </div>
            <div className="w-24 space-y-1.5">
              <Label className="text-xs">Max Tokens</Label>
              <Input
                id="max_tokens"
                type="number"
                min={1}
                className="h-9"
                value={maxTokens}
                onChange={(e) => setMaxTokens(Number(e.target.value) || 0)}
              />
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Badge variant="outline" className="h-9 gap-1 px-2.5">
            <span>{token ? '已登录' : '未登录'}</span>
          </Badge>
          <Button
            variant="outline"
            size="sm"
            className="h-9"
            onClick={handleStop}
            disabled={!running}
          >
            <Square className="mr-1.5 h-4 w-4" />
            停止
          </Button>
          <Button
            size="sm"
            className="h-9 px-4"
            onClick={handleRun}
            disabled={running || !selectedService}
          >
            <Play className="mr-1.5 h-4 w-4" />
            {running ? '运行中...' : '运行'}
          </Button>
        </div>
      </div>

      <div className="flex border-b lg:hidden">
        <button
          type="button"
          onClick={() => setMobileTab('request')}
          className={`flex-1 px-4 py-2 text-sm font-medium transition-colors ${
            mobileTab === 'request'
              ? 'border-b-2 border-primary text-primary'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          请求配置
        </button>
        <button
          type="button"
          onClick={() => setMobileTab('response')}
          className={`flex-1 px-4 py-2 text-sm font-medium transition-colors ${
            mobileTab === 'response'
              ? 'border-b-2 border-primary text-primary'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          响应结果
        </button>
      </div>

      <div className="grid flex-1 gap-4 overflow-hidden lg:grid-cols-[1fr_1fr] xl:grid-cols-[1.2fr_0.8fr]">
        <div className={`min-w-0 space-y-4 overflow-y-auto pr-1 ${mobileTab !== 'request' ? 'hidden lg:block' : ''}`}>
          <section className="space-y-4 rounded-lg border bg-card p-4 shadow-sm">
            {surface !== 'invoke' ? (
              <>
                <div className="space-y-2">
                  <Label htmlFor="system">System Prompt</Label>
                  <Textarea
                    id="system"
                    rows={4}
                    value={systemPrompt}
                    onChange={(e) => setSystemPrompt(e.target.value)}
                    placeholder="You are a helpful assistant."
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="user">User Prompt</Label>
                  <Textarea
                    id="user"
                    rows={6}
                    value={userPrompt}
                    onChange={(e) => setUserPrompt(e.target.value)}
                    placeholder="Enter your prompt here..."
                  />
                </div>
              </>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="variables">Variables JSON</Label>
                <Textarea
                  id="variables"
                  rows={12}
                  className="font-mono text-sm"
                  value={variablesText}
                  onChange={(e) => setVariablesText(e.target.value)}
                />
              </div>
            )}
          </section>

          <section className="space-y-4 rounded-lg border bg-card p-4 shadow-sm">
            <div className="space-y-2">
              <Label htmlFor="tools">Tools JSON</Label>
              <Textarea
                id="tools"
                rows={4}
                className="font-mono text-sm"
                value={toolsText}
                onChange={(e) => setToolsText(e.target.value)}
              />
            </div>

            <div className="space-y-2">
              <h2 className="text-sm font-medium">Request Preview</h2>
              <JsonBlock title="Payload JSON" value={requestBody} />
            </div>

            {error && <p className="text-destructive text-sm">{error}</p>}
          </section>
        </div>

        <div className={`min-w-0 flex flex-col overflow-hidden rounded-lg border bg-card shadow-sm ${mobileTab !== 'response' ? 'hidden lg:flex' : ''}`}>
          <div className="flex border-b">
            {[
              { id: 'stream', label: 'Stream Text' },
              { id: 'json', label: 'Response JSON' },
              { id: 'raw', label: 'Raw' },
              { id: 'events', label: 'Events' },
            ].map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => setOutputTab(tab.id as typeof outputTab)}
                className={`flex-1 px-2 py-2 text-xs font-medium transition-colors sm:text-sm ${
                  outputTab === tab.id
                    ? 'border-b-2 border-primary text-primary'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <div className="flex-1 overflow-y-auto p-4">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <Badge
                variant={
                  status == null
                    ? 'secondary'
                    : status < 400
                      ? 'default'
                      : 'destructive'
                }
              >
                {status ? `${status}` : running ? 'running' : 'idle'}
              </Badge>
              <Badge variant="outline">
                {durationMs != null ? `${durationMs} ms` : 'pending'}
              </Badge>
              <Badge variant="outline">{stream ? 'stream' : 'json'}</Badge>
              <Badge variant={cacheBadgeVariant(cacheTrace?.result)}>
                {cacheBadgeText(cacheTrace)}
              </Badge>
              {cacheTrace?.rewrites?.length ? (
                <Badge variant="secondary">rewrite {cacheTrace.rewrites.length}</Badge>
              ) : null}
              {cacheTrace?.promptCacheKey ? (
                <Badge variant="outline">
                  pck {cacheTrace.promptCacheKey.slice(0, 12)}
                </Badge>
              ) : null}
            </div>

            {outputTab === 'stream' && (
              <pre className="max-h-[calc(100%-3rem)] overflow-auto rounded-md border bg-muted p-3 text-xs leading-6 whitespace-pre-wrap break-words sm:p-4">
                {streamText || '等待流式输出...'}
              </pre>
            )}

            {outputTab === 'json' && (
              <JsonBlock title="Response JSON" value={resultData} />
            )}

            {outputTab === 'raw' && (
              <pre className="max-h-[calc(100%-3rem)] overflow-auto rounded-md border bg-muted p-3 text-xs leading-6 whitespace-pre-wrap break-words sm:p-4">
                {rawResponse || '运行后显示原始 HTTP / SSE 内容。'}
              </pre>
            )}

            {outputTab === 'events' && (
              <div className="max-h-[calc(100%-3rem)] space-y-2 overflow-auto">
                {eventLog.length === 0 ? (
                  <div className="text-muted-foreground text-sm">
                    运行后显示 SSE 事件。
                  </div>
                ) : (
                  eventLog.map((event, index) => (
                    <div
                      key={`${event.event}-${index}`}
                      className="rounded-md border bg-muted/40 p-3 text-xs"
                    >
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className="font-mono font-medium">{event.event}</span>
                      </div>
                      <pre className="overflow-auto whitespace-pre-wrap break-words font-mono leading-5">
                        {event.data}
                      </pre>
                    </div>
                  ))
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
