import 'package:flutter/material.dart';

import 'app_state.dart';
import 'screens/home_shell.dart';
import 'screens/onboarding_screen.dart';
import 'services/settings_store.dart';

/// Bootstraps the app: loads saved settings, probes the gateway, and shows
/// [OnboardingScreen] on first run or if that probe fails, otherwise the
/// bottom-nav [HomeShell]. Once past onboarding, staying reachable is each
/// screen's own job (a [ConnectionBanner] + retry) rather than bouncing the
/// user back here on every transient drop.
class AppRoot extends StatefulWidget {
  final SettingsStore? store;

  const AppRoot({super.key, this.store});

  @override
  State<AppRoot> createState() => _AppRootState();
}

class _AppRootState extends State<AppRoot> {
  late final SettingsStore _store = widget.store ?? SettingsStore();
  AppState? _appState;
  bool _checking = true;
  bool _reachable = false;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    final settings = await _store.load();
    final appState = AppState(settings: settings, store: _store);
    final reachable = settings.isConfigured && await appState.client.checkHealth();
    if (!mounted) return;
    setState(() {
      _appState = appState;
      _reachable = reachable;
      _checking = false;
    });
  }

  void _onConnected() => setState(() => _reachable = true);

  @override
  void dispose() {
    _appState?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_checking || _appState == null) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    if (!_reachable) {
      return OnboardingScreen(appState: _appState!, onConnected: _onConnected);
    }
    return AppStateScope(state: _appState!, child: const HomeShell());
  }
}
