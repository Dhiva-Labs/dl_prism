import 'package:flutter/material.dart';

import '../models/usage_info.dart';

/// Month-to-date spend vs budget, with a progress indicator and a
/// budget-exceeded highlight.
class UsageCard extends StatelessWidget {
  final UsageInfo? usage;

  const UsageCard({super.key, required this.usage});

  String _money(double v) => '\$${v.toStringAsFixed(2)}';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final u = usage;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Usage this month', style: theme.textTheme.titleMedium),
            const SizedBox(height: 12),
            if (u == null)
              Text(
                'No usage data yet.',
                style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
              )
            else ...[
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(
                    '${_money(u.monthTotalUsd)} / ${_money(u.budgetUsd)}',
                    style: theme.textTheme.headlineSmall,
                  ),
                  if (u.budgetExceeded)
                    Chip(
                      label: const Text('Budget exceeded'),
                      backgroundColor: theme.colorScheme.errorContainer,
                      labelStyle:
                          TextStyle(color: theme.colorScheme.onErrorContainer),
                      side: BorderSide.none,
                    ),
                ],
              ),
              const SizedBox(height: 8),
              ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: LinearProgressIndicator(
                  value: u.budgetFraction,
                  minHeight: 8,
                  backgroundColor: theme.colorScheme.surfaceContainerHighest,
                  color: u.budgetExceeded
                      ? theme.colorScheme.error
                      : theme.colorScheme.primary,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                '${u.totalRequests} request${u.totalRequests == 1 ? '' : 's'} this month',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
