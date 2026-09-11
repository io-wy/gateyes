import { useAuthStore } from '@/stores/auth-store'

export type PlaygroundSurface = 'responses' | 'chat' | 'messages' | 'invoke'

export interface PlaygroundEvent {
  event: string
  data: string
  json: unknown | null
}

export interface PlaygroundCacheTrace {
  result: string
  layer?: string
  reason?: string
  rewrites?: string[]
  promptCacheKey?: string
}

export interface GatewayModel {
  id: string
  provider?: string
  owned_by?: string
  enabled?: boolean
  capabilities?: {
    responses?: boolean
    chat?: boolean
    messages?: boolean
    embeddings?: boolean
    images?: boolean
    stream?: boolean
    tools?: boolean
    structured_output?: boolean
  }
}

export interface PlaygroundResult {
  ok: boolean
  status: number
  contentType: string
  raw: string
  data: unknown
  events: PlaygroundEvent[]
  streamText: string
  cache?: PlaygroundCacheTrace
}

function gatewayPath(prefix: string, surface: PlaygroundSurface) {
  const route =
    surface === 'chat'
      ? 'chat/completions'
      : surface === 'messages'
        ? 'messages'
        : surface === 'invoke'
          ? 'invoke'
          : 'responses'
  return `/service/${encodeURIComponent(prefix)}/${route}`
}

function authHeaders() {
  const token = useAuthStore.getState().token
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  return headers
}

export async function listGatewayModels(): Promise<GatewayModel[]> {
  const res = await fetch('/v1/models', {
    method: 'GET',
    headers: authHeaders(),
  })
  if (!res.ok) {
    throw new Error(`models request failed: ${res.status}`)
  }
  const body = (await res.json()) as { data?: GatewayModel[] }
  return body.data ?? []
}

function cacheTraceFromHeaders(headers: Headers): PlaygroundCacheTrace | undefined {
  const result = headers.get('x-gateyes-cache-result')
  if (!result) {
    return undefined
  }
  return {
    result,
    layer: headers.get('x-gateyes-cache-layer') || undefined,
    reason: headers.get('x-gateyes-cache-reason') || undefined,
    rewrites:
      headers
        .get('x-gateyes-cache-rewrites')
        ?.split(',')
        .map((item) => item.trim())
        .filter(Boolean) || undefined,
    promptCacheKey: headers.get('x-gateyes-prompt-cache-key') || undefined,
  }
}

function decodeEventBlock(block: string): PlaygroundEvent | null {
  const lines = block
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((line) => line.trimEnd())
    .filter(Boolean)
  if (lines.length === 0) {
    return null
  }

  const eventLine = lines.find((line) => line.startsWith('event:'))
  const dataLines = lines.filter((line) => line.startsWith('data:'))
  const event = eventLine ? eventLine.slice(6).trim() : 'message'
  const data = dataLines
    .map((line) => line.slice(5).trimStart())
    .join('\n')
  let json: unknown
  try {
    json = JSON.parse(data)
  } catch {
    json = null
  }
  return { event, data, json }
}

function extractText(payload: Record<string, unknown>) {
  const textFromContent = (content: unknown): string => {
    if (typeof content === 'string') {
      return content
    }
    if (!Array.isArray(content)) {
      return ''
    }
    return content
      .map((block) => {
        if (!block || typeof block !== 'object') {
          return ''
        }
        const item = block as Record<string, unknown>
        return typeof item.text === 'string'
          ? item.text
          : typeof item.content === 'string'
            ? item.content
            : ''
      })
      .join('')
  }

  const direct =
    typeof payload.delta === 'string'
      ? payload.delta
      : typeof payload.text === 'string'
        ? payload.text
        : typeof payload.message === 'string'
          ? payload.message
          : typeof payload.content === 'string'
            ? payload.content
            : ''
  if (direct) {
    return direct
  }

  const delta = payload.delta as Record<string, unknown> | undefined
  if (delta) {
    if (typeof delta.text === 'string') {
      return delta.text
    }
    if (typeof delta.content === 'string') {
      return delta.content
    }
  }

  const part = payload.part as Record<string, unknown> | undefined
  if (part && typeof part.text === 'string') {
    return part.text
  }

  const choices = payload.choices
  if (Array.isArray(choices) && choices.length > 0) {
    const choice = choices[0] as Record<string, unknown>
    const delta = choice.delta as Record<string, unknown> | undefined
    if (delta && typeof delta.content === 'string') {
      return delta.content
    }
    if (delta) {
      const contentText = textFromContent(delta.content)
      if (contentText) {
        return contentText
      }
    }
    const message = choice.message as Record<string, unknown> | undefined
    if (message && typeof message.content === 'string') {
      return message.content
    }
    if (message) {
      const contentText = textFromContent(message.content)
      if (contentText) {
        return contentText
      }
    }
  }

  const response = payload.response as Record<string, unknown> | undefined
  if (response) {
    if (typeof response.output_text === 'string') {
      return response.output_text
    }
    if (typeof response.status === 'string' && response.status === 'completed') {
      return ''
    }
    const output = response.output
    if (Array.isArray(output)) {
      for (const item of output as Record<string, unknown>[]) {
        const content = item.content
        if (Array.isArray(content)) {
          for (const block of content as Record<string, unknown>[]) {
            if (typeof block.text === 'string') {
              return block.text
            }
          }
        }
      }
    }
  }

  return ''
}

async function readStream(
  response: Response,
  onEvent: (event: PlaygroundEvent) => void,
  onText: (text: string) => void
) {
  const reader = response.body?.getReader()
  if (!reader) {
    return { raw: '', events: [], streamText: '' }
  }
  const decoder = new TextDecoder()
  let buffer = ''
  let raw = ''
  let streamText = ''
  const events: PlaygroundEvent[] = []

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }
    const chunk = decoder.decode(value, { stream: true })
    raw += chunk
    buffer = (buffer + chunk).replace(/\r\n/g, '\n').replace(/\r/g, '\n')

    let index = buffer.indexOf('\n\n')
    while (index >= 0) {
      const block = buffer.slice(0, index)
      buffer = buffer.slice(index + 2)
      const event = decodeEventBlock(block)
      if (event) {
        events.push(event)
        onEvent(event)
        if (
          event.json &&
          typeof event.json === 'object' &&
          !event.event.includes('completed') &&
          !event.event.endsWith('.done')
        ) {
          const payload = event.json as Record<string, unknown>
          const text = extractText(payload)
          if (text) {
            streamText += text
            onText(text)
          }
        }
      }
      index = buffer.indexOf('\n\n')
    }
  }

  const tail = decoder.decode()
  if (tail) {
    raw += tail
    buffer = (buffer + tail).replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  }

  if (buffer.trim()) {
    const event = decodeEventBlock(buffer)
    if (event) {
      events.push(event)
      onEvent(event)
      if (
        event.json &&
        typeof event.json === 'object' &&
        !event.event.includes('completed') &&
        !event.event.endsWith('.done')
      ) {
        const payload = event.json as Record<string, unknown>
        const text = extractText(payload)
        if (text) {
          streamText += text
          onText(text)
        }
      }
    }
  }

  return { raw, events, streamText }
}

export async function runPlaygroundRequest({
  prefix,
  surface,
  payload,
  stream,
  onEvent,
  onText,
  signal,
}: {
  prefix: string
  surface: PlaygroundSurface
  payload: Record<string, unknown>
  stream: boolean
  onEvent?: (event: PlaygroundEvent) => void
  onText?: (text: string) => void
  signal?: AbortSignal
}): Promise<PlaygroundResult> {
  const response = await fetch(gatewayPath(prefix, surface), {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ ...payload, stream }),
    signal,
  })

  const contentType = response.headers.get('content-type') || ''
  const cache = cacheTraceFromHeaders(response.headers)
  if (contentType.includes('text/event-stream')) {
    const streamed = await readStream(
      response,
      (event) => onEvent?.(event),
      (text) => onText?.(text)
    )
    return {
      ok: response.ok,
      status: response.status,
      contentType,
      raw: streamed.raw,
      data: null,
      events: streamed.events,
      streamText: streamed.streamText,
      cache,
    }
  }

  const raw = await response.text()
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    data = raw
  }
  return {
    ok: response.ok,
    status: response.status,
    contentType,
    raw,
    data,
    events: [],
    streamText: '',
    cache,
  }
}
