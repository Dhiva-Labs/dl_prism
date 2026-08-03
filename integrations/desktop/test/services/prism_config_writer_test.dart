import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:yaml/yaml.dart';
import 'package:desktop/data/provider_presets.dart';
import 'package:desktop/services/prism_config_writer.dart';

const _anthropic = ProviderPreset(
  name: 'anthropic',
  displayName: 'Claude (Anthropic)',
  description: 'test',
  type: 'anthropic',
  baseUrl: 'https://api.anthropic.com',
  badge: 'Cld',
  models: [
    PresetModel(
      id: 'claude-sonnet-4-5',
      aliases: ['claude-sonnet'],
      contextWindow: 200000,
      capabilities: ['code', 'reasoning', 'vision', 'tools'],
      speed: 'fast',
      inputPerMTok: 3.00,
      outputPerMTok: 15.00,
    ),
  ],
);

void main() {
  late Directory tmpDir;

  setUp(() async {
    // A dedicated temp directory per test — this suite must never touch
    // the real ~/.config/prism/prism.yaml.
    tmpDir = await Directory.systemTemp.createTemp('prism_desktop_config_test_');
  });

  tearDown(() async {
    if (await tmpDir.exists()) {
      await tmpDir.delete(recursive: true);
    }
  });

  group('connectProvider — fresh file', () {
    test('creates parent directories and a valid skeleton plus the provider', () async {
      final path = '${tmpDir.path}/nested/dir/prism.yaml';
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final file = File(path);
      expect(await file.exists(), isTrue);

      final doc = loadYaml(await file.readAsString()) as YamlMap;
      expect(doc['server']['listen'], '127.0.0.1:8130');

      final providers = doc['providers'] as YamlList;
      final names = providers.map((p) => (p as YamlMap)['name']).toList();
      expect(names, containsAll(['ollama', 'anthropic']));

      final anthropic = providers.firstWhere((p) => (p as YamlMap)['name'] == 'anthropic') as YamlMap;
      expect(anthropic['type'], 'anthropic');
      expect(anthropic['base_url'], 'https://api.anthropic.com');
      expect(anthropic['api_key'], 'sk-ant-test');
      final models = anthropic['models'] as YamlList;
      expect((models.first as YamlMap)['id'], 'claude-sonnet-4-5');

      // The skeleton's own routing.default (qwen-coder) is already set, so
      // connecting a fresh provider must not disturb it.
      expect(doc['routing']['default'], 'qwen-coder');
    });

    test('rejects an empty API key without writing anything', () async {
      final path = '${tmpDir.path}/prism.yaml';
      final writer = PrismConfigWriter(path: path);

      await expectLater(
        writer.connectProvider(preset: _anthropic, apiKey: '   '),
        throwsA(isA<PrismConfigException>()),
      );
      expect(await File(path).exists(), isFalse);
    });
  });

  group('connectProvider — existing file', () {
    const existingYaml = '''
# DL Prism configuration — hand-edited, keep this comment.
server:
  listen: 127.0.0.1:8130
  api_keys: []

providers:
  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:14b
        aliases: [qwen-coder]
        context_window: 32768
        capabilities: [code]
        speed: fast

routing:
  rules:
    - if: {task: code}
      then: qwen-coder
''';

    test('adds the new provider while preserving unrelated content and comments',
        () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(existingYaml);
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final rawAfter = await File(path).readAsString();
      expect(rawAfter, contains('# DL Prism configuration — hand-edited, keep this comment.'));

      final doc = loadYaml(rawAfter) as YamlMap;
      final providers = doc['providers'] as YamlList;
      expect(providers, hasLength(2));

      final ollama = providers.firstWhere((p) => (p as YamlMap)['name'] == 'ollama') as YamlMap;
      expect(ollama['base_url'], 'http://127.0.0.1:11434/v1');
      expect((ollama['models'] as YamlList).first['id'], 'qwen2.5-coder:14b');

      final rules = doc['routing']['rules'] as YamlList;
      expect(rules, hasLength(1));
    });

    test('connects the real google preset, writing both of its models', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(existingYaml);
      final writer = PrismConfigWriter(path: path);

      final google = kProviderPresets.firstWhere((p) => p.name == 'google');
      await writer.connectProvider(preset: google, apiKey: 'sk-google-test');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      final providers = doc['providers'] as YamlList;
      expect(providers, hasLength(2));

      final entry = providers.firstWhere((p) => (p as YamlMap)['name'] == 'google') as YamlMap;
      expect(entry['type'], 'openai');
      expect(entry['base_url'], 'https://generativelanguage.googleapis.com/v1beta/openai');
      expect(entry['api_key'], 'sk-google-test');

      final models = entry['models'] as YamlList;
      expect(models, hasLength(2));
      final ids = models.map((m) => (m as YamlMap)['id']).toList();
      expect(ids, ['gemini-2.5-pro', 'gemini-2.5-flash']);

      final pro = models.firstWhere((m) => (m as YamlMap)['id'] == 'gemini-2.5-pro') as YamlMap;
      expect(pro['context_window'], 1048576);
      expect((pro['aliases'] as YamlList).toList(), ['gemini', 'gemini-pro']);
      expect(pro['cost']['input_per_mtok'], 1.25);
      expect(pro['cost']['output_per_mtok'], 10.00);

      // Unaffected by connecting an unrelated provider.
      final ollama = providers.firstWhere((p) => (p as YamlMap)['name'] == 'ollama') as YamlMap;
      expect(ollama['base_url'], 'http://127.0.0.1:11434/v1');
    });

    test('replaces an existing provider with the same name in place', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(existingYaml);
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-first');
      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-second');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      final providers = doc['providers'] as YamlList;
      // Still just ollama + anthropic — the second connect replaced, not
      // duplicated, the anthropic entry.
      expect(providers, hasLength(2));
      final anthropic = providers.firstWhere((p) => (p as YamlMap)['name'] == 'anthropic') as YamlMap;
      expect(anthropic['api_key'], 'sk-second');
    });

    test('backfills routing.default from the ollama provider\'s first model when missing',
        () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(existingYaml); // routing has rules but no default
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      expect(doc['routing']['default'], 'qwen2.5-coder:14b');
    });

    test('backfills routing.default from the new provider when no ollama provider exists',
        () async {
      const noOllamaYaml = '''
server:
  listen: 127.0.0.1:8130
providers:
  - name: deepseek
    type: openai
    base_url: https://api.deepseek.com/v1
    api_key: sk-existing
    models:
      - id: deepseek-chat
''';
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(noOllamaYaml);
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      expect(doc['routing']['default'], 'claude-sonnet-4-5');
    });

    test('does not touch an existing routing.default', () async {
      const withDefaultYaml = '''
server:
  listen: 127.0.0.1:8130
providers:
  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:14b
routing:
  default: some-custom-default
''';
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(withDefaultYaml);
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      expect(doc['routing']['default'], 'some-custom-default');
    });

    test('chmods the file to 0600 after writing', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(existingYaml);
      final writer = PrismConfigWriter(path: path);

      await writer.connectProvider(preset: _anthropic, apiKey: 'sk-ant-test');

      final mode = await Process.run('stat', ['-c', '%a', path]);
      expect((mode.stdout as String).trim(), '600');
    });
  });

  group('connectedProviderNames / providerExists', () {
    test('returns an empty set when the file does not exist', () async {
      final writer = PrismConfigWriter(path: '${tmpDir.path}/nope.yaml');
      expect(await writer.connectedProviderNames(), isEmpty);
      expect(await writer.providerExists('anthropic'), isFalse);
    });

    test('lists provider names from an existing file', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString('''
providers:
  - name: ollama
    type: openai
    base_url: http://x
    models: [{id: m1}]
  - name: deepseek
    type: openai
    base_url: http://y
    models: [{id: m2}]
''');
      final writer = PrismConfigWriter(path: path);
      final names = await writer.connectedProviderNames();
      expect(names, {'ollama', 'deepseek'});
      expect(await writer.providerExists('deepseek'), isTrue);
      expect(await writer.providerExists('anthropic'), isFalse);
    });
  });

  group('disconnectProvider', () {
    const yamlWithTwoProviders = '''
providers:
  - name: ollama
    type: openai
    base_url: http://127.0.0.1:11434/v1
    local: true
    models:
      - id: qwen2.5-coder:14b
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key: sk-ant-test
    models:
      - id: claude-sonnet-4-5
routing:
  default: qwen-coder
''';

    test('removes the named provider block', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(yamlWithTwoProviders);
      final writer = PrismConfigWriter(path: path);

      await writer.disconnectProvider('anthropic');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      final providers = doc['providers'] as YamlList;
      expect(providers, hasLength(1));
      expect((providers.first as YamlMap)['name'], 'ollama');
      expect(await writer.providerExists('anthropic'), isFalse);
    });

    test('is a no-op when the provider is not present', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(yamlWithTwoProviders);
      final writer = PrismConfigWriter(path: path);

      await writer.disconnectProvider('does-not-exist');

      final doc = loadYaml(await File(path).readAsString()) as YamlMap;
      expect((doc['providers'] as YamlList), hasLength(2));
    });

    test('is a no-op when the file does not exist', () async {
      final writer = PrismConfigWriter(path: '${tmpDir.path}/nope.yaml');
      await writer.disconnectProvider('anthropic'); // must not throw
      expect(await File('${tmpDir.path}/nope.yaml').exists(), isFalse);
    });

    test('chmods the file to 0600 after removing', () async {
      final path = '${tmpDir.path}/prism.yaml';
      await File(path).writeAsString(yamlWithTwoProviders);
      final writer = PrismConfigWriter(path: path);

      await writer.disconnectProvider('anthropic');

      final mode = await Process.run('stat', ['-c', '%a', path]);
      expect((mode.stdout as String).trim(), '600');
    });
  });

  group('resolvePrismConfigPath', () {
    test('returns the configured path when non-empty', () {
      expect(resolvePrismConfigPath('/custom/path.yaml'), '/custom/path.yaml');
    });

    test('trims whitespace around a configured path', () {
      expect(resolvePrismConfigPath('  /custom/path.yaml  '), '/custom/path.yaml');
    });

    test('falls back to the default path when empty', () {
      expect(resolvePrismConfigPath(''), defaultPrismConfigPath());
      expect(resolvePrismConfigPath('   '), defaultPrismConfigPath());
    });

    test('the default path is under the home directory config dir', () {
      expect(defaultPrismConfigPath(), endsWith('/.config/prism/prism.yaml'));
    });
  });
}
