import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/data/provider_presets.dart';
import 'package:desktop/widgets/providers_card.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: SingleChildScrollView(child: child)));

InputChip _chipFor(WidgetTester tester, String displayName) =>
    tester.widget<InputChip>(find.widgetWithText(InputChip, displayName));

void main() {
  testWidgets('renders all twelve preset chips, none connected', (tester) async {
    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    expect(kProviderPresets, hasLength(12));
    expect(find.byType(InputChip), findsNWidgets(12));
    for (final preset in kProviderPresets) {
      expect(find.text(preset.displayName), findsOneWidget);
      final chip = _chipFor(tester, preset.displayName);
      expect(chip.selected, isFalse);
      expect(chip.onDeleted, isNull);
      expect(chip.onPressed, isNotNull);
    }
  });

  testWidgets('one connected: that chip is selected and offers disconnect, others do not',
      (tester) async {
    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {'anthropic'},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    final anthropic = kProviderPresets.firstWhere((p) => p.name == 'anthropic');
    final connectedChip = _chipFor(tester, anthropic.displayName);
    expect(connectedChip.selected, isTrue);
    expect(connectedChip.onDeleted, isNotNull);

    for (final preset in kProviderPresets.where((p) => p.name != 'anthropic')) {
      final chip = _chipFor(tester, preset.displayName);
      expect(chip.selected, isFalse, reason: preset.name);
      expect(chip.onDeleted, isNull, reason: preset.name);
    }

    expect(find.byIcon(Icons.link_off), findsOneWidget);
  });

  testWidgets('connected chips are sorted first', (tester) async {
    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {'zai', 'groq'},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    final chipTexts = tester
        .widgetList<InputChip>(find.byType(InputChip))
        .map((c) => (c.label as Text).data)
        .toList();

    final zaiName = kProviderPresets.firstWhere((p) => p.name == 'zai').displayName;
    final groqName = kProviderPresets.firstWhere((p) => p.name == 'groq').displayName;
    expect(chipTexts.sublist(0, 2), containsAll([zaiName, groqName]));
  });

  testWidgets('a busy provider shows a spinner and disables interaction', (tester) async {
    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {},
      busyNames: const {'anthropic'},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    final anthropic = kProviderPresets.firstWhere((p) => p.name == 'anthropic');
    final chip = _chipFor(tester, anthropic.displayName);
    expect(chip.onPressed, isNull);
  });

  testWidgets('connect dialog blocks submission with an empty API key', (tester) async {
    var connectCalled = false;
    final first = kProviderPresets.first;

    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {},
      busyNames: const {},
      onConnect: (_, _) async {
        connectCalled = true;
      },
      onDisconnect: (_) async {},
    )));

    await tester.tap(find.widgetWithText(InputChip, first.displayName));
    await tester.pumpAndSettle();

    expect(find.text('API key'), findsOneWidget);
    expect(find.textContaining('Adds these models'), findsOneWidget);
    expect(find.textContaining(first.models.first.id), findsOneWidget);

    // Submit with an empty field.
    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(find.text('API key must not be empty'), findsOneWidget);
    expect(connectCalled, isFalse);
    expect(find.byType(AlertDialog), findsOneWidget);
  });

  testWidgets('connect dialog submits the trimmed key and closes', (tester) async {
    ProviderPreset? connectedPreset;
    String? connectedKey;
    final first = kProviderPresets.first;

    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {},
      busyNames: const {},
      onConnect: (preset, key) async {
        connectedPreset = preset;
        connectedKey = key;
      },
      onDisconnect: (_) async {},
    )));

    await tester.tap(find.widgetWithText(InputChip, first.displayName));
    await tester.pumpAndSettle();

    await tester.enterText(find.widgetWithText(TextFormField, 'API key'), '  sk-test-123  ');
    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
    expect(connectedPreset?.name, first.name);
    expect(connectedKey, 'sk-test-123');
  });

  testWidgets('reconnecting an already-connected preset labels the dialog action Replace',
      (tester) async {
    final first = kProviderPresets.first;

    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: {first.name},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    await tester.tap(find.widgetWithText(InputChip, first.displayName));
    await tester.pumpAndSettle();

    expect(find.widgetWithText(FilledButton, 'Replace'), findsOneWidget);
    expect(find.textContaining('already exists'), findsOneWidget);
  });

  testWidgets('disconnect asks for confirmation before calling onDisconnect', (tester) async {
    String? disconnected;
    final first = kProviderPresets.first;

    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: {first.name},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (name) async {
        disconnected = name;
      },
    )));

    await tester.tap(find.byIcon(Icons.link_off));
    await tester.pumpAndSettle();

    expect(find.text('Disconnect provider?'), findsOneWidget);
    expect(disconnected, isNull);

    await tester.tap(find.widgetWithText(FilledButton, 'Disconnect'));
    await tester.pumpAndSettle();

    expect(disconnected, first.name);
  });

  testWidgets('the google preset dialog lists both of its models', (tester) async {
    final google = kProviderPresets.firstWhere((p) => p.name == 'google');
    expect(google.models, hasLength(2));

    await tester.pumpWidget(_wrap(ProvidersCard(
      connectedNames: const {},
      busyNames: const {},
      onConnect: (_, _) async {},
      onDisconnect: (_) async {},
    )));

    await tester.tap(find.widgetWithText(InputChip, google.displayName));
    await tester.pumpAndSettle();

    for (final model in google.models) {
      expect(find.textContaining(model.id), findsOneWidget);
    }
  });
}
