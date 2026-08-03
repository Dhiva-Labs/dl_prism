import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/ollama_model.dart';
import '../models/pull_progress.dart';

/// Thrown when Ollama returns a non-2xx response, an unparsable body, or a
/// mid-stream `{"error": "..."}` line during a pull.
class OllamaException implements Exception {
  final String message;
  final int? statusCode;

  OllamaException(this.message, [this.statusCode]);

  @override
  String toString() =>
      statusCode == null ? 'OllamaException: $message' : 'OllamaException($statusCode): $message';
}

/// Thin HTTP client for the local Ollama API (https://ollama.com), used to
/// install/run/stop/uninstall local models. Mirrors the style of
/// [PrismApiClient] — a dumb wrapper around the REST calls, no business
/// logic.
class OllamaClient {
  final String baseUrl;
  final http.Client _client;
  final Duration timeout;

  OllamaClient({
    required this.baseUrl,
    http.Client? client,
    this.timeout = const Duration(seconds: 3),
  }) : _client = client ?? http.Client();

  String get _normalizedBaseUrl =>
      baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl;

  Uri _uri(String path) => Uri.parse('$_normalizedBaseUrl$path');

  static const _jsonHeaders = {'Content-Type': 'application/json'};

  /// True when the Ollama server answers `/api/version`. Any network
  /// failure (connection refused, timeout, ...) is treated as
  /// "not detected" rather than thrown.
  Future<bool> checkHealth() async {
    try {
      final res = await _client.get(_uri('/api/version')).timeout(timeout);
      return res.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// `GET /api/tags` — models installed on disk.
  Future<List<OllamaModel>> fetchInstalledModels() async {
    final json = await _getJson('/api/tags');
    final data = json['models'] as List<dynamic>? ?? const [];
    return data
        .map((e) => OllamaModel.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  /// `GET /api/ps` — models currently loaded into memory.
  Future<List<LoadedModel>> fetchLoadedModels() async {
    final json = await _getJson('/api/ps');
    final data = json['models'] as List<dynamic>? ?? const [];
    return data
        .map((e) => LoadedModel.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  /// `POST /api/generate` with `keep_alive: -1` and an empty prompt — asks
  /// Ollama to load the model into memory without generating anything.
  Future<void> loadModel(String model) => _setKeepAlive(model, keepAlive: -1);

  /// `POST /api/generate` with `keep_alive: 0` and an empty prompt — asks
  /// Ollama to unload the model immediately.
  Future<void> unloadModel(String model) => _setKeepAlive(model, keepAlive: 0);

  Future<void> _setKeepAlive(String model, {required int keepAlive}) async {
    final http.Response res;
    try {
      res = await _client
          .post(
            _uri('/api/generate'),
            headers: _jsonHeaders,
            body: jsonEncode({
              'model': model,
              'prompt': '',
              'keep_alive': keepAlive,
              'stream': false,
            }),
          )
          .timeout(timeout);
    } catch (e) {
      throw OllamaException('Request to /api/generate failed: $e');
    }
    if (res.statusCode < 200 || res.statusCode >= 300) {
      throw OllamaException(
        'Failed to ${keepAlive == 0 ? 'stop' : 'run'} "$model": ${res.body}',
        res.statusCode,
      );
    }
  }

  /// `DELETE /api/delete` — uninstalls a model. Callers should confirm with
  /// the user before calling this; it is irreversible without re-pulling.
  Future<void> deleteModel(String model) async {
    final http.Response res;
    try {
      res = await _client
          .delete(
            _uri('/api/delete'),
            headers: _jsonHeaders,
            body: jsonEncode({'model': model}),
          )
          .timeout(timeout);
    } catch (e) {
      throw OllamaException('Request to /api/delete failed: $e');
    }
    if (res.statusCode < 200 || res.statusCode >= 300) {
      throw OllamaException('Failed to delete "$model": ${res.body}', res.statusCode);
    }
  }

  /// `POST /api/pull` — streams NDJSON progress lines as [PullProgress]
  /// events. Cancelling the returned subscription (e.g. `sub.cancel()`)
  /// aborts the request via `package:http`'s `AbortableRequest`, which is
  /// the supported way to cancel a streamed request on this client: it
  /// injects an error into the response stream and closes it immediately,
  /// so the app stops receiving/showing progress right away. Note this is
  /// an application-level abort, not a guaranteed instant socket close —
  /// `dart:io`'s `HttpClientResponse` has no API to truly interrupt an
  /// in-flight read once headers have been received (see `IOClient.send`'s
  /// doc comment in package:http), so a few more bytes Ollama was already
  /// sending may still be read from the wire and discarded rather than
  /// this app leaving an untouched, growing connection behind.
  ///
  /// The stream emits an error (as an [OllamaException]) and then closes if
  /// the server reports a non-2xx status or a `{"error": ...}` line: it does
  /// not throw synchronously, since pulls are long-running.
  Stream<PullProgress> pullModel(String model) {
    late StreamController<PullProgress> controller;
    StreamSubscription<String>? sub;
    final abortTrigger = Completer<void>();

    Future<void> run() async {
      http.StreamedResponse response;
      try {
        final req = http.AbortableRequest(
          'POST',
          _uri('/api/pull'),
          abortTrigger: abortTrigger.future,
        )
          ..headers.addAll(_jsonHeaders)
          ..body = jsonEncode({'model': model, 'stream': true});
        response = await _client.send(req);
      } catch (e) {
        controller.addError(OllamaException('Failed to start pull for "$model": $e'));
        await controller.close();
        return;
      }

      if (response.statusCode < 200 || response.statusCode >= 300) {
        final body = await response.stream.bytesToString();
        controller.addError(
          OllamaException('Pull failed for "$model": $body', response.statusCode),
        );
        await controller.close();
        return;
      }

      sub = response.stream.transform(utf8.decoder).transform(const LineSplitter()).listen(
        (line) {
          final trimmed = line.trim();
          if (trimmed.isEmpty) return;
          Map<String, dynamic> json;
          try {
            json = jsonDecode(trimmed) as Map<String, dynamic>;
          } catch (e) {
            controller.addError(OllamaException('Could not parse pull progress: $e'));
            return;
          }
          final progress = PullProgress.fromJson(json);
          if (progress.isError) {
            controller.addError(OllamaException(progress.error!));
            return;
          }
          controller.add(progress);
        },
        onError: (Object e) => controller.addError(OllamaException('Pull stream error: $e')),
        onDone: () => controller.close(),
        cancelOnError: false,
      );
    }

    controller = StreamController<PullProgress>(
      onListen: () => unawaited(run()),
      onCancel: () async {
        if (!abortTrigger.isCompleted) abortTrigger.complete();
        await sub?.cancel();
      },
    );
    return controller.stream;
  }

  Future<Map<String, dynamic>> _getJson(String path) async {
    final http.Response res;
    try {
      res = await _client.get(_uri(path)).timeout(timeout);
    } catch (e) {
      throw OllamaException('Request to $path failed: $e');
    }
    if (res.statusCode < 200 || res.statusCode >= 300) {
      throw OllamaException('Unexpected status for $path', res.statusCode);
    }
    try {
      return jsonDecode(res.body) as Map<String, dynamic>;
    } catch (e) {
      throw OllamaException('Could not parse response from $path: $e');
    }
  }

  void dispose() => _client.close();
}
