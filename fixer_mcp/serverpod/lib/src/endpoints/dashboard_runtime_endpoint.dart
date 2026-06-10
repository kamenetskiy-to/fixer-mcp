import 'dart:convert';
import 'dart:io';

import 'package:serverpod/serverpod.dart';

class DashboardRuntimeEndpoint extends Endpoint {
  final HttpClient _httpClient = HttpClient()..findProxy = (_) => 'DIRECT';

  Future<String> health(Session session) async {
    return 'ok';
  }

  Future<List<String>> topology(Session session) async {
    return const [
      'go:dashboard_api',
      'serverpod:fixer_dashboard_server',
      'node:node_bridge',
    ];
  }

  Future<Map<String, dynamic>> homeSnapshot(Session session) {
    return _getDashboardApiJson('/api/home');
  }

  Future<Map<String, dynamic>> projectSnapshot(Session session, int projectId) {
    return _getDashboardApiJson('/api/projects/$projectId/snapshot');
  }

  Future<Map<String, dynamic>> projectDocs(Session session, int projectId) {
    return _getDashboardApiJson('/api/projects/$projectId/docs');
  }

  Future<Map<String, dynamic>> threadBinding(Session session, int projectId) {
    return _getDashboardApiJson('/api/projects/$projectId/fixer-chat-binding');
  }

  Future<Map<String, dynamic>> sessionDetail(Session session, int sessionId) {
    return _getDashboardApiJson('/api/sessions/$sessionId');
  }

  Future<Map<String, dynamic>> threadMessages(
    Session session,
    String threadId,
  ) {
    return _getNodeBridgeJson('/thread/messages/read', {'threadId': threadId});
  }

  Future<Map<String, dynamic>> sendThreadMessage(
    Session session,
    String threadId,
    String prompt,
  ) {
    return _postNodeBridgeJson('/turn/start', {
      'threadId': threadId,
      'prompt': prompt,
    }, retryConnectionDrop: false);
  }

  Future<Map<String, dynamic>> threadTurnStatus(
    Session session,
    String streamId,
  ) {
    return _readNodeBridgeJson('/turn/status/${Uri.encodeComponent(streamId)}');
  }

  Future<Map<String, dynamic>> _getDashboardApiJson(String path) async {
    return _readJson(_dashboardApiUri(path));
  }

  Future<Map<String, dynamic>> _getNodeBridgeJson(
    String path,
    Map<String, dynamic> payload,
  ) async {
    return _postNodeBridgeJson(path, payload, retryConnectionDrop: true);
  }

  Future<Map<String, dynamic>> _postNodeBridgeJson(
    String path,
    Map<String, dynamic> payload, {
    required bool retryConnectionDrop,
  }) async {
    try {
      final request = await _httpClient.postUrl(_nodeBridgeUri(path));
      _prepareNodeBridgeRequest(request);
      request.write(jsonEncode(payload));
      final response = await request.close();
      return _decodeJsonResponse(response, path);
    } on HttpException catch (error) {
      if (!retryConnectionDrop || !_isConnectionDrop(error)) {
        rethrow;
      }
      final request = await _httpClient.postUrl(_nodeBridgeUri(path));
      _prepareNodeBridgeRequest(request);
      request.write(jsonEncode(payload));
      final response = await request.close();
      return _decodeJsonResponse(response, path);
    }
  }

  void _prepareNodeBridgeRequest(HttpClientRequest request) {
    request.headers.contentType = ContentType.json;
    request.headers.set(HttpHeaders.connectionHeader, 'close');
    request.persistentConnection = false;
  }

  Future<Map<String, dynamic>> _readNodeBridgeJson(String path) async {
    try {
      final request = await _httpClient.getUrl(_nodeBridgeUri(path));
      _prepareNodeBridgeRequest(request);
      final response = await request.close();
      return _decodeJsonResponse(response, path);
    } on HttpException catch (error) {
      if (!_isConnectionDrop(error)) {
        rethrow;
      }
      final request = await _httpClient.getUrl(_nodeBridgeUri(path));
      _prepareNodeBridgeRequest(request);
      final response = await request.close();
      return _decodeJsonResponse(response, path);
    }
  }

  Future<Map<String, dynamic>> _readJson(Uri uri) async {
    final response = await (await _httpClient.getUrl(uri)).close();
    return _decodeJsonResponse(response, uri.path);
  }

  Future<Map<String, dynamic>> _decodeJsonResponse(
    HttpClientResponse response,
    String path,
  ) async {
    final body = await utf8.decodeStream(response);
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw StateError('Backend $path returned ${response.statusCode}: $body');
    }
    final decoded = jsonDecode(body);
    if (decoded is Map<String, dynamic>) {
      return decoded;
    }
    if (decoded is Map) {
      return Map<String, dynamic>.from(decoded);
    }
    throw StateError('Backend $path returned a non-object JSON payload.');
  }

  Uri _dashboardApiUri(String path) {
    final base = _baseUrl(
      Platform.environment['FIXER_DASHBOARD_API_BASE_URL'],
      'http://127.0.0.1:8090',
    );
    return Uri.parse('$base$path');
  }

  Uri _nodeBridgeUri(String path) {
    final base = _baseUrl(
      Platform.environment['CODEX_BRIDGE_URL'],
      'http://127.0.0.1:14242',
    );
    return Uri.parse('$base$path');
  }

  String _baseUrl(String? raw, String fallback) {
    final value = raw?.trim().isNotEmpty == true ? raw!.trim() : fallback;
    return value.endsWith('/') ? value.substring(0, value.length - 1) : value;
  }

  bool _isConnectionDrop(HttpException error) {
    final message = error.message.toLowerCase();
    return message.contains('connection reset by peer') ||
        message.contains('connection closed before full header was received');
  }
}
