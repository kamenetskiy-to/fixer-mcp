import 'dart:async';

import 'package:fixer_dashboard_app/src/dashboard_runtime_client.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_models.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_repository.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses the canonical Mission Control wave contract', () {
    final snapshot = MissionControlWavesSnapshot.fromJson(_payload());

    expect(snapshot.projectId, 7);
    expect(snapshot.generatedAt, '2026-07-29T12:00:00Z');
    expect(snapshot.freshness.state, 'fresh');
    expect(snapshot.freshness.ageSeconds, 30);
    expect(snapshot.plannedWaves, hasLength(2));
    expect(snapshot.waves, hasLength(2));

    final planned = snapshot.plannedWaves.first;
    expect(planned.planId, 501);
    expect(planned.readinessState, 'ready');
    expect(planned.taskCounts.planned, 2);
    expect(planned.tasks.last.dependsOn, ['backend']);
    expect(planned.tasks.last.model, 'gpt-5.6-sol');
    expect(planned.tasks.last.mcpServers, ['dart_flutter', 'fixer_mcp']);
    expect(snapshot.plannedWaves.last.readinessErrors, [
      'frontend task overlaps an already running task',
    ]);

    final repairWave = snapshot.waves.first;
    expect(repairWave.waveId, 102);
    expect(repairWave.needsArchitect, isTrue);
    expect(repairWave.workerCounts.failed, 1);
    expect(repairWave.review.localSessionId, 4);
    expect(repairWave.repair.workerId, 613);
    expect(repairWave.workers.single.backend, 'codex');
    expect(repairWave.workers.single.model, 'gpt-5.6-sol');
    expect(repairWave.workers.single.reasoning, 'high');
    expect(repairWave.workers.single.outcome, 'failed');
    expect(repairWave.isActiveState, isFalse);
    expect(
      repairWave.actionCapabilities.values
          .firstWhere((item) => item.action == 'authorize_repair')
          .disabledReason,
      contains('cannot safely delegate'),
    );
  });

  test('counts only waves with a running lifecycle and active workers', () {
    final payload = _payload();
    final repairWave = (payload['waves'] as List).first as Map<String, dynamic>;
    repairWave['legacy_status'] = 'running';
    repairWave['operator_state'] = 'implementation_active';
    repairWave['worker_counts'] = {
      ...(repairWave['worker_counts'] as Map<String, dynamic>),
      'active': 1,
      'terminal': 0,
    };

    final wave = MissionControlWavesSnapshot.fromJson(payload).waves.first;
    expect(wave.isActiveState, isTrue);

    repairWave['legacy_status'] = 'review_ready';
    final reviewWave = MissionControlWavesSnapshot.fromJson(
      payload,
    ).waves.first;
    expect(reviewWave.isActiveState, isFalse);

    repairWave['legacy_status'] = 'partially_failed';
    final partiallyFailedWave = MissionControlWavesSnapshot.fromJson(
      payload,
    ).waves.first;
    expect(partiallyFailedWave.isActiveState, isTrue);

    (repairWave['worker_counts'] as Map<String, dynamic>)['active'] = 0;
    final staleWave = MissionControlWavesSnapshot.fromJson(payload).waves.first;
    expect(staleWave.isActiveState, isFalse);
  });

  test(
    'repository reads the project route and uses governed action route',
    () async {
      final client = _FakeRuntimeClient(_payload());
      final repository = BridgeMissionControlRepository(runtimeClient: client);

      final snapshot = await repository.loadWaves(7);
      await repository.runWaveAction(7, 102, 'authorize_repair');
      await repository.runWaveAction(7, 501, 'initialize');

      expect(snapshot.waves, hasLength(2));
      expect(client.readPaths, ['/api/projects/7/waves']);
      expect(client.postPaths, [
        '/api/actions/projects/7/waves/102/authorize_repair',
        '/api/actions/projects/7/planned-waves/501/initialize',
      ]);
      expect(repository.lastWaveActionResult.initializedWaveId, 103);
      await expectLater(
        repository.runWaveAction(7, 102, 'rewrite_sqlite'),
        throwsArgumentError,
      );
    },
  );

  test(
    'repository rejects Initialize without a normal wave identity',
    () async {
      final client = _FakeRuntimeClient(
        _payload(),
        initializeResponse: const <String, dynamic>{},
      );
      final repository = BridgeMissionControlRepository(runtimeClient: client);

      await expectLater(
        repository.runWaveAction(7, 501, 'initialize'),
        throwsStateError,
      );
      expect(repository.lastWaveActionResult.initializedWaveId, 0);
    },
  );

  testWidgets(
    'shows planned as a distinct filter with honest ownership and readiness',
    (tester) async {
      tester.view.physicalSize = const Size(1400, 1100);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      final repository = _FakeMissionControlRepository(
        MissionControlWavesSnapshot.fromJson(_payload()),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MissionControlWavesView(
              projectId: 7,
              repository: repository,
              pollInterval: const Duration(days: 1),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('mission-filter-planned')));
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('mission-plan-501')), findsOneWidget);
      expect(find.byKey(const ValueKey('mission-plan-502')), findsOneWidget);
      expect(find.byKey(const ValueKey('mission-wave-102')), findsNothing);
      expect(find.text('PLANNED, NOT INITIALIZED'), findsOneWidget);
      expect(
        find.textContaining(
          'owns no Netrunner sessions, worktrees, or resolved base SHA',
        ),
        findsOneWidget,
      );
      expect(find.textContaining('gpt-5.6-sol'), findsWidgets);
      expect(find.textContaining('dart_flutter, fixer_mcp'), findsOneWidget);
      expect(find.text('Depends on: backend'), findsOneWidget);
      expect(find.text('ready'), findsWidgets);

      await tester.tap(find.byKey(const ValueKey('mission-plan-502')));
      await tester.pumpAndSettle();
      expect(
        find.text('frontend task overlaps an already running task'),
        findsWidgets,
      );
      expect(
        tester
            .widget<OutlinedButton>(
              find.byKey(const ValueKey('plan-502-action-initialize')),
            )
            .onPressed,
        isNull,
      );
    },
  );

  testWidgets(
    'confirms governed initialization and selects the returned normal wave',
    (tester) async {
      tester.view.physicalSize = const Size(1400, 1100);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      final initializedPayload = _payload();
      final initializedPlan =
          (initializedPayload['planned_waves'] as List).first
              as Map<String, dynamic>;
      initializedPlan['status'] = 'initialized';
      initializedPlan['operator_state'] = 'initialized';
      initializedPlan['label'] = 'Initialized';
      initializedPlan['initialized_wave_id'] = 103;
      initializedPlan['action_capabilities'] = {
        'initialize': {
          'enabled': false,
          'disabled_reason': 'This plan is already initialized as wave 103.',
        },
        'launch': {
          'enabled': false,
          'disabled_reason': 'Use the normal wave lifecycle.',
        },
      };
      final repository = _FakeMissionControlRepository(
        MissionControlWavesSnapshot.fromJson(_payload()),
        initializedSnapshot: MissionControlWavesSnapshot.fromJson(
          initializedPayload,
        ),
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MissionControlWavesView(
              projectId: 7,
              repository: repository,
              pollInterval: const Duration(days: 1),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('mission-filter-planned')));
      await tester.pumpAndSettle();

      final initialize = find.byKey(
        const ValueKey('plan-501-action-initialize'),
      );
      final detailScroll = find.descendant(
        of: find.byKey(const ValueKey('mission-plan-detail-501')),
        matching: find.byType(Scrollable),
      );
      await tester.scrollUntilVisible(
        initialize,
        700,
        scrollable: detailScroll,
      );
      await tester.tap(initialize);
      await tester.pumpAndSettle();
      expect(find.text('Initialize planned wave?'), findsOneWidget);
      await tester.tap(
        find.byKey(const ValueKey('confirm-planned-wave-initialize')),
      );
      await tester.pumpAndSettle();

      expect(repository.actions, [(7, 501, 'initialize')]);
      expect(repository.loadCount, 2);
      expect(
        find.byKey(const ValueKey('mission-wave-detail-103')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'shows all waves, lifecycle evidence, providers, filters, and disabled reasons',
    (tester) async {
      tester.view.physicalSize = const Size(1400, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      final repository = _FakeMissionControlRepository(
        MissionControlWavesSnapshot.fromJson(_payload()),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MissionControlWavesView(
              projectId: 7,
              repository: repository,
              pollInterval: const Duration(days: 1),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Mission Control'), findsOneWidget);
      expect(find.byKey(const ValueKey('mission-wave-102')), findsOneWidget);
      expect(find.byKey(const ValueKey('mission-wave-103')), findsOneWidget);
      expect(find.text('ARCHITECT NEEDED'), findsWidgets);
      expect(find.textContaining('gpt-5.6-sol'), findsOneWidget);
      expect(find.text('Implementation review'), findsOneWidget);
      expect(find.text('Acceptance'), findsWidgets);
      expect(find.text('Active 0'), findsOneWidget);

      final disabledAction = find.byKey(
        const ValueKey('wave-102-action-authorize_repair'),
      );
      final detailScroll = find.descendant(
        of: find.byKey(const ValueKey('mission-wave-detail-102')),
        matching: find.byType(Scrollable),
      );
      await tester.scrollUntilVisible(
        disabledAction,
        600,
        scrollable: detailScroll,
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('cannot safely delegate'), findsWidgets);

      await tester.tap(
        find.byKey(const ValueKey('mission-filter-architectNeeded')),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('mission-wave-102')), findsOneWidget);
      expect(find.byKey(const ValueKey('mission-wave-103')), findsNothing);

      await tester.tap(find.byKey(const ValueKey('mission-control-refresh')));
      await tester.pumpAndSettle();
      expect(repository.loadCount, 2);
    },
  );

  testWidgets('confirms and dispatches only an enabled capability', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(900, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final payload = _payload();
    final firstWave = (payload['waves'] as List).first as Map<String, dynamic>;
    final capabilities =
        firstWave['action_capabilities'] as Map<String, dynamic>;
    capabilities['authorize_repair'] = {'enabled': true, 'disabled_reason': ''};
    final repository = _FakeMissionControlRepository(
      MissionControlWavesSnapshot.fromJson(payload),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MissionControlWavesView(
            projectId: 7,
            repository: repository,
            pollInterval: const Duration(days: 1),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final action = find.byKey(
      const ValueKey('wave-102-action-authorize_repair'),
    );
    final compactScroll = find.byWidgetPredicate(
      (widget) =>
          widget is Scrollable && widget.axisDirection == AxisDirection.down,
    );
    await tester.drag(compactScroll.last, const Offset(0, -800));
    await tester.pumpAndSettle();
    await tester.tap(action);
    await tester.pumpAndSettle();
    expect(find.text('Confirm wave action'), findsOneWidget);

    await tester.tap(
      find.byKey(const ValueKey('confirm-wave-action-authorize_repair')),
    );
    await tester.pumpAndSettle();
    expect(repository.actions, [(7, 102, 'authorize_repair')]);
  });

  testWidgets('polling does not overlap an in-flight refresh', (tester) async {
    final repository = _BlockingMissionControlRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MissionControlWavesView(
            projectId: 7,
            repository: repository,
            pollInterval: const Duration(seconds: 1),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(seconds: 3));
    expect(repository.loadCount, 1);

    repository.completer.complete(
      MissionControlWavesSnapshot.fromJson(_payload()),
    );
    await tester.pump();
    expect(find.text('Mission Control'), findsOneWidget);
  });
}

class _FakeRuntimeClient extends DashboardRuntimeClient {
  _FakeRuntimeClient(
    this.payload, {
    this.initializeResponse = const <String, dynamic>{'wave_id': 103},
  });

  final Map<String, dynamic> payload;
  final Map<String, dynamic> initializeResponse;
  final readPaths = <String>[];
  final postPaths = <String>[];

  @override
  Future<Map<String, dynamic>> readDashboardJson(String path) async {
    readPaths.add(path);
    return payload;
  }

  @override
  Future<Map<String, dynamic>> postDashboardJson(
    String path,
    Map<String, dynamic> payload,
  ) async {
    postPaths.add(path);
    if (path.endsWith('/planned-waves/501/initialize')) {
      return initializeResponse;
    }
    return const <String, dynamic>{};
  }
}

class _FakeMissionControlRepository
    implements MissionControlRepository, MissionControlWaveActionResultSource {
  _FakeMissionControlRepository(this.snapshot, {this.initializedSnapshot});

  MissionControlWavesSnapshot snapshot;
  final MissionControlWavesSnapshot? initializedSnapshot;
  final actions = <(int, int, String)>[];
  int loadCount = 0;
  MissionControlWaveActionResult _lastWaveActionResult =
      const MissionControlWaveActionResult();

  @override
  MissionControlWaveActionResult get lastWaveActionResult =>
      _lastWaveActionResult;

  @override
  Future<MissionControlWavesSnapshot> loadWaves(int projectId) async {
    loadCount += 1;
    return snapshot;
  }

  @override
  Future<void> runWaveAction(int projectId, int waveId, String action) async {
    _lastWaveActionResult = const MissionControlWaveActionResult();
    actions.add((projectId, waveId, action));
    if (action == 'initialize' && initializedSnapshot != null) {
      snapshot = initializedSnapshot!;
      final initializedWaveId = snapshot.plannedWaves
          .firstWhere((plan) => plan.planId == waveId)
          .initializedWaveId;
      _lastWaveActionResult = MissionControlWaveActionResult(
        initializedWaveId: initializedWaveId,
      );
    }
  }
}

class _BlockingMissionControlRepository implements MissionControlRepository {
  final completer = Completer<MissionControlWavesSnapshot>();
  int loadCount = 0;

  @override
  Future<MissionControlWavesSnapshot> loadWaves(int projectId) {
    loadCount += 1;
    return completer.future;
  }

  @override
  Future<void> runWaveAction(int projectId, int waveId, String action) async {}
}

Map<String, dynamic> _payload() {
  Map<String, dynamic> disabled(String reason) => {
    'enabled': false,
    'disabled_reason': reason,
  };
  final disabledReason =
      'Dashboard mutations are disabled because this API cannot safely delegate to the governed Wave Engine; use Fixer MCP.';
  Map<String, dynamic> capabilities() => {
    'launch': disabled('Wave launch is unavailable from this state.'),
    'wait': disabled(disabledReason),
    'authorize_repair': disabled(disabledReason),
    'pause': disabled(disabledReason),
    'resume': disabled('The wave is not paused.'),
    'transition_to_acceptance': disabled('Implementation review is pending.'),
    'complete': disabled('Acceptance is incomplete.'),
    'cleanup': disabled(disabledReason),
  };

  return <String, dynamic>{
    'project_id': 7,
    'generated_at': '2026-07-29T12:00:00Z',
    'freshness': {
      'state': 'fresh',
      'stale': false,
      'source_updated_at': '2026-07-29T11:59:30Z',
      'age_seconds': 30,
      'stale_after_seconds': 300,
    },
    'planned_waves': <Map<String, dynamic>>[
      {
        'plan_id': 501,
        'title': 'Draft Mission Control delivery',
        'status': 'planned',
        'operator_state': 'planned',
        'label': 'Planned',
        'next_action': 'initialize',
        'reason': 'Future work from the grand checklist.',
        'base_ref': 'main',
        'created_at': '2026-07-29T11:50:00Z',
        'updated_at': '2026-07-29T11:59:00Z',
        'task_counts': {'total': 2, 'planned': 2, 'materialized': 0},
        'tasks': <Map<String, dynamic>>[
          {
            'task_id': 5001,
            'key': 'backend',
            'position': 1,
            'task_description': 'Add the governed planned-wave contract.',
            'depends_on': <String>[],
            'backend': 'codex',
            'model': 'gpt-5.6-sol',
            'reasoning': 'high',
            'mcp_servers': ['fixer_mcp'],
          },
          {
            'task_id': 5002,
            'key': 'frontend',
            'position': 2,
            'task_description': 'Render Planned in Flutter Desktop.',
            'depends_on': ['backend'],
            'backend': 'codex',
            'model': 'gpt-5.6-sol',
            'reasoning': 'high',
            'mcp_servers': ['dart_flutter', 'fixer_mcp'],
          },
        ],
        'validation_errors': <String>[],
        'action_capabilities': {
          'initialize': {'enabled': true, 'disabled_reason': ''},
          'launch': {
            'enabled': false,
            'disabled_reason':
                'Initialize this planned definition before launch.',
          },
        },
      },
      {
        'plan_id': 502,
        'title': 'Blocked future delivery',
        'status': 'failed',
        'operator_state': 'initialization_failed',
        'label': 'Initialization failed',
        'next_action': 'retry_initialize',
        'failure_reason': 'frontend task overlaps an already running task',
        'created_at': '2026-07-29T11:45:00Z',
        'updated_at': '2026-07-29T11:58:00Z',
        'task_counts': {'total': 1, 'planned': 1, 'materialized': 0},
        'tasks': <Map<String, dynamic>>[
          {
            'task_id': 5003,
            'key': 'frontend',
            'position': 1,
            'task_description': 'Blocked frontend task.',
            'depends_on': <String>[],
          },
        ],
        'validation_errors': ['frontend task overlaps an already running task'],
        'action_capabilities': {
          'initialize': {
            'enabled': false,
            'disabled_reason': 'Resolve validation errors first.',
          },
          'launch': {
            'enabled': false,
            'disabled_reason':
                'Initialize this planned definition before launch.',
          },
        },
      },
    ],
    'waves': <Map<String, dynamic>>[
      {
        'wave_id': 102,
        'phase': 'implementation',
        'legacy_status': 'review_ready',
        'operator_state': 'repair_blocked',
        'label': 'Repair required',
        'next_action': 'authorize_repair',
        'gate_state': 'implementation_repair',
        'control_state': 'active',
        'control_reason': 'A worker failed before the review gate.',
        'failure_policy_state': 'repair_required',
        'failure_reason': 'Worker 613 failed its widget test.',
        'created_at': '2026-07-29T11:00:00Z',
        'updated_at': '2026-07-29T11:59:30Z',
        'launched_at': '2026-07-29T11:01:00Z',
        'worker_counts': {
          'total': 1,
          'active': 0,
          'terminal': 1,
          'review_ready': 0,
          'completed': 0,
          'failed': 1,
          'stopped': 0,
          'stale_epoch': 0,
          'blocked': 0,
          'retry_pending': 0,
        },
        'review': {
          'session_id': 88,
          'local_session_id': 4,
          'state': 'completed',
        },
        'acceptance': {'state': 'not_started'},
        'repair': {
          'state': 'required',
          'worker_id': 613,
          'session_id': 517,
          'local_session_id': 6,
          'attempt_count': 0,
        },
        'workers': [
          {
            'worker_id': 613,
            'session_id': 517,
            'local_session_id': 6,
            'status': 'failed',
            'session_status': 'review',
            'backend': 'codex',
            'model': 'gpt-5.6-sol',
            'reasoning': 'high',
            'outcome': 'failed',
            'failure_reason': 'Widget test failed.',
            'retry_pending': false,
            'updated_at': '2026-07-29T11:59:30Z',
          },
        ],
        'action_capabilities': capabilities(),
      },
      {
        'wave_id': 103,
        'phase': 'completed',
        'legacy_status': 'completed',
        'operator_state': 'completed',
        'label': 'Wave completed',
        'next_action': 'none',
        'gate_state': 'closed',
        'control_state': 'active',
        'failure_policy_state': 'passed',
        'created_at': '2026-07-29T09:00:00Z',
        'updated_at': '2026-07-29T10:00:00Z',
        'completed_at': '2026-07-29T10:00:00Z',
        'worker_counts': {'total': 0, 'terminal': 0},
        'review': {'state': 'completed'},
        'acceptance': {
          'session_id': 90,
          'local_session_id': 5,
          'state': 'completed',
        },
        'repair': {'state': 'passed', 'attempt_count': 0},
        'workers': <Map<String, dynamic>>[],
        'action_capabilities': capabilities(),
      },
    ],
  };
}
