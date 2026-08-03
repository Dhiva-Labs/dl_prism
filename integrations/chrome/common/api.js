// api.js — thin client for the DL Prism gateway REST API.
//
// This module is deliberately environment-agnostic: it only touches
// globalThis.fetch, TextDecoder and the Fetch streaming body reader, so the
// exact same file runs unmodified inside the extension (popup, options,
// background service worker) and under plain Node (see ../smoke/smoke.mjs).
// It never touches chrome.* APIs — callers pass in a plain {baseUrl, apiKey}
// config object instead.

const DEFAULT_BASE_URL = 'http://127.0.0.1:8130';

/** Error thrown for any non-2xx response from the gateway. */
export class PrismApiError extends Error {
  constructor(message, { status, type } = {}) {
    super(message);
    this.name = 'PrismApiError';
    this.status = status;
    this.type = type;
  }
}

/** Thrown when the gateway can't be reached at all (network/connection error). */
export class PrismUnreachableError extends Error {
  constructor(baseUrl, cause) {
    super(`could not reach DL Prism gateway at ${baseUrl}`);
    this.name = 'PrismUnreachableError';
    this.cause = cause;
  }
}

function normalizedBaseUrl(cfg) {
  const url = (cfg && cfg.baseUrl && cfg.baseUrl.trim()) || DEFAULT_BASE_URL;
  return url.replace(/\/+$/, '');
}

function buildHeaders(cfg, extra) {
  const headers = { 'Content-Type': 'application/json', ...(extra || {}) };
  if (cfg && cfg.apiKey) {
    headers.Authorization = `Bearer ${cfg.apiKey}`;
  }
  return headers;
}

async function errorFromResponse(res) {
  let data = null;
  try {
    data = await res.json();
  } catch (_err) {
    // body wasn't JSON — fall through to the generic message below.
  }
  if (data && data.error && data.error.message) {
    return new PrismApiError(data.error.message, { status: res.status, type: data.error.type });
  }
  return new PrismApiError(`request failed with status ${res.status}`, { status: res.status });
}

async function doFetch(cfg, path, init) {
  const url = `${normalizedBaseUrl(cfg)}${path}`;
  try {
    return await fetch(url, init);
  } catch (cause) {
    throw new PrismUnreachableError(normalizedBaseUrl(cfg), cause);
  }
}

/** GET /v1/models — resolves to the array of model objects (data[]). */
export async function listModels(cfg, { signal } = {}) {
  const res = await doFetch(cfg, '/v1/models', { headers: buildHeaders(cfg), signal });
  if (!res.ok) throw await errorFromResponse(res);
  const body = await res.json();
  return Array.isArray(body.data) ? body.data : [];
}

/** GET /admin/usage — resolves to the raw usage object. */
export async function getUsage(cfg, { signal } = {}) {
  const res = await doFetch(cfg, '/admin/usage', { headers: buildHeaders(cfg), signal });
  if (!res.ok) throw await errorFromResponse(res);
  return res.json();
}

/** POST /v1/route/explain — resolves to the raw explain object. */
export async function explainRoute(cfg, body, { signal } = {}) {
  const res = await doFetch(cfg, '/v1/route/explain', {
    method: 'POST',
    headers: buildHeaders(cfg),
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok) throw await errorFromResponse(res);
  return res.json();
}

/** Pulls the X-Prism-* routing headers off a fetch Response into a plain object. */
export function routeInfoFromHeaders(res) {
  return {
    provider: res.headers.get('X-Prism-Provider'),
    model: res.headers.get('X-Prism-Model'),
    mode: res.headers.get('X-Prism-Mode'),
    reason: res.headers.get('X-Prism-Reason'),
  };
}

/**
 * POST /v1/chat/completions.
 *
 * By default streams the reply (SSE `data: {chunk}` ... `data: [DONE]`) and
 * invokes onDelta(text) as fragments arrive. Pass `stream: false` in `body`
 * to use a single non-streaming request instead — onDelta still fires once
 * with the full text so callers don't need two code paths.
 *
 * Resolves to { routeInfo, fullText, usage } once the reply is complete.
 */
export async function chatCompletion(cfg, body, { onDelta, signal } = {}) {
  const wantsStream = body.stream !== false;
  const res = await doFetch(cfg, '/v1/chat/completions', {
    method: 'POST',
    headers: buildHeaders(cfg),
    body: JSON.stringify({ ...body, stream: wantsStream }),
    signal,
  });

  if (!res.ok) throw await errorFromResponse(res);

  const routeInfo = routeInfoFromHeaders(res);

  if (!wantsStream || !res.body || typeof res.body.getReader !== 'function') {
    const data = await res.json();
    const text = data?.choices?.[0]?.message?.content ?? '';
    if (onDelta && text) onDelta(text);
    return { routeInfo, fullText: text, usage: data.usage || null };
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let fullText = '';
  let usage = null;

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sepIdx;
    while ((sepIdx = buffer.indexOf('\n\n')) !== -1) {
      const rawEvent = buffer.slice(0, sepIdx);
      buffer = buffer.slice(sepIdx + 2);

      const dataLine = rawEvent.split('\n').find((l) => l.startsWith('data:'));
      if (!dataLine) continue;
      const payload = dataLine.slice(5).trim();
      if (payload === '[DONE]') {
        return { routeInfo, fullText, usage };
      }

      try {
        const chunk = JSON.parse(payload);
        const delta = chunk?.choices?.[0]?.delta?.content;
        if (delta) {
          fullText += delta;
          if (onDelta) onDelta(delta);
        }
        if (chunk?.usage) usage = chunk.usage;
      } catch (_err) {
        // Ignore a malformed SSE frame rather than aborting the whole reply.
      }
    }
  }

  return { routeInfo, fullText, usage };
}

/** Hits /v1/models purely to check reachability/auth; never throws. */
export async function testConnection(cfg) {
  try {
    const models = await listModels(cfg);
    return { ok: true, count: models.length };
  } catch (err) {
    return { ok: false, error: err.message };
  }
}

export const DEFAULTS = Object.freeze({ baseUrl: DEFAULT_BASE_URL });
