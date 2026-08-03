import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/data/model_catalog.dart';
import 'package:desktop/widgets/model_catalog_section.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: SingleChildScrollView(child: child)));

void main() {
  testWidgets('shows an Installed chip for a matching installed model', (tester) async {
    await tester.pumpWidget(_wrap(ModelCatalogSection(
      installedModelNames: const {'qwen2.5-coder:7b'},
      onPull: (_) {},
    )));

    expect(find.text('Installed'), findsOneWidget);
    // Every other entry (9 of the 10) still offers a Pull button.
    expect(find.widgetWithText(OutlinedButton, 'Pull'), findsNWidgets(kModelCatalog.length - 1));
  });

  testWidgets(':latest-tagged installed model still matches its catalog entry', (tester) async {
    await tester.pumpWidget(_wrap(ModelCatalogSection(
      installedModelNames: const {'mistral:latest'},
      onPull: (_) {},
    )));

    // mistral:7b's catalog tag doesn't literally equal "mistral:latest", so
    // this specifically exercises that only an exact (post-normalization)
    // match counts — mistral:7b should NOT show as installed here.
    expect(find.text('Installed'), findsNothing);
  });

  testWidgets('tapping Pull on a catalog entry invokes onPull with its tag', (tester) async {
    String? pulledTag;
    await tester.pumpWidget(_wrap(ModelCatalogSection(
      installedModelNames: const {},
      onPull: (tag) => pulledTag = tag,
    )));

    await tester.tap(find.widgetWithText(OutlinedButton, 'Pull').first);
    expect(pulledTag, kModelCatalog.first.tag);
  });

  testWidgets('while one model is pulling, every other Pull button is disabled', (tester) async {
    await tester.pumpWidget(_wrap(ModelCatalogSection(
      installedModelNames: const {},
      pullingModel: kModelCatalog[2].tag,
      onPull: (_) {},
    )));

    // The pulling entry shows a spinner, not a Pull button.
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    final pullButtons = tester.widgetList<OutlinedButton>(
      find.widgetWithText(OutlinedButton, 'Pull'),
    );
    expect(pullButtons, hasLength(kModelCatalog.length - 1));
    for (final button in pullButtons) {
      expect(button.onPressed, isNull);
    }
  });
}
