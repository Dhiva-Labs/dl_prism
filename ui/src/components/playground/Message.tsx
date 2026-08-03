import { CopyButton } from '../CopyButton'
import { formatNumber } from '../../lib/format'
import { spectralIndex } from '../../lib/spectral'
import { IconAlert } from '../Icons'
import type { RouteInfo } from '../../lib/types'
import type { Turn } from './types'

/** Per-reply transparency strip built from the X-Prism-* response headers. */
function RouteBar({ route }: { route: RouteInfo }) {
  const items: Array<[string, string]> = []
  if (route.provider) items.push(['provider', route.provider])
  if (route.model) items.push(['model', route.model])
  if (route.mode) items.push(['mode', route.mode])
  if (!items.length && !route.reason) return null

  return (
    <div className="route">
      {items.map(([key, value]) => (
        <span className="route__item" key={key}>
          {key === 'provider' ? (
            <span className={`route__dot spectral-${spectralIndex(value)}`} aria-hidden />
          ) : null}
          <span className="route__key">{key}</span>
          <span className="route__val">{value}</span>
        </span>
      ))}
      {route.reason ? <span className="route__reason">{route.reason}</span> : null}
    </div>
  )
}

export function Message({ turn }: { turn: Turn }) {
  const isUser = turn.role === 'user'
  const hasContent = turn.content.length > 0

  return (
    <article className={isUser ? 'msg msg--user' : 'msg msg--assistant'}>
      <header className="msg__head">
        <span className="msg__role">{isUser ? 'You' : 'Assistant'}</span>
        {turn.stopped ? <span className="badge">stopped</span> : null}
        {turn.usage ? (
          <span className="route__key" title="prompt → completion tokens">
            {formatNumber(turn.usage.prompt_tokens)} → {formatNumber(turn.usage.completion_tokens)} tok
          </span>
        ) : null}
        {hasContent ? (
          <span className="msg__actions">
            <CopyButton text={turn.content} label="Copy message" />
          </span>
        ) : null}
      </header>

      {/* A failed stream can still have delivered tokens — keep them. */}
      {hasContent || !turn.error ? (
        <div className="msg__body">
          {turn.content}
          {turn.pending ? <span className="caret" aria-label="Generating" /> : null}
          {turn.pending && !hasContent ? (
            <span style={{ color: 'var(--text-faint)' }}>waiting for the first token…</span>
          ) : null}
        </div>
      ) : null}

      {turn.error ? (
        <div className="msg__error" role="alert">
          <IconAlert size={14} />
          <span>{turn.error}</span>
        </div>
      ) : null}

      {!isUser && turn.route ? <RouteBar route={turn.route} /> : null}
    </article>
  )
}
