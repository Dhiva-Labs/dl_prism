import { IconProjects } from '../Icons'

/**
 * The workbench endpoints ship in a later gateway build than this dashboard
 * may be talking to. A 404 there is a version skew, not a fault — say so
 * plainly and leave the rest of the dashboard alone.
 */
export function WorkbenchUnavailable() {
  return (
    <div className="state">
      <IconProjects size={26} className="state__icon" />
      <div className="state__title">Workbench not available on this gateway</div>
      <div className="state__body">
        This build of <code className="mono">prism</code> doesn’t serve the project endpoints yet.
        Update prism to use projects, persistent chats, and the agent — every other page keeps
        working.
      </div>
    </div>
  )
}
