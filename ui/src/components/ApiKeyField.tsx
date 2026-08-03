import { useEffect, useState } from 'react'

import { useAuth } from '../lib/hooks'

/**
 * The bearer key lives in localStorage only — it is never sent anywhere but
 * the gateway's own origin. Rendered on the Status page and (compactly) in
 * the 401 banner.
 */
export function ApiKeyField({
  compact = false,
  onSaved,
}: {
  compact?: boolean
  onSaved?: () => void
}) {
  const { apiKey, setApiKey } = useAuth()
  const [draft, setDraft] = useState(apiKey)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    setDraft(apiKey)
  }, [apiKey])

  const dirty = draft.trim() !== apiKey

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    setApiKey(draft)
    setSaved(true)
    window.setTimeout(() => setSaved(false), 1800)
    onSaved?.()
  }

  if (compact) {
    return (
      <form className="banner__form" onSubmit={submit}>
        <input
          className="input"
          type="password"
          value={draft}
          placeholder="Paste API key"
          aria-label="Gateway API key"
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button type="submit" className="btn btn--primary btn--sm" disabled={!dirty}>
          Save
        </button>
      </form>
    )
  }

  return (
    <form className="field" onSubmit={submit} style={{ maxWidth: 460 }}>
      <label className="field__label" htmlFor="api-key">
        Gateway API key
      </label>
      <div style={{ display: 'flex', gap: 'var(--sp-2)' }}>
        <input
          id="api-key"
          className="input"
          type="password"
          value={draft}
          placeholder={apiKey ? '••••••••••••' : 'Leave empty if the gateway has no key'}
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button type="submit" className="btn btn--primary" disabled={!dirty}>
          {saved && !dirty ? 'Saved' : 'Save'}
        </button>
        {apiKey ? (
          <button type="button" className="btn btn--danger" onClick={() => setApiKey('')}>
            Clear
          </button>
        ) : null}
      </div>
      <p className="field__hint">
        Sent as <code className="mono">Authorization: Bearer …</code> on every request. Stored in this
        browser's localStorage only.
      </p>
    </form>
  )
}
