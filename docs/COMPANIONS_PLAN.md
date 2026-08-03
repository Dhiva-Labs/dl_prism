# DL Prism Companion Suite — Plan

The gateway is the product; companions put it inside every tool the user
already lives in (spec: "Routing API accessible through every interface").
One principle keeps this tractable: **every companion is a thin client of
the same REST API** — no companion gets its own routing logic.

## Integration matrix

| Surface            | Mechanism                                             | Wave |
|--------------------|-------------------------------------------------------|------|
| Web dashboard      | Embedded React app at `/`                             | 2 (in flight) |
| Codex CLI          | `OPENAI_BASE_URL=http://127.0.0.1:8130/v1` — config only | docs |
| Cursor / Continue / Windsurf | OpenAI-compatible base URL — config only     | docs |
| Claude Code (tools)| **MCP server**: `prism mcp` (stdio) with companion-model tools `prism_chat`, `prism_explain`, `prism_models`, `prism_usage` | 3 |
| Claude Code (model)| **Anthropic inbound**: `POST /v1/messages` translated onto the router, so `ANTHROPIC_BASE_URL` can point at Prism | 3 |
| VS Code            | Extension `integrations/vscode`: sidebar chat, status-bar spend/model, routing explain, model switcher | 3 |
| Chrome             | MV3 extension `integrations/chrome`: popup chat, context-menu "Ask Prism", options page | 3 |
| Desktop app        | Flutter shell `integrations/desktop`: manages the `prism serve` process, tray/launcher, wraps the dashboard | 4 |
| JetBrains          | Deferred — OpenAI-compatible base URL works in AI Assistant meanwhile | later |

## Wave 3 scopes

**MCP + Anthropic inbound (Go core, Opus)** — after the wave-2 backend agent
releases its files. `prism mcp` speaks MCP over stdio inside the existing
binary (no new deps if feasible; the protocol is JSON-RPC 2.0). Tools call
the local gateway HTTP API (base URL/key from config or flags) so the
gateway remains the single source of truth. `/v1/messages` accepts the
Anthropic Messages shape (incl. SSE) and translates onto the internal
OpenAI-shaped request path — the mirror of the existing outbound anthropic
adapter.

**VS Code (Sonnet)** — TypeScript + esbuild, no runtime deps. Sidebar
webview chat with streaming + route bar; status-bar item: month spend and
default model; commands: Ask About Selection, Explain Routing, Switch
Model; settings: baseUrl, apiKey, model. Packaged as .vsix, not published.

**Chrome (Sonnet)** — Manifest V3, vanilla TS/JS, no build deps beyond
esbuild if needed. Popup chat with streaming + model picker; context-menu
"Ask Prism about selection"; options page for base URL/key; host
permission only for the configured gateway origin.

## Wave 4

**Desktop (Flutter)** — window that manages the gateway lifecycle
(start/stop `prism serve`, show logs), quick usage glance, and opens the
dashboard. Reuses the user's existing Flutter toolchain. Built after the
dashboard is final so it can embed it.

## Docs

`docs/INTEGRATIONS.md` (wave 3) — copy-paste setup for every config-only
tool: Codex CLI, Cursor, Continue, Windsurf, Claude Code (both MCP and
base-URL modes), plus the extensions' install steps.
