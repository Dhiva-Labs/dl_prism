import 'package:flutter/material.dart';

import '../models/gateway_status.dart';
import '../services/gateway_process_controller.dart';
import 'log_pane.dart';
import 'status_dot.dart';

/// Top "control tower" card: gateway reachability, key status fields, and
/// start/stop controls for a gateway process we manage ourselves.
class GatewayCard extends StatefulWidget {
  final bool reachable;
  final GatewayStatus? status;
  final GatewayProcessController controller;
  final bool starting;
  final String? startError;
  final VoidCallback onStart;
  final VoidCallback onStop;
  final VoidCallback onOpenDashboard;

  const GatewayCard({
    super.key,
    required this.reachable,
    required this.status,
    required this.controller,
    required this.onStart,
    required this.onStop,
    required this.onOpenDashboard,
    this.starting = false,
    this.startError,
  });

  @override
  State<GatewayCard> createState() => _GatewayCardState();
}

class _GatewayCardState extends State<GatewayCard> {
  bool _showLogs = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: ListenableBuilder(
          listenable: widget.controller,
          builder: (context, _) {
            final weOwnProcess = widget.controller.isRunning;
            final runningExternally = widget.reachable && !weOwnProcess;
            final hasLogs = widget.controller.logLines.isNotEmpty;

            return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    StatusDot(healthy: widget.reachable, size: 12),
                    const SizedBox(width: 8),
                    Text(
                      widget.reachable ? 'Gateway reachable' : 'Gateway unreachable',
                      style: theme.textTheme.titleMedium,
                    ),
                    const Spacer(),
                    if (widget.starting)
                      const Padding(
                        padding: EdgeInsets.only(right: 12),
                        child: SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        ),
                      ),
                    if (!widget.reachable && !weOwnProcess)
                      FilledButton.icon(
                        onPressed: widget.starting ? null : widget.onStart,
                        icon: const Icon(Icons.play_arrow, size: 18),
                        label: const Text('Start gateway'),
                      ),
                    if (weOwnProcess)
                      OutlinedButton.icon(
                        onPressed: widget.onStop,
                        icon: const Icon(Icons.stop, size: 18),
                        label: const Text('Stop'),
                      ),
                  ],
                ),
                if (widget.startError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    widget.startError!,
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
                ],
                if (runningExternally) ...[
                  const SizedBox(height: 8),
                  Text(
                    'Running externally — not started by this app, so Stop is disabled.',
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                  ),
                ],
                if (widget.reachable && widget.status != null) ...[
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 24,
                    runSpacing: 8,
                    children: [
                      _Field(label: 'Version', value: widget.status!.version),
                      _Field(label: 'Uptime', value: widget.status!.uptimeLabel),
                      _Field(label: 'Mode', value: widget.status!.modeLabel),
                      _Field(label: 'Listening on', value: widget.status!.listen),
                    ],
                  ),
                ],
                const SizedBox(height: 12),
                FilledButton.icon(
                  onPressed: widget.onOpenDashboard,
                  icon: const Icon(Icons.dashboard_outlined, size: 18),
                  label: const Text('Open Dashboard'),
                ),
                if (hasLogs || weOwnProcess) ...[
                  const SizedBox(height: 12),
                  TextButton.icon(
                    onPressed: () => setState(() => _showLogs = !_showLogs),
                    icon: Icon(_showLogs ? Icons.expand_less : Icons.expand_more),
                    label: Text(_showLogs ? 'Hide logs' : 'Show logs'),
                  ),
                  if (_showLogs) LogPane(lines: widget.controller.logLines),
                ],
              ],
            );
          },
        ),
      ),
    );
  }
}

class _Field extends StatelessWidget {
  final String label;
  final String value;

  const _Field({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(label, style: theme.textTheme.labelSmall
            ?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
        Text(value.isEmpty ? '—' : value, style: theme.textTheme.bodyMedium),
      ],
    );
  }
}
