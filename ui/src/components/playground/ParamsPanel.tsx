import type { CSSProperties } from 'react'

import type { Params } from './types'

export function ParamsPanel({
  params,
  onChange,
}: {
  params: Params
  onChange: (next: Params) => void
}) {
  const tempOn = params.temperature !== null
  const temp = params.temperature ?? 0.7
  const fill = `${(temp / 2) * 100}%`

  return (
    <div className="pg__params">
      <div className="field">
        <label className="switch" htmlFor="pg-temp-on">
          <input
            id="pg-temp-on"
            type="checkbox"
            checked={tempOn}
            onChange={(e) => onChange({ ...params, temperature: e.target.checked ? 0.7 : null })}
          />
          <span className="switch__track" />
          <span>
            Temperature{' '}
            {tempOn ? <strong style={{ color: 'var(--text)' }}>{temp.toFixed(2)}</strong> : '(gateway default)'}
          </span>
        </label>
        <input
          className="range"
          type="range"
          min={0}
          max={2}
          step={0.05}
          value={temp}
          disabled={!tempOn}
          aria-label="Temperature"
          style={{ '--range-fill': fill, opacity: tempOn ? 1 : 0.4 } as CSSProperties}
          onChange={(e) => onChange({ ...params, temperature: Number(e.target.value) })}
        />
      </div>

      <div className="field">
        <label className="field__label" htmlFor="pg-max-tokens">
          Max tokens
        </label>
        <input
          id="pg-max-tokens"
          className="input"
          type="number"
          min={1}
          step={1}
          inputMode="numeric"
          placeholder="gateway default"
          value={params.maxTokens ?? ''}
          onChange={(e) => {
            const raw = e.target.value.trim()
            const parsed = Number(raw)
            onChange({
              ...params,
              maxTokens: raw === '' || !Number.isFinite(parsed) || parsed <= 0 ? null : Math.floor(parsed),
            })
          }}
        />
      </div>

      <div className="field">
        <span className="field__label">Response</span>
        <label className="switch" htmlFor="pg-stream">
          <input
            id="pg-stream"
            type="checkbox"
            checked={params.stream}
            onChange={(e) => onChange({ ...params, stream: e.target.checked })}
          />
          <span className="switch__track" />
          <span>Stream tokens as they arrive</span>
        </label>
      </div>
    </div>
  )
}
