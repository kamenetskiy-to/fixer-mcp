import 'dart:async';

import 'package:flutter/foundation.dart';

import 'workroom_models.dart';
import 'workroom_repository.dart';

typedef WorkroomReconnectDelay = Future<void> Function(Duration duration);

@immutable
class ProjectWorkroomState {
  const ProjectWorkroomState({
    required this.snapshot,
    required this.connectionStatus,
    required this.lastAppliedSeq,
    required this.connectionMessage,
    required this.surfaceError,
  });

  final ProjectWorkroomSnapshot snapshot;
  final WorkroomConnectionStatus connectionStatus;
  final int lastAppliedSeq;
  final String connectionMessage;
  final String surfaceError;

  ProjectWorkroomState copyWith({
    ProjectWorkroomSnapshot? snapshot,
    WorkroomConnectionStatus? connectionStatus,
    int? lastAppliedSeq,
    String? connectionMessage,
    String? surfaceError,
    bool clearConnectionMessage = false,
    bool clearSurfaceError = false,
  }) {
    return ProjectWorkroomState(
      snapshot: snapshot ?? this.snapshot,
      connectionStatus: connectionStatus ?? this.connectionStatus,
      lastAppliedSeq: lastAppliedSeq ?? this.lastAppliedSeq,
      connectionMessage: clearConnectionMessage
          ? ''
          : connectionMessage ?? this.connectionMessage,
      surfaceError: clearSurfaceError ? '' : surfaceError ?? this.surfaceError,
    );
  }
}

class ProjectWorkroomStore extends ChangeNotifier {
  ProjectWorkroomStore({
    required this.projectId,
    required this.repository,
    required this.cursorStore,
    required ProjectWorkroomSnapshot seedSnapshot,
    WorkroomReconnectDelay? reconnectDelay,
  }) : _state = ProjectWorkroomState(
         snapshot: seedSnapshot,
         connectionStatus: WorkroomConnectionStatus.cold,
         lastAppliedSeq: seedSnapshot.watermarkSeq,
         connectionMessage: '',
         surfaceError: '',
       ),
       _reconnectDelay = reconnectDelay ?? Future<void>.delayed;

  final int projectId;
  final ProjectWorkroomRepository repository;
  final ProjectUiCursorStore cursorStore;
  final WorkroomReconnectDelay _reconnectDelay;

  ProjectWorkroomState _state;
  ProjectWorkroomState get state => _state;

  bool _started = false;
  bool _disposed = false;
  bool _protocolBlocked = false;
  bool _replayRequested = false;
  StreamSubscription<ProjectUiFrame>? _subscription;
  int _commandOrdinal = 0;
  final Map<int, String> _appliedEventIds = <int, String>{};

  Future<void> start() async {
    if (_started || _disposed) return;
    _started = true;
    _emit(
      _state.copyWith(
        connectionStatus: WorkroomConnectionStatus.loadingSnapshot,
        clearConnectionMessage: true,
      ),
    );
    try {
      final snapshot = await repository.loadSnapshot(projectId);
      if (_disposed) return;
      if (snapshot.project.id != projectId) {
        throw WorkroomProtocolException(
          'project_mismatch',
          'Snapshot project ${snapshot.project.id} does not match $projectId.',
        );
      }
      await cursorStore.write(projectId, snapshot.watermarkSeq);
      if (_disposed) return;
      _emit(
        _state.copyWith(
          snapshot: snapshot,
          lastAppliedSeq: snapshot.watermarkSeq,
          connectionStatus: WorkroomConnectionStatus.replaying,
          clearConnectionMessage: true,
          clearSurfaceError: true,
        ),
      );
    } on WorkroomProtocolException catch (error) {
      _protocolBlocked = true;
      _emit(
        _state.copyWith(
          connectionStatus: WorkroomConnectionStatus.protocolError,
          connectionMessage: error.toString(),
        ),
      );
      return;
    } on Object catch (error) {
      final savedCursor = await cursorStore.read(projectId);
      if (_disposed) return;
      _emit(
        _state.copyWith(
          lastAppliedSeq: savedCursor ?? _state.lastAppliedSeq,
          connectionStatus: WorkroomConnectionStatus.reconnecting,
          connectionMessage: error.toString(),
        ),
      );
    }
    unawaited(_listenLoop());
  }

  Future<void> stop() async {
    if (_disposed) return;
    _disposed = true;
    await _subscription?.cancel();
    _subscription = null;
  }

  @override
  void dispose() {
    unawaited(stop());
    super.dispose();
  }

  void selectThread(String threadId) {
    if (!_state.snapshot.threads.any((thread) => thread.id == threadId)) {
      return;
    }
    _emit(
      _state.copyWith(
        snapshot: _state.snapshot.copyWith(selectedThreadId: threadId),
      ),
    );
  }

  Future<void> loadHistoricalThread(
    String threadId,
    Future<List<WorkroomFixerTurn>> Function(String threadId) loader,
  ) async {
    selectThread(threadId);
    List<WorkroomFixerTurn> turns;
    try {
      turns = await loader(threadId);
    } catch (error) {
      debugPrint('[workroom] transcript load failed for $threadId: $error');
      return;
    }
    final retained = _state.snapshot.turns
        .where((turn) => turn.threadId != threadId)
        .toList(growable: true);
    retained.addAll(turns);
    retained.sort((left, right) => left.ordinal.compareTo(right.ordinal));
    _emit(
      _state.copyWith(
        snapshot: _state.snapshot.copyWith(
          selectedThreadId: threadId,
          turns: retained,
        ),
      ),
    );
  }

  Future<void> reloadSnapshot() async {
    final snapshot = await repository.loadSnapshot(projectId);
    if (_disposed) return;
    _emit(
      _state.copyWith(
        snapshot: snapshot,
        lastAppliedSeq: snapshot.watermarkSeq,
        connectionStatus: WorkroomConnectionStatus.live,
      ),
    );
  }

  void startNewFixerThread() {
    _emit(
      _state.copyWith(snapshot: _state.snapshot.copyWith(selectedThreadId: '')),
    );
  }

  void selectHandsInstruction(String instructionId) {
    final hands = _state.snapshot.hands;
    if (!hands.instructions.any(
      (instruction) => instruction.id == instructionId,
    )) {
      return;
    }
    _emit(
      _state.copyWith(
        snapshot: _state.snapshot.copyWith(
          hands: hands.copyWith(selectedInstructionId: instructionId),
        ),
      ),
    );
  }

  void activateSurface(String surfaceId) {
    WorkroomSurfaceState? selected;
    for (final surface in _state.snapshot.surfaceHistory) {
      if (surface.document.instanceId == surfaceId) {
        selected = surface;
        break;
      }
    }
    if (selected == null) return;
    _emit(
      _state.copyWith(
        snapshot: _state.snapshot.copyWith(activeSurface: selected),
        clearSurfaceError: true,
      ),
    );
  }

  Future<FixerTurnReceipt> sendFixerTurn(String content) async {
    final threadId = _state.snapshot.selectedThreadId;
    final receipt = await repository.sendFixerTurn(
      projectId: projectId,
      threadId: threadId,
      content: content,
      idempotencyKey: _nextKey('fixer-turn'),
    );
    return receipt;
  }

  Future<GenUiActionReceipt> requestSurface(RegisteredSurfaceRequest request) {
    request.validate();
    return repository.requestSurface(
      projectId: projectId,
      threadId: _state.snapshot.selectedThreadId,
      request: request,
      idempotencyKey: _nextKey('surface-${request.surfaceType}'),
    );
  }

  Future<GenUiActionReceipt> invokeAction(
    GenUiActionDescriptor action, {
    Map<String, dynamic> input = const <String, dynamic>{},
    required bool confirmed,
  }) {
    final surface = _state.snapshot.activeSurface;
    if (surface == null) {
      throw StateError('No active GenUI surface.');
    }
    return repository.invokeAction(
      projectId: projectId,
      surfaceId: surface.document.instanceId,
      surfaceRevision: surface.document.revision,
      action: action,
      input: input,
      confirmed: confirmed,
      idempotencyKey: _nextKey('action-${action.actionId}'),
    );
  }

  Future<GenUiActionReceipt> submitFeedback(int vote) async {
    if (vote != 1 && vote != -1) {
      throw ArgumentError.value(vote, 'vote', 'Vote must be 1 or -1.');
    }
    final surface = _state.snapshot.activeSurface;
    if (surface == null) throw StateError('No active GenUI surface.');
    final action = GenUiActionDescriptor(
      ref: 'feedback',
      actionId: 'genui.feedback.submit',
      actionVersion: 1,
      label: vote == 1 ? 'Helpful' : 'Not helpful',
      target: GenUiActionTarget(
        type: 'genui_surface',
        id: surface.document.instanceId,
      ),
      enabled: true,
      disabledReasonCode: '',
      disabledReason: '',
      confirmation: 'none',
      inputSchema: 'feedback.v1',
    );
    final receipt = await invokeAction(
      action,
      input: {'vote': vote},
      confirmed: true,
    );
    if (receipt.succeeded) {
      final acceptedVote = receipt.vote == 1 || receipt.vote == -1
          ? receipt.vote
          : vote;
      _emit(
        _state.copyWith(
          snapshot: _state.snapshot.copyWith(
            activeSurface: surface.copyWith(feedbackVote: acceptedVote),
          ),
        ),
      );
    }
    return receipt;
  }

  Future<HandsInstructionReceipt> submitHandsInstruction({
    required String instructionText,
    required List<String> declaredWriteScope,
    required String requestedLane,
    required String requestedModel,
    required String requestedReasoning,
  }) {
    return repository.submitHandsInstruction(
      projectId: projectId,
      instructionText: instructionText,
      declaredWriteScope: declaredWriteScope,
      requestedLane: requestedLane,
      requestedModel: requestedModel,
      requestedReasoning: requestedReasoning,
      idempotencyKey: _nextKey('hands-instruction'),
    );
  }

  Future<GenUiActionReceipt> cancelHandsInstruction(
    String instructionId, {
    String reason = '',
  }) {
    return repository.cancelHandsInstruction(
      projectId: projectId,
      instructionId: instructionId,
      reason: reason,
      idempotencyKey: _nextKey('hands-cancel'),
    );
  }

  Future<GenUiActionReceipt> selectHandsLane(String provider) {
    return repository.selectHandsLane(
      projectId: projectId,
      provider: provider,
      idempotencyKey: _nextKey('hands-lane'),
    );
  }

  Future<GenUiActionReceipt> reviewHandsInstruction({
    required String instructionId,
    required String decision,
    required String reviewNote,
  }) {
    return repository.reviewHandsInstruction(
      projectId: projectId,
      instructionId: instructionId,
      decision: decision,
      reviewNote: reviewNote,
      idempotencyKey: _nextKey('hands-review-$decision'),
    );
  }

  Future<void> _listenLoop() async {
    var reconnectAttempt = 0;
    while (!_disposed && !_protocolBlocked) {
      _replayRequested = false;
      _emit(
        _state.copyWith(
          connectionStatus: reconnectAttempt == 0
              ? WorkroomConnectionStatus.replaying
              : WorkroomConnectionStatus.reconnecting,
        ),
      );
      try {
        await _consumeStream();
        reconnectAttempt = 0;
      } on WorkroomProtocolException catch (error) {
        _protocolBlocked = true;
        _emit(
          _state.copyWith(
            connectionStatus: WorkroomConnectionStatus.protocolError,
            connectionMessage: error.toString(),
          ),
        );
        break;
      } on Object catch (error) {
        if (_disposed) break;
        final text = error.toString();
        if (_looksLikeAuthorizationLoss(text)) {
          _emit(
            _state.copyWith(
              connectionStatus: WorkroomConnectionStatus.authorizationLost,
              connectionMessage: text,
            ),
          );
          break;
        }
        reconnectAttempt += 1;
        _emit(
          _state.copyWith(
            connectionStatus: _replayRequested
                ? WorkroomConnectionStatus.replaying
                : WorkroomConnectionStatus.reconnecting,
            connectionMessage: _replayRequested
                ? 'A sequence gap was detected; replaying durable events.'
                : text,
          ),
        );
      }
      if (_disposed || _protocolBlocked) break;
      final delay = _replayRequested
          ? Duration.zero
          : Duration(milliseconds: 250 * reconnectAttempt.clamp(1, 8));
      await _reconnectDelay(delay);
    }
  }

  Future<void> _consumeStream() async {
    final stream = repository.watchProjectUi(
      projectId,
      afterSeq: _state.lastAppliedSeq,
      protocolVersion: projectWorkroomProtocolVersion,
    );
    final controller = StreamController<ProjectUiFrame>();
    Object? streamError;
    StackTrace? streamStack;
    _subscription = stream.listen(
      controller.add,
      onError: (Object error, StackTrace stackTrace) {
        streamError = error;
        streamStack = stackTrace;
        unawaited(controller.close());
      },
      onDone: () => unawaited(controller.close()),
      cancelOnError: true,
    );
    try {
      await for (final frame in controller.stream) {
        await _applyFrame(frame);
        if (_replayRequested || _protocolBlocked || _disposed) break;
      }
    } finally {
      await _subscription?.cancel();
      _subscription = null;
      await controller.close();
    }
    if (_protocolBlocked || _disposed) return;
    if (_replayRequested) {
      throw StateError('sequence_gap');
    }
    if (streamError != null) {
      Error.throwWithStackTrace(
        streamError!,
        streamStack ?? StackTrace.current,
      );
    }
    throw StateError('The Serverpod project event stream ended.');
  }

  Future<void> _applyFrame(ProjectUiFrame frame) async {
    switch (frame) {
      case ProjectUiEventFrame():
        await _applyEvent(frame.event);
      case ProjectUiEventBatchFrame():
        for (final event in frame.events) {
          await _applyEvent(event);
          if (_replayRequested || _protocolBlocked) break;
        }
      case ProjectUiHeartbeatFrame():
        if (_state.connectionStatus == WorkroomConnectionStatus.live &&
            _state.connectionMessage.isEmpty) {
          return;
        }
        _emit(
          _state.copyWith(
            connectionStatus: WorkroomConnectionStatus.live,
            clearConnectionMessage: true,
          ),
        );
      case ProjectUiProtocolErrorFrame():
        throw WorkroomProtocolException(
          frame.reasonCode,
          frame.message.isEmpty
              ? 'Supported protocol versions: '
                    '${frame.minimumVersion}-${frame.maximumVersion}.'
              : frame.message,
        );
    }
  }

  Future<void> _applyEvent(ProjectUiEvent event) async {
    if (event.projectId != projectId) {
      throw WorkroomProtocolException(
        'project_mismatch',
        'Received an event for project ${event.projectId}.',
      );
    }
    final currentSeq = _state.lastAppliedSeq;
    if (event.seq <= currentSeq) {
      final knownId = _appliedEventIds[event.seq];
      if (knownId != null && knownId != event.eventId) {
        throw WorkroomProtocolException(
          'sequence_corruption',
          'Sequence ${event.seq} was delivered with two event IDs.',
        );
      }
      return;
    }
    if (event.seq != currentSeq + 1) {
      _replayRequested = true;
      return;
    }

    var nextSnapshot = _state.snapshot;
    var surfaceError = '';
    try {
      nextSnapshot = _reduce(nextSnapshot, event);
    } on WorkroomProtocolException catch (error) {
      if (event.kind == 'genui.surface.presented' ||
          event.kind == 'genui.surface.revised') {
        surfaceError = error.toString();
      } else {
        rethrow;
      }
    }
    nextSnapshot = nextSnapshot.copyWith(watermarkSeq: event.seq);
    await cursorStore.write(projectId, event.seq);
    if (_disposed) return;
    _appliedEventIds[event.seq] = event.eventId;
    if (_appliedEventIds.length > 512) {
      _appliedEventIds.remove(_appliedEventIds.keys.first);
    }
    _emit(
      _state.copyWith(
        snapshot: nextSnapshot,
        lastAppliedSeq: event.seq,
        connectionStatus: WorkroomConnectionStatus.live,
        clearConnectionMessage: true,
        surfaceError: surfaceError,
        clearSurfaceError: surfaceError.isEmpty,
      ),
    );
  }

  ProjectWorkroomSnapshot _reduce(
    ProjectWorkroomSnapshot snapshot,
    ProjectUiEvent event,
  ) {
    final payload = event.payload;
    final replacement =
        payload['snapshot'] ?? payload['workroom'] ?? payload['read_model'];
    if (replacement != null) {
      final parsed = ProjectWorkroomSnapshot.fromJson(
        Map<String, dynamic>.from(replacement as Map),
      );
      if (parsed.project.id != projectId) {
        throw const WorkroomProtocolException(
          'project_mismatch',
          'Replacement snapshot belongs to another project.',
        );
      }
      return parsed;
    }

    switch (event.kind) {
      case 'fixer.thread.created':
      case 'fixer.thread.changed':
        final raw = payload['thread'] ?? payload;
        final thread = WorkroomFixerThread.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        final threads = _replaceBy(snapshot.threads, thread, (item) => item.id);
        return snapshot.copyWith(
          threads: threads,
          selectedThreadId: snapshot.selectedThreadId.isEmpty
              ? thread.id
              : snapshot.selectedThreadId,
        );
      case 'fixer.turn.appended':
      case 'fixer.turn.changed':
        final raw = payload['turn'] ?? payload;
        final turn = WorkroomFixerTurn.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        final turns =
            _replaceBy(snapshot.turns, turn, (item) => item.id).toList(
              growable: true,
            )..sort((left, right) => left.ordinal.compareTo(right.ordinal));
        return snapshot.copyWith(turns: turns);
      case 'genui.surface.presented':
      case 'genui.surface.revised':
        final raw = payload['surface'] ?? payload;
        final surface = WorkroomSurfaceState.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        if (surface.document.projectId != projectId) {
          throw const WorkroomProtocolException(
            'project_mismatch',
            'Surface belongs to another project.',
          );
        }
        WorkroomSurfaceState? previousRevision;
        for (final item in snapshot.surfaceHistory) {
          if (item.document.instanceId == surface.document.instanceId) {
            previousRevision = item;
            break;
          }
        }
        if (previousRevision != null &&
            surface.document.revision <= previousRevision.document.revision) {
          throw WorkroomProtocolException(
            'surface_revision_stale',
            'Surface ${surface.document.instanceId} revision '
                '${surface.document.revision} does not advance revision '
                '${previousRevision.document.revision}.',
          );
        }
        final history = _replaceBy(
          snapshot.surfaceHistory,
          surface,
          (item) => item.document.instanceId,
        );
        return snapshot.copyWith(
          activeSurface: surface,
          surfaceHistory: history,
        );
      case 'genui.surface.revoked':
        final surfaceId =
            payload['surface_id']?.toString() ?? event.aggregateId;
        if (snapshot.activeSurface?.document.instanceId != surfaceId) {
          return snapshot;
        }
        return snapshot.copyWith(activeSurface: null);
      case 'genui.feedback.recorded':
        final surface = snapshot.activeSurface;
        if (surface == null) return snapshot;
        final surfaceId =
            payload['surface_id']?.toString() ?? event.aggregateId;
        if (surface.document.instanceId != surfaceId) return snapshot;
        final vote = _payloadInt(payload, 'vote');
        return snapshot.copyWith(
          activeSurface: surface.copyWith(feedbackVote: vote),
        );
      case 'hands.identity.changed':
        final raw = payload['hands'] ?? payload;
        return snapshot.copyWith(
          hands: WorkroomHandsState.fromJson(
            Map<String, dynamic>.from(raw as Map),
          ),
        );
      case 'hands.lane.changed':
        final raw = payload['lane'] ?? payload;
        final lane = HandsProviderLane.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        final lanes = _replaceBy(
          snapshot.hands.lanes,
          lane,
          (item) => item.provider,
        );
        return snapshot.copyWith(
          hands: snapshot.hands.copyWith(
            lanes: lanes,
            selectedLane:
                payload['selected'] == true || payload['is_selected'] == true
                ? lane.provider
                : snapshot.hands.selectedLane,
          ),
        );
      case 'hands.instruction.created':
      case 'hands.instruction.changed':
        final raw = payload['instruction'] ?? payload;
        final instruction = HandsInstruction.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        final instructions =
            _replaceBy(
                snapshot.hands.instructions,
                instruction,
                (item) => item.id,
              ).toList(growable: true)
              ..sort((left, right) => right.ordinal.compareTo(left.ordinal));
        return snapshot.copyWith(
          hands: snapshot.hands.copyWith(
            instructions: instructions,
            selectedInstructionId: snapshot.hands.selectedInstructionId.isEmpty
                ? instruction.id
                : snapshot.hands.selectedInstructionId,
            operationalState:
                payload['operational_state']?.toString() ??
                snapshot.hands.operationalState,
            queueDepth:
                _payloadNullableInt(payload, 'queue_depth') ??
                snapshot.hands.queueDepth,
          ),
        );
      case 'hands.instruction.event_appended':
        final instructionId =
            payload['instruction_id']?.toString() ?? event.aggregateId;
        final raw = payload['event'] ?? payload;
        final instructionEvent = HandsInstructionEvent.fromJson(
          Map<String, dynamic>.from(raw as Map),
        );
        final instructions = snapshot.hands.instructions
            .map((instruction) {
              if (instruction.id != instructionId) return instruction;
              return instruction.copyWith(
                events: List.unmodifiable([
                  ...instruction.events,
                  instructionEvent,
                ]),
                updatedAt: instructionEvent.createdAt,
              );
            })
            .toList(growable: false);
        return snapshot.copyWith(
          hands: snapshot.hands.copyWith(instructions: instructions),
        );
      case 'hands.generation.changed':
        return snapshot.copyWith(
          hands: snapshot.hands.copyWith(
            operationalState:
                payload['operational_state']?.toString() ??
                payload['state']?.toString() ??
                snapshot.hands.operationalState,
          ),
        );
      case 'lease.changed':
        return snapshot.copyWith(
          hands: snapshot.hands.copyWith(
            activeLeaseSummary:
                payload['lease_summary']?.toString() ??
                payload['summary']?.toString() ??
                snapshot.hands.activeLeaseSummary,
          ),
        );
      case 'project.changed':
        final raw = payload['project'] ?? payload;
        final map = Map<String, dynamic>.from(raw as Map);
        return snapshot.copyWith(
          project: snapshot.project.copyWith(
            name: map['name']?.toString(),
            cwd: map['cwd']?.toString(),
          ),
        );
      case 'genui.demand.recorded':
      case 'genui.action.changed':
      case 'planned_wave.changed':
      case 'wave.changed':
      case 'session.changed':
      case 'backlog.changed':
      case 'document.changed':
      case 'skill.changed':
        // These events either carry a replacement surface above or invalidate
        // a server renderer. The active trusted document remains visible until
        // the server emits its next immutable surface revision.
        return snapshot;
      default:
        throw WorkroomProtocolException(
          'unsupported_event',
          'Unsupported project event kind: ${event.kind}.',
        );
    }
  }

  List<T> _replaceBy<T>(
    List<T> source,
    T replacement,
    String Function(T item) idOf,
  ) {
    final id = idOf(replacement);
    var found = false;
    final result = source
        .map((item) {
          if (idOf(item) != id) return item;
          found = true;
          return replacement;
        })
        .toList(growable: true);
    if (!found) result.add(replacement);
    return List.unmodifiable(result);
  }

  int _payloadInt(Map<String, dynamic> payload, String key) {
    return _payloadNullableInt(payload, key) ?? 0;
  }

  int? _payloadNullableInt(Map<String, dynamic> payload, String key) {
    final value = payload[key];
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '');
  }

  bool _looksLikeAuthorizationLoss(String message) {
    final normalized = message.toLowerCase();
    return normalized.contains('unauthorized') ||
        normalized.contains('forbidden') ||
        normalized.contains('authentication revoked');
  }

  String _nextKey(String prefix) {
    _commandOrdinal += 1;
    return '$prefix-${DateTime.now().microsecondsSinceEpoch}-$_commandOrdinal';
  }

  void _emit(ProjectWorkroomState value) {
    if (_disposed) return;
    _state = value;
    notifyListeners();
  }
}
