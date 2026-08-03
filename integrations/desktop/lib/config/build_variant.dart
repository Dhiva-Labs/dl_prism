/// Compile-time build-variant flag.
///
/// Pass `--dart-define=EMBED_DASHBOARD=true` to build the **full** variant
/// (adds an embedded WebKitGTK dashboard window via
/// `package:desktop_webview_window`). The default — no define, or any
/// value other than the literal string `true` — is the **engine** (lite)
/// variant: "Open dashboard" always uses the system browser.
///
/// Flipping this flag alone is NOT enough to produce a lite bundle that
/// runs on a machine without `libwebkit2gtk-4.1`: Flutter's Linux plugin
/// system links every plugin declared in `pubspec.yaml` directly into the
/// runner executable at build time (see `generated_plugins.cmake`) and
/// unconditionally registers it at process startup (see
/// `generated_plugin_registrant.cc`'s `fl_register_plugins`), regardless
/// of any Dart-level const — this was verified empirically for this exact
/// dependency: with `desktop_webview_window` merely present in
/// `pubspec.yaml`, both `build/linux/.../bundle/desktop` (the main
/// executable) and its bundled `lib/libdesktop_webview_window_plugin.so`
/// carry a hard ELF `NEEDED` entry for `libwebkit2gtk-4.1.so.0` (checked
/// via `readelf -d`), transitively pulling in `libsoup-3.0.so.0` and
/// `libjavascriptcoregtk-4.1.so.0` too. The OS dynamic linker refuses to
/// even start the process if those aren't resolvable — before any Dart
/// code (including this flag) ever runs.
///
/// (A Dart conditional export/import keyed on a custom `--dart-define`,
/// e.g. `export 'a.dart' if (EMBED_DASHBOARD == 'true') 'b.dart';`, was
/// tried first as a way to make the plugin import itself conditional at
/// the source level. It doesn't work reliably on this SDK (3.12.2) —
/// verified with `dart run`/`dart compile exe` passing `-DEMBED_DASHBOARD=
/// true`: the branch always resolved to the default, regardless of the
/// define's value. Only `dart.library.*` conditions were observed to
/// actually switch branches. So this project does NOT rely on that
/// mechanism.)
///
/// `tool/build_variants.sh` therefore builds the engine variant by (a)
/// removing the `desktop_webview_window` line from `pubspec.yaml` and (b)
/// swapping `lib/services/dashboard_webview.dart`'s content for a stub
/// with no reference to that package, building, then restoring both
/// files — not by relying on this flag to keep the plugin import "dead".
/// This flag only controls the Dart-level runtime choice: whether
/// [DashboardLauncher] (`lib/services/dashboard_launcher.dart`) ever
/// *attempts* the embedded path at all.
const bool kEmbedDashboard = bool.fromEnvironment('EMBED_DASHBOARD');
