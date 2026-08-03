import { useEffect, useRef, useState, type ReactNode } from 'react'

import { useEscape } from '../lib/hooks'

/**
 * Centred modal chrome shared by the two blocking questions the workbench
 * asks: "are you sure?" and "what should this be called?". Rendered inline —
 * the shell has no transformed ancestors, so `position: fixed` behaves.
 */
function Modal({
  title,
  onClose,
  children,
}: {
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  useEscape(true, onClose)

  useEffect(() => {
    panelRef.current?.focus()
  }, [])

  return (
    <>
      <div className="modal__scrim" onClick={onClose} aria-hidden />
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        ref={panelRef}
      >
        <h2 className="modal__title">{title}</h2>
        {children}
      </div>
    </>
  )
}

export function Confirm({
  open,
  title,
  body,
  confirmLabel = 'Delete',
  busy = false,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  body: ReactNode
  confirmLabel?: string
  busy?: boolean
  error?: string
  onConfirm: () => void
  onCancel: () => void
}) {
  if (!open) return null

  return (
    <Modal title={title} onClose={busy ? () => undefined : onCancel}>
      <div className="modal__body">{body}</div>
      {error ? (
        <div className="modal__error" role="alert">
          {error}
        </div>
      ) : null}
      <div className="modal__foot">
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--sm btn--danger"
          onClick={onConfirm}
          disabled={busy}
        >
          {busy ? 'Working…' : confirmLabel}
        </button>
      </div>
    </Modal>
  )
}

/** Single-field prompt — used for creating and renaming projects. */
export function PromptDialog({
  open,
  title,
  label,
  placeholder,
  initialValue = '',
  confirmLabel = 'Save',
  busy = false,
  error,
  onSubmit,
  onCancel,
}: {
  open: boolean
  title: string
  label: string
  placeholder?: string
  initialValue?: string
  confirmLabel?: string
  busy?: boolean
  error?: string
  onSubmit: (value: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState(initialValue)
  const inputRef = useRef<HTMLInputElement | null>(null)

  // Reopening for a different row must not show the previous row's text.
  useEffect(() => {
    if (open) {
      setValue(initialValue)
      const id = window.setTimeout(() => inputRef.current?.select(), 0)
      return () => window.clearTimeout(id)
    }
    return undefined
  }, [open, initialValue])

  if (!open) return null

  const submit = () => {
    const trimmed = value.trim()
    if (trimmed && !busy) onSubmit(trimmed)
  }

  return (
    <Modal title={title} onClose={busy ? () => undefined : onCancel}>
      <div className="field">
        <label className="field__label" htmlFor="dialog-input">
          {label}
        </label>
        <input
          id="dialog-input"
          ref={inputRef}
          className="input"
          value={value}
          placeholder={placeholder}
          disabled={busy}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              submit()
            }
          }}
        />
      </div>
      {error ? (
        <div className="modal__error" role="alert">
          {error}
        </div>
      ) : null}
      <div className="modal__foot">
        <button type="button" className="btn btn--sm" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn--sm btn--primary"
          onClick={submit}
          disabled={busy || !value.trim()}
        >
          {busy ? 'Working…' : confirmLabel}
        </button>
      </div>
    </Modal>
  )
}
