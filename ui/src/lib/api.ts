/**
 * Typed client for the DL Prism gateway.
 *
 * The Go binary serves both the SPA and the API from one origin, so every
 * URL here is relative — no base URL configuration, no CORS.
 */

import { getApiKey, markUnauthorized } from './auth'
import type {
  ChatChunk,
  ChatCompletion,
  ChatRequest,
  ExplainResult,
  ModelList,
  RouteInfo,
  RoutingConfig,
  StatusReport,
  Usage,
  UsageReport,
} from './types'

export class ApiError extends Error {
  readonly status: number
  readonly type: string | undefined

  constructor(message: string, status: number, type?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.type = type
  }

  /** 404 on an endpoint the parallel backend work hasn't shipped yet. */
  get isNotImplemented(): boolean {
    return this.status === 404
  }

  get isUnauthorized(): boolean {
    return this.status === 401
  }

  /** The gateway refused because the resource is already busy. */
  get isConflict(): boolean {
    return this.status === 409
  }

  /** The gateway isn't reachable at all (process down, network dropped). */
  get isOffline(): boolean {
    return this.status === 0
  }
}

interface ErrorEnvelope {
  error?: { message?: string; type?: string }
}

export function authHeaders(extra?: Record<string, string>): Headers {
  const headers = new Headers(extra)
  const key = getApiKey()
  if (key) headers.set('Authorization', `Bearer ${key}`)
  return headers
}

/** Turn any failure — HTTP, transport, or malformed body — into an ApiError. */
export async function normalizeError(res: Response): Promise<ApiError> {
  if (res.status === 401) markUnauthorized()

  let message = ''
  let type: string | undefined
  const body = await res.text().catch(() => '')
  if (body) {
    try {
      const parsed = JSON.parse(body) as ErrorEnvelope
      if (parsed.error?.message) {
        message = parsed.error.message
        type = parsed.error.type
      }
    } catch {
      // Non-JSON body (Go's default 404 handler emits plain text).
      const trimmed = body.trim()
      if (trimmed && trimmed.length < 300) message = trimmed
    }
  }
  if (!message) message = `Request failed with status ${res.status}`
  return new ApiError(message, res.status, type)
}

export function transportError(err: unknown): ApiError {
  const detail = err instanceof Error ? err.message : String(err)
  return new ApiError(`Cannot reach the gateway (${detail})`, 0)
}

/**
 * One JSON round-trip. Empty bodies (204 on DELETE) resolve to `undefined`
 * rather than throwing, so callers can type those as `Promise<void>`.
 */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, { ...init, headers: authHeaders(init?.headers as Record<string, string>) })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw transportError(err)
  }
  if (!res.ok) throw await normalizeError(res)

  const body = await res.text()
  if (!body.trim()) return undefined as T
  try {
    return JSON.parse(body) as T
  } catch {
    throw new ApiError('The gateway returned a malformed response body', res.status)
  }
}

// ---------------------------------------------------------------------------
// Read endpoints
// ---------------------------------------------------------------------------

export function listModels(signal?: AbortSignal): Promise<ModelList> {
  return request<ModelList>('v1/models', { signal })
}

export function getUsage(signal?: AbortSignal): Promise<UsageReport> {
  return request<UsageReport>('admin/usage', { signal })
}

export function getStatus(signal?: AbortSignal): Promise<StatusReport> {
  return request<StatusReport>('admin/status', { signal })
}

export function getRouting(signal?: AbortSignal): Promise<RoutingConfig> {
  return request<RoutingConfig>('admin/routing', { signal })
}

export function explainRoute(body: ChatRequest, signal?: AbortSignal): Promise<ExplainResult> {
  return request<ExplainResult>('v1/route/explain', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
}

// ---------------------------------------------------------------------------
// Chat completions
// ---------------------------------------------------------------------------

function readRouteHeaders(res: Response): RouteInfo {
  return {
    provider: res.headers.get('X-Prism-Provider') ?? '',
    model: res.headers.get('X-Prism-Model') ?? '',
    mode: res.headers.get('X-Prism-Mode') ?? '',
    reason: res.headers.get('X-Prism-Reason') ?? '',
  }
}

/**
 * Incremental SSE pump shared by the chat and agent streams. Buffers raw
 * bytes, splits on the blank-line event delimiter, and hands each raw event
 * block to `onEvent` as it arrives so the UI paints while the response is
 * still open. `onEvent` returns true to stop reading.
 */
export async function pumpSse(res: Response, onEvent: (raw: string) => boolean): Promise<void> {
  if (!res.body) throw new ApiError('Streaming is unavailable in this browser', 0)

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')

      let cut = buffer.indexOf('\n\n')
      while (cut !== -1) {
        const event = buffer.slice(0, cut)
        buffer = buffer.slice(cut + 2)
        if (onEvent(event)) return
        cut = buffer.indexOf('\n\n')
      }
    }
    // Flush a trailing event that arrived without its blank-line terminator.
    buffer += decoder.decode()
    if (buffer.trim()) onEvent(buffer)
  } finally {
    reader.cancel().catch(() => undefined)
  }
}

export interface StreamHandlers {
  /** Fired once, as soon as response headers land (before the first token). */
  onRoute?: (route: RouteInfo) => void
  onToken?: (text: string) => void
  onUsage?: (usage: Usage) => void
}

/**
 * Streaming completion. Forwards `data:` payloads as they arrive so tokens
 * render while the response is still open; terminates on `data: [DONE]`.
 */
export async function streamChat(
  body: ChatRequest,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  let res: Response
  try {
    res = await fetch('v1/chat/completions', {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', Accept: 'text/event-stream' }),
      body: JSON.stringify({ ...body, stream: true, stream_options: { include_usage: true } }),
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw transportError(err)
  }

  if (!res.ok) throw await normalizeError(res)
  handlers.onRoute?.(readRouteHeaders(res))

  const handleEvent = (raw: string): boolean => {
    // An SSE event may carry several `data:` lines; the gateway sends one.
    let payload = ''
    for (const line of raw.split('\n')) {
      if (line.startsWith('data:')) payload += line.slice(5).trimStart()
    }
    if (!payload) return false
    if (payload === '[DONE]') return true

    let chunk: ChatChunk
    try {
      chunk = JSON.parse(payload) as ChatChunk
    } catch {
      return false // ignore keep-alives / partial garbage rather than aborting
    }
    const delta = chunk.choices?.[0]?.delta?.content
    if (delta) handlers.onToken?.(delta)
    if (chunk.usage) handlers.onUsage?.(chunk.usage)
    return false
  }

  await pumpSse(res, handleEvent)
}

export interface ChatResult {
  content: string
  route: RouteInfo
  usage?: Usage
}

/** Non-streaming completion, used when the user turns streaming off. */
export async function sendChat(body: ChatRequest, signal?: AbortSignal): Promise<ChatResult> {
  let res: Response
  try {
    res = await fetch('v1/chat/completions', {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ ...body, stream: false }),
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw transportError(err)
  }
  if (!res.ok) throw await normalizeError(res)

  const route = readRouteHeaders(res)
  const json = (await res.json()) as ChatCompletion
  const result: ChatResult = {
    content: json.choices?.[0]?.message?.content ?? '',
    route,
  }
  if (json.usage) result.usage = json.usage
  return result
}
