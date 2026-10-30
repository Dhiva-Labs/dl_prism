# Changelog

## 0.2.0 - 2026-10-16

First tagged release. The gateway is 0.2.0 (`Makefile` and `cmd/prism/main.go`); the desktop
companion is 1.0.0 (`integrations/desktop/pubspec.yaml`), and its version is the one in the
desktop tarball names. The tag `v0.2.0` follows the gateway version.

### Added
- Gateway (`prism`): one OpenAI-compatible endpoint (`/v1/chat/completions`) and an Anthropic
  Messages-compatible endpoint (`/v1/messages`), both streaming; manual and automatic routing with
  policy rules; failover with a circuit breaker; cost ledger and monthly budget; admin API;
  `prism mcp` (MCP over stdio); an embedded web dashboard.
- Providers: OpenAI-compatible (cloud and local engines), Anthropic, and providers that use a
  logged-in vendor CLI on the same machine.
- Workbench: projects, persistent chats and an acting agent confined to a per-project sandbox,
  with approvals for writes and shell commands.
- Companions in `integrations/`: Flutter desktop control window (Linux), VS Code extension,
  Chrome and Firefox extension, Android app.
- Release builds of the gateway for Linux amd64, Linux arm64, Windows amd64 and Windows arm64,
  and of the desktop companion for Linux x64 and arm64 (two variants: with the embedded WebKitGTK
  dashboard window, and browser-only). Every leg is built, tested and run on a native runner of
  its own architecture, and a published release gets all of them attached by
  `.github/workflows/builds.yml`.

### Not included
- The Android app, the VS Code extension and the browser extension are not built by CI and are
  not release assets.
