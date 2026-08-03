import type { ChainStep, ExplainResult } from '../lib/types'
import { formatUsd } from '../lib/format'
import { IconArrowDown } from './Icons'

export function Chain({ chain }: { chain: ChainStep[] }) {
  if (!chain?.length) {
    return <p className="section__hint">The gateway returned no candidate chain.</p>
  }

  return (
    <div className="chain">
      {chain.map((step, i) => (
        <div key={`${step.provider}/${step.model}/${i}`}>
          {i > 0 ? (
            <div className="chain__arrow" aria-hidden>
              <IconArrowDown size={13} />
            </div>
          ) : null}
          <div className={i === 0 ? 'chain__step chain__step--primary' : 'chain__step'}>
            <span className="chain__idx">{i + 1}</span>
            <div className="chain__body">
              <div className="chain__model">{step.model}</div>
              <div className="chain__provider">
                {step.provider}
                {i === 0 ? ' · attempted first' : ' · fallback'}
              </div>
            </div>
            <span className={step.local ? 'badge badge--success' : 'badge'}>
              {step.local ? 'local' : 'cloud'}
            </span>
          </div>
        </div>
      ))}
    </div>
  )
}

/** Shared by the Playground explain drawer and the Routing dry-run box. */
export function ExplainView({ result }: { result: ExplainResult }) {
  const { profile } = result

  return (
    <>
      <section className="section">
        <h3 className="section__title">Decision</h3>
        <div className="kv">
          <span className="kv__k">Mode</span>
          <span className="kv__v">
            <span className="badge badge--accent">{result.mode || 'unknown'}</span>
          </span>
          <span className="kv__k">Reason</span>
          <span className="kv__v">{result.reason || '—'}</span>
          {result.est_input_cost_usd !== undefined ? (
            <>
              <span className="kv__k">Est. input</span>
              <span className="kv__v mono">{formatUsd(result.est_input_cost_usd)}</span>
            </>
          ) : null}
        </div>
      </section>

      {profile ? (
        <section className="section">
          <h3 className="section__title">Prompt profile</h3>
          <div className="chips">
            <span className="badge">task: {profile.task || 'unknown'}</span>
            {profile.language ? <span className="badge">lang: {profile.language}</span> : null}
            <span className="badge">{profile.vision ? 'vision' : 'text only'}</span>
            <span className="badge">~{profile.est_tokens} tok</span>
          </div>
        </section>
      ) : null}

      <section className="section">
        <h3 className="section__title">Candidate chain</h3>
        <p className="section__hint">Tried top to bottom; the first provider that answers wins.</p>
        <Chain chain={result.chain ?? []} />
      </section>
    </>
  )
}
