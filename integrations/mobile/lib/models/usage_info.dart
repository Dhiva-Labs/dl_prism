/// Data models for `GET /admin/usage`.
library;

/// Aggregated usage for a single model over the current month.
class ModelUsage {
  final String provider;
  final String model;
  final int requests;
  final int promptTokens;
  final int completionTokens;
  final double costUsd;

  const ModelUsage({
    required this.provider,
    required this.model,
    required this.requests,
    required this.promptTokens,
    required this.completionTokens,
    required this.costUsd,
  });

  factory ModelUsage.fromJson(Map<String, dynamic> json) {
    return ModelUsage(
      provider: json['provider'] as String? ?? '',
      model: json['model'] as String? ?? '',
      requests: (json['requests'] as num?)?.toInt() ?? 0,
      promptTokens: (json['prompt_tokens'] as num?)?.toInt() ?? 0,
      completionTokens: (json['completion_tokens'] as num?)?.toInt() ?? 0,
      costUsd: (json['cost_usd'] as num?)?.toDouble() ?? 0,
    );
  }
}

/// The gateway's `/admin/usage` response.
class UsageInfo {
  final double monthTotalUsd;
  final double budgetUsd;
  final bool budgetExceeded;
  final List<ModelUsage> models;

  const UsageInfo({
    required this.monthTotalUsd,
    required this.budgetUsd,
    required this.budgetExceeded,
    required this.models,
  });

  factory UsageInfo.fromJson(Map<String, dynamic> json) {
    final rawModels = json['models'] as List<dynamic>? ?? const [];
    return UsageInfo(
      monthTotalUsd: (json['month_total_usd'] as num?)?.toDouble() ?? 0,
      budgetUsd: (json['budget_usd'] as num?)?.toDouble() ?? 0,
      budgetExceeded: json['budget_exceeded'] as bool? ?? false,
      models: rawModels
          .map((e) => ModelUsage.fromJson(e as Map<String, dynamic>))
          .toList(growable: false),
    );
  }

  /// Fraction of budget consumed, clamped to [0, 1] for progress display.
  /// Returns 0 when there is no budget configured.
  double get budgetFraction {
    if (budgetUsd <= 0) return 0;
    final fraction = monthTotalUsd / budgetUsd;
    if (fraction.isNaN || fraction.isInfinite) return 0;
    return fraction.clamp(0, 1).toDouble();
  }

  /// Total request count across all models this month.
  int get totalRequests => models.fold(0, (sum, m) => sum + m.requests);
}
