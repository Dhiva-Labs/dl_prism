import { HashRouter, Navigate, Route, Routes } from 'react-router-dom'

import { Shell } from './components/Shell'
import Models from './pages/Models'
import Playground from './pages/Playground'
import Project from './pages/Project'
import Projects from './pages/Projects'
import Routing from './pages/Routing'
import Status from './pages/Status'
import Usage from './pages/Usage'

/**
 * HashRouter: the Go server hands out one index.html with no route fallback,
 * so all navigation stays client-side behind the `#`.
 */
export default function App() {
  return (
    <HashRouter>
      <Routes>
        <Route element={<Shell />}>
          <Route index element={<Navigate to="/playground" replace />} />
          <Route path="/projects" element={<Projects />} />
          {/* One splat route, not two paths: switching chats must not remount
              the project view (and refetch the chat list) on every click. */}
          <Route path="/projects/:projectId/*" element={<Project />} />
          <Route path="/playground" element={<Playground />} />
          <Route path="/models" element={<Models />} />
          <Route path="/routing" element={<Routing />} />
          <Route path="/usage" element={<Usage />} />
          <Route path="/status" element={<Status />} />
          <Route path="*" element={<Navigate to="/playground" replace />} />
        </Route>
      </Routes>
    </HashRouter>
  )
}
