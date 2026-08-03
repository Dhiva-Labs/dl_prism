import 'route_info.dart';

enum ChatRole { user, assistant }

/// One turn in the on-screen conversation.
///
/// Assistant turns start with [isStreaming] true and an empty [content],
/// grow as SSE deltas arrive, and settle once the stream ends. If the stream
/// breaks partway through, [content] keeps whatever text already arrived and
/// [error] is set — the chat screen renders the partial reply followed by an
/// error strip rather than discarding it (see COMPANIONS mobile spec:
/// "graceful mid-stream error handling").
class ChatMessage {
  final ChatRole role;
  String content;
  bool isStreaming;
  String? error;
  RouteInfo? route;

  ChatMessage({
    required this.role,
    this.content = '',
    this.isStreaming = false,
    this.error,
    this.route,
  });

  factory ChatMessage.user(String content) =>
      ChatMessage(role: ChatRole.user, content: content);

  factory ChatMessage.assistantPlaceholder() =>
      ChatMessage(role: ChatRole.assistant, isStreaming: true);
}
