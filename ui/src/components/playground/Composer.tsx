import { useEffect, useRef, type ReactNode } from 'react'

import { estimateTokens } from '../../lib/format'
import { IconSend, IconStop } from '../Icons'

export function Composer({
  value,
  onChange,
  onSend,
  onStop,
  busy,
  disabled,
  placeholder,
  hint,
}: {
  value: string
  onChange: (next: string) => void
  onSend: () => void
  onStop: () => void
  busy: boolean
  disabled: boolean
  /** Overrides the default idle/disabled placeholders. */
  placeholder?: string
  /** Extra line in the footer, left of the token estimate. */
  hint?: ReactNode
}) {
  const ref = useRef<HTMLTextAreaElement | null>(null)

  // Grow with the content, up to the CSS max-height.
  useEffect(() => {
    const node = ref.current
    if (!node) return
    node.style.height = 'auto'
    node.style.height = `${Math.min(node.scrollHeight, 200)}px`
  }, [value])

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends; Shift+Enter (and IME composition) inserts a newline.
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      if (!busy && value.trim()) onSend()
    }
  }

  return (
    <div className="composer">
      <div className="composer__inner">
        <div className="composer__box">
          <textarea
            ref={ref}
            className="textarea"
            rows={1}
            value={value}
            placeholder={placeholder ?? (disabled ? 'Waiting for the model list…' : 'Send a message…')}
            aria-label="Message"
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={handleKeyDown}
          />
          {busy ? (
            <button type="button" className="btn" onClick={onStop} aria-label="Stop generating">
              <IconStop size={13} />
              Stop
            </button>
          ) : (
            <button
              type="button"
              className="btn btn--primary"
              onClick={onSend}
              disabled={disabled || !value.trim()}
              aria-label="Send message"
            >
              <IconSend size={14} />
              Send
            </button>
          )}
        </div>
        <div className="composer__foot">
          <span>
            <kbd>Enter</kbd> to send · <kbd>Shift</kbd>+<kbd>Enter</kbd> for a newline
          </span>
          {hint}
          {value.trim() ? <span>~{estimateTokens(value)} tokens</span> : null}
        </div>
      </div>
    </div>
  )
}
