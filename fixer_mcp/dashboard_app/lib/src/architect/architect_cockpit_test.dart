// This scoped test is run directly by the Netrunner; keep it beside the
// scoped implementation because the wave write boundary excludes test/.
// ignore_for_file: depend_on_referenced_packages

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../dashboard_models.dart';
import 'architect_cockpit.dart';

void main() {
  testWidgets('reviews code diff and approves a doc proposal', (tester) async {
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
    expect(find.textContaining('Review-netrunner'), findsWidgets);
    expect(find.text('Checkout refresh'), findsWidgets);
    expect(find.text('Basic diff viewer'), findsOneWidget);
    expect(find.text('+ lib/checkout.dart'), findsOneWidget);
    expect(find.text('Doc proposals'), findsOneWidget);

    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();
    expect(repository.proposalStatusChanges, [(501, 'approved')]);
    expect(find.text('Doc proposal approved into main.'), findsOneWidget);

    await tester.tap(find.text('Merge'));
    await tester.pumpAndSettle();
    expect(repository.orderStatusChanges, [(41, 'completed')]);
  });
}

class _FakeArchitectCockpitRepository implements ArchitectCockpitRepository {
  final orderStatusChanges = <(int, String)>[];
  final proposalStatusChanges = <(int, String)>[];
  String _proposalStatus = 'pending';

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
  ];

  @override
  Future<List<ArchitectOrderRecord>> loadWeeklyOrders() async => _orders;

  @override
  Future<NetrunnerDetailSnapshot> loadOrderDetail(int sessionId) async =>
      _detail();

  @override
  Future<NetrunnerDetailSnapshot> setOrderStatus(
    int sessionId,
    String status,
  ) async {
    orderStatusChanges.add((sessionId, status));
    return _detail();
  }

  @override
  Future<NetrunnerDetailSnapshot> setProposalStatus(
    int proposalId,
    String status,
  ) async {
    proposalStatusChanges.add((proposalId, status));
    _proposalStatus = status;
    return _detail();
  }

  NetrunnerDetailSnapshot _detail() {
    return NetrunnerDetailSnapshot(
      session: SessionDetailRecord(
        id: 41,
        localId: 1,
        projectId: 7,
        taskDescription: 'Improve checkout conversion.',
        status: 'review',
        backend: 'codex',
        model: 'gpt-5.6',
        reasoning: 'high',
        reportRaw: '',
        structuredFinalReport: const FinalReportRecord(
          filesChanged: ['lib/checkout.dart'],
          commandsRun: ['flutter test'],
          checksRun: ['flutter test'],
          blockers: [],
          residualRisks: [],
          cleanupClaims: {},
        ),
        attachedDocs: const [],
        mcpServers: const [],
        proposals: [
          DocProposalSummaryRecord(
            id: 501,
            localId: 1,
            status: _proposalStatus,
            proposedDocType: 'runbook',
            proposedContent: 'Document the checkout retry flow.',
            targetProjectDocId: 12,
          ),
        ],
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
