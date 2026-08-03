# DL Prism

A local-first AI gateway. One OpenAI-compatible endpoint in front of every
model you use — cloud or local — with manual and automatic routing, policy
rules, failover, and budget-aware cost tracking. Single static Go binary.

```
any OpenAI-compatible tool ──► prism ──► Anthropic / OpenAI / DeepSeek /
        (base_url swap)                  Ollama / vLLM / LM Studio / ...
```

Because Prism speaks the OpenAI wire protocol, Claude Code, Cursor,
Windsurf, Continue, Codex CLI, and anything else with a configurable
`base_url` can use it unchanged — set the base URL to
`http://127.0.0.1:8130/v1` and pick a model (or `auto`).

## Quick start

```bash
go build ./cmd/prism
cp prism.example.yaml prism.yaml   # edit providers & keys
export ANTHROPIC_API_KEY=...       # keys come from the environment
./prism serve
```

```bash
curl http://127.0.0.1:8130/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Write a Python dedupe function"}]}'
```

The `model` field selects the routing mode:

| `model`              | Mode                                             |
|----------------------|--------------------------------------------------|
| `claude-sonnet`, ... | **Manual** — any model id or alias in the registry |
| `auto` (or empty)    | **Automatic** — classifier + your policy rules   |
| `pipeline:<name>`    | Hybrid pipelines (planned, M4)                   |

## Automatic routing

Prism classifies each request with fast heuristics (task kind, programming
language, image parts, estimated tokens — no LLM call) and walks your rules
top to bottom; first full match wins:

```yaml
routing:
  default: claude-sonnet
  rules:
    - if: {language: python}      # "IF Language = Python THEN Qwen Coder"
      then: qwen-coder
    - if: {task: security}
      then: claude-sonnet
    - if: {offline: true}
      then: local:any
    - if: {budget_exceeded: true}
      then: local:any
```

Every response reports the decision in `X-Prism-Provider`, `X-Prism-Model`,
`X-Prism-Mode`, and `X-Prism-Reason` headers, and you can dry-run any
request:

```bash
./prism explain "Audit this nginx config for vulnerabilities"
```

## Failover & health

Configure ordered fallback chains per model; Prism tries the next candidate
on connection errors, 429s, and 5xxs (never after streaming has started).
A circuit breaker demotes a provider after 3 consecutive failures for 30 s.

```yaml
  fallbacks:
    claude-sonnet: [gpt-5, deepseek, qwen-coder]
```

## Cost & budget

Per-model prices live in the config; usage is recorded to
`~/.local/share/prism/usage.jsonl` (token counts and costs only — never
prompt content). When `budget.monthly_usd` is spent, the
`budget_exceeded` rule condition fires — e.g. to force local models for the
rest of the month. Check spend with `./prism usage` or `GET /admin/usage`.

## CLI

```
prism serve   [-c config] [--listen addr] [--offline]
prism models                 # unified registry across all providers
prism explain [-m model] <prompt>
prism usage
prism mcp     [--gateway url] [--api-key key]   # MCP server over stdio
```

## Claude Code & MCP

Two ways in, both covered in **[docs/INTEGRATIONS.md](docs/INTEGRATIONS.md)**
along with Codex CLI, Cursor, Continue, Windsurf and the browser/editor
extensions:

```bash
claude mcp add dl-prism -- prism mcp     # companion models as tools
```

`prism mcp` speaks MCP over stdio and exposes `prism_chat`, `prism_explain`,
`prism_models` and `prism_usage`, so Claude Code can consult a cheaper, local
or specialised model mid-session. The tools call the running gateway over
HTTP, so routing, breaker and ledger state stay in one place.

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8130   # route Claude Code itself
export ANTHROPIC_API_KEY=dummy
```

`POST /v1/messages` accepts the Anthropic Messages shape — system blocks,
image content, SSE — translates it onto the same routing and failover path as
the OpenAI endpoint, and translates the answer back. (Tool definitions are
rejected with a 400 for now, matching the outbound `anthropic` adapter.)

## Web dashboard

`GET /` serves the built-in web dashboard (a React SPA built from `ui/` and
embedded in the binary — no separate install). Open
`http://127.0.0.1:8130` after `prism serve` for a playground, the model
registry, routing rules, usage charts, and gateway status. Static assets are
public; every data call it makes goes through the same bearer-key auth as
the rest of the API.

## API

- `POST /v1/chat/completions` — OpenAI-compatible, streaming and non-streaming
- `POST /v1/messages` — Anthropic Messages-compatible, streaming and non-streaming
- `POST /v1/messages/count_tokens` — token estimate for the Anthropic SDKs' preflight
- `GET  /v1/models` — unified registry (with `prism` extension metadata)
- `POST /v1/route/explain` — the routing decision for a request, without executing it
- `GET  /admin/usage` — current-month spend, per-model breakdown, and a 30-day daily series
- `GET  /admin/status` — version, uptime, offline mode, listen address, and per-provider health
- `GET  /admin/routing` — read-only view of the routing config (default, rules, fallbacks)
- `GET  /healthz`

## Providers

Two adapter types cover effectively everything:

- `type: openai` — OpenAI, DeepSeek, Qwen, Mistral, Groq, OpenRouter, and
  all local engines (Ollama, vLLM, LM Studio, llama.cpp server). Requests
  pass through untouched apart from the model field, so tools/function
  calling work end to end.
- `type: anthropic` — full translation to the Anthropic Messages API,
  including streaming. (Tool-call translation is not implemented yet.)

API keys are referenced via `api_key_env` (or `${VAR}` in `api_key`) and
never appear in logs or API responses. The server refuses to listen beyond
loopback unless client `api_keys` are configured.

## Docs

- [docs/SPEC.md](docs/SPEC.md) — the Intelligent AI Routing product spec
- [docs/PLAN.md](docs/PLAN.md) — implementation plan and milestones
- [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) — copy-paste setup for every client
- [docs/COMPANIONS_PLAN.md](docs/COMPANIONS_PLAN.md) — the companion-surface plan
- [docs/SPEC_REVIEW.md](docs/SPEC_REVIEW.md) — engineering review of the spec

## Status

v0.2.0 — M0–M3 of the plan plus the companion surfaces:

- **Routing**: manual + automatic modes, policy engine, failover with circuit
  breaker, cost ledger and budget condition, CLI.
- **Interfaces**: OpenAI `/v1/chat/completions`, Anthropic `/v1/messages`
  (both streaming), embedded web dashboard at `/`, admin API
  (`/admin/usage`, `/admin/status`, `/admin/routing`).
- **Companions**: `prism mcp` (MCP over stdio) for Claude Code and Codex, a
  VS Code extension and a Chrome extension in `integrations/`.

Planned next (M4): hybrid pipelines (`pipeline:<name>`), tool-call
translation for the anthropic adapter, config import/export, hardware-aware
routing, load-balancing weights, Flutter desktop shell.
