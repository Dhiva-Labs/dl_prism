/// Preset cloud-provider connection blocks offered by the "Providers" card.
///
/// Field names/values are kept consistent with the repo's
/// `prism.example.yaml` (see `internal/config/config.go` for the Go struct
/// these ultimately decode into — it uses strict/known-fields YAML
/// decoding, so every key written here must match exactly).
///
/// Model IDs, aliases, context windows, and per-token pricing below are a
/// **point-in-time snapshot as of 2026-08**. Providers change their
/// catalogs and pricing over time — this file is plain data and is meant
/// to be edited freely as that happens; nothing outside this file needs to
/// change to add, remove, or update a preset. The tiles UI
/// (`widgets/providers_card.dart`) simply renders whatever is in
/// [kProviderPresets].
library;

/// One model entry within a [ProviderPreset]'s `models:` list.
class PresetModel {
  final String id;
  final List<String> aliases;
  final int contextWindow;
  final List<String> capabilities;
  final String speed;
  final double inputPerMTok;
  final double outputPerMTok;

  const PresetModel({
    required this.id,
    this.aliases = const [],
    required this.contextWindow,
    required this.capabilities,
    required this.speed,
    required this.inputPerMTok,
    required this.outputPerMTok,
  });

  /// A plain-Dart representation suitable for `YamlEditor` insertion.
  Map<String, dynamic> toYamlMap() => {
        'id': id,
        if (aliases.isNotEmpty) 'aliases': aliases,
        'context_window': contextWindow,
        'capabilities': capabilities,
        'speed': speed,
        'cost': {'input_per_mtok': inputPerMTok, 'output_per_mtok': outputPerMTok},
      };
}

/// One connectable cloud provider preset.
class ProviderPreset {
  final String name;
  final String displayName;
  final String description;
  final String type;
  final String baseUrl;
  final List<PresetModel> models;

  /// A short (2-4 character) text badge standing in for a logo, e.g. "GPT"
  /// or "Grok" — deliberately text, not an image asset, per the "no
  /// external images" constraint on the compact tile UI.
  final String badge;

  const ProviderPreset({
    required this.name,
    required this.displayName,
    required this.description,
    required this.type,
    required this.baseUrl,
    required this.models,
    required this.badge,
  });

  /// A plain-Dart representation of the full `providers:` list entry,
  /// suitable for `YamlEditor` insertion. The key is written literally as
  /// `api_key` (not `api_key_env`) per the config schema.
  Map<String, dynamic> toYamlMap(String apiKey) => {
        'name': name,
        'type': type,
        'base_url': baseUrl,
        'api_key': apiKey,
        'models': models.map((m) => m.toYamlMap()).toList(),
      };
}

const List<ProviderPreset> kProviderPresets = [
  ProviderPreset(
    name: 'anthropic',
    displayName: 'Claude (Anthropic)',
    description: 'Connect Anthropic\'s API to route requests to Claude models.',
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
  ),
  ProviderPreset(
    name: 'openai',
    displayName: 'OpenAI / Codex',
    description: 'Connect OpenAI\'s API to route requests to GPT-5 models.',
    type: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    badge: 'GPT',
    models: [
      PresetModel(
        id: 'gpt-5',
        contextWindow: 400000,
        capabilities: ['code', 'reasoning', 'vision', 'tools'],
        speed: 'medium',
        inputPerMTok: 1.25,
        outputPerMTok: 10.00,
      ),
      PresetModel(
        id: 'gpt-5-mini',
        contextWindow: 400000,
        capabilities: ['code', 'reasoning', 'vision', 'tools'],
        speed: 'fast',
        inputPerMTok: 0.25,
        outputPerMTok: 2.00,
      ),
    ],
  ),
  ProviderPreset(
    name: 'moonshot',
    displayName: 'Kimi (Moonshot AI)',
    description: 'Connect Moonshot AI\'s API to route requests to Kimi models.',
    type: 'openai',
    baseUrl: 'https://api.moonshot.ai/v1',
    badge: 'Kimi',
    models: [
      PresetModel(
        id: 'kimi-k2-0905-preview',
        aliases: ['kimi', 'kimi-k2'],
        contextWindow: 262144,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'medium',
        inputPerMTok: 0.60,
        outputPerMTok: 2.50,
      ),
    ],
  ),
  ProviderPreset(
    name: 'deepseek',
    displayName: 'DeepSeek',
    description: 'Connect DeepSeek\'s API to route requests to DeepSeek models.',
    type: 'openai',
    baseUrl: 'https://api.deepseek.com/v1',
    badge: 'DS',
    models: [
      PresetModel(
        id: 'deepseek-chat',
        aliases: ['deepseek'],
        contextWindow: 128000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'medium',
        inputPerMTok: 0.27,
        outputPerMTok: 1.10,
      ),
    ],
  ),
  ProviderPreset(
    name: 'google',
    displayName: 'Gemini',
    description: 'Connect Google\'s API to route requests to Gemini models.',
    type: 'openai',
    baseUrl: 'https://generativelanguage.googleapis.com/v1beta/openai',
    badge: 'Gem',
    models: [
      PresetModel(
        id: 'gemini-2.5-pro',
        aliases: ['gemini', 'gemini-pro'],
        contextWindow: 1048576,
        capabilities: ['code', 'reasoning', 'vision', 'tools'],
        speed: 'medium',
        inputPerMTok: 1.25,
        outputPerMTok: 10.00,
      ),
      PresetModel(
        id: 'gemini-2.5-flash',
        aliases: ['gemini-flash'],
        contextWindow: 1048576,
        capabilities: ['code', 'reasoning', 'vision', 'tools'],
        speed: 'fast',
        inputPerMTok: 0.30,
        outputPerMTok: 2.50,
      ),
    ],
  ),
  ProviderPreset(
    name: 'xai',
    displayName: 'Grok (xAI)',
    description: 'Connect xAI\'s API to route requests to Grok models.',
    type: 'openai',
    baseUrl: 'https://api.x.ai/v1',
    badge: 'Grok',
    models: [
      PresetModel(
        id: 'grok-4',
        aliases: ['grok'],
        contextWindow: 256000,
        capabilities: ['code', 'reasoning', 'vision', 'tools'],
        speed: 'medium',
        inputPerMTok: 3.00,
        outputPerMTok: 15.00,
      ),
    ],
  ),
  ProviderPreset(
    name: 'mistral',
    displayName: 'Mistral',
    description: 'Connect Mistral AI\'s API to route requests to Mistral models.',
    type: 'openai',
    baseUrl: 'https://api.mistral.ai/v1',
    badge: 'Mis',
    models: [
      PresetModel(
        id: 'mistral-large-latest',
        aliases: ['mistral-large'],
        contextWindow: 128000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'medium',
        inputPerMTok: 2.00,
        outputPerMTok: 6.00,
      ),
    ],
  ),
  ProviderPreset(
    name: 'groq',
    displayName: 'Groq',
    description: 'Connect Groq\'s API to route requests to fast Llama inference.',
    type: 'openai',
    baseUrl: 'https://api.groq.com/openai/v1',
    badge: 'GRQ',
    models: [
      PresetModel(
        id: 'llama-3.3-70b-versatile',
        aliases: ['groq-llama'],
        contextWindow: 128000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'fast',
        inputPerMTok: 0.59,
        outputPerMTok: 0.79,
      ),
    ],
  ),
  ProviderPreset(
    name: 'openrouter',
    displayName: 'OpenRouter',
    description:
        'Connect OpenRouter, a meta-gateway; cost varies by routed model — the '
        '0.00 shown below is just this catalog entry\'s placeholder.',
    type: 'openai',
    baseUrl: 'https://openrouter.ai/api/v1',
    badge: 'OR',
    models: [
      PresetModel(
        id: 'openrouter/auto',
        aliases: ['openrouter'],
        contextWindow: 200000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'medium',
        inputPerMTok: 0,
        outputPerMTok: 0,
      ),
    ],
  ),
  ProviderPreset(
    name: 'together',
    displayName: 'Together AI',
    description: 'Connect Together AI\'s API to route requests to open-weight models.',
    type: 'openai',
    baseUrl: 'https://api.together.xyz/v1',
    badge: 'Tog',
    models: [
      PresetModel(
        id: 'meta-llama/Llama-3.3-70B-Instruct-Turbo',
        aliases: ['together-llama'],
        contextWindow: 128000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'fast',
        inputPerMTok: 0.88,
        outputPerMTok: 0.88,
      ),
    ],
  ),
  ProviderPreset(
    name: 'dashscope',
    displayName: 'Qwen (Alibaba)',
    description: 'Connect Alibaba\'s DashScope API to route requests to Qwen models.',
    type: 'openai',
    baseUrl: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
    badge: 'Qwen',
    models: [
      PresetModel(
        id: 'qwen3-coder-plus',
        aliases: ['qwen-cloud'],
        contextWindow: 1000000,
        capabilities: ['code', 'tools'],
        speed: 'medium',
        inputPerMTok: 1.00,
        outputPerMTok: 5.00,
      ),
    ],
  ),
  ProviderPreset(
    name: 'zai',
    displayName: 'GLM (Z.ai)',
    description: 'Connect Z.ai\'s API to route requests to GLM models.',
    type: 'openai',
    baseUrl: 'https://api.z.ai/api/paas/v4',
    badge: 'GLM',
    models: [
      PresetModel(
        id: 'glm-4.6',
        aliases: ['glm'],
        contextWindow: 200000,
        capabilities: ['code', 'reasoning', 'tools'],
        speed: 'medium',
        inputPerMTok: 0.60,
        outputPerMTok: 2.20,
      ),
    ],
  ),
];
