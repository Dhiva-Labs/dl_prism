# Spec Review — Intelligent AI Routing

Engineering review of [SPEC.md](SPEC.md), written before implementation.
Verdict up front: **the spec is buildable and the core is a well-understood
product shape** (LiteLLM, OpenRouter, and Portkey all prove the market), but
it is silent on the single most important design question, and three of its
features are much bigger than they look.

---

## What the spec gets right

- **Manual / Automatic split** is clean and maps naturally onto the
  existing `model` request field — no new client protocol needed.
- **Policy examples are concrete** (language, task, offline, VRAM, budget).
  They translate almost 1:1 into a first-match rule engine.
- **Failover chains and provider profiles** are the industry-standard
  shape; nothing exotic, all implementable with a circuit breaker and an
  ordered candidate list.
- **"Provider-agnostic, new providers without architectural changes"** is
  achievable for free, because nearly every provider (including local
  engines: Ollama, vLLM, LM Studio, llama.cpp) now exposes an
  OpenAI-compatible endpoint. One generic adapter covers them all.

## Gaps — things the spec never says

1. **No wire protocol is specified.** The spec lists eleven client
   integrations but never says how a client talks to the router. This is
   the make-or-break decision. Resolution (adopted in the plan): expose an
   OpenAI-compatible API so all listed tools work by changing a base URL.
   Without this, each integration is a bespoke plugin project.
2. **Where do capability scores come from?** "Router selects Claude because
   of strong repository reasoning" presumes a quality ranking per
   task-type per model. No source is given, and no benchmark feed exists
   that updates itself. Resolution: a static, user-editable capability
   matrix in config (`capabilities: [code, reasoning, vision]`), refined by
   rules. Honest limitation: automatic mode is only as good as the config.
3. **Request analysis is unspecified.** Classifying "is this a security
   audit?" perfectly requires an LLM call, which adds latency and cost to
   every request — the thing a router must not do. Resolution: fast
   heuristics (code fences, keywords, image parts, token count) with the
   option of a local-model classifier later. Accept ~90 % accuracy.
4. **Token/cost estimation before execution** needs a tokenizer per model
   family. Resolution: chars/4 estimate, clearly labelled an estimate;
   exact usage is recorded post-flight from provider `usage` fields.
5. **"OmniRoute compatibility" is unverifiable.** I could not find a public
   OmniRoute config format specification to be compatible with. Risk: we
   guess a format and are compatible with nothing. Resolution: define
   Prism's own YAML as canonical now; add importers later for formats that
   are actually published (LiteLLM `config.yaml`, Continue's config). If
   you have OmniRoute config samples, drop them in `docs/reference/` and
   the importer becomes concrete.

## Features that are bigger than they look

1. **Smart Hybrid Mode is a workflow orchestrator**, not a router feature.
   Multi-step pipelines with different models per step, plus "unified final
   response", is a second product layer (state, partial failure handling,
   step outputs feeding step inputs). Deferred to M4 with a schema defined
   now (`pipeline:<name>`), so the API doesn't break when it lands.
2. **Enterprise features** (teams, org-wide restrictions, audit) all
   require authentication and identity, which the spec never mentions.
   Deferred entirely; v1 is single-user local-first, which is also the
   fastest path to something usable.
3. **Hardware-aware routing** (VRAM, GPU vendor, model loading time) needs
   a per-OS hardware probe and Ollama-specific introspection. Deferred to
   M4; the policy engine already accepts the conditions, so the probe just
   fills in facts when it arrives.

## Internal tensions to resolve by convention

- **Latency vs cost vs quality** can point at three different models for
  the same request. The spec lists all three as factors without a
  tie-break. Resolution: the policy list is *ordered*; first match wins;
  no scoring blend in v1. Predictable beats clever for a router.
- **Failover vs streaming**: once tokens have streamed to the client, you
  cannot silently fail over mid-response. Resolution: fail over only
  before the first byte reaches the client; after that, the error
  surfaces to the client.
- **"IF Offline THEN local only"** requires knowing you're offline, and
  probing connectivity on every request adds latency. Resolution: offline
  is an explicit mode (`--offline` / config flag) in v1; passive detection
  (recent connect failures flip the flag) later.

## Security notes (for a tool that holds all your API keys)

- Keys live in the config as `${ENV}` references or are read from
  environment variables — never logged, never echoed in `/v1/models` or
  explain output.
- The server binds `127.0.0.1` by default; binding wider requires
  explicitly configured client API keys.
- The usage ledger stores token counts and costs, never prompt content.

## Recommended cut for v1

Build: unified registry, manual + automatic modes, policy rules, failover,
cost ledger + budget condition, OpenAI-compatible API, CLI.
Defer: hybrid pipelines, MCP server, import/export, hardware probe,
enterprise/teams, embeddings, native Gemini adapter.
