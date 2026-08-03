/** Shapes returned by the DL Prism gateway. Mirrors docs/UI_PLAN.md. */

export interface ModelCost {
  input: number
  output: number
}

/** Present on every model except the synthetic "auto" entry. */
export interface PrismMeta {
  aliases?: string[]
  local?: boolean
  context_window?: number
  capabilities?: string[]
  speed?: string
  cost_per_mtok?: ModelCost
  /** Only the "auto" pseudo-model carries this. */
  description?: string
}

export interface Model {
  id: string
  object?: string
  created?: number
  owned_by: string
  prism?: PrismMeta
}

export interface ModelList {
  object: string
  data: Model[]
}

export type ChatRole = 'system' | 'user' | 'assistant'

export interface ChatMessage {
  role: ChatRole
  content: string
}

export interface ChatRequest {
  model: string
  messages: ChatMessage[]
  stream?: boolean
  stream_options?: { include_usage: boolean }
  temperature?: number
  max_tokens?: number
}

export interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface ChatChunk {
  id?: string
  object?: string
  model?: string
  choices?: Array<{
    index: number
    delta?: { role?: string; content?: string }
    message?: { role: string; content: string }
    finish_reason?: string | null
  }>
  usage?: Usage
}

export interface ChatCompletion {
  id: string
  object: string
  model: string
  choices: Array<{
    index: number
    message: { role: string; content: string }
    finish_reason?: string | null
  }>
  usage?: Usage
}

/** Routing decision echoed through X-Prism-* response headers. */
export interface RouteInfo {
  provider: string
  model: string
  mode: string
  reason: string
}

export interface ChainStep {
  provider: string
  model: string
  local: boolean
}

export interface RouteProfile {
  task: string
  language?: string
  vision: boolean
  est_tokens: number
}

export interface ExplainResult {
  mode: string
  reason: string
  profile?: RouteProfile
  chain: ChainStep[]
  est_input_cost_usd?: number
}

export interface UsageModelRow {
  provider: string
  model: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  cost_usd: number
}

export interface UsageDay {
  date: string
  cost_usd: number
  requests: number
}

export interface UsageReport {
  month_total_usd: number
  budget_usd: number
  budget_exceeded: boolean
  models: UsageModelRow[]
  /** Added in v0.2; absent on older gateways. */
  daily?: UsageDay[]
}

export interface ProviderStatus {
  name: string
  type: string
  local: boolean
  healthy: boolean
  models: number
}

export interface StatusReport {
  version: string
  uptime_seconds: number
  offline: boolean
  listen: string
  providers: ProviderStatus[]
}

export interface RoutingRule {
  if: string
  then: string
}

export interface RoutingConfig {
  default: string
  rules: RoutingRule[]
  fallbacks: Record<string, string[]>
}

/* ===========================================================================
   Workbench — projects, chats, knowledge, agent runs.
   Mirrors docs/WORKBENCH_PLAN.md (wave A). The backend lands in parallel, so
   every optional field here is one the UI renders without.
   =========================================================================== */

/** Timestamps arrive as RFC3339 or unix seconds depending on the encoder. */
export type Timestamp = string | number

export type ApprovalMode = 'always' | 'mutations' | 'never'

export interface ProjectSettings {
  agent_enabled: boolean
  approval_mode: ApprovalMode
  /** Empty means "use the gateway default" (the `auto` policy model). */
  model: string
  system_prompt: string
}

export interface Project {
  id: string
  name: string
  created_at?: Timestamp
  updated_at?: Timestamp
  /** Server-side count so the card doesn't have to fetch every chat list. */
  chat_count?: number
  settings?: Partial<ProjectSettings>
}

export interface Chat {
  id: string
  project_id?: string
  title?: string
  created_at?: Timestamp
  updated_at?: Timestamp
  message_count?: number
}

/**
 * One line of `chats/<id>.jsonl`. Deliberately loose: the persisted event log
 * and the live SSE stream are the same vocabulary, and the field names for the
 * persisted user/assistant turns are not nailed down in the plan.
 */
export interface ChatEvent {
  type?: string
  role?: string
  /** `text` on stream events, `content` on stored messages — accept both. */
  text?: string
  content?: string
  message?: string
  id?: string
  tool_call_id?: string
  name?: string
  args?: unknown
  arguments?: unknown
  output?: string
  truncated?: boolean
  approved?: boolean
  usage?: Usage
  created_at?: Timestamp
}

export interface ChatDetail extends Chat {
  events?: ChatEvent[]
  /** Some encoders may prefer a plain message array; both are rendered. */
  messages?: ChatEvent[]
}

export interface KnowledgeFile {
  name: string
  size?: number
  /** The gateway sends `modified`; `modified_at` is accepted as a synonym. */
  modified?: Timestamp
  modified_at?: Timestamp
  /** True when the file is small enough to be folded into the system context. */
  in_context?: boolean
}

export type AgentEventType =
  | 'message_delta'
  | 'tool_call'
  | 'tool_await_approval'
  | 'tool_result'
  | 'done'
  | 'error'

export interface AgentEvent {
  type?: AgentEventType | string
  text?: string
  id?: string
  tool_call_id?: string
  name?: string
  args?: unknown
  arguments?: unknown
  output?: string
  truncated?: boolean
  /** False on a tool the user denied. Absent means the call was not gated. */
  approved?: boolean
  usage?: Usage
  message?: string
  route?: RouteInfo
}

export interface AgentRunRequest {
  chat_id: string
  message: string
  model?: string
}

export interface AgentApproveRequest {
  chat_id: string
  tool_call_id: string
  approve: boolean
}
