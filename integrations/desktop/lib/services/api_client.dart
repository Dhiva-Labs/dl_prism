import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/gateway_status.dart';
import '../models/model_info.dart';
import '../models/usage_info.dart';

/// Thrown when the gateway returns a non-2xx response or an unparsable body.
class ApiException implements Exception {
  final String message;
  final int? statusCode;

  ApiException(this.message, [this.statusCode]);

  @override
  String toString() =>
      statusCode == null ? 'ApiException: $message' : 'ApiException($statusCode): $message';
}

/// Thin HTTP client for the DL Prism gateway REST API.
///
/// This is intentionally the only place that knows about gateway endpoints;
/// callers work with the typed models in `lib/models`.
class PrismApiClient {
  final String baseUrl;
  final String apiKey;
  final http.Client _client;
  final Duration timeout;

  PrismApiClient({
    required this.baseUrl,
    this.apiKey = '',
    http.Client? client,
    this.timeout = const Duration(seconds: 3),
  }) : _client = client ?? http.Client();

  String get _normalizedBaseUrl =>
      baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl;

  Map<String, String> get _headers => {
        if (apiKey.trim().isNotEmpty) 'Authorization': 'Bearer ${apiKey.trim()}',
      };

  Uri _uri(String path) => Uri.parse('$_normalizedBaseUrl$path');

  /// Returns true when `/healthz` responds with HTTP 200 and body `ok`.
  /// Any network failure (connection refused, timeout, DNS error, ...)
  /// is treated as "unreachable" and yields false rather than throwing.
  Future<bool> checkHealth() async {
    try {
      final res =
          await _client.get(_uri('/healthz'), headers: _headers).timeout(timeout);
      return res.statusCode == 200 && res.body.trim() == 'ok';
    } catch (_) {
      return false;
    }
  }

  Future<GatewayStatus> fetchStatus() async {
    final json = await _getJson('/admin/status');
    return GatewayStatus.fromJson(json);
  }

  Future<UsageInfo> fetchUsage() async {
    final json = await _getJson('/admin/usage');
    return UsageInfo.fromJson(json);
  }

  Future<List<ModelInfo>> fetchModels() async {
    final json = await _getJson('/v1/models');
    final data = json['data'] as List<dynamic>? ?? const [];
    return data
        .map((e) => ModelInfo.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<Map<String, dynamic>> _getJson(String path) async {
    final http.Response res;
    try {
      res = await _client.get(_uri(path), headers: _headers).timeout(timeout);
    } catch (e) {
      throw ApiException('Request to $path failed: $e');
    }
    if (res.statusCode < 200 || res.statusCode >= 300) {
      throw ApiException('Unexpected status for $path', res.statusCode);
    }
    try {
      return jsonDecode(res.body) as Map<String, dynamic>;
    } catch (e) {
      throw ApiException('Could not parse response from $path: $e');
    }
  }

  void dispose() => _client.close();
}
