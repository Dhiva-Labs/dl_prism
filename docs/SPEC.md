# Intelligent AI Routing

> Original product specification, as provided on 2026-08-03. See
> [PLAN.md](PLAN.md) for the implementation plan and
> [SPEC_REVIEW.md](SPEC_REVIEW.md) for the engineering review of this spec.

## Overview

The platform includes an advanced routing engine similar to OmniRoute, allowing requests to be automatically or manually routed to the most appropriate AI provider or model.

Instead of locking users to a single provider, the platform acts as a unified AI gateway capable of routing requests across local and cloud models based on configurable rules.

---

## Routing Modes

### Manual Mode

Users explicitly select the model.

Examples:

- Claude Sonnet
- GPT-5
- Gemini 2.5 Pro
- Qwen3 Coder
- DeepSeek Coder
- Llama 3
- Mistral

---

### Automatic Mode

The platform analyzes the request and automatically chooses the most suitable model.

Factors include:

- Coding capability
- Reasoning quality
- Vision support
- Context length
- Latency
- Cost
- Local GPU availability
- Offline availability
- User preferences

Example

User:
"Review this 50,000-line Rust project."

↓

Router selects Claude because of strong repository reasoning.

---

User:
"Generate a Dockerfile."

↓

Router selects the local Qwen Coder model.

---

### Smart Hybrid Mode

Complex tasks can be distributed across multiple models.

Example

User Request → Claude (Architecture Analysis) → Qwen (Implementation) → Gemini (Image / Diagram Processing) → GPT (Documentation Cleanup) → Unified Final Response

The routing process is transparent and configurable.

---

## OmniRoute Compatibility

Support OmniRoute-compatible configurations where applicable, allowing users to reuse routing profiles and provider definitions instead of creating everything from scratch.

Features include:

- Import routing profiles
- Export routing configurations
- Compatible provider definitions
- Shared model aliases
- Routing presets
- Team-wide routing policies

---

## Routing Policies

Users can define routing rules.

Examples

- IF Language = Python THEN Model = Qwen Coder
- IF Task = Security Audit THEN Model = Claude
- IF Task = Vision THEN Model = Gemini
- IF Offline THEN Use Local Models Only
- IF GPU Memory < 8GB THEN Use Small Quantized Models
- IF Monthly Budget Reached THEN Use Local Models

---

## Cost Optimization

The router continuously estimates API costs.

Examples

- Prefer local models when possible
- Fall back to cloud models only when necessary
- Budget alerts
- Monthly usage limits
- Cost comparison before execution

---

## Performance Optimization

Route based on:

- Available RAM
- Available VRAM
- GPU vendor
- CPU architecture
- Context size
- Model loading time
- Network latency

---

## Failover

If a provider fails:

Claude → GPT → Gemini → Local Qwen → DeepSeek → User Notification

---

## Load Balancing

Support multiple endpoints.

Example

Local GPU Server 1 → Local GPU Server 2 → Cloud Provider → Backup Provider

---

## Provider Profiles

Each provider stores:

- Endpoint
- Authentication
- Model List
- Tool Calling Support
- Vision Support
- Embedding Support
- Context Window
- Rate Limits
- Cost Information

---

## Unified Model Registry

The application exposes every connected model through one interface regardless of provider.

Example

Available Models

- Claude Sonnet
- GPT-5
- Gemini 2.5 Pro
- DeepSeek Coder
- Qwen3 Coder
- Llama 3
- Gemma
- Phi-4

Each model displays:

- Provider
- Local / Remote
- Context Window
- Tool Support
- Vision Support
- Embeddings
- Estimated Speed
- Estimated Cost

---

## Routing API

The routing engine is accessible through every interface.

- Desktop Application
- CLI
- MCP Server
- REST API
- VS Code Extension
- JetBrains Plugin
- Claude Code
- Codex CLI
- Cursor
- Windsurf
- Continue

Every request passes through the routing engine before execution.

---

## Enterprise Features

- Shared routing policies
- Team presets
- Organization-wide model restrictions
- Cost tracking
- Audit logs
- Provider failover
- On-prem routing
- Multi-server inference clusters

---

## Future Support

The routing engine is provider-agnostic and should support new providers without requiring architectural changes.

Examples

- New local inference engines
- Future OpenAI models
- Future Anthropic models
- Future Google models
- Enterprise inference servers
- Self-hosted OpenAI-compatible APIs
- Custom model providers
