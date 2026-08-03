import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

/// Builds the argument list for `prism serve`, appending `-c <configPath>`
/// only when a config path was actually provided.
List<String> buildServeArgs(String configPath) {
  final trimmed = configPath.trim();
  return [
    'serve',
    if (trimmed.isNotEmpty) ...['-c', trimmed],
  ];
}

/// Owns the lifecycle of a `prism serve` child process spawned by this app.
///
/// This controller only ever tracks a process it started itself with
/// [start]. It never attaches to, and therefore never has the ability to
/// kill, a gateway instance that happens to already be running externally —
/// that case is a UI-level decision (see the home screen), based on whether
/// [isRunning] is false while the gateway still answers `/healthz`.
class GatewayProcessController extends ChangeNotifier {
  static const int maxLogLines = 500;

  Process? _process;
  final List<String> _logLines = [];
  StreamSubscription<String>? _stdoutSub;
  StreamSubscription<String>? _stderrSub;

  /// True while a child process started by this controller is alive.
  bool get isRunning => _process != null;

  /// The child's pid, if running.
  int? get pid => _process?.pid;

  /// Ring buffer of the last [maxLogLines] output lines (stdout + stderr).
  List<String> get logLines => List.unmodifiable(_logLines);

  /// Starts `executable args...` as a child process and begins streaming its
  /// stdout/stderr into [logLines]. Throws if a process is already running
  /// (call [stop] first) or if the OS fails to spawn the executable.
  Future<void> start({
    required String executable,
    required List<String> args,
  }) async {
    if (_process != null) {
      throw StateError('A gateway process is already running (pid $pid)');
    }
    _addLog('\$ $executable ${args.join(' ')}');
    final Process proc;
    try {
      proc = await Process.start(executable, args, runInShell: false);
    } catch (e) {
      _addLog('Failed to start "$executable": $e');
      rethrow;
    }
    _process = proc;
    notifyListeners();

    _stdoutSub = proc.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(_addLog, onError: (Object e) => _addLog('[stdout error] $e'));
    _stderrSub = proc.stderr
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen((line) => _addLog('[stderr] $line'),
            onError: (Object e) => _addLog('[stderr error] $e'));

    unawaited(proc.exitCode.then(_handleExit));
  }

  void _handleExit(int code) {
    _addLog('Gateway process exited with code $code');
    _stdoutSub?.cancel();
    _stderrSub?.cancel();
    _stdoutSub = null;
    _stderrSub = null;
    _process = null;
    notifyListeners();
  }

  /// Sends SIGTERM to the child process this controller started.
  /// Returns false (and does nothing) if no such process is running —
  /// in particular, it will never signal a process this controller did
  /// not spawn.
  bool stop() {
    final proc = _process;
    if (proc == null) return false;
    _addLog('Stopping gateway (SIGTERM)...');
    proc.kill(ProcessSignal.sigterm);
    return true;
  }

  void _addLog(String line) {
    _logLines.add(line);
    if (_logLines.length > maxLogLines) {
      _logLines.removeRange(0, _logLines.length - maxLogLines);
    }
    notifyListeners();
  }

  @override
  void dispose() {
    _stdoutSub?.cancel();
    _stderrSub?.cancel();
    // Best effort: if we still own a child when the app is disposed, ask it
    // to terminate. We never do this for processes we didn't start.
    _process?.kill(ProcessSignal.sigterm);
    super.dispose();
  }
}
