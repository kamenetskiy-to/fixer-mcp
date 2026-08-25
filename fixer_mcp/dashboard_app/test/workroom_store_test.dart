import 'dart:async';

import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_repository.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'workroom_test_fakes.dart';

void main() {
  test(
    'reduces ordered events, ignores duplicates, and replays gaps',
    () async {
      final repository = _ScriptedRepository(workroomFixture());
      final cursors = MemoryProjectUiCursorStore();
      final store = ProjectWorkroomStore(
        projectId: 1,
        repository: repository,
        cursorStore: cursors,
        seedSnapshot: workroomFixture(),
        reconnectDelay: (_) async {},
      );
      addTearDown(() async {
        await store.stop();
        await repository.close();
      });

      await store.start();
      await _waitFor(
        () => repository.watchCalls.length == 1,
        () => 'initial stream subscription',
      );
      expect(repository.watchCalls, [40]);

      repository.controllers[0].add(_turnEvent(41, 'event-41', 'First replay'));
      await _waitFor(
        () => store.state.lastAppliedSeq == 41,
        () =>
            'event 41 reduction (${store.state.connectionStatus}: '
            '${store.state.connectionMessage}; '
            'watch calls: ${repository.watchCalls})',
      );
      expect(await cursors.read(1), 41);
      expect(store.state.snapshot.turns.last.content, 'First replay');

      repository.controllers[0].add(_turnEvent(41, 'event-41', 'Duplicate'));
      await Future<void>.delayed(Duration.zero);
      expect(store.state.snapshot.turns.last.content, 'First replay');

      repository.controllers[0].add(_turnEvent(43, 'event-43', 'Gap'));
      await _waitFor(
        () => repository.watchCalls.length == 2,
        () =>
            'gap replay subscription (${store.state.connectionStatus}: '
            '${store.state.connectionMessage})',
      );
      expect(repository.watchCalls.last, 41);
      expect(store.state.lastAppliedSeq, 41);

      repository.controllers[1].add(_turnEvent(42, 'event-42', 'Recovered 42'));
      repository.controllers[1].add(_turnEvent(43, 'event-43', 'Recovered 43'));
      await _waitFor(
        () => store.state.lastAppliedSeq == 43,
        () =>
            'replayed events through 43 (${store.state.connectionStatus}: '
            '${store.state.connectionMessage})',
      );
      expect(store.state.snapshot.turns.last.content, 'Recovered 43');
      expect(store.state.connectionStatus, WorkroomConnectionStatus.live);
    },
  );

  test(
    'reconnect preserves visible state and resumes committed cursor',
    () async {
      final repository = _ScriptedRepository(workroomFixture());
      final store = ProjectWorkroomStore(
        projectId: 1,
        repository: repository,
        cursorStore: MemoryProjectUiCursorStore(),
        seedSnapshot: workroomFixture(),
        reconnectDelay: (_) async {},
      );
      addTearDown(() async {
        await store.stop();
        await repository.close();
      });

      await store.start();
      await _waitFor(
        () => repository.watchCalls.isNotEmpty,
        () => 'initial reconnect-test subscription',
      );
      repository.controllers.first.add(
        _turnEvent(41, 'event-41', 'Durable visible state'),
      );
      await _waitFor(
        () => store.state.lastAppliedSeq == 41,
        () =>
            'visible event before reconnect '
            '(${store.state.connectionStatus}: '
            '${store.state.connectionMessage})',
      );
      final surfaceId = store.state.snapshot.activeSurface!.document.instanceId;

      repository.controllers.first.addError(StateError('socket lost'));
      await _waitFor(
        () => repository.watchCalls.length == 2,
        () =>
            'replacement subscription after disconnect '
            '(${store.state.connectionStatus}: '
            '${store.state.connectionMessage})',
      );

      expect(repository.watchCalls.last, 41);
      expect(store.state.snapshot.turns.last.content, 'Durable visible state');
      expect(
        store.state.snapshot.activeSurface!.document.instanceId,
        surfaceId,
      );
    },
  );

  test('same sequence with a different event id blocks the reducer', () async {
    final repository = _ScriptedRepository(workroomFixture());
    final store = ProjectWorkroomStore(
      projectId: 1,
      repository: repository,
      cursorStore: MemoryProjectUiCursorStore(),
      seedSnapshot: workroomFixture(),
      reconnectDelay: (_) async {},
    );
    addTearDown(() async {
      await store.stop();
      await repository.close();
    });

    await store.start();
    await _waitFor(
      () => repository.watchCalls.isNotEmpty,
      () => 'initial corruption-test subscription',
    );
    repository.controllers.first.add(_turnEvent(41, 'event-a', 'Applied'));
    await _waitFor(
      () => store.state.lastAppliedSeq == 41,
      () =>
          'first event before corruption '
          '(${store.state.connectionStatus}: ${store.state.connectionMessage})',
    );
    repository.controllers.first.add(_turnEvent(41, 'event-b', 'Corrupt'));

    await _waitFor(
      () =>
          store.state.connectionStatus ==
          WorkroomConnectionStatus.protocolError,
      () =>
          'protocol block after duplicate sequence '
          '(${store.state.connectionStatus}: ${store.state.connectionMessage})',
    );
    expect(store.state.connectionMessage, contains('sequence_corruption'));
  });

  test('invalid surface revision retains the last valid surface', () async {
    final repository = _ScriptedRepository(workroomFixture());
    final store = ProjectWorkroomStore(
      projectId: 1,
      repository: repository,
      cursorStore: MemoryProjectUiCursorStore(),
      seedSnapshot: workroomFixture(),
      reconnectDelay: (_) async {},
    );
    addTearDown(() async {
      await store.stop();
      await repository.close();
    });

    await store.start();
    await _waitFor(
      () => repository.watchCalls.isNotEmpty,
      () => 'initial invalid-revision subscription',
    );
    final original = store.state.snapshot.activeSurface!;
    final staleDocument = surfaceFixture(
      projectId: 1,
      surfaceType: 'project.overview',
      instanceId: original.document.instanceId,
      sourceSeq: 41,
    );
    repository.controllers.first.add(
      ProjectUiEventFrame(
        ProjectUiEvent(
          projectId: 1,
          seq: 41,
          eventId: 'event-stale-surface',
          schemaVersion: 1,
          kind: 'genui.surface.revised',
          aggregateType: 'genui_surface',
          aggregateId: staleDocument.instanceId,
          aggregateRevision: 1,
          createdAt: '2026-07-30T12:00:00Z',
          payload: {
            'surface': {'document': staleDocument.toJson()},
          },
        ),
      ),
    );

    await _waitFor(
      () => store.state.lastAppliedSeq == 41,
      () => 'stale surface event reduction',
    );
    expect(
      store.state.snapshot.activeSurface!.document.revision,
      original.document.revision,
    );
    expect(store.state.surfaceError, contains('surface_revision_stale'));
  });

  test(
    'feedback changes visible state only after a successful receipt',
    () async {
      final repository = _ScriptedRepository(workroomFixture());
      final receipt = Completer<GenUiActionReceipt>();
      repository.invokeActionCompleter = receipt;
      final store = ProjectWorkroomStore(
        projectId: 1,
        repository: repository,
        cursorStore: MemoryProjectUiCursorStore(),
        seedSnapshot: workroomFixture(),
        reconnectDelay: (_) async {},
      );
      addTearDown(() async {
        await store.stop();
        await repository.close();
      });

      await store.start();
      final pending = store.submitFeedback(1);
      await Future<void>.delayed(Duration.zero);
      expect(store.state.snapshot.activeSurface!.feedbackVote, 0);

      receipt.complete(
        const GenUiActionReceipt(
          status: 'recorded',
          reasonCode: '',
          message: '',
          projectSeq: 41,
          vote: 1,
        ),
      );
      await pending;
      expect(store.state.snapshot.activeSurface!.feedbackVote, 1);
    },
  );

  test('routine heartbeats do not rebuild an already-live workroom', () async {
    final repository = _ScriptedRepository(workroomFixture());
    final store = ProjectWorkroomStore(
      projectId: 1,
      repository: repository,
      cursorStore: MemoryProjectUiCursorStore(),
      seedSnapshot: workroomFixture(),
      reconnectDelay: (_) async {},
    );
    addTearDown(() async {
      await store.stop();
      await repository.close();
    });

    await store.start();
    await _waitFor(
      () => repository.watchCalls.isNotEmpty,
      () => 'initial heartbeat-test subscription',
    );
    repository.controllers.first.add(
      const ProjectUiHeartbeatFrame(
        serverTime: '2026-07-30T12:00:00Z',
        journalHead: 40,
      ),
    );
    await _waitFor(
      () => store.state.connectionStatus == WorkroomConnectionStatus.live,
      () => 'first heartbeat transition',
    );

    var notifications = 0;
    store.addListener(() => notifications += 1);
    repository.controllers.first.add(
      const ProjectUiHeartbeatFrame(
        serverTime: '2026-07-30T12:00:20Z',
        journalHead: 40,
      ),
    );
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect(notifications, 0);
  });
}

ProjectUiEventFrame _turnEvent(int sequence, String eventId, String content) {
  return ProjectUiEventFrame(
    ProjectUiEvent(
      projectId: 1,
      seq: sequence,
      eventId: eventId,
      schemaVersion: 1,
      kind: 'fixer.turn.appended',
      aggregateType: 'fixer_turn',
      aggregateId: 'turn-$sequence',
      aggregateRevision: 1,
      createdAt: '2026-07-30T12:00:00Z',
      payload: {
        'turn': {
          'id': 'turn-$sequence',
          'thread_id': 'fixer-thread',
          'ordinal': sequence,
          'role': 'fixer',
          'content': content,
          'status': 'complete',
          'created_at': '2026-07-30T12:00:00Z',
        },
      },
    ),
  );
}

Future<void> _waitFor(
  bool Function() condition,
  String Function() failureReason,
) async {
  for (var attempt = 0; attempt < 200; attempt++) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 1));
  }
  fail('Condition was not reached: ${failureReason()}.');
}

class _ScriptedRepository implements ProjectWorkroomRepository {
  _ScriptedRepository(this.snapshot);

  final ProjectWorkroomSnapshot snapshot;
  final List<int> watchCalls = [];
  final List<StreamController<ProjectUiFrame>> controllers = [];
  Completer<GenUiActionReceipt>? invokeActionCompleter;

  Future<void> close() async {
    for (final controller in controllers) {
      if (!controller.isClosed) await controller.close();
    }
  }

  @override
  Future<ProjectWorkroomSnapshot> loadSnapshot(int projectId) async => snapshot;

  @override
  Stream<ProjectUiFrame> watchProjectUi(
    int projectId, {
    required int afterSeq,
    int protocolVersion = projectWorkroomProtocolVersion,
  }) {
    watchCalls.add(afterSeq);
    final controller = StreamController<ProjectUiFrame>();
    controllers.add(controller);
    return controller.stream;
  }

  @override
  Future<FixerTurnReceipt> sendFixerTurn({
    required int projectId,
    required String threadId,
    required String content,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<GenUiActionReceipt> requestSurface({
    required int projectId,
    required String threadId,
    required RegisteredSurfaceRequest request,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
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
  }) {
    final completer = invokeActionCompleter;
    if (completer == null) throw UnimplementedError();
    return completer.future;
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
  }) {
    throw UnimplementedError();
  }

  @override
  Future<GenUiActionReceipt> cancelHandsInstruction({
    required int projectId,
    required String instructionId,
    required String reason,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<GenUiActionReceipt> selectHandsLane({
    required int projectId,
    required String provider,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<GenUiActionReceipt> reviewHandsInstruction({
    required int projectId,
    required String instructionId,
    required String decision,
    required String reviewNote,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }
}
