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
