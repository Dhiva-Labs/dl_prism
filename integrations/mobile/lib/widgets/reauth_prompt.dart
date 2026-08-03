import 'package:flutter/material.dart';

/// Inline "your API key was rejected" prompt shown in place of (or above)
/// a screen's content on a 401, with a field to enter a corrected key right
/// there rather than forcing a trip to Settings.
class ReauthPrompt extends StatefulWidget {
  final String message;
  final ValueChanged<String> onSubmit;

  const ReauthPrompt({super.key, required this.message, required this.onSubmit});

  @override
  State<ReauthPrompt> createState() => _ReauthPromptState();
}

class _ReauthPromptState extends State<ReauthPrompt> {
  final _controller = TextEditingController();
  bool _obscure = true;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      color: theme.colorScheme.errorContainer,
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.lock_outline, color: theme.colorScheme.onErrorContainer),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Authentication needed',
                    style: theme.textTheme.titleMedium
                        ?.copyWith(color: theme.colorScheme.onErrorContainer),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(widget.message, style: TextStyle(color: theme.colorScheme.onErrorContainer)),
            const SizedBox(height: 12),
            TextField(
              controller: _controller,
              obscureText: _obscure,
              style: TextStyle(color: theme.colorScheme.onErrorContainer),
              decoration: InputDecoration(
                labelText: 'API key',
                labelStyle: TextStyle(color: theme.colorScheme.onErrorContainer),
                filled: true,
                fillColor: theme.colorScheme.surface,
                suffixIcon: IconButton(
                  icon: Icon(_obscure ? Icons.visibility : Icons.visibility_off),
                  onPressed: () => setState(() => _obscure = !_obscure),
                ),
                border: const OutlineInputBorder(),
              ),
              onSubmitted: (v) => widget.onSubmit(v.trim()),
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton(
                onPressed: () => widget.onSubmit(_controller.text.trim()),
                child: const Text('Save and retry'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
