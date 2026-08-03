/// A curated, hand-picked list of Ollama models surfaced directly in the
/// UI so a user doesn't need to already know a tag name to get started.
///
/// This is intentionally small and opinionated, not exhaustive — the
/// free-text pull field in [LocalModelsCard] covers anything newer or more
/// obscure than what's curated here.
library;

/// One curated, pullable Ollama model.
class CatalogModel {
  final String tag;
  final String displayName;
  final String family;
  final double approxSizeGB;
  final String blurb;
  final List<String> capabilities;

  const CatalogModel({
    required this.tag,
    required this.displayName,
    required this.family,
    required this.approxSizeGB,
    required this.blurb,
    required this.capabilities,
  });

  /// True if [installedName] (as reported by `GET /api/tags`) refers to
  /// this catalog entry's model.
  bool matchesInstalled(String installedName) => sameOllamaModelRef(tag, installedName);
}

/// Ollama treats an absent/explicit `:latest` tag as equivalent to no tag
/// at all — `foo` and `foo:latest` name the same pull target. Comparing two
/// model references with this in mind avoids a curated entry silently
/// failing to show as "Installed" just because of how the tag was written.
bool sameOllamaModelRef(String a, String b) => _stripLatestTag(a) == _stripLatestTag(b);

String _stripLatestTag(String ref) {
  final trimmed = ref.trim();
  const suffix = ':latest';
  if (trimmed.toLowerCase().endsWith(suffix)) {
    return trimmed.substring(0, trimmed.length - suffix.length);
  }
  return trimmed;
}

/// The curated catalog shown above the free-text pull field.
const List<CatalogModel> kModelCatalog = [
  CatalogModel(
    tag: 'qwen2.5-coder:3b',
    displayName: 'Qwen 2.5 Coder 3B',
    family: 'Qwen',
    approxSizeGB: 1.9,
    blurb: 'Small, fast coding model — good for quick completions on modest hardware.',
    capabilities: ['code'],
  ),
  CatalogModel(
    tag: 'qwen2.5-coder:7b',
    displayName: 'Qwen 2.5 Coder 7B',
    family: 'Qwen',
    approxSizeGB: 4.7,
    blurb: 'Balanced coding model for everyday local development work.',
    capabilities: ['code'],
  ),
  CatalogModel(
    tag: 'qwen2.5-coder:14b',
    displayName: 'Qwen 2.5 Coder 14B',
    family: 'Qwen',
    approxSizeGB: 9.0,
    blurb: 'Larger coding model with stronger reasoning on harder problems.',
    capabilities: ['code', 'reasoning'],
  ),
  CatalogModel(
    tag: 'qwen3:8b',
    displayName: 'Qwen 3 8B',
    family: 'Qwen',
    approxSizeGB: 5.2,
    blurb: 'General-purpose model from the Qwen 3 generation.',
    capabilities: ['reasoning', 'general'],
  ),
  CatalogModel(
    tag: 'llama3.2:3b',
    displayName: 'Llama 3.2 3B',
    family: 'Llama',
    approxSizeGB: 2.0,
    blurb: "Meta's compact general-purpose model.",
    capabilities: ['general'],
  ),
  CatalogModel(
    tag: 'llama3.1:8b',
    displayName: 'Llama 3.1 8B',
    family: 'Llama',
    approxSizeGB: 4.9,
    blurb: "Meta's popular mid-size general-purpose model.",
    capabilities: ['general', 'reasoning'],
  ),
  CatalogModel(
    tag: 'gemma3:4b',
    displayName: 'Gemma 3 4B',
    family: 'Gemma',
    approxSizeGB: 3.3,
    blurb: "Google's compact open-weight model.",
    capabilities: ['general'],
  ),
  CatalogModel(
    tag: 'phi4:14b',
    displayName: 'Phi-4 14B',
    family: 'Phi',
    approxSizeGB: 9.1,
    blurb: "Microsoft's reasoning-focused model.",
    capabilities: ['reasoning'],
  ),
  CatalogModel(
    tag: 'deepseek-r1:8b',
    displayName: 'DeepSeek R1 8B',
    family: 'DeepSeek',
    approxSizeGB: 5.2,
    blurb: 'Distilled reasoning-focused model.',
    capabilities: ['reasoning'],
  ),
  CatalogModel(
    tag: 'mistral:7b',
    displayName: 'Mistral 7B',
    family: 'Mistral',
    approxSizeGB: 4.1,
    blurb: 'General-purpose model from Mistral AI.',
    capabilities: ['general'],
  ),
];
