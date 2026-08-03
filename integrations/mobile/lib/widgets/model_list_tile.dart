import 'package:flutter/material.dart';

import '../models/model_info.dart';

/// One entry in the Models registry list: id/aliases, a local-vs-cloud
/// chip, context window, and a chip per capability.
class ModelListTile extends StatelessWidget {
  final ModelInfo model;

  const ModelListTile({super.key, required this.model});

  String _formatContextWindow(int tokens) {
    if (tokens >= 1000) {
      final k = tokens / 1000;
      return '${k == k.roundToDouble() ? k.toInt() : k.toStringAsFixed(1)}K ctx';
    }
    return '$tokens ctx';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(model.id, style: theme.textTheme.titleMedium),
                ),
                if (model.local != null)
                  Chip(
                    label: Text(model.local! ? 'Local' : 'Cloud'),
                    avatar: Icon(model.local! ? Icons.computer : Icons.cloud_outlined, size: 16),
                    backgroundColor: model.local!
                        ? theme.colorScheme.tertiaryContainer
                        : theme.colorScheme.secondaryContainer,
                    labelStyle: TextStyle(
                      color: model.local!
                          ? theme.colorScheme.onTertiaryContainer
                          : theme.colorScheme.onSecondaryContainer,
                    ),
                    visualDensity: VisualDensity.compact,
                    side: BorderSide.none,
                  ),
              ],
            ),
            Text(
              model.ownedBy,
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
            if (model.aliases.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: Text(
                  'aka ${model.aliases.join(', ')}',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant, fontStyle: FontStyle.italic),
                ),
              ),
            const SizedBox(height: 10),
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                if (model.contextWindow != null)
                  Chip(
                    label: Text(_formatContextWindow(model.contextWindow!)),
                    visualDensity: VisualDensity.compact,
                    side: BorderSide.none,
                    backgroundColor: theme.colorScheme.surfaceContainerHighest,
                  ),
                if (model.speed != null)
                  Chip(
                    avatar: const Icon(Icons.speed, size: 14),
                    label: Text(model.speed!),
                    visualDensity: VisualDensity.compact,
                    side: BorderSide.none,
                    backgroundColor: theme.colorScheme.surfaceContainerHighest,
                  ),
                for (final cap in model.capabilities)
                  Chip(
                    label: Text(cap),
                    visualDensity: VisualDensity.compact,
                    side: BorderSide.none,
                    backgroundColor: theme.colorScheme.surfaceContainerHighest,
                  ),
              ],
            ),
            if (model.cost != null && (model.cost!.inputPerMTok > 0 || model.cost!.outputPerMTok > 0))
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  '\$${model.cost!.inputPerMTok.toStringAsFixed(2)} in / '
                  '\$${model.cost!.outputPerMTok.toStringAsFixed(2)} out per M tok',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
