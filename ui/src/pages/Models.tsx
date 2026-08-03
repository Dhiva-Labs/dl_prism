import { useMemo, useState } from 'react'

import { getStatus, listModels } from '../lib/api'
import { formatContext, formatRate } from '../lib/format'
import { useAsync } from '../lib/hooks'
import type { Model, ProviderStatus } from '../lib/types'
import { sortModels } from '../components/ModelPicker'
import { IconRefresh } from '../components/Icons'
import { Empty, ErrorState, TableSkeleton } from '../components/States'

function HealthCell({ health }: { health: ProviderStatus | undefined }) {
  if (!health) return <span className="route__key">unknown</span>
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}>
      <span className={health.healthy ? 'dot dot--ok' : 'dot dot--bad'} aria-hidden />
      <span style={{ color: health.healthy ? 'var(--text-muted)' : 'var(--danger)' }}>
        {health.healthy ? 'healthy' : 'degraded'}
      </span>
    </span>
  )
}

function ModelRow({ model, health }: { model: Model; health: ProviderStatus | undefined }) {
  const meta = model.prism
  const isAuto = model.id === 'auto'
  const aliases = meta?.aliases ?? []
  const capabilities = meta?.capabilities ?? []

  return (
    <tr>
      <td>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span className="mono" style={{ fontWeight: 600 }}>
            {model.id}
          </span>
          {isAuto && meta?.description ? (
            <span className="field__hint">{meta.description}</span>
          ) : null}
          {aliases.length ? (
            <span className="field__hint">
              alias: <span className="mono">{aliases.join(', ')}</span>
            </span>
          ) : null}
        </div>
      </td>
      <td>{model.owned_by || '—'}</td>
      <td>
        {isAuto ? (
          <span className="badge badge--accent">policy</span>
        ) : meta?.local ? (
          <span className="badge badge--success">local</span>
        ) : (
          <span className="badge">cloud</span>
        )}
      </td>
      <td>
        <HealthCell health={isAuto ? undefined : health} />
      </td>
      <td className="num">{isAuto ? '—' : formatContext(meta?.context_window)}</td>
      <td>
        {capabilities.length ? (
          <span className="chips">
            {capabilities.map((cap) => (
              <span className="badge" key={cap}>
                {cap}
              </span>
            ))}
          </span>
        ) : (
          <span className="route__key">—</span>
        )}
      </td>
      <td>{meta?.speed ? <span className="badge">{meta.speed}</span> : <span className="route__key">—</span>}</td>
      <td className="num">{isAuto ? '—' : formatRate(meta?.cost_per_mtok?.input)}</td>
      <td className="num">{isAuto ? '—' : formatRate(meta?.cost_per_mtok?.output)}</td>
    </tr>
  )
}

export default function Models() {
  const models = useAsync((signal) => listModels(signal), [])
  const status = useAsync((signal) => getStatus(signal), [])
  const [query, setQuery] = useState('')

  const healthByProvider = useMemo(() => {
    const map = new Map<string, ProviderStatus>()
    for (const provider of status.data?.providers ?? []) map.set(provider.name, provider)
    return map
  }, [status.data])

  const rows = useMemo(() => {
    const sorted = sortModels(models.data?.data ?? [])
    const needle = query.trim().toLowerCase()
    if (!needle) return sorted
    return sorted.filter((model) => {
      const haystack = [
        model.id,
        model.owned_by,
        ...(model.prism?.aliases ?? []),
        ...(model.prism?.capabilities ?? []),
      ]
        .join(' ')
        .toLowerCase()
      return haystack.includes(needle)
    })
  }, [models.data, query])

  const total = models.data?.data.length ?? 0
  // /admin/status is a v0.2 addition; a 404 just means no health column data.
  const healthUnavailable = Boolean(status.error) && !status.loading

  return (
    <div className="content content--pad">
      <div className="page">
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Registry</h2>
            <span className="section__hint">
              {total > 0 ? `${total} entries` : 'Everything the gateway can route to'}
              {healthUnavailable ? ' · provider health unavailable on this build' : ''}
            </span>
            <div className="topbar__spacer" />
            <input
              className="input"
              style={{ width: 200 }}
              type="search"
              value={query}
              placeholder="Filter models…"
              aria-label="Filter models"
              onChange={(e) => setQuery(e.target.value)}
            />
            <button
              type="button"
              className="btn btn--sm"
              onClick={() => {
                models.reload()
                status.reload()
              }}
            >
              <IconRefresh size={13} />
              Refresh
            </button>
          </div>

          {models.loading && !models.data ? (
            <TableSkeleton rows={4} cols={6} />
          ) : models.error ? (
            <ErrorState error={models.error} what="the model registry" onRetry={models.reload} />
          ) : rows.length === 0 ? (
            <div className="card">
              <Empty
                title={query ? 'No models match that filter' : 'No models registered'}
                body={
                  query
                    ? 'Try a different provider, alias, or capability.'
                    : 'Add a provider to prism.yaml and restart the gateway.'
                }
                {...(query
                  ? {
                      action: (
                        <button type="button" className="btn btn--sm" onClick={() => setQuery('')}>
                          Clear filter
                        </button>
                      ),
                    }
                  : {})}
              />
            </div>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Model</th>
                    <th>Provider</th>
                    <th>Type</th>
                    <th>Health</th>
                    <th className="num">Context</th>
                    <th>Capabilities</th>
                    <th>Speed</th>
                    <th className="num">In / MTok</th>
                    <th className="num">Out / MTok</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((model) => (
                    <ModelRow
                      key={model.id}
                      model={model}
                      health={healthByProvider.get(model.owned_by)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
