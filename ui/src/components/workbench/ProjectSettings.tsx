import { useEffect, useState } from 'react'

import { ApiError } from '../../lib/api'
import type { ApprovalMode, Model, Project } from '../../lib/types'
import { projectSettings, updateProject } from '../../lib/workbench'
import { ModelPicker } from '../ModelPicker'

/** One line each — the whole point is that the user can choose without docs. */
const APPROVAL_MODES: Array<{ value: ApprovalMode; label: string; hint: string }> = [
  {
    value: 'always',
    label: 'Always ask',
    hint: 'Every tool call waits for you, including reads.',
  },
  {
    value: 'mutations',
    label: 'Ask before changes',
    hint: 'Reads run freely; writes and shell commands wait for you.',
  },
  {
    value: 'never',
    label: 'Never ask',
    hint: 'Tools run unattended inside this project’s workspace.',
  },
]

export function ProjectSettings({
  project,
  models,
  onSaved,
}: {
  project: Project
  models: Model[]
  onSaved: (project: Project) => void
}) {
  const initial = projectSettings(project)

  const [name, setName] = useState(project.name)
  const [agentEnabled, setAgentEnabled] = useState(initial.agent_enabled)
  const [approvalMode, setApprovalMode] = useState<ApprovalMode>(initial.approval_mode)
  const [model, setModel] = useState(initial.model || 'auto')
  const [systemPrompt, setSystemPrompt] = useState(initial.system_prompt)

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)
  const [saved, setSaved] = useState(false)

  // Re-seed when the drawer is reopened against a different project.
  useEffect(() => {
    const next = projectSettings(project)
    setName(project.name)
    setAgentEnabled(next.agent_enabled)
    setApprovalMode(next.approval_mode)
    setModel(next.model || 'auto')
    setSystemPrompt(next.system_prompt)
    setError(undefined)
    setSaved(false)
  }, [project])

  const save = async () => {
    setSaving(true)
    setError(undefined)
    try {
      const updated = await updateProject(project.id, {
        name: name.trim() || project.name,
        settings: {
          agent_enabled: agentEnabled,
          approval_mode: approvalMode,
          model: model === 'auto' ? '' : model,
          system_prompt: systemPrompt,
        },
      })
      // A gateway that answers 204 leaves us to merge the patch locally.
      onSaved(
        updated?.id
          ? updated
          : {
              ...project,
              name: name.trim() || project.name,
              settings: {
                agent_enabled: agentEnabled,
                approval_mode: approvalMode,
                model: model === 'auto' ? '' : model,
                system_prompt: systemPrompt,
              },
            },
      )
      setSaved(true)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="field">
        <label className="field__label" htmlFor="ps-name">
          Name
        </label>
        <input
          id="ps-name"
          className="input"
          value={name}
          onChange={(event) => {
            setName(event.target.value)
            setSaved(false)
          }}
        />
      </div>

      <div className="field">
        <span className="field__label">Agent tools</span>
        <label className="switch">
          <input
            type="checkbox"
            checked={agentEnabled}
            onChange={(event) => {
              setAgentEnabled(event.target.checked)
              setSaved(false)
            }}
          />
          <span className="switch__track" aria-hidden />
          <span>{agentEnabled ? 'On — the agent can act' : 'Off — chat only'}</span>
        </label>
        <span className="field__hint">
          Tools read and write inside this project’s own workspace directory and cannot reach outside
          it.
        </span>
      </div>

      <div className="field">
        <span className="field__label">Approval mode</span>
        <div className="radios">
          {APPROVAL_MODES.map((mode) => (
            <label
              key={mode.value}
              className={approvalMode === mode.value ? 'radio radio--on' : 'radio'}
            >
              <input
                type="radio"
                name="approval-mode"
                value={mode.value}
                checked={approvalMode === mode.value}
                disabled={!agentEnabled}
                onChange={() => {
                  setApprovalMode(mode.value)
                  setSaved(false)
                }}
              />
              <span className="radio__body">
                <span className="radio__label">{mode.label}</span>
                <span className="radio__hint">{mode.hint}</span>
              </span>
            </label>
          ))}
        </div>
        {!agentEnabled ? (
          <span className="field__hint">Switch tools on to choose how they are approved.</span>
        ) : null}
      </div>

      <div className="field">
        <label className="field__label" htmlFor="ps-model">
          Default model
        </label>
        <ModelPicker
          id="ps-model"
          models={models}
          value={model}
          onChange={(next) => {
            setModel(next)
            setSaved(false)
          }}
        />
        <span className="field__hint">
          Used for every chat in this project unless the conversation overrides it. Tool calling
          needs a tool-capable model.
        </span>
      </div>

      <div className="field">
        <label className="field__label" htmlFor="ps-prompt">
          System prompt
        </label>
        <textarea
          id="ps-prompt"
          className="textarea"
          rows={6}
          style={{ resize: 'vertical' }}
          value={systemPrompt}
          placeholder="Standing instructions for every chat in this project…"
          onChange={(event) => {
            setSystemPrompt(event.target.value)
            setSaved(false)
          }}
        />
        <span className="field__hint">
          Prepended to every conversation, alongside any small text files attached as knowledge.
        </span>
      </div>

      {error ? (
        <div className="modal__error" role="alert">
          {error}
        </div>
      ) : null}

      <div className="wb__drawerfoot">
        {saved && !error ? <span className="field__hint">Saved.</span> : null}
        <div className="pg__spacer" />
        <button type="button" className="btn btn--primary" onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save settings'}
        </button>
      </div>
    </>
  )
}
