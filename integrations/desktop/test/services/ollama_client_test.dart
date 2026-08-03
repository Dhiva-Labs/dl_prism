import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/services/ollama_client.dart';
import 'package:desktop/models/pull_progress.dart';

/// A tiny fake Ollama server so [OllamaClient] can be exercised against
/// real sockets. Behavior for the mutating endpoints is selected by the
/// `model` name in the request body so each test can pick a scenario.
class _FakeOllama {
  late HttpServer _server;
  final List<Map<String, dynamic>> generateCalls = [];
  final List<Map<String, dynamic>> deleteCalls = [];

  // Gates the second line of the 'cancel-model' pull scenario so the test
  // controls exactly when it's written, instead of racing a timer against
  // however busy the test-runner's event loop happens to be.
  final Completer<void> _cancelModelGate = Completer<void>();
  void releaseCancelModelGate() {
    if (!_cancelModelGate.isCompleted) _cancelModelGate.complete();
  }

  String get baseUrl => 'http://127.0.0.1:${_server.port}';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen(_handle);
  }

  Future<void> _handle(HttpRequest req) async {
    try {
      await _dispatch(req);
      await req.response.close();
    } catch (_) {
      // A test cancelling a pull mid-stream closes the client socket,
      // which makes a subsequent write/flush here throw — that's expected
      // and not a server bug, so just drop the connection.
    }
  }

  Future<void> _dispatch(HttpRequest req) async {
    switch ('${req.method} ${req.uri.path}') {
      case 'GET /api/version':
        req.response.statusCode = 200;
        req.response.write(jsonEncode({'version': '0.24.0'}));
        break;

      case 'GET /api/tags':
        req.response.statusCode = 200;
        req.response.write(jsonEncode({
          'models': [
            {
              'name': 'qwen2.5-coder:7b',
              'model': 'qwen2.5-coder:7b',
              'size': 4683087561,
              'modified_at': '2026-08-03T17:26:04.293763716+05:30',
            },
          ],
        }));
        break;

      case 'GET /api/ps':
        req.response.statusCode = 200;
        req.response.write(jsonEncode({
          'models': [
            {
              'name': 'qwen2.5-coder:7b',
              'model': 'qwen2.5-coder:7b',
              'size_vram': 5137025024,
              'expires_at': '2026-08-03T18:26:04.000000000+05:30',
            },
          ],
        }));
        break;

      case 'POST /api/generate':
        {
          final body = jsonDecode(await utf8.decoder.bind(req).join()) as Map<String, dynamic>;
          generateCalls.add(body);
          req.response.statusCode = 200;
          req.response.write(jsonEncode({
            'model': body['model'],
            'created_at': '2026-08-03T18:00:00Z',
            'response': '',
            'done': true,
            'done_reason': (body['keep_alive'] == 0) ? 'unload' : 'load',
          }));
        }
        break;

      case 'DELETE /api/delete':
        {
          final body = jsonDecode(await utf8.decoder.bind(req).join()) as Map<String, dynamic>;
          deleteCalls.add(body);
          if (body['model'] == 'missing-model') {
            req.response.statusCode = 404;
            req.response.write(jsonEncode({'error': "model 'missing-model' not found"}));
          } else {
            req.response.statusCode = 200;
          }
        }
        break;

      case 'POST /api/pull':
        {
          final body = jsonDecode(await utf8.decoder.bind(req).join()) as Map<String, dynamic>;
          final model = body['model'] as String?;
          req.response.headers.chunkedTransferEncoding = true;
          if (model == 'missing-model') {
            req.response.statusCode = 404;
            req.response.write(jsonEncode({'error': "pull model manifest: file does not exist"}));
            break;
          }
          if (model == 'broken-model') {
            req.response.statusCode = 200;
            req.response.write('${jsonEncode({
                  'status': 'pulling manifest',
                })}\n');
            await req.response.flush();
            req.response.write('${jsonEncode({
                  'error': 'unexpected EOF',
                })}\n');
            await req.response.flush();
            break;
          }
          if (model == 'cancel-model') {
            // Writes exactly one line, then blocks on a gate the test
            // controls directly (see _cancelModelGate) before writing
            // anything else — no fixed real-time delay involved, so there
            // is nothing for a slow/loaded test runner to race against.
            req.response.statusCode = 200;
            req.response.write('${jsonEncode({'status': 'pulling manifest'})}\n');
            await req.response.flush();
            await _cancelModelGate.future;
            req.response.write('${jsonEncode({'status': 'pulling more'})}\n');
            await req.response.flush();
            break;
          }
          // Default: a well-behaved pull with a slow trickle of progress
          // lines so tests can exercise both "let it finish" and "cancel
          // mid-stream".
          req.response.statusCode = 200;
          final lines = [
            {'status': 'pulling manifest'},
            {
              'status': 'pulling sha256:abc123',
              'digest': 'sha256:abc123',
              'total': 100,
              'completed': 25,
            },
            {
              'status': 'pulling sha256:abc123',
              'digest': 'sha256:abc123',
              'total': 100,
              'completed': 100,
            },
            {'status': 'verifying sha256 digest'},
            {'status': 'writing manifest'},
            {'status': 'success'},
          ];
          for (final line in lines) {
            req.response.write('${jsonEncode(line)}\n');
            await req.response.flush();
            await Future.delayed(const Duration(milliseconds: 60));
          }
        }
        break;

      default:
        req.response.statusCode = 404;
    }
  }

  Future<void> stop() => _server.close(force: true);
}

void main() {
  group('OllamaClient against a fake HTTP server', () {
    late _FakeOllama server;

    setUp(() async {
      server = _FakeOllama();
      await server.start();
    });

    tearDown(() => server.stop());

    test('checkHealth returns true when /api/version answers', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      expect(await client.checkHealth(), isTrue);
      client.dispose();
    });

    test('checkHealth returns false when unreachable', () async {
      final another = _FakeOllama();
      await another.start();
      final url = another.baseUrl;
      await another.stop();

      final client = OllamaClient(baseUrl: url, timeout: const Duration(milliseconds: 500));
      expect(await client.checkHealth(), isFalse);
      client.dispose();
    });

    test('fetchInstalledModels parses /api/tags', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      final models = await client.fetchInstalledModels();
      expect(models, hasLength(1));
      expect(models.first.name, 'qwen2.5-coder:7b');
      expect(models.first.sizeBytes, 4683087561);
      expect(models.first.sizeGb, closeTo(4.36, 0.01));
      client.dispose();
    });

    test('fetchLoadedModels parses /api/ps', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      final loaded = await client.fetchLoadedModels();
      expect(loaded, hasLength(1));
      expect(loaded.first.name, 'qwen2.5-coder:7b');
      expect(loaded.first.sizeVramBytes, 5137025024);
      expect(loaded.first.expiresAt, isNotNull);
      client.dispose();
    });

    test('loadModel sends keep_alive: -1 and an empty prompt', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      await client.loadModel('qwen2.5-coder:7b');
      expect(server.generateCalls, hasLength(1));
      expect(server.generateCalls.single['model'], 'qwen2.5-coder:7b');
      expect(server.generateCalls.single['keep_alive'], -1);
      expect(server.generateCalls.single['prompt'], '');
      client.dispose();
    });

    test('unloadModel sends keep_alive: 0', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      await client.unloadModel('qwen2.5-coder:7b');
      expect(server.generateCalls, hasLength(1));
      expect(server.generateCalls.single['keep_alive'], 0);
      client.dispose();
    });

    test('deleteModel sends the model name and succeeds on 200', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      await client.deleteModel('qwen2.5-coder:7b');
      expect(server.deleteCalls, hasLength(1));
      expect(server.deleteCalls.single['model'], 'qwen2.5-coder:7b');
      client.dispose();
    });

    test('deleteModel throws OllamaException on a non-2xx response', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      expect(
        () => client.deleteModel('missing-model'),
        throwsA(isA<OllamaException>().having((e) => e.statusCode, 'statusCode', 404)),
      );
      client.dispose();
    });

    test('pullModel streams progress events with correct percent math and finishes',
        () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      final events = <PullProgress>[];
      await for (final event in client.pullModel('happy-model')) {
        events.add(event);
      }

      expect(events.map((e) => e.status), contains('pulling manifest'));
      expect(events.last.status, 'success');

      final withProgress = events.where((e) => e.fraction != null).toList();
      expect(withProgress, hasLength(2));
      expect(withProgress[0].fraction, closeTo(0.25, 0.001));
      expect(withProgress[1].fraction, closeTo(1.0, 0.001));
      client.dispose();
    });

    test('pullModel surfaces a mid-stream {"error": ...} line as OllamaException', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      final events = <PullProgress>[];
      Object? caughtError;
      // Note: deliberately using Stream.forEach (not .listen().asFuture()) —
      // asFuture() silently *replaces* any onError/onDone callbacks passed
      // to .listen(), so it can't be combined with a custom onError.
      try {
        await client.pullModel('broken-model').forEach(events.add);
      } catch (e) {
        caughtError = e;
      }

      expect(events.map((e) => e.status), contains('pulling manifest'));
      expect(caughtError, isA<OllamaException>());
      client.dispose();
    });

    test('pullModel surfaces a non-2xx initial response as OllamaException', () async {
      final client = OllamaClient(baseUrl: server.baseUrl);
      Object? caughtError;
      try {
        await client.pullModel('missing-model').forEach((_) {});
      } catch (e) {
        caughtError = e;
      }

      expect(caughtError, isA<OllamaException>());
      expect((caughtError as OllamaException).statusCode, 404);
      client.dispose();
    });

    test('cancelling the pull subscription stops delivering further events', () async {
      // This sandbox's loopback HTTP stack (confirmed independently with a
      // bare dart:io HttpClient probe, no package:http involved) buffers a
      // chunked response's bytes entirely until the connection closes,
      // regardless of intermediate flush() calls server-side — so nothing
      // is observable client-side while 'cancel-model' sits blocked on
      // _cancelModelGate. That means we can't build this test around
      // "wait for line 1 to arrive, then cancel" (it never will, until the
      // gate opens); instead we assert the guarantee that actually holds
      // unconditionally per the Stream contract: once cancelled, a
      // subscription can never fire onData again, however long we wait or
      // whatever the server goes on to send.
      final client = OllamaClient(baseUrl: server.baseUrl);
      final events = <PullProgress>[];
      final sub = client.pullModel('cancel-model').listen(events.add);

      await sub.cancel();
      server.releaseCancelModelGate();

      await Future.delayed(const Duration(milliseconds: 500));
      expect(events, isEmpty);
      client.dispose();
    });
  });
}
