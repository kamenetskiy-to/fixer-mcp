import 'package:fixer_dashboard_app/src/dashboard_logic.dart';
import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'derives grouped autonomous history and activity labels from session history',
    () {
      final project = const ProjectRecord(
        id: 1,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
      );

      final data = buildProjectDashboardData(
        project,
        const [
          SessionRecord(
            id: 11,
            localId: 1,
            projectId: 1,
            taskDescription: 'Autonomous reconnaissance session',
            status: 'completed',
            report: '',
          ),
          SessionRecord(
            id: 23,
            localId: 2,
            projectId: 1,
            taskDescription: 'Autonomous follow-up session',
            status: 'completed',
            report: '',
          ),
          SessionRecord(
            id: 41,
            localId: 3,
            projectId: 1,
            taskDescription: 'Routine maintenance',
            status: 'completed',
            report: '',
          ),
          SessionRecord(
            id: 56,
            localId: 4,
            projectId: 1,
            taskDescription: 'Autonomous repair pass',
            status: 'in_progress',
            report: '',
          ),
          SessionRecord(
            id: 68,
            localId: 5,
            projectId: 1,
            taskDescription: 'Autonomous wrap-up',
            status: 'pending',
            report: '',
          ),
        ],
        null,
        null,
      );

      expect(data.latestActivitySessionId, 68);
      expect(data.latestActivityLocalSessionId, 5);
      expect(data.latestActivityLabel, contains('#5'));
      expect(data.autonomousRun.hasRun, isTrue);
      expect(data.autonomousRun.source, 'derived from session history');
      expect(data.autonomousRun.groups, hasLength(2));
      expect(data.autonomousRun.groups.first.stateLabel, 'completed');
      expect(data.autonomousRun.groups.last.stateLabel, 'running');
      expect(data.autonomousRun.currentStep, contains('#4 in_progress'));
      expect(data.autonomousRun.lastCompletedStep, contains('#2 completed'));
      expect(data.autonomousRun.nextStep, contains('#5 pending'));
      expect(data.sessions.first.localId, 1);
      expect(data.sessions.last.localId, 5);
    },
  );

  test('prefers explicit autonomous status over derived history', () {
    final project = const ProjectRecord(
      id: 1,
      name: 'Fixer MCP',
      cwd: '/tmp/self_orchestration',
    );

    final data = buildProjectDashboardData(
      project,
      const [
        SessionRecord(
          id: 1,
          localId: 1,
          projectId: 1,
          taskDescription: 'Autonomous reconnaissance session',
          status: 'completed',
          report: '',
        ),
        SessionRecord(
          id: 8,
          localId: 2,
          projectId: 1,
          taskDescription: 'Autonomous follow-up session',
          status: 'completed',
          report: '',
        ),
      ],
      const AutonomousStatusRecord(
        projectId: 1,
        sessionId: 8,
        state: 'blocked',
        summary: 'Waiting on a manual unblock',
        updatedAt: '2026-03-19 23:00:00',
        blocker: 'Need environment access',
        evidence: 'status row',
      ),
      null,
    );

    expect(data.autonomousRun.stateLabel, 'blocked');
    expect(data.autonomousRun.summary, 'Waiting on a manual unblock');
    expect(data.autonomousRun.latestActivitySessionId, 8);
    expect(data.autonomousRun.latestActivityLocalSessionId, 2);
    expect(data.autonomousRun.latestActivityLabel, contains('status #2'));
  });

  test('does not treat incidental "autonomous" prose as an autonomous run', () {
    final project = const ProjectRecord(
      id: 8,
      name: 'French Exam',
      cwd: '/tmp/french_exam',
    );

    final data = buildProjectDashboardData(
      project,
      const [
        SessionRecord(
          id: 241,
          localId: 20,
          projectId: 8,
          taskDescription:
              '[SCREENSHOT-CAPTURES-V1] This session prepares assets without autonomous intervention.',
          status: 'completed',
          report: '',
        ),
      ],
      null,
      null,
    );

    expect(data.autonomousRun.hasRun, isFalse);
    expect(data.autonomousRun.groups, isEmpty);
  });
}
