# DL Prism Workbench — Plan (v0.3)

The dashboard grows from a playground into a **workbench**: Claude-app-style
projects with persistent conversations and attached knowledge, plus a
Codex-style agent that can act — browse the filesystem, edit files, run
commands, fetch the web — under explicit, per-project permission.

## Principles

- **Local-first state**: everything lives in
  `~/.local/share/prism/workbench/` as plain files (JSON/JSONL) — no
  database dependency, greppable, backupable.
- **Agency is opt-in and scoped**: agent tools are OFF per project until
  enabled; every project gets its own `workspace/` directory and tools
  cannot escape it; shell/writes require user approval by default.
- **The router stays the brain**: the agent loop calls the same
  `/v1/chat/completions` path, so policies, failover, budgets, and the
  ledger all apply to agent turns. Tool-calling requires a tool-capable
  provider (openai-type passthrough — OpenAI, or local Qwen via Ollama).

## Storage layout

```
workbench/projects/<id>/project.json      # name, settings, timestamps
                        chats/<id>.jsonl  # one event per line
                        files/            # attached knowledge
                        workspace/        # the agent's sandbox cwd
```

`project.settings`: `{agent_enabled, approval_mode: always|mutations|never,
model, system_prompt}`.

## API contract (wave A)

- `GET/POST /admin/projects`, `PATCH/DELETE /admin/projects/{id}`
- `GET/POST /admin/projects/{id}/chats`, `GET/DELETE /admin/chats/{id}`
- `POST/GET/DELETE /admin/projects/{id}/files` (knowledge; small text files
  are folded into the system context, all are readable by tools)
- `POST /v1/agent/run` `{chat_id, message, model?}` → SSE event stream:
  `message_delta{text}` · `tool_call{id,name,args}` ·
  `tool_await_approval{id}` · `tool_result{id,output,truncated}` ·
  `done{usage}` · `error{message}`
- `POST /v1/agent/approve` `{chat_id, tool_call_id, approve}`

All behind the existing bearer auth.

## Agent tools

| Wave | Tool | Notes |
|---|---|---|
| A | `list_dir`, `read_file` | workspace + attached files, path-escape-proof |
| A | `write_file` | approval-gated under `mutations`/`always` |
| A | `shell` | workspace cwd, 60 s timeout, output capped, approval-gated |
| A | `web_fetch` | GET → readable text, size-capped, no local addresses |
| B | `browser` | chromedp-driven headless Chrome: navigate/read/screenshot/click; requires Chrome |
| B | `watch_video` | ffmpeg frame sampling → vision-capable model; requires ffmpeg |
| B | `transcribe` | audio track → local whisper if present |

## UI (wave A)

Sidebar gains **Projects**: project list → chat list → conversation.
Conversation view renders the agent timeline: streamed text, collapsible
tool-call cards (name, args, output), inline approval prompts
(Approve / Deny buttons wired to `/v1/agent/approve`), file-attachment
management, per-project settings sheet (agent toggle, approval mode,
default model). The plain Playground stays for stateless one-offs.

## Wave B (after A verifies)

Browser + video tools, tool-progress streaming for long commands,
project export/import, and desktop/mobile surfacing of projects.
