import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:desktop/screens/settings_screen.dart';
import 'package:desktop/services/settings_store.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('saving persists settings and pops them back to the caller', (tester) async {
    // The settings form now has six fields plus Save/Cancel; at the default
    // 800x600 test surface the trailing button row falls beyond the
    // scrollable ListView's initially-built extent (sliver children beyond
    // the viewport aren't materialized until scrolled into view). Widen the
    // surface instead of teaching every test to scroll first.
    tester.view.physicalSize = const Size(900, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = SettingsStore();
    AppSettings? popped;

    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => ElevatedButton(
          child: const Text('open'),
          onPressed: () async {
            popped = await Navigator.of(context).push<AppSettings>(
              MaterialPageRoute(
                builder: (_) => SettingsScreen(
                  initial: const AppSettings(),
                  store: store,
                ),
              ),
            );
          },
        ),
      ),
    ));

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.widgetWithText(TextFormField, 'Gateway base URL'),
      'http://example.local:8130',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'prism binary path'),
      '/opt/prism/bin/prism',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'Ollama base URL'),
      'http://localhost:11500',
    );

    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(popped, isNotNull);
    expect(popped!.baseUrl, 'http://example.local:8130');
    expect(popped!.binaryPath, '/opt/prism/bin/prism');
    expect(popped!.ollamaBaseUrl, 'http://localhost:11500');

    final persisted = await store.load();
    expect(persisted.baseUrl, 'http://example.local:8130');
    expect(persisted.ollamaBaseUrl, 'http://localhost:11500');
  });

  testWidgets('an empty base URL fails validation and blocks save', (tester) async {
    tester.view.physicalSize = const Size(900, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = SettingsStore();

    await tester.pumpWidget(MaterialApp(
      home: SettingsScreen(initial: const AppSettings(), store: store),
    ));

    await tester.enterText(find.widgetWithText(TextFormField, 'Gateway base URL'), '');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(find.text('Base URL is required'), findsOneWidget);
  });
}
