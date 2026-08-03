import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:dlprism/app_state.dart';
import 'package:dlprism/screens/usage_screen.dart';
import 'package:dlprism/services/api_client.dart';
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

Future<AppState> _buildAppState(
  Future<http.Response> Function(http.Request request) handleUsage,
) async {
  SharedPreferences.setMockInitialValues({});
  final store = SettingsStore(secureStorage: _FakeSecureStorageBackend());
  final settings = await store.load();
  final mockClient = MockClient((request) => handleUsage(request));
  final client = PrismApiClient(baseUrl: 'http://mock.invalid', client: mockClient);
  return AppState(settings: settings, store: store, client: client);
}

http.Response _usageJson(Map<String, dynamic> body, {int status = 200}) {
  return http.Response(jsonEncode(body), status,
      headers: {'content-type': 'application/json'});
}

void main() {
  testWidgets('under budget: shows spend, progress bar, and no exceeded chip', (tester) async {
    final appState = await _buildAppState((_) async => _usageJson({
          'month_total_usd': 3.5,
          'budget_usd': 10.0,
          'budget_exceeded': false,
          'models': [
            {
              'provider': 'fakelocal',
              'model': 'qwen2.5-coder:14b',
              'requests': 5,
              'prompt_tokens': 100,
              'completion_tokens': 200,
              'cost_usd': 3.5,
            },
          ],
        }));

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('\$3.50 / \$10.00'), findsOneWidget);
    expect(find.byKey(const Key('usage_budget_exceeded_chip')), findsNothing);
    expect(find.text('qwen2.5-coder:14b'), findsOneWidget);
    expect(find.textContaining('fakelocal'), findsOneWidget);

    final progress = tester.widget<LinearProgressIndicator>(find.byType(LinearProgressIndicator));
    expect(progress.value, closeTo(0.35, 0.001));
  });

  testWidgets('budget exceeded: shows the exceeded chip and a red progress bar', (tester) async {
    final appState = await _buildAppState((_) async => _usageJson({
          'month_total_usd': 12.0,
          'budget_usd': 10.0,
          'budget_exceeded': true,
          'models': <Map<String, dynamic>>[],
        }));

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('\$12.00 / \$10.00'), findsOneWidget);
    expect(find.byKey(const Key('usage_budget_exceeded_chip')), findsOneWidget);
    expect(find.text('Budget exceeded'), findsOneWidget);

    final progress = tester.widget<LinearProgressIndicator>(find.byType(LinearProgressIndicator));
    // budgetFraction clamps to 1.0 even though spend exceeds budget.
    expect(progress.value, 1.0);
  });

  testWidgets('no budget configured: shows spend without a fraction-based bar', (tester) async {
    final appState = await _buildAppState((_) async => _usageJson({
          'month_total_usd': 1.0,
          'budget_usd': 0.0,
          'budget_exceeded': false,
          'models': <Map<String, dynamic>>[],
        }));

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('\$1.00 / \$0.00'), findsOneWidget);
    expect(find.textContaining('No budget configured'), findsOneWidget);
    final progress = tester.widget<LinearProgressIndicator>(find.byType(LinearProgressIndicator));
    expect(progress.value, 0);
  });

  testWidgets('no requests yet this month shows an empty state, not a blank list', (tester) async {
    final appState = await _buildAppState((_) async => _usageJson({
          'month_total_usd': 0.0,
          'budget_usd': 10.0,
          'budget_exceeded': false,
          'models': <Map<String, dynamic>>[],
        }));

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('No requests yet this month.'), findsOneWidget);
  });

  testWidgets('an unreachable gateway shows a banner with retry, never a blank screen',
      (tester) async {
    var attempts = 0;
    final appState = await _buildAppState((_) async {
      attempts++;
      throw Exception('Connection refused');
    });

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.cloud_off), findsOneWidget);
    expect(find.text('Retry'), findsOneWidget);
    expect(attempts, 1);

    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(attempts, 2);
  });

  testWidgets('a 401 shows the inline reauth prompt instead of the usage card', (tester) async {
    final appState = await _buildAppState((_) async => _usageJson({
          'error': {'message': 'invalid or missing API key', 'type': 'auth_error'},
        }, status: 401));

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const UsageScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Authentication needed'), findsOneWidget);
    expect(find.byKey(const Key('usage_budget_exceeded_chip')), findsNothing);
  });
}
