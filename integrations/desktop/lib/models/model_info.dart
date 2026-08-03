/// Data model for a single entry of `GET /v1/models`.
library;

class ModelInfo {
  final String id;
  final String ownedBy;
  final bool? local;

  const ModelInfo({required this.id, required this.ownedBy, this.local});

  factory ModelInfo.fromJson(Map<String, dynamic> json) {
    final prism = json['prism'] as Map<String, dynamic>?;
    return ModelInfo(
      id: json['id'] as String? ?? '',
      ownedBy: json['owned_by'] as String? ?? '',
      local: prism == null ? null : prism['local'] as bool?,
    );
  }
}
