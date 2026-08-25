import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

Directory _findRepositoryRoot() {
  var cursor = Directory.current.absolute;
  while (true) {
    final dispatcher = File('${cursor.path}/scripts/fixer_studio_channels');
    final app = Directory('${cursor.path}/fixer_mcp/dashboard_app');
    if (dispatcher.existsSync() && app.existsSync()) {
      return cursor;
    }
    final parent = cursor.parent;
    if (parent.path == cursor.path) {
      throw StateError('Could not locate the repository root.');
    }
    cursor = parent;
  }
}

void main() {
  late Directory repository;

  setUpAll(() {
    repository = _findRepositoryRoot();
  });

  test(
    'channel scripts are syntactically valid and pass their self-test',
    () async {
      const scripts = <String>[
        'scripts/fixer_studio_channels',
        'scripts/install_fixer_studio_channels.sh',
        'scripts/promote_fixer_studio_stable.sh',
        'scripts/verify_fixer_studio_channels.sh',
        'fixer_mcp/scripts/fixer_studio_backend_service.sh',
      ];

      for (final relativePath in scripts) {
        final syntax = await Process.run('/bin/bash', [
          '-n',
          relativePath,
        ], workingDirectory: repository.path);
        expect(
          syntax.exitCode,
          0,
          reason: '$relativePath failed bash -n:\n${syntax.stderr}',
        );
      }

      final selfTest = await Process.run('/bin/bash', [
        'scripts/fixer_studio_channels',
        'self-test',
      ], workingDirectory: repository.path);
      expect(selfTest.exitCode, 0, reason: selfTest.stderr.toString());
      expect(selfTest.stdout, contains('self-test: PASS'));
      expect(selfTest.stdout, contains('post-swap rollback boundaries'));
      expect(
        selfTest.stdout,
        contains('strong distinct-process lock identity'),
      );

      final backendSelfTest = await Process.run('/bin/bash', [
        'fixer_mcp/scripts/fixer_studio_backend_service.sh',
        'self-test',
      ], workingDirectory: repository.path);
      expect(
        backendSelfTest.exitCode,
        0,
        reason: backendSelfTest.stderr.toString(),
      );
      expect(
        backendSelfTest.stdout,
        contains('stale owned listener reconciles once for concurrent clients'),
      );
      expect(
        backendSelfTest.stdout,
        contains('stale unowned listener is never stopped'),
      );
      expect(
        backendSelfTest.stdout,
        contains(
          'client close and retry preserve the shared compatible listener',
        ),
      );
      expect(
        backendSelfTest.stdout,
        contains(
          'new Experimental fingerprint preserves Stable-supported routes',
        ),
      );
    },
    timeout: const Timeout(Duration(minutes: 2)),
  );

  test('checked-in macOS metadata identifies Experimental distinctly', () {
    final appInfo = File(
      '${repository.path}/fixer_mcp/dashboard_app/macos/Runner/Configs/AppInfo.xcconfig',
    ).readAsStringSync();
    final infoPlist = File(
      '${repository.path}/fixer_mcp/dashboard_app/macos/Runner/Info.plist',
    ).readAsStringSync();

    expect(appInfo, contains('PRODUCT_NAME = Fixer Studio Experimental'));
    expect(
      appInfo,
      contains(
        'PRODUCT_BUNDLE_IDENTIFIER = '
        'dev.fixermcp.fixerStudio.experimental',
      ),
    );
    expect(appInfo, contains('FIXER_STUDIO_CHANNEL = experimental'));
    expect(infoPlist, contains('<key>CFBundleDisplayName</key>'));
    expect(infoPlist, contains('<key>FixerStudioChannel</key>'));
  });

  test('Makefile exposes the complete governed channel lifecycle', () {
    final makefile = File(
      '${repository.path}/fixer_mcp/Makefile',
    ).readAsStringSync();

    for (final target in const <String>[
      'studio-channels-bootstrap:',
      'studio-experimental-refresh:',
      'studio-stable-promote:',
      'studio-channels-verify:',
      'studio-updater-uninstall:',
    ]) {
      expect(makefile, contains(target));
    }
    expect(makefile, contains('/Applications/Fixer Studio Stable.app'));
    expect(makefile, contains('/Applications/Fixer Studio Experimental.app'));

    final rootMakefile = File('${repository.path}/Makefile').readAsStringSync();
    final backendLauncher = File(
      '${repository.path}/fixer_mcp/scripts/'
      'fixer_studio_backend_service.sh',
    ).readAsStringSync();
    expect(rootMakefile, contains('start-server:'));
    expect(rootMakefile, contains('run-server: stop-server'));
    expect(backendLauncher, contains('make_backend start-server'));
    for (final witness in const <String>[
      '/clientAuth/login',
      '/clientProfile/current',
      '/dashboardRuntime/health',
    ]) {
      expect(backendLauncher, contains(witness));
    }
    expect(backendLauncher, isNot(contains('/fixer-studio/contract')));
    expect(backendLauncher, contains('make_backend stop-server'));
    expect(
      backendLauncher.indexOf('partial_listeners_are_owned ||'),
      lessThan(backendLauncher.indexOf('make_backend stop-server')),
      reason: 'lifecycle stop must follow exact PID/start ownership proof',
    );
  });
}
