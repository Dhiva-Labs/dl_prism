import { getStatus } from '../lib/api'
import { formatNumber, formatUptime } from '../lib/format'
import { useAsync } from '../lib/hooks'
import { ApiKeyField } from '../components/ApiKeyField'
import { IconKey, IconRefresh } from '../components/Icons'
import { Empty, ErrorState, Loading } from '../components/States'

export default function Status() {
  const status = useAsync((signal) => getStatus(signal), [])
  const report = status.data

  return (
    <div className="content content--pad">
      <div className="page">
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Gateway</h2>
            <div className="topbar__spacer" />
            <button type="button" className="btn btn--sm" onClick={status.reload}>
              <IconRefresh size={13} />
              Refresh
            </button>
          </div>

          {status.loading && !report ? (
            <Loading label="Reading gateway status…" />
          ) : status.error ? (
            <ErrorState error={status.error} what="gateway status" onRetry={status.reload} />
          ) : report ? (
            <div className="stats">
              <div className="stat">
                <span className="stat__label">Version</span>
                <span className="stat__value mono" style={{ fontSize: 'var(--fs-xl)' }}>
                  {report.version || '—'}
                </span>
              </div>
              <div className="stat">
                <span className="stat__label">Uptime</span>
                <span className="stat__value" style={{ fontSize: 'var(--fs-xl)' }}>
                  {formatUptime(report.uptime_seconds)}
                </span>
              </div>
              <div className="stat">
                <span className="stat__label">Mode</span>
                <span className="stat__value" style={{ fontSize: 'var(--fs-xl)' }}>
                  <span className={report.offline ? 'badge badge--warn' : 'badge badge--success'}>
                    {report.offline ? 'offline' : 'online'}
                  </span>
                </span>
                <span className="stat__meta">
                  {report.offline
                    ? 'cloud providers are disabled; local models only'
                    : 'cloud providers reachable'}
                </span>
              </div>
              <div className="stat">
                <span className="stat__label">Listening on</span>
                <span className="stat__value mono" style={{ fontSize: 'var(--fs-xl)' }}>
                  {report.listen || '—'}
                </span>
              </div>
            </div>
          ) : null}
        </section>

        {report ? (
          <section className="section">
            <div className="section__head">
              <h2 className="section__title">Providers</h2>
              <span className="section__hint">
                Health reflects the circuit breaker, not a live ping.
              </span>
            </div>

            {report.providers?.length ? (
              <div className="providers">
                {report.providers.map((provider) => (
                  <div className="provider" key={provider.name}>
                    <span
                      className={provider.healthy ? 'dot dot--ok' : 'dot dot--bad'}
                      role="img"
                      aria-label={provider.healthy ? 'Healthy' : 'Degraded'}
                    />
                    <div className="provider__body">
                      <div className="provider__name">{provider.name}</div>
                      <div className="provider__meta">
                        {provider.type || 'provider'} · {formatNumber(provider.models)} models
                      </div>
                    </div>
                    <span className={provider.local ? 'badge badge--success' : 'badge'}>
                      {provider.local ? 'local' : 'cloud'}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="card">
                <Empty
                  title="No providers configured"
                  body="Add at least one provider to prism.yaml and restart the gateway."
                />
              </div>
            )}
          </section>
        ) : null}

        <section className="section">
          <div className="section__head">
            <h2 className="section__title">
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}>
                <IconKey size={14} />
                Authentication
              </span>
            </h2>
          </div>
          <div className="card card--pad">
            <ApiKeyField />
          </div>
        </section>
      </div>
    </div>
  )
}
