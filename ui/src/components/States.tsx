import type { ReactNode } from 'react'

import { ApiError } from '../lib/api'
import { IconAlert, IconInbox, IconRefresh } from './Icons'

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="state" role="status" aria-live="polite">
      <div className="spinner" />
      <div className="state__body">{label}</div>
    </div>
  )
}

export function Empty({
  title,
  body,
  action,
}: {
  title: string
  body?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="state">
      <IconInbox size={26} className="state__icon" />
      <div className="state__title">{title}</div>
      {body ? <div className="state__body">{body}</div> : null}
      {action}
    </div>
  )
}

/**
 * One error surface for every page. Distinguishes the three failure modes the
 * user can actually act on: gateway unreachable, endpoint not shipped yet
 * (the v0.2 admin routes land in a parallel branch), and everything else.
 */
export function ErrorState({
  error,
  what,
  onRetry,
}: {
  error: ApiError
  what: string
  onRetry?: () => void
}) {
  let title = `Couldn't load ${what}`
  let body: ReactNode = error.message

  if (error.isOffline) {
    title = 'Gateway unreachable'
    body = (
      <>
        The dashboard can't reach the DL Prism API. Check that <code className="mono">prism serve</code>{' '}
        is still running.
      </>
    )
  } else if (error.isNotImplemented) {
    title = 'Not available on this gateway'
    body = (
      <>
        This build doesn't expose {what} yet. Everything else on the dashboard keeps working.
      </>
    )
  } else if (error.isUnauthorized) {
    title = 'Authentication required'
    body = 'This gateway is protected by an API key. Add it in the banner above or on the Status page.'
  }

  return (
    <div className="state state--error" role="alert">
      <IconAlert size={26} className="state__icon" />
      <div className="state__title">{title}</div>
      <div className="state__body">{body}</div>
      {onRetry ? (
        <button type="button" className="btn btn--sm" onClick={onRetry}>
          <IconRefresh size={13} />
          Retry
        </button>
      ) : null}
    </div>
  )
}

/** Slim inline variant for a widget inside an otherwise-healthy page. */
export function InlineNotice({
  tone = 'muted',
  children,
}: {
  tone?: 'muted' | 'warn'
  children: ReactNode
}) {
  return (
    <div
      className="card card--pad"
      style={{
        color: tone === 'warn' ? 'var(--warn)' : 'var(--text-muted)',
        fontSize: 'var(--fs-sm)',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--sp-2)',
      }}
    >
      <IconAlert size={14} />
      <span>{children}</span>
    </div>
  )
}

export function TableSkeleton({ rows = 4, cols = 5 }: { rows?: number; cols?: number }) {
  return (
    <div className="table-wrap" aria-hidden>
      <table className="table">
        <tbody>
          {Array.from({ length: rows }, (_, r) => (
            <tr key={r}>
              {Array.from({ length: cols }, (_, c) => (
                <td key={c}>
                  <div className="skeleton" style={{ height: 12, width: c === 0 ? '70%' : '45%' }} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
