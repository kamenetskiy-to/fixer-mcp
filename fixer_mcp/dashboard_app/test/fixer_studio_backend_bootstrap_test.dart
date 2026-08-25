import 'dart:convert';
import 'dart:io';

import 'package:fixer_dashboard_app/src/fixer_studio_backend_bootstrap.dart';
import 'package:flutter_test/flutter_test.dart';

enum _RouteTable { current, stale }

class _ServerpodFixture {
  _ServerpodFixture._(this.server);

  final HttpServer server;
  final requests = <String>[];
  _RouteTable routeTable = _RouteTable.current;
  String? stalePath;

  Uri get base => Uri.parse('http://127.0.0.1:${server.port}/');

  static Future<_ServerpodFixture> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final fixture = _ServerpodFixture._(server);
    server.listen(fixture._handle);
    return fixture;
  }

  Future<void> _handle(HttpRequest request) async {
    final body = await utf8.decodeStream(request);
    requests.add('${request.method} ${request.uri.path} $body');

    if (request.method == 'GET' &&
        const {
          '/dashboard/health',
          '/bridge/health',
          '/livez',
        }.contains(request.uri.path)) {
      request.response
        ..statusCode = HttpStatus.ok
        ..headers.contentType = ContentType.json
        ..write('{"ok":true}');
      await request.response.close();
      return;
    }

    final stale =
        routeTable == _RouteTable.stale || stalePath == request.uri.path;
    if (request.method != 'POST' || stale) {
      request.response
        ..statusCode = HttpStatus.notFound
        ..write('Endpoint not found');
      await request.response.close();
      return;
    }

    switch (request.uri.path) {
      case '/clientAuth/login':
        request.response
          ..statusCode = HttpStatus.badRequest
          ..write('Missing required query parameter: password');
        break;
      case '/clientProfile/current':
        request.response.statusCode = HttpStatus.unauthorized;
        break;
      case '/dashboardRuntime/health':
        request.response
          ..statusCode = HttpStatus.ok
          ..headers.contentType = ContentType.json
          ..write('"ok"');
        break;
      default:
        request.response
          ..statusCode = HttpStatus.notFound
          ..write('Endpoint not found');
        break;
    }
    await request.response.close();
  }

  Future<bool> probe({
    ServerpodCompatibilityClassifier compatibilityClassifier =
        FixerStudioBackendBootstrap.classifyServerpodCompatibilityResponse,
  }) {
    return FixerStudioBackendBootstrap.probeEndpoints(
      dashboardHealth: base.resolve('dashboard/health'),
      bridgeHealth: base.resolve('bridge/health'),
      serverpodLiveness: base.resolve('livez'),
      serverpodBase: base,
      compatibilityClassifier: compatibilityClassifier,
    );
  }
}

Future<File> _writeLauncher(Directory temporary) async {
  final launcher = File(
    '${temporary.path}/fixer_mcp/scripts/fixer_studio_backend_service.sh',
  );
  await launcher.parent.create(recursive: true);
  await launcher.writeAsString('#!/bin/sh\n');
  return launcher;
}

Future<File> _writeWorkingDirectoryProbe(Directory temporary) async {
  final launcher = File(
    '${temporary.path}/fixer_mcp/scripts/fixer_studio_backend_service.sh',
  );
  final marker = File('${temporary.path}/launcher.cwd');
  await launcher.parent.create(recursive: true);
  await launcher.writeAsString('#!/bin/sh\npwd > "${marker.path}"\nexit 1\n');
  await Process.run('chmod', ['+x', launcher.path]);
  return marker;
}

void main() {
  test('compatible backend is a no-op', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'fixer-studio-backend-compatible-',
    );
    final fixture = await _ServerpodFixture.start();
    addTearDown(() => temporary.delete(recursive: true));
    addTearDown(() => fixture.server.close(force: true));
    await _writeLauncher(temporary);

    var classifierCalls = 0;
    var launcherCalls = 0;
    final bootstrap = FixerStudioBackendBootstrap(
      projectRoot: temporary.path,
      readyProbe: () => fixture.probe(
        compatibilityClassifier: (statusCode, body) {
          classifierCalls++;
          return FixerStudioBackendBootstrap.classifyServerpodCompatibilityResponse(
            statusCode,
            body,
          );
        },
      ),
      listenerIdentityProbe: () async => 'fixture|1|start',
      launcherRunner: (executable, arguments) async {
        launcherCalls++;
        return ProcessResult(1, 0, '', '');
      },
    );

    await bootstrap.ensureReady();
    await bootstrap.ensureReady();

    expect(launcherCalls, 0);
    expect(classifierCalls, 6);
    for (final path in const [
      '/clientAuth/login',
      '/clientProfile/current',
      '/dashboardRuntime/health',
    ]) {
      expect(
        fixture.requests.where((entry) => entry.contains(path)),
        hasLength(2),
      );
    }
    expect(
      fixture.requests.firstWhere(
        (entry) => entry.contains('/clientAuth/login'),
      ),
      contains('{"email":"fixer-studio-compatibility-invalid"}'),
    );
  });

  test('live but stale Serverpod is not ready', () async {
    final fixture = await _ServerpodFixture.start();
    addTearDown(() => fixture.server.close(force: true));

    for (final stalePath in const [
      '/clientAuth/login',
      '/clientProfile/current',
      '/dashboardRuntime/health',
    ]) {
      fixture.stalePath = stalePath;
      expect(
        await fixture.probe(),
        isFalse,
        reason: '$stalePath must be part of the compatibility fingerprint',
      );
    }

    fixture
      ..stalePath = null
      ..routeTable = _RouteTable.stale;
    expect(await fixture.probe(), isFalse);
    expect(
      fixture.requests,
      contains(predicate<String>((entry) => entry.startsWith('GET /livez '))),
    );
  });

  test('owned stale Serverpod reconciles and is re-probed', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'fixer-studio-backend-owned-stale-',
    );
    final fixture = await _ServerpodFixture.start();
    addTearDown(() => temporary.delete(recursive: true));
    addTearDown(() => fixture.server.close(force: true));
    await _writeLauncher(temporary);
    fixture.routeTable = _RouteTable.stale;

    var launcherCalls = 0;
    var startCalls = 0;
    var stopCalls = 0;
    final bootstrap = FixerStudioBackendBootstrap(
      projectRoot: temporary.path,
      clientPid: 4242,
      readyProbe: fixture.probe,
      listenerIdentityProbe: () async => 'fixture|1|start',
      launcherRunner: (executable, arguments) async {
        launcherCalls++;
        stopCalls++;
        startCalls++;
        fixture.routeTable = _RouteTable.current;
        return ProcessResult(1, 0, '', '');
      },
    );

    await bootstrap.ensureReady();

    expect(launcherCalls, 1);
    expect(startCalls, 1);
    expect(stopCalls, 1);
    expect(
      fixture.requests.where((entry) => entry.contains('/clientAuth/login')),
      hasLength(2),
      reason: 'the replacement must receive a fresh compatibility proof',
    );
  });

  test(
    'unowned stale Serverpod fails closed without lifecycle calls',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'fixer-studio-backend-unowned-stale-',
      );
      final fixture = await _ServerpodFixture.start();
      addTearDown(() => temporary.delete(recursive: true));
      addTearDown(() => fixture.server.close(force: true));
      await _writeLauncher(temporary);
      fixture.routeTable = _RouteTable.stale;

      var startCalls = 0;
      var stopCalls = 0;
      final bootstrap = FixerStudioBackendBootstrap(
        projectRoot: temporary.path,
        clientPid: 4242,
        readyProbe: fixture.probe,
        listenerIdentityProbe: () async => 'fixture|1|start',
        launcherRunner: (executable, arguments) async {
          return ProcessResult(
            1,
            1,
            '',
            'incompatible backend is unowned or PID-reused; refusing mutation',
          );
        },
      );

      await expectLater(
        bootstrap.ensureReady(),
        throwsA(
          isA<StateError>().having(
            (error) => error.message,
            'message',
            contains('unowned or PID-reused'),
          ),
        ),
      );
      expect(startCalls, 0);
      expect(stopCalls, 0);
      expect(fixture.routeTable, _RouteTable.stale);
    },
  );

  test(
    'listener identity change delegates the retry to the locked launcher',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'fixer-studio-backend-identity-change-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      await _writeLauncher(temporary);

      var identityCalls = 0;
      var launcherCalls = 0;
      final bootstrap = FixerStudioBackendBootstrap(
        projectRoot: temporary.path,
        readyProbe: () async => true,
        listenerIdentityProbe: () async {
          identityCalls++;
          return identityCalls == 1 ? 'old|1|start' : 'new|2|start';
        },
        launcherRunner: (executable, arguments) async {
          launcherCalls++;
          return ProcessResult(1, 0, '', '');
        },
      );

      await bootstrap.ensureReady();

      expect(launcherCalls, 1);
      expect(identityCalls, 4);
    },
  );

  test('missing backend launcher fails truthfully', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'fixer-studio-backend-missing-',
    );
    addTearDown(() => temporary.delete(recursive: true));

    final bootstrap = FixerStudioBackendBootstrap(
      projectRoot: temporary.path,
      readyProbe: () async => false,
    );

    await expectLater(
      bootstrap.ensureReady(),
      throwsA(
        isA<StateError>().having(
          (error) => error.message,
          'message',
          contains('Backend launcher not found'),
        ),
      ),
    );
  });

  test('launcher starts from a TCC-safe working directory', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'fixer-studio-backend-working-directory-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final marker = await _writeWorkingDirectoryProbe(temporary);
    final bootstrap = FixerStudioBackendBootstrap(
      projectRoot: temporary.path,
      readyProbe: () async => false,
      listenerIdentityProbe: () async => 'injected',
    );

    await expectLater(bootstrap.ensureReady(), throwsA(isA<StateError>()));
    expect(
      await marker.readAsString(),
      '${Directory(Directory.systemTemp.path).resolveSymbolicLinksSync()}\n',
    );
  });
}
