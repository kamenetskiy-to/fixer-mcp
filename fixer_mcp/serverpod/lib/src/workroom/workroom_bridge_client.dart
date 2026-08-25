import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

const workroomProtocolVersion = 1;
const workroomEventBatchLimit = 100;
const workroomPendingEventLimit = 256;
const workroomPendingByteLimit = 1024 * 1024;
const workroomSnapshotByteLimit = 8 * 1024 * 1024;
const workroomJournalWaitMilliseconds = 20000;

/// Authenticated principal information Serverpod forwards to the private Go
/// authority. Roles are derived at the Serverpod boundary, never from a client
/// request body.
class WorkroomBridgePrincipal {
  const WorkroomBridgePrincipal({
    required this.principalId,
    required this.roles,
    required this.projectId,
    required this.requestId,
  });

  final String principalId;
  final List<String> roles;
  final int projectId;
  final String requestId;
}

/// Internal transport failure. Endpoint code maps these failures to safe
/// serializable errors or protocol frames and never forwards a raw body.
class WorkroomBridgeFailure implements Exception {
  const WorkroomBridgeFailure(this.reasonCode, this.message, {this.statusCode});

  final String reasonCode;
  final String message;
  final int? statusCode;

  @override
  String toString() => '$reasonCode: $message';
}

/// HMAC-authenticated, bounded loopback client for the authoritative Go
/// Project Workroom bridge.
class WorkroomBridgeClient {
  WorkroomBridgeClient({
    HttpClient? httpClient,
    Uri? baseUri,
    String? secret,
    String? serviceId,
    DateTime Function()? now,
  }) : _httpClient = httpClient ?? (HttpClient()..findProxy = (_) => 'DIRECT'),
       _baseUri = baseUri ?? _environmentBaseUri(),
       _secret = secret ?? _requiredEnvironment('FIXER_WORKROOM_BRIDGE_SECRET'),
       _serviceId = _nonEmpty(
         serviceId ?? Platform.environment['FIXER_WORKROOM_BRIDGE_SERVICE_ID'],
         'serverpod',
       ),
       _now = now ?? DateTime.now;

  final HttpClient _httpClient;
  final Uri _baseUri;
  final String _secret;
  final String _serviceId;
  final DateTime Function() _now;

  Future<Map<String, dynamic>> getJson(
    String path,
    WorkroomBridgePrincipal principal, {
    Map<String, String> queryParameters = const {},
    int maxResponseBytes = workroomPendingByteLimit,
  }) {
    return _requestJson(
      method: 'GET',
      path: path,
      principal: principal,
      queryParameters: queryParameters,
      bodyBytes: Uint8List(0),
      maxResponseBytes: maxResponseBytes,
    );
  }

  Future<Map<String, dynamic>> postJson(
    String path,
    WorkroomBridgePrincipal principal,
    Map<String, dynamic> body,
  ) {
    return _requestJson(
      method: 'POST',
      path: path,
      principal: principal,
      bodyBytes: Uint8List.fromList(utf8.encode(jsonEncode(body))),
      maxResponseBytes: workroomPendingByteLimit,
    );
  }

  Future<Map<String, dynamic>> _requestJson({
    required String method,
    required String path,
    required WorkroomBridgePrincipal principal,
    required Uint8List bodyBytes,
    required int maxResponseBytes,
    Map<String, String> queryParameters = const {},
  }) async {
    if (!path.startsWith('/') || principal.projectId < 1) {
      throw const WorkroomBridgeFailure(
        'invalid_bridge_request',
        'The bridge path or project binding is invalid.',
      );
    }
    final base = _baseUri.toString().replaceFirst(RegExp(r'/$'), '');
    final uri = Uri.parse('$base$path').replace(
      queryParameters: queryParameters.isEmpty ? null : queryParameters,
    );
    final request = method == 'GET'
        ? await _httpClient.getUrl(uri)
        : await _httpClient.postUrl(uri);
    request.headers.contentType = ContentType.json;
    request.headers.set(HttpHeaders.connectionHeader, 'close');
    request.persistentConnection = false;
    _sign(request, method, uri, principal, bodyBytes);
    if (bodyBytes.isNotEmpty) {
      request.add(bodyBytes);
    }

    final response = await request.close();
    final responseBytes = BytesBuilder(copy: false);
    var length = 0;
    await for (final chunk in response) {
      length += chunk.length;
      if (length > maxResponseBytes) {
        throw const WorkroomBridgeFailure(
          'response_too_large',
          'The private bridge response exceeded the stream memory bound.',
        );
      }
      responseBytes.add(chunk);
    }
    final responseBody = utf8.decode(responseBytes.takeBytes());
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw WorkroomBridgeFailure(
        'bridge_rejected',
        'The Go authority rejected the signed request.',
        statusCode: response.statusCode,
      );
    }
    final decoded = jsonDecode(responseBody);
    if (decoded is! Map) {
      throw const WorkroomBridgeFailure(
        'invalid_bridge_response',
        'The private bridge returned a non-object JSON payload.',
      );
    }
    return Map<String, dynamic>.from(decoded);
  }

  void _sign(
    HttpClientRequest request,
    String method,
    Uri uri,
    WorkroomBridgePrincipal principal,
    Uint8List bodyBytes,
  ) {
    final roles =
        principal.roles
            .map((role) => role.trim().toLowerCase())
            .where((role) => role.isNotEmpty)
            .toSet()
            .toList()
          ..sort();
    final issuedAt = _now().toUtc().millisecondsSinceEpoch ~/ 1000;
    final expiresAt = issuedAt + 60;
    final bodyHash = sha256.convert(bodyBytes).toString();
    final canonical = <String>[
      method.toUpperCase(),
      uri.path,
      uri.query,
      _serviceId,
      principal.principalId,
      roles.join(','),
      principal.projectId.toString(),
      principal.requestId,
      issuedAt.toString(),
      expiresAt.toString(),
      bodyHash,
    ].join('\n');
    final signature = Hmac(
      sha256,
      utf8.encode(_secret),
    ).convert(utf8.encode(canonical)).toString();

    request.headers
      ..set('X-Workroom-Service', _serviceId)
      ..set('X-Workroom-Principal', principal.principalId)
      ..set('X-Workroom-Roles', roles.join(','))
      ..set('X-Workroom-Project-ID', principal.projectId.toString())
      ..set('X-Workroom-Request-ID', principal.requestId)
      ..set('X-Workroom-Issued-At', issuedAt.toString())
      ..set('X-Workroom-Expires-At', expiresAt.toString())
      ..set('X-Workroom-Body-SHA256', bodyHash)
      ..set('X-Workroom-Signature', signature);
  }

  void close({bool force = false}) => _httpClient.close(force: force);

  static Uri _environmentBaseUri() {
    final raw = _nonEmpty(
      Platform.environment['FIXER_DASHBOARD_API_BASE_URL'],
      'http://127.0.0.1:18090',
    );
    final uri = Uri.tryParse(raw);
    if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
      throw const WorkroomBridgeFailure(
        'invalid_bridge_configuration',
        'FIXER_DASHBOARD_API_BASE_URL is invalid.',
      );
    }
    return uri;
  }

  static String _requiredEnvironment(String key) {
    final value = Platform.environment[key]?.trim();
    if (value == null || value.isEmpty) {
      throw WorkroomBridgeFailure('bridge_not_configured', '$key is required.');
    }
    return value;
  }

  static String _nonEmpty(String? value, String fallback) {
    final normalized = value?.trim();
    return normalized == null || normalized.isEmpty ? fallback : normalized;
  }
}
