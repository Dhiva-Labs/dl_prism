import { useEffect, useRef, type ReactNode } from 'react'

import { useEscape } from '../lib/hooks'
import { IconClose } from './Icons'

/**
 * Right-hand side drawer. Rendered inline (no portal) — the app shell has no
 * transformed ancestors, so `position: fixed` behaves.
 */
export function Drawer({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  useEscape(open, onClose)

  useEffect(() => {
    if (open) panelRef.current?.focus()
  }, [open])

  if (!open) return null

  return (
    <>
      <div className="drawer__scrim" onClick={onClose} aria-hidden />
      <div
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        ref={panelRef}
      >
        <div className="drawer__head">
          <h2 className="drawer__title">{title}</h2>
          <button type="button" className="btn btn--ghost btn--icon" onClick={onClose} aria-label="Close drawer">
            <IconClose size={15} />
          </button>
        </div>
        <div className="drawer__body">{children}</div>
      </div>
    </>
  )
}
