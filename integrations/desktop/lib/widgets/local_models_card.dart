import 'package:flutter/material.dart';

import '../models/ollama_model.dart';
import '../models/pull_progress.dart';
import 'model_catalog_section.dart';

/// Card for installing/running/stopping/uninstalling local Ollama models.
///
/// This widget is purely presentational — all the Ollama HTTP calls and
/// polling live in [HomeScreen]; this just renders whatever state it's
/// handed and reports user intent via callbacks.
class LocalModelsCard extends StatefulWidget {
  final bool reachable;
  final List<OllamaModel> installedModels;
  final Set<String> loadedModelNames;

  /// Model names that currently have a run/stop/delete request in flight.
  final Set<String> busyModelNames;

  /// The model currently being pulled, if any.
  final String? pullingModel;
  final PullProgress? pullProgress;
  final String? pullError;

  final VoidCallback onRetry;
  final VoidCallback onInstallOllama;
  final void Function(String name) onRun;
  final void Function(String name) onStop;
  final void Function(String name) onDelete;
  final void Function(String name) onPull;
  final VoidCallback onCancelPull;

  const LocalModelsCard({
    super.key,
    required this.reachable,
    required this.installedModels,
    required this.loadedModelNames,
    required this.busyModelNames,
    required this.onRetry,
    required this.onInstallOllama,
    required this.onRun,
    required this.onStop,
    required this.onDelete,
    required this.onPull,
    required this.onCancelPull,
    this.pullingModel,
    this.pullProgress,
    this.pullError,
  });

  @override
  State<LocalModelsCard> createState() => _LocalModelsCardState();
}

class _LocalModelsCardState extends State<LocalModelsCard> {
  final _pullCtrl = TextEditingController();

  @override
  void dispose() {
    _pullCtrl.dispose();
    super.dispose();
  }

  Future<void> _confirmDelete(String name) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete model?'),
        content: Text('This removes "$name" from disk. You can pull it again later.'),
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
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed == true) widget.onDelete(name);
  }

  void _submitPull() {
    final name = _pullCtrl.text.trim();
    if (name.isEmpty) return;
    widget.onPull(name);
    _pullCtrl.clear();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Local models', style: theme.textTheme.titleMedium),
            const SizedBox(height: 12),
            if (!widget.reachable)
              _NotDetected(onInstall: widget.onInstallOllama, onRetry: widget.onRetry)
            else ...[
              if (widget.installedModels.isEmpty)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    'No models installed yet. Try pulling a small starter model, '
                    'e.g. "qwen2.5-coder:3b" or "llama3.2:1b".',
                    style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
                  ),
                )
              else
                ...widget.installedModels.map(
                  (m) => _ModelRow(
                    model: m,
                    loaded: widget.loadedModelNames.contains(m.name),
                    busy: widget.busyModelNames.contains(m.name),
                    onRun: () => widget.onRun(m.name),
                    onStop: () => widget.onStop(m.name),
                    onDelete: () => _confirmDelete(m.name),
                  ),
                ),
              const Divider(height: 24),
              ModelCatalogSection(
                installedModelNames: widget.installedModels.map((m) => m.name).toSet(),
                pullingModel: widget.pullingModel,
                onPull: widget.onPull,
              ),
              const SizedBox(height: 12),
              if (widget.pullingModel != null)
                _PullProgressRow(
                  modelName: widget.pullingModel!,
                  progress: widget.pullProgress,
                  error: widget.pullError,
                  onCancel: widget.onCancelPull,
                )
              else
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _pullCtrl,
                        decoration: const InputDecoration(
                          isDense: true,
                          labelText: 'Pull something else',
                          hintText: 'Not in the catalog above? e.g. mistral-nemo:12b',
                          border: OutlineInputBorder(),
                        ),
                        onSubmitted: (_) => _submitPull(),
                      ),
                    ),
                    const SizedBox(width: 8),
                    FilledButton.icon(
                      onPressed: _submitPull,
                      icon: const Icon(Icons.download, size: 18),
                      label: const Text('Pull'),
                    ),
                  ],
                ),
            ],
          ],
        ),
      ),
    );
  }
}

class _NotDetected extends StatelessWidget {
  final VoidCallback onInstall;
  final VoidCallback onRetry;

  const _NotDetected({required this.onInstall, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Expanded(
          child: Text(
            'Ollama not detected.',
            style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
        OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
        const SizedBox(width: 8),
        FilledButton.icon(
          onPressed: onInstall,
          icon: const Icon(Icons.open_in_new, size: 18),
          label: const Text('Install Ollama'),
        ),
      ],
    );
  }
}

class _ModelRow extends StatelessWidget {
  final OllamaModel model;
  final bool loaded;
  final bool busy;
  final VoidCallback onRun;
  final VoidCallback onStop;
  final VoidCallback onDelete;

  const _ModelRow({
    required this.model,
    required this.loaded,
    required this.busy,
    required this.onRun,
    required this.onStop,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(model.name, style: theme.textTheme.bodyMedium),
                Text(
                  '${model.sizeGb.toStringAsFixed(1)} GB',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
            ),
          ),
          if (loaded)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Chip(
                label: const Text('loaded'),
                visualDensity: VisualDensity.compact,
                backgroundColor: theme.colorScheme.tertiaryContainer,
                labelStyle: TextStyle(
                  color: theme.colorScheme.onTertiaryContainer,
                  fontSize: 12,
                ),
                side: BorderSide.none,
                padding: const EdgeInsets.symmetric(horizontal: 4),
              ),
            ),
          if (busy)
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 8),
              child: SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            )
          else ...[
            if (loaded)
              TextButton(onPressed: onStop, child: const Text('Stop'))
            else
              TextButton(onPressed: onRun, child: const Text('Run')),
            IconButton(
              tooltip: 'Delete',
              icon: Icon(Icons.delete_outline, color: theme.colorScheme.error, size: 20),
              onPressed: onDelete,
            ),
          ],
        ],
      ),
    );
  }
}

class _PullProgressRow extends StatelessWidget {
  final String modelName;
  final PullProgress? progress;
  final String? error;
  final VoidCallback onCancel;

  const _PullProgressRow({
    required this.modelName,
    required this.progress,
    required this.error,
    required this.onCancel,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final fraction = progress?.fraction;
    final statusText = error ?? progress?.status ?? 'starting...';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Text('Pulling $modelName', style: theme.textTheme.bodyMedium),
            ),
            TextButton(onPressed: onCancel, child: const Text('Cancel')),
          ],
        ),
        const SizedBox(height: 4),
        ClipRRect(
          borderRadius: BorderRadius.circular(4),
          child: LinearProgressIndicator(
            value: fraction,
            minHeight: 6,
            backgroundColor: theme.colorScheme.surfaceContainerHighest,
            color: error != null ? theme.colorScheme.error : theme.colorScheme.primary,
          ),
        ),
        const SizedBox(height: 4),
        Row(
          children: [
            Expanded(
              child: Text(
                statusText,
                style: theme.textTheme.bodySmall?.copyWith(
                  color:
                      error != null ? theme.colorScheme.error : theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
            if (fraction != null)
              Text(
                '${(fraction * 100).toStringAsFixed(0)}%',
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
          ],
        ),
      ],
    );
  }
}
