import 'package:flutter/widgets.dart';

import 'services/api_client.dart';
import 'services/settings_store.dart';

/// App-wide state: the current [AppSettings] and the [PrismApiClient] built
/// from them. A single instance is created in `AppRoot` and shared through
/// [AppStateScope] to the whole bottom-nav shell, so every screen talks to
/// the same client and sees settings changes (e.g. an API key rotation
/// entered in Settings) without threading callbacks through four screens by
/// hand.
class AppState extends ChangeNotifier {
  /// [client], when supplied, is used as-is instead of building a
  /// [PrismApiClient] from [settings] — the seam widget tests use to hand
  /// this a `PrismApiClient(client: MockClient(...))` so a screen's
  /// `AppStateScope.of(context).client` calls hit a deterministic fake
  /// instead of a real socket. Production call sites never pass it.
  AppState({required AppSettings settings, required this.store, PrismApiClient? client})
      : _settings = settings,
        _client = client ?? PrismApiClient(baseUrl: settings.baseUrl, apiKey: settings.apiKey);

  final SettingsStore store;
  AppSettings _settings;
  PrismApiClient _client;

  AppSettings get settings => _settings;
  PrismApiClient get client => _client;

  /// Persists [next], rebuilds the API client against it, and notifies
  /// listeners. Every screen holding a reference to [client] should treat it
  /// as possibly-stale across an `await` and re-read [AppState.client]
  /// rather than caching it locally across a settings change.
  Future<void> updateSettings(AppSettings next) async {
    await store.save(next);
    final oldClient = _client;
    _settings = next;
    _client = PrismApiClient(baseUrl: next.baseUrl, apiKey: next.apiKey);
    oldClient.dispose();
    notifyListeners();
  }

  /// Convenience for the common "just the API key changed" case (inline
  /// reauth after a 401, or a key rotation).
  Future<void> updateApiKey(String apiKey) =>
      updateSettings(_settings.copyWith(apiKey: apiKey));

  @override
  void dispose() {
    _client.dispose();
    super.dispose();
  }
}

/// Provides an [AppState] to a subtree via [InheritedNotifier], so widgets
/// rebuild when settings/client change without an extra state-management
/// dependency (the project intentionally limits its dependency list — see
/// pubspec.yaml — so this is `InheritedNotifier`, not `provider`/`riverpod`).
class AppStateScope extends InheritedNotifier<AppState> {
  const AppStateScope({super.key, required AppState state, required super.child})
      : super(notifier: state);

  static AppState of(BuildContext context) {
    final scope = context.dependOnInheritedWidgetOfExactType<AppStateScope>();
    assert(scope != null, 'No AppStateScope found in context');
    return scope!.notifier!;
  }
}
