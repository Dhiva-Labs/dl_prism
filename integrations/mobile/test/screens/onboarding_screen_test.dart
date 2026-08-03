import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:dlprism/app_state.dart';
import 'package:dlprism/screens/onboarding_screen.dart';
import 'package:dlprism/services/secure_storage_backend.dart';
import 'package:dlprism/services/settings_store.dart';

class _FakeSecureStorageBackend implements SecureStorageBackend {
  final Map<String, String> data = {};
  @override
  Future<void> write(String key, String value) async => data[key] = value;
  @override
  Future<String?> read(String key) async => data[key];
  @override
  Future<void> delete(String key) async => data.remove(key);
}

/// Serves `/v1/models`, optionally requiring a specific bearer key.
class _FakeGateway {
  late HttpServer _server;
  final String? requiredKey;
  _FakeGateway({this.requiredKey});

  String get baseUrl => 'http://127.0.0.1:${_server.port}';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen((req) async {
      if (req.uri.path != '/v1/models') {
        req.response.statusCode = 404;
        await req.response.close();
        return;
      }
      final auth = req.headers.value('authorization');
      if (requiredKey != null && auth != 'Bearer $requiredKey') {
        req.response.statusCode = 401;
        req.response.headers.contentType = ContentType.json;
        req.response.write(jsonEncode({
          'error': {'message': 'invalid or missing API key', 'type': 'auth_error'},
        }));
        await req.response.close();
        return;
      }
      req.response.statusCode = 200;
      req.response.headers.contentType = ContentType.json;
      req.response.write(jsonEncode({
        'object': 'list',
        'data': [
          {'id': 'auto', 'object': 'model', 'created': 0, 'owned_by': 'prism'},
          {'id': 'gpt-test', 'object': 'model', 'created': 0, 'owned_by': 'primary'},
        ],
      }));
      await req.response.close();
    });
  }

  Future<void> stop() => _server.close(force: true);
}

Future<AppState> _buildAppState() async {
  SharedPreferences.setMockInitialValues({});
  final store = SettingsStore(secureStorage: _FakeSecureStorageBackend());
  final settings = await store.load();
  return AppState(settings: settings, store: store);
}

/// Common per-test setup for anything that exercises a real network
/// round-trip in this file:
///
///  - The onboarding form's guidance text pushes "Test connection" and the
///    result panel below the default 800x600 test surface, so a taller
///    phone-shaped surface is needed for them to be hit-testable at all.
///  - `TestWidgetsFlutterBinding` installs an `HttpOverrides` that makes
///    every `HttpClient` request return a stub HTTP 400 with no real
///    network I/O, specifically to stop widget tests from hitting the
///    network by accident. This suite hits a real loopback `HttpServer` on
///    purpose (mirroring the desktop app's and this project's own
///    `api_client_test.dart` fake-gateway pattern instead of mocking
///    `http.Client`), so that override has to come back off.
void _prepareForRealNetworkTest(WidgetTester tester) {
  tester.view.physicalSize = const Size(420, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  HttpOverrides.global = null;
}

void main() {
  testWidgets('shows a validation error for an empty base URL', (tester) async {
    _prepareForRealNetworkTest(tester);
    final appState = await _buildAppState();
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () {}),
    ));

    await tester.enterText(find.byKey(const Key('onboarding_base_url')), '');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.text('Base URL is required'), findsOneWidget);
  });

  testWidgets('shows a validation error for a malformed URL', (tester) async {
    _prepareForRealNetworkTest(tester);
    final appState = await _buildAppState();
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () {}),
    ));

    await tester.enterText(find.byKey(const Key('onboarding_base_url')), 'not a url');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Enter a full URL'), findsOneWidget);
  });

  testWidgets('rejects a public http:// host via the private-network guard', (tester) async {
    _prepareForRealNetworkTest(tester);
    final appState = await _buildAppState();
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () {}),
    ));

    await tester.enterText(find.byKey(const Key('onboarding_base_url')), 'http://8.8.8.8:8130');
    await tester.tap(find.text('Test connection'));
    await tester.pumpAndSettle();

    expect(find.textContaining('only for private/LAN or VPN addresses'), findsOneWidget);
  });

  testWidgets('a successful test connection reports the model count and enables Continue',
      (tester) async {
    _prepareForRealNetworkTest(tester);
    final gateway = _FakeGateway();
    addTearDown(gateway.stop);

    final appState = await _buildAppState();
    var connected = false;
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () => connected = true),
    ));

    // `AutomatedTestWidgetsFlutterBinding` runs the whole test body inside a
    // FakeAsync zone, under which real async I/O never actually completes
    // (see `tester.runAsync`'s doc). Both starting the fake server and
    // triggering + awaiting the request have to happen inside the same
    // `runAsync` callback so they share the real (non-fake) zone; splitting
    // them across separate `runAsync` calls (or leaving either one outside)
    // reproducibly leaves the request never reaching the server.
    await tester.runAsync(() async {
      await gateway.start();
      await tester.enterText(find.byKey(const Key('onboarding_base_url')), gateway.baseUrl);
      await tester.tap(find.text('Test connection'));
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    await tester.pumpAndSettle();

    expect(find.textContaining('Connected — 2 models available.'), findsOneWidget);

    final continueButton =
        tester.widget<FilledButton>(find.byKey(const Key('onboarding_continue')));
    expect(continueButton.onPressed, isNotNull);

    await tester.tap(find.byKey(const Key('onboarding_continue')));
    await tester.pumpAndSettle();

    expect(connected, isTrue);
    expect(appState.settings.baseUrl, gateway.baseUrl);
  });

  testWidgets('a rejected API key is reported distinctly and Continue stays disabled',
      (tester) async {
    _prepareForRealNetworkTest(tester);
    final gateway = _FakeGateway(requiredKey: 'correct-key');
    addTearDown(gateway.stop);

    final appState = await _buildAppState();
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () {}),
    ));

    await tester.runAsync(() async {
      await gateway.start();
      await tester.enterText(find.byKey(const Key('onboarding_base_url')), gateway.baseUrl);
      await tester.enterText(find.byKey(const Key('onboarding_api_key')), 'wrong-key');
      await tester.tap(find.text('Test connection'));
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    await tester.pumpAndSettle();

    expect(find.textContaining('API key was rejected'), findsOneWidget);
    final continueButton =
        tester.widget<FilledButton>(find.byKey(const Key('onboarding_continue')));
    expect(continueButton.onPressed, isNull);
  });

  testWidgets('an unreachable gateway is reported and Continue stays disabled', (tester) async {
    _prepareForRealNetworkTest(tester);
    final appState = await _buildAppState();
    await tester.pumpWidget(MaterialApp(
      home: OnboardingScreen(appState: appState, onConnected: () {}),
    ));

    await tester.runAsync(() async {
      await tester.enterText(
        find.byKey(const Key('onboarding_base_url')),
        'http://127.0.0.1:1', // nothing listens on port 1: real ECONNREFUSED
      );
      await tester.tap(find.text('Test connection'));
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('onboarding_result')), findsOneWidget);
    final continueButton =
        tester.widget<FilledButton>(find.byKey(const Key('onboarding_continue')));
    expect(continueButton.onPressed, isNull);
  });
}
