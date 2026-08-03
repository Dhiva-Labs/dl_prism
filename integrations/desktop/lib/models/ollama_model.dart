/// Data models for Ollama's `GET /api/tags` and `GET /api/ps`.
library;

/// An installed (on-disk) model, as reported by `GET /api/tags`.
class OllamaModel {
  final String name;
  final int sizeBytes;
  final DateTime? modifiedAt;

  const OllamaModel({
    required this.name,
    required this.sizeBytes,
    this.modifiedAt,
  });

  factory OllamaModel.fromJson(Map<String, dynamic> json) {
    final modifiedRaw = json['modified_at'] as String?;
    return OllamaModel(
      name: json['name'] as String? ?? json['model'] as String? ?? '',
      sizeBytes: (json['size'] as num?)?.toInt() ?? 0,
      modifiedAt: modifiedRaw == null ? null : DateTime.tryParse(modifiedRaw),
    );
  }

  double get sizeGb => sizeBytes / (1024 * 1024 * 1024);
}

/// A currently-loaded (in-memory) model, as reported by `GET /api/ps`.
class LoadedModel {
  final String name;
  final int sizeVramBytes;
  final DateTime? expiresAt;

  const LoadedModel({
    required this.name,
    required this.sizeVramBytes,
    this.expiresAt,
  });

  factory LoadedModel.fromJson(Map<String, dynamic> json) {
    final expiresRaw = json['expires_at'] as String?;
    return LoadedModel(
      name: json['name'] as String? ?? json['model'] as String? ?? '',
      sizeVramBytes:
          (json['size_vram'] as num?)?.toInt() ?? (json['size'] as num?)?.toInt() ?? 0,
      expiresAt: expiresRaw == null ? null : DateTime.tryParse(expiresRaw),
    );
  }
}
