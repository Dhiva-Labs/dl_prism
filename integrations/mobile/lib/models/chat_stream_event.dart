import 'route_info.dart';

/// One event out of [PrismApiClient.streamChatCompletion]'s event stream —
/// a sealed hierarchy so the chat screen's `switch` is exhaustive and the
/// analyzer catches a missed case.
sealed class ChatStreamEvent {
  const ChatStreamEvent();
}

/// Emitted once, as soon as response headers arrive (before any body byte),
/// carrying the `X-Prism-*` routing decision for this reply.
class ChatRouteEvent extends ChatStreamEvent {
  final RouteInfo route;
  const ChatRouteEvent(this.route);
}

/// A text fragment to append to the in-progress assistant message.
class ChatDeltaEvent extends ChatStreamEvent {
  final String text;
  const ChatDeltaEvent(this.text);
}

/// The stream finished normally (`data: [DONE]`, or the connection closed
/// cleanly without one).
class ChatDoneEvent extends ChatStreamEvent {
  const ChatDoneEvent();
}

/// The request failed outright, or the stream broke partway through. Any
/// [ChatDeltaEvent]s already emitted are not retracted — the caller keeps
/// that partial text and renders this alongside it as an error strip.
class ChatErrorEvent extends ChatStreamEvent {
  final String message;
  final int? statusCode;
  const ChatErrorEvent(this.message, {this.statusCode});

  /// Whether this looks like an auth failure the Chat screen should offer an
  /// inline reauth prompt for, rather than just a retry.
  bool get isAuthError => statusCode == 401;
}
