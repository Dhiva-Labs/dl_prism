import 'package:shared_preferences/shared_preferences.dart';

/// User-configurable settings, persisted via [SettingsStore].
class AppSettings {
  static const String defaultBaseUrl = 'http://127.0.0.1:8130';
  static const String defaultBinaryPath = 'prism';
  static const String defaultOllamaBaseUrl = 'http://127.0.0.1:11434';

  final String baseUrl;
  final String apiKey;
  final String binaryPath;
  final String configPath;
  final String ollamaBaseUrl;

  /// Where the Providers card writes preset connections. Empty means "use
  /// the default `~/.config/prism/prism.yaml`" — see
  /// `resolvePrismConfigPath` in `prism_config_writer.dart`. Distinct from
  /// [configPath] (the `-c` flag passed to `prism serve`), since a user may
  /// point the running gateway at one file while still wanting the app to
  /// default to editing the standard location, or vice versa.
  final String prismConfigPath;

  const AppSettings({
    this.baseUrl = defaultBaseUrl,
    this.apiKey = '',
    this.binaryPath = defaultBinaryPath,
    this.configPath = '',
    this.ollamaBaseUrl = defaultOllamaBaseUrl,
    this.prismConfigPath = '',
  });

  AppSettings copyWith({
    String? baseUrl,
    String? apiKey,
    String? binaryPath,
    String? configPath,
    String? ollamaBaseUrl,
    String? prismConfigPath,
  }) {
    return AppSettings(
      baseUrl: baseUrl ?? this.baseUrl,
      apiKey: apiKey ?? this.apiKey,
      binaryPath: binaryPath ?? this.binaryPath,
      configPath: configPath ?? this.configPath,
      ollamaBaseUrl: ollamaBaseUrl ?? this.ollamaBaseUrl,
      prismConfigPath: prismConfigPath ?? this.prismConfigPath,
    );
  }
}

/// Persists [AppSettings] to disk via `shared_preferences`.
class SettingsStore {
  static const _keyBaseUrl = 'gateway_base_url';
  static const _keyApiKey = 'gateway_api_key';
  static const _keyBinaryPath = 'prism_binary_path';
  static const _keyConfigPath = 'prism_config_path';
  static const _keyOllamaBaseUrl = 'ollama_base_url';
  static const _keyPrismConfigPath = 'prism_config_yaml_path';

  Future<AppSettings> load() async {
    final prefs = await SharedPreferences.getInstance();
    return AppSettings(
      baseUrl: prefs.getString(_keyBaseUrl) ?? AppSettings.defaultBaseUrl,
      apiKey: prefs.getString(_keyApiKey) ?? '',
      binaryPath: prefs.getString(_keyBinaryPath) ?? AppSettings.defaultBinaryPath,
      configPath: prefs.getString(_keyConfigPath) ?? '',
      ollamaBaseUrl: prefs.getString(_keyOllamaBaseUrl) ?? AppSettings.defaultOllamaBaseUrl,
      prismConfigPath: prefs.getString(_keyPrismConfigPath) ?? '',
    );
  }

  Future<void> save(AppSettings settings) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyBaseUrl, settings.baseUrl);
    await prefs.setString(_keyApiKey, settings.apiKey);
    await prefs.setString(_keyBinaryPath, settings.binaryPath);
    await prefs.setString(_keyConfigPath, settings.configPath);
    await prefs.setString(_keyOllamaBaseUrl, settings.ollamaBaseUrl);
    await prefs.setString(_keyPrismConfigPath, settings.prismConfigPath);
  }
}
