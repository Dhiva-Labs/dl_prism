import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:desktop/services/gateway_process_controller.dart';

/// Writes a tiny POSIX shell script that stands in for `prism serve` in
/// tests: it prints a couple of recognizable lines to stdout/stderr, then
/// sleeps so the test controls its lifetime (via SIGTERM) rather than the
/// script exiting on its own.
Future<File> _writeFakeServeScript(Directory dir) async {
  final file = File('${dir.path}/fake_prism.sh');
  await file.writeAsString('''
#!/bin/sh
echo "starting fake gateway"
echo "listening on 127.0.0.1:8130" 1>&2
sleep 30
''');
  await Process.run('chmod', ['+x', file.path]);
  return file;
}

void main() {
  group('buildServeArgs', () {
    test('is just "serve" when no config path is set', () {
      expect(buildServeArgs(''), ['serve']);
      expect(buildServeArgs('   '), ['serve']);
    });

    test('appends -c <path> when a config path is set', () {
      expect(buildServeArgs('/etc/prism.yaml'), ['serve', '-c', '/etc/prism.yaml']);
    });

    test('trims whitespace around the config path', () {
      expect(buildServeArgs('  /etc/prism.yaml  '), ['serve', '-c', '/etc/prism.yaml']);
    });
  });

  group('GatewayProcessController with a fake child command', () {
    late Directory tmpDir;

    setUp(() async {
      tmpDir = await Directory.systemTemp.createTemp('prism_desktop_test_');
    });

    tearDown(() async {
      if (await tmpDir.exists()) {
        await tmpDir.delete(recursive: true);
      }
    });

    test('start() spawns the process and streams stdout/stderr into logLines', () async {
      final script = await _writeFakeServeScript(tmpDir);
      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      expect(controller.isRunning, isFalse);
      await controller.start(executable: script.path, args: const []);
      expect(controller.isRunning, isTrue);
      expect(controller.pid, isNotNull);

      // Give the child a moment to write its startup lines.
      await Future.delayed(const Duration(milliseconds: 300));

      final logs = controller.logLines.join('\n');
      expect(logs, contains('starting fake gateway'));
      expect(logs, contains('[stderr] listening on 127.0.0.1:8130'));

      controller.stop();
      await Future.delayed(const Duration(milliseconds: 300));
      expect(controller.isRunning, isFalse);
    });

    test('stop() terminates a running child and it cannot be stopped twice', () async {
      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      await controller.start(executable: '/bin/sleep', args: const ['30']);
      expect(controller.isRunning, isTrue);

      final stopped = controller.stop();
      expect(stopped, isTrue);

      await Future.delayed(const Duration(milliseconds: 300));
      expect(controller.isRunning, isFalse);
      expect(controller.logLines.join('\n'), contains('exited with code'));

      // Calling stop again when nothing is running must be a safe no-op —
      // this controller must never attempt to signal a process it doesn't
      // own.
      expect(controller.stop(), isFalse);
    });

    test('a crashing/self-exiting child returns the controller to not-running', () async {
      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      // `false` is a standard POSIX utility that exits immediately with a
      // non-zero status, simulating a gateway crash on startup.
      await controller.start(executable: '/bin/false', args: const []);
      expect(controller.isRunning, isTrue);

      await Future.delayed(const Duration(milliseconds: 300));
      expect(controller.isRunning, isFalse);
      expect(controller.logLines.join('\n'), contains('exited with code'));
    });

    test('starting twice without stopping throws', () async {
      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      await controller.start(executable: '/bin/sleep', args: const ['30']);
      expect(() => controller.start(executable: '/bin/sleep', args: const ['30']),
          throwsStateError);

      controller.stop();
      await Future.delayed(const Duration(milliseconds: 300));
    });

    test('log ring buffer is capped at maxLogLines', () async {
      final script = File('${tmpDir.path}/spam.sh');
      await script.writeAsString('''
#!/bin/sh
i=0
while [ \$i -lt 600 ]; do
  echo "line \$i"
  i=\$((i+1))
done
''');
      await Process.run('chmod', ['+x', script.path]);

      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      await controller.start(executable: script.path, args: const []);

      // Wait for the script to finish emitting and exit on its own.
      var waited = 0;
      while (controller.isRunning && waited < 5000) {
        await Future.delayed(const Duration(milliseconds: 100));
        waited += 100;
      }

      expect(controller.logLines.length, lessThanOrEqualTo(GatewayProcessController.maxLogLines));
      expect(controller.logLines.last, contains('exited with code'));
    });

    test('starting a nonexistent executable throws and leaves controller stopped', () async {
      final controller = GatewayProcessController();
      addTearDown(controller.dispose);

      await expectLater(
        controller.start(executable: '/no/such/binary/prism', args: const []),
        throwsA(anything),
      );
      expect(controller.isRunning, isFalse);
    });
  });
}
