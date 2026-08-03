import 'package:flutter/material.dart';

import '../models/gateway_status.dart';
import 'status_dot.dart';

/// Card listing each provider the *running gateway* currently reports
/// (from `/admin/status`): name, local/cloud chip, health dot, and model
/// count. Not to be confused with [ProvidersCard], which offers one-click
/// connect/disconnect against the gateway *config file* regardless of
/// whether the gateway is currently running.
class ProvidersList extends StatelessWidget {
  final List<ProviderInfo> providers;

  const ProvidersList({super.key, required this.providers});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Gateway providers', style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            if (providers.isEmpty)
              Text(
                'No provider data yet.',
                style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
              )
            else
              ...providers.map((p) => _ProviderTile(provider: p)),
          ],
        ),
      ),
    );
  }
}

class _ProviderTile extends StatelessWidget {
  final ProviderInfo provider;

  const _ProviderTile({required this.provider});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          StatusDot(healthy: provider.healthy),
          const SizedBox(width: 10),
          Expanded(
            child: Text(provider.name, style: theme.textTheme.bodyMedium),
          ),
          Chip(
            label: Text(provider.local ? 'local' : 'cloud'),
            visualDensity: VisualDensity.compact,
            backgroundColor: provider.local
                ? theme.colorScheme.tertiaryContainer
                : theme.colorScheme.secondaryContainer,
            labelStyle: TextStyle(
              color: provider.local
                  ? theme.colorScheme.onTertiaryContainer
                  : theme.colorScheme.onSecondaryContainer,
              fontSize: 12,
            ),
            side: BorderSide.none,
            padding: const EdgeInsets.symmetric(horizontal: 4),
          ),
          const SizedBox(width: 12),
          Text(
            '${provider.models} model${provider.models == 1 ? '' : 's'}',
            style: theme.textTheme.bodySmall
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}
