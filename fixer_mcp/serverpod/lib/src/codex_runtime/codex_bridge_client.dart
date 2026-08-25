import 'dart:async';
import 'dart:convert';
import 'dart:io';

Uri? _bridgeBaseUrlOverrideForTesting;

Uri resolveCodexBridgeBaseUrl() {
  final override = _bridgeBaseUrlOverrideForTesting;
  if (override != null) return override;
  final raw =
      Platform.environment['CODEX_BRIDGE_URL'] ?? 'http://127.0.0.1:14242';
  return Uri.parse(raw);
}

void setCodexBridgeBaseUrlOverrideForTesting(Uri? baseUrl) {
  _bridgeBaseUrlOverrideForTesting = baseUrl;
}

class CodexBridgeClient {
  CodexBridgeClient({required this.baseUrl});

  final Uri baseUrl;

  HttpClient _newHttpClient() {
    final httpClient = HttpClient();
    // Prevent environment/system proxy settings from hijacking localhost calls.
    httpClient.findProxy = (_) => 'DIRECT';
    return httpClient;
  }

  Future<T> _retry<T>(Future<T> Function() fn) async {
    // codex-bridge might still be starting right after a backend restart.
    const attempts = 25;
    for (var i = 0; i < attempts; i++) {
      try {
        return await fn();
      } on SocketException {
        if (i == attempts - 1) rethrow;
        await Future<void>.delayed(const Duration(milliseconds: 200));
      }
    }
    // Unreachable
    return await fn();
  }

  Future<List<String>> listMcpServers() async {
    return _retry(() async {
      final httpClient = _newHttpClient();
      try {
        final uri = baseUrl.replace(path: '/mcp/servers');
        final req = await httpClient.getUrl(uri);
        final resp = await req.close();
        final respBody = await resp.transform(utf8.decoder).join();
        if (resp.statusCode < 200 || resp.statusCode >= 300) {
          throw HttpException(
            'codex-bridge GET /mcp/servers failed: ${resp.statusCode} $respBody',
            uri: uri,
          );
        }
        final decoded = jsonDecode(respBody);
        if (decoded is! Map<String, dynamic>) {
          throw const FormatException('Expected JSON object response');
        }
        final servers = decoded['servers'];
        if (servers is! List) return const [];
        return servers
            .whereType<String>()
            .map((s) => s.trim())
            .where((s) => s.isNotEmpty)
            .toList();
      } finally {
        httpClient.close(force: true);
      }
    });
  }

  Future<Map<String, dynamic>> mcpOptions({
    String? cwd,
    String? mcpProfile,
    List<String>? mcpServers,
    Map<String, dynamic>? mcpServerConfigs,
  }) async {
    return postJson('/mcp/options', {
      if (cwd != null) 'cwd': cwd,
      if (mcpProfile != null) 'mcpProfile': mcpProfile,
      if (mcpServers != null) 'mcpServers': mcpServers,
      if (mcpServerConfigs != null) 'mcpServerConfigs': mcpServerConfigs,
    });
  }

  Future<Map<String, dynamic>> modelList({
    int limit = 100,
    String? cursor,
  }) async {
    return postJson('/model/list', {'limit': limit, 'cursor': cursor});
  }

  Future<Map<String, dynamic>> postJson(
    String path,
    Map<String, dynamic> body,
  ) async {
    return _retry(() async {
      final httpClient = _newHttpClient();
      try {
        final uri = baseUrl.replace(path: path);
        final req = await httpClient.postUrl(uri);
        req.headers.contentType = ContentType.json;
        req.write(jsonEncode(body));
        final resp = await req.close();
        final respBody = await resp.transform(utf8.decoder).join();
        if (resp.statusCode < 200 || resp.statusCode >= 300) {
          throw HttpException(
            'codex-bridge POST $path failed: ${resp.statusCode} $respBody',
            uri: uri,
          );
        }
        final decoded = jsonDecode(respBody);
        if (decoded is! Map<String, dynamic>) {
          throw const FormatException('Expected JSON object response');
        }
        return decoded;
      } finally {
        httpClient.close(force: true);
      }
    });
  }

  Future<Map<String, dynamic>> readThread({required String threadId}) {
    return postJson('/thread/read', {'threadId': threadId});
  }

  Future<Stream<String>> getSseLines(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    final httpClient = _newHttpClient();
    final uri = baseUrl.replace(path: path, queryParameters: queryParameters);
    final req = await httpClient.getUrl(uri);
    req.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
    final resp = await req.close();
    if (resp.statusCode < 200 || resp.statusCode >= 300) {
      final respBody = await resp.transform(utf8.decoder).join();
      httpClient.close(force: true);
      throw HttpException(
        'codex-bridge SSE $path failed: ${resp.statusCode} $respBody',
        uri: uri,
      );
    }

    // Close client when consumer cancels.
    final controller = StreamController<String>(sync: true);
    late final StreamSubscription<String> sub;
    sub = resp
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(
          controller.add,
          onError: controller.addError,
          onDone: () => controller.close(),
          cancelOnError: false,
        );
    controller.onCancel = () async {
      await sub.cancel();
      httpClient.close(force: true);
    };
    return controller.stream;
  }

  Future<Map<String, dynamic>> applyThreadMcp({
    required String threadId,
    required List<String> mcpServers,
    String? cwd,
    Map<String, dynamic>? mcpServerConfigs,
    Map<String, dynamic>? promptProfile,
  }) async {
    return postJson('/thread/mcp/apply', {
      'threadId': threadId,
      'cwd': cwd,
      // Explicit allowlist. Empty list => disable all.
      'mcpServers': mcpServers,
      if (mcpServerConfigs != null) 'mcpServerConfigs': mcpServerConfigs,
      if (promptProfile != null) ...promptProfile,
    });
  }

  Future<Map<String, dynamic>> listSkills({
    required List<String> cwds,
    bool forceReload = false,
  }) async {
    return postJson('/skills/list', {'cwds': cwds, 'forceReload': forceReload});
  }

  Future<Map<String, dynamic>> interruptTurn({
    required String threadId,
    required String turnId,
  }) async {
    return postJson('/turn/interrupt', {
      'threadId': threadId,
      'turnId': turnId,
    });
  }

  Future<Map<String, dynamic>> startThreadCompact({
    required String threadId,
  }) async {
    return postJson('/thread/compact/start', {'threadId': threadId});
  }

  Future<Map<String, dynamic>> readAccountRateLimits() async {
    return postJson('/account/rate-limits/read', const {});
  }
}
