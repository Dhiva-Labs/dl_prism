import type { Model } from '../lib/types'

/** "auto" always sorts first; everything else alphabetically by id. */
export function sortModels(models: Model[]): Model[] {
  return [...models].sort((a, b) => {
    if (a.id === 'auto') return -1
    if (b.id === 'auto') return 1
    return a.id.localeCompare(b.id)
  })
}

function optionLabel(model: Model): string {
  if (model.id === 'auto') return 'auto — route by policy'
  const local = model.prism?.local
  return `${model.id} (${local ? 'local' : 'cloud'})`
}

export function ModelPicker({
  models,
  value,
  onChange,
  id = 'model-picker',
  className = 'select',
  disabled = false,
  label = 'Model',
}: {
  models: Model[]
  value: string
  onChange: (next: string) => void
  id?: string
  className?: string
  disabled?: boolean
  label?: string
}) {
  const sorted = sortModels(models)
  // Keep the selection visible even if the gateway no longer advertises it.
  const known = sorted.some((m) => m.id === value)

  return (
    <select
      id={id}
      className={className}
      value={value}
      aria-label={label}
      disabled={disabled || sorted.length === 0}
      onChange={(e) => onChange(e.target.value)}
    >
      {sorted.length === 0 ? <option value="">No models available</option> : null}
      {!known && value ? <option value={value}>{value} (unavailable)</option> : null}
      {sorted.map((model) => (
        <option key={model.id} value={model.id}>
          {optionLabel(model)}
        </option>
      ))}
    </select>
  )
}
