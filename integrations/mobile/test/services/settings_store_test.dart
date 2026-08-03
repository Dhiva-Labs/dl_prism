import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SettingsStore', () {
    late _FakeSecureStorageBackend secureStorage;
    late SettingsStore store;

    setUp(() {
      SharedPreferences.setMockInitialValues({});
      secureStorage = _FakeSecureStorageBackend();
      store = SettingsStore(secureStorage: secureStorage);
    });

    test('load returns defaults when nothing was ever saved', () async {
      final settings = await store.load();
      expect(settings.baseUrl, AppSettings.defaultBaseUrl);
      expect(settings.apiKey, '');
      expect(settings.defaultModelId, AppSettings.defaultModel);
    });

    test('save then load round-trips base URL and default model via shared_preferences',
        () async {
      await store.save(const AppSettings(
        baseUrl: 'http://192.168.1.20:8130',
        apiKey: 'sk-test',
        defaultModelId: 'gpt-test',
      ));
      final loaded = await store.load();
      expect(loaded.baseUrl, 'http://192.168.1.20:8130');
      expect(loaded.defaultModelId, 'gpt-test');
    });

    test('the API key is written to secure storage, not shared_preferences', () async {
      await store.save(const AppSettings(apiKey: 'sk-super-secret'));

      expect(secureStorage.data.values, contains('sk-super-secret'));

      final prefs = await SharedPreferences.getInstance();
      final allPrefValues = prefs.getKeys().map(prefs.get).toString();
      expect(allPrefValues, isNot(contains('sk-super-secret')));
    });

    test('load reads the API key back from secure storage', () async {
      await store.save(const AppSettings(apiKey: 'sk-super-secret'));
      final loaded = await store.load();
      expect(loaded.apiKey, 'sk-super-secret');
    });

    test('saving an empty API key deletes the secure-storage entry', () async {
      await store.save(const AppSettings(apiKey: 'sk-super-secret'));
      expect(secureStorage.data, isNotEmpty);

      await store.save(const AppSettings(apiKey: ''));
      expect(secureStorage.data, isEmpty);
      final loaded = await store.load();
      expect(loaded.apiKey, '');
    });
  });

  group('AppSettings', () {
    test('copyWith overrides only the given fields', () {
      const original = AppSettings(baseUrl: 'http://a', apiKey: 'k', defaultModelId: 'm');
      final updated = original.copyWith(apiKey: 'k2');
      expect(updated.baseUrl, 'http://a');
      expect(updated.apiKey, 'k2');
      expect(updated.defaultModelId, 'm');
    });

    test('isConfigured is true whenever baseUrl is non-blank, key or not', () {
      expect(const AppSettings(baseUrl: 'http://x', apiKey: '').isConfigured, isTrue);
      expect(const AppSettings(baseUrl: '   ', apiKey: 'k').isConfigured, isFalse);
    });
  });
}
