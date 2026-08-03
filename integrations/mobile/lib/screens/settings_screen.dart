import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../app_state.dart';
import '../models/model_info.dart';
import '../services/api_client.dart';
import '../services/private_network_guard.dart';
import '../services/settings_store.dart';

/// Gateway base URL, API key, default model, and a shortcut to open the
/// gateway's own web dashboard in the browser.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _formKey = GlobalKey<FormState>();
  late TextEditingController _baseUrlCtrl;
  late TextEditingController _apiKeyCtrl;
  String _defaultModel = AppSettings.defaultModel;
  List<ModelInfo> _availableModels = const [];

  bool _obscureKey = true;
  bool _saving = false;
  bool _testing = false;
  String? _testMessage;
  bool _testOk = false;
  bool _initialized = false;

  AppState get _appState => AppStateScope.of(context);

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_initialized) {
      final settings = _appState.settings;
      _baseUrlCtrl = TextEditingController(text: settings.baseUrl);
      _apiKeyCtrl = TextEditingController(text: settings.apiKey);
      _defaultModel = settings.defaultModelId;
      _initialized = true;
      WidgetsBinding.instance.addPostFrameCallback((_) => _loadModels());
    }
  }

  @override
  void dispose() {
    _baseUrlCtrl.dispose();
    _apiKeyCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadModels() async {
    try {
      final models = await _appState.client.fetchModels();
      if (!mounted) return;
      setState(() {
        _availableModels = models;
        if (!models.any((m) => m.id == _defaultModel)) {
          _defaultModel = models.isNotEmpty ? models.first.id : AppSettings.defaultModel;
        }
      });
    } catch (_) {
      // Best-effort only: Settings must still be usable (e.g. to fix a
      // wrong base URL) when the gateway cannot currently be reached.
    }
  }

  Future<void> _testConnection() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() {
      _testing = true;
      _testMessage = null;
    });
    final client = PrismApiClient(
      baseUrl: _baseUrlCtrl.text.trim(),
      apiKey: _apiKeyCtrl.text.trim(),
    );
    try {
      final models = await client.fetchModels();
      if (!mounted) return;
      setState(() {
        _testOk = true;
        _testMessage = 'Connected — ${models.length} model${models.length == 1 ? '' : 's'} available.';
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _testOk = false;
        _testMessage = e.isAuthError ? 'Reached the gateway, but the API key was rejected.' : e.message;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _testOk = false;
        _testMessage = 'Could not connect: $e';
      });
    } finally {
      client.dispose();
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _saving = true);
    await _appState.updateSettings(
      AppSettings(
        baseUrl: _baseUrlCtrl.text.trim(),
        apiKey: _apiKeyCtrl.text.trim(),
        defaultModelId: _defaultModel,
      ),
    );
    if (!mounted) return;
    setState(() => _saving = false);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('Settings saved')),
    );
  }

  Future<void> _openDashboard() async {
    final uri = Uri.tryParse(_baseUrlCtrl.text.trim());
    if (uri == null) return;
    final launched = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!launched && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not open $uri')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final modelChoices = <String>{
      'auto',
      ..._availableModels.map((m) => m.id),
      _defaultModel,
    }.toList();

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: Form(
            key: _formKey,
            child: ListView(
              padding: const EdgeInsets.all(20),
              children: [
                TextFormField(
                  key: const Key('settings_base_url'),
                  controller: _baseUrlCtrl,
                  keyboardType: TextInputType.url,
                  decoration: const InputDecoration(
                    labelText: 'Gateway base URL',
                    hintText: 'http://192.168.1.20:8130',
                    border: OutlineInputBorder(),
                  ),
                  validator: validateGatewayBaseUrl,
                ),
                const SizedBox(height: 16),
                TextFormField(
                  key: const Key('settings_api_key'),
                  controller: _apiKeyCtrl,
                  obscureText: _obscureKey,
                  decoration: InputDecoration(
                    labelText: 'API key',
                    helperText: 'Stored in the Android Keystore',
                    border: const OutlineInputBorder(),
                    suffixIcon: IconButton(
                      icon: Icon(_obscureKey ? Icons.visibility : Icons.visibility_off),
                      onPressed: () => setState(() => _obscureKey = !_obscureKey),
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                DropdownButtonFormField<String>(
                  key: const Key('settings_default_model'),
                  initialValue: modelChoices.contains(_defaultModel) ? _defaultModel : 'auto',
                  decoration: const InputDecoration(
                    labelText: 'Default model',
                    border: OutlineInputBorder(),
                  ),
                  items: [
                    for (final id in modelChoices) DropdownMenuItem(value: id, child: Text(id)),
                  ],
                  onChanged: (v) {
                    if (v != null) setState(() => _defaultModel = v);
                  },
                ),
                const SizedBox(height: 20),
                OutlinedButton.icon(
                  onPressed: _testing ? null : _testConnection,
                  icon: _testing
                      ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.wifi_tethering),
                  label: const Text('Test connection'),
                ),
                if (_testMessage != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 10),
                    child: Container(
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: _testOk ? theme.colorScheme.tertiaryContainer : theme.colorScheme.errorContainer,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(
                        _testMessage!,
                        style: TextStyle(
                          color: _testOk
                              ? theme.colorScheme.onTertiaryContainer
                              : theme.colorScheme.onErrorContainer,
                        ),
                      ),
                    ),
                  ),
                const SizedBox(height: 12),
                OutlinedButton.icon(
                  onPressed: _openDashboard,
                  icon: const Icon(Icons.open_in_browser),
                  label: const Text('Open dashboard'),
                ),
                const SizedBox(height: 24),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton(
                    key: const Key('settings_save'),
                    onPressed: _saving ? null : _save,
                    child: _saving
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Text('Save'),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
