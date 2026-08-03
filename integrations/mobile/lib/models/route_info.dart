/// The routing decision the gateway made for one reply, read off the
/// `X-Prism-*` response headers `/v1/chat/completions` sets before the first
/// byte of the body (see `internal/server/server.go`'s `executeChain`) —
/// present on both streamed and non-streamed responses.
library;

class RouteInfo {
  final String? provider;
  final String? model;
  final String? mode;
  final String? reason;

  const RouteInfo({this.provider, this.model, this.mode, this.reason});

  /// True once at least one header was actually present, so callers can tell
  /// "no route info" (an error before routing happened) from "routed with
  /// nothing but blank headers" (should not happen, but render nothing
  /// rather than an empty chip row either way).
  bool get isEmpty =>
      provider == null && model == null && mode == null && reason == null;

  factory RouteInfo.fromHeaders(Map<String, String> headers) {
    // http headers are case-insensitive on the wire; both the streamed and
    // non-streamed HTTP clients used here already lower-case header names.
    String? get(String name) {
      final v = headers[name.toLowerCase()];
      return (v == null || v.isEmpty) ? null : v;
    }

    return RouteInfo(
      provider: get('X-Prism-Provider'),
      model: get('X-Prism-Model'),
      mode: get('X-Prism-Mode'),
      reason: get('X-Prism-Reason'),
    );
  }
}
