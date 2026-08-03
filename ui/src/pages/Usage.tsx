import { useMemo } from 'react'

import { getUsage } from '../lib/api'
import { fillDailyGaps, formatNumber, formatUsd } from '../lib/format'
import { useAsync } from '../lib/hooks'
import { DailyCostChart } from '../components/DailyCostChart'
import { IconRefresh } from '../components/Icons'
import { Empty, ErrorState, InlineNotice, Loading, TableSkeleton } from '../components/States'

function Stat({
  label,
  value,
  meta,
  alert = false,
  children,
}: {
  label: string
  value: string
  meta?: string
  alert?: boolean
  children?: React.ReactNode
}) {
  return (
    <div className={alert ? 'stat stat--alert' : 'stat'}>
      <span className="stat__label">{label}</span>
      <span className="stat__value">{value}</span>
      {meta ? <span className="stat__meta">{meta}</span> : null}
      {children}
    </div>
  )
}

export default function Usage() {
  const usage = useAsync((signal) => getUsage(signal), [])
  const report = usage.data

  const days = useMemo(() => fillDailyGaps(report?.daily, 30), [report?.daily])

  // `daily` is a v0.2 addition — an older gateway simply omits the key.
  const dailyMissing = Boolean(report) && report?.daily === undefined

  const budget = report?.budget_usd ?? 0
  const spent = report?.month_total_usd ?? 0
  const remaining = Math.max(budget - spent, 0)
  const pct = budget > 0 ? Math.min((spent / budget) * 100, 100) : 0
  const exceeded = report?.budget_exceeded ?? false

  if (usage.loading && !report) {
    return (
      <div className="content content--pad">
        <Loading label="Loading usage…" />
      </div>
    )
  }

  if (usage.error) {
    return (
      <div className="content content--pad">
        <ErrorState error={usage.error} what="usage" onRetry={usage.reload} />
      </div>
    )
  }

  return (
    <div className="content content--pad">
      <div className="page">
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">This month</h2>
            <div className="topbar__spacer" />
            <button type="button" className="btn btn--sm" onClick={usage.reload}>
              <IconRefresh size={13} />
              Refresh
            </button>
          </div>

          <div className="stats">
            <Stat
              label="Spent"
              value={formatUsd(spent)}
              meta={exceeded ? 'over budget' : 'billable cloud usage'}
              alert={exceeded}
            />
            <Stat
              label="Budget"
              value={budget > 0 ? formatUsd(budget) : 'unlimited'}
              meta={budget > 0 ? `${pct.toFixed(0)}% used` : 'no cap configured'}
            >
              {budget > 0 ? (
                <div className="meter" aria-hidden>
                  <div
                    className={exceeded ? 'meter__fill meter__fill--alert' : 'meter__fill'}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              ) : null}
            </Stat>
            <Stat
              label="Remaining"
              value={budget > 0 ? formatUsd(remaining) : '—'}
              meta={budget > 0 ? (exceeded ? 'cloud calls are being blocked' : 'until the cap') : undefined}
              alert={exceeded}
            />
            <Stat
              label="Requests"
              value={formatNumber(
                (report?.models ?? []).reduce((sum, row) => sum + (row.requests ?? 0), 0),
              )}
              meta={`${report?.models?.length ?? 0} models used`}
            />
          </div>
        </section>

        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Daily cost</h2>
            <span className="section__hint">Last 30 days</span>
          </div>
          {dailyMissing ? (
            <InlineNotice>
              This gateway build doesn't report a daily breakdown yet — only the month total above.
            </InlineNotice>
          ) : (
            <DailyCostChart days={days} />
          )}
        </section>

        <section className="section">
          <div className="section__head">
            <h2 className="section__title">By model</h2>
            <span className="section__hint">Month to date</span>
          </div>

          {usage.loading ? (
            <TableSkeleton rows={3} cols={6} />
          ) : (report?.models?.length ?? 0) === 0 ? (
            <div className="card">
              <Empty
                title="No requests yet this month"
                body="Send a prompt from the Playground and it will show up here."
              />
            </div>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Model</th>
                    <th>Provider</th>
                    <th className="num">Requests</th>
                    <th className="num">Prompt tok</th>
                    <th className="num">Completion tok</th>
                    <th className="num">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {(report?.models ?? [])
                    .slice()
                    .sort((a, b) => b.cost_usd - a.cost_usd || b.requests - a.requests)
                    .map((row) => (
                      <tr key={`${row.provider}/${row.model}`}>
                        <td className="mono">{row.model}</td>
                        <td>{row.provider}</td>
                        <td className="num">{formatNumber(row.requests)}</td>
                        <td className="num">{formatNumber(row.prompt_tokens)}</td>
                        <td className="num">{formatNumber(row.completion_tokens)}</td>
                        <td className="num">{formatUsd(row.cost_usd)}</td>
                      </tr>
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
