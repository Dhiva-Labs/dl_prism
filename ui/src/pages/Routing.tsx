import { useCallback, useState } from 'react'

import { ApiError, explainRoute, getRouting, listModels } from '../lib/api'
import { useAsync } from '../lib/hooks'
import type { ExplainResult } from '../lib/types'
import { Chain } from '../components/ExplainView'
import { IconArrowRight, IconExplain, IconRefresh } from '../components/Icons'
import { ModelPicker } from '../components/ModelPicker'
import { Empty, ErrorState, InlineNotice, Loading } from '../components/States'

function RuleList({ rules }: { rules: Array<{ if: string; then: string }> }) {
  if (!rules.length) {
    return (
      <div className="card">
        <Empty
          title="No routing rules"
          body="Every request falls through to the default model. Add rules under routing.rules in prism.yaml."
        />
      </div>
    )
  }

  return (
    <div className="rules">
      {rules.map((rule, i) => (
        <div className={i === 0 ? 'rule rule--first' : 'rule'} key={`${i}-${rule.if}`}>
          <span className="rule__idx" title={`Rule ${i + 1}`}>
            {i + 1}
          </span>
          <span className="rule__cond">{rule.if}</span>
          <IconArrowRight size={14} className="arrow" />
          <span className="rule__then">{rule.then}</span>
        </div>
      ))}
    </div>
  )
}

function Fallbacks({ fallbacks }: { fallbacks: Record<string, string[]> }) {
  const entries = Object.entries(fallbacks ?? {}).filter(([, chain]) => chain?.length)

  if (!entries.length) {
    return (
      <div className="card">
        <Empty
          title="No fallback chains"
          body="A failing provider surfaces its error directly instead of retrying elsewhere."
        />
      </div>
    )
  }

  return (
    <div className="rules">
      {entries.map(([model, chain]) => (
        <div className="fallback" key={model}>
          <span className="fallback__from">{model}</span>
          {chain.map((hop, i) => (
            <span key={`${hop}-${i}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}>
              <IconArrowRight size={13} className="arrow" />
              <span className="fallback__hop">{hop}</span>
            </span>
          ))}
        </div>
      ))}
    </div>
  )
}

function DryRun() {
  const models = useAsync((signal) => listModels(signal), [])
  const [prompt, setPrompt] = useState('Refactor this Python function to be tail recursive')
  const [model, setModel] = useState('auto')
  const [result, setResult] = useState<ExplainResult | undefined>(undefined)
  const [error, setError] = useState<ApiError | undefined>(undefined)
  const [loading, setLoading] = useState(false)

  const run = useCallback(async () => {
    const text = prompt.trim()
    if (!text) return
    setLoading(true)
    setError(undefined)
    try {
      const explained = await explainRoute({
        model,
        messages: [{ role: 'user', content: text }],
      })
      setResult(explained)
    } catch (err) {
      setResult(undefined)
      setError(err instanceof ApiError ? err : new ApiError(String(err), 0))
    } finally {
      setLoading(false)
    }
  }, [model, prompt])

  return (
    <div className="card card--pad dryrun">
      <div className="field">
        <label className="field__label" htmlFor="dryrun-prompt">
          Prompt
        </label>
        <textarea
          id="dryrun-prompt"
          className="textarea"
          rows={3}
          value={prompt}
          placeholder="Type a prompt to see where it would be routed…"
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              void run()
            }
          }}
        />
      </div>

      <div className="dryrun__row">
        <div className="field">
          <label className="field__label" htmlFor="dryrun-model">
            Requested model
          </label>
          <ModelPicker
            id="dryrun-model"
            models={models.data?.data ?? []}
            value={model}
            onChange={setModel}
            label="Requested model"
          />
        </div>
        <button
          type="button"
          className="btn btn--primary"
          onClick={run}
          disabled={loading || !prompt.trim()}
        >
          <IconExplain size={14} />
          {loading ? 'Explaining…' : 'Dry run'}
        </button>
      </div>

      {models.error ? (
        <InlineNotice>
          Model list unavailable — you can still dry-run against <code className="mono">auto</code>.
        </InlineNotice>
      ) : null}

      {loading ? <Loading label="Asking the router…" /> : null}

      {error && !loading ? (
        <ErrorState error={error} what="the dry run" onRetry={run} />
      ) : null}

      {result && !loading ? (
        <div className="dryrun__out">
          <div className="section">
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
                  <span className="kv__v mono">
                    {result.est_input_cost_usd === 0
                      ? 'free'
                      : `$${result.est_input_cost_usd.toFixed(6)}`}
                  </span>
                </>
              ) : null}
            </div>
            {result.profile ? (
              <div className="chips" style={{ marginTop: 'var(--sp-2)' }}>
                <span className="badge">task: {result.profile.task || 'unknown'}</span>
                {result.profile.language ? (
                  <span className="badge">lang: {result.profile.language}</span>
                ) : null}
                <span className="badge">{result.profile.vision ? 'vision' : 'text only'}</span>
                <span className="badge">~{result.profile.est_tokens} tok</span>
              </div>
            ) : null}
          </div>
          <div className="section">
            <h3 className="section__title">Candidate chain</h3>
            <Chain chain={result.chain ?? []} />
          </div>
        </div>
      ) : null}
    </div>
  )
}

export default function Routing() {
  const routing = useAsync((signal) => getRouting(signal), [])

  return (
    <div className="content content--pad">
      <div className="page">
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Policy</h2>
            <span className="section__hint">
              Rules are evaluated top to bottom — the first match wins.
            </span>
            <div className="topbar__spacer" />
            <button type="button" className="btn btn--sm" onClick={routing.reload}>
              <IconRefresh size={13} />
              Refresh
            </button>
          </div>

          {routing.loading && !routing.data ? (
            <Loading label="Loading routing policy…" />
          ) : routing.error ? (
            <ErrorState error={routing.error} what="the routing policy" onRetry={routing.reload} />
          ) : routing.data ? (
            <>
              <div className="card card--pad" style={{ display: 'flex', alignItems: 'center', gap: 'var(--sp-3)' }}>
                <span className="route__key">default model</span>
                <span className="mono" style={{ fontWeight: 600 }}>
                  {routing.data.default || '—'}
                </span>
                <span className="section__hint">used when no rule matches</span>
              </div>
              <RuleList rules={routing.data.rules ?? []} />
            </>
          ) : null}
        </section>

        {routing.data ? (
          <section className="section">
            <div className="section__head">
              <h2 className="section__title">Fallback chains</h2>
              <span className="section__hint">Tried in order when the primary provider fails.</span>
            </div>
            <Fallbacks fallbacks={routing.data.fallbacks ?? {}} />
          </section>
        ) : null}

        {/* Always available: /v1/route/explain ships in v0.1, unlike /admin/routing. */}
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Dry run</h2>
            <span className="section__hint">
              Ask the router what it would do — nothing is sent upstream.
            </span>
          </div>
          <DryRun />
        </section>
      </div>
    </div>
  )
}
