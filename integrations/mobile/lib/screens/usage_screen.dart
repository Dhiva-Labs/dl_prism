import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models/usage_info.dart';
import '../services/api_client.dart';
import '../widgets/connection_banner.dart';
import '../widgets/reauth_prompt.dart';

/// Month-to-date spend vs budget (`GET /admin/usage`): a progress bar, a
/// budget-exceeded state, and a per-model breakdown.
class UsageScreen extends StatefulWidget {
  const UsageScreen({super.key});

  @override
  State<UsageScreen> createState() => _UsageScreenState();
}

enum _LoadState { loading, loaded, unreachable, authError }

class _UsageScreenState extends State<UsageScreen> {
  _LoadState _state = _LoadState.loading;
  UsageInfo? _usage;
  String _errorMessage = '';

  AppState get _appState => AppStateScope.of(context);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    setState(() => _state = _LoadState.loading);
    try {
      final usage = await _appState.client.fetchUsage();
      if (!mounted) return;
      setState(() {
        _usage = usage;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = e.isAuthError ? _LoadState.authError : _LoadState.unreachable;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = '$e';
        _state = _LoadState.unreachable;
      });
    }
  }

  Future<void> _submitReauth(String apiKey) async {
    await _appState.updateApiKey(apiKey);
    if (!mounted) return;
    _load();
  }

  String _money(double v) => '\$${v.toStringAsFixed(2)}';

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Usage'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _state == _LoadState.loading ? null : _load,
          ),
        ],
      ),
      body: switch (_state) {
        _LoadState.loading => const Center(child: CircularProgressIndicator()),
        _LoadState.authError => Column(
            children: [
              ReauthPrompt(
                message: 'Your API key was rejected by the gateway.',
                onSubmit: _submitReauth,
              ),
            ],
          ),
        _LoadState.unreachable => Column(
            children: [
              ConnectionBanner(message: _errorMessage, onRetry: _load),
              const Expanded(
                child: Center(child: Text('No usage data available right now.')),
              ),
            ],
          ),
        _LoadState.loaded => _buildLoaded(context, _usage!),
      },
    );
  }

  Widget _buildLoaded(BuildContext context, UsageInfo usage) {
    final theme = Theme.of(context);
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '${_money(usage.monthTotalUsd)} / ${_money(usage.budgetUsd)}',
                        style: theme.textTheme.headlineSmall,
                      ),
                      if (usage.budgetExceeded)
                        Chip(
                          key: const Key('usage_budget_exceeded_chip'),
                          label: const Text('Budget exceeded'),
                          backgroundColor: theme.colorScheme.errorContainer,
                          labelStyle: TextStyle(color: theme.colorScheme.onErrorContainer),
                          side: BorderSide.none,
                        ),
                    ],
                  ),
                  const SizedBox(height: 10),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: LinearProgressIndicator(
                      value: usage.budgetUsd > 0 ? usage.budgetFraction : 0,
                      minHeight: 8,
                      backgroundColor: theme.colorScheme.surfaceContainerHighest,
                      color: usage.budgetExceeded ? theme.colorScheme.error : theme.colorScheme.primary,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    usage.budgetUsd > 0
                        ? '${usage.totalRequests} request${usage.totalRequests == 1 ? '' : 's'} this month'
                        : 'No budget configured — ${usage.totalRequests} request${usage.totalRequests == 1 ? '' : 's'} this month',
                    style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text('By model', style: theme.textTheme.titleMedium),
          const SizedBox(height: 8),
          if (usage.models.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 16),
              child: Text(
                'No requests yet this month.',
                style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
              ),
            )
          else
            for (final m in usage.models)
              Card(
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  title: Text(m.model),
                  subtitle: Text(
                    '${m.provider} · ${m.requests} req · '
                    '${m.promptTokens + m.completionTokens} tok',
                  ),
                  trailing: Text(_money(m.costUsd), style: theme.textTheme.titleSmall),
                ),
              ),
        ],
      ),
    );
  }
}
