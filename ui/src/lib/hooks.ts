import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'

import {
  dismissAuthBanner,
  getAuthSnapshot,
  setApiKey,
  subscribe as subscribeAuth,
  type AuthState,
} from './auth'
import { ApiError } from './api'
import { getTheme, setTheme, subscribeTheme, toggleTheme, type Theme } from './theme'

export function useTheme(): {
  theme: Theme
  setTheme: (t: Theme) => void
  toggle: () => void
} {
  const theme = useSyncExternalStore(subscribeTheme, getTheme, () => 'dark' as Theme)
  return { theme, setTheme, toggle: toggleTheme }
}

export function useAuth(): AuthState & {
  setApiKey: (key: string) => void
  dismiss: () => void
} {
  const state = useSyncExternalStore(subscribeAuth, getAuthSnapshot, getAuthSnapshot)
  return { ...state, setApiKey, dismiss: dismissAuthBanner }
}

export interface AsyncState<T> {
  data: T | undefined
  error: ApiError | undefined
  loading: boolean
  reload: () => void
}

/**
 * Fetch-on-mount with abort, manual reload, and normalized errors. Deliberately
 * tiny — no cache, no dedupe; every page here shows live operational data and
 * the user reloading is the correct refresh story.
 */
export function useAsync<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  deps: readonly unknown[] = [],
): AsyncState<T> {
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<ApiError | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [nonce, setNonce] = useState(0)

  const fetcherRef = useRef(fetcher)
  useLayoutEffect(() => {
    fetcherRef.current = fetcher
  })

  useEffect(() => {
    const controller = new AbortController()
    let live = true
    setLoading(true)
    setError(undefined)

    fetcherRef
      .current(controller.signal)
      .then((result) => {
        if (!live) return
        setData(result)
        setError(undefined)
      })
      .catch((err: unknown) => {
        if (!live) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setData(undefined)
        setError(err instanceof ApiError ? err : new ApiError(String(err), 0))
      })
      .finally(() => {
        if (live) setLoading(false)
      })

    return () => {
      live = false
      controller.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nonce, ...deps])

  const reload = useCallback(() => setNonce((n) => n + 1), [])
  return { data, error, loading, reload }
}

/** Measured width of an element, for SVG charts that must size to their box. */
export function useElementWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T | null>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const node = ref.current
    if (!node) return
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry) setWidth(entry.contentRect.width)
    })
    observer.observe(node)
    setWidth(node.getBoundingClientRect().width)
    return () => observer.disconnect()
  }, [])

  return [ref, width]
}

/** Copy-to-clipboard with a transient "Copied" acknowledgement. */
export function useCopy(resetMs = 1600): [boolean, (text: string) => void] {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  useEffect(
    () => () => {
      if (timer.current) window.clearTimeout(timer.current)
    },
    [],
  )

  const copy = useCallback(
    (text: string) => {
      const done = () => {
        setCopied(true)
        if (timer.current) window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => setCopied(false), resetMs)
      }
      if (navigator.clipboard?.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(() => fallback(text, done))
      } else {
        fallback(text, done)
      }
    },
    [resetMs],
  )

  return [copied, copy]
}

/** execCommand path for non-secure origins, where the async clipboard is blocked. */
function fallback(text: string, done: () => void): void {
  try {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    document.execCommand('copy')
    document.body.removeChild(area)
    done()
  } catch {
    /* clipboard genuinely unavailable — stay silent rather than alert() */
  }
}

/** Escape key handler for drawers and popovers. */
export function useEscape(active: boolean, onEscape: () => void): void {
  useEffect(() => {
    if (!active) return
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onEscape()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [active, onEscape])
}
