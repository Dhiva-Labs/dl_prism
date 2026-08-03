# DL Prism Companion — Chrome & Firefox

A thin browser client for [DL Prism](https://github.com/dhivalabs/dl_prism), the
local AI-gateway. Every feature here is a REST call to your own running
gateway (`prism serve`) — the extension carries no routing logic of its own.
One codebase, two loadable targets: Chrome (Manifest V3 service worker) and
Firefox (Manifest V3 event page).

## Features

- Toolbar popup chat with streaming replies, a model picker ("auto" first)
  and a route line under each reply (provider/model/mode/reason).
- Context menu → *Ask DL Prism about "…"* on any text selection, opened in a
  small standalone popup window.
- Options page for the gateway base URL, API key and default model, with a
  *Test connection* button.
- Month-to-date spend line, wired to `/admin/usage`.

## Install — Chrome

No build step required; the repo root itself is a loadable extension.

1. Open `chrome://extensions`.
2. Turn on **Developer mode** (top right).
3. **Load unpacked** → select this directory (`integrations/chrome`) directly,
   *or* build first (see below) and select `dist/chrome` instead — both are
   equivalent, `dist/chrome/manifest.json` is a byte-for-byte copy of the one
   at the repo root.
4. Open the extension's *Options* and set the gateway URL (default
   `http://127.0.0.1:8130`) and API key, if you configured one.

Host permissions (`http://127.0.0.1/*`, `http://localhost/*`) are granted
automatically at install time on Chrome — nothing else to do.

## Install — Firefox

Firefox needs the build step, since its manifest differs from Chrome's (see
below).

```bash
node tool/build.mjs      # writes dist/chrome/ and dist/firefox/
```

**Temporary install (for local use/testing):**

1. Open `about:debugging#/runtime/this-firefox`.
2. **Load Temporary Add-on…** → select `dist/firefox/manifest.json`.
3. Open the extension's *Options* (or the toolbar popup) — if you see a
   banner reading *"Grant access to your local gateway"*, click it and
   accept the prompt. Firefox does not auto-grant `host_permissions` at
   install the way Chrome does, so this one-time step is what actually lets
   the extension reach `127.0.0.1`/`localhost`.

Temporary add-ons are removed when Firefox restarts, so you'll repeat steps
1–2 each session.

**Permanent install** requires the `.xpi` to be signed by Mozilla (AMO) —
either self-distributed after signing via `web-ext sign` (needs an AMO API
key) or listed on addons.mozilla.org. Unsigned `.xpi`s only load on Firefox
Developer Edition/Nightly with `xpinstall.signatures.required` disabled. For
day-to-day local use, the temporary-install flow above is the supported path.

## Build

```bash
node tool/build.mjs
```

Zero npm dependencies. Copies the shared sources (`background.js`, `common/`,
`popup/`, `options/`, `icons/`) into `dist/chrome/` and `dist/firefox/`, each
with a manifest tailored to that browser:

| | Chrome | Firefox |
|---|---|---|
| Background | `service_worker` | `scripts` (event page) — Firefox does not reliably support MV3 service workers |
| `browser_specific_settings` | — | `gecko.id` + `gecko.strict_min_version` |
| Everything else (`permissions`, `host_permissions`, `action`, `icons`, `options_ui`) | identical | identical |

The repo root (`manifest.json`, `background.js`, etc.) is untouched by the
build and stays loadable in Chrome exactly as before — `dist/` is purely a
build artifact (gitignored).

## Cross-browser API notes

The extension code (`background.js`, `popup/popup.js`, `options/options.js`)
targets the `chrome.*` namespace only — no `browser.*`, no
webextension-polyfill. Firefox implements `chrome.*` as an alias over the
same APIs in MV3, so this runs unmodified in both browsers, with two
exceptions handled in `common/compat.js`:

- `contextMenus.create`/`removeAll` are documented as callback-only even
  under MV3 (they don't reliably resolve a promise when the callback is
  omitted) — `compat.js` always passes a callback and additionally chains
  off a returned promise if one comes back, so either calling convention
  works.
- The "grant access" permission flow (`common/permission-ui.js`) uses
  `chrome.permissions.contains`/`.request`, wrapped the same way.

`chrome.storage.local`, `chrome.windows.create` and `chrome.action` are used
directly with the promise-returning calling convention (omit the callback),
which both browsers support in MV3.

### Firefox host-permission grant flow

Firefox (MV3) does not auto-grant `host_permissions` at install. The popup
and options pages both call `permissions.contains({ origins: [...] })` on
load; if not granted, a banner appears with a *"Grant access to your local
gateway"* button that calls `permissions.request(...)` directly inside the
click handler (required for the permission prompt to fire — browsers need a
live user gesture). On Chrome, `contains()` already resolves `true` at
install time, so the banner never renders — this is a plain no-op there, not
a browser check.

## Smoke test

```bash
node smoke/smoke.mjs [baseUrl] [apiKey]   # defaults to http://127.0.0.1:8130
```

Exercises `common/api.js` (the exact module the extension loads) against a
live gateway from plain Node — the same escape hatch used when a real
browser isn't available to click through the UI by hand.

## Regenerating icons

```bash
node icons/generate-icons.js
```

Dev-time only; draws the toolbar icon at 16/32/48/128px with a hand-rolled
PNG encoder (no npm deps). Not shipped — excluded from both `dist/` targets
by `tool/build.mjs`.
