import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { ApiError, listModels } from '../lib/api'
import { formatNumber, formatRelative, formatTimestamp, parseTime } from '../lib/format'
import { useAsync } from '../lib/hooks'
import type { Project } from '../lib/types'
import {
  createProject,
  deleteProject,
  listProjects,
  projectSettings,
  updateProject,
} from '../lib/workbench'
import { Confirm, PromptDialog } from '../components/Dialog'
import { Drawer } from '../components/Drawer'
import { IconPencil, IconPlus, IconProjects, IconRefresh, IconSettings, IconTrash } from '../components/Icons'
import { ErrorState, Loading } from '../components/States'
import { ProjectSettings } from '../components/workbench/ProjectSettings'
import { WorkbenchUnavailable } from '../components/workbench/Unavailable'

function ProjectCard({
  project,
  onRename,
  onSettings,
  onDelete,
}: {
  project: Project
  onRename: () => void
  onSettings: () => void
  onDelete: () => void
}) {
  const settings = projectSettings(project)
  // Older gateways don't count chats for the list; say nothing rather than "0".
  const chats = typeof project.chat_count === 'number' ? project.chat_count : undefined

  return (
    <article className="pcard">
      <Link className="pcard__link" to={`/projects/${encodeURIComponent(project.id)}`}>
        <span className="pcard__glyph" aria-hidden>
          <IconProjects size={16} />
        </span>
        <h3 className="pcard__name">{project.name || 'Untitled project'}</h3>
        <div className="pcard__meta">
          {chats !== undefined ? (
            <>
              {chats === 1 ? '1 chat' : `${formatNumber(chats)} chats`}
              <span className="pcard__sep" aria-hidden>
                ·
              </span>
            </>
          ) : null}
          <span title={formatTimestamp(project.updated_at)}>
            {project.updated_at ? `updated ${formatRelative(project.updated_at)}` : 'new'}
          </span>
        </div>
        <div className="pcard__badges">
          {settings.agent_enabled ? (
            <span className="badge badge--accent">agent on</span>
          ) : (
            <span className="badge">chat only</span>
          )}
          {settings.model ? <span className="badge mono">{settings.model}</span> : null}
        </div>
      </Link>

      <div className="pcard__actions">
        <button
          type="button"
          className="btn btn--ghost btn--icon"
          aria-label={`Rename ${project.name}`}
          title="Rename"
          onClick={onRename}
        >
          <IconPencil size={14} />
        </button>
        <button
          type="button"
          className="btn btn--ghost btn--icon"
          aria-label={`Settings for ${project.name}`}
          title="Settings"
          onClick={onSettings}
        >
          <IconSettings size={14} />
        </button>
        <button
          type="button"
          className="btn btn--ghost btn--icon"
          aria-label={`Delete ${project.name}`}
          title="Delete"
          onClick={onDelete}
        >
          <IconTrash size={14} />
        </button>
      </div>
    </article>
  )
}

export default function Projects() {
  const projects = useAsync((signal) => listProjects(signal), [])
  const models = useAsync((signal) => listModels(signal), [])
  const navigate = useNavigate()

  const [createOpen, setCreateOpen] = useState(false)
  const [renaming, setRenaming] = useState<Project | undefined>(undefined)
  const [deleting, setDeleting] = useState<Project | undefined>(undefined)
  const [settingsFor, setSettingsFor] = useState<Project | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [dialogError, setDialogError] = useState<string | undefined>(undefined)

  const rows = useMemo(() => {
    const list = [...(projects.data ?? [])]
    list.sort((a, b) => {
      const at = parseTime(a.updated_at ?? a.created_at)?.getTime() ?? 0
      const bt = parseTime(b.updated_at ?? b.created_at)?.getTime() ?? 0
      if (at !== bt) return bt - at
      return (a.name || '').localeCompare(b.name || '')
    })
    return list
  }, [projects.data])

  const fail = (err: unknown) => setDialogError(err instanceof ApiError ? err.message : String(err))

  const create = async (name: string) => {
    setBusy(true)
    setDialogError(undefined)
    try {
      const project = await createProject(name)
      setCreateOpen(false)
      projects.reload()
      if (project?.id) navigate(`/projects/${encodeURIComponent(project.id)}`)
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const rename = async (name: string) => {
    if (!renaming) return
    setBusy(true)
    setDialogError(undefined)
    try {
      await updateProject(renaming.id, { name })
      setRenaming(undefined)
      projects.reload()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!deleting) return
    setBusy(true)
    setDialogError(undefined)
    try {
      await deleteProject(deleting.id)
      setDeleting(undefined)
      projects.reload()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const unavailable = projects.error?.isNotImplemented

  return (
    <div className="content content--pad">
      <div className="page">
        <section className="section">
          <div className="section__head">
            <h2 className="section__title">Projects</h2>
            <span className="section__hint">
              Persistent conversations, attached knowledge, and an agent that can act — scoped per
              project.
            </span>
            <div className="topbar__spacer" />
            <button type="button" className="btn btn--sm" onClick={projects.reload}>
              <IconRefresh size={13} />
              Refresh
            </button>
            <button
              type="button"
              className="btn btn--sm btn--primary"
              onClick={() => {
                setDialogError(undefined)
                setCreateOpen(true)
              }}
              disabled={Boolean(unavailable)}
            >
              <IconPlus size={13} />
              New project
            </button>
          </div>

          {projects.loading && !projects.data ? (
            <Loading label="Reading projects…" />
          ) : unavailable ? (
            <div className="card">
              <WorkbenchUnavailable />
            </div>
          ) : projects.error ? (
            <ErrorState error={projects.error} what="your projects" onRetry={projects.reload} />
          ) : rows.length === 0 ? (
            <div className="card">
              <div className="state">
                <IconProjects size={26} className="state__icon" />
                <div className="state__title">No projects yet</div>
                <div className="state__body">
                  A project keeps a set of conversations together with the files they draw on, so
                  the context survives between sessions. Give one an agent and it can also read,
                  write, and run commands inside its own workspace directory.
                </div>
                <button
                  type="button"
                  className="btn btn--primary"
                  onClick={() => {
                    setDialogError(undefined)
                    setCreateOpen(true)
                  }}
                >
                  <IconPlus size={14} />
                  Create the first project
                </button>
              </div>
            </div>
          ) : (
            <div className="pcards">
              {rows.map((project) => (
                <ProjectCard
                  key={project.id}
                  project={project}
                  onRename={() => {
                    setDialogError(undefined)
                    setRenaming(project)
                  }}
                  onSettings={() => setSettingsFor(project)}
                  onDelete={() => {
                    setDialogError(undefined)
                    setDeleting(project)
                  }}
                />
              ))}
            </div>
          )}
        </section>
      </div>

      <PromptDialog
        open={createOpen}
        title="New project"
        label="Project name"
        placeholder="Gateway rewrite"
        confirmLabel="Create"
        busy={busy}
        error={dialogError}
        onSubmit={create}
        onCancel={() => setCreateOpen(false)}
      />

      <PromptDialog
        open={Boolean(renaming)}
        title="Rename project"
        label="Project name"
        initialValue={renaming?.name ?? ''}
        confirmLabel="Rename"
        busy={busy}
        error={dialogError}
        onSubmit={rename}
        onCancel={() => setRenaming(undefined)}
      />

      <Confirm
        open={Boolean(deleting)}
        title="Delete project"
        body={
          <>
            <strong>{deleting?.name}</strong> is deleted with its chats, attached files, and agent
            workspace. This cannot be undone.
          </>
        }
        busy={busy}
        error={dialogError}
        onConfirm={remove}
        onCancel={() => setDeleting(undefined)}
      />

      <Drawer
        open={Boolean(settingsFor)}
        title={`${settingsFor?.name ?? 'Project'} settings`}
        onClose={() => setSettingsFor(undefined)}
      >
        {settingsFor ? (
          <ProjectSettings
            project={settingsFor}
            models={models.data?.data ?? []}
            onSaved={(updated) => {
              setSettingsFor(updated)
              projects.reload()
            }}
          />
        ) : null}
      </Drawer>
    </div>
  )
}
