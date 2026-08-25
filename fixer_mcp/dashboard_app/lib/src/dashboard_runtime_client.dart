import 'dart:convert';
import 'dart:io';

class DashboardRuntimeClient {
  DashboardRuntimeClient({
    this.dashboardBaseUrl,
    this.serverpodBaseUrl,
    this.authHeaderProvider,
    HttpClient? httpClient,
  }) : _httpClient = httpClient ?? HttpClient();

  final String? dashboardBaseUrl;
  final String? serverpodBaseUrl;
  Future<String?> Function()? authHeaderProvider;
  final HttpClient _httpClient;

  static const defaultDashboardBaseUrl = 'http://127.0.0.1:18090';

  Future<Map<String, dynamic>> readDashboardJson(String path) async {
    final request = await _httpClient.getUrl(_resolveDashboardUri(path));
    await _authorize(request);
    return _sendJson(request, path);
  }

  Future<Map<String, dynamic>> postDashboardJson(
    String path,
    Map<String, dynamic> payload,
  ) async {
    final request = await _httpClient.postUrl(_resolveDashboardUri(path));
    await _authorize(request);
    request.headers.contentType = ContentType.json;
    request.write(jsonEncode(payload));
    return _sendJson(request, path);
  }

  Future<Map<String, dynamic>> callServerpodEndpoint(
    String endpoint,
    String method,
    Map<String, dynamic> payload,
  ) async {
    final result = await callServerpodEndpointValue(endpoint, method, payload);
    if (result is Map<String, dynamic>) {
      return result;
    }
    if (result is Map) {
      return Map<String, dynamic>.from(result);
    }
    throw StateError('Unexpected Serverpod payload for $endpoint/$method');
  }

  Future<dynamic> callServerpodEndpointValue(
    String endpoint,
    String method,
    Map<String, dynamic> payload,
  ) async {
    final path = '/$endpoint/$method';
    final request = await _httpClient.postUrl(_resolveServerpodUri(path));
    await _authorize(request);
    request.headers.contentType = ContentType.json;
    request.write(jsonEncode(payload));
    final decoded = await _sendJson(request, path);
    if (!decoded.containsKey('result')) {
      return decoded;
    }
    return decoded['result'];
  }

  Future<void> _authorize(HttpClientRequest request) async {
    final value = await authHeaderProvider?.call();
    if (value != null && value.trim().isNotEmpty) {
      request.headers.set(HttpHeaders.authorizationHeader, value.trim());
    }
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
      defaultDashboardBaseUrl,
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
