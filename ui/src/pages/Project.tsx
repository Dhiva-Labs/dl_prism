import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { ApiError, listModels } from '../lib/api'
import { formatRelative, formatTimestamp, parseTime } from '../lib/format'
import { useAsync } from '../lib/hooks'
import type { Chat, Project as ProjectRecord } from '../lib/types'
import { createChat, deleteChat, listChats, listProjects, projectSettings } from '../lib/workbench'
import { Confirm } from '../components/Dialog'
import { Drawer } from '../components/Drawer'
import {
  IconArrowLeft,
  IconPaperclip,
  IconPlus,
  IconSettings,
  IconTrash,
} from '../components/Icons'
import { ErrorState, Loading } from '../components/States'
import { Conversation } from '../components/workbench/Conversation'
import { KnowledgePanel } from '../components/workbench/KnowledgePanel'
import { ProjectSettings } from '../components/workbench/ProjectSettings'
import { WorkbenchUnavailable } from '../components/workbench/Unavailable'

function chatTime(chat: Chat): number {
  return parseTime(chat.updated_at ?? chat.created_at)?.getTime() ?? 0
}

export default function Project() {
  const params = useParams()
  const navigate = useNavigate()
  const projectId = params.projectId ?? ''
  // The route is a splat (see App.tsx); the only tail we render is a chat.
  const tail = params['*'] ?? ''
  const chatId = tail.startsWith('chats/') ? decodeURIComponent(tail.slice('chats/'.length)) : ''

  // The contract has no GET /admin/projects/{id}, so the record comes from
  // the list. Cheap: the list is small and already cached by the browser.
  const projects = useAsync((signal) => listProjects(signal), [])
  // The list is stamped with the project it belongs to: this view is not
  // remounted between projects, so a stale list must never be treated as this
  // project's — it would auto-open another project's conversation.
  const chats = useAsync(
    async (signal) => ({ projectId, list: await listChats(projectId, signal) }),
    [projectId],
  )
  const models = useAsync((signal) => listModels(signal), [])

  const [override, setOverride] = useState<ProjectRecord | undefined>(undefined)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [filesOpen, setFilesOpen] = useState(false)
  const [deleting, setDeleting] = useState<Chat | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)

  const project = useMemo(() => {
    const found = (projects.data ?? []).find((entry) => entry.id === projectId)
    if (override && override.id === projectId) return { ...found, ...override } as ProjectRecord
    return found
  }, [projects.data, projectId, override])

  const loaded = chats.data?.projectId === projectId ? chats.data.list : undefined

  const rows = useMemo(() => {
    const list = [...(loaded ?? [])]
    list.sort((a, b) => chatTime(b) - chatTime(a))
    return list
  }, [loaded])

  // Land on the most recent conversation rather than an empty right column.
  useEffect(() => {
    if (chatId || !loaded) return
    const first = rows[0]
    if (first) {
      navigate(`/projects/${encodeURIComponent(projectId)}/chats/${encodeURIComponent(first.id)}`, {
        replace: true,
      })
    }
  }, [chatId, loaded, rows, projectId, navigate])

  const fail = (err: unknown) => setError(err instanceof ApiError ? err.message : String(err))

  const newChat = async () => {
    setBusy(true)
    setError(undefined)
    try {
      const chat = await createChat(projectId)
      chats.reload()
      if (chat?.id) {
        navigate(`/projects/${encodeURIComponent(projectId)}/chats/${encodeURIComponent(chat.id)}`)
      }
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const removeChat = async () => {
    if (!deleting) return
    setBusy(true)
    setError(undefined)
    try {
      await deleteChat(deleting.id)
      const wasOpen = deleting.id === chatId
      setDeleting(undefined)
      chats.reload()
      if (wasOpen) navigate(`/projects/${encodeURIComponent(projectId)}`, { replace: true })
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const unavailable = projects.error?.isNotImplemented || chats.error?.isNotImplemented

  if (unavailable) {
    return (
      <div className="content content--pad">
        <div className="page">
          <div className="card">
            <WorkbenchUnavailable />
          </div>
        </div>
      </div>
    )
  }

  if (projects.loading && !projects.data) {
    return (
      <div className="content content--pad">
        <Loading label="Opening the project…" />
      </div>
    )
  }

  if (projects.error) {
    return (
      <div className="content content--pad">
        <ErrorState error={projects.error} what="this project" onRetry={projects.reload} />
      </div>
    )
  }

  if (!project) {
    return (
      <div className="content content--pad">
        <div className="page">
          <div className="card">
            <div className="state">
              <div className="state__title">That project is gone</div>
              <div className="state__body">
                It may have been deleted from another window or by editing the workbench directory
                directly.
              </div>
              <Link className="btn btn--sm" to="/projects">
                <IconArrowLeft size={13} />
                All projects
              </Link>
            </div>
          </div>
        </div>
      </div>
    )
  }

  const settings = projectSettings(project)
  const openChat = rows.find((chat) => chat.id === chatId)

  return (
    <div className="content content--flush">
      <div className="wb">
        <aside className="wb__rail" aria-label="Project">
          <div className="wb__railhead">
            <Link className="wb__back" to="/projects">
              <IconArrowLeft size={13} />
              All projects
            </Link>
            <h2 className="wb__projname" title={project.name}>
              {project.name || 'Untitled project'}
            </h2>
            <div className="chips">
              {settings.agent_enabled ? (
                <span className="badge badge--accent">agent on</span>
              ) : (
                <span className="badge">chat only</span>
              )}
              {settings.model ? <span className="badge mono">{settings.model}</span> : null}
            </div>
          </div>

          <div className="wb__railbar">
            <span className="route__key">Chats</span>
            <div className="pg__spacer" />
            <button
              type="button"
              className="btn btn--sm"
              onClick={newChat}
              disabled={busy}
              title="Start a new chat"
            >
              <IconPlus size={13} />
              New
            </button>
          </div>

          <div className="wb__chats">
            {!loaded && chats.loading ? (
              <Loading label="Loading chats…" />
            ) : chats.error ? (
              <ErrorState error={chats.error} what="this project's chats" onRetry={chats.reload} />
            ) : rows.length === 0 ? (
              <div className="state state--inline">
                <div className="state__body">
                  No chats yet. Start one — it is saved with the project.
                </div>
              </div>
            ) : (
              rows.map((chat) => {
                const active = chat.id === chatId
                return (
                  <div className={active ? 'chatrow chatrow--active' : 'chatrow'} key={chat.id}>
                    <Link
                      className="chatrow__link"
                      to={`/projects/${encodeURIComponent(projectId)}/chats/${encodeURIComponent(chat.id)}`}
                    >
                      <span className="chatrow__title">{chat.title || 'Untitled chat'}</span>
                      <span
                        className="chatrow__meta"
                        title={formatTimestamp(chat.updated_at ?? chat.created_at)}
                      >
                        {formatRelative(chat.updated_at ?? chat.created_at)}
                      </span>
                    </Link>
                    <button
                      type="button"
                      className="btn btn--ghost btn--icon chatrow__del"
                      aria-label={`Delete ${chat.title || 'chat'}`}
                      onClick={() => {
                        setError(undefined)
                        setDeleting(chat)
                      }}
                    >
                      <IconTrash size={13} />
                    </button>
                  </div>
                )
              })
            )}
          </div>

          {error ? (
            <div className="wb__railerror" role="alert">
              {error}
            </div>
          ) : null}

          <div className="wb__railfoot">
            <button type="button" className="btn btn--sm" onClick={() => setFilesOpen(true)}>
              <IconPaperclip size={13} />
              Knowledge
            </button>
            <button type="button" className="btn btn--sm" onClick={() => setSettingsOpen(true)}>
              <IconSettings size={13} />
              Settings
            </button>
          </div>
        </aside>

        {chatId ? (
          <Conversation
            key={chatId}
            chatId={chatId}
            title={openChat?.title ?? ''}
            project={project}
            models={models.data?.data ?? []}
            onOpenSettings={() => setSettingsOpen(true)}
            onRunFinished={chats.reload}
          />
        ) : (
          <div className="wb__conv">
            <div className="wb__convscroll">
              <div className="state">
                <div className="state__title">Start a conversation</div>
                <div className="state__body">
                  Chats in <strong>{project.name}</strong> keep their history, their attached files,
                  and — when you switch tools on — everything the agent did.
                </div>
                <button type="button" className="btn btn--primary" onClick={newChat} disabled={busy}>
                  <IconPlus size={14} />
                  New chat
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      <Confirm
        open={Boolean(deleting)}
        title="Delete chat"
        body={
          <>
            <strong>{deleting?.title || 'This chat'}</strong> and its full transcript are removed.
            The project’s files and workspace are untouched.
          </>
        }
        busy={busy}
        error={error}
        onConfirm={removeChat}
        onCancel={() => setDeleting(undefined)}
      />

      <Drawer open={filesOpen} title="Project knowledge" onClose={() => setFilesOpen(false)}>
        {filesOpen ? <KnowledgePanel projectId={projectId} /> : null}
      </Drawer>

      <Drawer
        open={settingsOpen}
        title={`${project.name} settings`}
        onClose={() => setSettingsOpen(false)}
      >
        {settingsOpen ? (
          <ProjectSettings
            project={project}
            models={models.data?.data ?? []}
            onSaved={(updated) => {
              setOverride(updated)
              projects.reload()
            }}
          />
        ) : null}
      </Drawer>
    </div>
  )
}
