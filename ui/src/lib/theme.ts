/**
 * Theme store. The initial value is applied by an inline script in index.html
 * (before first paint); this module keeps it in sync afterwards.
 */

const STORAGE_KEY = 'dlprism.theme'

export type Theme = 'dark' | 'light'

type Listener = () => void

const listeners = new Set<Listener>()

function systemTheme(): Theme {
  try {
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

function initial(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    /* storage unavailable */
  }
  return systemTheme()
}

let theme: Theme = initial()

function apply(next: Theme): void {
  document.documentElement.setAttribute('data-theme', next)
  document.documentElement.style.colorScheme = next
}

apply(theme)

export function getTheme(): Theme {
  return theme
}

export function setTheme(next: Theme): void {
  if (next === theme) return
  theme = next
  apply(theme)
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    /* storage unavailable */
  }
  for (const fn of listeners) fn()
}

export function toggleTheme(): void {
  setTheme(theme === 'dark' ? 'light' : 'dark')
}

export function subscribeTheme(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
