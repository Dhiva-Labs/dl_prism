#!/usr/bin/env bash
#
# Builds both DL Prism Desktop variants:
#
#   full   — embedded WebKitGTK dashboard window (package:desktop_webview_window)
#   engine — no webview dependency at all; "Open dashboard" always uses the browser
#
# WHY THIS SCRIPT EXISTS (read before changing it)
# --------------------------------------------------
# A Dart-level `const bool kEmbedDashboard = bool.fromEnvironment(...)` flag
# is NOT enough to produce a real "engine" bundle that runs on a machine
# without libwebkit2gtk-4.1. Verified empirically on this machine: with
# `desktop_webview_window` merely present in pubspec.yaml, both the main
# executable (build/linux/x64/release/bundle/desktop) and its bundled
# lib/libdesktop_webview_window_plugin.so carry a hard ELF `NEEDED` entry
# for libwebkit2gtk-4.1.so.0 (`readelf -d`), because:
#   - Flutter's generated linux/flutter/generated_plugins.cmake does
#     `target_link_libraries(${BINARY_NAME} PRIVATE ${plugin}_plugin)` for
#     every plugin declared in pubspec.yaml, unconditionally.
#   - generated_plugin_registrant.cc's fl_register_plugins() unconditionally
#     calls every plugin's *_register_with_registrar() at native/GTK
#     startup, before any Dart code (including this flag) runs.
#   - The plugin's own linux/CMakeLists.txt does
#     `pkg_check_modules(WebKit REQUIRED IMPORTED_TARGET webkit2gtk-4.1)`
#     and links its .so against it.
# So the OS dynamic linker refuses to even start the process on a machine
# missing libwebkit2gtk-4.1.so.0 (or its own transitive deps
# libsoup-3.0.so.0 / libjavascriptcoregtk-4.1.so.0) — a runtime flag can't
# prevent that; the dependency has to be genuinely absent from the build.
#
# (A Dart conditional export/import keyed on a custom --dart-define was
# tried as an alternative — `export 'a.dart' if (EMBED_DASHBOARD == 'true')
# 'b.dart';` — so that the plugin import itself would be source-level
# conditional. It does not work reliably on Dart 3.12.2: verified with
# `dart run`/`dart compile exe -DEMBED_DASHBOARD=true`, the branch always
# resolved to the default regardless of the define's value. Only
# `dart.library.*` conditions were observed to actually switch branches.
# So this script does the swap itself instead of relying on that.)
#
# THE APPROACH
# ------------
# 1. Build "full" first, using the working tree exactly as committed (it
#    normally contains the real dependency and implementation).
# 2. Temporarily: remove the desktop_webview_window line from pubspec.yaml
#    (sed) and overwrite lib/services/dashboard_webview.dart with a stub
#    that has no reference to that package.
# 3. `flutter pub get` (regenerates pubspec.lock and, on next build, the
#    Linux plugin registrant/CMake files without the dependency) and build
#    "engine".
# 4. ALWAYS restore both files to their original content (trap on EXIT),
#    and run `flutter pub get` once more so the working tree — including
#    generated Linux build files — ends up back in the "full" state.
#
# Usage: tool/build_variants.sh [--skip-full] [--skip-engine]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

DIST_DIR="$ROOT_DIR/dist"
FULL_DIR="$DIST_DIR/dl-prism-desktop-full"
ENGINE_DIR="$DIST_DIR/dl-prism-desktop-engine"
BUNDLE_SRC="$ROOT_DIR/build/linux/x64/release/bundle"

PUBSPEC="$ROOT_DIR/pubspec.yaml"
WEBVIEW_FILE="$ROOT_DIR/lib/services/dashboard_webview.dart"

SKIP_FULL=0
SKIP_ENGINE=0
for arg in "$@"; do
  case "$arg" in
    --skip-full) SKIP_FULL=1 ;;
    --skip-engine) SKIP_ENGINE=1 ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

log() { printf '\n\033[1;36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$1" >&2; }

copy_bundle_to() {
  local dest="$1"
  rm -rf "$dest"
  mkdir -p "$(dirname "$dest")"
  cp -r "$BUNDLE_SRC" "$dest"
}

bundle_size() {
  du -sh "$1" 2>/dev/null | cut -f1
}

check_webkit_devel() {
  if ! pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
    cat >&2 <<'EOF'

error: webkit2gtk-4.1 development files were not found via pkg-config.

The "full" variant needs them to compile package:desktop_webview_window's
Linux plugin. Install them with:

    sudo apt install libwebkit2gtk-4.1-dev

(On a machine without network/sudo access, you can instead download the
.deb with `apt-get download libwebkit2gtk-4.1-dev` — this works without
root — extract it with `dpkg -x` into a local directory, fix up its
prefix=/libdir= lines in the extracted .pc files to point at that
directory, and point PKG_CONFIG_PATH at the extracted pkgconfig dir. Only
do this as a last resort; the apt package is the supported path.)
EOF
    exit 1
  fi
}

# ---------------------------------------------------------------------------
# Always restore the working tree to the "full" configuration on exit,
# whether we get here by success, failure, or interruption.
# ---------------------------------------------------------------------------
RESTORE_NEEDED=0
restore_full_state() {
  if [ "$RESTORE_NEEDED" -eq 1 ]; then
    log "Restoring working tree to the full configuration"
    if [ -f "$PUBSPEC.full.bak" ]; then
      mv "$PUBSPEC.full.bak" "$PUBSPEC"
    fi
    if [ -f "$WEBVIEW_FILE.full.bak" ]; then
      mv "$WEBVIEW_FILE.full.bak" "$WEBVIEW_FILE"
    fi
    flutter pub get >/dev/null
    RESTORE_NEEDED=0
  fi
}
trap restore_full_state EXIT

# ---------------------------------------------------------------------------
# 1. full — the working tree as committed.
# ---------------------------------------------------------------------------
if [ "$SKIP_FULL" -eq 0 ]; then
  log "Building FULL variant (embedded WebKitGTK dashboard)"
  check_webkit_devel
  flutter pub get
  flutter build linux --release --dart-define=EMBED_DASHBOARD=true
  copy_bundle_to "$FULL_DIR"
  log "full bundle: $FULL_DIR ($(bundle_size "$FULL_DIR"))"
else
  log "Skipping full variant (--skip-full)"
fi

# ---------------------------------------------------------------------------
# 2/3. engine — dependency removed, stub implementation swapped in.
# ---------------------------------------------------------------------------
if [ "$SKIP_ENGINE" -eq 0 ]; then
  log "Preparing engine variant: removing desktop_webview_window dependency"
  cp "$PUBSPEC" "$PUBSPEC.full.bak"
  cp "$WEBVIEW_FILE" "$WEBVIEW_FILE.full.bak"
  RESTORE_NEEDED=1

  # Remove the single dependency line. This assumes the standard pubspec.yaml
  # layout used in this project (one `  desktop_webview_window: ^x.y.z` line
  # under `dependencies:`); if that line's formatting changes, update this.
  sed -i '/^  desktop_webview_window:/d' "$PUBSPEC"
  if grep -q 'desktop_webview_window' "$PUBSPEC"; then
    echo "error: failed to remove desktop_webview_window from pubspec.yaml" >&2
    exit 1
  fi

  cat > "$WEBVIEW_FILE" <<'EOF'
/// STUB — generated by tool/build_variants.sh for the "engine" build.
///
/// The real implementation (checked into the repo) lives in this same
/// file and imports package:desktop_webview_window; this stub replaces it
/// only for the duration of the engine build, then is restored. See that
/// file's own doc comment (in the working tree's normal/full state) and
/// lib/config/build_variant.dart for why a runtime flag alone can't do
/// this — the dependency has to be genuinely absent from pubspec.yaml.
Future<void> openDashboardWebview(String url) async {
  throw UnsupportedError(
    'This build was compiled without embedded-dashboard support.',
  );
}
EOF

  log "Building ENGINE variant (browser-only dashboard)"
  flutter pub get
  flutter build linux --release --dart-define=EMBED_DASHBOARD=false
  copy_bundle_to "$ENGINE_DIR"
  log "engine bundle: $ENGINE_DIR ($(bundle_size "$ENGINE_DIR"))"

  restore_full_state
  trap - EXIT
else
  log "Skipping engine variant (--skip-engine)"
fi

log "Done."
if [ -d "$FULL_DIR" ]; then
  echo "  full:   $FULL_DIR ($(bundle_size "$FULL_DIR"))"
fi
if [ -d "$ENGINE_DIR" ]; then
  echo "  engine: $ENGINE_DIR ($(bundle_size "$ENGINE_DIR"))"
fi
