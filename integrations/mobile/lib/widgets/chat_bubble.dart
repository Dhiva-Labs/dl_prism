import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/chat_message.dart';
import 'route_chip_row.dart';

/// One chat turn: a user or assistant bubble. Assistant bubbles show a
/// streaming cursor while [ChatMessage.isStreaming] is true, the route chip
/// row once routing info has arrived, a copy action once there is text to
/// copy, and — the "graceful mid-stream error handling" requirement — an
/// error strip below whatever partial text already streamed in if
/// [ChatMessage.error] is set. The partial text is never discarded.
class ChatBubble extends StatelessWidget {
  final ChatMessage message;
  final VoidCallback? onRetry;

  const ChatBubble({super.key, required this.message, this.onRetry});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isUser = message.role == ChatRole.user;
    final bubbleColor =
        isUser ? theme.colorScheme.primaryContainer : theme.colorScheme.surfaceContainerHigh;
    final textColor = isUser ? theme.colorScheme.onPrimaryContainer : theme.colorScheme.onSurface;

    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: MediaQuery.sizeOf(context).width * 0.85),
        child: Column(
          crossAxisAlignment: isUser ? CrossAxisAlignment.end : CrossAxisAlignment.start,
          children: [
            Container(
              margin: const EdgeInsets.symmetric(vertical: 4),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              decoration: BoxDecoration(
                color: bubbleColor,
                borderRadius: BorderRadius.circular(16),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (message.content.isNotEmpty)
                    SelectableText(message.content, style: TextStyle(color: textColor))
                  else if (message.isStreaming)
                    _TypingIndicator(color: textColor)
                  else if (message.error == null)
                    Text('(empty reply)', style: TextStyle(color: textColor)),
                  if (message.isStreaming && message.content.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: _TypingIndicator(color: textColor),
                    ),
                  if (!isUser && message.route != null) RouteChipRow(route: message.route!),
                ],
              ),
            ),
            if (message.error != null) _ErrorStrip(message: message.error!, onRetry: onRetry),
            if (!isUser && !message.isStreaming && message.content.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(left: 4),
                child: IconButton(
                  icon: const Icon(Icons.copy, size: 16),
                  visualDensity: VisualDensity.compact,
                  tooltip: 'Copy',
                  onPressed: () => Clipboard.setData(ClipboardData(text: message.content)),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _TypingIndicator extends StatelessWidget {
  final Color color;
  const _TypingIndicator({required this.color});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 16,
      height: 16,
      child: CircularProgressIndicator(strokeWidth: 2, color: color),
    );
  }
}

class _ErrorStrip extends StatelessWidget {
  final String message;
  final VoidCallback? onRetry;
  const _ErrorStrip({required this.message, this.onRetry});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      key: const Key('chat_error_strip'),
      margin: const EdgeInsets.only(top: 2, bottom: 6),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.error_outline, size: 16, color: theme.colorScheme.onErrorContainer),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onErrorContainer),
            ),
          ),
          if (onRetry != null)
            TextButton(
              onPressed: onRetry,
              style: TextButton.styleFrom(
                foregroundColor: theme.colorScheme.onErrorContainer,
                minimumSize: Size.zero,
                padding: const EdgeInsets.symmetric(horizontal: 8),
              ),
              child: const Text('Retry'),
            ),
        ],
      ),
    );
  }
}
