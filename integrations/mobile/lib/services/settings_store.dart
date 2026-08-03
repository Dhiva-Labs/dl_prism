import 'package:shared_preferences/shared_preferences.dart';

import 'secure_storage_backend.dart';

/// User-configurable settings, persisted via [SettingsStore].
///
/// [apiKey] is the one field that never touches `shared_preferences`: it is
/// a bearer credential for a gateway that may be reachable over LAN/VPN, so
/// it is kept only in [SecureStorageBackend] (Android Keystore). Everything
/// else here is a plain preference, not a secret.
class AppSettings {
  static const String defaultBaseUrl = 'http://127.0.0.1:8130';
  static const String defaultModel = 'auto';

  final String baseUrl;
  final String apiKey;
  final String defaultModelId;

  const AppSettings({
    this.baseUrl = defaultBaseUrl,
    this.apiKey = '',
    this.defaultModelId = defaultModel,
  });

  AppSettings copyWith({
    String? baseUrl,
    String? apiKey,
    String? defaultModelId,
  }) {
    return AppSettings(
      baseUrl: baseUrl ?? this.baseUrl,
      apiKey: apiKey ?? this.apiKey,
      defaultModelId: defaultModelId ?? this.defaultModelId,
    );
  }

  /// Whether enough is configured to attempt talking to a gateway. A blank
  /// API key is legal (a loopback-only gateway with no `api_keys` set
  /// accepts anything, per docs/INTEGRATIONS.md), so only the base URL is
  /// required.
  bool get isConfigured => baseUrl.trim().isNotEmpty;
}

/// Persists [AppSettings]: `baseUrl`/`defaultModelId` via
/// `shared_preferences`, `apiKey` via [SecureStorageBackend].
class SettingsStore {
  SettingsStore({SecureStorageBackend? secureStorage})
      : _secureStorage = secureStorage ?? FlutterSecureStorageBackend();

  static const _keyBaseUrl = 'gateway_base_url';
  static const _keyDefaultModel = 'default_model_id';
  static const _secureKeyApiKey = 'gateway_api_key';

  final SecureStorageBackend _secureStorage;

  Future<AppSettings> load() async {
    final prefs = await SharedPreferences.getInstance();
    final apiKey = await _secureStorage.read(_secureKeyApiKey);
    return AppSettings(
      baseUrl: prefs.getString(_keyBaseUrl) ?? AppSettings.defaultBaseUrl,
      apiKey: apiKey ?? '',
      defaultModelId:
          prefs.getString(_keyDefaultModel) ?? AppSettings.defaultModel,
    );
  }

  Future<void> save(AppSettings settings) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyBaseUrl, settings.baseUrl);
    await prefs.setString(_keyDefaultModel, settings.defaultModelId);
    if (settings.apiKey.isEmpty) {
      await _secureStorage.delete(_secureKeyApiKey);
    } else {
      await _secureStorage.write(_secureKeyApiKey, settings.apiKey);
    }
  }
}
