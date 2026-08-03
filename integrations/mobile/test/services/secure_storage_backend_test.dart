import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:dlprism/services/secure_storage_backend.dart';

/// The plugin's method channel — see
/// `flutter_secure_storage_platform_interface`'s
/// `MethodChannelFlutterSecureStorage`. Mocking it lets
/// [FlutterSecureStorageBackend] itself run under plain `flutter test`
/// (no real Android Keystore involved) instead of only exercising a fake
/// that stands in for it.
const _channel = MethodChannel('plugins.it_nomads.com/flutter_secure_storage');

/// In-memory [SecureStorageBackend] stand-in, the same role
/// `FakeSecureStorageBackend` plays in secure_shell_go's credential-store
/// tests: everything that needs the platform Keystore talks to the
/// interface, not `FlutterSecureStorage` directly, so tests never need a
/// platform channel.
class _FakeSecureStorageBackend implements SecureStorageBackend {
  final Map<String, String> data = {};

  @override
  Future<void> write(String key, String value) async => data[key] = value;

  @override
  Future<String?> read(String key) async => data[key];

  @override
  Future<void> delete(String key) async => data.remove(key);
}

/// Stands in for a Keystore that refuses every write (e.g. no device
/// credential set on some emulator images).
class _UnavailableSecureStorageBackend implements SecureStorageBackend {
  @override
  Future<void> write(String key, String value) async {
    throw const SecureStorageUnavailableException('unavailable', code: 'KeystoreLocked');
  }

  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> delete(String key) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('_FakeSecureStorageBackend (sanity check of the test double itself)', () {
    test('round-trips a value', () async {
      final backend = _FakeSecureStorageBackend();
      await backend.write('api_key', 'sk-test-123');
      expect(await backend.read('api_key'), 'sk-test-123');
    });

    test('read of a never-written key is null', () async {
      final backend = _FakeSecureStorageBackend();
      expect(await backend.read('missing'), isNull);
    });

    test('delete removes the entry', () async {
      final backend = _FakeSecureStorageBackend();
      await backend.write('api_key', 'sk-test-123');
      await backend.delete('api_key');
      expect(await backend.read('api_key'), isNull);
    });

    test('an unavailable backend throws on write', () async {
      final backend = _UnavailableSecureStorageBackend();
      await expectLater(
        backend.write('api_key', 'sk-test-123'),
        throwsA(isA<SecureStorageUnavailableException>()),
      );
    });
  });

  group('SecureStorageUnavailableException', () {
    test('carries the platform code and a message', () {
      const e = SecureStorageUnavailableException('nope', code: 'KeystoreLocked');
      expect(e.message, 'nope');
      expect(e.code, 'KeystoreLocked');
      expect(e.toString(), 'nope');
    });
  });

  group('FlutterSecureStorageBackend against a mocked platform channel', () {
    late Map<String, String> store;

    setUp(() {
      store = {};
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, (call) async {
        switch (call.method) {
          case 'write':
            store[call.arguments['key'] as String] = call.arguments['value'] as String;
            return null;
          case 'read':
            return store[call.arguments['key'] as String];
          case 'delete':
            store.remove(call.arguments['key'] as String);
            return null;
          default:
            throw UnimplementedError(call.method);
        }
      });
    });

    tearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, null);
    });

    test('write then read round-trips through the (mocked) platform channel', () async {
      final backend = FlutterSecureStorageBackend();
      await backend.write('gateway_api_key', 'sk-live-abc');
      expect(await backend.read('gateway_api_key'), 'sk-live-abc');
    });

    test('delete removes the entry', () async {
      final backend = FlutterSecureStorageBackend();
      await backend.write('gateway_api_key', 'sk-live-abc');
      await backend.delete('gateway_api_key');
      expect(await backend.read('gateway_api_key'), isNull);
    });

    test('read of a missing key is null, not an error', () async {
      final backend = FlutterSecureStorageBackend();
      expect(await backend.read('never_written'), isNull);
    });

    test('write wraps a PlatformException into SecureStorageUnavailableException', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'write') {
          throw PlatformException(code: 'KeystoreLocked', message: 'no device credential set');
        }
        return null;
      });
      final backend = FlutterSecureStorageBackend();
      await expectLater(
        backend.write('gateway_api_key', 'sk-live-abc'),
        throwsA(isA<SecureStorageUnavailableException>()
            .having((e) => e.code, 'code', 'KeystoreLocked')),
      );
    });

    test('read fails closed to null on a PlatformException rather than throwing', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'read') {
          throw PlatformException(code: 'KeystoreLocked', message: 'locked');
        }
        return null;
      });
      final backend = FlutterSecureStorageBackend();
      expect(await backend.read('gateway_api_key'), isNull);
    });

    test('delete is a best-effort no-op on a PlatformException rather than throwing', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'delete') {
          throw PlatformException(code: 'KeystoreLocked', message: 'locked');
        }
        return null;
      });
      final backend = FlutterSecureStorageBackend();
      await backend.delete('gateway_api_key'); // must not throw
    });
  });
}
