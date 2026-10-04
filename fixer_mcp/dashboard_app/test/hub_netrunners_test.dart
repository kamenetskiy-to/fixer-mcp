import 'package:fixer_dashboard_app/src/dashboard_runtime_client.dart';
import 'package:fixer_dashboard_app/src/hub/netrunners/netrunners.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses the grouped API response newest-first', () {
    final snapshot = NetrunnerExplorerSnapshot.fromJson(_payload());

    expect(snapshot.waveGroups.map((group) => group.waveId), [8, 7]);
    expect(snapshot.waveGroups.last.sessions.map((session) => session.id), [
      12,
      11,
      10,
    ]);
    expect(snapshot.ungroupedSessions.single.kind, 'legacy');
  });

  test('bridge repository requests the grouped project endpoint', () async {
    final client = _FakeRuntimeClient();
    final repository = BridgeNetrunnerExplorerRepository(runtimeClient: client);

    final snapshot = await repository.loadProjectNetrunners(42);

    expect(client.requests, ['/api/projects/42/netrunners']);
    expect(snapshot.waveGroups.first.waveId, 8);
  });

  testWidgets(
    'shows wave roles, launch-gated backend details, and independent filters',
    (tester) async {
      tester.view.physicalSize = const Size(1200, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final selected = <int>[];

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NetrunnerExplorer(
              snapshot: NetrunnerExplorerSnapshot.fromJson(_payload()),
              onSessionSelected: (session) => selected.add(session.id),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Wave 8'), findsOneWidget);
      expect(find.text('Wave 7'), findsOneWidget);
      expect(find.text('Ungrouped sessions'), findsOneWidget);
      expect(find.text('Worker'), findsWidgets);
      expect(find.text('Reviewer'), findsOneWidget);
      expect(find.text('Manual'), findsOneWidget);
      expect(find.text('Legacy'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('netrunner-backend-10')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('netrunner-backend-20')), findsNothing);

      final waveEightY = tester
          .getTopLeft(find.byKey(const ValueKey('netrunner-wave-8')))
          .dy;
      final waveSevenY = tester
          .getTopLeft(find.byKey(const ValueKey('netrunner-wave-7')))
          .dy;
      expect(waveEightY, lessThan(waveSevenY));

      await tester.tap(find.byKey(const ValueKey('netrunner-filter-review')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('netrunner-session-12')), findsNothing);
      expect(
        find.byKey(const ValueKey('netrunner-session-11')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('netrunner-wave-8')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('netrunner-filter-pending')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('netrunner-wave-8')), findsNothing);
      expect(find.byKey(const ValueKey('netrunner-wave-7')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('netrunner-session-11')));
      expect(selected, [11]);
    },
  );
}

class _FakeRuntimeClient extends DashboardRuntimeClient {
  final requests = <String>[];

  @override
  Future<Map<String, dynamic>> readDashboardJson(String path) async {
    requests.add(path);
    return _payload();
  }
}

Map<String, dynamic> _payload() {
  return {
    'wave_groups': [
      {
        'wave_id': 7,
        'wave_identity': 'wave-7',
        'status': 'completed',
        'created_at': '2026-07-20T09:00:00Z',
        'updated_at': '2026-07-20T12:00:00Z',
        'launched_at': '2026-07-20T09:05:00Z',
        'completed_at': '2026-07-20T12:00:00Z',
        'worker_count': 1,
        'reviewer_count': 1,
        'manual_count': 1,
        'sessions': [
          _session(
            id: 10,
            localId: 10,
            waveId: 7,
            kind: 'worker',
            headline: 'Repository worker',
            status: 'completed',
            membershipStatus: 'review_ready',
            backend: 'codex',
            model: 'gpt-5.6',
            reasoning: 'high',
            launchedAt: '2026-07-20T09:05:00Z',
          ),
          _session(
            id: 11,
            localId: 11,
            waveId: 7,
            kind: 'manual',
            headline: 'Architect verification',
            status: 'in_progress',
            membershipStatus: 'running',
            backend: 'droid',
            model: 'sonnet',
            reasoning: 'high',
            launchedAt: '2026-07-20T11:45:00Z',
          ),
          _session(
            id: 12,
            localId: 12,
            waveId: 7,
            kind: 'reviewer',
            headline: 'Post-wave review',
            status: 'review',
            membershipStatus: 'exited',
            backend: 'codex',
            model: 'gpt-5.6',
            reasoning: 'high',
            launchedAt: '2026-07-20T11:05:00Z',
          ),
        ],
      },
      {
        'wave_id': 8,
        'wave_identity': 'wave-8',
        'status': 'created',
        'created_at': '2026-07-21T09:00:00Z',
        'updated_at': '2026-07-21T09:00:00Z',
        'worker_count': 1,
        'reviewer_count': 0,
        'manual_count': 0,
        'sessions': [
          _session(
            id: 20,
            localId: 20,
            waveId: 8,
            kind: 'worker',
            headline: 'Pending worker',
            status: 'pending',
            membershipStatus: 'created',
            backend: 'codex',
            model: 'must-not-render',
            reasoning: 'high',
            launchedAt: '',
          ),
        ],
      },
    ],
    'ungrouped_sessions': [
      _session(
        id: 13,
        localId: 13,
        waveId: 0,
        kind: 'legacy',
        headline: 'Legacy cleanup',
        status: 'completed',
        membershipStatus: '',
        backend: '',
        model: '',
        reasoning: '',
        launchedAt: '',
      ),
    ],
  };
}

Map<String, dynamic> _session({
  required int id,
  required int localId,
  required int waveId,
  required String kind,
  required String headline,
  required String status,
  required String membershipStatus,
  required String backend,
  required String model,
  required String reasoning,
  required String launchedAt,
}) {
  return {
    'id': id,
    'local_id': localId,
    'project_id': 2,
    if (waveId > 0) 'wave_id': waveId,
    'role': 'netrunner',
    'kind': kind,
    'headline': headline,
    'task_preview': '$headline details',
    'status': status,
    'membership_status': membershipStatus,
    'backend': backend,
    'model': model,
    'reasoning': reasoning,
    'created_at': '2026-07-20T09:00:00Z',
    'updated_at': '2026-07-20T10:00:00Z',
    'launched_at': launchedAt,
    'completed_at': '',
  };
}
