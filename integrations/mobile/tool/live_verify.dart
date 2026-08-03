// Live-stack verification for PrismApiClient against a real running
// DL Prism gateway (not a fake HttpServer, not a MockClient) — models list,
// streaming chat (X-Prism-* headers + SSE deltas), and usage/budget.
//
// This is a throwaway developer probe, not part of `flutter test` (kept out
// of test/ so it is never auto-discovered), the same role `tool/` plays in
// the sibling secure_shell_go project. Run it with:
//
//   dart run tool/live_verify.dart <base_url> <api_key>
//
// against a gateway started separately (see the mobile companion's build
// report for the exact prism.yaml + fake-upstream setup used to produce
// it). Exits non-zero on any check failure.
import 'dart:io';

import 'package:dlprism/models/chat_message.dart';
import 'package:dlprism/models/chat_stream_event.dart';
import 'package:dlprism/services/api_client.dart';

int _failures = 0;

void check(String label, bool condition) {
  if (condition) {
    stdout.writeln('  [ok] $label');
  } else {
    stdout.writeln('  [FAIL] $label');
    _failures++;
  }
}

Future<void> main(List<String> args) async {
  final baseUrl = args.isNotEmpty ? args[0] : 'http://127.0.0.1:8131';
  final apiKey = args.length > 1 ? args[1] : '';

  final client = PrismApiClient(baseUrl: baseUrl, apiKey: apiKey);

  stdout.writeln('== GET /v1/models ==');
  final models = await client.fetchModels();
  check('at least 2 models (auto + registry)', models.length >= 2);
  check('first model is auto', models.first.id == 'auto' && models.first.isAuto);
  final registryModel = models.firstWhere((m) => !m.isAuto);
  check('registry model has aliases', registryModel.aliases.isNotEmpty);
  check('registry model has context_window', registryModel.contextWindow != null);
  check('registry model has capabilities', registryModel.capabilities.isNotEmpty);
  check('registry model has speed', registryModel.speed != null);
  check('registry model has cost', registryModel.cost != null);
  check('registry model marked non-local (openai adapter)', registryModel.local == false);
  stdout.writeln('  models: ${models.map((m) => m.id).join(', ')}');

  stdout.writeln('== POST /v1/chat/completions (stream) ==');
  final events = <ChatStreamEvent>[];
  final buffer = StringBuffer();
  await for (final event in client.streamChatCompletion(
    model: registryModel.id,
    messages: [ChatMessage.user('Say hi from the live mobile verification probe.')],
  )) {
    events.add(event);
    if (event is ChatDeltaEvent) buffer.write(event.text);
  }
  final route = events.whereType<ChatRouteEvent>().isEmpty
      ? null
      : events.whereType<ChatRouteEvent>().first.route;
  check('route event received', route != null);
  check('X-Prism-Provider present', route?.provider != null && route!.provider!.isNotEmpty);
  check('X-Prism-Model present', route?.model != null);
  check('X-Prism-Mode present', route?.mode != null);
  check('at least one delta received', events.whereType<ChatDeltaEvent>().isNotEmpty);
  check('stream ended with ChatDoneEvent', events.whereType<ChatDoneEvent>().isNotEmpty);
  check('no ChatErrorEvent', events.whereType<ChatErrorEvent>().isEmpty);
  stdout.writeln('  route: provider=${route?.provider} model=${route?.model} mode=${route?.mode}');
  stdout.writeln('  reply: "${buffer.toString()}"');

  stdout.writeln('== GET /admin/usage ==');
  // Give the ledger a moment to record the request the chat call above just
  // made before reading it back.
  await Future<void>.delayed(const Duration(milliseconds: 300));
  final usage = await client.fetchUsage();
  check('budget_usd matches configured monthly budget (10.0)', usage.budgetUsd == 10.0);
  check('month_total_usd reflects the chat request just made', usage.monthTotalUsd > 0);
  check('at least one model in usage.models', usage.models.isNotEmpty);
  check('budgetFraction is between 0 and 1', usage.budgetFraction >= 0 && usage.budgetFraction <= 1);
  stdout.writeln('  month_total_usd=${usage.monthTotalUsd} budget_usd=${usage.budgetUsd} '
      'requests=${usage.totalRequests}');

  stdout.writeln('== GET /v1/models with a wrong key (401 surfacing) ==');
  final wrongKeyClient = PrismApiClient(baseUrl: baseUrl, apiKey: 'definitely-wrong-key');
  try {
    await wrongKeyClient.fetchModels();
    check('wrong key rejected with 401', false);
  } on ApiException catch (e) {
    check('wrong key rejected with 401', e.statusCode == 401 && e.isAuthError);
  }
  wrongKeyClient.dispose();

  client.dispose();

  stdout.writeln('');
  if (_failures == 0) {
    stdout.writeln('ALL CHECKS PASSED');
    exit(0);
  } else {
    stdout.writeln('$_failures CHECK(S) FAILED');
    exit(1);
  }
}
