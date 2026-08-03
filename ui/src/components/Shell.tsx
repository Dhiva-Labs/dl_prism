import { NavLink, Outlet, useLocation } from 'react-router-dom'

import { useAuth, useTheme } from '../lib/hooks'
import { NAV } from '../nav'
import { ApiKeyField } from './ApiKeyField'
import { IconAlert, IconClose, IconMoon, IconSun, PrismGlyph } from './Icons'

function ThemeToggle() {
  const { theme, toggle } = useTheme()
  const next = theme === 'dark' ? 'light' : 'dark'
  return (
    <button
      type="button"
      className="btn btn--ghost btn--icon"
      onClick={toggle}
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
    >
      {theme === 'dark' ? <IconSun size={15} /> : <IconMoon size={15} />}
    </button>
  )
}

/** Appears on the first 401 from any endpoint; dismissable, reappears on the next 401. */
function AuthBanner() {
  const { authRequired, dismissed, dismiss } = useAuth()
  if (!authRequired || dismissed) return null

  return (
    <div className="banner" role="alert">
      <IconAlert size={16} className="banner__icon" />
      <span className="banner__text">
        The gateway rejected the request (401). Add the API key it was started with to continue.
      </span>
      <ApiKeyField compact />
      <button
        type="button"
        className="btn btn--ghost btn--icon"
        onClick={dismiss}
        aria-label="Dismiss API key prompt"
      >
        <IconClose size={14} />
      </button>
    </div>
  )
}

function Sidebar() {
  return (
    <nav className="sidebar" aria-label="Primary">
      <div className="sidebar__brand">
        <PrismGlyph size={20} className="sidebar__glyph" />
        <span className="sidebar__name">DL Prism</span>
      </div>

      <div className="sidebar__nav">
        {NAV.map((entry) => (
          <NavLink
            key={entry.path}
            to={entry.path}
            className={({ isActive }) => (isActive ? 'navlink navlink--active' : 'navlink')}
            title={entry.label}
          >
            <span className="navlink__icon">{entry.icon}</span>
            <span>{entry.label}</span>
          </NavLink>
        ))}
      </div>

      <div className="sidebar__footer">
        <span className="sidebar__version">one beam in — every model out</span>
        <ThemeToggle />
      </div>
    </nav>
  )
}

export function Shell() {
  const { pathname } = useLocation()
  const current = NAV.find((entry) => pathname.startsWith(entry.path)) ?? NAV[0]

  return (
    <div className="app">
      <Sidebar />
      <div className="main">
        <header className="topbar">
          <h1 className="topbar__title">{current?.label}</h1>
          <span className="topbar__subtitle">{current?.blurb}</span>
          <div className="topbar__spacer" />
        </header>
        <AuthBanner />
        <Outlet />
      </div>
    </div>
  )
}
