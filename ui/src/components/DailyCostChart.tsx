import { useMemo, useState } from 'react'

import { formatDateLong, formatDateShort, formatUsd, type DenseDay } from '../lib/format'
import { useElementWidth } from '../lib/hooks'
import { Empty } from './States'

const HEIGHT = 180
const PAD_TOP = 12
const PAD_BOTTOM = 22
const PAD_LEFT = 52
const PAD_RIGHT = 8

/** Pick a round-ish top-of-axis so gridline labels read cleanly. */
function niceCeil(value: number): number {
  if (value <= 0) return 1
  const exponent = Math.floor(Math.log10(value))
  const magnitude = 10 ** exponent
  const normalized = value / magnitude
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10
  return step * magnitude
}

/**
 * Hand-rolled SVG bar chart — a single accent series, horizontal gridlines
 * only, no chart library. Sized to its container so the bars stay crisp
 * rather than being stretched by preserveAspectRatio.
 */
export function DailyCostChart({ days }: { days: DenseDay[] }) {
  const [wrapRef, width] = useElementWidth<HTMLDivElement>()
  const [hovered, setHovered] = useState<number | null>(null)

  const hasSpend = days.some((day) => day.cost_usd > 0)

  const geometry = useMemo(() => {
    const plotW = Math.max(width - PAD_LEFT - PAD_RIGHT, 10)
    const plotH = HEIGHT - PAD_TOP - PAD_BOTTOM
    const max = niceCeil(Math.max(...days.map((d) => d.cost_usd), 0))
    const slot = days.length ? plotW / days.length : plotW
    const barW = Math.max(Math.min(slot - 3, 26), 2)

    const ticks = [0, 0.5, 1].map((fraction) => ({
      value: max * fraction,
      y: PAD_TOP + plotH - plotH * fraction,
    }))

    return { plotW, plotH, max, slot, barW, ticks }
  }, [days, width])

  if (!days.length || !hasSpend) {
    return (
      <div className="card">
        <Empty
          title="No spend recorded yet"
          body="Daily costs appear here once requests start flowing through a metered provider. Local models are free and stay at zero."
        />
      </div>
    )
  }

  const { plotH, max, slot, barW, ticks } = geometry
  const active = hovered !== null ? days[hovered] : undefined
  const activeX = hovered !== null ? PAD_LEFT + slot * hovered + slot / 2 : 0

  // Label roughly every 5th day so the axis never crowds on narrow screens.
  const labelEvery = width < 420 ? 10 : width < 700 ? 6 : 4

  return (
    <div className="card card--pad">
      <div className="chart" ref={wrapRef}>
        {width > 0 ? (
          <svg
            height={HEIGHT}
            viewBox={`0 0 ${width} ${HEIGHT}`}
            role="img"
            aria-label={`Daily cost for the last ${days.length} days`}
            onMouseLeave={() => setHovered(null)}
          >
            {ticks.map((tick) => (
              <g key={tick.y}>
                <line
                  className="chart__grid"
                  x1={PAD_LEFT}
                  x2={width - PAD_RIGHT}
                  y1={tick.y}
                  y2={tick.y}
                />
                <text className="chart__label" x={PAD_LEFT - 8} y={tick.y + 3} textAnchor="end">
                  {formatUsd(tick.value)}
                </text>
              </g>
            ))}

            {days.map((day, i) => {
              const ratio = max > 0 ? day.cost_usd / max : 0
              const barH = day.cost_usd > 0 ? Math.max(ratio * plotH, 2) : 2
              const x = PAD_LEFT + slot * i + (slot - barW) / 2
              const y = PAD_TOP + plotH - barH
              const zero = day.cost_usd === 0
              return (
                <g key={day.date}>
                  <rect
                    className="chart__hit"
                    x={PAD_LEFT + slot * i}
                    y={PAD_TOP}
                    width={slot}
                    height={plotH}
                    onMouseEnter={() => setHovered(i)}
                  >
                    <title>{`${formatDateLong(day.date)} — ${formatUsd(day.cost_usd)}`}</title>
                  </rect>
                  <rect
                    className={[
                      'chart__bar',
                      zero ? 'chart__bar--zero' : '',
                      hovered === i ? 'chart__bar--active' : '',
                    ]
                      .filter(Boolean)
                      .join(' ')}
                    x={x}
                    y={y}
                    width={barW}
                    height={barH}
                    rx={barW > 6 ? 2 : 1}
                    pointerEvents="none"
                  />
                </g>
              )
            })}

            <line
              className="chart__grid"
              x1={PAD_LEFT}
              x2={width - PAD_RIGHT}
              y1={PAD_TOP + plotH}
              y2={PAD_TOP + plotH}
            />

            {days.map((day, i) =>
              i % labelEvery === 0 ? (
                <text
                  key={`label-${day.date}`}
                  className="chart__label"
                  x={PAD_LEFT + slot * i + slot / 2}
                  y={HEIGHT - 6}
                  textAnchor="middle"
                >
                  {formatDateShort(day.date)}
                </text>
              ) : null,
            )}
          </svg>
        ) : (
          <div className="skeleton" style={{ height: HEIGHT }} />
        )}

        {active ? (
          <div
            className="chart__tip"
            style={{ left: activeX, top: PAD_TOP - 4 }}
            role="status"
            aria-live="polite"
          >
            <div className="chart__tip-date">{formatDateLong(active.date)}</div>
            <div className="chart__tip-value">{formatUsd(active.cost_usd)}</div>
            <div className="chart__tip-date">
              {active.requests} {active.requests === 1 ? 'request' : 'requests'}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}
