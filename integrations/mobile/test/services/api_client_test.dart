import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:dlprism/models/chat_message.dart';
import 'package:dlprism/models/chat_stream_event.dart';
import 'package:dlprism/services/api_client.dart';

/// A tiny fake gateway server so the API client is exercised against real
/// sockets rather than mocked HTTP internals — mirrors the desktop app's
/// `test/services/api_client_test.dart`.
///
/// [PrismApiClient] always appends a fixed suffix to `baseUrl` (`/v1/models`,
/// `/v1/chat/completions`, ...), so different scenarios for the same
/// endpoint (success vs. 401 vs. a mid-stream drop) are reached by pointing
/// a client's `baseUrl` at `<gateway>/<scenario>` and matching on the full
/// path here — the same trick the desktop test suite doesn't need only
/// because it has fewer scenarios per endpoint.
class _FakeGateway {
  late HttpServer _server;
  final List<Map<String, String>> requestHeaders = [];
  final List<String> requestedPaths = [];

  String get baseUrl => 'http://127.0.0.1:${_server.port}';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen(_handle);
  }

  Future<void> _handle(HttpRequest req) async {
    final path = req.uri.path;
    requestedPaths.add(path);
    final headers = <String, String>{};
    req.headers.forEach((name, values) => headers[name] = values.join(','));
    requestHeaders.add(headers);

    if (path.endsWith('/v1/models')) {
      if (path.startsWith('/authfail')) return _writeUnauthorized(req);
      return _writeModels(req);
    }
    if (path.endsWith('/admin/usage')) return _writeUsage(req);
    if (path.endsWith('/admin/status')) return _writeStatus(req);
    if (path.endsWith('/v1/chat/completions')) {
      if (path.startsWith('/authfail')) return _writeUnauthorized(req);
      if (path.startsWith('/midstream')) return _writeMidstreamDrop(req);
      return _writeChatStream(req);
    }
    req.response.statusCode = 404;
    await req.response.close();
  }

  Future<void> _writeModels(HttpRequest req) async {
    req.response.statusCode = 200;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'object': 'list',
      'data': [
        {'id': 'auto', 'object': 'model', 'created': 0, 'owned_by': 'prism'},
        {
          'id': 'qwen2.5-coder:14b',
          'object': 'model',
          'created': 0,
          'owned_by': 'fakelocal',
          'prism': {
            'aliases': ['qwen-coder'],
            'local': true,
            'context_window': 32768,
            'capabilities': ['code', 'tools'],
            'speed': 'fast',
            'cost_per_mtok': {'input': 0.0, 'output': 0.0},
          },
        },
      ],
    }));
    await req.response.close();
  }

  Future<void> _writeUsage(HttpRequest req) async {
    req.response.statusCode = 200;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'month_total_usd': 1.23,
      'budget_usd': 10.0,
      'budget_exceeded': false,
      'models': [
        {
          'provider': 'fakelocal',
          'model': 'qwen2.5-coder:14b',
          'requests': 16,
          'prompt_tokens': 192,
          'completion_tokens': 322,
          'cost_usd': 0.0,
        },
      ],
    }));
    await req.response.close();
  }

  Future<void> _writeStatus(HttpRequest req) async {
    req.response.statusCode = 200;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'version': '0.2.0',
      'uptime_seconds': 134,
      'offline': false,
      'listen': '127.0.0.1:8130',
      'providers': [
        {'name': 'fakelocal', 'type': 'openai', 'local': true, 'healthy': true, 'models': 1},
      ],
    }));
    await req.response.close();
  }

  Future<void> _writeUnauthorized(HttpRequest req) async {
    req.response.statusCode = 401;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'error': {'message': 'invalid or missing API key', 'type': 'auth_error'},
    }));
    await req.response.close();
  }

  Future<void> _writeChatStream(HttpRequest req) async {
    req.response.headers.set('Content-Type', 'text/event-stream');
    req.response.headers.set('X-Prism-Provider', 'primary');
    req.response.headers.set('X-Prism-Model', 'gpt-test');
    req.response.headers.set('X-Prism-Mode', 'auto');
    req.response.headers.set('X-Prism-Reason', 'default route');

    // Deliberately mixes in lines an SSE parser must tolerate without
    // aborting the whole reply: a comment, a bare event field, a blank
    // keep-alive line, unparsable JSON, and a chunk with no choices.
    const lines = [
      ': keep-alive',
      'event: message',
      'data: {"choices":[{"delta":{"role":"assistant","content":""}}]}',
      '',
      'data: {"choices":[{"delta":{"content":"Hel"}}]}',
      'data: not-json-at-all',
      'data: {"choices":[]}',
      'data: {"choices":[{"delta":{"content":"lo"}}]}',
      'data: {malformed',
      'data: {"choices":[{"delta":{},"finish_reason":"stop"}]}',
      'data: [DONE]',
    ];
    for (final line in lines) {
      req.response.write('$line\n');
      await req.response.flush();
    }
    await req.response.close();
  }

  /// Simulates a connection that dies mid-reply: detaches the raw socket
  /// *before* any header is written (dart:io's `detachSocket` refuses once
  /// headers are sent — `StateError: Headers already sent`), hand-writes a
  /// chunked HTTP/1.1 response with one complete chunk, then destroys the
  /// socket without ever sending the terminating zero-length chunk. A
  /// chunked-transfer client reading this sees the connection close before
  /// the body logically ended, which is exactly the "connection lost
  /// mid-stream" case `streamChatCompletion` must recover from.
  Future<void> _writeMidstreamDrop(HttpRequest req) async {
    final socket = await req.response.detachSocket();
    socket.write(
      'HTTP/1.1 200 OK\r\n'
      'Content-Type: text/event-stream\r\n'
      'X-Prism-Provider: primary\r\n'
      'X-Prism-Model: gpt-test\r\n'
      'X-Prism-Mode: auto\r\n'
      'Transfer-Encoding: chunked\r\n'
      'Connection: close\r\n'
      '\r\n',
    );
    final chunkBytes = utf8.encode('data: {"choices":[{"delta":{"content":"Hel"}}]}\n\n');
    socket.write('${chunkBytes.length.toRadixString(16)}\r\n');
    socket.add(chunkBytes);
    socket.write('\r\n');
    await socket.flush();
    await socket.close();
  }

  Future<void> stop() => _server.close(force: true);
}

void main() {
  group('PrismApiClient against a fake HTTP server', () {
    late _FakeGateway gateway;

    setUp(() async {
      gateway = _FakeGateway();
      await gateway.start();
    });

    tearDown(() => gateway.stop());

    test('fetchModels parses full prism metadata', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final models = await client.fetchModels();
      expect(models, hasLength(2));
      expect(models[0].id, 'auto');
      expect(models[0].isAuto, isTrue);
      final entry = models[1];
      expect(entry.id, 'qwen2.5-coder:14b');
      expect(entry.local, isTrue);
      expect(entry.aliases, ['qwen-coder']);
      expect(entry.contextWindow, 32768);
      expect(entry.capabilities, ['code', 'tools']);
      expect(entry.speed, 'fast');
      client.dispose();
    });

    test('fetchUsage parses /admin/usage into a UsageInfo', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final usage = await client.fetchUsage();
      expect(usage.monthTotalUsd, 1.23);
      expect(usage.budgetUsd, 10.0);
      expect(usage.budgetExceeded, isFalse);
      expect(usage.totalRequests, 16);
      expect(usage.budgetFraction, closeTo(0.123, 0.0001));
      client.dispose();
    });

    test('fetchStatus parses /admin/status into a GatewayStatus', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final status = await client.fetchStatus();
      expect(status.version, '0.2.0');
      expect(status.uptimeSeconds, 134);
      expect(status.providers, hasLength(1));
      expect(status.providers.first.local, isTrue);
      client.dispose();
    });

    test('sends Authorization: Bearer header when an API key is set', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl, apiKey: 'secret-key');
      await client.fetchModels();
      expect(gateway.requestHeaders.last['authorization'], 'Bearer secret-key');
      client.dispose();
    });

    test('omits Authorization header when no API key is set', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      await client.fetchModels();
      expect(gateway.requestHeaders.last.containsKey('authorization'), isFalse);
      client.dispose();
    });

    test('401 on a GET surfaces as ApiException(401) with the gateway message', () async {
      final client = PrismApiClient(baseUrl: '${gateway.baseUrl}/authfail');
      try {
        await client.fetchModels();
        fail('expected an ApiException');
      } on ApiException catch (e) {
        expect(e.statusCode, 401);
        expect(e.isAuthError, isTrue);
        expect(e.message, contains('invalid or missing API key'));
      }
      client.dispose();
    });

    test('streamChatCompletion emits route info, tolerates malformed lines, and completes',
        () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final events = await client
          .streamChatCompletion(
            model: 'auto',
            messages: [ChatMessage.user('hi')],
          )
          .toList();

      expect(events.whereType<ChatRouteEvent>(), hasLength(1));
      final route = events.whereType<ChatRouteEvent>().first.route;
      expect(route.provider, 'primary');
      expect(route.model, 'gpt-test');
      expect(route.mode, 'auto');
      expect(route.reason, 'default route');

      final deltas = events.whereType<ChatDeltaEvent>().map((e) => e.text).join();
      expect(deltas, 'Hello');

      expect(events.whereType<ChatDoneEvent>(), hasLength(1));
      expect(events.whereType<ChatErrorEvent>(), isEmpty);
      client.dispose();
    });

    test('streamChatCompletion surfaces a 401 as an auth ChatErrorEvent', () async {
      final client = PrismApiClient(baseUrl: '${gateway.baseUrl}/authfail');
      final events = await client
          .streamChatCompletion(model: 'auto', messages: [ChatMessage.user('hi')])
          .toList();

      expect(events, hasLength(1));
      final error = events.single as ChatErrorEvent;
      expect(error.statusCode, 401);
      expect(error.isAuthError, isTrue);
      client.dispose();
    });

    test('streamChatCompletion keeps partial text and reports an error on a mid-stream drop',
        () async {
      final client = PrismApiClient(baseUrl: '${gateway.baseUrl}/midstream');
      final events = await client
          .streamChatCompletion(model: 'auto', messages: [ChatMessage.user('hi')])
          .toList();

      // Route info always arrives (it comes off the response headers,
      // received before any body byte). Whether the one already-complete
      // SSE chunk written before the drop shows up as a ChatDeltaEvent
      // depends on how much dart:io's own chunked-transfer decoder had
      // buffered versus flushed to the application stream at the instant
      // the socket was destroyed — not something this test controls or
      // should assert on. What must hold regardless: nothing already
      // yielded is retracted, the stream never throws past this method's
      // own try/catch, and it ends in exactly one error event rather than
      // a normal completion.
      expect(events.whereType<ChatRouteEvent>(), hasLength(1));
      final deltas = events.whereType<ChatDeltaEvent>().map((e) => e.text).join();
      expect('Hel', startsWith(deltas)); // deltas is '' or a prefix of 'Hel'
      expect(events.whereType<ChatErrorEvent>(), hasLength(1));
      expect(events.whereType<ChatDoneEvent>(), isEmpty);
      expect(events.last, isA<ChatErrorEvent>());
      client.dispose();
    });
  });

  group('PrismApiClient against an unreachable server', () {
    test('fetchModels throws ApiException rather than hanging', () async {
      final gateway = _FakeGateway();
      await gateway.start();
      final url = gateway.baseUrl;
      await gateway.stop();

      final client = PrismApiClient(
        baseUrl: url,
        timeout: const Duration(milliseconds: 500),
      );
      expect(() => client.fetchModels(), throwsA(isA<ApiException>()));
      client.dispose();
    });

    test('streamChatCompletion yields a single ChatErrorEvent rather than throwing', () async {
      final gateway = _FakeGateway();
      await gateway.start();
      final url = gateway.baseUrl;
      await gateway.stop();

      final client = PrismApiClient(
        baseUrl: url,
        streamConnectTimeout: const Duration(milliseconds: 500),
      );
      final events = await client
          .streamChatCompletion(model: 'auto', messages: [ChatMessage.user('hi')])
          .toList();
      expect(events, hasLength(1));
      expect(events.single, isA<ChatErrorEvent>());
      client.dispose();
    });
  });
}
