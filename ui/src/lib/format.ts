/** Presentation helpers. Pure functions, no React. */

/** 200000 → "200k", 32768 → "32k", 1_500_000 → "1.5M" */
export function formatContext(tokens: number | undefined): string {
  if (!tokens || tokens <= 0) return '—'
  if (tokens >= 1_000_000) {
    const m = tokens / 1_000_000
    return `${m % 1 === 0 ? m : m.toFixed(1)}M`
  }
  if (tokens >= 1000) return `${Math.round(tokens / 1000)}k`
  return String(tokens)
}

/** Costs range from $0.000012 to $1234.56, so pick precision by magnitude. */
export function formatUsd(value: number | undefined | null): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  if (value === 0) return '$0.00'
  const abs = Math.abs(value)
  if (abs < 0.01) return `$${value.toFixed(6)}`
  if (abs < 1) return `$${value.toFixed(4)}`
  return `$${value.toFixed(2)}`
}

/** Per-MTok prices: free models should read "free", not "$0.00". */
export function formatRate(value: number | undefined): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  if (value === 0) return 'free'
  if (value < 1) return `$${value.toFixed(2)}`
  return `$${value % 1 === 0 ? value.toFixed(0) : value.toFixed(2)}`
}

export function formatNumber(value: number | undefined): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  return value.toLocaleString('en-US')
}

/** 93784 → "1d 2h 3m" */
export function formatUptime(seconds: number | undefined): string {
  if (seconds === undefined || seconds === null || Number.isNaN(seconds) || seconds < 0) return '—'
  const total = Math.floor(seconds)
  if (total < 60) return `${total}s`

  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const secs = total % 60

  const parts: string[] = []
  if (days) parts.push(`${days}d`)
  if (hours) parts.push(`${hours}h`)
  if (minutes) parts.push(`${minutes}m`)
  if (!days && !hours) parts.push(`${secs}s`)
  return parts.join(' ')
}

/** "2026-08-03" → "Aug 3". Parsed as UTC-naive to avoid timezone drift. */
export function formatDateShort(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  if (!y || !m || !d) return iso
  const date = new Date(Date.UTC(y, m - 1, d))
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
}

export function formatDateLong(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  if (!y || !m || !d) return iso
  const date = new Date(Date.UTC(y, m - 1, d))
  return date.toLocaleDateString('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}

function toIsoDate(date: Date): string {
  const y = date.getUTCFullYear()
  const m = String(date.getUTCMonth() + 1).padStart(2, '0')
  const d = String(date.getUTCDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

export interface DenseDay {
  date: string
  cost_usd: number
  requests: number
}

/**
 * The gateway omits days with no traffic. A bar chart with holes in it lies
 * about the shape of the series, so re-expand to a contiguous window ending
 * today (or on the last reported day, whichever is later).
 */
export function fillDailyGaps(
  daily: Array<{ date: string; cost_usd: number; requests: number }> | undefined,
  days = 30,
): DenseDay[] {
  const byDate = new Map<string, { cost_usd: number; requests: number }>()
  let last: string | undefined
  for (const row of daily ?? []) {
    if (!row?.date) continue
    byDate.set(row.date, { cost_usd: row.cost_usd ?? 0, requests: row.requests ?? 0 })
    if (!last || row.date > last) last = row.date
  }

  const today = toIsoDate(new Date())
  const endIso = last && last > today ? last : today
  const [ey, em, ed] = endIso.split('-').map(Number)
  if (!ey || !em || !ed) return []
  const end = new Date(Date.UTC(ey, em - 1, ed))

  const out: DenseDay[] = []
  for (let i = days - 1; i >= 0; i--) {
    const cursor = new Date(end)
    cursor.setUTCDate(cursor.getUTCDate() - i)
    const iso = toIsoDate(cursor)
    const hit = byDate.get(iso)
    out.push({ date: iso, cost_usd: hit?.cost_usd ?? 0, requests: hit?.requests ?? 0 })
  }
  return out
}

/** Approximate token count for the composer hint. Deliberately rough. */
export function estimateTokens(text: string): number {
  if (!text) return 0
  return Math.ceil(text.length / 4)
}
