/// A single NDJSON line from Ollama's `POST /api/pull` stream, e.g.
/// `{"status":"pulling <digest>","digest":"sha256:...","total":123,"completed":45}`
/// or, on failure, `{"error":"..."}`.
class PullProgress {
  final String status;
  final String? digest;
  final int? total;
  final int? completed;
  final String? error;

  const PullProgress({
    required this.status,
    this.digest,
    this.total,
    this.completed,
    this.error,
  });

  factory PullProgress.fromJson(Map<String, dynamic> json) {
    return PullProgress(
      status: json['status'] as String? ?? '',
      digest: json['digest'] as String?,
      total: (json['total'] as num?)?.toInt(),
      completed: (json['completed'] as num?)?.toInt(),
      error: json['error'] as String?,
    );
  }

  bool get isError => error != null;

  /// Progress fraction for the current layer, in [0, 1], or null when the
  /// server hasn't reported a size for this status line yet (e.g.
  /// "pulling manifest").
  double? get fraction {
    final t = total;
    final c = completed;
    if (t == null || t <= 0 || c == null) return null;
    return (c / t).clamp(0, 1).toDouble();
  }

  bool get isDone => status.toLowerCase() == 'success';
}
