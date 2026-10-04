import 'package:fixer_dashboard_app/src/architect_cockpit.dart';
import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:fixer_dashboard_app/src/dashboard_runtime_client.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('loads all architect orders with one backend request', () async {
    final runtimeClient = _FakeDashboardRuntimeClient();
    final repository = BridgeArchitectCockpitRepository(
      runtimeClient: runtimeClient,
    );
    final orders = await repository.loadWeeklyOrders();

    expect(runtimeClient.requests, ['/api/architect/orders']);
    expect(orders, hasLength(1));
    expect(orders.single.projectName, 'Checkout');
  });

  testWidgets('shows weekly branch status, diff placeholder, and decisions', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1400, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final repository = _FakeArchitectCockpitRepository();
    await tester.pumpWidget(
      MaterialApp(home: ArchitectCockpitScreen(repository: repository)),
    );
    await tester.pumpAndSettle();

    expect(find.text('Weekly consolidation'), findsOneWidget);
    expect(find.text('Checkout refresh'), findsWidgets);
    expect(find.text('Review'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('architect-order-41')));
    await tester.pumpAndSettle();
    expect(find.text('Basic diff viewer'), findsOneWidget);
    expect(
      find.text('+ fixer_mcp/dashboard_app/lib/main.dart'),
      findsOneWidget,
    );

    await tester.tap(find.text('Merge'));
    await tester.pumpAndSettle();
    expect(repository.statusChanges, [(41, 'completed')]);
    expect(
      find.text('Branch merged into the weekly consolidation.'),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const ValueKey('architect-order-42')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Reject'));
    await tester.pumpAndSettle();
    expect(repository.statusChanges, [(41, 'completed'), (42, 'pending')]);
  });
}

class _FakeDashboardRuntimeClient extends DashboardRuntimeClient {
  final requests = <String>[];

  @override
  Future<Map<String, dynamic>> readDashboardJson(String path) async {
    requests.add(path);
    return {
      'projects': [
        {'id': 7, 'name': 'Checkout', 'cwd': '/tmp/checkout'},
        {'id': 8, 'name': 'Receipts', 'cwd': '/tmp/receipts'},
      ],
      'sessions': [
        {
          'id': 41,
          'local_id': 1,
          'project_id': 7,
          'headline': 'Checkout refresh',
          'task_preview': 'Improve checkout conversion.',
          'status': 'review',
          'backend': 'codex',
          'model': 'gpt-5.6',
          'reasoning': 'high',
          'attached_doc_count': 0,
          'mcp_count': 0,
          'proposal_count': 0,
          'pending_proposal_count': 0,
          'worker_state': {
            'running_count': 0,
            'has_running': false,
            'processes': <Object>[],
          },
          'rework_count': 0,
          'forced_stop_count': 0,
        },
      ],
    };
  }
}

class _FakeArchitectCockpitRepository implements ArchitectCockpitRepository {
  final statusChanges = <(int, String)>[];

  final _orders = [
    const ArchitectOrderRecord(
      sessionId: 41,
      localSessionId: 1,
      projectId: 7,
      projectName: 'Checkout',
      branchName: 'session/1',
      headline: 'Checkout refresh',
      taskPreview: 'Improve checkout conversion.',
      status: 'review',
      buildStatus: 'Built',
      reviewerStatus: 'Awaiting review',
      workerRunning: false,
    ),
    const ArchitectOrderRecord(
      sessionId: 42,
      localSessionId: 2,
      projectId: 7,
      projectName: 'Checkout',
      branchName: 'session/2',
      headline: 'Receipt emails',
      taskPreview: 'Send receipts after payment.',
      status: 'in_progress',
      buildStatus: 'Building',
      reviewerStatus: 'In progress',
      workerRunning: true,
    ),
  ];

  @override
  Future<List<ArchitectOrderRecord>> loadWeeklyOrders() async => _orders;

  @override
  Future<NetrunnerDetailSnapshot> loadOrderDetail(int sessionId) async =>
      _detail(sessionId);

  @override
  Future<NetrunnerDetailSnapshot> setOrderStatus(
    int sessionId,
    String status,
  ) async {
    statusChanges.add((sessionId, status));
    return _detail(sessionId);
  }

  NetrunnerDetailSnapshot _detail(int sessionId) {
    return NetrunnerDetailSnapshot(
      session: SessionDetailRecord(
        id: sessionId,
        localId: sessionId == 41 ? 1 : 2,
        projectId: 7,
        taskDescription: 'Task',
        status: 'review',
        backend: 'codex',
        model: 'gpt-5.6',
        reasoning: 'high',
        reportRaw: '',
        structuredFinalReport: const FinalReportRecord(
          filesChanged: ['fixer_mcp/dashboard_app/lib/main.dart'],
          commandsRun: [],
          checksRun: ['flutter test'],
          blockers: [],
          residualRisks: [],
          cleanupClaims: {},
        ),
        attachedDocs: const [],
        mcpServers: const [],
        proposals: const [],
        workerState: const WorkerStateSummary(
          runningCount: 0,
          hasRunning: false,
          processes: [],
        ),
        reworkCount: 0,
        forcedStopCount: 0,
        repairSourceSessionId: 0,
        localRepairSourceId: 0,
        availableDocs: const [],
        availableMcpServers: const [],
        allowedStatusTargets: const ['completed', 'pending'],
        statusActionNote: '',
      ),
    );
  }
}
