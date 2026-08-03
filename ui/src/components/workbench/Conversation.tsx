import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError } from '../../lib/api'
import { useAsync } from '../../lib/hooks'
import type { Model, Project, RouteInfo, Usage } from '../../lib/types'
import { approveTool, getChat, projectSettings, streamAgentRun } from '../../lib/workbench'
import { IconArrowDown, IconSettings, IconTool } from '../Icons'
import { ModelPicker } from '../ModelPicker'
import { Composer } from '../playground/Composer'
import { Message } from '../playground/Message'
import { ErrorState, Loading } from '../States'
import { ToolCard } from './ToolCard'
import {
  appendDelta,
  awaitApproval,
  closeRun,
  markDecision,
  message,
  settleTool,
  startTool,
  timelineFromChat,
  type TimelineItem,
} from './timeline'

export function Conversation({
  chatId,
  title,
  project,
  models,
  onOpenSettings,
  onRunFinished,
}: {
  chatId: string
  title: string
  project: Project
  models: Model[]
  onOpenSettings: () => void
  onRunFinished: () => void
}) {
  const settings = projectSettings(project)

  const chat = useAsync((signal) => getChat(chatId, signal), [chatId])
  const [items, setItems] = useState<TimelineItem[]>([])
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | undefined>(undefined)
  const [model, setModel] = useState(settings.model || 'auto')

  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const [atBottom, setAtBottom] = useState(true)

  // Replay the stored log whenever a different chat is opened.
  useEffect(() => {
    setItems(timelineFromChat(chat.data))
    setNotice(undefined)
    setAtBottom(true)
  }, [chat.data])

  // The project's default model wins until the user overrides it here.
  useEffect(() => {
    setModel(settings.model || 'auto')
  }, [project.id, settings.model])

  // Never leave a stream running behind a closed chat.
  useEffect(() => () => abortRef.current?.abort(), [chatId])

  const handleScroll = useCallback(() => {
    const node = scrollRef.current
    if (!node) return
    setAtBottom(node.scrollHeight - node.scrollTop - node.clientHeight < 48)
  }, [])

  const scrollToBottom = useCallback(() => {
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
    setAtBottom(true)
  }, [])

  useEffect(() => {
    if (!atBottom) return
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [items, atBottom])

  // --- the run ----------------------------------------------------------
  const send = useCallback(async () => {
    const text = draft.trim()
    if (!text || busy) return

    setItems((prev) => [...prev, message('user', text), message('assistant', '', { pending: true })])
    setDraft('')
    setBusy(true)
    setNotice(undefined)
    setAtBottom(true)

    const controller = new AbortController()
    abortRef.current = controller

    let route: RouteInfo | undefined
    let usage: Usage | undefined
    let streamError: string | undefined

    try {
      await streamAgentRun(
        { chat_id: chatId, message: text, ...(model && model !== 'auto' ? { model } : {}) },
        {
          onRoute: (next) => {
            if (next.provider || next.model) route = next
          },
          onDelta: (delta) => setItems((prev) => appendDelta(prev, delta)),
          onToolCall: (call) => setItems((prev) => startTool(prev, call)),
          onAwaitApproval: (id) => setItems((prev) => awaitApproval(prev, id)),
          onToolResult: (result) => setItems((prev) => settleTool(prev, result)),
          onDone: (next) => {
            usage = next
          },
          // An in-band error keeps everything that already streamed in.
          onError: (msg) => {
            streamError = msg
          },
        },
        controller.signal,
      )
      setItems((prev) => closeRun(prev, { usage, route, error: streamError }))
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        setItems((prev) => closeRun(prev, { stopped: true, route }))
      } else {
        // Key the busy case on the status: the gateway's wording may change.
        const detail =
          err instanceof ApiError && err.isConflict
            ? 'A run is already active in this chat. Wait for it to finish, or open a new chat.'
            : err instanceof ApiError
              ? err.message
              : String(err)
        setItems((prev) => closeRun(prev, { error: detail, route }))
      }
    } finally {
      abortRef.current = null
      setBusy(false)
      onRunFinished()
    }
  }, [busy, chatId, draft, model, onRunFinished])

  const stop = useCallback(() => abortRef.current?.abort(), [])

  const decide = useCallback(
    async (callId: string, approve: boolean) => {
      setItems((prev) => markDecision(prev, callId, { decisionPending: true }))
      try {
        await approveTool({ chat_id: chatId, tool_call_id: callId, approve })
        setItems((prev) =>
          markDecision(prev, callId, {
            decisionPending: false,
            state: approve ? 'approved' : 'denied',
          }),
        )
      } catch (err) {
        const detail = err instanceof ApiError ? err.message : String(err)
        setNotice(`Couldn't send the decision: ${detail}`)
        setItems((prev) => markDecision(prev, callId, { decisionPending: false, state: 'awaiting' }))
      }
    },
    [chatId],
  )

  // --- render -----------------------------------------------------------
  const modeLabel = settings.agent_enabled
    ? `tools on · approval: ${settings.approval_mode}`
    : 'tools off'

  return (
    <div className="wb__conv">
      <div className="wb__topline">
        <h2 className="wb__convtitle">{title || 'Untitled chat'}</h2>
        <span className={settings.agent_enabled ? 'badge badge--accent' : 'badge'}>{modeLabel}</span>
        <div className="pg__spacer" />
        {busy ? (
          <span className="working" role="status">
            <span className="working__dot" aria-hidden />
            agent working
          </span>
        ) : null}
        <label className="route__key" htmlFor="wb-model">
          Model
        </label>
        <ModelPicker
          id="wb-model"
          className="select wb__model"
          models={models}
          value={model}
          onChange={setModel}
        />
      </div>

      {chat.loading && !chat.data ? (
        <div className="wb__convscroll">
          <Loading label="Opening the conversation…" />
        </div>
      ) : chat.error && items.length === 0 ? (
        <div className="wb__convscroll">
          <ErrorState error={chat.error} what="this conversation" onRetry={chat.reload} />
        </div>
      ) : (
        <>
          <div className="wb__convscroll" ref={scrollRef} onScroll={handleScroll}>
            <div className="pg__thread">
              {items.length === 0 ? (
                <div className="state">
                  <div className="state__title">A fresh conversation</div>
                  <div className="state__body">
                    Messages here are kept with the project, and{' '}
                    {settings.agent_enabled
                      ? 'the agent can use its tools inside this project’s workspace.'
                      : 'replies stay chat-only until you switch tools on in project settings.'}
                  </div>
                </div>
              ) : (
                items.map((item) =>
                  item.kind === 'tool' ? (
                    <ToolCard key={item.id} tool={item} onDecide={decide} />
                  ) : (
                    <Message
                      key={item.id}
                      turn={{
                        id: item.id,
                        role: item.role,
                        content: item.text,
                        route: item.route,
                        usage: item.usage,
                        error: item.error,
                        pending: item.pending,
                        stopped: item.stopped,
                      }}
                    />
                  ),
                )
              )}
            </div>
          </div>

          {!atBottom ? (
            <button type="button" className="btn btn--sm pg__jump" onClick={scrollToBottom}>
              <IconArrowDown size={13} />
              Jump to latest
            </button>
          ) : null}

          {notice ? (
            <div className="wb__notice wb__notice--warn" role="alert">
              <span>{notice}</span>
            </div>
          ) : null}

          {!settings.agent_enabled ? (
            <div className="wb__notice">
              <IconTool size={14} />
              <span>
                Tools are off for this project — replies are chat only, nothing is read, written, or
                run.
              </span>
              <button type="button" className="btn btn--sm" onClick={onOpenSettings}>
                <IconSettings size={13} />
                Project settings
              </button>
            </div>
          ) : null}

          <Composer
            value={draft}
            onChange={setDraft}
            onSend={send}
            onStop={stop}
            busy={busy}
            disabled={busy}
            placeholder={busy ? 'The agent is working — Stop to interrupt it.' : 'Send a message…'}
          />
        </>
      )}
    </div>
  )
}
