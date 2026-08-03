# DL Prism — Implementation Plan

DL Prism is a local-first AI gateway: one endpoint in front of every AI
provider you use — cloud or local — with manual, automatic, and (later)
hybrid routing. It implements the [Intelligent AI Routing spec](SPEC.md).

Stack: **Go**, single static binary `prism` (server + CLI in one).
Repo: `dhiva-labs/dl_prism`.

---

## The one decision everything hangs on

**Prism speaks the OpenAI wire protocol.** It exposes
`POST /v1/chat/completions` and `GET /v1/models`, byte-compatible with the
OpenAI API, including SSE streaming.

This is how the spec's "Routing API accessible through every interface"
requirement (Claude Code, Cursor, Windsurf, Continue, VS Code, JetBrains,
Codex CLI…) is actually satisfied: every one of those tools already knows how
to talk to an OpenAI-compatible base URL. We do not write eleven plugins; we
become a drop-in `base_url`. Model selection rides on the existing `model`
field:

| `model` value        | Routing mode                          |
|----------------------|---------------------------------------|
| `claude-sonnet` etc. | Manual — alias resolved via registry  |
| `auto`               | Automatic — classifier + policy rules |
| `pipeline:<name>`    | Smart Hybrid — named pipeline (M4)    |

---

## Architecture

```
client (any OpenAI-compatible tool)
   │  POST /v1/chat/completions {model:"auto", ...}
   ▼
┌──────────────────────────── prism ────────────────────────────┐
│ server    HTTP, auth, request parsing                         │
│   ▼                                                           │
│ router    classify → policy rules → decision + fallback chain │
│   ▼                                                           │
│ registry  alias → (provider, model), capabilities, pricing    │
│   ▼                                                           │
│ provider  adapters: openai-compat │ anthropic │ (gemini M4)   │
│   ▼                                                           │
│ cost      token estimate, usage ledger, monthly budget state  │
│ health    per-provider circuit breaker feeding failover       │
└───────────────────────────────────────────────────────────────┘
   ▼                    ▼                  ▼
 api.openai.com   api.anthropic.com   localhost:11434 (Ollama)…
```

### Packages

| Package             | Responsibility                                                       |
|---------------------|----------------------------------------------------------------------|
| `cmd/prism`         | CLI: `serve`, `models`, `explain`, `usage`                           |
| `internal/config`   | `prism.yaml` load/validate, `${ENV}` expansion                       |
| `internal/registry` | unified model registry: id/alias lookup, `local:<capability>` groups |
| `internal/provider` | adapter interface + `openai` and `anthropic` implementations         |
| `internal/router`   | request classifier, policy engine, route decision, circuit breaker   |
| `internal/cost`     | pricing math, JSONL usage ledger, monthly budget state               |
| `internal/server`   | HTTP endpoints, failover execution loop                              |

### Provider adapters

Two adapters cover almost the whole market:

- **`openai`** — generic OpenAI-compatible passthrough. Covers OpenAI,
  DeepSeek, Qwen/DashScope, Mistral, Groq, OpenRouter, and every local
  engine that matters: **Ollama, vLLM, LM Studio, llama.cpp server** (all
  expose `/v1/chat/completions`). The request body is forwarded with only
  the `model` field rewritten; streaming is piped through untouched.
- **`anthropic`** — full bidirectional translation between OpenAI chat
  format and the Anthropic Messages API, including SSE event translation
  (`content_block_delta` → chat chunks) and usage mapping.
- **`gemini`** (M4) — Google exposes an OpenAI-compatible endpoint, so the
  `openai` adapter works day one; a native adapter comes later for
  Gemini-only features.

New providers = new config entry, usually `type: openai`. That satisfies the
spec's "future support without architectural changes."

### Routing decision flow (`model: "auto"`)

1. **Classify** the request (heuristics, no LLM call, <1 ms): task kind
   (code / security / docs / vision / general), programming language (from
   code-fence info strings and keyword scoring), image parts present,
   estimated context tokens (chars/4).
2. **Evaluate policy rules** top-to-bottom from `prism.yaml`; first full
   match wins. Conditions: `task`, `language`, `vision`, `offline`,
   `budget_exceeded`, `min_context_tokens`. Targets: a model alias or
   `local:any` / `local:<capability>`.
3. **Resolve** the target through the registry; filter out models whose
   context window is smaller than the estimate.
4. **Build the failover chain**: primary + configured `fallbacks`, skipping
   providers whose circuit breaker is open (3 consecutive failures opens the
   circuit for 30 s; all-open falls back to trying anyway).
5. Execute; on a retryable failure (connect error, 429, 5xx) move down the
   chain — but **never after the first byte has streamed to the client**.

Every response carries `X-Prism-Provider`, `X-Prism-Model`, and
`X-Prism-Reason` headers, and `POST /v1/route/explain` returns the full
decision without calling any upstream — the spec's "transparent and
configurable" requirement.

### Cost & budget

- Static per-model pricing in config (`cost.input_per_mtok` /
  `output_per_mtok`); local models cost 0.
- Pre-flight estimate from the token estimate; actual usage recorded
  post-flight from response `usage` fields into a JSONL ledger at
  `~/.local/share/prism/usage.jsonl`.
- Current-month spend vs `budget.monthly_usd` produces the
  `budget_exceeded` policy condition — the spec's "IF Monthly Budget
  Reached THEN Use Local Models" rule works out of the box.

### Config (`prism.yaml`)

```yaml
server:
  listen: 127.0.0.1:8130
  api_keys: []                      # optional bearer tokens for clients

providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    models:
      - id: claude-sonnet-4-5
        aliases: [claude-sonnet]
        context_window: 200000
        capabilities: [code, reasoning, vision, tools]
        cost: {input_per_mtok: 3, output_per_mtok: 15}

  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:14b
        aliases: [qwen-coder]
        context_window: 32768
        capabilities: [code]

routing:
  default: claude-sonnet
  rules:
    - if: {language: python}        then: qwen-coder
    - if: {task: security}          then: claude-sonnet
    - if: {vision: true}            then: gemini-pro
    - if: {offline: true}           then: local:any
    - if: {budget_exceeded: true}   then: local:any
  fallbacks:
    claude-sonnet: [gpt-5, qwen-coder]

budget:
  monthly_usd: 50
```

---

## Milestones

- **M0 — Core gateway** *(this session)*: config, registry, `openai`
  adapter with streaming passthrough, manual routing, `/v1/models`, CLI.
- **M1 — Resilience** *(this session)*: circuit breaker, ordered failover
  chains, `X-Prism-*` transparency headers, `/v1/route/explain`.
- **M2 — Automatic mode** *(this session)*: heuristic classifier, policy
  engine, `model: "auto"`, `anthropic` adapter with SSE translation.
- **M3 — Cost** *(this session, basic)*: pricing, ledger, monthly budget
  condition, `prism usage`.
- **M4 — Later**: hybrid pipelines (`pipeline:<name>` — sequential steps,
  each independently routed), MCP server mode, OmniRoute/LiteLLM config
  import-export, hardware probe (VRAM via `nvidia-smi`/`rocm-smi` feeding
  `gpu_memory_gb` conditions), latency-aware scoring, load balancing
  weights across duplicate endpoints, native Gemini adapter, embeddings
  endpoint, Flutter desktop UI talking to `prism serve`.

Deliberately **out of scope for v1**: enterprise/team features (needs
auth+identity), LLM-based classification, tool-call translation in the
anthropic adapter (passthrough providers support tools day one).
