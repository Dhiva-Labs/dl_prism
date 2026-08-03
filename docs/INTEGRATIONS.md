# Integrations

Copy-paste setup for every surface Prism plugs into. All of them assume the
gateway is running:

```bash
prism serve            # http://127.0.0.1:8130 by default
```

Provider and model setup lives in your config — start from
[prism.example.yaml](../prism.example.yaml) and copy it to `./prism.yaml` or
`~/.config/prism/prism.yaml`.

**About API keys.** If `server.api_keys` in your config is empty (the default
for a loopback-only gateway), Prism accepts any client key — but most clients
*insist* on sending something, so put a dummy value in. If you did configure
`api_keys`, send one of those values instead. Prism accepts it as either
`Authorization: Bearer <key>` or `x-api-key: <key>`.

---

## Claude Code — as tools (MCP)

`prism mcp` is an MCP server over stdio. It exposes the gateway's companion
models as tools, so Claude Code can consult a cheaper, local, or specialised
model without leaving the session.

```bash
claude mcp add dl-prism -- prism mcp
```

Or check it into the project as `.mcp.json`:

```json
{
  "mcpServers": {
    "dl-prism": {
      "command": "prism",
      "args": ["mcp", "--gateway", "http://127.0.0.1:8130"],
      "env": { "PRISM_API_KEY": "" }
    }
  }
}
```

Use the absolute path to the binary (`/usr/local/bin/prism`, `./prism`) if it
is not on the PATH your MCP host uses.

| Flag / env | Meaning |
|------------|---------|
| `--gateway URL`, `$PRISM_GATEWAY` | Gateway base URL (default `http://127.0.0.1:8130`) |
| `--api-key KEY`, `$PRISM_API_KEY` | Sent as `Authorization: Bearer …` when set |

Tools:

| Tool | Arguments | Returns |
|------|-----------|---------|
| `prism_chat` | `prompt` (required), `model` (default `auto`), `system` | The model's reply plus a `— routed provider/model (mode: reason)` footer |
| `prism_explain` | `prompt` (required), `model` | The routing decision, classifier profile and failover chain as JSON — no upstream call |
| `prism_models` | — | Table of model id, provider, local, context window |
| `prism_usage` | — | This month's spend, the budget, and per-model token/cost lines |

The tools are thin HTTP clients of the *running* gateway, so routing rules,
circuit-breaker state and the usage ledger stay in one place. If the gateway
is not running, a tool call comes back as a tool error telling you so — it
does not break the MCP session.

## Claude Code — as the model (ANTHROPIC_BASE_URL)

Prism also speaks the Anthropic Messages API on the way *in*, so Claude Code
can send its own traffic through the gateway and be routed by your rules:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8130
export ANTHROPIC_API_KEY=dummy          # or one of your configured api_keys
export ANTHROPIC_MODEL=auto             # or any model id/alias from prism_models
claude
```

`POST /v1/messages` (streaming and non-streaming) and
`POST /v1/messages/count_tokens` are implemented. The request is translated
onto Prism's internal request shape, routed and failed over exactly like an
OpenAI-format request, then translated back to Anthropic events.

Current limitation: **tool definitions are rejected with a 400**, the same
limitation the outbound `type: anthropic` adapter has. Claude Code leans on
tools heavily, so this mode suits prompt-only use; use the MCP mode above for
day-to-day agentic work, or point Claude Code at a real Anthropic key and let
Prism serve your *other* tools.

---

## Codex CLI

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8130/v1
export OPENAI_API_KEY=dummy
codex --model auto
```

---

## Use your subscription as a provider

Everything above points a client *at* Prism. This points Prism *at* a CLI.

If you already pay for Claude Pro/Max or a ChatGPT plan, the logged-in CLI on
your machine is a model endpoint you have bought and are probably
under-using. Prism can route to it like any other provider: it runs the CLI's
official headless mode once per request and translates the JSONL events it
prints into OpenAI chat completions. Nothing is billed per token — the calls
come out of your plan.

```yaml
providers:
  - name: claude-subscription
    type: claude-code
    models:
      - id: sonnet                 # sonnet | opus | haiku, or a full model name
        aliases: [claude-sub]
        context_window: 200000
        capabilities: [code, reasoning]

  - name: codex-subscription
    type: codex-cli
    models:
      - id: gpt-5.1-codex
        aliases: [codex-sub]
        context_window: 400000
```

Neither type takes a `base_url` or a key — the CLI owns its endpoint and its
credentials — and both are rejected if you set one. Requirements: the CLI is
installed and already logged in (`claude` / `codex`, verify with a manual
`claude -p hi`). If it is not on the `PATH` the gateway process sees, name it:

```yaml
    command: [/home/you/.local/bin/claude]     # or [npx, "@anthropic-ai/claude-code"]
```

### The overflow-valve pattern

Leave `cost` at its default of zero, because a subscription is flat-rate.
That makes these providers the obvious place for spend to go when the
metered budget runs out:

```yaml
budget:
  monthly_usd: 50

routing:
  rules:
    - if: {budget_exceeded: true}
      then: claude-sub
```

Instead of dropping to a local 8B model for the rest of the month, the
gateway keeps answering on the plan you already pay for. The same works as a
failover chain entry:

```yaml
  fallbacks:
    claude-sonnet: [claude-sub, gpt-5, qwen-coder]
```

### What Prism runs

Per request, in an empty temporary directory, with no stdin:

```bash
claude --tools "" --print --output-format stream-json --verbose \
       --model <id> --safe-mode --max-turns 1 \
       [--include-partial-messages] -- <prompt>

codex exec --json --skip-git-repo-check --model <id> \
       --sandbox read-only -- <prompt>
```

The optional flags are feature-detected from the CLI's own `--help` once per
binary and dropped if your version does not have them, so an older install
still works — it just loses that hardening. `--tools ""` and `--sandbox
read-only` are what keep the CLI to chat: no file access, no shell, no web.
`--safe-mode` stops your `CLAUDE.md`, skills, plugins, hooks and MCP servers
from shaping a gateway request. Everything after `--` is the prompt, so a
prompt starting with a dash can never be read as a flag.

Prism also removes `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` and the matching
API-key and model variables from the child's environment. If you followed the
sections above, those point at Prism — inherited, the gateway would call a
CLI that calls the gateway, forever. Dropping the keys is deliberate too:
these types exist to spend a subscription, and the `API_KEY=dummy` that setup
asks for would otherwise fail the CLI's own auth. For API-key billing use
`type: anthropic` or `type: openai` instead.

### How your messages are flattened

A CLI takes one prompt string, not a message list, so Prism renders the
conversation:

```
System: <every system and developer message, blank-line joined>

User: <text>

Assistant: <text>

User: <text>
```

One exception: a single user message with no system prompt is passed through
verbatim, so an ordinary one-shot completion reaches the CLI exactly as you
wrote it.

### Limits

- **No images and no tool calls.** Both are rejected with a 400, the same
  limitation the outbound `anthropic` adapter has. Route vision and
  tool-calling traffic to an API-type provider.
- **It answers as the CLI.** The CLI's own system prompt is in the context,
  so replies may carry its agentic framing and its prompt overhead shows up
  in your token counts. This is a chat bridge, not an API replacement.
- **Streaming is real only where the CLI supports it.** With
  `--include-partial-messages` (Claude Code) you get token-by-token deltas.
  Codex has no partial mode, so its answer arrives as a single chunk followed
  by `[DONE]` — with the upside that a streaming request keeps full failover,
  because nothing is written until the CLI has succeeded.
- **Latency is CLI latency.** Process spawn plus the CLI's own startup sits
  in front of every request. There is no timeout beyond the client's.
- **Rate limits are plan limits.** When the plan's window is exhausted the
  CLI exits with an error; Prism reports that as a retryable upstream failure
  and fails over to the next candidate in the chain.

## Cursor

Settings → Models → *Override OpenAI Base URL*:

```
Base URL:  http://127.0.0.1:8130/v1
API key:   dummy
Model:     auto        (add it as a custom model name)
```

Cursor's cloud features still call Cursor's own backend; the override applies
to the chat/completions traffic.

## Continue (VS Code / JetBrains)

`~/.continue/config.json`:

```json
{
  "models": [
    {
      "title": "DL Prism (auto)",
      "provider": "openai",
      "model": "auto",
      "apiBase": "http://127.0.0.1:8130/v1",
      "apiKey": "dummy"
    }
  ]
}
```

## Windsurf

Settings → Cascade → *Add custom model provider* → OpenAI-compatible:

```
Base URL:  http://127.0.0.1:8130/v1
API key:   dummy
Model:     auto
```

## Anything else OpenAI-compatible

```bash
curl http://127.0.0.1:8130/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer dummy' \
  -d '{"model":"auto","messages":[{"role":"user","content":"hi"}]}'
```

`GET /v1/models` advertises `auto` plus every model in your registry, so
clients that populate a model picker from the API just work.

---

## VS Code extension

Source: [`integrations/vscode`](../integrations/vscode). Sidebar chat with
streaming, a status-bar item showing month spend and the default model, and
commands for *Ask About Selection*, *Explain Routing* and *Switch Model*.

Install the packaged build:

```bash
code --install-extension integrations/vscode/dl-prism-0.1.0.vsix
```

Or build it yourself:

```bash
cd integrations/vscode
npm install
npm run package            # produces dl-prism-<version>.vsix
code --install-extension dl-prism-0.1.0.vsix
```

Settings (`Preferences → Settings → DL Prism`):

| Setting | Default |
|---------|---------|
| `dlPrism.baseUrl` | `http://127.0.0.1:8130` |
| `dlPrism.apiKey` | empty |
| `dlPrism.model` | `auto` |

## Chrome extension

Source: [`integrations/chrome`](../integrations/chrome) — Manifest V3, no
build step. Popup chat with streaming and a model picker, a context-menu
*Ask Prism about selection*, and an options page.

1. Open `chrome://extensions`.
2. Turn on **Developer mode** (top right).
3. **Load unpacked** → select the `integrations/chrome` directory.
4. Open the extension's *Options* and set the gateway URL (default
   `http://127.0.0.1:8130`) and API key if you configured one.

Host permissions are limited to `http://127.0.0.1/*` and
`http://localhost/*`; a gateway on another host needs that origin added to
`manifest.json`.

### Firefox

Same source, same features, one extra build step (`integrations/chrome` is a
single codebase for both browsers):

```bash
cd integrations/chrome
node tool/build.mjs         # writes dist/chrome/ and dist/firefox/
```

1. Open `about:debugging#/runtime/this-firefox`.
2. **Load Temporary Add-on…** → select `dist/firefox/manifest.json`.
3. Open the extension's *Options* (or the toolbar popup). If a *"Grant
   access to your local gateway"* banner appears, click it and accept the
   prompt — unlike Chrome, Firefox does not auto-grant `host_permissions` at
   install, so this one-time step is what lets the extension reach
   `127.0.0.1`/`localhost`.

Temporary add-ons unload on Firefox restart, so repeat steps 1–2 each
session. A permanent install needs the build signed by Mozilla (AMO) — see
[`integrations/chrome/README.md`](../integrations/chrome/README.md) for
details.

---

## Web dashboard

No setup: `http://127.0.0.1:8130/` serves the dashboard straight out of the
binary — playground, model registry, routing rules, usage charts and gateway
status.
