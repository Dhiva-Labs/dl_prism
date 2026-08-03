import 'package:flutter/material.dart';

import '../data/model_catalog.dart';

/// Curated catalog of pullable models, shown above the free-text pull
/// field in [LocalModelsCard]. Each entry is either already installed
/// ("Installed" chip) or offers a one-click Pull button.
class ModelCatalogSection extends StatelessWidget {
  final Set<String> installedModelNames;

  /// The model currently being pulled (from anywhere — catalog or
  /// free-text), used to disable every *other* Pull button so only one
  /// pull runs at a time.
  final String? pullingModel;
  final void Function(String tag) onPull;

  const ModelCatalogSection({
    super.key,
    required this.installedModelNames,
    required this.onPull,
    this.pullingModel,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Catalog', style: theme.textTheme.titleSmall),
        const SizedBox(height: 4),
        ...kModelCatalog.map((entry) {
          final installed =
              installedModelNames.any((name) => entry.matchesInstalled(name));
          final pullingThis = pullingModel == entry.tag;
          final pullDisabled = pullingModel != null && !pullingThis;
          return _CatalogTile(
            entry: entry,
            installed: installed,
            pulling: pullingThis,
            pullDisabled: pullDisabled,
            onPull: () => onPull(entry.tag),
          );
        }),
      ],
    );
  }
}

class _CatalogTile extends StatelessWidget {
  final CatalogModel entry;
  final bool installed;
  final bool pulling;
  final bool pullDisabled;
  final VoidCallback onPull;

  const _CatalogTile({
    required this.entry,
    required this.installed,
    required this.pulling,
    required this.pullDisabled,
    required this.onPull,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Row(
                  children: [
                    Text(entry.displayName, style: theme.textTheme.bodyMedium),
                    const SizedBox(width: 8),
                    Text(
                      '${entry.family} · ~${entry.approxSizeGB.toStringAsFixed(1)} GB',
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                    ),
                  ],
                ),
                Text(
                  entry.blurb,
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          if (installed)
            Chip(
              label: const Text('Installed'),
              visualDensity: VisualDensity.compact,
              backgroundColor: theme.colorScheme.tertiaryContainer,
              labelStyle: TextStyle(
                color: theme.colorScheme.onTertiaryContainer,
                fontSize: 12,
              ),
              side: BorderSide.none,
              padding: const EdgeInsets.symmetric(horizontal: 4),
            )
          else if (pulling)
            const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          else
            OutlinedButton(
              onPressed: pullDisabled ? null : onPull,
              child: const Text('Pull'),
            ),
        ],
      ),
    );
  }
}
