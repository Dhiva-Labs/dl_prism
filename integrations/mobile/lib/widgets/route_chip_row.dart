import 'package:flutter/material.dart';

import '../models/route_info.dart';

/// Per-reply chip row surfacing the `X-Prism-*` routing decision: provider,
/// model, mode (auto/manual/failover), and reason as a tooltip on an info
/// icon (the reason string can be long, e.g. "task=code; failed over
/// after: ...").
class RouteChipRow extends StatelessWidget {
  final RouteInfo route;

  const RouteChipRow({super.key, required this.route});

  @override
  Widget build(BuildContext context) {
    if (route.isEmpty) return const SizedBox.shrink();
    final theme = Theme.of(context);

    Widget chip(String label, IconData icon) => Chip(
          avatar: Icon(icon, size: 16),
          label: Text(label, style: theme.textTheme.labelSmall),
          visualDensity: VisualDensity.compact,
          materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
          padding: const EdgeInsets.symmetric(horizontal: 4),
        );

    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Wrap(
        spacing: 6,
        runSpacing: 4,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          if (route.provider != null) chip(route.provider!, Icons.dns_outlined),
          if (route.model != null) chip(route.model!, Icons.smart_toy_outlined),
          if (route.mode != null) chip(route.mode!, Icons.alt_route),
          if (route.reason != null)
            Tooltip(
              message: route.reason!,
              child: Icon(Icons.info_outline, size: 16, color: theme.colorScheme.onSurfaceVariant),
            ),
        ],
      ),
    );
  }
}
