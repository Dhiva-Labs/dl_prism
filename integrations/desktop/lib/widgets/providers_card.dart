import 'package:flutter/material.dart';

import '../data/provider_presets.dart';

/// Card of one-click cloud provider connect/disconnect tiles, laid out as a
/// compact wrap of chips (there are a dozen presets — a vertical list would
/// make this card dominate the screen). Purely presentational: [HomeScreen]
/// owns the actual config-file read/write via `PrismConfigWriter` and
/// passes the resulting state down.
class ProvidersCard extends StatelessWidget {
  final Set<String> connectedNames;

  /// Provider names with a connect/disconnect request in flight.
  final Set<String> busyNames;

  /// Called with the preset and the trimmed, non-empty API key once the
  /// connect dialog is submitted. Works for both a fresh connect and a
  /// replace of an existing connection — the config writer handles both
  /// the same way.
  final Future<void> Function(ProviderPreset preset, String apiKey) onConnect;
  final Future<void> Function(String name) onDisconnect;

  const ProvidersCard({
    super.key,
    required this.connectedNames,
    required this.busyNames,
    required this.onConnect,
    required this.onDisconnect,
  });

  Future<void> _openConnectDialog(BuildContext context, ProviderPreset preset) async {
    final alreadyConnected = connectedNames.contains(preset.name);
    final apiKey = await showDialog<String>(
      context: context,
      builder: (context) => _ConnectDialog(preset: preset, replacing: alreadyConnected),
    );
    if (apiKey == null) return;
    await onConnect(preset, apiKey);
  }

  Future<void> _confirmDisconnect(BuildContext context, ProviderPreset preset) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Disconnect provider?'),
        content: Text(
          'This removes the "${preset.name}" provider block (including its API '
          'key) from the gateway config. You can reconnect any time.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
              foregroundColor: Theme.of(context).colorScheme.onError,
            ),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Disconnect'),
          ),
        ],
      ),
    );
    if (confirmed == true) await onDisconnect(preset.name);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    // Connected presets first, otherwise the catalog's own order — a plain
    // filter (unlike List.sort, which Dart doesn't guarantee is stable)
    // preserves relative order within each group.
    final connected =
        kProviderPresets.where((p) => connectedNames.contains(p.name)).toList();
    final notConnected =
        kProviderPresets.where((p) => !connectedNames.contains(p.name)).toList();
    final ordered = [...connected, ...notConnected];

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Providers', style: theme.textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              'Connect a cloud provider to the gateway with one click.',
              style: theme.textTheme.bodySmall
                  ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: ordered
                  .map(
                    (preset) => _ProviderChip(
                      preset: preset,
                      connected: connectedNames.contains(preset.name),
                      busy: busyNames.contains(preset.name),
                      onTap: () => _openConnectDialog(context, preset),
                      onDisconnect: () => _confirmDisconnect(context, preset),
                    ),
                  )
                  .toList(),
            ),
          ],
        ),
      ),
    );
  }
}

class _ProviderChip extends StatelessWidget {
  final ProviderPreset preset;
  final bool connected;
  final bool busy;
  final VoidCallback onTap;
  final VoidCallback onDisconnect;

  const _ProviderChip({
    required this.preset,
    required this.connected,
    required this.busy,
    required this.onTap,
    required this.onDisconnect,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final avatar = busy
        ? const Padding(
            padding: EdgeInsets.all(4),
            child: SizedBox(
              width: 12,
              height: 12,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          )
        : CircleAvatar(
            radius: 11,
            backgroundColor:
                connected ? theme.colorScheme.tertiary : theme.colorScheme.secondaryContainer,
            child: Text(
              preset.badge,
              style: TextStyle(
                fontSize: 8,
                fontWeight: FontWeight.bold,
                color: connected
                    ? theme.colorScheme.onTertiary
                    : theme.colorScheme.onSecondaryContainer,
              ),
            ),
          );

    return Tooltip(
      message: preset.description,
      child: InputChip(
        avatar: avatar,
        label: Text(preset.displayName),
        selected: connected,
        showCheckmark: false,
        onPressed: busy ? null : onTap,
        onDeleted: (connected && !busy) ? onDisconnect : null,
        deleteIcon: const Icon(Icons.link_off, size: 16),
        deleteButtonTooltipMessage: 'Disconnect',
      ),
    );
  }
}

/// Description + the models it will add + obscured API-key field +
/// Connect/Replace dialog for one preset. Pops with the trimmed API key on
/// submit, or `null` on cancel.
class _ConnectDialog extends StatefulWidget {
  final ProviderPreset preset;
  final bool replacing;

  const _ConnectDialog({required this.preset, required this.replacing});

  @override
  State<_ConnectDialog> createState() => _ConnectDialogState();
}

class _ConnectDialogState extends State<_ConnectDialog> {
  final _formKey = GlobalKey<FormState>();
  final _keyCtrl = TextEditingController();
  bool _obscure = true;

  @override
  void dispose() {
    _keyCtrl.dispose();
    super.dispose();
  }

  void _submit() {
    if (!_formKey.currentState!.validate()) return;
    Navigator.of(context).pop(_keyCtrl.text.trim());
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: Text(widget.preset.displayName),
      content: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.preset.description),
            const SizedBox(height: 12),
            Text('Adds these models:', style: theme.textTheme.labelMedium),
            const SizedBox(height: 4),
            ...widget.preset.models.map(
              (m) => Text(
                '• ${m.id}${m.aliases.isNotEmpty ? ' (${m.aliases.join(', ')})' : ''}',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ),
            if (widget.replacing) ...[
              const SizedBox(height: 8),
              Text(
                'A connection for "${widget.preset.name}" already exists. '
                'Connecting again replaces its API key.',
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ],
            const SizedBox(height: 16),
            TextFormField(
              controller: _keyCtrl,
              autofocus: true,
              obscureText: _obscure,
              decoration: InputDecoration(
                labelText: 'API key',
                border: const OutlineInputBorder(),
                suffixIcon: IconButton(
                  icon: Icon(_obscure ? Icons.visibility : Icons.visibility_off),
                  onPressed: () => setState(() => _obscure = !_obscure),
                ),
              ),
              validator: (v) =>
                  (v == null || v.trim().isEmpty) ? 'API key must not be empty' : null,
              onFieldSubmitted: (_) => _submit(),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: _submit,
          child: Text(widget.replacing ? 'Replace' : 'Connect'),
        ),
      ],
    );
  }
}
