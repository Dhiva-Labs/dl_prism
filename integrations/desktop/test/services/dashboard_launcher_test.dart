import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/config/build_variant.dart';
import 'package:desktop/services/dashboard_launcher.dart';

void main() {
  group('flag gating (engine build behavior)', () {
    test('kEmbedDashboard is false by default (this is how `flutter test` — '
        'and the engine variant — compile)', () {
      // Sanity check on the premise of the next test: if this ever starts
      // failing it means the test binary was compiled with
      // --dart-define=EMBED_DASHBOARD=true, which would make the "never
      // calls openEmbedded" test below meaningless.
      expect(kEmbedDashboard, isFalse);
    });

    test(
      'with the default (real, compile-time) flag, open() never calls '
      'openEmbedded and goes straight to the browser',
      () async {
        var embeddedCalls = 0;
        Uri? launchedUri;
        final launcher = DashboardLauncher(
          // Deliberately NOT passing embedDashboard: this exercises the
          // real kEmbedDashboard const, i.e. exactly what the engine
          // build's compiled behavior is.
          openEmbedded: (url) async {
            embeddedCalls++;
            throw StateError('must never be called in the engine variant');
          },
          canLaunch: (uri) async => true,
          launchInBrowser: (uri) async {
            launchedUri = uri;
            return true;
          },
        );

        final result = await launcher.open('http://127.0.0.1:8130');

        expect(embeddedCalls, 0, reason: 'no webview construction attempted at all');
        expect(result, DashboardOpenResult.openedBrowser);
        expect(launchedUri, Uri.parse('http://127.0.0.1:8130'));
      },
    );

    test('embedDashboard: false behaves identically to the default', () async {
      var embeddedCalls = 0;
      final launcher = DashboardLauncher(
        embedDashboard: false,
        openEmbedded: (url) async => embeddedCalls++,
        canLaunch: (uri) async => true,
        launchInBrowser: (uri) async => true,
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(embeddedCalls, 0);
      expect(result, DashboardOpenResult.openedBrowser);
    });
  });

  group('embedded-path fallback (embedDashboard: true, full-variant behavior)', () {
    test('a successful "webview available" probe opens embedded and never touches the browser',
        () async {
      var browserCalls = 0;
      String? embeddedUrl;
      final launcher = DashboardLauncher(
        embedDashboard: true,
        openEmbedded: (url) async {
          embeddedUrl = url;
        },
        canLaunch: (uri) async {
          browserCalls++;
          return true;
        },
        launchInBrowser: (uri) async {
          browserCalls++;
          return true;
        },
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(result, DashboardOpenResult.openedEmbedded);
      expect(embeddedUrl, 'http://127.0.0.1:8130');
      expect(browserCalls, 0);
    });

    test('a failing "webview unavailable" probe falls back to the browser', () async {
      var embeddedAttempted = false;
      Uri? launchedUri;
      final launcher = DashboardLauncher(
        embedDashboard: true,
        openEmbedded: (url) async {
          embeddedAttempted = true;
          throw Exception('libwebkit2gtk-4.1 not found');
        },
        canLaunch: (uri) async => true,
        launchInBrowser: (uri) async {
          launchedUri = uri;
          return true;
        },
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(embeddedAttempted, isTrue);
      expect(result, DashboardOpenResult.openedBrowserAfterEmbeddedFailure);
      expect(launchedUri, Uri.parse('http://127.0.0.1:8130'));
    });

    test('embedded failure AND browser failure both surface as failed', () async {
      final launcher = DashboardLauncher(
        embedDashboard: true,
        openEmbedded: (url) async => throw Exception('no webview'),
        canLaunch: (uri) async => false,
        launchInBrowser: (uri) async => true,
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(result, DashboardOpenResult.failed);
    });
  });

  group('input validation and plain browser-launch failures', () {
    test('an unparseable/schemeless base URL returns invalidUrl without calling anything',
        () async {
      var anyCalled = false;
      final launcher = DashboardLauncher(
        embedDashboard: true,
        openEmbedded: (url) async => anyCalled = true,
        canLaunch: (uri) async {
          anyCalled = true;
          return true;
        },
        launchInBrowser: (uri) async {
          anyCalled = true;
          return true;
        },
      );

      final result = await launcher.open('not a url');

      expect(result, DashboardOpenResult.invalidUrl);
      expect(anyCalled, isFalse);
    });

    test('canLaunch() returning false yields failed without attempting to launch', () async {
      var launchCalled = false;
      final launcher = DashboardLauncher(
        embedDashboard: false,
        canLaunch: (uri) async => false,
        launchInBrowser: (uri) async {
          launchCalled = true;
          return true;
        },
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(result, DashboardOpenResult.failed);
      expect(launchCalled, isFalse);
    });

    test('launchInBrowser() returning false yields failed', () async {
      final launcher = DashboardLauncher(
        embedDashboard: false,
        canLaunch: (uri) async => true,
        launchInBrowser: (uri) async => false,
      );

      final result = await launcher.open('http://127.0.0.1:8130');

      expect(result, DashboardOpenResult.failed);
    });
  });
}
