import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/models/ollama_model.dart';
import 'package:desktop/models/pull_progress.dart';
import 'package:desktop/widgets/local_models_card.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: child));

/// Pumps [child] in a tall virtual viewport. `LocalModelsCard` now includes
/// the full model catalog (10+ rows) and is meant to live inside
/// [HomeScreen]'s scrollable body — outside a scrollable ancestor, at the
/// default 800x600 test surface size it would genuinely overflow. Widening
/// the test surface here is simpler and more robust than teaching every
/// test to scroll to whatever it needs to tap.
Future<void> _pump(WidgetTester tester, Widget child) async {
  tester.view.physicalSize = const Size(1000, 3000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(_wrap(child));
}

void main() {
  testWidgets('unreachable: shows "Ollama not detected" with Install and Retry',
      (tester) async {
    var installTapped = false;
    var retryTapped = false;

    await _pump(tester, LocalModelsCard(
      reachable: false,
      installedModels: const [],
      loadedModelNames: const {},
      busyModelNames: const {},
      onRetry: () => retryTapped = true,
      onInstallOllama: () => installTapped = true,
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (_) {},
      onCancelPull: () {},
    ));

    expect(find.text('Ollama not detected.'), findsOneWidget);
    expect(find.text('Install Ollama'), findsOneWidget);
    expect(find.text('Retry'), findsOneWidget);

    await tester.tap(find.text('Install Ollama'));
    await tester.tap(find.text('Retry'));
    expect(installTapped, isTrue);
    expect(retryTapped, isTrue);
  });

  testWidgets('reachable with no models shows a starter-model hint', (tester) async {
    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: const [],
      loadedModelNames: const {},
      busyModelNames: const {},
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (_) {},
      onCancelPull: () {},
    ));

    expect(find.textContaining('No models installed yet'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Pull something else'), findsOneWidget);
  });

  testWidgets('lists installed models, shows a loaded chip, and Run/Stop appropriately',
      (tester) async {
    const models = [
      OllamaModel(name: 'qwen2.5-coder:7b', sizeBytes: 4683087561),
      OllamaModel(name: 'llama3.2:1b', sizeBytes: 1300000000),
    ];

    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: models,
      loadedModelNames: const {'qwen2.5-coder:7b'},
      busyModelNames: const {},
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (_) {},
      onCancelPull: () {},
    ));

    expect(find.text('qwen2.5-coder:7b'), findsOneWidget);
    expect(find.text('llama3.2:1b'), findsOneWidget);
    expect(find.text('loaded'), findsOneWidget);
    expect(find.text('4.4 GB'), findsOneWidget);

    // Loaded model gets Stop, unloaded model gets Run.
    expect(find.widgetWithText(TextButton, 'Stop'), findsOneWidget);
    expect(find.widgetWithText(TextButton, 'Run'), findsOneWidget);
  });

  testWidgets('Delete requires confirmation before invoking onDelete', (tester) async {
    const models = [OllamaModel(name: 'llama3.2:1b', sizeBytes: 1300000000)];
    String? deleted;

    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: models,
      loadedModelNames: const {},
      busyModelNames: const {},
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (name) => deleted = name,
      onPull: (_) {},
      onCancelPull: () {},
    ));

    await tester.tap(find.byIcon(Icons.delete_outline));
    await tester.pumpAndSettle();

    expect(find.text('Delete model?'), findsOneWidget);
    expect(deleted, isNull);

    await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
    await tester.pumpAndSettle();
    expect(deleted, isNull);

    await tester.tap(find.byIcon(Icons.delete_outline));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
    await tester.pumpAndSettle();

    expect(deleted, 'llama3.2:1b');
  });

  testWidgets('submitting the pull field invokes onPull with the typed name', (tester) async {
    String? pulled;

    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: const [],
      loadedModelNames: const {},
      busyModelNames: const {},
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (name) => pulled = name,
      onCancelPull: () {},
    ));

    await tester.enterText(
        find.widgetWithText(TextField, 'Pull something else'), 'mistral-nemo:12b');
    await tester.tap(find.widgetWithText(FilledButton, 'Pull'));
    await tester.pump();

    expect(pulled, 'mistral-nemo:12b');
  });

  testWidgets('shows a determinate progress bar and percent while pulling, with cancel',
      (tester) async {
    var cancelled = false;
    const progress = PullProgress(
      status: 'pulling sha256:abc123',
      digest: 'sha256:abc123',
      total: 100,
      completed: 40,
    );

    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: const [],
      loadedModelNames: const {},
      busyModelNames: const {},
      pullingModel: 'qwen2.5-coder:3b',
      pullProgress: progress,
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (_) {},
      onCancelPull: () => cancelled = true,
    ));

    expect(find.text('Pulling qwen2.5-coder:3b'), findsOneWidget);
    expect(find.text('40%'), findsOneWidget);
    final bar = tester.widget<LinearProgressIndicator>(find.byType(LinearProgressIndicator));
    expect(bar.value, closeTo(0.4, 0.001));

    await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
    expect(cancelled, isTrue);
  });

  testWidgets('shows the pull error message when the stream fails', (tester) async {
    await _pump(tester, LocalModelsCard(
      reachable: true,
      installedModels: const [],
      loadedModelNames: const {},
      busyModelNames: const {},
      pullingModel: 'qwen2.5-coder:3b',
      pullError: 'pull model manifest: file does not exist',
      onRetry: () {},
      onInstallOllama: () {},
      onRun: (_) {},
      onStop: (_) {},
      onDelete: (_) {},
      onPull: (_) {},
      onCancelPull: () {},
    ));

    expect(find.text('pull model manifest: file does not exist'), findsOneWidget);
  });
}
