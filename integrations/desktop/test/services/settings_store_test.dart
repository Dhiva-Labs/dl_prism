import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:desktop/services/settings_store.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SettingsStore', () {
    test('load() returns defaults when nothing has been saved', () async {
      SharedPreferences.setMockInitialValues({});
      final store = SettingsStore();

      final settings = await store.load();

      expect(settings.baseUrl, AppSettings.defaultBaseUrl);
      expect(settings.apiKey, '');
      expect(settings.binaryPath, AppSettings.defaultBinaryPath);
      expect(settings.configPath, '');
      expect(settings.ollamaBaseUrl, AppSettings.defaultOllamaBaseUrl);
    });

    test('save() then load() round-trips all fields', () async {
      SharedPreferences.setMockInitialValues({});
      final store = SettingsStore();

      const settings = AppSettings(
        baseUrl: 'http://localhost:9999',
        apiKey: 'sk-test-123',
        binaryPath: '/usr/local/bin/prism',
        configPath: '/home/user/prism.yaml',
        ollamaBaseUrl: 'http://localhost:11500',
      );
      await store.save(settings);

      final reloaded = await store.load();
      expect(reloaded.baseUrl, 'http://localhost:9999');
      expect(reloaded.apiKey, 'sk-test-123');
      expect(reloaded.binaryPath, '/usr/local/bin/prism');
      expect(reloaded.configPath, '/home/user/prism.yaml');
      expect(reloaded.ollamaBaseUrl, 'http://localhost:11500');
    });

    test('a second save() overwrites the first', () async {
      SharedPreferences.setMockInitialValues({});
      final store = SettingsStore();

      await store.save(const AppSettings(baseUrl: 'http://first:1'));
      await store.save(const AppSettings(baseUrl: 'http://second:2'));

      final reloaded = await store.load();
      expect(reloaded.baseUrl, 'http://second:2');
    });
  });

  group('AppSettings.copyWith', () {
    test('overrides only the given fields', () {
      const original = AppSettings(
        baseUrl: 'http://a',
        apiKey: 'key',
        binaryPath: 'prism',
        configPath: 'cfg.yaml',
        ollamaBaseUrl: 'http://b',
      );
      final updated = original.copyWith(apiKey: 'new-key');

      expect(updated.baseUrl, original.baseUrl);
      expect(updated.apiKey, 'new-key');
      expect(updated.binaryPath, original.binaryPath);
      expect(updated.configPath, original.configPath);
      expect(updated.ollamaBaseUrl, original.ollamaBaseUrl);
    });
  });
}
