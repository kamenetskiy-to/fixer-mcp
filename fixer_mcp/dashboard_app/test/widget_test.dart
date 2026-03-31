import 'dart:ui';
import 'dart:async';

import 'package:fixer_dashboard_app/main.dart';
import 'package:fixer_dashboard_app/src/dashboard_logic.dart';
import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:fixer_dashboard_app/src/dashboard_repository.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';

class FakeStreamingDashboardRepository implements DashboardRepository {
  FakeStreamingDashboardRepository(DashboardSnapshot snapshot)
    : _snapshot = snapshot {
    _controller.add(snapshot);
  }

  final StreamController<DashboardSnapshot> _controller =
      StreamController<DashboardSnapshot>();
  DashboardSnapshot _snapshot;

  void push(DashboardSnapshot snapshot) {
    _snapshot = snapshot;
    _controller.add(snapshot);
  }

  @override
  Future<DashboardSnapshot> loadSnapshot() async => _snapshot;

  @override
  Stream<DashboardSnapshot> watchSnapshot({
    Duration interval = const Duration(seconds: 2),
  }) {
    return _controller.stream;
  }

  Future<void> dispose() async {
    await _controller.close();
  }
}

void main() {
  testWidgets('updates from the live stream and opens autonomous run details', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final initialProject = buildProjectDashboardData(
      const ProjectRecord(
        id: 1,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
      ),
      [
        const SessionRecord(
          id: 11,
          localId: 1,
          projectId: 1,
          taskDescription: 'Autonomous dashboard session',
          status: 'completed',
          report: '',
        ),
        const SessionRecord(
          id: 27,
          localId: 2,
          projectId: 1,
          taskDescription: 'Routine maintenance',
          status: 'pending',
          report:
              '## Routine notes\n\n- pending verification\n- waiting for unblock',
        ),
      ],
      const AutonomousStatusRecord(
        projectId: 1,
        sessionId: 1,
        state: 'completed',
        summary: 'Initial pass complete',
        focus: 'project list',
        evidence: 'explicit status row',
        updatedAt: '2026-03-19 23:00:00',
      ),
      null,
    );
    final updatedProject = buildProjectDashboardData(
      const ProjectRecord(
        id: 1,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
      ),
      [
        const SessionRecord(
          id: 11,
          localId: 1,
          projectId: 1,
          taskDescription: 'Autonomous dashboard session',
          status: 'completed',
          report: '',
        ),
        const SessionRecord(
          id: 27,
          localId: 2,
          projectId: 1,
          taskDescription: 'Routine maintenance',
          status: 'pending',
          report:
              '## Routine notes\n\n- pending verification\n- waiting for unblock',
        ),
        const SessionRecord(
          id: 41,
          localId: 3,
          projectId: 1,
          taskDescription: 'Autonomous follow-up session',
          status: 'in_progress',
          report: 'Detailed runner report for autonomous follow-up session.',
        ),
        const SessionRecord(
          id: 58,
          localId: 4,
          projectId: 1,
          taskDescription: 'Autonomous wrap-up',
          status: 'pending',
          report: '',
        ),
      ],
      const AutonomousStatusRecord(
        projectId: 1,
        sessionId: 3,
        state: 'running',
        summary: 'Scanning the current status surface',
        focus: 'project list',
        evidence: 'explicit status row',
        updatedAt: '2026-03-19 23:10:00',
      ),
      null,
    );

    final snapshot = DashboardSnapshot(
      databasePath: '/tmp/fixer.db',
      projects: [initialProject],
    );
    final repository = FakeStreamingDashboardRepository(snapshot);
    await tester.pumpWidget(FixerDashboardApp(repository: repository));
    await tester.pump();
    await tester.pump();

    expect(find.text('Fixer MCP Dashboard'), findsOneWidget);
    expect(find.byKey(const ValueKey('autonomous-run-card-1')), findsOneWidget);
    expect(find.text('running'), findsNothing);

    repository.push(
      DashboardSnapshot(
        databasePath: '/tmp/fixer.db',
        projects: [updatedProject],
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(find.byKey(const ValueKey('autonomous-run-card-2')), findsOneWidget);
    expect(find.text('running'), findsWidgets);
    expect(find.text('#3 Autonomous follow-up session'), findsNothing);
    expect(find.text('#2 Routine maintenance'), findsOneWidget);
    expect(find.byKey(const ValueKey('session-report-27')), findsNothing);
    expect(
      find.byKey(const ValueKey('session-report-toggle-27')),
      findsOneWidget,
    );

    await tester.tap(find.text('#2 Routine maintenance'));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('session-report-27')), findsOneWidget);
    expect(find.text('pending verification'), findsOneWidget);

    final runCard = find.byKey(const ValueKey('autonomous-run-card-2'));
    await tester.tap(runCard);
    await tester.pumpAndSettle();

    expect(find.textContaining('Fixer MCP · Run #2'), findsOneWidget);
    expect(find.text('Sessions #3-#4'), findsWidgets);
    expect(find.text('Global sessions #41-#58'), findsWidgets);
    expect(find.text('#3 Autonomous follow-up session'), findsOneWidget);
    expect(find.text('Global #41'), findsWidgets);
    expect(find.byKey(const ValueKey('session-report-41')), findsNothing);
    expect(
      find.byKey(const ValueKey('session-report-toggle-41')),
      findsOneWidget,
    );

    await tester.tap(find.text('#3 Autonomous follow-up session').last);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const ValueKey('session-report-toggle-41')),
      findsOneWidget,
    );
    expect(find.byKey(const ValueKey('session-report-41')), findsOneWidget);

    await repository.dispose();
  });
}
