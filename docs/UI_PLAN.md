# DL Prism Web UI — Plan

Production dashboard for the gateway, embedded in the `prism` binary so the
single-binary deployment story is unchanged: `prism serve` → open the listen
address → full UI. Replaces the v0.1 test console.

## Stack

- **Vite + React + TypeScript** in `ui/`, built to `ui/dist`
- Embedded via `go:embed` (`ui/embed.go`, imported by `internal/server`)
- **HashRouter** — no server-side route fallback needed
- Hand-rolled CSS design system (no UI kit, no external fonts/CDNs —
  everything must work fully offline); dark theme default, light supported
- Only runtime deps: `react`, `react-dom`, `react-router-dom`

## Pages

| Page       | Contents                                                                          |
|------------|-----------------------------------------------------------------------------------|
| Playground | Streaming chat, model picker, request params, per-reply route transparency, explain drawer |
| Models     | Registry table: provider, local badge, context window, capabilities, speed, cost, live health |
| Routing    | Ordered rule list (condition → target), default model, fallback chains, interactive dry-run |
| Usage      | Month spend vs budget, daily cost bar chart (30 days), per-model breakdown        |
| Status     | Gateway version, uptime, online/offline mode, listen address, provider health     |

## Backend additions (v0.2)

- `GET /admin/status` — `{version, uptime_seconds, offline, listen,
  providers: [{name, type, local, healthy, models}]}` (healthy = circuit
  breaker state)
- `GET /admin/usage` — gains `daily: [{date, cost_usd, requests}]`, last 30 days
- `GET /admin/routing` — read-only view of the routing config:
  `{default, rules: [{if, then}], fallbacks}` (conditions rendered via
  `Condition.Describe()`)
- SPA served at `/`; test console (`console.go` / `console.html`) removed

Auth model unchanged: static UI assets are public, every data endpoint sits
behind the optional bearer keys; the UI stores a key locally and sends it.

## Team split

- **Opus** — the entire React app (`ui/` only)
- **Sonnet** — backend endpoints, SPA serving, tests (Go only)
- **Integrator** — embed pre-wiring, build pipeline, end-to-end verification
  in the browser, version bump to 0.2.0
