import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Thin seam over Android's Keystore-backed secure storage.
///
/// Everything that needs the protected store talks to this instead of
/// `FlutterSecureStorage` directly, so tests can substitute an in-memory
/// fake instead of the real backend, which needs a platform channel and
/// does not run under plain `flutter test`.
///
/// [read] and [delete] fail closed (return null / do nothing) on a platform
/// error: a secret that cannot be read is, to every caller in this app,
/// indistinguishable from one that was never saved, so the safe direction is
/// to re-prompt for the API key rather than guess. [write] is the one method
/// that must not be treated as fire-and-forget: it can throw
/// [SecureStorageUnavailableException] if the Keystore refuses the write.
abstract class SecureStorageBackend {
  Future<void> write(String key, String value);
  Future<String?> read(String key);
  Future<void> delete(String key);
}

/// Thrown by [FlutterSecureStorageBackend.write] when the Keystore could not
/// protect the secret (e.g. no device credential/PIN set on some emulator
/// images, or a corrupted keystore entry). There is no plaintext fallback
/// anywhere near this type: its purpose is to make that failure visible
/// instead of silently storing the API key unprotected.
class SecureStorageUnavailableException implements Exception {
  const SecureStorageUnavailableException(this.message, {this.code});

  final String message;
  final String? code;

  @override
  String toString() => message;
}

/// Production backend: Android Keystore-backed `EncryptedSharedPreferences`.
class FlutterSecureStorageBackend implements SecureStorageBackend {
  // No AndroidOptions override needed: flutter_secure_storage's Android
  // backend is Keystore-backed by default (EncryptedSharedPreferences is
  // deprecated as of the underlying Jetpack Security library and the plugin
  // now migrates existing data to its replacement cipher automatically).
  FlutterSecureStorageBackend() : _storage = const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  @override
  Future<void> write(String key, String value) async {
    try {
      await _storage.write(key: key, value: value);
    } on PlatformException catch (e) {
      throw SecureStorageUnavailableException(
        'The device keystore refused to save the API key'
        '${(e.code).isEmpty ? '' : ' (${e.code})'}. Try again, or check '
        'that a device PIN/pattern/password is set.',
        code: e.code.isEmpty ? null : e.code,
      );
    }
  }

  @override
  Future<String?> read(String key) async {
    try {
      return await _storage.read(key: key);
    } on PlatformException {
      return null;
    }
  }

  @override
  Future<void> delete(String key) async {
    try {
      await _storage.delete(key: key);
    } on PlatformException {
      // Best-effort: nothing to clean up if the store cannot be reached.
    }
  }
}
