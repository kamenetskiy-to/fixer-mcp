import 'dart:async';

import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_repository.dart';

class FakeProjectWorkroomRepository implements ProjectWorkroomRepository {
  FakeProjectWorkroomRepository({ProjectWorkroomSnapshot? snapshot})
    : snapshot = snapshot ?? workroomFixture();

  ProjectWorkroomSnapshot snapshot;
  final _frames = StreamController<ProjectUiFrame>.broadcast();
  final List<RegisteredSurfaceRequest> surfaceRequests = [];
  final List<int> feedbackVotes = [];
  final List<String> submittedInstructions = [];
  var watchCalls = <int>[];
  var selectedLane = '';
  var _sequence = 40;

  Future<void> close() => _frames.close();

  @override
  Future<ProjectWorkroomSnapshot> loadSnapshot(int projectId) async => snapshot;

  @override
  Stream<ProjectUiFrame> watchProjectUi(
    int projectId, {
    required int afterSeq,
    int protocolVersion = projectWorkroomProtocolVersion,
  }) async* {
    watchCalls.add(afterSeq);
    yield ProjectUiHeartbeatFrame(
      serverTime: '2026-07-30T12:00:00Z',
      journalHead: _sequence,
    );
    yield* _frames.stream;
  }

  @override
  Future<FixerTurnReceipt> sendFixerTurn({
    required int projectId,
    required String threadId,
    required String content,
    required String idempotencyKey,
  }) async {
    _emit('fixer.turn.appended', 'fixer_turn', 'turn-${_sequence + 1}', {
      'turn': {
        'id': 'turn-${_sequence + 1}',
        'thread_id': threadId,
        'ordinal': snapshot.turns.length + 1,
        'role': 'user',
        'content': content,
        'status': 'complete',
        'created_at': '2026-07-30T12:00:01Z',
      },
    });
    final normalized = content.toLowerCase();
    if (normalized.contains('ресерч') && normalized.contains('юрк')) {
      _emit('fixer.turn.appended', 'fixer_turn', 'turn-${_sequence + 1}', {
        'turn': {
          'id': 'turn-${_sequence + 1}',
          'thread_id': threadId,
          'ordinal': snapshot.turns.length + 2,
          'role': 'fixer',
          'content':
              'Открываю юридический ресерч проекта как управляемую GenUI-поверхность.',
          'status': 'complete',
          'created_at': '2026-07-30T12:00:02Z',
        },
      });
      final document = surfaceFixture(
        projectId: projectId,
        surfaceType: 'research.legal',
        instanceId: 'surface-legal',
        sourceSeq: _sequence + 1,
      );
      _emit('genui.surface.presented', 'genui_surface', document.instanceId, {
        'surface': {'document': document.toJson(), 'feedback_vote': 0},
      });
    }
    return FixerTurnReceipt(
      turnId: 'turn-$_sequence',
      status: 'accepted',
      projectSeq: _sequence,
    );
  }

  @override
  Future<GenUiActionReceipt> requestSurface({
    required int projectId,
    required String threadId,
    required RegisteredSurfaceRequest request,
    required String idempotencyKey,
  }) async {
    request.validate();
    surfaceRequests.add(request);
    final document = surfaceFixture(
      projectId: projectId,
      surfaceType: request.surfaceType,
      instanceId: 'surface-${surfaceRequests.length}',
      sourceSeq: _sequence + 1,
    );
    _emit('genui.surface.presented', 'genui_surface', document.instanceId, {
      'surface': {'document': document.toJson(), 'feedback_vote': 0},
    });
    return GenUiActionReceipt(
      status: 'succeeded',
      reasonCode: '',
      message: '',
      projectSeq: _sequence,
      vote: 0,
    );
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
    if (action.actionId == 'genui.feedback.submit') {
      final vote = input['vote'] as int;
      feedbackVotes.add(vote);
      return GenUiActionReceipt(
        status: 'recorded',
        reasonCode: '',
        message: '',
        projectSeq: _sequence,
        vote: vote,
      );
    }
    return GenUiActionReceipt(
      status: 'succeeded',
      reasonCode: '',
      message: 'Action completed.',
      projectSeq: _sequence,
      vote: 0,
    );
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
    submittedInstructions.add(instructionText);
    final instructionId = 'instruction-${submittedInstructions.length + 1}';
    _emit('hands.instruction.created', 'hands_instruction', instructionId, {
      'instruction': {
        'id': instructionId,
        'ordinal': submittedInstructions.length + 1,
        'instruction_text': instructionText,
        'state': 'queued',
        'requested_lane': requestedLane,
        'requested_model': requestedModel,
        'requested_reasoning': requestedReasoning,
        'declared_write_scope': declaredWriteScope,
        'issuer': 'architect',
        'created_at': '2026-07-30T12:01:00Z',
        'updated_at': '2026-07-30T12:01:00Z',
        'events': <Map<String, dynamic>>[],
      },
      'queue_depth': 1,
      'operational_state': 'queued',
    });
    return HandsInstructionReceipt(
      instructionId: instructionId,
      ordinal: submittedInstructions.length + 1,
      state: 'queued',
      lane: requestedLane,
      projectSeq: _sequence,
    );
  }

  @override
  Future<GenUiActionReceipt> cancelHandsInstruction({
    required int projectId,
    required String instructionId,
    required String reason,
    required String idempotencyKey,
  }) async {
    return _success();
  }

  @override
  Future<GenUiActionReceipt> selectHandsLane({
    required int projectId,
    required String provider,
    required String idempotencyKey,
  }) async {
    selectedLane = provider;
    _emit('hands.lane.changed', 'hands_lane', provider, {
      'selected': true,
      'lane': {
        'provider': provider,
        'model': 'fixture-model',
        'reasoning': 'medium',
      },
    });
    return _success();
  }

  @override
  Future<GenUiActionReceipt> reviewHandsInstruction({
    required int projectId,
    required String instructionId,
    required String decision,
    required String reviewNote,
    required String idempotencyKey,
  }) async {
    return _success();
  }

  GenUiActionReceipt _success() {
    return GenUiActionReceipt(
      status: 'succeeded',
      reasonCode: '',
      message: '',
      projectSeq: _sequence,
      vote: 0,
    );
  }

  void _emit(
    String kind,
    String aggregateType,
    String aggregateId,
    Map<String, dynamic> payload,
  ) {
    _sequence += 1;
    _frames.add(
      ProjectUiEventFrame(
        ProjectUiEvent(
          projectId: snapshot.project.id,
          seq: _sequence,
          eventId: 'event-$_sequence',
          schemaVersion: 1,
          kind: kind,
          aggregateType: aggregateType,
          aggregateId: aggregateId,
          aggregateRevision: 1,
          createdAt: '2026-07-30T12:00:00Z',
          payload: payload,
        ),
      ),
    );
  }
}

ProjectWorkroomSnapshot workroomFixture({int projectId = 1}) {
  final overview = WorkroomSurfaceState(
    document: surfaceFixture(
      projectId: projectId,
      surfaceType: 'project.overview',
      instanceId: 'surface-overview',
      sourceSeq: 40,
    ),
    feedbackVote: 0,
  );
  return ProjectWorkroomSnapshot(
    project: WorkroomProject(
      id: projectId,
      name: 'Fixer MCP',
      cwd: '/tmp/self_orchestration',
    ),
    protocolVersion: 1,
    watermarkSeq: 40,
    threads: const [
      WorkroomFixerThread(
        id: 'fixer-thread',
        headline: 'Production Fixer',
        provider: 'codex',
        state: 'active',
        createdAt: '2026-07-30T10:00:00Z',
        updatedAt: '2026-07-30T12:00:00Z',
      ),
    ],
    selectedThreadId: 'fixer-thread',
    turns: const [
      WorkroomFixerTurn(
        id: 'turn-1',
        threadId: 'fixer-thread',
        ordinal: 1,
        role: 'fixer',
        content: 'Project state is ready.',
        status: 'complete',
        createdAt: '2026-07-30T12:00:00Z',
      ),
    ],
    activeSurface: overview,
    surfaceHistory: [overview],
    hands: const WorkroomHandsState(
      actorId: 'hands-project-1',
      displayName: 'Руки',
      authorityState: 'enabled',
      operationalState: 'awaiting_review',
      selectedLane: 'codex',
      queueDepth: 0,
      activeLeaseSummary: 'fixer_mcp/dashboard_app',
      lanes: [
        HandsProviderLane(
          provider: 'codex',
          model: 'gpt-5.6-luna',
          reasoning: 'high',
        ),
        HandsProviderLane(
          provider: 'claude',
          model: 'sonnet',
          reasoning: 'high',
        ),
        HandsProviderLane(
          provider: 'kimi-code',
          model: 'kimi-k3-256k',
          reasoning: 'default',
        ),
        HandsProviderLane(
          provider: 'antigravity',
          model: 'default',
          reasoning: 'default',
        ),
      ],
      instructions: [
        HandsInstruction(
          id: 'instruction-1',
          ordinal: 1,
          instructionText: 'Implement the governed Flutter workroom.',
          state: 'awaiting_review',
          stateReasonCode: '',
          stateReasonText: '',
          requestedLane: 'codex',
          declaredWriteScope: ['fixer_mcp/dashboard_app'],
          issuer: 'architect',
          createdAt: '2026-07-30T11:00:00Z',
          updatedAt: '2026-07-30T12:00:00Z',
          events: [
            HandsInstructionEvent(
              ordinal: 1,
              eventType: 'generation.completed',
              fromState: 'running',
              toState: 'awaiting_review',
              detail: 'Structured report received.',
              createdAt: '2026-07-30T12:00:00Z',
            ),
          ],
          report: 'All focused checks passed.',
          repositoryDiff: '3 files changed',
          reviewReference: 'session:550',
          generation: 1,
        ),
      ],
      selectedInstructionId: 'instruction-1',
    ),
    capabilities: WorkroomCapabilities.permissiveLocal,
  );
}

GenUiSurfaceDocument surfaceFixture({
  required int projectId,
  required String surfaceType,
  required String instanceId,
  required int sourceSeq,
}) {
  final key = '$surfaceType.v1';
  final label = switch (key) {
    'wave.list.v1' => 'Wave #145 · gpt-5.6-sol',
    'backlog.list.v1' => 'Integrate the hub',
    'docs.tree.v1' => 'Codex Hub Desktop Migration Brief',
    'execution.list.v1' => 'Flutter App Shell for the Fixer MCP GUI.',
    'skills.catalog.v1' => 'init-fixer',
    'research.legal.v1' => 'Юридический ресерч',
    'unsupported.request.v1' => 'Unsupported surface request',
    _ => 'Project state is ready.',
  };
  return GenUiSurfaceDocument.fromJson({
    'protocol': 'fixer.genui',
    'protocol_version': 1,
    'instance_id': instanceId,
    'surface_type': surfaceType,
    'surface_version': 1,
    'project_id': projectId,
    'revision': 1,
    'source_seq': sourceSeq,
    'title': key,
    'generated_at': '2026-07-30T12:00:00Z',
    if (surfaceType == 'unsupported.request') 'demand_example_id': 'demand-123',
    'components': [
      {'kind': 'text', 'id': 'heading', 'variant': 'heading', 'text': label},
      if (surfaceType == 'research.legal') ...[
        {
          'kind': 'key_value',
          'id': 'research-source',
          'rows': [
            {'label': 'Источник', 'value': 'research/legal/legal_memo.md'},
          ],
        },
        {
          'kind': 'markdown',
          'id': 'legal-research',
          'source': '# Юридический меморандум\n\nКанонический ресерч проекта.',
        },
      ] else if (surfaceType == 'unsupported.request')
        {
          'kind': 'callout',
          'id': 'unsupported',
          'tone': 'warning',
          'title': 'Unsupported request',
          'body': 'Demand example demand-123 was recorded.',
        }
      else
        {
          'kind': 'status_badge',
          'id': 'state',
          'label': 'live',
          'tone': 'success',
        },
    ],
    'actions': <Map<String, dynamic>>[],
  });
}
