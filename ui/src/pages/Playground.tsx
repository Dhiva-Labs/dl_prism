import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, explainRoute, listModels, sendChat, streamChat } from '../lib/api'
import { useAsync } from '../lib/hooks'
import type { ChatMessage, ChatRequest, ExplainResult, RouteInfo, Usage } from '../lib/types'
import { Drawer } from '../components/Drawer'
import { ExplainView } from '../components/ExplainView'
import { IconArrowDown, IconChevron, IconExplain, IconTrash } from '../components/Icons'
import { ModelPicker } from '../components/ModelPicker'
import { Composer } from '../components/playground/Composer'
import { Message } from '../components/playground/Message'
import { ParamsPanel } from '../components/playground/ParamsPanel'
import { DEFAULT_PARAMS, type Params, type Turn } from '../components/playground/types'
import { ErrorState, Loading } from '../components/States'

const MODEL_KEY = 'dlprism.model'

let seq = 0
const nextId = (): string => `t${++seq}`

function readStoredModel(): string {
  try {
    return localStorage.getItem(MODEL_KEY) ?? 'auto'
  } catch {
    return 'auto'
  }
}

/** Errored and still-empty turns are not part of the conversation we replay. */
function historyFrom(list: Turn[]): ChatMessage[] {
  return list
    .filter((turn) => !turn.error && turn.content.trim().length > 0)
    .map((turn) => ({ role: turn.role, content: turn.content }))
}

export default function Playground() {
  const models = useAsync((signal) => listModels(signal), [])
  const modelList = models.data?.data ?? []

  const [model, setModel] = useState<string>(readStoredModel)
  const [draft, setDraft] = useState('')
  const [turns, setTurns] = useState<Turn[]>([])
  const [params, setParams] = useState<Params>(DEFAULT_PARAMS)
  const [paramsOpen, setParamsOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const [drawerOpen, setDrawerOpen] = useState(false)
  const [explain, setExplain] = useState<ExplainResult | undefined>(undefined)
  const [explainError, setExplainError] = useState<ApiError | undefined>(undefined)
  const [explainLoading, setExplainLoading] = useState(false)

  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const [atBottom, setAtBottom] = useState(true)

  useEffect(() => {
    try {
      localStorage.setItem(MODEL_KEY, model)
    } catch {
      /* storage unavailable */
    }
  }, [model])

  // Abort any in-flight stream when the page unmounts.
  useEffect(() => () => abortRef.current?.abort(), [])

  // --- autoscroll -------------------------------------------------------
  const handleScroll = useCallback(() => {
    const node = scrollRef.current
    if (!node) return
    const distance = node.scrollHeight - node.scrollTop - node.clientHeight
    setAtBottom(distance < 48)
  }, [])

  const scrollToBottom = useCallback(() => {
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
    setAtBottom(true)
  }, [])

  useEffect(() => {
    // Follow the stream only while the user is parked at the bottom.
    if (!atBottom) return
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [turns, atBottom])

  // --- request construction --------------------------------------------
  const buildRequest = useCallback(
    (messages: ChatMessage[], stream: boolean): ChatRequest => {
      const body: ChatRequest = { model, messages }
      if (stream) {
        body.stream = true
        body.stream_options = { include_usage: true }
      }
      if (params.temperature !== null) body.temperature = params.temperature
      if (params.maxTokens !== null) body.max_tokens = params.maxTokens
      return body
    },
    [model, params.maxTokens, params.temperature],
  )

  const patch = (id: string, changes: Partial<Turn>) => {
    setTurns((prev) => prev.map((turn) => (turn.id === id ? { ...turn, ...changes } : turn)))
  }

  const send = useCallback(async () => {
    const text = draft.trim()
    if (!text || busy) return

    const userTurn: Turn = { id: nextId(), role: 'user', content: text }
    const replyId = nextId()
    const replyTurn: Turn = { id: replyId, role: 'assistant', content: '', pending: true }

    const history = [...historyFrom(turns), { role: 'user' as const, content: text }]

    setTurns((prev) => [...prev, userTurn, replyTurn])
    setDraft('')
    setBusy(true)
    setAtBottom(true)

    const controller = new AbortController()
    abortRef.current = controller

    try {
      if (params.stream) {
        let buffered = ''
        await streamChat(
          buildRequest(history, true),
          {
            onRoute: (route: RouteInfo) => patch(replyId, { route }),
            onToken: (token: string) => {
              buffered += token
              patch(replyId, { content: buffered })
            },
            onUsage: (usage: Usage) => patch(replyId, { usage }),
          },
          controller.signal,
        )
        patch(replyId, { pending: false })
      } else {
        const result = await sendChat(buildRequest(history, false), controller.signal)
        patch(replyId, {
          content: result.content,
          route: result.route,
          ...(result.usage ? { usage: result.usage } : {}),
          pending: false,
        })
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        // Keep whatever streamed in before the user hit Stop.
        patch(replyId, { pending: false, stopped: true })
      } else {
        const message = err instanceof ApiError ? err.message : String(err)
        patch(replyId, { pending: false, error: message })
      }
    } finally {
      abortRef.current = null
      setBusy(false)
    }
  }, [buildRequest, busy, draft, params.stream, turns])

  const stop = useCallback(() => abortRef.current?.abort(), [])

  const clear = useCallback(() => {
    abortRef.current?.abort()
    setTurns([])
    setAtBottom(true)
  }, [])

  // --- explain ----------------------------------------------------------
  const runExplain = useCallback(async () => {
    setDrawerOpen(true)
    setExplainLoading(true)
    setExplain(undefined)
    setExplainError(undefined)

    const text = draft.trim()
    const messages = historyFrom(turns)
    if (text) messages.push({ role: 'user', content: text })
    // Nothing to profile yet — send a neutral probe so the drawer still shows
    // the default route rather than a validation error.
    if (!messages.length) messages.push({ role: 'user', content: 'Hello' })

    try {
      const result = await explainRoute({ model, messages })
      setExplain(result)
    } catch (err) {
      setExplainError(err instanceof ApiError ? err : new ApiError(String(err), 0))
    } finally {
      setExplainLoading(false)
    }
  }, [draft, model, turns])

  // --- render -----------------------------------------------------------
  if (models.loading && !models.data) {
    return (
      <div className="content content--pad">
        <Loading label="Loading models…" />
      </div>
    )
  }

  return (
    <div className="content content--flush">
      <div className="pg">
        <div className="pg__bar">
          <label className="route__key" htmlFor="pg-model">
            Model
          </label>
          <ModelPicker
            id="pg-model"
            className="select pg__model"
            models={modelList}
            value={model}
            onChange={setModel}
          />

          <button
            type="button"
            className="btn btn--sm"
            onClick={() => setParamsOpen((open) => !open)}
            aria-expanded={paramsOpen}
            aria-controls="pg-params"
          >
            Parameters
            <span
              aria-hidden
              style={{
                display: 'grid',
                placeItems: 'center',
                transform: paramsOpen ? 'rotate(180deg)' : 'none',
                transition: 'transform var(--transition)',
              }}
            >
              <IconChevron size={13} />
            </span>
          </button>

          <div className="pg__spacer" />

          <button type="button" className="btn btn--sm" onClick={runExplain}>
            <IconExplain size={13} />
            Explain route
          </button>
          <button
            type="button"
            className="btn btn--sm btn--danger"
            onClick={clear}
            disabled={turns.length === 0}
          >
            <IconTrash size={13} />
            Clear
          </button>
        </div>

        {paramsOpen ? (
          <div id="pg-params">
            <ParamsPanel params={params} onChange={setParams} />
          </div>
        ) : null}

        {models.error ? (
          <div className="content content--pad">
            <ErrorState error={models.error} what="the model list" onRetry={models.reload} />
          </div>
        ) : (
          <>
            <div className="pg__scroll" ref={scrollRef} onScroll={handleScroll}>
              <div className="pg__thread">
                {turns.length === 0 ? (
                  <div className="state">
                    <div className="state__title">Ready when you are</div>
                    <div className="state__body">
                      Send a prompt to watch DL Prism pick a provider. Every reply carries the routing
                      decision it was served by; <strong>Explain route</strong> shows the decision
                      without spending a request.
                    </div>
                  </div>
                ) : (
                  turns.map((turn) => <Message key={turn.id} turn={turn} />)
                )}
              </div>
            </div>

            {!atBottom ? (
              <button type="button" className="btn btn--sm pg__jump" onClick={scrollToBottom}>
                <IconArrowDown size={13} />
                Jump to latest
              </button>
            ) : null}

            <Composer
              value={draft}
              onChange={setDraft}
              onSend={send}
              onStop={stop}
              busy={busy}
              disabled={modelList.length === 0}
            />
          </>
        )}
      </div>

      <Drawer open={drawerOpen} title="Routing explanation" onClose={() => setDrawerOpen(false)}>
        {explainLoading ? <Loading label="Asking the router…" /> : null}
        {explainError ? (
          <ErrorState error={explainError} what="the routing explanation" onRetry={runExplain} />
        ) : null}
        {explain && !explainLoading ? (
          <>
            <p className="section__hint">
              Dry run for the current draft and conversation against{' '}
              <code className="mono">{model}</code>. No request is sent upstream.
            </p>
            <ExplainView result={explain} />
          </>
        ) : null}
      </Drawer>
    </div>
  )
}
