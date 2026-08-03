/// Data models for `GET /admin/status`. Mirrors the desktop app's
/// `GatewayStatus` (`integrations/desktop/lib/models/gateway_status.dart`).
/// No screen surfaces this yet — Settings could grow a "gateway info" row
/// later — but the client supports it now since it is a one-line addition
/// once `fetchUsage`/`fetchModels` already exist, and the app should not
/// have to touch the client again to add it.
library;

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
}
