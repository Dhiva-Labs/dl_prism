import 'package:flutter/material.dart';

import '../app_state.dart';
import '../services/api_client.dart';
import '../services/private_network_guard.dart';

/// First-run (or "gateway unreachable at startup") screen: base URL, API
/// key, and a "Test connection" round-trip to `/v1/models` before letting
/// the user into the app. The gateway must be listening on an address the
/// phone can actually reach — LAN IP, Tailscale/VPN address, never
/// `127.0.0.1` unless the phone and gateway share a loopback (e.g. an
/// emulator with adb reverse) — and if it listens beyond loopback it must
/// have `server.api_keys` set. See `docs/INTEGRATIONS.md` in the DL Prism
/// repo for the full setup, including how to run `prism serve` on a
/// reachable address.
class OnboardingScreen extends StatefulWidget {
  final AppState appState;
  final VoidCallback onConnected;

  const OnboardingScreen({super.key, required this.appState, required this.onConnected});

  @override
  State<OnboardingScreen> createState() => _OnboardingScreenState();
}

enum _TestResult { none, ok, authError, unreachable }

class _OnboardingScreenState extends State<OnboardingScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _baseUrlCtrl =
      TextEditingController(text: widget.appState.settings.baseUrl);
  late final TextEditingController _apiKeyCtrl =
      TextEditingController(text: widget.appState.settings.apiKey);

  bool _obscureKey = true;
  bool _testing = false;
  bool _connecting = false;
  _TestResult _result = _TestResult.none;
  String _resultMessage = '';

  @override
  void dispose() {
    _baseUrlCtrl.dispose();
    _apiKeyCtrl.dispose();
    super.dispose();
  }

  Future<void> _testConnection() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() {
      _testing = true;
      _result = _TestResult.none;
    });
    final client = PrismApiClient(
      baseUrl: _baseUrlCtrl.text.trim(),
      apiKey: _apiKeyCtrl.text.trim(),
    );
    try {
      final models = await client.fetchModels();
      if (!mounted) return;
      setState(() {
        _result = _TestResult.ok;
        _resultMessage =
            'Connected — ${models.length} model${models.length == 1 ? '' : 's'} available.';
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _result = e.isAuthError ? _TestResult.authError : _TestResult.unreachable;
        _resultMessage = e.message;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _result = _TestResult.unreachable;
        _resultMessage = 'Could not connect: $e';
      });
    } finally {
      client.dispose();
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _connect() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _connecting = true);
    final settings = widget.appState.settings.copyWith(
      baseUrl: _baseUrlCtrl.text.trim(),
      apiKey: _apiKeyCtrl.text.trim(),
    );
    await widget.appState.updateSettings(settings);
    if (!mounted) return;
    setState(() => _connecting = false);
    widget.onConnected();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Connect to DL Prism')),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 480),
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Form(
                key: _formKey,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.change_history, size: 48, color: theme.colorScheme.primary),
                    const SizedBox(height: 12),
                    Text('Find your gateway', style: theme.textTheme.headlineSmall),
                    const SizedBox(height: 8),
                    Text(
                      'DL Prism Mobile talks to a gateway running elsewhere on your '
                      'network — a computer on your LAN, or reachable over Tailscale/VPN. '
                      'The gateway is not on this phone. It must listen on an address this '
                      'phone can reach, and if that address is anything other than '
                      'loopback, it needs server.api_keys set in its config. See '
                      'docs/INTEGRATIONS.md in the DL Prism repo for setup.',
                      style: theme.textTheme.bodyMedium
                          ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                    ),
                    const SizedBox(height: 24),
                    TextFormField(
                      key: const Key('onboarding_base_url'),
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
                      key: const Key('onboarding_api_key'),
                      controller: _apiKeyCtrl,
                      obscureText: _obscureKey,
                      decoration: InputDecoration(
                        labelText: 'API key',
                        helperText: 'Required if the gateway has server.api_keys set',
                        border: const OutlineInputBorder(),
                        suffixIcon: IconButton(
                          icon: Icon(_obscureKey ? Icons.visibility : Icons.visibility_off),
                          onPressed: () => setState(() => _obscureKey = !_obscureKey),
                        ),
                      ),
                    ),
                    const SizedBox(height: 20),
                    OutlinedButton.icon(
                      onPressed: _testing ? null : _testConnection,
                      icon: _testing
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(strokeWidth: 2))
                          : const Icon(Icons.wifi_tethering),
                      label: const Text('Test connection'),
                    ),
                    if (_result != _TestResult.none) ...[
                      const SizedBox(height: 12),
                      Container(
                        key: const Key('onboarding_result'),
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: _result == _TestResult.ok
                              ? theme.colorScheme.tertiaryContainer
                              : theme.colorScheme.errorContainer,
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Row(
                          children: [
                            Icon(
                              _result == _TestResult.ok ? Icons.check_circle_outline : Icons.error_outline,
                              color: _result == _TestResult.ok
                                  ? theme.colorScheme.onTertiaryContainer
                                  : theme.colorScheme.onErrorContainer,
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                _result == _TestResult.authError
                                    ? 'Reached the gateway, but the API key was rejected: $_resultMessage'
                                    : _resultMessage,
                                style: TextStyle(
                                  color: _result == _TestResult.ok
                                      ? theme.colorScheme.onTertiaryContainer
                                      : theme.colorScheme.onErrorContainer,
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                    const SizedBox(height: 24),
                    SizedBox(
                      width: double.infinity,
                      child: FilledButton(
                        key: const Key('onboarding_continue'),
                        onPressed: (_result == _TestResult.ok && !_connecting) ? _connect : null,
                        child: _connecting
                            ? const SizedBox(
                                width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                            : const Text('Continue'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
