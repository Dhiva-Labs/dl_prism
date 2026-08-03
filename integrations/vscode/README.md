# DL Prism for VS Code

A thin VS Code client for [DL Prism](https://github.com/dhivalabs/dl_prism), the
local AI-gateway. Every feature here is a REST call to your own running
gateway — the extension carries no routing logic of its own.

## Features

- **Sidebar chat** (activity bar → DL Prism) — streaming replies, a model
  picker ("auto" first), and a route bar under each reply showing which
  provider/model actually answered and why.
- **Status bar item** — `⚡ prism <model> · $<month spend>`, refreshed every
  60s from `/admin/usage`. Click it to switch the default model. Shows
  `prism offline` (no popups) when the gateway can't be reached.
- **Commands** (`Cmd/Ctrl+Shift+P`):
  - `DL Prism: Ask About Selection` — send the current selection plus a
    typed question straight into the chat view.
  - `DL Prism: Explain Routing` — see the routing decision for a prompt via
    `/v1/route/explain`, rendered in the "DL Prism Routing" output channel.
  - `DL Prism: Switch Model` — QuickPick populated from `/v1/models`.
  - `DL Prism: Open Dashboard` — opens the gateway's base URL in your
    browser.

## Settings

| Setting             | Default                   | Notes                                              |
|----------------------|---------------------------|-----------------------------------------------------|
| `dlPrism.baseUrl`    | `http://127.0.0.1:8130`   | Gateway origin, no trailing slash.                  |
| `dlPrism.apiKey`     | `""`                      | Optional bearer key. Stored as plain text — prefer a local-only key and avoid Settings Sync for this one. |
| `dlPrism.model`      | `"auto"`                  | Default model for new chats and the status bar.     |

## Install (from a packaged .vsix)

```bash
npx @vscode/vsce package --no-dependencies --allow-missing-repository
code --install-extension dl-prism-0.1.0.vsix
```

Or, from the Extensions view: `…` menu → "Install from VSIX…".

## Development

```bash
npm install
npm run typecheck   # tsc --noEmit
npm run build        # esbuild bundle -> dist/extension.js
npm run smoke         # exercise ApiClient against a live gateway (models, streaming chat, usage)
```

Press `F5` in VS Code (with this folder open) to launch an Extension
Development Host.
