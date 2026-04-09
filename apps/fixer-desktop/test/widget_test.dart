import 'package:fixer_desktop/bridge_repository.dart';
import 'package:fixer_desktop/main.dart';
import 'package:fixer_desktop/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('renders project dashboard and session workspace', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      FixerDesktopApp(
        repository: _FakeDesktopBridgeRepository(),
        bridgeUrl: 'http://127.0.0.1:8765',
      ),
    );

    await tester.pumpAndSettle();

    expect(find.text('Fixer Desktop'), findsOneWidget);
    expect(find.text('Overseer'), findsOneWidget);
    expect(find.text('Fixer MCP'), findsWidgets);
    expect(find.text('Session 92'), findsWidgets);
    expect(find.text('Task brief'), findsOneWidget);
    expect(find.text('fixer_mcp'), findsOneWidget);
    expect(find.text('PID 44123'), findsOneWidget);
  });
}

class _FakeDesktopBridgeRepository implements DesktopBridgeRepository {
  @override
  Future<ProjectDashboard> fetchDashboard(int projectId) async {
    return ProjectDashboard(
      id: 2,
      name: 'Fixer MCP',
      cwd: '/tmp/self_orchestration',
      sessionCounts: const {'in_progress': 1, 'review': 1},
      pendingDocProposals: 1,
      activeWorkerCount: 1,
      runStatus: RunStatus(
        state: 'running',
        summary: 'Desktop slice in progress',
        focus: 'desktop bridge + app shell',
      ),
      sessions: [
        DashboardSession(
          id: 92,
          status: 'in_progress',
          taskTitle: 'Build the first desktop slice',
          taskDescription: 'Create a bridge and first desktop shell.',
          cliBackend: 'codex',
          cliModel: 'gpt-5.4',
          cliReasoning: 'high',
          attachedDocCount: 2,
          pendingProposalCount: 1,
        ),
      ],
    );
  }

  @override
  Future<List<ProjectSummary>> fetchProjects() async {
    return [
      ProjectSummary(
        id: 2,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
        sessionCounts: const {'in_progress': 1, 'review': 1},
        pendingDocProposals: 1,
        activeWorkerCount: 1,
        latestRunStatus: RunStatus(
          state: 'running',
          summary: 'Desktop slice in progress',
          focus: 'desktop bridge + app shell',
        ),
      ),
    ];
  }

  @override
  Future<SessionDetail> fetchSession(int sessionId) async {
    return SessionDetail(
      id: 92,
      status: 'in_progress',
      taskTitle: 'Build the first desktop slice',
      taskDescription: 'Create a bridge and first desktop shell.',
      cliBackend: 'codex',
      cliModel: 'gpt-5.4',
      cliReasoning: 'high',
      attachedDocs: [
        AttachedDoc(id: 11, title: 'Migration Plan', docType: 'architecture'),
      ],
      mcpServers: [
        McpServerEntry(
          name: 'fixer_mcp',
          category: 'Control',
          shortDescription: 'Fixer orchestration tools',
          howTo: 'Use for project-bound operations.',
        ),
      ],
      docProposals: [
        DocProposalEntry(
          id: 7,
          status: 'pending',
          proposedDocType: 'architecture',
        ),
      ],
      workerProcesses: [
        WorkerProcessEntry(
          pid: 44123,
          status: 'running',
          updatedAt: '2026-04-04 10:00:01',
        ),
      ],
    );
  }
}
