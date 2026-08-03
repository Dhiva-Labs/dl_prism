/// Data model for a single entry of `GET /v1/models`.
///
/// The gateway always returns a synthetic `auto` entry first (routed by
/// policy, no `prism` metadata beyond a description) followed by one entry
/// per registry model, each carrying a `prism` object with the routing
/// metadata the Models screen renders as chips.
library;

class ModelCost {
  final double inputPerMTok;
  final double outputPerMTok;

  const ModelCost({this.inputPerMTok = 0, this.outputPerMTok = 0});

  factory ModelCost.fromJson(Map<String, dynamic>? json) {
    if (json == null) return const ModelCost();
    return ModelCost(
      inputPerMTok: (json['input'] as num?)?.toDouble() ?? 0,
      outputPerMTok: (json['output'] as num?)?.toDouble() ?? 0,
    );
  }
}

class ModelInfo {
  final String id;
  final String ownedBy;
  final List<String> aliases;
  final bool? local;
  final int? contextWindow;
  final List<String> capabilities;
  final String? speed;
  final ModelCost? cost;

  const ModelInfo({
    required this.id,
    required this.ownedBy,
    this.aliases = const [],
    this.local,
    this.contextWindow,
    this.capabilities = const [],
    this.speed,
    this.cost,
  });

  /// True for the synthetic `auto` entry the gateway always lists first.
  bool get isAuto => id == 'auto';

  factory ModelInfo.fromJson(Map<String, dynamic> json) {
    final prism = json['prism'] as Map<String, dynamic>?;
    return ModelInfo(
      id: json['id'] as String? ?? '',
      ownedBy: json['owned_by'] as String? ?? '',
      aliases: (prism?['aliases'] as List<dynamic>?)
              ?.map((e) => e.toString())
              .toList(growable: false) ??
          const [],
      local: prism == null ? null : prism['local'] as bool?,
      contextWindow: (prism?['context_window'] as num?)?.toInt(),
      capabilities: (prism?['capabilities'] as List<dynamic>?)
              ?.map((e) => e.toString())
              .toList(growable: false) ??
          const [],
      speed: prism?['speed'] as String?,
      cost: prism?['cost_per_mtok'] == null
          ? null
          : ModelCost.fromJson(prism!['cost_per_mtok'] as Map<String, dynamic>),
    );
  }
}
