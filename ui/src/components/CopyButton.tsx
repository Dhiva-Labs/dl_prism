import { useCopy } from '../lib/hooks'
import { IconCheck, IconCopy } from './Icons'

export function CopyButton({
  text,
  label = 'Copy',
  className = 'btn btn--ghost btn--icon',
}: {
  text: string
  label?: string
  className?: string
}) {
  const [copied, copy] = useCopy()
  return (
    <button
      type="button"
      className={className}
      onClick={() => copy(text)}
      aria-label={copied ? 'Copied' : label}
      title={copied ? 'Copied' : label}
    >
      {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
    </button>
  )
}
