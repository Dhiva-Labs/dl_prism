import 'package:flutter/material.dart';

import '../services/prism_config_writer.dart';
import '../services/settings_store.dart';

/// Settings form. Pops with the saved [AppSettings] on success, or `null`
/// if the user cancels/backs out without saving.
class SettingsScreen extends StatefulWidget {
  final AppSettings initial;
  final SettingsStore store;

  const SettingsScreen({super.key, required this.initial, required this.store});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _baseUrlCtrl;
  late final TextEditingController _apiKeyCtrl;
  late final TextEditingController _binaryPathCtrl;
  late final TextEditingController _configPathCtrl;
  late final TextEditingController _ollamaBaseUrlCtrl;
  late final TextEditingController _prismConfigPathCtrl;
  bool _obscureKey = true;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _baseUrlCtrl = TextEditingController(text: widget.initial.baseUrl);
    _apiKeyCtrl = TextEditingController(text: widget.initial.apiKey);
    _binaryPathCtrl = TextEditingController(text: widget.initial.binaryPath);
    _configPathCtrl = TextEditingController(text: widget.initial.configPath);
    _ollamaBaseUrlCtrl = TextEditingController(text: widget.initial.ollamaBaseUrl);
    _prismConfigPathCtrl = TextEditingController(text: widget.initial.prismConfigPath);
  }

  @override
  void dispose() {
    _baseUrlCtrl.dispose();
    _apiKeyCtrl.dispose();
    _binaryPathCtrl.dispose();
    _configPathCtrl.dispose();
    _ollamaBaseUrlCtrl.dispose();
    _prismConfigPathCtrl.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _saving = true);
    final settings = AppSettings(
      baseUrl: _baseUrlCtrl.text.trim(),
      apiKey: _apiKeyCtrl.text.trim(),
      binaryPath: _binaryPathCtrl.text.trim().isEmpty
          ? AppSettings.defaultBinaryPath
          : _binaryPathCtrl.text.trim(),
      configPath: _configPathCtrl.text.trim(),
      ollamaBaseUrl: _ollamaBaseUrlCtrl.text.trim().isEmpty
          ? AppSettings.defaultOllamaBaseUrl
          : _ollamaBaseUrlCtrl.text.trim(),
      prismConfigPath: _prismConfigPathCtrl.text.trim(),
    );
    await widget.store.save(settings);
    if (!mounted) return;
    Navigator.of(context).pop(settings);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Form(
              key: _formKey,
              child: ListView(
                shrinkWrap: true,
                children: [
                  TextFormField(
                    controller: _baseUrlCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Gateway base URL',
                      hintText: 'http://127.0.0.1:8130',
                      border: OutlineInputBorder(),
                    ),
                    validator: (v) {
                      final value = v?.trim() ?? '';
                      if (value.isEmpty) return 'Base URL is required';
                      final uri = Uri.tryParse(value);
                      if (uri == null || !uri.hasScheme) {
                        return 'Enter a valid URL, e.g. http://127.0.0.1:8130';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    controller: _apiKeyCtrl,
                    obscureText: _obscureKey,
                    decoration: InputDecoration(
                      labelText: 'API key (optional)',
                      border: const OutlineInputBorder(),
                      suffixIcon: IconButton(
                        icon: Icon(
                            _obscureKey ? Icons.visibility : Icons.visibility_off),
                        onPressed: () => setState(() => _obscureKey = !_obscureKey),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    controller: _binaryPathCtrl,
                    decoration: const InputDecoration(
                      labelText: 'prism binary path',
                      hintText: 'prism',
                      helperText: 'Executable used for "Start gateway" (default: prism on PATH)',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    controller: _configPathCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Config file path (optional)',
                      hintText: '/path/to/prism.yaml',
                      helperText: 'Passed as -c <path> when starting the gateway',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    controller: _ollamaBaseUrlCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Ollama base URL',
                      hintText: 'http://127.0.0.1:11434',
                      helperText: 'Used by the Local models card',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    controller: _prismConfigPathCtrl,
                    decoration: InputDecoration(
                      labelText: 'Gateway config file (optional)',
                      hintText: defaultPrismConfigPath(),
                      helperText:
                          'Where the Providers card writes connections. Defaults to '
                          '~/.config/prism/prism.yaml',
                      border: const OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 24),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      TextButton(
                        onPressed: _saving ? null : () => Navigator.of(context).pop(),
                        child: const Text('Cancel'),
                      ),
                      const SizedBox(width: 8),
                      FilledButton(
                        onPressed: _saving ? null : _save,
                        child: const Text('Save'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
