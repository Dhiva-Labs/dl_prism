import { useState } from 'react'

import { formatNumber } from '../../lib/format'
import { spectralIndex } from '../../lib/spectral'
import { IconChevron, IconShield, IconTool } from '../Icons'
import { prettyArgs, toolSummary, type TimelineTool } from './timeline'

/** Guard rail for a runaway tool: the DOM should never hold a 5 MB string. */
const OUTPUT_CAP = 20000

const STATE_BADGE: Record<TimelineTool['state'], { label: string; className: string }> = {
  running: { label: 'running', className: 'badge' },
  awaiting: { label: 'needs approval', className: 'badge badge--warn' },
  approved: { label: 'approved', className: 'badge badge--accent' },
  denied: { label: 'denied', className: 'badge badge--danger' },
  done: { label: 'done', className: 'badge badge--success' },
  error: { label: 'incomplete', className: 'badge badge--danger' },
}

/**
 * Inline permission prompt. The run stream stays open while this sits here —
 * answering it POSTs to /v1/agent/approve on a separate request and the
 * result arrives back down the original stream as `tool_result`.
 */
function ApprovalStrip({
  tool,
  onDecide,
}: {
  tool: TimelineTool
  onDecide: (callId: string, approve: boolean) => void
}) {
  const summary = toolSummary(tool.args)

  return (
    <div className="approve" role="group" aria-label={`Approve ${tool.name}`}>
      <IconShield size={15} className="approve__icon" />
      <div className="approve__text">
        <strong>{tool.name}</strong> wants to run
        {summary ? (
          <>
            {' '}
            <code className="mono approve__target">{summary}</code>
          </>
        ) : null}
        . It only proceeds if you allow it.
      </div>
      <div className="approve__actions">
        <button
          type="button"
          className="btn btn--sm"
          disabled={tool.decisionPending}
          onClick={() => onDecide(tool.callId, false)}
        >
          Deny
        </button>
        <button
          type="button"
          className="btn btn--sm btn--primary"
          disabled={tool.decisionPending}
          onClick={() => onDecide(tool.callId, true)}
        >
          {tool.decisionPending ? 'Sending…' : 'Approve'}
        </button>
      </div>
    </div>
  )
}

export function ToolCard({
  tool,
  onDecide,
}: {
  tool: TimelineTool
  onDecide: (callId: string, approve: boolean) => void
}) {
  // Collapsed by default — the transcript is the story, tools are the margin
  // notes. A pending decision is the exception: it must be readable in place.
  const [open, setOpen] = useState(false)
  const expanded = open || tool.state === 'awaiting'

  const band = spectralIndex(tool.name)
  const summary = toolSummary(tool.args)
  const badge = STATE_BADGE[tool.state]

  const output = tool.output ?? ''
  const clipped = output.length > OUTPUT_CAP
  const shown = clipped ? output.slice(0, OUTPUT_CAP) : output

  return (
    <div className={`tool tool--band-${band}${tool.state === 'awaiting' ? ' tool--awaiting' : ''}`}>
      <button
        type="button"
        className="tool__head"
        aria-expanded={expanded}
        onClick={() => setOpen((value) => !value)}
      >
        <span className={expanded ? 'tool__caret tool__caret--open' : 'tool__caret'} aria-hidden>
          <IconChevron size={13} />
        </span>
        <IconTool size={13} className="tool__glyph" />
        <span className="tool__name mono">{tool.name}</span>
        {summary ? <span className="tool__summary mono">{summary}</span> : null}
        <span className={badge.className}>{badge.label}</span>
        {tool.state === 'running' ? <span className="tool__pulse" aria-hidden /> : null}
      </button>

      {expanded ? (
        <div className="tool__body">
          <div className="tool__label">Arguments</div>
          <pre className="tool__code mono">{prettyArgs(tool.args)}</pre>

          {tool.state === 'done' || tool.state === 'denied' || output ? (
            <>
              <div className="tool__label">
                Output
                {tool.truncated ? <span className="badge badge--warn">truncated</span> : null}
              </div>
              <pre className="tool__code tool__code--out mono">
                {shown || <span className="tool__empty">(no output)</span>}
              </pre>
              {tool.truncated ? (
                <div className="tool__note">
                  The gateway capped this output — read the file directly for the rest.
                </div>
              ) : null}
              {clipped ? (
                <div className="tool__note">
                  Showing the first {formatNumber(OUTPUT_CAP)} characters of{' '}
                  {formatNumber(output.length)}.
                </div>
              ) : null}
            </>
          ) : null}

          {tool.state === 'error' ? (
            <div className="tool__note tool__note--warn">
              The run ended before this tool reported back.
            </div>
          ) : null}
        </div>
      ) : null}

      {tool.state === 'awaiting' ? <ApprovalStrip tool={tool} onDecide={onDecide} /> : null}
    </div>
  )
}
