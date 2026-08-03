import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/chat_message.dart';
import '../models/chat_stream_event.dart';
import '../models/gateway_status.dart';
import '../models/model_info.dart';
import '../models/route_info.dart';
import '../models/usage_info.dart';

/// Thrown when the gateway returns a non-2xx response or an unparsable body.
class ApiException implements Exception {
  final String message;
  final int? statusCode;

  ApiException(this.message, [this.statusCode]);

  /// Whether the gateway rejected the request for an invalid/missing API
  /// key — the one status the UI treats specially with an inline reauth
  /// prompt rather than a generic error banner.
  bool get isAuthError => statusCode == 401;

  @override
  String toString() =>
      statusCode == null ? 'ApiException: $message' : 'ApiException($statusCode): $message';
}

/// Thin HTTP client for the DL Prism gateway REST API.
///
/// This is intentionally the only place that knows about gateway endpoints;
/// callers work with the typed models in `lib/models`. Mirrors the desktop
/// app's `PrismApiClient` (`integrations/desktop/lib/services/api_client.dart`)
/// but adds streaming chat, since the phone talks to a gateway over
/// LAN/Tailscale/VPN rather than loopback — hence the longer default
/// timeouts here than the desktop client's.
class PrismApiClient {
  final String baseUrl;
  final String apiKey;
  final http.Client _client;

  /// Timeout for simple request/response calls (models, usage).
  final Duration timeout;

  /// Timeout for receiving the *first* byte of a chat completion response
  /// (i.e. the routing decision plus upstream connect). Once headers arrive
  /// this no longer applies — a slow model keeps streaming for as long as it
  /// takes; only a connection that never answers at all is bounded.
  final Duration streamConnectTimeout;

  PrismApiClient({
    required this.baseUrl,
    this.apiKey = '',
    http.Client? client,
    this.timeout = const Duration(seconds: 8),
    this.streamConnectTimeout = const Duration(seconds: 20),
  }) : _client = client ?? http.Client();

  String get _normalizedBaseUrl =>
      baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl;

  Map<String, String> get _headers => {
        if (apiKey.trim().isNotEmpty) 'Authorization': 'Bearer ${apiKey.trim()}',
      };

  Uri _uri(String path) => Uri.parse('$_normalizedBaseUrl$path');

  /// Returns true when `/healthz` responds with HTTP 200. Any network
  /// failure (connection refused, timeout, DNS error, ...) is treated as
  /// "unreachable" and yields false rather than throwing.
  Future<bool> checkHealth() async {
    try {
      final res = await _client.get(_uri('/healthz'), headers: _headers).timeout(timeout);
      return res.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// `GET /v1/models`. Also doubles as the Onboarding screen's "Test
  /// connection" check: a 200 here means the base URL is reachable and (if
  /// `api_keys` is configured) the key is accepted.
  Future<List<ModelInfo>> fetchModels() async {
    final json = await _getJson('/v1/models');
    final data = json['data'] as List<dynamic>? ?? const [];
    return data
        .map((e) => ModelInfo.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<UsageInfo> fetchUsage() async {
    final json = await _getJson('/admin/usage');
    return UsageInfo.fromJson(json);
  }

  Future<GatewayStatus> fetchStatus() async {
    final json = await _getJson('/admin/status');
    return GatewayStatus.fromJson(json);
  }

  Future<Map<String, dynamic>> _getJson(String path) async {
    final http.Response res;
    try {
      res = await _client.get(_uri(path), headers: _headers).timeout(timeout);
    } catch (e) {
      throw ApiException('Could not reach the gateway: $e');
    }
    if (res.statusCode == 401) {
      throw ApiException(_authErrorMessage(res.body), 401);
    }
    if (res.statusCode < 200 || res.statusCode >= 300) {
      throw ApiException(_errorMessage(res.body) ?? 'Unexpected status for $path', res.statusCode);
    }
    try {
      return jsonDecode(res.body) as Map<String, dynamic>;
    } catch (e) {
      throw ApiException('Could not parse response from $path: $e');
    }
  }

  /// Streams `POST /v1/chat/completions` with `stream: true` and turns the
  /// SSE body into typed [ChatStreamEvent]s.
  ///
  /// Tolerant of malformed lines: anything that is not a well-formed
  /// `data: {...}` chunk (blank keep-alive lines, stray `event:`/`id:`
  /// fields, or a line that fails to parse as JSON) is skipped rather than
  /// aborting the stream, since the gateway pipes upstream SSE bytes through
  /// largely unchanged (see `internal/provider/openai.go`'s `pipeSSE`) and a
  /// single odd line from an upstream should not blow up the whole reply.
  Stream<ChatStreamEvent> streamChatCompletion({
    required String model,
    required List<ChatMessage> messages,
  }) async* {
    final request = http.Request('POST', _uri('/v1/chat/completions'));
    request.headers['Content-Type'] = 'application/json';
    request.headers.addAll(_headers);
    request.body = jsonEncode({
      'model': model,
      'stream': true,
      'messages': messages
          .map((m) => {
                'role': m.role == ChatRole.user ? 'user' : 'assistant',
                'content': m.content,
              })
          .toList(),
    });

    final http.StreamedResponse response;
    try {
      response = await _client.send(request).timeout(streamConnectTimeout);
    } catch (e) {
      yield ChatErrorEvent('Could not reach the gateway: $e');
      return;
    }

    if (response.statusCode == 401) {
      final body = await response.stream.bytesToString();
      yield ChatErrorEvent(_authErrorMessage(body), statusCode: 401);
      return;
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      final body = await response.stream.bytesToString();
      yield ChatErrorEvent(
        _errorMessage(body) ?? 'Gateway returned HTTP ${response.statusCode}',
        statusCode: response.statusCode,
      );
      return;
    }

    yield ChatRouteEvent(RouteInfo.fromHeaders(response.headers));

    final lines = response.stream.transform(utf8.decoder).transform(const LineSplitter());
    try {
      await for (final line in lines) {
        final data = _sseData(line);
        if (data == null) continue;
        if (data == '[DONE]') {
          yield const ChatDoneEvent();
          return;
        }
        final content = _deltaContent(data);
        if (content != null && content.isNotEmpty) {
          yield ChatDeltaEvent(content);
        }
      }
      // Upstream closed the connection without an explicit [DONE]. Treat as
      // a normal end rather than leaving the UI stuck showing "streaming".
      yield const ChatDoneEvent();
    } catch (e) {
      yield ChatErrorEvent('Connection lost while streaming: $e');
    }
  }

  /// Extracts the payload of an SSE `data:` line, or null if [line] is not
  /// one (blank lines, other SSE fields like `event:`/`id:`/`:comment`).
  static String? _sseData(String line) {
    if (!line.startsWith('data:')) return null;
    var data = line.substring(5);
    if (data.startsWith(' ')) data = data.substring(1);
    return data;
  }

  /// Pulls `choices[0].delta.content` out of one chat-completion-chunk JSON
  /// payload. Returns null for anything that does not parse as an object
  /// with that shape (malformed-line tolerance) or that carries no text
  /// (role-only / finish-reason-only chunks).
  static String? _deltaContent(String data) {
    try {
      final chunk = jsonDecode(data);
      if (chunk is! Map<String, dynamic>) return null;
      final choices = chunk['choices'];
      if (choices is! List || choices.isEmpty) return null;
      final first = choices.first;
      if (first is! Map<String, dynamic>) return null;
      final delta = first['delta'];
      if (delta is! Map<String, dynamic>) return null;
      final content = delta['content'];
      return content is String ? content : null;
    } catch (_) {
      return null;
    }
  }

  /// The gateway's error envelope is `{"error":{"message":...,"type":...}}`
  /// (see `writeErr` in `internal/server/server.go`). Falls back to null
  /// (caller supplies a generic message) if [body] does not match.
  static String? _errorMessage(String body) {
    try {
      final decoded = jsonDecode(body);
      if (decoded is Map<String, dynamic>) {
        final error = decoded['error'];
        if (error is Map<String, dynamic>) {
          final message = error['message'];
          if (message is String && message.isNotEmpty) return message;
        }
      }
    } catch (_) {
      // fall through
    }
    return null;
  }

  static String _authErrorMessage(String body) =>
      _errorMessage(body) ?? 'Invalid or missing API key.';

  void dispose() => _client.close();
}
