import 'dart:io';

import 'package:yaml/yaml.dart';
import 'package:yaml_edit/yaml_edit.dart';

import '../data/provider_presets.dart';

/// Thrown when a config file can't be read, parsed, or written.
class PrismConfigException implements Exception {
  final String message;
  PrismConfigException(this.message);

  @override
  String toString() => 'PrismConfigException: $message';
}

/// The gateway config's default location when no custom path is set.
String defaultPrismConfigPath() {
  final home = Platform.environment['HOME'] ?? '';
  return '$home/.config/prism/prism.yaml';
}

/// Resolves the effective config path: [configured] if non-empty, else the
/// default `~/.config/prism/prism.yaml`.
String resolvePrismConfigPath(String configured) {
  final trimmed = configured.trim();
  return trimmed.isEmpty ? defaultPrismConfigPath() : trimmed;
}

/// A minimal, valid config written when no file exists yet at all: a local
/// Ollama provider (matching the one this app manages) plus a routing
/// default that resolves to it, so the gateway is immediately usable.
const String _skeletonYaml = '''
server:
  listen: 127.0.0.1:8130
  api_keys: []

providers:
  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:7b
        aliases: [qwen-coder]
        context_window: 32768
        capabilities: [code]
        speed: fast

routing:
  default: qwen-coder
''';

/// Reads/writes the `prism.yaml` gateway config, preserving unrelated
/// content, formatting, and comments via `package:yaml_edit`.
///
/// This is the only place in the app that touches the config file; the
/// gateway itself remains the source of truth once restarted.
class PrismConfigWriter {
  final String path;

  const PrismConfigWriter({required this.path});

  /// Provider names currently present in the config file's `providers:`
  /// list. Returns an empty set if the file doesn't exist or can't be
  /// parsed (rather than throwing), since this is used for read-only
  /// status display.
  Future<Set<String>> connectedProviderNames() async {
    final file = File(path);
    if (!await file.exists()) return {};
    String content;
    try {
      content = await file.readAsString();
    } catch (_) {
      return {};
    }
    try {
      final doc = loadYaml(content);
      final providers = doc is YamlMap ? doc['providers'] : null;
      if (providers is! YamlList) return {};
      return providers
          .whereType<YamlMap>()
          .map((p) => p['name'])
          .whereType<String>()
          .where((n) => n.isNotEmpty)
          .toSet();
    } catch (_) {
      return {};
    }
  }

  Future<bool> providerExists(String name) async =>
      (await connectedProviderNames()).contains(name);

  /// Adds (or replaces, if a provider with the same name already exists)
  /// [preset]'s block, using [apiKey] as its literal `api_key`. Creates the
  /// file (and any missing parent directories) from a minimal skeleton if
  /// it doesn't exist yet. Backfills `routing.default` if it's missing,
  /// preferring the first model of an existing `ollama` provider, else the
  /// first model of [preset] itself. Chmods the file to 0600 afterward.
  Future<void> connectProvider({
    required ProviderPreset preset,
    required String apiKey,
  }) async {
    final trimmedKey = apiKey.trim();
    if (trimmedKey.isEmpty) {
      throw PrismConfigException('API key must not be empty.');
    }

    final file = File(path);
    String content;
    if (await file.exists()) {
      try {
        content = await file.readAsString();
      } catch (e) {
        throw PrismConfigException('Could not read $path: $e');
      }
    } else {
      try {
        await file.parent.create(recursive: true);
      } catch (e) {
        throw PrismConfigException('Could not create ${file.parent.path}: $e');
      }
      content = _skeletonYaml;
    }

    final YamlEditor editor;
    try {
      editor = YamlEditor(content);
    } catch (e) {
      throw PrismConfigException('Could not parse $path as YAML: $e');
    }

    final providerMap = preset.toYamlMap(trimmedKey);
    final existingIndex = _findProviderIndex(editor, preset.name);
    if (existingIndex != null) {
      editor.update(['providers', existingIndex], providerMap);
    } else {
      final providersNode = editor.parseAt(['providers'], orElse: () => wrapAsYamlNode(null));
      if (providersNode.value == null) {
        editor.update(['providers'], [providerMap]);
      } else {
        editor.appendToList(['providers'], providerMap);
      }
    }

    _backfillRoutingDefault(editor, fallbackModelId: preset.models.first.id);

    await _write(file, editor);
  }

  /// Removes the named provider's block, if present. A no-op if the file
  /// or the named provider doesn't exist.
  Future<void> disconnectProvider(String name) async {
    final file = File(path);
    if (!await file.exists()) return;

    final String content;
    try {
      content = await file.readAsString();
    } catch (e) {
      throw PrismConfigException('Could not read $path: $e');
    }

    final YamlEditor editor;
    try {
      editor = YamlEditor(content);
    } catch (e) {
      throw PrismConfigException('Could not parse $path as YAML: $e');
    }

    final idx = _findProviderIndex(editor, name);
    if (idx == null) return;
    editor.remove(['providers', idx]);

    await _write(file, editor);
  }

  Future<void> _write(File file, YamlEditor editor) async {
    try {
      await file.writeAsString(editor.toString());
    } catch (e) {
      throw PrismConfigException('Could not write $path: $e');
    }
    await _chmod600(file);
  }

  int? _findProviderIndex(YamlEditor editor, String name) {
    final node = editor.parseAt(['providers'], orElse: () => wrapAsYamlNode(null));
    if (node is! YamlList) return null;
    for (var i = 0; i < node.length; i++) {
      final entry = node.nodes[i];
      if (entry is YamlMap && entry['name'] == name) return i;
    }
    return null;
  }

  void _backfillRoutingDefault(YamlEditor editor, {required String fallbackModelId}) {
    final routingNode = editor.parseAt(['routing'], orElse: () => wrapAsYamlNode(null));
    if (routingNode is YamlMap) {
      final current = routingNode['default'];
      if (current is String && current.trim().isNotEmpty) return; // already set
    }

    final ollamaFirstModel = _firstModelIdForProvider(editor, 'ollama');
    final fallback = ollamaFirstModel ?? fallbackModelId;

    if (routingNode.value == null) {
      editor.update(['routing'], {'default': fallback});
    } else {
      editor.update(['routing', 'default'], fallback);
    }
  }

  String? _firstModelIdForProvider(YamlEditor editor, String providerName) {
    final idx = _findProviderIndex(editor, providerName);
    if (idx == null) return null;
    final provider = editor.parseAt(['providers', idx]);
    if (provider is! YamlMap) return null;
    final models = provider['models'];
    if (models is YamlList && models.isNotEmpty) {
      final first = models.nodes.first;
      if (first is YamlMap) {
        final id = first['id'];
        if (id is String && id.isNotEmpty) return id;
      }
    }
    return null;
  }

  /// Best-effort; a sandbox/platform without `chmod` simply leaves the file
  /// at its default permissions rather than failing the whole write.
  Future<void> _chmod600(File file) async {
    try {
      await Process.run('chmod', ['600', file.path]);
    } catch (_) {
      // No-op — see doc comment.
    }
  }
}
