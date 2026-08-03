import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/models/gateway_status.dart';
import 'package:desktop/services/gateway_process_controller.dart';
import 'package:desktop/widgets/gateway_card.dart';

Widget _wrap(Widget child) => MaterialApp(home: Scaffold(body: child));

void main() {
  testWidgets('offers "Start gateway" when unreachable and not our child', (tester) async {
    final controller = GatewayProcessController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(_wrap(GatewayCard(
      reachable: false,
      status: null,
      controller: controller,
      onStart: () {},
      onStop: () {},
      onOpenDashboard: () {},
    )));

    expect(find.text('Gateway unreachable'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Start gateway'), findsOneWidget);
    expect(find.widgetWithText(OutlinedButton, 'Stop'), findsNothing);
  });

  testWidgets('shows status fields and disables Stop when reachable but externally started',
      (tester) async {
    final controller = GatewayProcessController();
    addTearDown(controller.dispose);

    const status = GatewayStatus(
      version: '0.2.0',
      uptimeSeconds: 134,
      offline: false,
      listen: '127.0.0.1:8130',
      providers: [],
    );

    await tester.pumpWidget(_wrap(GatewayCard(
      reachable: true,
      status: status,
      controller: controller,
      onStart: () {},
      onStop: () {},
      onOpenDashboard: () {},
    )));

    expect(find.text('Gateway reachable'), findsOneWidget);
    expect(find.text('0.2.0'), findsOneWidget);
    expect(find.text('127.0.0.1:8130'), findsOneWidget);
    expect(
      find.textContaining('Running externally — not started by this app'),
      findsOneWidget,
    );
    expect(find.widgetWithText(OutlinedButton, 'Stop'), findsNothing);
  });

  testWidgets('shows an enabled Stop button when we own the child process', (tester) async {
    final controller = GatewayProcessController();
    addTearDown(controller.dispose);

    // Real process I/O (Process.start/exitCode) relies on the genuine event
    // loop, not the fake-async zone testWidgets() runs its body in — it
    // must be driven via tester.runAsync() or the awaits below never
    // resolve.
    await tester.runAsync(() async {
      await controller.start(executable: '/bin/sleep', args: const ['30']);
    });

    await tester.pumpWidget(_wrap(GatewayCard(
      reachable: false,
      status: null,
      controller: controller,
      onStart: () {},
      onStop: controller.stop,
      onOpenDashboard: () {},
    )));

    expect(find.widgetWithText(OutlinedButton, 'Stop'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Start gateway'), findsNothing);

    await tester.runAsync(() async {
      controller.stop();
      await Future.delayed(const Duration(milliseconds: 300));
    });
  });

  testWidgets('the Open Dashboard button is always present and wired to its callback',
      (tester) async {
    final controller = GatewayProcessController();
    addTearDown(controller.dispose);
    var opened = false;

    // Present even while unreachable — GatewayCard doesn't gate visibility
    // on reachability; the caller (HomeScreen) decides what happens on tap
    // (e.g. "start the gateway first" when unreachable).
    await tester.pumpWidget(_wrap(GatewayCard(
      reachable: false,
      status: null,
      controller: controller,
      onStart: () {},
      onStop: () {},
      onOpenDashboard: () => opened = true,
    )));

    final dashboardButton = find.widgetWithText(FilledButton, 'Open Dashboard');
    expect(dashboardButton, findsOneWidget);

    await tester.tap(dashboardButton);
    expect(opened, isTrue);
  });
}
