import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/data/provider_presets.dart';

void main() {
  group('kProviderPresets', () {
    test('has exactly twelve presets with unique names and non-empty badges', () {
      expect(kProviderPresets, hasLength(12));
      final names = kProviderPresets.map((p) => p.name).toList();
      expect(names.toSet(), hasLength(12), reason: 'provider names must be unique');
      for (final preset in kProviderPresets) {
        expect(preset.badge, isNotEmpty, reason: preset.name);
        expect(preset.displayName, isNotEmpty, reason: preset.name);
        expect(preset.description, isNotEmpty, reason: preset.name);
        expect(preset.baseUrl, startsWith('https://'), reason: preset.name);
        expect(preset.models, isNotEmpty, reason: preset.name);
        expect(preset.type, anyOf('openai', 'anthropic'), reason: preset.name);
        // The chip UI renders both the badge (in its avatar) and the
        // displayName (as its label) as literal Text descendants of the
        // same InputChip. If they were ever identical strings, widget
        // finders like `find.text(displayName)` would match twice for one
        // chip — regression coverage for exactly that bug (the "groq"
        // preset originally had badge: 'Groq' == displayName: 'Groq').
        expect(preset.badge, isNot(equals(preset.displayName)), reason: preset.name);
      }
      expect(names, containsAll([
        'anthropic', 'openai', 'moonshot', 'deepseek',
        'google', 'xai', 'mistral', 'groq',
        'openrouter', 'together', 'dashscope', 'zai',
      ]));
    });

    test('the google preset has two models with the specified metadata', () {
      final google = kProviderPresets.firstWhere((p) => p.name == 'google');
      expect(google.displayName, 'Gemini');
      expect(google.type, 'openai');
      expect(google.baseUrl, 'https://generativelanguage.googleapis.com/v1beta/openai');
      expect(google.models, hasLength(2));

      final pro = google.models.firstWhere((m) => m.id == 'gemini-2.5-pro');
      expect(pro.aliases, ['gemini', 'gemini-pro']);
      expect(pro.contextWindow, 1048576);
      expect(pro.capabilities, ['code', 'reasoning', 'vision', 'tools']);
      expect(pro.speed, 'medium');
      expect(pro.inputPerMTok, 1.25);
      expect(pro.outputPerMTok, 10.00);

      final flash = google.models.firstWhere((m) => m.id == 'gemini-2.5-flash');
      expect(flash.aliases, ['gemini-flash']);
      expect(flash.contextWindow, 1048576);
      expect(flash.speed, 'fast');
      expect(flash.inputPerMTok, 0.30);
      expect(flash.outputPerMTok, 2.50);
    });

    test('openrouter is priced at zero and its blurb explains why', () {
      final openrouter = kProviderPresets.firstWhere((p) => p.name == 'openrouter');
      expect(openrouter.models.single.inputPerMTok, 0);
      expect(openrouter.models.single.outputPerMTok, 0);
      expect(openrouter.description, contains('meta-gateway'));
      expect(openrouter.description, contains('cost varies by routed model'));
    });

    test('PresetModel.toYamlMap includes cost even when zero', () {
      final openrouter = kProviderPresets.firstWhere((p) => p.name == 'openrouter');
      final map = openrouter.models.single.toYamlMap();
      expect(map['cost'], {'input_per_mtok': 0, 'output_per_mtok': 0});
    });

    test('ProviderPreset.toYamlMap writes the literal api_key field', () {
      final zai = kProviderPresets.firstWhere((p) => p.name == 'zai');
      final map = zai.toYamlMap('sk-test');
      expect(map['name'], 'zai');
      expect(map['api_key'], 'sk-test');
      expect(map.containsKey('api_key_env'), isFalse);
      expect((map['models'] as List).single, isA<Map<String, dynamic>>());
    });
  });
}
