import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:dlprism/app_state.dart';
import 'package:dlprism/screens/chat_screen.dart';
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

http.StreamedResponse _modelsResponse() {
  final body = utf8.encode(jsonEncode({
    'object': 'list',
    'data': [
      {'id': 'auto', 'object': 'model', 'created': 0, 'owned_by': 'prism'},
      {'id': 'gpt-test', 'object': 'model', 'created': 0, 'owned_by': 'primary'},
    ],
  }));
  return http.StreamedResponse(
    Stream.value(body),
    200,
    headers: {'content-type': 'application/json'},
  );
}

/// Builds an [AppState] whose client is a [MockClient]: `GET /v1/models`
/// always answers the same canned list; `POST /v1/chat/completions` streams
/// from whatever controller `chatStreamOf` currently points at, so each
/// test controls exactly what bytes (and when) the "gateway" sends back —
/// no real socket involved, so ordinary `pump()`/`pumpAndSettle()` suffice
/// (see the Onboarding screen's tests for why real sockets need
/// `tester.runAsync` instead, and why this is the simpler choice whenever a
/// screen already goes through `AppState.client`).
Future<AppState> _buildAppState({
  required StreamController<List<int>> Function() chatStreamOf,
  int chatStatusCode = 200,
  Map<String, String> chatHeaders = const {
    'content-type': 'text/event-stream',
    'x-prism-provider': 'primary',
    'x-prism-model': 'gpt-test',
    'x-prism-mode': 'auto',
    'x-prism-reason': 'default route',
  },
}) async {
  SharedPreferences.setMockInitialValues({});
  final store = SettingsStore(secureStorage: _FakeSecureStorageBackend());
  final settings = await store.load();

  final mockClient = MockClient.streaming((request, bodyStream) async {
    if (request.method == 'GET' && request.url.path == '/v1/models') {
      return _modelsResponse();
    }
    if (request.method == 'POST' && request.url.path == '/v1/chat/completions') {
      return http.StreamedResponse(chatStreamOf().stream, chatStatusCode, headers: chatHeaders);
    }
    return http.StreamedResponse(Stream.value(utf8.encode('not found')), 404);
  });

  final client = PrismApiClient(baseUrl: 'http://mock.invalid', client: mockClient);
  return AppState(settings: settings, store: store, client: client);
}

void addSse(StreamController<List<int>> controller, String data) {
  controller.add(utf8.encode('data: $data\n\n'));
}

/// A bounded stand-in for `pumpAndSettle()`. The chat screen keeps an
/// animated auto-scroll running after every stream event
/// (`_scrollToEnd`'s `animateTo`), and `pumpAndSettle` waits for *zero*
/// scheduled frames — a moving target every time a new event arrives —
/// which made it flake between "settles fine" and "times out" depending
/// on exactly how many stream events a test happened to trigger. A fixed
/// number of pumps flushes the same async chain deterministically
/// without depending on that.
Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 15; i++) {
    await tester.pump(const Duration(milliseconds: 20));
  }
}

void main() {
  // The chat input field's caret blinks on a repeating timer while focused,
  // which schedules a new frame forever and makes pumpAndSettle() time out
  // any time a test leaves the field focused (entering text focuses it).
  // This is Flutter's own documented escape hatch for it.
  EditableText.debugDeterministicCursor = true;

  testWidgets('sending a message streams deltas in and shows the route chips', (tester) async {
    late StreamController<List<int>> chatStream;
    final appState = await _buildAppState(chatStreamOf: () => chatStream);
    chatStream = StreamController<List<int>>();

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const ChatScreen()),
    ));
    await _settle(tester); // model list load

    await tester.enterText(find.byKey(const Key('chat_input_field')), 'hi there');
    await tester.tap(find.byKey(const Key('chat_send_button')));
    await tester.pump();

    expect(find.text('hi there'), findsOneWidget);

    addSse(chatStream, '{"choices":[{"delta":{"role":"assistant","content":""}}]}');
    await tester.pump();

    addSse(chatStream, '{"choices":[{"delta":{"content":"Hello"}}]}');
    await tester.pump();
    expect(find.textContaining('Hello'), findsOneWidget);

    addSse(chatStream, '{"choices":[{"delta":{"content":" world"}}]}');
    await tester.pump();
    expect(find.textContaining('Hello world'), findsOneWidget);

    addSse(chatStream, '[DONE]');
    await chatStream.close();
    await _settle(tester);

    // Route chips derived from the X-Prism-* headers.
    expect(find.text('primary'), findsOneWidget);
    expect(find.text('gpt-test'), findsOneWidget); // route chip only (picker still shows "auto")
    // "auto" legitimately appears twice: the model picker's current
    // selection, and the route chip row's mode chip (X-Prism-Mode: auto).
    expect(find.text('auto'), findsNWidgets(2));

    // The send button is re-enabled once streaming finishes.
    final sendButton = tester.widget<IconButton>(find.byKey(const Key('chat_send_button')));
    expect(sendButton.onPressed, isNotNull);
  });

  testWidgets('a mid-stream failure keeps partial text and shows an error strip', (tester) async {
    late StreamController<List<int>> chatStream;
    final appState = await _buildAppState(chatStreamOf: () => chatStream);
    chatStream = StreamController<List<int>>();

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const ChatScreen()),
    ));
    await _settle(tester);

    await tester.enterText(find.byKey(const Key('chat_input_field')), 'tell me a story');
    await tester.tap(find.byKey(const Key('chat_send_button')));
    await tester.pump();

    addSse(chatStream, '{"choices":[{"delta":{"content":"Once upon a"}}]}');
    await tester.pump();
    expect(find.textContaining('Once upon a'), findsOneWidget);

    // The connection dies mid-reply — no [DONE], the stream just errors out.
    chatStream.addError(Exception('connection reset'));
    await chatStream.close();
    await _settle(tester);

    // Partial text survives...
    expect(find.textContaining('Once upon a'), findsOneWidget);
    // ...alongside an error strip.
    expect(find.byKey(const Key('chat_error_strip')), findsOneWidget);
    expect(find.text('Retry'), findsOneWidget);

    // Sending is re-enabled so the user isn't stuck.
    final sendButton = tester.widget<IconButton>(find.byKey(const Key('chat_send_button')));
    expect(sendButton.onPressed, isNotNull);
  });

  testWidgets('a 401 mid-conversation shows the inline reauth prompt', (tester) async {
    late StreamController<List<int>> chatStream;
    final appState = await _buildAppState(
      chatStreamOf: () => chatStream,
      chatStatusCode: 401,
      chatHeaders: const {'content-type': 'application/json'},
    );
    chatStream = StreamController<List<int>>();

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const ChatScreen()),
    ));
    await _settle(tester);

    await tester.enterText(find.byKey(const Key('chat_input_field')), 'hi');
    await tester.tap(find.byKey(const Key('chat_send_button')));
    chatStream.add(utf8.encode(jsonEncode({
      'error': {'message': 'invalid or missing API key', 'type': 'auth_error'},
    })));
    await chatStream.close();
    await _settle(tester);

    expect(find.text('Authentication needed'), findsOneWidget);
    expect(find.byKey(const Key('chat_error_strip')), findsOneWidget);
  });

  testWidgets('clear conversation empties the message list', (tester) async {
    late StreamController<List<int>> chatStream;
    final appState = await _buildAppState(chatStreamOf: () => chatStream);
    chatStream = StreamController<List<int>>();

    await tester.pumpWidget(MaterialApp(
      home: AppStateScope(state: appState, child: const ChatScreen()),
    ));
    await _settle(tester);

    await tester.enterText(find.byKey(const Key('chat_input_field')), 'hello');
    await tester.tap(find.byKey(const Key('chat_send_button')));
    await tester.pump();
    addSse(chatStream, '{"choices":[{"delta":{"content":"hi"}}]}');
    addSse(chatStream, '[DONE]');
    await chatStream.close();
    await _settle(tester);

    expect(find.text('hello'), findsOneWidget);

    await tester.tap(find.byTooltip('Clear conversation'));
    await _settle(tester);

    expect(find.text('hello'), findsNothing);
    expect(find.textContaining('Ask anything'), findsOneWidget);
  });
}
