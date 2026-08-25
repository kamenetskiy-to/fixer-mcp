import 'dart:convert';
import 'dart:io';

import 'package:fixer_dashboard_client/fixer_dashboard_client.dart' show Client;
import 'package:shared_preferences/shared_preferences.dart';

import '../dashboard_runtime_client.dart';
import '../client_order_repository.dart';
import 'workroom_models.dart';

abstract class ProjectWorkroomRepository {
  Future<ProjectWorkroomSnapshot> loadSnapshot(int projectId);

  Stream<ProjectUiFrame> watchProjectUi(
    int projectId, {
    required int afterSeq,
    int protocolVersion = projectWorkroomProtocolVersion,
  });

  Future<FixerTurnReceipt> sendFixerTurn({
    required int projectId,
    required String threadId,
    required String content,
    required String idempotencyKey,
  });

  Future<GenUiActionReceipt> requestSurface({
    required int projectId,
    required String threadId,
    required RegisteredSurfaceRequest request,
    required String idempotencyKey,
  });

  Future<GenUiActionReceipt> invokeAction({
    required int projectId,
    required String surfaceId,
    required int surfaceRevision,
    required GenUiActionDescriptor action,
    required Map<String, dynamic> input,
    required bool confirmed,
    required String idempotencyKey,
  });

  Future<HandsInstructionReceipt> submitHandsInstruction({
    required int projectId,
    required String instructionText,
    required List<String> declaredWriteScope,
    required String requestedLane,
    required String requestedModel,
    required String requestedReasoning,
    required String idempotencyKey,
  });

  Future<GenUiActionReceipt> cancelHandsInstruction({
    required int projectId,
    required String instructionId,
    required String reason,
    required String idempotencyKey,
  });

  Future<GenUiActionReceipt> selectHandsLane({
    required int projectId,
    required String provider,
    required String idempotencyKey,
  });

  Future<GenUiActionReceipt> reviewHandsInstruction({
    required int projectId,
    required String instructionId,
    required String decision,
    required String reviewNote,
    required String idempotencyKey,
  });
}

class ServerpodProjectWorkroomRepository implements ProjectWorkroomRepository {
  ServerpodProjectWorkroomRepository({
    String? serverpodBaseUrl,
    DashboardRuntimeClient? runtimeClient,
    Client? client,
    ClientSessionAuthProvider? authProvider,
  }) : _runtimeClient =
           runtimeClient ??
           DashboardRuntimeClient(serverpodBaseUrl: serverpodBaseUrl),
       _client = client ?? Client(_serverpodHost(serverpodBaseUrl)),
       _authProvider =
           authProvider ??
           ClientSessionAuthProvider(
             sessionStore: SharedPreferencesClientSessionStore(),
           ) {
    _client.authKeyProvider = _authProvider;
    _runtimeClient.authHeaderProvider ??= () => _authProvider.authHeaderValue;
  }

  final DashboardRuntimeClient _runtimeClient;
  final Client _client;
  final ClientSessionAuthProvider _authProvider;

  @override
  Future<ProjectWorkroomSnapshot> loadSnapshot(int projectId) async {
    final value = await _runtimeClient.callServerpodEndpointValue(
      'dashboardRuntime',
      'getProjectWorkroomSnapshot',
      {'projectId': projectId},
    );
    return ProjectWorkroomSnapshot.fromJson(_transportJson(value));
  }

  @override
  Stream<ProjectUiFrame> watchProjectUi(
    int projectId, {
    required int afterSeq,
    int protocolVersion = projectWorkroomProtocolVersion,
  }) async* {
    await _authProvider.initialize();
    var cursor = afterSeq;
    while (true) {
      final value = await _call('waitProjectUiEventsJson', {
        'projectId': projectId,
        'afterSeq': cursor,
        'protocolVersion': protocolVersion,
      });
      final json = _transportJson(value);
      final returnedProjectId = json['project_id'];
      final returnedProtocolVersion = json['protocol_version'];
      if (returnedProjectId != projectId ||
          returnedProtocolVersion != protocolVersion) {
        yield ProjectUiProtocolErrorFrame(
          reasonCode: 'stream_binding_invalid',
          message: 'The replay stream returned an invalid project binding.',
          minimumVersion: projectWorkroomProtocolVersion,
          maximumVersion: projectWorkroomProtocolVersion,
        );
        return;
      }
      final rawEvents = json['events'];
      if (rawEvents is! List || rawEvents.length > 100) {
        throw const WorkroomProtocolException(
          'event_batch_invalid',
          'The replay stream returned an invalid event batch.',
        );
      }
      final events = rawEvents
          .map((event) => ProjectUiEvent.fromJson(_transportJson(event)))
          .toList(growable: false);
      if (events.isNotEmpty) {
        cursor = events.last.seq;
        yield ProjectUiEventBatchFrame(events);
      } else {
        yield ProjectUiHeartbeatFrame(
          serverTime: json['server_time']?.toString() ?? '',
          journalHead: json['head_seq'] is int
              ? json['head_seq'] as int
              : cursor,
        );
      }
    }
  }

  @override
  Future<FixerTurnReceipt> sendFixerTurn({
    required int projectId,
    required String threadId,
    required String content,
    required String idempotencyKey,
  }) async {
    final value = await _call('sendFixerTurn', {
      'projectId': projectId,
      'threadId': threadId,
      'content': content,
      'idempotencyKey': idempotencyKey,
    });
    return FixerTurnReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<GenUiActionReceipt> requestSurface({
    required int projectId,
    required String threadId,
    required RegisteredSurfaceRequest request,
    required String idempotencyKey,
  }) async {
    request.validate();
    final value = await _call('requestGenuiSurface', {
      'projectId': projectId,
      'threadId': threadId,
      'surfaceType': request.surfaceType,
      'surfaceVersion': request.surfaceVersion,
      'argumentsJson': jsonEncode(request.arguments),
      'idempotencyKey': idempotencyKey,
    });
    return GenUiActionReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<GenUiActionReceipt> invokeAction({
    required int projectId,
    required String surfaceId,
    required int surfaceRevision,
    required GenUiActionDescriptor action,
    required Map<String, dynamic> input,
    required bool confirmed,
    required String idempotencyKey,
  }) async {
    final value = await _call('invokeGenuiAction', {
      'request': {
        'project_id': projectId,
        'surface_id': surfaceId,
        'surface_revision': surfaceRevision,
        'action_id': action.actionId,
        'action_version': action.actionVersion,
        'target': action.target.toJson(),
        'input': input,
        'confirmed': confirmed,
        'idempotency_key': idempotencyKey,
      },
    });
    return GenUiActionReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<HandsInstructionReceipt> submitHandsInstruction({
    required int projectId,
    required String instructionText,
    required List<String> declaredWriteScope,
    required String requestedLane,
    required String requestedModel,
    required String requestedReasoning,
    required String idempotencyKey,
  }) async {
    final value = await _call('submitHandsInstruction', {
      'request': {
        'project_id': projectId,
        'instruction_text': instructionText,
        'declared_write_scope': declaredWriteScope,
        'requested_lane': requestedLane,
        'requested_model': requestedModel,
        'requested_reasoning': requestedReasoning,
        'idempotency_key': idempotencyKey,
      },
    });
    return HandsInstructionReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<GenUiActionReceipt> cancelHandsInstruction({
    required int projectId,
    required String instructionId,
    required String reason,
    required String idempotencyKey,
  }) async {
    final value = await _call('cancelHandsInstruction', {
      'projectId': projectId,
      'instructionId': instructionId,
      'reason': reason,
      'idempotencyKey': idempotencyKey,
    });
    return GenUiActionReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<GenUiActionReceipt> selectHandsLane({
    required int projectId,
    required String provider,
    required String idempotencyKey,
  }) async {
    final value = await _call('selectHandsLane', {
      'projectId': projectId,
      'provider': provider,
      'idempotencyKey': idempotencyKey,
    });
    return GenUiActionReceipt.fromJson(_receiptJson(value));
  }

  @override
  Future<GenUiActionReceipt> reviewHandsInstruction({
    required int projectId,
    required String instructionId,
    required String decision,
    required String reviewNote,
    required String idempotencyKey,
  }) async {
    final actionId = decision == 'accept'
        ? 'hands.review.accept'
        : 'hands.review.request_changes';
    final value = await _call('invokeGenuiAction', {
      'request': {
        'project_id': projectId,
        'action_id': actionId,
        'action_version': 1,
        'target': {'type': 'hands_instruction', 'id': instructionId},
        'input': {'decision': decision, 'review_note': reviewNote},
        'confirmed': true,
        'idempotency_key': idempotencyKey,
      },
    });
    return GenUiActionReceipt.fromJson(_receiptJson(value));
  }

  Future<dynamic> _call(String method, Map<String, dynamic> payload) {
    return _runtimeClient.callServerpodEndpointValue(
      'dashboardRuntime',
      method,
      payload,
    );
  }
}

abstract class ProjectUiCursorStore {
  Future<int?> read(int projectId);

  Future<void> write(int projectId, int sequence);

  Future<void> clear(int projectId);
}

class SharedPreferencesProjectUiCursorStore implements ProjectUiCursorStore {
  SharedPreferencesProjectUiCursorStore({SharedPreferences? preferences})
    : _preferences = preferences;

  SharedPreferences? _preferences;

  Future<SharedPreferences> get _instance async {
    return _preferences ??= await SharedPreferences.getInstance();
  }

  String _key(int projectId) => 'project_workroom_cursor_v1_$projectId';

  @override
  Future<int?> read(int projectId) async {
    return (await _instance).getInt(_key(projectId));
  }

  @override
  Future<void> write(int projectId, int sequence) async {
    await (await _instance).setInt(_key(projectId), sequence);
  }

  @override
  Future<void> clear(int projectId) async {
    await (await _instance).remove(_key(projectId));
  }
}

class MemoryProjectUiCursorStore implements ProjectUiCursorStore {
  final Map<int, int> _values = <int, int>{};

  @override
  Future<int?> read(int projectId) async => _values[projectId];

  @override
  Future<void> write(int projectId, int sequence) async {
    _values[projectId] = sequence;
  }

  @override
  Future<void> clear(int projectId) async {
    _values.remove(projectId);
  }
}

Map<String, dynamic> _transportJson(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  try {
    final dynamic json = value.toJson();
    if (json is Map<String, dynamic>) return json;
    if (json is Map) return Map<String, dynamic>.from(json);
  } on Object {
    // Fall through.
  }
  throw StateError('Unexpected Serverpod workroom payload.');
}

Map<String, dynamic> _receiptJson(dynamic value) {
  final json = _transportJson(value);
  final receipt = json['receipt'] ?? json['result'];
  return receipt == null ? json : _transportJson(receipt);
}

String _serverpodHost(String? explicit) {
  final configured = explicit?.trim().isNotEmpty == true
      ? explicit!.trim()
      : Platform.environment['SERVERPOD_API_URL']?.trim().isNotEmpty == true
      ? Platform.environment['SERVERPOD_API_URL']!.trim()
      : 'http://127.0.0.1:28080';
  return configured.endsWith('/')
      ? configured.substring(0, configured.length - 1)
      : configured;
}
