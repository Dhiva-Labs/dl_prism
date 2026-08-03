# DL Prism Desktop

A small Flutter (Linux) control-tower window for the [DL Prism](../../README.md)
gateway. It is a thin client of the gateway's REST API — no routing logic
lives here.

## What it does

- **Gateway card** — polls `GET /healthz` every 5 seconds and shows
  reachable/unreachable. When reachable, shows version, uptime, mode
  (online/offline) and listen address from `GET /admin/status`.
- **Process management** — if the gateway isn't reachable, "Start gateway"
  spawns `<prism binary> serve [-c <config path>]` as a child process and
  streams its stdout/stderr into a scrolling log pane (last ~500 lines).
  "Stop" sends SIGTERM to that child. If the gateway is already running
  externally (i.e. we didn't start it), the card says so and disables Stop —
  this app only ever signals processes it spawned itself.
- **Usage card** — month spend vs budget with a progress bar, a
  budget-exceeded highlight, and request count, from `GET /admin/usage`.
- **Providers list** — name, local/cloud chip, health dot and model count
  for each provider in `/admin/status`.
- **Open Dashboard** — a prominent button in the Gateway card (and a
  matching app-bar shortcut) that opens the gateway's base URL. If the
  gateway isn't reachable yet, it just hints to start it first rather than
  opening anything. See **Variants** below for what "open" actually does —
  it differs between the two build variants this app ships as.
- **Local models (Ollama)** — a card for installing/running/stopping/deleting
  local models via a local [Ollama](https://ollama.com) instance:
  - Lists installed models (`GET /api/tags`) with size in GB, and flags
    whichever are currently loaded into memory (`GET /api/ps`, polled
    alongside the gateway on the same 5s cycle) with a "loaded" chip.
  - **Run** / **Stop** per model calls `POST /api/generate` with an empty
    prompt and `keep_alive: -1` (load) or `keep_alive: 0` (unload) —
    Ollama's documented way to manage what's resident in memory without
    generating anything.
  - **Delete** (`DELETE /api/delete`) asks for confirmation first and is
    styled destructively.
  - A pull row (text field + Pull button) drives `POST /api/pull` with
    `{"model": name, "stream": true}` and renders the NDJSON progress
    stream as a determinate progress bar with the current status text and
    per-layer percent; **Cancel** uses `package:http`'s `AbortableRequest`
    (there's no separate cancel endpoint — this is Ollama's own
    cancellation mechanism) to immediately stop the app from processing or
    showing further progress. Note this is an application-level abort:
    `dart:io`'s `HttpClientResponse` has no API to truly interrupt an
    in-flight socket read once headers are received, so a few more bytes
    already in flight may still be read and discarded rather than left as
    an untouched, growing connection.
  - When Ollama isn't reachable, the card shows "Ollama not detected" with
    an **Install Ollama** button (`url_launcher` → ollama.com/download) and
    a **Retry** button. An empty model list shows a starter-model hint.
  - Above the free-text pull field, a **catalog** (`lib/data/model_catalog.dart`)
    of 10 curated models (name, family, ~size, blurb) each show either an
    "Installed" chip (matched against `/api/tags`, tolerating a `:latest`
    tag on either side) or a one-click Pull button. Only one pull runs at a
    time — every other Pull button (catalog or free-text) disables while
    one is in flight.
- **Providers card** — one-click connect/disconnect for 12 cloud provider
  presets (`lib/data/provider_presets.dart` — a plain data file, safe to
  edit; pricing/model IDs are a point-in-time snapshot as of 2026-08),
  shown as a compact wrap of chips (text-badge "logos", no image assets)
  with connected ones sorted first:
  - Anthropic, OpenAI/Codex, Moonshot (Kimi), DeepSeek, Google (Gemini),
    xAI (Grok), Mistral, Groq, OpenRouter, Together AI, Alibaba DashScope
    (Qwen), and Z.ai (GLM).
  - Tapping a chip opens a dialog with the preset's description, the
    models it will add, and an obscured API-key field; already-connected
    presets label the action **Replace** instead of **Connect**. A
    connected chip's own delete affordance is **Disconnect** (confirms
    first).
  - Connecting/disconnecting writes/removes the provider's block in the
    gateway's `prism.yaml` via `lib/services/prism_config_writer.dart`,
    using `package:yaml_edit` so unrelated content, formatting, and
    comments in the file survive untouched. Defaults to
    `~/.config/prism/prism.yaml`; overridable via the new "Gateway config
    file" setting. If the file doesn't exist yet, it's created with a
    minimal skeleton (loopback listener, a local `ollama` provider,
    `routing.default: qwen-coder`) plus the connected preset. The API key
    is written as a literal `api_key` field, and the file is `chmod 600`
    after every write. If `routing.default` ends up unset after a write,
    it's backfilled from the `ollama` provider's first model, else the
    newly-connected preset's first model.
  - After any config change, a snackbar prompts a gateway restart to apply
    it — with a one-click **Restart** action when the gateway is running
    as this app's own child process (reusing the same start/stop
    machinery), or just the hint to restart manually when it's running
    externally (this app never signals a process it didn't spawn).
- **Settings** — gateway base URL, API key, `prism` binary path, config
  file path, Ollama base URL, and gateway config file path, persisted with
  `shared_preferences`. The gateway API key is sent as an `Authorization:
  Bearer` header on every gateway request (Ollama's local API has no auth
  of its own).
- Material 3 UI that follows the system light/dark theme (seed color
  `0xFF6D5EF0`), sized as a compact ~900x640 utility window.

## Variants

This app ships as two build variants:

|                        | **full**                                   | **engine** (lite)                   |
|------------------------|---------------------------------------------|--------------------------------------|
| "Open Dashboard"       | Embedded WebKitGTK window, titled "DL Prism Dashboard" (~1200x800, chromeless — no address bar/tabs) | Always opens the system browser |
| Extra runtime dependency | `libwebkit2gtk-4.1` (+ its own deps: `libsoup-3.0`, `libjavascriptcoregtk-4.1`) | none |
| Built with              | `desktop_webview_window` present in `pubspec.yaml`, `--dart-define=EMBED_DASHBOARD=true` | dependency removed from `pubspec.yaml` entirely |
| Bundle size (this build)| 25.15 MiB                                  | 25.08 MiB                            |

Build both with:

```sh
cd integrations/desktop
tool/build_variants.sh
# tool/build_variants.sh --skip-full     # only rebuild engine
# tool/build_variants.sh --skip-engine   # only rebuild full
```

which leaves `dist/dl-prism-desktop-full/` and `dist/dl-prism-desktop-engine/`
each a self-contained, runnable bundle (same layout as `flutter build`'s own
`build/linux/x64/release/bundle/`), and leaves the working tree back in the
**full** configuration (the normal, committed state) when it's done, even if
a build fails partway.

### Why a build script and not just the `EMBED_DASHBOARD` flag

`lib/config/build_variant.dart` defines `const bool kEmbedDashboard =
bool.fromEnvironment('EMBED_DASHBOARD')`, and that flag genuinely does
gate the Dart-level behavior (see `lib/services/dashboard_launcher.dart`).
But it is **not** sufficient by itself to produce a lite bundle that runs
on a machine without `libwebkit2gtk-4.1` — verified empirically on this
machine:

With `desktop_webview_window` merely present in `pubspec.yaml` (regardless
of the dart-define value), both the main executable
(`build/linux/x64/release/bundle/desktop`) and its bundled
`lib/libdesktop_webview_window_plugin.so` carry a hard ELF `NEEDED` entry
for `libwebkit2gtk-4.1.so.0` (checked with `readelf -d`), which transitively
pulls in `libsoup-3.0.so.0` and `libjavascriptcoregtk-4.1.so.0` too. This is
because:

- Flutter's generated `linux/flutter/generated_plugins.cmake` does
  `target_link_libraries(${BINARY_NAME} PRIVATE ${plugin}_plugin)` for
  *every* plugin declared in `pubspec.yaml`, unconditionally — this is a
  hard link-time dependency of the runner executable itself, not something
  dlopen'd lazily at a point Dart code could intercept.
- `generated_plugin_registrant.cc`'s `fl_register_plugins()` unconditionally
  calls every plugin's `*_register_with_registrar()` during native/GTK
  startup — before the Dart VM even starts running `main()`, let alone
  evaluates `kEmbedDashboard`.
- The plugin's own `linux/CMakeLists.txt` does
  `pkg_check_modules(WebKit REQUIRED IMPORTED_TARGET webkit2gtk-4.1)` and
  links its `.so` against it.

So the OS's dynamic linker refuses to even *start* the process on a
machine missing `libwebkit2gtk-4.1.so.0` — a Dart-level runtime flag
cannot prevent ELF loading from failing before any Dart code runs.
`tool/build_variants.sh` therefore builds the engine variant by actually
removing the dependency: it `sed`s the `desktop_webview_window:` line out
of `pubspec.yaml` **and** temporarily overwrites
`lib/services/dashboard_webview.dart` (the one file in the app that
imports the plugin) with a stub, runs `flutter pub get` (which regenerates
the Linux plugin registrant/CMake files without the dependency), builds,
then restores both files.

(A Dart conditional export/import keyed on a custom `--dart-define` —
`export 'a.dart' if (EMBED_DASHBOARD == 'true') 'b.dart';` — was tried
first, as a way to make the plugin import itself conditional at the source
level without a build-script file swap. It doesn't work reliably on this
SDK (Dart 3.12.2): verified with `dart run`/`dart compile exe
-DEMBED_DASHBOARD=true`, the branch always resolved to the default
regardless of the define's value — only `dart.library.*` conditions were
observed to actually switch branches. So the file-swap approach in
`tool/build_variants.sh` is what's actually used, not a conditional
import.)

### Fallback behavior (full variant)

If creating the embedded webview throws at runtime — most likely
`libwebkit2gtk-4.1` missing or failing to initialize on the user's
machine, even though the app itself linked against it at build time — the
app automatically falls back to `url_launcher`'s browser open and shows a
one-line snackbar explaining that the embedded dashboard wasn't available.
This whole decision tree (`DashboardLauncher` in
`lib/services/dashboard_launcher.dart`) is unit-tested with an injectable
"webview available" probe — see **Testing notes** below — without ever
touching the real native plugin.

### Packaging note (`.deb`)

If you package the **full** variant as a `.deb`, add a `Depends:` line for
the runtime library (not the `-dev` package):

```
Depends: libwebkit2gtk-4.1-0
```

The **engine** variant needs no such line — it has no `libwebkit2gtk-4.1`
reference anywhere in its bundle (verified: `readelf -d` on every `.so` in
`dist/dl-prism-desktop-engine/lib/` and on the executable itself).

### Building the full variant needs `libwebkit2gtk-4.1-dev` at build time

Only for compiling — `flutter analyze` and `flutter test` need neither
variant's native dependencies, since they never invoke the Linux CMake
build. To build (or `flutter run -d linux`) the full variant:

```sh
sudo apt install libwebkit2gtk-4.1-dev
```

On a machine without `sudo`/network access to install packages normally,
`apt-get download libwebkit2gtk-4.1-dev` (and its further dependencies:
`libsoup-3.0-dev`, `libjavascriptcoregtk-4.1-dev`, `libnghttp2-dev`,
`libpsl-dev`, `libsqlite3-dev`, `libsysprof-capture-4-dev`) works without
root; extract each `.deb` with `dpkg -x pkg.deb somedir/`, fix the
`prefix=`/`libdir=` lines in the extracted `.pc` files to point at
`somedir/usr` instead of `/usr` (`PKG_CONFIG_SYSROOT_DIR` looked like the
"correct" tool for this but over-prefixes *every* resolved path, including
ones from unrelated, already-installed system `.pc` files like
`glib-2.0.pc` — editing `prefix=` directly in just the extracted files
avoids that), symlink the runtime `.so.N` files the `-dev` package's
unversioned `.so` symlinks point to (the `-dev` package assumes the
matching runtime package's versioned `.so` is already installed
system-wide, which it typically is), and point `PKG_CONFIG_PATH` (not
`PKG_CONFIG_SYSROOT_DIR`) at the extracted `pkgconfig/` directory before
running `flutter build linux`. This is exactly how the full variant was
verified to build in this sandbox (which has the runtime
`libwebkit2gtk-4.1-0` package but not the `-dev` one, and no `sudo`).

## Layout

```
lib/
  config/     build_variant.dart (kEmbedDashboard compile-time flag)
  data/       model_catalog.dart (curated pullable Ollama models), provider_presets.dart
              (cloud provider connect presets) — plain data, no logic
  models/     GatewayStatus, UsageInfo, ModelInfo — typed views of the gateway API JSON
              OllamaModel, LoadedModel, PullProgress — typed views of the Ollama API JSON
  services/   PrismApiClient (gateway HTTP), OllamaClient (Ollama HTTP, incl. NDJSON
              pull streaming), GatewayProcessController (child process lifecycle + log
              ring buffer), PrismConfigWriter (prism.yaml read/write via yaml_edit),
              DashboardLauncher (embedded-vs-browser decision, testable),
              dashboard_webview.dart (the ONLY file importing desktop_webview_window —
              see Variants above), SettingsStore (shared_preferences)
  screens/    HomeScreen (polling + layout), SettingsScreen
  widgets/    GatewayCard, UsageCard, ProvidersList, LocalModelsCard, ModelCatalogSection,
              ProvidersCard, LogPane, StatusDot
tool/
  build_variants.sh   builds both variants into dist/, see Variants above
```

## Build & run

Requires the Flutter SDK with Linux desktop support enabled
(`flutter config --enable-linux-desktop`).

```sh
cd integrations/desktop
flutter pub get
flutter analyze                                       # clean, no native deps needed
flutter test                                           # unit + widget tests, no native deps needed
flutter run -d linux --dart-define=EMBED_DASHBOARD=true         # full variant, debug mode
flutter build linux --release --dart-define=EMBED_DASHBOARD=true # full variant, release
```

The working tree's `pubspec.yaml` normally contains the full variant's
`desktop_webview_window` dependency, so a plain `flutter build linux`
builds the full variant (needs `libwebkit2gtk-4.1-dev` — see **Variants**
above). To get both variants — including a genuine engine/lite bundle —
use `tool/build_variants.sh` instead of calling `flutter build` directly;
see **Variants** above for why.

The release binary and its bundled assets land at:

```
build/linux/x64/release/bundle/desktop          # flutter build linux, either variant
dist/dl-prism-desktop-full/desktop              # tool/build_variants.sh
dist/dl-prism-desktop-engine/desktop            # tool/build_variants.sh
```

Run it directly, or copy the whole bundle directory wherever you like —
it's self-contained (the executable expects its `data/` and `lib/`
siblings alongside it).

On first launch the app defaults to `http://127.0.0.1:8130` with no API key
and `prism` resolved from `PATH`; change any of that from the settings
screen (gear icon in the app bar).

## Testing notes

- `test/services/api_client_test.dart` spins up a real `dart:io` `HttpServer`
  as a fake gateway to exercise `/healthz`, `/admin/status`, `/admin/usage`,
  `/v1/models`, the `Authorization: Bearer` header, and unreachable-server
  handling — no mocked HTTP internals.
- `test/services/gateway_process_controller_test.dart` exercises the process
  controller against real child processes (`/bin/sleep`, `/bin/false`, and
  small generated shell scripts standing in for `prism serve`), covering
  start/stop, stdout/stderr capture, the log ring buffer cap, crash handling,
  and the "never double-start / never stop what you didn't start" invariants.
- `test/services/ollama_client_test.dart` similarly spins up a real fake
  Ollama server: `/api/tags` and `/api/ps` parsing, the `/api/pull` NDJSON
  progress stream (percent math per layer, a mid-stream `{"error":...}`
  line, a non-2xx initial response), `/api/generate` payloads for
  load/unload (asserting the exact `keep_alive` values sent), and
  `/api/delete`. The pull-cancellation test gates the fake server's second
  NDJSON line on a `Completer` the test controls directly rather than a
  fixed delay, so it can't race a slow/loaded test runner — cancelling a
  `StreamSubscription` is a hard guarantee against further delivery per the
  Stream contract, so this is a deterministic check, not a timing one.
- `test/services/settings_store_test.dart` round-trips `AppSettings` through
  `shared_preferences` (mocked in-memory).
- `test/widgets/*` and `test/screens/*` cover the presentational components
  and the settings form in isolation, including `LocalModelsCard`'s
  unreachable/empty/listed+loaded/pulling-with-progress states and its
  delete-confirmation dialog.
- `test/data/model_catalog_test.dart` and `test/data/provider_presets_test.dart`
  cover the catalog/preset data itself: the `:latest`-tolerant installed-model
  matching, and preset shape/count/uniqueness (including a regression test
  for a real bug caught during development — a preset's chip "logo" badge
  text colliding with its display name, which made widget finders match the
  same chip twice).
- `test/services/prism_config_writer_test.dart` writes into a fresh
  `Directory.systemTemp` per test (never the real
  `~/.config/prism/prism.yaml`): fresh-file creation with the skeleton,
  preserving unrelated content/comments when adding to an existing file,
  replacing an existing provider in place (including the real `google`
  preset's two models), disconnect/remove, `routing.default` backfill from
  either the `ollama` provider or the new preset, and a `stat`-based check
  that the file ends up `chmod 600`.
- `test/widgets/providers_card_test.dart` covers the chip grid's states
  (none/one/multiple connected, sort order, busy spinner) and the connect
  dialog (empty-key validation, Connect vs. Replace wording, the models
  list shown, and that all 12 presets render).
- `test/services/dashboard_launcher_test.dart` covers `DashboardLauncher`
  entirely without touching the real `desktop_webview_window` plugin: a
  sanity check that `kEmbedDashboard` really is `false` under `flutter
  test` (i.e. this is genuinely what the engine variant's compiled
  behavior looks like), a test with the *default* constructor and an
  `openEmbedded` probe that fails the test outright if called at all
  (proving the embedded path is unreachable code, not just "unlikely to
  run"), and — via the injectable `embedDashboard: true` override, which
  makes the otherwise-dead branch reachable in a normal test run —
  fake "webview available"/"webview unavailable" probes exercising the
  embedded-success, embedded-failure-falls-back-to-browser, and
  both-fail cases, plus input validation and plain browser-launch
  failures.

## No system tray yet

There is intentionally no `window_manager` / tray-icon integration in this
first iteration — dependencies are kept to `http`, `url_launcher`,
`shared_preferences`, `yaml`, `yaml_edit` (the latter two solely for
editing `prism.yaml` in place for the Providers card), and
`desktop_webview_window` (full variant only — see **Variants** above), per
the companion plan (`docs/COMPANIONS_PLAN.md`). Tray support
(minimize-to-tray, quick actions from the tray menu) is left for a later
iteration; the window closes/quits normally like any other desktop app for
now.
