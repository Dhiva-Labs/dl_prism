/**
 * The conversation timeline: the shape the agent SSE stream is folded into
 * before it is rendered, plus the pure transitions the run applies to it.
 *
 * An agent turn interleaves prose and tool use, so a run produces several
 * items: text, a tool card, more text. Each transition returns a new array —
 * they are called straight from `setItems`.
 */

import type { ChatDetail, ChatEvent, RouteInfo, Usage } from '../../lib/types'

export type ToolState = 'running' | 'awaiting' | 'approved' | 'denied' | 'done' | 'error'

export interface TimelineMessage {
  kind: 'message'
  id: string
  role: 'user' | 'assistant'
  text: string
  /** True while deltas are still landing in this bubble. */
  pending?: boolean
  stopped?: boolean
  error?: string
  route?: RouteInfo
  usage?: Usage
}

export interface TimelineTool {
  kind: 'tool'
  id: string
  /** The `tool_call_id` the approval endpoint expects. */
  callId: string
  name: string
  args: unknown
  state: ToolState
  output?: string
  truncated?: boolean
  /** True between clicking Approve/Deny and the POST resolving. */
  decisionPending?: boolean
}

export type TimelineItem = TimelineMessage | TimelineTool

let seq = 0
export const nextItemId = (): string => `wb${++seq}`

export function message(
  role: 'user' | 'assistant',
  text: string,
  extra: Partial<TimelineMessage> = {},
): TimelineMessage {
  return { kind: 'message', id: nextItemId(), role, text, ...extra }
}

/** The tail bubble, if it is an assistant message still open for deltas. */
function openAssistant(items: TimelineItem[]): TimelineMessage | undefined {
  const last = items[items.length - 1]
  if (last && last.kind === 'message' && last.role === 'assistant' && last.pending) return last
  return undefined
}

function replaceLast(items: TimelineItem[], next: TimelineItem): TimelineItem[] {
  return [...items.slice(0, -1), next]
}

function patchTool(
  items: TimelineItem[],
  callId: string,
  changes: Partial<TimelineTool>,
): TimelineItem[] {
  return items.map((item) =>
    item.kind === 'tool' && item.callId === callId ? { ...item, ...changes } : item,
  )
}

/** `message_delta` — grow the open bubble, or open a new one after a tool. */
export function appendDelta(items: TimelineItem[], text: string): TimelineItem[] {
  const open = openAssistant(items)
  if (open) return replaceLast(items, { ...open, text: open.text + text })
  return [...items, message('assistant', text, { pending: true })]
}

/** Close the open bubble so the tool card starts a fresh block. */
function sealOpen(items: TimelineItem[]): TimelineItem[] {
  const open = openAssistant(items)
  if (!open) return items
  if (!open.text) return items.slice(0, -1) // drop an empty placeholder bubble
  return replaceLast(items, { ...open, pending: false })
}

export function startTool(
  items: TimelineItem[],
  call: { id: string; name: string; args: unknown },
): TimelineItem[] {
  const sealed = sealOpen(items)
  return [
    ...sealed,
    {
      kind: 'tool',
      id: nextItemId(),
      callId: call.id,
      name: call.name,
      args: call.args,
      state: 'running',
    },
  ]
}

/**
 * `tool_await_approval` may name a call the stream never announced (the
 * gateway is free to gate before emitting the call), so synthesise a card
 * rather than dropping the approval on the floor.
 */
export function awaitApproval(items: TimelineItem[], callId: string): TimelineItem[] {
  const known = items.some((item) => item.kind === 'tool' && item.callId === callId)
  if (known) return patchTool(items, callId, { state: 'awaiting', decisionPending: false })
  const sealed = sealOpen(items)
  return [
    ...sealed,
    {
      kind: 'tool',
      id: nextItemId(),
      callId,
      name: 'tool',
      args: undefined,
      state: 'awaiting',
    },
  ]
}

/**
 * A `tool_result` is the gateway reporting on the call, so it always settles
 * the card — it must never leave one in a pre-result state
 * (`running`/`awaiting`/`approved`), which `closeRun` would later brand
 * "incomplete".
 *
 *   approved: false → denied (the only verdict the gateway sends)
 *   approved: true  → done
 *   absent          → done, EXCEPT for a call this session denied, whose
 *                     verdict is already known locally. Per the contract only
 *                     denials carry the flag, so "absent" is the ordinary
 *                     approved-or-ungated case and must reach `done`.
 */
function resolvedState(previous: ToolState, approved: boolean | undefined): ToolState {
  if (approved === false) return 'denied'
  if (approved === true) return 'done'
  return previous === 'denied' ? 'denied' : 'done'
}

export function settleTool(
  items: TimelineItem[],
  result: { id: string; output: string; truncated: boolean; approved?: boolean },
): TimelineItem[] {
  return items.map((item) =>
    item.kind === 'tool' && item.callId === result.id
      ? {
          ...item,
          state: resolvedState(item.state, result.approved),
          output: result.output,
          truncated: result.truncated,
          decisionPending: false,
        }
      : item,
  )
}

/**
 * True once the gateway has reported on the call. Settled is terminal: the
 * approval round-trip below must not walk a card back out of it.
 */
function isSettled(tool: TimelineTool): boolean {
  return tool.state === 'done' || tool.state === 'denied' || tool.output !== undefined
}

/** The Approve/Deny POST is in flight — freeze the buttons. */
export function pendingDecision(items: TimelineItem[], callId: string): TimelineItem[] {
  return items.map((item) =>
    item.kind === 'tool' && item.callId === callId && !isSettled(item)
      ? { ...item, decisionPending: true }
      : item,
  )
}

/**
 * The Approve/Deny POST came back. `outcome` is what the user chose, or
 * undefined when the POST itself failed and the strip has to come back.
 *
 * The guard is the point: the gateway resumes the tool the moment it receives
 * the approval, so `tool_result` routinely overtakes this POST's own response
 * (a fast local model writing a small file wins that race every time). Forcing
 * `approved` here would drag an already-settled card back to a pre-result
 * state and strand it as "incomplete" for the rest of the run. A settled card
 * only ever loses its pending flag.
 */
export function settleDecision(
  items: TimelineItem[],
  callId: string,
  outcome: 'approved' | 'denied' | undefined,
): TimelineItem[] {
  return items.map((item) => {
    if (item.kind !== 'tool' || item.callId !== callId) return item
    if (isSettled(item)) return { ...item, decisionPending: false }
    return { ...item, decisionPending: false, state: outcome ?? 'awaiting' }
  })
}

/** Terminal states for the whole run: nothing is left pending or awaiting. */
export function closeRun(
  items: TimelineItem[],
  outcome: { usage?: Usage; route?: RouteInfo; error?: string; stopped?: boolean } = {},
): TimelineItem[] {
  let closed = items.map((item): TimelineItem => {
    if (item.kind === 'tool') {
      if (item.state === 'running' || item.state === 'awaiting' || item.state === 'approved') {
        return { ...item, state: 'error', decisionPending: false }
      }
      return item
    }
    return item.pending ? { ...item, pending: false } : item
  })

  const last = closed[closed.length - 1]
  const decorations: Partial<TimelineMessage> = {}
  if (outcome.usage) decorations.usage = outcome.usage
  if (outcome.route) decorations.route = outcome.route
  if (outcome.error) decorations.error = outcome.error
  if (outcome.stopped) decorations.stopped = true
  if (!Object.keys(decorations).length) return closed

  if (last && last.kind === 'message' && last.role === 'assistant') {
    closed = replaceLast(closed, { ...last, ...decorations })
  } else {
    closed = [...closed, message('assistant', '', decorations)]
  }
  return closed
}

// ---------------------------------------------------------------------------
// Replay of a stored chat
// ---------------------------------------------------------------------------

function eventText(event: ChatEvent): string {
  return event.text ?? event.content ?? ''
}

/**
 * Fold `chats/<id>.jsonl` back into the same timeline the live run produces.
 * The plan pins the streaming vocabulary but not how a finished turn is
 * persisted, so both a `messages` array and a `type`-tagged event log are
 * accepted, and unknown event types are skipped rather than rendered raw.
 */
export function timelineFromChat(detail: ChatDetail | undefined): TimelineItem[] {
  const events = detail?.events ?? detail?.messages ?? []
  const items: TimelineItem[] = []

  for (const event of events) {
    if (!event) continue
    const type = event.type ?? (event.role ? 'message' : '')

    switch (type) {
      case 'message':
      case 'message_delta':
      case 'user_message':
      case 'assistant_message': {
        const role = event.role === 'user' || type === 'user_message' ? 'user' : 'assistant'
        const text = eventText(event)
        const tail = items[items.length - 1]
        // Consecutive deltas from one turn collapse into a single bubble.
        if (type === 'message_delta' && tail?.kind === 'message' && tail.role === role) {
          items[items.length - 1] = { ...tail, text: tail.text + text }
        } else if (text) {
          items.push(message(role, text, event.usage ? { usage: event.usage } : {}))
        }
        break
      }
      case 'tool_call':
        items.push({
          kind: 'tool',
          id: nextItemId(),
          callId: event.id ?? event.tool_call_id ?? '',
          name: event.name ?? 'tool',
          args: event.args ?? event.arguments,
          state: 'running',
        })
        break
      case 'tool_await_approval':
        // A stored approval prompt is already settled one way or the other.
        break
      case 'tool_result': {
        const callId = event.id ?? event.tool_call_id ?? ''
        const index = items.findIndex((item) => item.kind === 'tool' && item.callId === callId)
        const previous = index >= 0 ? (items[index] as TimelineTool).state : 'done'
        // Same verdict rule as the live stream, so a replayed denial looks
        // exactly like the one the user just answered.
        const patch = {
          state: resolvedState(previous, event.approved),
          output: event.output ?? '',
          truncated: Boolean(event.truncated),
        }
        if (index >= 0) {
          items[index] = { ...(items[index] as TimelineTool), ...patch }
        } else {
          items.push({
            kind: 'tool',
            id: nextItemId(),
            callId,
            name: event.name ?? 'tool',
            args: event.args ?? event.arguments,
            ...patch,
          })
        }
        break
      }
      case 'error':
        items.push(message('assistant', '', { error: event.message ?? 'The run failed' }))
        break
      default:
        break
    }
  }

  return items
}

// ---------------------------------------------------------------------------
// Tool argument presentation
// ---------------------------------------------------------------------------

/** Tool args arrive as an object or as an OpenAI-style JSON string. */
export function decodeArgs(args: unknown): unknown {
  if (typeof args !== 'string') return args
  const trimmed = args.trim()
  if (!trimmed) return undefined
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return args
  try {
    return JSON.parse(trimmed)
  } catch {
    return args
  }
}

export function prettyArgs(args: unknown): string {
  const decoded = decodeArgs(args)
  if (decoded === undefined || decoded === null) return '{}'
  if (typeof decoded === 'string') return decoded
  try {
    return JSON.stringify(decoded, null, 2)
  } catch {
    return String(decoded)
  }
}

/** The one line worth showing on a collapsed card: what the tool acts on. */
export function toolSummary(args: unknown): string {
  const decoded = decodeArgs(args)
  if (typeof decoded === 'string') return decoded
  if (!decoded || typeof decoded !== 'object') return ''
  const record = decoded as Record<string, unknown>
  for (const key of ['command', 'path', 'file', 'url', 'query', 'dir']) {
    const value = record[key]
    if (typeof value === 'string' && value) return value
  }
  const first = Object.values(record).find((value) => typeof value === 'string' && value)
  return typeof first === 'string' ? first : ''
}
