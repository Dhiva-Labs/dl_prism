import 'dart:io';

/// The actual "cleartext for private ranges only" policy — see the doc
/// comment on `android/app/src/main/res/xml/network_security_config.xml`
/// for why that file cannot express this itself (no CIDR matching in
/// Android's network security config, only literal hosts). This is what
/// the Onboarding and Settings screens call before accepting an `http://`
/// base URL.
///
/// Returns true if [host] is safe for a cleartext (non-TLS) connection:
/// loopback, RFC 1918 private IPv4, RFC 4193 unique-local IPv6, link-local,
/// or Tailscale/CGNAT's 100.64.0.0/10 range.
///
/// A bare hostname that is not an IP literal at all (mDNS `.local`, a
/// Tailscale MagicDNS name, a router-assigned LAN name like `nas.home`) is
/// treated as private too. Classifying it correctly would need a DNS
/// lookup, which is more machinery than a synchronous form validator
/// should own; the risk this guard exists for is a public *IP* address
/// receiving a bearer key over cleartext; a bare hostname without one is
/// not that case, and the app will still refuse to send the request at all
/// if the host turns out to be unreachable.
bool isPrivateNetworkHost(String host) {
  final ip = InternetAddress.tryParse(host);
  if (ip == null) return true;
  if (ip.isLoopback) return true;

  final bytes = ip.rawAddress;
  if (ip.type == InternetAddressType.IPv4) {
    final a = bytes[0], b = bytes[1];
    if (a == 10) return true; // 10.0.0.0/8
    if (a == 172 && b >= 16 && b <= 31) return true; // 172.16.0.0/12
    if (a == 192 && b == 168) return true; // 192.168.0.0/16
    if (a == 169 && b == 254) return true; // 169.254.0.0/16 link-local
    if (a == 100 && b >= 64 && b <= 127) return true; // 100.64.0.0/10 (Tailscale CGNAT)
    return false;
  }

  // IPv6.
  if (bytes[0] == 0xfe && (bytes[1] & 0xc0) == 0x80) return true; // fe80::/10 link-local
  if ((bytes[0] & 0xfe) == 0xfc) return true; // fc00::/7 unique-local
  return false;
}

/// Validates a base-URL string against both "is this a URL at all" and the
/// cleartext policy above. Returns a user-facing error message, or null if
/// [value] is acceptable. Shared by the Onboarding and Settings forms so
/// the rule can't drift between them.
String? validateGatewayBaseUrl(String? value) {
  final trimmed = value?.trim() ?? '';
  if (trimmed.isEmpty) return 'Base URL is required';
  final uri = Uri.tryParse(trimmed);
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
    return 'Enter a full URL, e.g. http://192.168.1.20:8130';
  }
  if (uri.scheme != 'http' && uri.scheme != 'https') {
    return 'URL must start with http:// or https://';
  }
  if (uri.scheme == 'http' && !isPrivateNetworkHost(uri.host)) {
    return 'Plain http:// is only for private/LAN or VPN addresses. Use '
        'https:// for a public host.';
  }
  return null;
}
