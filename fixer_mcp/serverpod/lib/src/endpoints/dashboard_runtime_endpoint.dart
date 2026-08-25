import 'dart:convert';
import 'dart:io';

import 'package:fixer_dashboard_server/src/auth/client_auth_middleware.dart';
import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_bridge_client.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_codec.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_stream.dart';
import 'package:serverpod/serverpod.dart' hide Order;

import '../codex_runtime/codex_thread_service.dart';

class DashboardRuntimeEndpoint extends ClientProtectedEndpoint {
  final HttpClient _httpClient = HttpClient()..findProxy = (_) => 'DIRECT';

  static const _architectWorkroomEmail = 'architect@example.com';

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

  /// Returns one SQLite-consistent read model and the exact journal watermark
  /// from which the client starts its replay stream.
  Future<ProjectWorkroomSnapshot> getProjectWorkroomSnapshot(
    Session session,
    int projectId,
  ) async {
    final principal = await _requireProjectPrincipal(session, projectId);
    final bridge = WorkroomBridgeClient();
    try {
      final json = await bridge.getJson(
        '/internal/v1/projects/$projectId/workroom/snapshot',
        principal,
        maxResponseBytes: workroomSnapshotByteLimit,
      );
      final snapshot = decodeProjectWorkroomSnapshot(json);
      if (snapshot.projectId != projectId ||
          snapshot.protocolVersion != workroomProtocolVersion) {
        throw const WorkroomBridgeFailure(
          'snapshot_protocol_mismatch',
          'The snapshot project or protocol binding is invalid.',
        );
      }
      return snapshot;
    } on WorkroomBridgeFailure catch (failure) {
      throw _publicBridgeException(failure);
    } finally {
      bridge.close(force: true);
    }
  }

  /// Replays the authoritative journal after [afterSeq], then long-tails it.
  /// The pump holds at most one bounded Go batch when the WebSocket listener
  /// pauses and force-closes its active HTTP wait on cancellation.
  Stream<ProjectUiFrame> watchProjectUi(
    Session session,
    int projectId,
    int afterSeq,
    int protocolVersion,
  ) {
    WorkroomBridgeClient? bridge;
    WorkroomBridgePrincipal? currentPrincipal;
    var cleanedUp = false;
    void closeBridge(Session _) => bridge?.close(force: true);
    void cleanup() {
      if (cleanedUp) return;
      cleanedUp = true;
      session.removeWillCloseListener(closeBridge);
      bridge?.close(force: true);
    }

    session.addWillCloseListener(closeBridge);
    return WorkroomStreamPump().open(
      projectId: projectId,
      afterSeq: afterSeq,
      protocolVersion: protocolVersion,
      authorize: () async {
        currentPrincipal = await _requireProjectPrincipal(session, projectId);
      },
      fetchBatch: (cursor) async {
        final principal = currentPrincipal;
        if (principal == null) {
          throw const WorkroomBridgeFailure(
            'authorization_state_missing',
            'The stream authorization state is unavailable.',
          );
        }
        final activeBridge = bridge ??= WorkroomBridgeClient();
        final json = await activeBridge.getJson(
          '/internal/v1/projects/$projectId/events',
          principal,
          queryParameters: {
            'after_seq': cursor.toString(),
            'limit': workroomEventBatchLimit.toString(),
            'wait_ms': workroomJournalWaitMilliseconds.toString(),
          },
        );
        return decodeWorkroomJournalBatch(json);
      },
      cancelWait: cleanup,
      onClosed: cleanup,
    );
  }

  Future<FixerTurnReceipt> sendFixerTurn(
    Session session,
    int projectId,
    String threadId,
    String content,
    String idempotencyKey,
  ) async {
    return _withProjectBridge(session, projectId, (bridge, principal) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/$projectId/fixer/turns',
        principal,
        {
          'thread_id': threadId,
          'content': content,
          'idempotency_key': idempotencyKey,
        },
      );
      return decodeFixerTurnReceipt(json);
    });
  }

  Future<GenuiActionReceipt> requestGenuiSurface(
    Session session,
    int projectId,
    String threadId,
    String surfaceType,
    int surfaceVersion,
    String argumentsJson,
    String idempotencyKey,
  ) async {
    final arguments = _decodeJsonObject(
      argumentsJson,
      reasonCode: 'invalid_surface_arguments',
      message: 'Surface arguments must be a JSON object.',
    );
    return _withProjectBridge(session, projectId, (bridge, principal) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/$projectId/genui/surfaces',
        principal,
        {
          'thread_id': threadId,
          'surface_type': surfaceType,
          'surface_version': surfaceVersion,
          'arguments': arguments,
          'provider': 'fixer',
          'model': 'governed-intent-router',
          'idempotency_key': idempotencyKey,
        },
      );
      return decodeGenuiActionReceipt(json);
    });
  }

  /// Bounded long-poll fallback for clients whose generated protocol artifact
  /// does not yet include the typed Workroom stream. Journal sequence remains
  /// authoritative, so reconnect resumes from the caller's committed cursor.
  Future<Map<String, dynamic>> waitProjectUiEventsJson(
    Session session,
    int projectId,
    int afterSeq,
    int protocolVersion,
  ) async {
    if (afterSeq < 0 || protocolVersion != workroomProtocolVersion) {
      throw WorkroomBridgeException(
        reasonCode: 'protocol_version_unsupported',
        message: 'The Project Workroom replay cursor is invalid.',
      );
    }
    return _withProjectBridge(session, projectId, (bridge, principal) async {
      final json = await bridge.getJson(
        '/internal/v1/projects/$projectId/events',
        principal,
        queryParameters: {
          'after_seq': afterSeq.toString(),
          'limit': workroomEventBatchLimit.toString(),
          'wait_ms': workroomJournalWaitMilliseconds.toString(),
        },
      );
      if (json['project_id'] != projectId) {
        throw const WorkroomBridgeFailure(
          'event_project_mismatch',
          'The event batch project binding is invalid.',
        );
      }
      return {
        ...json,
        'protocol_version': workroomProtocolVersion,
        'server_time': DateTime.now().toUtc().toIso8601String(),
      };
    });
  }

  Future<GenuiActionReceipt> invokeGenuiAction(
    Session session,
    GenuiActionRequest request,
  ) async {
    final input = _decodeActionInput(request.inputJson);
    return _withProjectBridge(session, request.projectId, (
      bridge,
      principal,
    ) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/${request.projectId}/genui/actions',
        principal,
        {
          'protocol_version': request.protocolVersion,
          'surface_id': request.surfaceId,
          'surface_revision': request.surfaceRevision,
          'action_id': request.actionId,
          'action_version': request.actionVersion,
          'target_type': request.targetType,
          'target_id': request.targetId,
          'input': input,
          'confirmed': request.confirmed,
          'idempotency_key': request.idempotencyKey,
        },
      );
      return decodeGenuiActionReceipt(json);
    });
  }

  Future<HandsInstructionReceipt> submitHandsInstruction(
    Session session,
    HandsInstructionRequest request,
  ) async {
    return _withProjectBridge(session, request.projectId, (
      bridge,
      principal,
    ) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/${request.projectId}/hands/instructions',
        principal,
        {
          'instruction_text': request.instructionText,
          'declared_write_scope': request.declaredWriteScope,
          'requested_lane': request.requestedLane,
          'idempotency_key': request.idempotencyKey,
        },
      );
      return decodeHandsInstructionReceipt(json);
    });
  }

  Future<GenuiActionReceipt> selectHandsLane(
    Session session,
    int projectId,
    String provider,
    String idempotencyKey,
  ) async {
    return _withProjectBridge(session, projectId, (bridge, principal) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/$projectId/hands/lane',
        principal,
        {'provider': provider, 'idempotency_key': idempotencyKey},
      );
      return decodeGenuiActionReceipt(json);
    });
  }

  Future<CommandReceipt> cancelHandsInstruction(
    Session session,
    int projectId,
    String instructionId,
    String idempotencyKey,
  ) async {
    return _withProjectBridge(session, projectId, (bridge, principal) async {
      final json = await bridge.postJson(
        '/internal/v1/projects/$projectId/hands/instructions/'
        '${Uri.encodeComponent(instructionId)}/cancel',
        principal,
        {'idempotency_key': idempotencyKey},
      );
      return decodeCommandReceipt(json);
    });
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
  ) async {
    final messages = await CodexThreadService().listMessages(session, threadId);
    return {
      'threadId': threadId.trim(),
      'availability': 'codex_app_server',
      'transcriptAvailable': true,
      'messages': [
        for (final message in messages)
          {
            'id': message.id,
            'role': message.role,
            'text': message.text,
            'turnId': message.turnId,
            'createdAt': message.createdAt.toIso8601String(),
            'source': 'serverpod_codex_message',
          },
      ],
      'sendSupported': true,
      'sendEndpoint': '/turn/start',
      'streamEndpointTemplate': '/turn/stream/{streamId}',
      'turnStatusEndpointTemplate': '/turn/status/{streamId}',
    };
  }

  Future<Map<String, dynamic>> sendThreadMessage(
    Session session,
    String threadId,
    String prompt,
    String model,
    String reasoning,
  ) {
    return CodexThreadService().startTurn(
      session,
      threadId,
      prompt,
      model: model,
      reasoning: reasoning,
    );
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
      'http://127.0.0.1:18090',
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

  Future<WorkroomBridgePrincipal> _requireProjectPrincipal(
    Session session,
    int projectId,
  ) async {
    if (projectId < 1) {
      throw NotAuthorizedException(
        reason: AuthenticationFailureReason.insufficientAccess,
      );
    }
    final client = await ClientAuthMiddleware.requireClient(session);
    final clientId = client.id!;
    final isArchitect =
        client.email.trim().toLowerCase() == _architectWorkroomEmail;
    if (isArchitect) {
      return WorkroomBridgePrincipal(
        principalId: 'client:$clientId',
        roles: const ['architect', 'project_member'],
        projectId: projectId,
        requestId: '${session.sessionId}:${const Uuid().v4()}',
      );
    }
    final membership = await Order.db.findFirstRow(
      session,
      where: (table) =>
          table.clientId.equals(clientId) &
          table.assignedProjectId.equals(projectId),
    );
    if (membership == null) {
      throw NotAuthorizedException(
        reason: AuthenticationFailureReason.insufficientAccess,
      );
    }
    return WorkroomBridgePrincipal(
      principalId: 'client:$clientId',
      roles: const ['project_member'],
      projectId: projectId,
      requestId: '${session.sessionId}:${const Uuid().v4()}',
    );
  }

  Future<T> _withProjectBridge<T>(
    Session session,
    int projectId,
    Future<T> Function(
      WorkroomBridgeClient bridge,
      WorkroomBridgePrincipal principal,
    )
    operation,
  ) async {
    final principal = await _requireProjectPrincipal(session, projectId);
    final bridge = WorkroomBridgeClient();
    try {
      return await operation(bridge, principal);
    } on WorkroomBridgeFailure catch (failure) {
      throw _publicBridgeException(failure);
    } finally {
      bridge.close(force: true);
    }
  }

  Map<String, dynamic> _decodeActionInput(String raw) {
    return _decodeJsonObject(
      raw,
      reasonCode: 'invalid_action_input',
      message: 'Action input must be a JSON object.',
    );
  }

  Map<String, dynamic> _decodeJsonObject(
    String raw, {
    required String reasonCode,
    required String message,
  }) {
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) return Map<String, dynamic>.from(decoded);
    } on FormatException {
      // Mapped to a stable client-safe error below.
    }
    throw WorkroomBridgeException(reasonCode: reasonCode, message: message);
  }

  WorkroomBridgeException _publicBridgeException(
    WorkroomBridgeFailure failure,
  ) {
    return WorkroomBridgeException(
      reasonCode: failure.reasonCode,
      message: 'Project Workroom transport failed safely.',
    );
  }
}
