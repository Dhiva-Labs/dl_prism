import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/data/model_catalog.dart';

void main() {
  group('kModelCatalog', () {
    test('has the ten specified curated entries with correct tags', () {
      final tags = kModelCatalog.map((m) => m.tag).toList();
      expect(tags, [
        'qwen2.5-coder:3b',
        'qwen2.5-coder:7b',
        'qwen2.5-coder:14b',
        'qwen3:8b',
        'llama3.2:3b',
        'llama3.1:8b',
        'gemma3:4b',
        'phi4:14b',
        'deepseek-r1:8b',
        'mistral:7b',
      ]);
    });

    test('every entry has a positive approximate size and a non-empty blurb', () {
      for (final entry in kModelCatalog) {
        expect(entry.approxSizeGB, greaterThan(0), reason: entry.tag);
        expect(entry.blurb, isNotEmpty, reason: entry.tag);
        expect(entry.displayName, isNotEmpty, reason: entry.tag);
        expect(entry.family, isNotEmpty, reason: entry.tag);
        expect(entry.capabilities, isNotEmpty, reason: entry.tag);
      }
    });
  });

  group('sameOllamaModelRef', () {
    test('exact matches are equal', () {
      expect(sameOllamaModelRef('qwen2.5-coder:7b', 'qwen2.5-coder:7b'), isTrue);
    });

    test('differing tags are not equal', () {
      expect(sameOllamaModelRef('qwen2.5-coder:7b', 'qwen2.5-coder:14b'), isFalse);
    });

    test('an explicit :latest suffix is tolerated against a bare name', () {
      expect(sameOllamaModelRef('mistral', 'mistral:latest'), isTrue);
      expect(sameOllamaModelRef('mistral:latest', 'mistral'), isTrue);
    });

    test('is case-insensitive only for the :latest suffix, not the model name', () {
      expect(sameOllamaModelRef('Mistral:LATEST', 'Mistral'), isTrue);
      expect(sameOllamaModelRef('mistral', 'Mistral'), isFalse);
    });
  });

  group('CatalogModel.matchesInstalled', () {
    final qwen7b = kModelCatalog.firstWhere((m) => m.tag == 'qwen2.5-coder:7b');

    test('matches an installed model with the identical tag', () {
      expect(qwen7b.matchesInstalled('qwen2.5-coder:7b'), isTrue);
    });

    test('does not match a different tag of the same model family', () {
      expect(qwen7b.matchesInstalled('qwen2.5-coder:3b'), isFalse);
    });

    test('does not match an unrelated model', () {
      expect(qwen7b.matchesInstalled('llama3.2:3b'), isFalse);
    });
  });
}
