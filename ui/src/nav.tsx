import type { ReactNode } from 'react'

import {
  IconModels,
  IconPlayground,
  IconRouting,
  IconStatus,
  IconUsage,
} from './components/Icons'

export interface NavEntry {
  path: string
  label: string
  /** Shown next to the page title in the top bar. */
  blurb: string
  icon: ReactNode
}

export const NAV: NavEntry[] = [
  {
    path: '/playground',
    label: 'Playground',
    blurb: 'Chat against the gateway and see every routing decision',
    icon: <IconPlayground />,
  },
  {
    path: '/models',
    label: 'Models',
    blurb: 'Everything the gateway can route to',
    icon: <IconModels />,
  },
  {
    path: '/routing',
    label: 'Routing',
    blurb: 'Policy rules, fallbacks, and a dry-run explainer',
    icon: <IconRouting />,
  },
  {
    path: '/usage',
    label: 'Usage',
    blurb: 'Spend against budget over the last 30 days',
    icon: <IconUsage />,
  },
  {
    path: '/status',
    label: 'Status',
    blurb: 'Gateway health and dashboard settings',
    icon: <IconStatus />,
  },
]
