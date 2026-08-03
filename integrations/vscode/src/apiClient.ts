// Thin REST client for the DL Prism gateway.
//
// Deliberately has ZERO dependency on the `vscode` module so it can be
// unit/smoke-tested with plain Node (see scripts/smoke-test.mjs, which
// imports the esbuild-bundled dist/apiClient.mjs build of this file) and
// reused unmodified from the extension host.
//
// Uses only the global `fetch` / `AbortController` / `TextDecoder` that
// Node 18+ (the runtime backing VS Code >=1.82 and any Node 18+ shell)
// provides — no runtime npm dependencies.

import type {
  ChatMessage,
  ChatCompletionResponse,
  ChatStreamEvent,
  ModelsResponse,
  PrismApiErrorBody,
  PrismModel,
  RouteExplainResponse,
  RouteInfo,
  UsageResponse,
} from "./types";

export interface ApiClientConfig {
  baseUrl: string;
  apiKey?: string;
  /** Request timeout in ms for non-streaming calls. Default 15000. */
  timeoutMs?: number;
}

export class PrismApiError extends Error {
  readonly status?: number;
  readonly type?: string;
  /** True for network-level failures (gateway unreachable, DNS, refused, timeout). */
  readonly offline: boolean;

  constructor(message: string, opts?: { status?: number; type?: string; offline?: boolean }) {
    super(message);
    this.name = "PrismApiError";
    this.status = opts?.status;
    this.type = opts?.type;
    this.offline = opts?.offline ?? false;
  }
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  stream?: boolean;
}

function normalizeBaseUrl(baseUrl: string): string {
  return baseUrl.trim().replace(/\/+$/, "");
}

async function readErrorBody(res: Response): Promise<string> {
  try {
    const text = await res.text();
    if (!text) return res.statusText || `HTTP ${res.status}`;
    try {
      const parsed = JSON.parse(text) as PrismApiErrorBody;
      if (parsed?.error?.message) return parsed.error.message;
    } catch {
      // not JSON — fall through to raw text
    }
    return text.slice(0, 500);
  } catch {
    return res.statusText || `HTTP ${res.status}`;
  }
}

function routeInfoFromHeaders(headers: Headers): RouteInfo {
  return {
    provider: headers.get("x-prism-provider"),
    model: headers.get("x-prism-model"),
    mode: headers.get("x-prism-mode"),
    reason: headers.get("x-prism-reason"),
  };
}

/** True when the error looks like "could not reach the server at all". */
function isNetworkError(err: unknown): boolean {
  if (err instanceof Error) {
    if (err.name === "AbortError") return false; // caller-initiated cancel, not "offline"
    const msg = err.message.toLowerCase();
    return (
      msg.includes("fetch failed") ||
      msg.includes("econnrefused") ||
      msg.includes("enotfound") ||
      msg.includes("network") ||
      msg.includes("timed out") ||
      msg.includes("timeout") ||
      err.name === "TypeError"
    );
  }
  return false;
}

export class ApiClient {
  private baseUrl: string;
  private apiKey?: string;
  private timeoutMs: number;

  constructor(config: ApiClientConfig) {
    this.baseUrl = normalizeBaseUrl(config.baseUrl);
    this.apiKey = config.apiKey;
    this.timeoutMs = config.timeoutMs ?? 15000;
  }

  updateConfig(config: ApiClientConfig): void {
    this.baseUrl = normalizeBaseUrl(config.baseUrl);
    this.apiKey = config.apiKey;
    this.timeoutMs = config.timeoutMs ?? this.timeoutMs;
  }

  getBaseUrl(): string {
    return this.baseUrl;
  }

  private url(path: string): string {
    return `${this.baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
  }

  private headers(extra?: Record<string, string>): Record<string, string> {
    const h: Record<string, string> = { ...extra };
    if (this.apiKey) {
      h["Authorization"] = `Bearer ${this.apiKey}`;
    }
    return h;
  }

  /** Wraps fetch with a timeout + consistent PrismApiError translation for non-streaming calls. */
  private async request(path: string, init: RequestInit, signal?: AbortSignal): Promise<Response> {
    const controller = new AbortController();
    const onOuterAbort = () => controller.abort();
    signal?.addEventListener("abort", onOuterAbort);
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const res = await fetch(this.url(path), {
        ...init,
        headers: this.headers(init.headers as Record<string, string> | undefined),
        signal: controller.signal,
      });
      if (!res.ok) {
        const message = await readErrorBody(res);
        throw new PrismApiError(message, { status: res.status });
      }
      return res;
    } catch (err) {
      if (err instanceof PrismApiError) throw err;
      if (err instanceof Error && err.name === "AbortError" && signal?.aborted) {
        throw new PrismApiError("Request cancelled.", { offline: false });
      }
      throw new PrismApiError(
        err instanceof Error ? err.message : "Could not reach the DL Prism gateway.",
        { offline: true }
      );
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onOuterAbort);
    }
  }

  async listModels(signal?: AbortSignal): Promise<PrismModel[]> {
    const res = await this.request("/v1/models", { method: "GET" }, signal);
    const body = (await res.json()) as ModelsResponse;
    return body.data ?? [];
  }

  async getUsage(signal?: AbortSignal): Promise<UsageResponse> {
    const res = await this.request("/admin/usage", { method: "GET" }, signal);
    return (await res.json()) as UsageResponse;
  }

  async explainRoute(
    messages: ChatMessage[],
    model: string,
    signal?: AbortSignal
  ): Promise<RouteExplainResponse> {
    const res = await this.request(
      "/v1/route/explain",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model, messages }),
      },
      signal
    );
    return (await res.json()) as RouteExplainResponse;
  }

  /** Non-streaming chat completion. Returns the parsed response plus the routing decision. */
  async chatComplete(
    req: ChatCompletionRequest,
    signal?: AbortSignal
  ): Promise<{ response: ChatCompletionResponse; route: RouteInfo }> {
    const res = await this.request(
      "/v1/chat/completions",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...req, stream: false }),
      },
      signal
    );
    const route = routeInfoFromHeaders(res.headers);
    const response = (await res.json()) as ChatCompletionResponse;
    return { response, route };
  }

  /**
   * Streams a chat completion, invoking `onEvent` for the routing decision
   * (as soon as headers arrive), each content delta, and finally a "done"
   * (or "error") event. Never throws for gateway-reachable HTTP errors —
   * those surface as an "error" event so callers can render them inline;
   * it only throws for truly unexpected synchronous setup failures.
   */
  async chatStream(
    req: ChatCompletionRequest,
    onEvent: (event: ChatStreamEvent) => void,
    signal?: AbortSignal
  ): Promise<void> {
    let res: Response;
    try {
      res = await fetch(this.url("/v1/chat/completions"), {
        method: "POST",
        headers: this.headers({ "Content-Type": "application/json" }),
        body: JSON.stringify({ ...req, stream: true }),
        signal,
      });
    } catch (err) {
      if (err instanceof Error && err.name === "AbortError") {
        onEvent({ type: "done" });
        return;
      }
      const offline = isNetworkError(err);
      onEvent({
        type: "error",
        message: offline
          ? "DL Prism gateway is unreachable. Check dlPrism.baseUrl and that the gateway is running."
          : err instanceof Error
            ? err.message
            : "Unknown error contacting the gateway.",
      });
      return;
    }

    if (!res.ok) {
      const message = await readErrorBody(res);
      onEvent({ type: "error", message });
      return;
    }

    onEvent({ type: "route", route: routeInfoFromHeaders(res.headers) });

    if (!res.body) {
      // Some environments may not expose a streamable body; fall back to
      // reading the whole thing as one chunk (still SSE-framed).
      const text = await res.text();
      this.consumeSseChunk(text, onEvent);
      onEvent({ type: "done" });
      return;
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder("utf-8");
    let buffer = "";
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() ?? "";
        for (const line of lines) {
          if (this.handleSseLine(line, onEvent)) {
            onEvent({ type: "done" });
            return;
          }
        }
      }
      // Flush any trailing partial line.
      if (buffer.trim()) {
        this.handleSseLine(buffer, onEvent);
      }
      onEvent({ type: "done" });
    } catch (err) {
      if (err instanceof Error && err.name === "AbortError") {
        onEvent({ type: "done" });
        return;
      }
      onEvent({
        type: "error",
        message: err instanceof Error ? err.message : "Stream interrupted.",
      });
    } finally {
      reader.releaseLock?.();
    }
  }

  /** Parses one SSE line. Returns true when a terminal "[DONE]" marker was seen. */
  private handleSseLine(line: string, onEvent: (event: ChatStreamEvent) => void): boolean {
    const trimmed = line.trim();
    if (!trimmed.startsWith("data:")) return false;
    const payload = trimmed.slice("data:".length).trim();
    if (payload === "[DONE]") return true;
    if (!payload) return false;
    try {
      const parsed = JSON.parse(payload) as ChatCompletionResponse;
      const delta = parsed.choices?.[0]?.delta;
      if (delta?.content) {
        onEvent({ type: "delta", content: delta.content });
      }
    } catch {
      // Ignore malformed SSE frames rather than killing the whole stream.
    }
    return false;
  }

  private consumeSseChunk(text: string, onEvent: (event: ChatStreamEvent) => void): void {
    for (const line of text.split("\n")) {
      if (this.handleSseLine(line, onEvent)) return;
    }
  }
}
