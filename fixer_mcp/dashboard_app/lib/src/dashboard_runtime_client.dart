import 'dart:convert';
import 'dart:io';

class DashboardRuntimeClient {
  DashboardRuntimeClient({
    this.dashboardBaseUrl,
    this.serverpodBaseUrl,
    HttpClient? httpClient,
  }) : _httpClient = httpClient ?? HttpClient();

  final String? dashboardBaseUrl;
  final String? serverpodBaseUrl;
  final HttpClient _httpClient;

  Future<Map<String, dynamic>> readDashboardJson(String path) async {
    final request = await _httpClient.getUrl(_resolveDashboardUri(path));
    return _sendJson(request, path);
  }

  Future<Map<String, dynamic>> postDashboardJson(
    String path,
    Map<String, dynamic> payload,
  ) async {
    final request = await _httpClient.postUrl(_resolveDashboardUri(path));
    request.headers.contentType = ContentType.json;
    request.write(jsonEncode(payload));
    return _sendJson(request, path);
  }

  Future<Map<String, dynamic>> callServerpodEndpoint(
    String endpoint,
    String method,
    Map<String, dynamic> payload,
  ) async {
    final path = '/$endpoint/$method';
    final request = await _httpClient.postUrl(_resolveServerpodUri(path));
    request.headers.contentType = ContentType.json;
    request.write(jsonEncode(payload));
    final decoded = await _sendJson(request, path);
    final result = decoded['result'];
    if (result is Map<String, dynamic>) {
      return result;
    }
    if (result is Map) {
      return Map<String, dynamic>.from(result);
    }
    return decoded;
  }

  Future<Map<String, dynamic>> _sendJson(
    HttpClientRequest request,
    String path,
  ) async {
    final response = await request.close();
    final responseBody = await utf8.decodeStream(response);
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw StateError(
        'Dashboard backend ${response.statusCode}: $responseBody',
      );
    }
    final decoded = jsonDecode(responseBody);
    if (decoded is Map<String, dynamic>) {
      return decoded;
    }
    if (decoded is Map) {
      return Map<String, dynamic>.from(decoded);
    }
    throw StateError('Unexpected backend payload for $path');
  }

  Uri _resolveServerpodUri(String path) {
    return Uri.parse('${_serverpodBase()}$path');
  }

  Uri _resolveDashboardUri(String path) {
    return Uri.parse('${_dashboardBase()}$path');
  }

  String _serverpodBase() {
    return _normalizeBase(
      serverpodBaseUrl,
      Platform.environment['SERVERPOD_API_URL'],
      'http://127.0.0.1:28080',
    );
  }

  String _dashboardBase() {
    return _normalizeBase(
      dashboardBaseUrl,
      Platform.environment['FIXER_DASHBOARD_API_BASE_URL'],
      'http://127.0.0.1:8090',
    );
  }

  String _normalizeBase(
    String? explicit,
    String? environment,
    String fallback,
  ) {
    final rawBase = explicit?.trim().isNotEmpty == true
        ? explicit!.trim()
        : environment?.trim().isNotEmpty == true
        ? environment!.trim()
        : fallback;
    return rawBase.endsWith('/')
        ? rawBase.substring(0, rawBase.length - 1)
        : rawBase;
  }
}
