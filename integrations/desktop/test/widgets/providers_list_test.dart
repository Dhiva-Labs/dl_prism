import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/models/gateway_status.dart';
import 'package:desktop/widgets/providers_list.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: child));

void main() {
  testWidgets('shows placeholder when there are no providers', (tester) async {
    await tester.pumpWidget(_wrap(const ProvidersList(providers: [])));
    expect(find.text('No provider data yet.'), findsOneWidget);
  });

  testWidgets('lists each provider with name, local/cloud chip and model count', (tester) async {
    const providers = [
      ProviderInfo(name: 'fakelocal', type: 'openai', local: true, healthy: true, models: 1),
      ProviderInfo(name: 'fakecloud', type: 'openai', local: false, healthy: false, models: 2),
    ];
    await tester.pumpWidget(_wrap(const ProvidersList(providers: providers)));

    expect(find.text('fakelocal'), findsOneWidget);
    expect(find.text('fakecloud'), findsOneWidget);
    expect(find.text('local'), findsOneWidget);
    expect(find.text('cloud'), findsOneWidget);
    expect(find.text('1 model'), findsOneWidget);
    expect(find.text('2 models'), findsOneWidget);
  });
}
