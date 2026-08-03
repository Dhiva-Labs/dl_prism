import 'package:flutter/material.dart';

/// Persistent top-of-screen banner for "the gateway could not be reached" —
/// used instead of a blank/spinner-forever screen so the user always has a
/// next action. Distinct from [ReauthPrompt]: this is for network-level
/// unreachability (wrong host, gateway down, no route), not a rejected key.
class ConnectionBanner extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;
  final bool retrying;

  const ConnectionBanner({
    super.key,
    required this.message,
    required this.onRetry,
    this.retrying = false,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(Icons.cloud_off, color: theme.colorScheme.onErrorContainer, size: 20),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                message,
                style: TextStyle(color: theme.colorScheme.onErrorContainer),
              ),
            ),
            const SizedBox(width: 8),
            retrying
                ? SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: theme.colorScheme.onErrorContainer,
                    ),
                  )
                : TextButton(
                    onPressed: onRetry,
                    style: TextButton.styleFrom(foregroundColor: theme.colorScheme.onErrorContainer),
                    child: const Text('Retry'),
                  ),
          ],
        ),
      ),
    );
  }
}
