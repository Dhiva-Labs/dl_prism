import type { RouteInfo, Usage } from '../../lib/types'

export interface Turn {
  id: string
  role: 'user' | 'assistant'
  content: string
  /** Populated from the X-Prism-* response headers on assistant turns. */
  route?: RouteInfo
  usage?: Usage
  /** Set when the request failed; the turn renders as an error card. */
  error?: string
  /** True while tokens are still streaming in. */
  pending?: boolean
  /** True when the user hit Stop mid-stream. */
  stopped?: boolean
}

export interface Params {
  /** null means "omit from the request and let the gateway decide". */
  temperature: number | null
  maxTokens: number | null
  stream: boolean
}

export const DEFAULT_PARAMS: Params = {
  temperature: null,
  maxTokens: null,
  stream: true,
}
