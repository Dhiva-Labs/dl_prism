// Shared types for the DL Prism gateway REST API.
// Kept dependency-free so this file (and apiClient.ts, which only imports
// from here) can be reused outside the VS Code extension host, e.g. by
// scripts/smoke-test.mjs.

export interface PrismCostPerMTok {
  input?: number;
  output?: number;
}

export interface PrismModelMeta {
  aliases?: string[];
  local?: boolean;
  context_window?: number;
  capabilities?: string[];
  speed?: string;
  cost_per_mtok?: PrismCostPerMTok;
  description?: string;
}

export interface PrismModel {
  id: string;
  object?: string;
  created?: number;
  owned_by?: string;
  prism?: PrismModelMeta;
}

export interface ModelsResponse {
  object: string;
  data: PrismModel[];
}

export interface ChatMessage {
  role: "system" | "user" | "assistant";
  content: string;
}

export interface ChatCompletionChoice {
  index: number;
  message?: ChatMessage;
  delta?: Partial<ChatMessage>;
  finish_reason: string | null;
}

export interface ChatCompletionResponse {
  id: string;
  object: string;
  model: string;
  choices: ChatCompletionChoice[];
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

/** Routing decision surfaced via X-Prism-* response headers. */
export interface RouteInfo {
  provider: string | null;
  model: string | null;
  mode: string | null;
  reason: string | null;
}

export interface RouteExplainChainEntry {
  provider: string;
  model: string;
  local: boolean;
}

export interface RouteExplainResponse {
  mode: string;
  reason: string;
  profile?: Record<string, unknown>;
  chain: RouteExplainChainEntry[];
  est_input_cost_usd?: number;
}

export interface UsageModelEntry {
  provider: string;
  model: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
}

export interface UsageResponse {
  month_total_usd: number;
  budget_usd: number;
  budget_exceeded: boolean;
  models: UsageModelEntry[];
}

export interface PrismApiErrorBody {
  error?: {
    message?: string;
    type?: string;
  };
}

/** A single streamed chat delta, forwarded to the webview as it arrives. */
export interface ChatStreamEvent {
  type: "route" | "delta" | "done" | "error";
  route?: RouteInfo;
  content?: string;
  message?: string;
}
