import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/services/api_client.dart';

/// A tiny fake gateway server so the API client can be exercised against
/// real sockets rather than mocked HTTP internals.
class _FakeGateway {
  late HttpServer _server;
  final List<Map<String, String>> requestHeaders = [];
  final List<String> requestedPaths = [];

  String get baseUrl => 'http://127.0.0.1:${_server.port}';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen((req) async {
      requestedPaths.add(req.uri.path);
      final headers = <String, String>{};
      req.headers.forEach((name, values) => headers[name] = values.join(','));
      requestHeaders.add(headers);
      switch (req.uri.path) {
        case '/healthz':
          req.response.statusCode = 200;
          req.response.write('ok');
          break;
        case '/admin/status':
          req.response.statusCode = 200;
          req.response.headers.contentType = ContentType.json;
          req.response.write(jsonEncode({
            'version': '0.2.0',
            'uptime_seconds': 134,
            'offline': false,
            'listen': '127.0.0.1:8130',
            'providers': [
              {
                'name': 'fakelocal',
                'type': 'openai',
                'local': true,
                'healthy': true,
                'models': 1,
              },
              {
                'name': 'fakecloud',
                'type': 'openai',
                'local': false,
                'healthy': true,
                'models': 1,
              },
            ],
          }));
          break;
        case '/admin/usage':
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
            'daily': [
              {'date': '2026-08-03', 'cost_usd': 0.0, 'requests': 16},
            ],
          }));
          break;
        case '/v1/models':
          req.response.statusCode = 200;
          req.response.headers.contentType = ContentType.json;
          req.response.write(jsonEncode({
            'object': 'list',
            'data': [
              {
                'id': 'auto',
                'object': 'model',
                'created': 0,
                'owned_by': 'prism',
              },
              {
                'id': 'qwen2.5-coder:14b',
                'object': 'model',
                'created': 0,
                'owned_by': 'fakelocal',
                'prism': {'local': true},
              },
            ],
          }));
          break;
        case '/broken-json':
          req.response.statusCode = 200;
          req.response.write('not json');
          break;
        case '/server-error':
          req.response.statusCode = 500;
          req.response.write('boom');
          break;
        default:
          req.response.statusCode = 404;
      }
      await req.response.close();
    });
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

    test('checkHealth returns true for a healthy /healthz', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      expect(await client.checkHealth(), isTrue);
      client.dispose();
    });

    test('fetchStatus parses /admin/status into a GatewayStatus', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final status = await client.fetchStatus();
      expect(status.version, '0.2.0');
      expect(status.uptimeSeconds, 134);
      expect(status.offline, isFalse);
      expect(status.listen, '127.0.0.1:8130');
      expect(status.providers, hasLength(2));
      expect(status.providers.first.name, 'fakelocal');
      expect(status.providers.first.local, isTrue);
      expect(status.providers.first.healthy, isTrue);
      expect(status.providers.first.models, 1);
      client.dispose();
    });

    test('fetchUsage parses /admin/usage into a UsageInfo', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final usage = await client.fetchUsage();
      expect(usage.monthTotalUsd, 1.23);
      expect(usage.budgetUsd, 10.0);
      expect(usage.budgetExceeded, isFalse);
      expect(usage.models, hasLength(1));
      expect(usage.totalRequests, 16);
      expect(usage.budgetFraction, closeTo(0.123, 0.0001));
      client.dispose();
    });

    test('fetchModels parses /v1/models into ModelInfo list', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      final models = await client.fetchModels();
      expect(models, hasLength(2));
      expect(models[0].id, 'auto');
      expect(models[1].local, isTrue);
      client.dispose();
    });

    test('sends Authorization: Bearer header when an API key is set', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl, apiKey: 'secret-key');
      await client.checkHealth();
      expect(gateway.requestHeaders.last['authorization'], 'Bearer secret-key');
      client.dispose();
    });

    test('omits Authorization header when no API key is set', () async {
      final client = PrismApiClient(baseUrl: gateway.baseUrl);
      await client.checkHealth();
      expect(gateway.requestHeaders.last.containsKey('authorization'), isFalse);
      client.dispose();
    });

  });

  group('PrismApiClient against an unreachable server', () {
    test('checkHealth returns false rather than throwing', () async {
      final gateway = _FakeGateway();
      await gateway.start();
      final url = gateway.baseUrl;
      await gateway.stop();

      final client = PrismApiClient(
        baseUrl: url,
        timeout: const Duration(milliseconds: 500),
      );
      expect(await client.checkHealth(), isFalse);
      client.dispose();
    });

    test('fetchStatus throws ApiException when unreachable', () async {
      final gateway = _FakeGateway();
      await gateway.start();
      final url = gateway.baseUrl;
      await gateway.stop();

      final client = PrismApiClient(
        baseUrl: url,
        timeout: const Duration(milliseconds: 500),
      );
      expect(() => client.fetchStatus(), throwsA(isA<ApiException>()));
      client.dispose();
    });
  });
}
