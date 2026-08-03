/**
 * Bearer key store. The gateway's data endpoints sit behind an optional
 * static key; the UI keeps it in localStorage and attaches it to every
 * request. A 401 anywhere flips `authRequired`, which surfaces the banner.
 */

const STORAGE_KEY = 'dlprism.apiKey'

type Listener = () => void

const listeners = new Set<Listener>()

function read(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

let apiKey = read()
let authRequired = false
let dismissed = false

function emit(): void {
  for (const fn of listeners) fn()
}

export function subscribe(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function getApiKey(): string {
  return apiKey
}

export function setApiKey(next: string): void {
  apiKey = next.trim()
  try {
    if (apiKey) localStorage.setItem(STORAGE_KEY, apiKey)
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* private mode / storage disabled — keep it in memory for this session */
  }
  // Give the new key a chance; re-show the banner only if it 401s again.
  authRequired = false
  dismissed = false
  emit()
}

/** Called by the fetch wrapper whenever the gateway answers 401. */
export function markUnauthorized(): void {
  if (authRequired) return
  authRequired = true
  emit()
}

export function dismissAuthBanner(): void {
  dismissed = true
  emit()
}

export interface AuthState {
  apiKey: string
  authRequired: boolean
  dismissed: boolean
}

let snapshot: AuthState = { apiKey, authRequired, dismissed }

/** Stable snapshot for useSyncExternalStore. */
export function getAuthSnapshot(): AuthState {
  if (
    snapshot.apiKey !== apiKey ||
    snapshot.authRequired !== authRequired ||
    snapshot.dismissed !== dismissed
  ) {
    snapshot = { apiKey, authRequired, dismissed }
  }
  return snapshot
}
