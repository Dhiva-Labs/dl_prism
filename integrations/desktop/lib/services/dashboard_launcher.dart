import 'package:url_launcher/url_launcher.dart';

import '../config/build_variant.dart';
import 'dashboard_webview.dart' as webview;

/// What happened when [DashboardLauncher.open] was called.
enum DashboardOpenResult {
  /// Opened in the embedded WebKitGTK window (full variant only).
  openedEmbedded,

  /// Opened in the system browser — the normal, unremarkable path for the
  /// engine variant (or the full variant when `kEmbedDashboard` is false
  /// at runtime). Nothing worth telling the user here.
  openedBrowser,

  /// The embedded attempt failed (e.g. `libwebkit2gtk-4.1` missing or
  /// failed to initialize) and this call fell back to the browser, which
  /// succeeded. Distinct from [openedBrowser] so the caller knows to show
  /// a one-line explanation — the user asked for the embedded dashboard
  /// and silently got something else instead.
  openedBrowserAfterEmbeddedFailure,

  /// The embedded attempt failed and the browser fallback ALSO failed
  /// (e.g. no browser handler registered for http(s) URLs).
  failed,

  /// The configured base URL isn't a valid URL at all.
  invalidUrl,
}

/// Coordinates "open the dashboard".
///
/// In the full variant this tries the embedded WebKitGTK window first,
/// falling back to the system browser if that throws (e.g.
/// `libwebkit2gtk-4.1` missing or failed to initialize on the user's
/// machine) — with [DashboardOpenResult] telling the caller which
/// happened, so it can show an explanatory snackbar only when a fallback
/// actually occurred. In the engine variant it always uses the browser.
///
/// Every collaborator is constructor-injectable, including [embedDashboard]
/// itself (defaulting to the real compile-time [kEmbedDashboard]):
///  - The default-constructed [DashboardLauncher] exercises exactly what
///    the engine build does, since `flutter test` compiles with
///    `kEmbedDashboard` at its default (`false`) — a test using the
///    default constructor and an [openEmbedded] probe that would fail the
///    test if called is a direct check that the embedded path is truly
///    unreachable code for that variant, not just "unlikely to run".
///  - Passing `embedDashboard: true` lets a test reach the embedded-vs-
///    fallback branch and exercise it against a fake "webview available"
///    probe (a fast-failing or fast-succeeding [openEmbedded]) without
///    ever touching the real native plugin — see
///    `test/services/dashboard_launcher_test.dart`.
class DashboardLauncher {
  final bool embedDashboard;
  final Future<void> Function(String url) openEmbedded;
  final Future<bool> Function(Uri uri) launchInBrowser;
  final Future<bool> Function(Uri uri) canLaunch;

  DashboardLauncher({
    bool? embedDashboard,
    Future<void> Function(String url)? openEmbedded,
    Future<bool> Function(Uri uri)? launchInBrowser,
    Future<bool> Function(Uri uri)? canLaunch,
  })  : embedDashboard = embedDashboard ?? kEmbedDashboard,
        openEmbedded = openEmbedded ?? webview.openDashboardWebview,
        launchInBrowser = launchInBrowser ??
            ((uri) => launchUrl(uri, mode: LaunchMode.externalApplication)),
        canLaunch = canLaunch ?? canLaunchUrl;

  Future<DashboardOpenResult> open(String baseUrl) async {
    final uri = Uri.tryParse(baseUrl);
    if (uri == null || !uri.hasScheme) return DashboardOpenResult.invalidUrl;

    var embeddedFailed = false;
    if (embedDashboard) {
      try {
        await openEmbedded(baseUrl);
        return DashboardOpenResult.openedEmbedded;
      } catch (_) {
        embeddedFailed = true;
        // Fall through to the browser fallback below.
      }
    }

    final opened = await _openInBrowser(uri);
    if (!opened) return DashboardOpenResult.failed;
    return embeddedFailed
        ? DashboardOpenResult.openedBrowserAfterEmbeddedFailure
        : DashboardOpenResult.openedBrowser;
  }

  Future<bool> _openInBrowser(Uri uri) async {
    if (!await canLaunch(uri)) return false;
    return launchInBrowser(uri);
  }
}
