import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/models/usage_info.dart';
import 'package:desktop/widgets/usage_card.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: child));

void main() {
  testWidgets('shows placeholder text when usage is null', (tester) async {
    await tester.pumpWidget(_wrap(const UsageCard(usage: null)));
    expect(find.text('No usage data yet.'), findsOneWidget);
  });

  testWidgets('renders spend, budget and request count', (tester) async {
    const usage = UsageInfo(
      monthTotalUsd: 3.5,
      budgetUsd: 10,
      budgetExceeded: false,
      models: [
        ModelUsage(
          provider: 'fakelocal',
          model: 'qwen',
          requests: 7,
          promptTokens: 10,
          completionTokens: 20,
          costUsd: 3.5,
        ),
      ],
      daily: [],
    );
    await tester.pumpWidget(_wrap(const UsageCard(usage: usage)));

    expect(find.text('\$3.50 / \$10.00'), findsOneWidget);
    expect(find.text('7 requests this month'), findsOneWidget);
    expect(find.text('Budget exceeded'), findsNothing);
  });

  testWidgets('shows the budget-exceeded chip when over budget', (tester) async {
    const usage = UsageInfo(
      monthTotalUsd: 12,
      budgetUsd: 10,
      budgetExceeded: true,
      models: [],
      daily: [],
    );
    await tester.pumpWidget(_wrap(const UsageCard(usage: usage)));

    expect(find.text('Budget exceeded'), findsOneWidget);
  });
}
