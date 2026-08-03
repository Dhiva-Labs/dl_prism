import 'package:desktop_webview_window/desktop_webview_window.dart';

/// Opens the gateway dashboard in a separate, chromeless WebKitGTK window.
///
/// This is the ONLY file in the app allowed to import
/// `package:desktop_webview_window`. Isolating the import here (rather than
/// scattering `if (kEmbedDashboard)`-guarded imports through the UI) is
/// what lets `tool/build_variants.sh` produce a genuine "engine" (lite)
/// bundle: it temporarily overwrites *this file's content* with a stub
/// that has no such import (and removes the pubspec dependency) before
/// building that variant, then restores this file afterward. See
/// `lib/config/build_variant.dart` for why that's necessary — a runtime
/// flag alone can't prevent a Linux Flutter plugin from being linked into
/// the executable.
///
/// The Linux implementation of `desktop_webview_window` opens a genuinely
/// separate native GTK/WebKitGTK window (optionally via a helper process)
/// rather than an inline widget embedded in the Flutter view — Flutter's
/// Linux embedder has no supported way to host a foreign GTK widget (like
/// a WebKitGTK WebView) inline, so a standalone chromeless window is the
/// only viable approach here, and is what this produces.
///
/// Throws if the native webview couldn't be created (e.g. the system's
/// `libwebkit2gtk-4.1` is missing, mismatched, or failed to initialize at
/// runtime) — callers (see `DashboardLauncher`) must catch this and fall
/// back to the system browser.
Future<void> openDashboardWebview(String url) async {
  final webview = await WebviewWindow.create(
    configuration: const CreateConfiguration(
      windowWidth: 1200,
      windowHeight: 800,
      title: 'DL Prism Dashboard',
    ),
  );
  webview.launch(url);
}
