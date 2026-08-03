/// Data models for `GET /admin/status`.
library;

/// A single upstream provider entry as reported by the gateway.
class ProviderInfo {
  final String name;
  final String type;
  final bool local;
  final bool healthy;
  final int models;

  const ProviderInfo({
    required this.name,
    required this.type,
    required this.local,
    required this.healthy,
    required this.models,
  });

  factory ProviderInfo.fromJson(Map<String, dynamic> json) {
    return ProviderInfo(
      name: json['name'] as String? ?? '',
      type: json['type'] as String? ?? '',
      local: json['local'] as bool? ?? false,
      healthy: json['healthy'] as bool? ?? false,
      models: (json['models'] as num?)?.toInt() ?? 0,
    );
  }
}

/// The gateway's `/admin/status` response.
class GatewayStatus {
  final String version;
  final int uptimeSeconds;
  final bool offline;
  final String listen;
  final List<ProviderInfo> providers;

  const GatewayStatus({
    required this.version,
    required this.uptimeSeconds,
    required this.offline,
    required this.listen,
    required this.providers,
  });

  factory GatewayStatus.fromJson(Map<String, dynamic> json) {
    final rawProviders = json['providers'] as List<dynamic>? ?? const [];
    return GatewayStatus(
      version: json['version'] as String? ?? '',
      uptimeSeconds: (json['uptime_seconds'] as num?)?.toInt() ?? 0,
      offline: json['offline'] as bool? ?? false,
      listen: json['listen'] as String? ?? '',
      providers: rawProviders
          .map((e) => ProviderInfo.fromJson(e as Map<String, dynamic>))
          .toList(growable: false),
    );
  }

  /// Human readable uptime, e.g. "2h 14m" or "45s".
  String get uptimeLabel {
    final d = Duration(seconds: uptimeSeconds);
    if (d.inDays > 0) {
      return '${d.inDays}d ${d.inHours % 24}h';
    }
    if (d.inHours > 0) {
      return '${d.inHours}h ${d.inMinutes % 60}m';
    }
    if (d.inMinutes > 0) {
      return '${d.inMinutes}m ${d.inSeconds % 60}s';
    }
    return '${d.inSeconds}s';
  }

  String get modeLabel => offline ? 'Offline' : 'Online';
}
