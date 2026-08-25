import 'dart:convert';
import 'dart:io';

typedef BackendReadyProbe = Future<bool> Function();
typedef BackendListenerIdentityProbe = Future<String?> Function();
typedef ServerpodCompatibilityClassifier =
    bool Function(int statusCode, String responseBody);
typedef BackendLauncherRunner =
    Future<ProcessResult> Function(String executable, List<String> arguments);

/// Connects Fixer Studio to the one shared local backend.
///
/// The launcher is an idempotent readiness command, not a child backend owned
/// by this app process. In particular, closing or retrying either delivery
/// channel must never terminate services used by the other channel.
class FixerStudioBackendBootstrap {
  static const serverpodCompatibilityFingerprint =
      'fixer-studio-serverpod-rpc-v1';

  static const _requiredListenerPorts = <int>[18090, 14242, 28080, 28082];

  FixerStudioBackendBootstrap({
    required this.projectRoot,
    BackendReadyProbe? readyProbe,
    BackendListenerIdentityProbe? listenerIdentityProbe,
    ServerpodCompatibilityClassifier compatibilityClassifier =
        classifyServerpodCompatibilityResponse,
    BackendLauncherRunner? launcherRunner,
    int? clientPid,
  }) : _readyProbe =
           readyProbe ??
           (() => _probeDefaultEndpoints(compatibilityClassifier)),
       _listenerIdentityProbe =
           listenerIdentityProbe ??
           (readyProbe == null
               ? _probeDefaultListenerIdentities
               : _injectedListenerIdentity),
       _launcherRunner = launcherRunner ?? _runLauncher,
       _clientPid = clientPid ?? pid;

  final String projectRoot;
  final BackendReadyProbe _readyProbe;
  final BackendListenerIdentityProbe _listenerIdentityProbe;
  final BackendLauncherRunner _launcherRunner;
  final int _clientPid;

  File get launcher =>
      File('$projectRoot/fixer_mcp/scripts/fixer_studio_backend_service.sh');

  Future<void> ensureReady() async {
    if (await _probeStableBackend()) return;
    if (projectRoot.trim().isEmpty) {
      throw StateError('The Fixer Studio project root is not configured.');
    }
    if (!launcher.existsSync()) {
      throw StateError('Backend launcher not found: ${launcher.path}');
    }

    final result = await _launcherRunner(launcher.path, ['run', '$_clientPid']);
    if (result.exitCode != 0) {
      final detail = result.stderr.toString().trim();
      throw StateError(
        detail.isEmpty
            ? 'Backend launcher exited with code ${result.exitCode}.'
            : 'Backend launcher exited with code ${result.exitCode}: $detail',
      );
    }
    if (!await _probeStableBackend()) {
      throw StateError(
        'Backend launcher exited successfully before a stable, compatible '
        'shared backend became ready.',
      );
    }
  }

  /// Compatibility is valid only for the exact listener identities that were
  /// present throughout the probe. A changed or unreadable identity is handed
  /// to the launcher, which retries under the shared startup lock.
  Future<bool> _probeStableBackend() async {
    final before = await _listenerIdentityProbe();
    if (before == null) return false;
    final ready = await _readyProbe();
    final after = await _listenerIdentityProbe();
    return ready && after != null && before == after;
  }

  static Future<ProcessResult> _runLauncher(
    String executable,
    List<String> arguments,
  ) => Process.run(
    executable,
    arguments,
    // The app can be launched from a Dock/LaunchAgent whose inherited cwd is
    // inside ~/Desktop. macOS TCC denies getcwd() there for child processes;
    // the launcher resolves the project from its executable path instead.
    workingDirectory: Directory.systemTemp.path,
  );

  static Future<String?> _injectedListenerIdentity() async => 'injected';

  static Future<String?> _probeDefaultListenerIdentities() async {
    try {
      final identities = <String>[];
      for (final port in _requiredListenerPorts) {
        final listeners = await Process.run('lsof', [
          '-nP',
          '-tiTCP:$port',
          '-sTCP:LISTEN',
        ]);
        if (listeners.exitCode != 0 && listeners.exitCode != 1) return null;
        final pids =
            listeners.stdout
                .toString()
                .split(RegExp(r'\s+'))
                .where((value) => value.isNotEmpty)
                .toSet()
                .toList()
              ..sort();
        if (pids.isEmpty) {
          identities.add('$port|none|none');
          continue;
        }
        for (final listenerPid in pids) {
          final start = await Process.run('ps', [
            '-p',
            listenerPid,
            '-o',
            'lstart=',
          ]);
          final startIdentity = start.stdout.toString().trim();
          if (start.exitCode != 0 || startIdentity.isEmpty) return null;
          identities.add('$port|$listenerPid|$startIdentity');
        }
      }
      return identities.join('\n');
    } on Object {
      return null;
    }
  }

  static Future<bool> _probeDefaultEndpoints(
    ServerpodCompatibilityClassifier compatibilityClassifier,
  ) async {
    return probeEndpoints(
      dashboardHealth: Uri.parse('http://127.0.0.1:18090/health'),
      bridgeHealth: Uri.parse('http://127.0.0.1:14242/health'),
      serverpodLiveness: Uri.parse('http://127.0.0.1:28080/livez'),
      serverpodBase: Uri.parse('http://127.0.0.1:28080/'),
      compatibilityClassifier: compatibilityClassifier,
    );
  }

  /// Proves both process liveness and the versioned Serverpod RPC fingerprint
  /// expected by this app. Every request is non-mutating: the login witness
  /// deliberately omits a required credential and the protected witnesses use
  /// no client token.
  static Future<bool> probeEndpoints({
    required Uri dashboardHealth,
    required Uri bridgeHealth,
    required Uri serverpodLiveness,
    required Uri serverpodBase,
    ServerpodCompatibilityClassifier compatibilityClassifier =
        classifyServerpodCompatibilityResponse,
  }) async {
    final client = HttpClient()
      ..connectionTimeout = const Duration(milliseconds: 500);
    try {
      for (final endpoint in <Uri>[
        dashboardHealth,
        bridgeHealth,
        serverpodLiveness,
      ]) {
        final request = await client.getUrl(endpoint);
        final response = await request.close().timeout(
          const Duration(seconds: 1),
        );
        await response.drain<void>();
        if (response.statusCode != HttpStatus.ok) return false;
      }

      final witnesses = <(String, Map<String, dynamic>)>[
        (
          'clientAuth/login',
          const {'email': 'fixer-studio-compatibility-invalid'},
        ),
        ('clientProfile/current', const {}),
        ('dashboardRuntime/health', const {}),
      ];
      for (final (path, payload) in witnesses) {
        final request = await client.postUrl(serverpodBase.resolve(path));
        request.headers.contentType = ContentType.json;
        request.write(jsonEncode(payload));
        final response = await request.close().timeout(
          const Duration(seconds: 1),
        );
        final body = await response.transform(const Utf8Decoder()).join();
        if (!compatibilityClassifier(response.statusCode, body)) return false;
      }
      return true;
    } on Object {
      return false;
    } finally {
      client.close(force: true);
    }
  }

  /// Accepts only a real JSON application response or Serverpod's documented
  /// validation/authentication failures. In particular, an unknown endpoint,
  /// malformed response, transport failure, or unclassified status is never a
  /// compatibility proof.
  static bool classifyServerpodCompatibilityResponse(
    int statusCode,
    String responseBody,
  ) {
    final body = responseBody.trim();
    if (body.toLowerCase().contains('endpoint not found')) return false;

    if (statusCode >= 200 && statusCode < 300) {
      return body.isNotEmpty && _isJson(body);
    }
    if (statusCode == HttpStatus.badRequest) {
      return _isJson(body) ||
          body.startsWith('Missing required query parameter:') ||
          body == 'Invalid JSON in body' ||
          body == 'Endpoint name is not valid' ||
          body == 'Endpoint method is not of the expected type' ||
          body.startsWith('Request has invalid "authorization" header');
    }
    if (statusCode == HttpStatus.unauthorized ||
        statusCode == HttpStatus.forbidden) {
      return body.isEmpty || _isJson(body);
    }
    return false;
  }

  static bool _isJson(String body) {
    if (body.isEmpty) return false;
    try {
      jsonDecode(body);
      return true;
    } on FormatException {
      return false;
    }
  }
}
