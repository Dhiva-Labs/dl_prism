/**
 * Typed client for the workbench endpoints (docs/WORKBENCH_PLAN.md, wave A).
 *
 * The backend for these lands in parallel with this UI, so every reader here
 * is deliberately tolerant: list endpoints accept a bare array or a wrapped
 * object, timestamps accept either encoding, and a 404 is treated by the
 * pages as "this gateway is older than the workbench" rather than an error.
 */

import { ApiError, authHeaders, normalizeError, pumpSse, request, transportError } from './api'
import type {
  AgentApproveRequest,
  AgentEvent,
  AgentRunRequest,
  Chat,
  ChatDetail,
  ChatEvent,
  KnowledgeFile,
  Project,
  ProjectSettings,
  RouteInfo,
  Usage,
} from './types'

const JSON_HEADERS = { 'Content-Type': 'application/json' }

/** Accepts `[...]`, `{projects: [...]}`, `{data: [...]}`, or `{items: [...]}`. */
function unwrapList<T>(payload: unknown, key: string): T[] {
  if (Array.isArray(payload)) return payload as T[]
  if (payload && typeof payload === 'object') {
    const record = payload as Record<string, unknown>
    for (const candidate of [key, 'data', 'items', 'results']) {
      const value = record[candidate]
      if (Array.isArray(value)) return value as T[]
    }
  }
  return []
}

export const DEFAULT_SETTINGS: ProjectSettings = {
  agent_enabled: false,
  approval_mode: 'mutations',
  model: '',
  system_prompt: '',
}

/** Settings are optional on the wire; the UI always renders a full set. */
export function projectSettings(project: Project | undefined): ProjectSettings {
  return { ...DEFAULT_SETTINGS, ...(project?.settings ?? {}) }
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

export async function listProjects(signal?: AbortSignal): Promise<Project[]> {
  const payload = await request<unknown>('admin/projects', { signal })
  return unwrapList<Project>(payload, 'projects')
}

export function createProject(name: string, signal?: AbortSignal): Promise<Project> {
  return request<Project>('admin/projects', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ name }),
    signal,
  })
}

export function updateProject(
  id: string,
  patch: { name?: string; settings?: Partial<ProjectSettings> },
  signal?: AbortSignal,
): Promise<Project> {
  return request<Project>(`admin/projects/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: JSON_HEADERS,
    body: JSON.stringify(patch),
    signal,
  })
}

export function deleteProject(id: string, signal?: AbortSignal): Promise<void> {
  return request<void>(`admin/projects/${encodeURIComponent(id)}`, { method: 'DELETE', signal })
}

// ---------------------------------------------------------------------------
// Chats
// ---------------------------------------------------------------------------

export async function listChats(projectId: string, signal?: AbortSignal): Promise<Chat[]> {
  const payload = await request<unknown>(`admin/projects/${encodeURIComponent(projectId)}/chats`, {
    signal,
  })
  return unwrapList<Chat>(payload, 'chats')
}

export function createChat(projectId: string, title?: string, signal?: AbortSignal): Promise<Chat> {
  return request<Chat>(`admin/projects/${encodeURIComponent(projectId)}/chats`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(title ? { title } : {}),
    signal,
  })
}

/**
 * The gateway answers `{chat, events}`; flatten it so the timeline sees one
 * record. A flat body (or a `messages` array) is still accepted.
 */
export async function getChat(chatId: string, signal?: AbortSignal): Promise<ChatDetail> {
  const payload = await request<unknown>(`admin/chats/${encodeURIComponent(chatId)}`, { signal })
  const record = (payload ?? {}) as Record<string, unknown>
  const nested =
    record.chat && typeof record.chat === 'object' ? (record.chat as ChatDetail) : undefined
  const detail: ChatDetail = { ...(nested ?? (record as unknown as ChatDetail)) }
  // On a list row `events` is a count, so only an array is a real event log.
  if (Array.isArray(record.events)) detail.events = record.events as ChatEvent[]
  return detail
}

export function deleteChat(chatId: string, signal?: AbortSignal): Promise<void> {
  return request<void>(`admin/chats/${encodeURIComponent(chatId)}`, { method: 'DELETE', signal })
}

// ---------------------------------------------------------------------------
// Knowledge files
// ---------------------------------------------------------------------------

export async function listFiles(projectId: string, signal?: AbortSignal): Promise<KnowledgeFile[]> {
  const payload = await request<unknown>(`admin/projects/${encodeURIComponent(projectId)}/files`, {
    signal,
  })
  return unwrapList<KnowledgeFile>(payload, 'files')
}

/** Multipart upload; the part is named `file` and carries the original name. */
export async function uploadFile(
  projectId: string,
  file: File,
  signal?: AbortSignal,
): Promise<void> {
  const form = new FormData()
  form.append('file', file, file.name)

  let res: Response
  try {
    // No Content-Type header: the browser must set the multipart boundary.
    res = await fetch(`admin/projects/${encodeURIComponent(projectId)}/files`, {
      method: 'POST',
      headers: authHeaders(),
      body: form,
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw transportError(err)
  }
  if (!res.ok) throw await normalizeError(res)
}

/**
 * The gateway selects the file with `?name=` on the collection route. A
 * gateway that only routes the RESTful sub-path is handled too, since the
 * plan's `DELETE /admin/projects/{id}/files` line does not pin either form.
 */
export async function deleteFile(
  projectId: string,
  name: string,
  signal?: AbortSignal,
): Promise<void> {
  const base = `admin/projects/${encodeURIComponent(projectId)}/files`
  try {
    await request<void>(`${base}?name=${encodeURIComponent(name)}`, { method: 'DELETE', signal })
  } catch (err) {
    const status = err instanceof ApiError ? err.status : 0
    if (status !== 404 && status !== 405) throw err
    await request<void>(`${base}/${encodeURIComponent(name)}`, { method: 'DELETE', signal })
  }
}

// ---------------------------------------------------------------------------
// Agent run
// ---------------------------------------------------------------------------

export interface ToolCallEvent {
  id: string
  name: string
  args: unknown
}

export interface ToolResultEvent {
  id: string
  output: string
  truncated: boolean
  /** False when the user denied the call; absent when it was never gated. */
  approved?: boolean
}

export interface AgentHandlers {
  /** Fired once from the X-Prism-* response headers, if the run sets them. */
  onRoute?: (route: RouteInfo) => void
  onDelta?: (text: string) => void
  onToolCall?: (event: ToolCallEvent) => void
  onAwaitApproval?: (toolCallId: string) => void
  onToolResult?: (event: ToolResultEvent) => void
  onDone?: (usage: Usage | undefined) => void
  /** In-band `error` event; the stream may still be carrying prior content. */
  onError?: (message: string) => void
}

function readRouteHeaders(res: Response): RouteInfo {
  return {
    provider: res.headers.get('X-Prism-Provider') ?? '',
    model: res.headers.get('X-Prism-Model') ?? '',
    mode: res.headers.get('X-Prism-Mode') ?? '',
    reason: res.headers.get('X-Prism-Reason') ?? '',
  }
}

/**
 * Split one raw SSE block into its event name and JSON payload. The contract
 * names the event types but not the framing, so both spellings are read: an
 * `event:` line, or a `type` field inside the `data:` JSON.
 */
function parseAgentEvent(raw: string): { type: string; data: AgentEvent } | 'done' | undefined {
  let name = ''
  let payload = ''
  for (const line of raw.split('\n')) {
    if (line.startsWith('event:')) name = line.slice(6).trim()
    else if (line.startsWith('data:')) payload += line.slice(5).trimStart()
  }
  if (!payload) return undefined
  if (payload === '[DONE]') return 'done'

  let data: AgentEvent
  try {
    data = JSON.parse(payload) as AgentEvent
  } catch {
    return undefined // keep-alive or partial garbage — never abort the run
  }
  const type = name || data.type || ''
  if (!type) return undefined
  return { type, data }
}

export async function streamAgentRun(
  body: AgentRunRequest,
  handlers: AgentHandlers,
  signal?: AbortSignal,
): Promise<void> {
  let res: Response
  try {
    res = await fetch('v1/agent/run', {
      method: 'POST',
      headers: authHeaders({ ...JSON_HEADERS, Accept: 'text/event-stream' }),
      body: JSON.stringify(body),
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw transportError(err)
  }
  if (!res.ok) throw await normalizeError(res)
  handlers.onRoute?.(readRouteHeaders(res))

  await pumpSse(res, (raw) => {
    const parsed = parseAgentEvent(raw)
    if (!parsed) return false
    if (parsed === 'done') return true

    const { type, data } = parsed
    const id = data.id ?? data.tool_call_id ?? ''

    switch (type) {
      case 'message_delta':
        if (data.text) handlers.onDelta?.(data.text)
        return false
      case 'tool_call':
        handlers.onToolCall?.({ id, name: data.name ?? 'tool', args: data.args ?? data.arguments })
        return false
      case 'tool_await_approval':
        handlers.onAwaitApproval?.(id)
        return false
      case 'tool_result':
        handlers.onToolResult?.({
          id,
          output: data.output ?? '',
          truncated: Boolean(data.truncated),
          ...(typeof data.approved === 'boolean' ? { approved: data.approved } : {}),
        })
        return false
      case 'done':
        if (data.route) handlers.onRoute?.(data.route)
        handlers.onDone?.(data.usage)
        return true
      case 'error':
        handlers.onError?.(data.message ?? 'The agent run failed')
        return false
      default:
        return false
    }
  })
}

export function approveTool(body: AgentApproveRequest, signal?: AbortSignal): Promise<void> {
  return request<void>('v1/agent/approve', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(body),
    signal,
  })
}
