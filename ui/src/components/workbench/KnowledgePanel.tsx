import { useRef, useState } from 'react'

import { ApiError } from '../../lib/api'
import { formatBytes, formatRelative } from '../../lib/format'
import { useAsync } from '../../lib/hooks'
import type { KnowledgeFile } from '../../lib/types'
import { deleteFile, listFiles, uploadFile } from '../../lib/workbench'
import { Confirm } from '../Dialog'
import { IconFile, IconPaperclip, IconTrash } from '../Icons'
import { Empty, ErrorState, Loading } from '../States'

/** Matches the gateway's fold-into-context rule (docs/WORKBENCH_PLAN.md). */
const CONTEXT_LIMIT = 32 * 1024

function joinsContext(file: KnowledgeFile): boolean {
  if (typeof file.in_context === 'boolean') return file.in_context
  return (file.size ?? 0) > 0 && (file.size ?? 0) <= CONTEXT_LIMIT
}

export function KnowledgePanel({ projectId }: { projectId: string }) {
  const files = useAsync((signal) => listFiles(projectId, signal), [projectId])
  const inputRef = useRef<HTMLInputElement | null>(null)

  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)
  const [pendingDelete, setPendingDelete] = useState<KnowledgeFile | undefined>(undefined)

  const attach = async (list: FileList | null) => {
    if (!list?.length) return
    setBusy(true)
    setError(undefined)
    try {
      for (const file of Array.from(list)) {
        await uploadFile(projectId, file)
      }
      files.reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
      if (inputRef.current) inputRef.current.value = ''
    }
  }

  const remove = async () => {
    if (!pendingDelete) return
    setBusy(true)
    setError(undefined)
    try {
      await deleteFile(projectId, pendingDelete.name)
      setPendingDelete(undefined)
      files.reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const rows = files.data ?? []

  return (
    <>
      <p className="section__hint">
        Attached files live with the project. Text files under 32 KB are folded into the system
        context automatically; anything larger stays on disk for the agent’s tools to read.
      </p>

      <div className="wb__drawerfoot">
        <button
          type="button"
          className="btn btn--primary btn--sm"
          onClick={() => inputRef.current?.click()}
          disabled={busy}
        >
          <IconPaperclip size={13} />
          {busy ? 'Working…' : 'Attach files'}
        </button>
        <input
          ref={inputRef}
          type="file"
          multiple
          className="visually-hidden"
          onChange={(event) => attach(event.target.files)}
        />
      </div>

      {error ? (
        <div className="modal__error" role="alert">
          {error}
        </div>
      ) : null}

      {files.loading && !files.data ? (
        <Loading label="Reading attachments…" />
      ) : files.error ? (
        <ErrorState error={files.error} what="attached files" onRetry={files.reload} />
      ) : rows.length === 0 ? (
        <Empty
          title="Nothing attached yet"
          body="Add notes, specs, or data files and every chat in this project can use them."
        />
      ) : (
        <div className="files">
          {rows.map((file) => (
            <div className="file" key={file.name}>
              <IconFile size={15} className="file__glyph" />
              <div className="file__body">
                <div className="file__name mono">{file.name}</div>
                <div className="file__meta">
                  {formatBytes(file.size)}
                  {file.modified ?? file.modified_at
                    ? ` · ${formatRelative(file.modified ?? file.modified_at)}`
                    : ''}
                </div>
              </div>
              {joinsContext(file) ? (
                <span className="badge badge--accent" title="Folded into the system context">
                  in context
                </span>
              ) : (
                <span className="badge" title="Readable by the agent's tools">
                  on disk
                </span>
              )}
              <button
                type="button"
                className="btn btn--ghost btn--icon"
                aria-label={`Remove ${file.name}`}
                onClick={() => setPendingDelete(file)}
              >
                <IconTrash size={14} />
              </button>
            </div>
          ))}
        </div>
      )}

      <Confirm
        open={Boolean(pendingDelete)}
        title="Remove attachment"
        body={
          <>
            <code className="mono">{pendingDelete?.name}</code> is deleted from the project’s files
            directory. Chats that already referenced it keep their transcripts.
          </>
        }
        confirmLabel="Remove"
        busy={busy}
        onConfirm={remove}
        onCancel={() => setPendingDelete(undefined)}
      />
    </>
  )
}
