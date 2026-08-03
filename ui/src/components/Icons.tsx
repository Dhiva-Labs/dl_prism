/**
 * Inline SVG icon set — no icon package, no network requests.
 * All icons inherit `currentColor` and default to a 16px box.
 */

interface IconProps {
  size?: number
  className?: string
}

function svgProps(size: number, className?: string) {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
    focusable: false,
    className,
  }
}

/** The DL Prism mark: a beam entering a prism and splitting on exit. */
export function PrismGlyph({ size = 20, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden
      focusable="false"
      className={className}
    >
      <path
        d="M12 3.5 21.5 20.5H2.5z"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinejoin="round"
      />
      <path d="M1 12h5.2" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" opacity="0.55" />
      <path d="M15.2 14.4H23" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" opacity="0.9" />
      <path d="M16 17.2h7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" opacity="0.6" />
      <path d="M16.8 20h6.2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" opacity="0.35" />
    </svg>
  )
}

export function IconPlayground({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 8.9 8.9 0 0 1-3.8-.9L3 20.5l1.6-4.9A8.4 8.4 0 0 1 12 3.1a8.4 8.4 0 0 1 9 8.4z" />
    </svg>
  )
}

export function IconModels({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <ellipse cx="12" cy="6" rx="8" ry="3.2" />
      <path d="M4 6v6c0 1.8 3.6 3.2 8 3.2s8-1.4 8-3.2V6" />
      <path d="M4 12v6c0 1.8 3.6 3.2 8 3.2s8-1.4 8-3.2v-6" />
    </svg>
  )
}

export function IconRouting({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <circle cx="5" cy="12" r="2.3" />
      <circle cx="19" cy="5.5" r="2.3" />
      <circle cx="19" cy="18.5" r="2.3" />
      <path d="M7.3 11.2 16.8 6.4M7.3 12.8l9.5 4.8" />
    </svg>
  )
}

export function IconUsage({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M4 20V10M10 20V4M16 20v-7M22 20H2" />
    </svg>
  )
}

export function IconStatus({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M2 12h4.5l2.5-7 4 14 2.6-7H22" />
    </svg>
  )
}

export function IconSun({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </svg>
  )
}

export function IconMoon({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z" />
    </svg>
  )
}

export function IconSend({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="m4 12 16-8-6 8 6 8z" />
    </svg>
  )
}

export function IconStop({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor" stroke="none" />
    </svg>
  )
}

export function IconCopy({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1" />
    </svg>
  )
}

export function IconCheck({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="m4 12.5 5 5L20 6.5" />
    </svg>
  )
}

export function IconTrash({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2M5.5 6l1 14a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1l1-14" />
    </svg>
  )
}

export function IconClose({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  )
}

export function IconRefresh({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M20.5 12a8.5 8.5 0 1 1-2.6-6.1" />
      <path d="M20.5 3.5V10H14" />
    </svg>
  )
}

export function IconChevron({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="m6 9 6 6 6-6" />
    </svg>
  )
}

export function IconArrowRight({ size = 14, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M4 12h15M13 6l6 6-6 6" />
    </svg>
  )
}

export function IconArrowDown({ size = 14, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M12 4v15M6 13l6 6 6-6" />
    </svg>
  )
}

export function IconExplain({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.6 9.2a2.5 2.5 0 1 1 3.3 2.4c-.6.2-.9.7-.9 1.3v.6" />
      <path d="M12 17h.01" />
    </svg>
  )
}

export function IconAlert({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M12 3.5 22 20.5H2z" />
      <path d="M12 9.5v4.5M12 17.5h.01" />
    </svg>
  )
}

export function IconInbox({ size = 22, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <path d="M3 13h5l1.5 3h5L16 13h5" />
      <path d="M5.5 4.5h13l2.5 8.5v5a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18v-5z" />
    </svg>
  )
}

export function IconKey({ size = 16, className }: IconProps) {
  return (
    <svg {...svgProps(size, className)}>
      <circle cx="8" cy="15" r="4" />
      <path d="m10.9 12.1 8.1-8.1M16.5 6.5l2 2M14 9l2 2" />
    </svg>
  )
}
