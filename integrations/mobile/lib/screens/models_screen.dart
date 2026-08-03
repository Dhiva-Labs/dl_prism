import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models/model_info.dart';
import '../services/api_client.dart';
import '../widgets/connection_banner.dart';
import '../widgets/model_list_tile.dart';
import '../widgets/reauth_prompt.dart';

/// Registry list (`GET /v1/models`): every model the gateway can route to,
/// with local/cloud, context window and capability chips.
class ModelsScreen extends StatefulWidget {
  const ModelsScreen({super.key});

  @override
  State<ModelsScreen> createState() => _ModelsScreenState();
}

enum _LoadState { loading, loaded, unreachable, authError }

class _ModelsScreenState extends State<ModelsScreen> {
  _LoadState _state = _LoadState.loading;
  List<ModelInfo> _models = const [];
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
      final models = await _appState.client.fetchModels();
      if (!mounted) return;
      setState(() {
        _models = models;
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Models'),
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
        _LoadState.authError => ReauthPrompt(
            message: 'Your API key was rejected by the gateway.',
            onSubmit: _submitReauth,
          ),
        _LoadState.unreachable => Column(
            children: [
              ConnectionBanner(message: _errorMessage, onRetry: _load),
              const Expanded(
                child: Center(child: Text('No model list available right now.')),
              ),
            ],
          ),
        _LoadState.loaded => _buildLoaded(context),
      },
    );
  }

  Widget _buildLoaded(BuildContext context) {
    final registryModels = _models.where((m) => !m.isAuto).toList(growable: false);
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        key: const Key('models_list'),
        padding: const EdgeInsets.symmetric(vertical: 8),
        children: [
          const Card(
            margin: EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            child: ListTile(
              leading: Icon(Icons.alt_route),
              title: Text('auto'),
              subtitle: Text('Automatic routing via policy rules'),
            ),
          ),
          if (registryModels.isEmpty)
            const Padding(
              padding: EdgeInsets.all(24),
              child: Center(child: Text('No models registered on this gateway yet.')),
            )
          else
            for (final model in registryModels) ModelListTile(model: model),
        ],
      ),
    );
  }
}
