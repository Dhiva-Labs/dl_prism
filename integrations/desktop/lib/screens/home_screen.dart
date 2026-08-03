import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../config/build_variant.dart';
import '../data/provider_presets.dart';
import '../models/gateway_status.dart';
import '../models/ollama_model.dart';
import '../models/pull_progress.dart';
import '../models/usage_info.dart';
import '../services/api_client.dart';
import '../services/dashboard_launcher.dart';
import '../services/gateway_process_controller.dart';
import '../services/ollama_client.dart';
import '../services/prism_config_writer.dart';
import '../services/settings_store.dart';
import '../widgets/gateway_card.dart';
import '../widgets/local_models_card.dart';
import '../widgets/providers_card.dart';
import '../widgets/providers_list.dart';
import '../widgets/usage_card.dart';
import 'settings_screen.dart';

const _ollamaInstallUrl = 'https://ollama.com/download';

/// The single "control tower" screen: gateway health, usage, providers,
/// and process controls. Polls `/healthz` (and, when reachable,
/// `/admin/status` + `/admin/usage`) every 5 seconds.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  static const _pollInterval = Duration(seconds: 5);

  final SettingsStore _store = SettingsStore();
  final GatewayProcessController _controller = GatewayProcessController();
  final DashboardLauncher _dashboardLauncher = DashboardLauncher();

  PrismApiClient? _api;
  OllamaClient? _ollama;
  AppSettings _settings = const AppSettings();
  Timer? _pollTimer;

  bool _loadingSettings = true;
  bool _reachable = false;
  GatewayStatus? _status;
  UsageInfo? _usage;
  bool _starting = false;
  String? _startError;

  // Local models (Ollama) state.
  bool _ollamaReachable = false;
  List<OllamaModel> _installedModels = const [];
  Set<String> _loadedModelNames = const {};
  final Set<String> _busyModelNames = {};
  String? _pullingModel;
  PullProgress? _pullProgress;
  String? _pullError;
  StreamSubscription<PullProgress>? _pullSub;

  // Providers (gateway config) state.
  PrismConfigWriter? _configWriter;
  Set<String> _connectedProviders = const {};
  final Set<String> _busyProviderNames = {};

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onControllerChanged);
    _init();
  }

  Future<void> _init() async {
    final settings = await _store.load();
    if (!mounted) return;
    setState(() {
      _settings = settings;
      _loadingSettings = false;
    });
    _rebuildApiClient();
    unawaited(_poll());
    unawaited(_pollOllama());
    unawaited(_pollProviders());
    _pollTimer = Timer.periodic(_pollInterval, (_) {
      _poll();
      _pollOllama();
      _pollProviders();
    });
  }

  void _rebuildApiClient() {
    _api?.dispose();
    _api = PrismApiClient(baseUrl: _settings.baseUrl, apiKey: _settings.apiKey);
    _ollama?.dispose();
    _ollama = OllamaClient(baseUrl: _settings.ollamaBaseUrl);
    _configWriter = PrismConfigWriter(path: resolvePrismConfigPath(_settings.prismConfigPath));
  }

  void _onControllerChanged() {
    if (!mounted) return;
    // Refresh promptly when our child exits/crashes so the UI doesn't wait
    // out the rest of the poll interval to notice.
    if (!_controller.isRunning) {
      unawaited(_poll());
    }
    setState(() {});
  }

  Future<void> _poll() async {
    final api = _api;
    if (api == null) return;
    final reachable = await api.checkHealth();
    if (!mounted) return;
    if (!reachable) {
      setState(() {
        _reachable = false;
        _status = null;
        _usage = null;
      });
      return;
    }
    try {
      final status = await api.fetchStatus();
      final usage = await api.fetchUsage();
      if (!mounted) return;
      setState(() {
        _reachable = true;
        _status = status;
        _usage = usage;
      });
    } on ApiException {
      // healthz answered but the admin endpoints didn't (e.g. mid-restart);
      // keep the last known status/usage rather than flashing empty state.
      if (!mounted) return;
      setState(() => _reachable = true);
    }
  }

  Future<void> _startGateway() async {
    setState(() {
      _starting = true;
      _startError = null;
    });
    try {
      final binary =
          _settings.binaryPath.trim().isEmpty ? AppSettings.defaultBinaryPath : _settings.binaryPath.trim();
      await _controller.start(
        executable: binary,
        args: buildServeArgs(_settings.configPath),
      );
      // Poll quickly for a few seconds while the gateway boots so the card
      // updates without waiting for the full 5s tick.
      for (var i = 0; i < 10; i++) {
        await Future.delayed(const Duration(milliseconds: 500));
        if (!mounted) return;
        await _poll();
        if (_reachable) break;
      }
    } catch (e) {
      if (!mounted) return;
      setState(() => _startError = 'Failed to start gateway: $e');
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  void _stopGateway() => _controller.stop();

  Future<void> _pollOllama() async {
    final ollama = _ollama;
    if (ollama == null) return;
    final reachable = await ollama.checkHealth();
    if (!mounted) return;
    if (!reachable) {
      setState(() {
        _ollamaReachable = false;
        _installedModels = const [];
        _loadedModelNames = const {};
      });
      return;
    }
    try {
      final installed = await ollama.fetchInstalledModels();
      final loaded = await ollama.fetchLoadedModels();
      if (!mounted) return;
      setState(() {
        _ollamaReachable = true;
        _installedModels = installed;
        _loadedModelNames = loaded.map((m) => m.name).toSet();
      });
    } on OllamaException {
      if (!mounted) return;
      setState(() => _ollamaReachable = true);
    }
  }

  Future<void> _installOllama() async {
    final uri = Uri.parse(_ollamaInstallUrl);
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    }
  }

  Future<void> _runModel(String name) async {
    final ollama = _ollama;
    if (ollama == null) return;
    setState(() => _busyModelNames.add(name));
    try {
      await ollama.loadModel(name);
    } catch (_) {
      // Surfaced implicitly: the model just won't show as "loaded" after
      // the refresh below. Keeping this UI-simple matches the rest of the
      // app's light-touch error handling for background refreshes.
    } finally {
      if (mounted) setState(() => _busyModelNames.remove(name));
    }
    await _pollOllama();
  }

  Future<void> _stopModel(String name) async {
    final ollama = _ollama;
    if (ollama == null) return;
    setState(() => _busyModelNames.add(name));
    try {
      await ollama.unloadModel(name);
    } catch (_) {
      // See _runModel.
    } finally {
      if (mounted) setState(() => _busyModelNames.remove(name));
    }
    await _pollOllama();
  }

  Future<void> _deleteModel(String name) async {
    final ollama = _ollama;
    if (ollama == null) return;
    setState(() => _busyModelNames.add(name));
    try {
      await ollama.deleteModel(name);
    } catch (_) {
      // See _runModel.
    } finally {
      if (mounted) setState(() => _busyModelNames.remove(name));
    }
    await _pollOllama();
  }

  void _startPull(String name) {
    final ollama = _ollama;
    if (ollama == null || _pullingModel != null) return;
    setState(() {
      _pullingModel = name;
      _pullProgress = null;
      _pullError = null;
    });
    _pullSub = ollama.pullModel(name).listen(
      (progress) {
        if (!mounted) return;
        setState(() => _pullProgress = progress);
      },
      onError: (Object e) {
        if (!mounted) return;
        setState(() => _pullError = e.toString());
      },
      onDone: () {
        if (!mounted) return;
        setState(() {
          _pullingModel = null;
          _pullProgress = null;
        });
        unawaited(_pollOllama());
      },
    );
  }

  void _cancelPull() {
    _pullSub?.cancel();
    _pullSub = null;
    setState(() {
      _pullingModel = null;
      _pullProgress = null;
      _pullError = null;
    });
  }

  Future<void> _pollProviders() async {
    final writer = _configWriter;
    if (writer == null) return;
    final names = await writer.connectedProviderNames();
    if (!mounted) return;
    setState(() => _connectedProviders = names);
  }

  Future<void> _connectProvider(ProviderPreset preset, String apiKey) async {
    final writer = _configWriter;
    if (writer == null) return;
    setState(() => _busyProviderNames.add(preset.name));
    try {
      await writer.connectProvider(preset: preset, apiKey: apiKey);
      await _pollProviders();
      if (!mounted) return;
      _notifyConfigChanged('Connected ${preset.displayName}.');
    } on PrismConfigException catch (e) {
      if (!mounted) return;
      _showError(e.message);
    } finally {
      if (mounted) setState(() => _busyProviderNames.remove(preset.name));
    }
  }

  Future<void> _disconnectProvider(String name) async {
    final writer = _configWriter;
    if (writer == null) return;
    setState(() => _busyProviderNames.add(name));
    try {
      await writer.disconnectProvider(name);
      await _pollProviders();
      if (!mounted) return;
      _notifyConfigChanged('Disconnected $name.');
    } on PrismConfigException catch (e) {
      if (!mounted) return;
      _showError(e.message);
    } finally {
      if (mounted) setState(() => _busyProviderNames.remove(name));
    }
  }

  /// Tells the user their config change needs a gateway restart to take
  /// effect, offering a one-click Restart when we can safely do it
  /// ourselves (i.e. the gateway is our own child process) — restarting a
  /// gateway we didn't start would violate the "never touch a process we
  /// didn't spawn" rule the process controller otherwise guarantees.
  void _notifyConfigChanged(String message) {
    final messenger = ScaffoldMessenger.of(context);
    messenger.hideCurrentSnackBar();
    if (_controller.isRunning) {
      messenger.showSnackBar(
        SnackBar(
          content: Text('$message Restart the gateway to apply.'),
          action: SnackBarAction(label: 'Restart', onPressed: () => unawaited(_restartGateway())),
        ),
      );
    } else {
      messenger.showSnackBar(
        SnackBar(content: Text('$message Restart the gateway to apply.')),
      );
    }
  }

  void _showError(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  /// Stops and restarts the gateway child process this app started, so a
  /// just-written config change is picked up. Only ever acts on a process
  /// we own — see [GatewayProcessController].
  Future<void> _restartGateway() async {
    if (!_controller.isRunning) return;
    _controller.stop();
    var waited = 0;
    while (_controller.isRunning && waited < 5000) {
      await Future.delayed(const Duration(milliseconds: 100));
      waited += 100;
    }
    if (!mounted) return;
    await _startGateway();
  }

  Future<void> _openSettings() async {
    final result = await Navigator.of(context).push<AppSettings>(
      MaterialPageRoute(
        builder: (_) => SettingsScreen(initial: _settings, store: _store),
      ),
    );
    if (result == null) return;
    setState(() => _settings = result);
    _rebuildApiClient();
    unawaited(_poll());
    unawaited(_pollOllama());
    unawaited(_pollProviders());
  }

  Future<void> _openDashboard() async {
    if (!_reachable) {
      _showError('Start the gateway first, then open the dashboard.');
      return;
    }
    final result = await _dashboardLauncher.open(_settings.baseUrl);
    if (!mounted) return;
    switch (result) {
      case DashboardOpenResult.openedEmbedded:
      case DashboardOpenResult.openedBrowser:
        break; // success, nothing to say
      case DashboardOpenResult.openedBrowserAfterEmbeddedFailure:
        _showError('Couldn\'t open the embedded dashboard — opened it in your browser instead.');
      case DashboardOpenResult.invalidUrl:
        _showError('The gateway base URL in settings isn\'t valid.');
      case DashboardOpenResult.failed:
        _showError(
          kEmbedDashboard
              ? 'Couldn\'t open the dashboard (embedded window and browser both failed).'
              : 'Couldn\'t open the dashboard in your browser.',
        );
    }
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _pullSub?.cancel();
    _controller.removeListener(_onControllerChanged);
    _controller.dispose();
    _api?.dispose();
    _ollama?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_loadingSettings) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    return Scaffold(
      appBar: AppBar(
        title: const Text('DL Prism Desktop'),
        actions: [
          TextButton.icon(
            onPressed: _openDashboard,
            icon: const Icon(Icons.open_in_new, size: 18),
            label: const Text('Open dashboard'),
          ),
          const SizedBox(width: 8),
          IconButton(
            tooltip: 'Settings',
            icon: const Icon(Icons.settings_outlined),
            onPressed: _openSettings,
          ),
          const SizedBox(width: 8),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => Future.wait([_poll(), _pollOllama(), _pollProviders()]),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            GatewayCard(
              reachable: _reachable,
              status: _status,
              controller: _controller,
              starting: _starting,
              startError: _startError,
              onStart: _startGateway,
              onStop: _stopGateway,
              onOpenDashboard: _openDashboard,
            ),
            const SizedBox(height: 16),
            UsageCard(usage: _usage),
            const SizedBox(height: 16),
            ProvidersList(providers: _status?.providers ?? const []),
            const SizedBox(height: 16),
            ProvidersCard(
              connectedNames: _connectedProviders,
              busyNames: _busyProviderNames,
              onConnect: _connectProvider,
              onDisconnect: _disconnectProvider,
            ),
            const SizedBox(height: 16),
            LocalModelsCard(
              reachable: _ollamaReachable,
              installedModels: _installedModels,
              loadedModelNames: _loadedModelNames,
              busyModelNames: _busyModelNames,
              pullingModel: _pullingModel,
              pullProgress: _pullProgress,
              pullError: _pullError,
              onRetry: () => unawaited(_pollOllama()),
              onInstallOllama: () => unawaited(_installOllama()),
              onRun: (name) => unawaited(_runModel(name)),
              onStop: (name) => unawaited(_stopModel(name)),
              onDelete: (name) => unawaited(_deleteModel(name)),
              onPull: _startPull,
              onCancelPull: _cancelPull,
            ),
          ],
        ),
      ),
    );
  }
}
