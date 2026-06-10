import 'package:fixer_dashboard_app/main.dart';
import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:fixer_dashboard_app/src/dashboard_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:markdown_widget/markdown_widget.dart';

class FakeDashboardRepository implements DashboardRepository {
  @override
  Future<HomeSnapshot> loadHomeSnapshot() async => _home;

  @override
  Future<ProjectWorkspaceSnapshot> loadProjectWorkspace(int projectId) async =>
      _project;

  @override
  Future<FixerChatBindingRecord> loadFixerChatBinding(int projectId) async =>
      _project.fixerChat;

  @override
  Future<FixerChatBindingRecord> loadOverseerChatBinding(int projectId) async =>
      _home.defaultChatBinding;

  @override
  Future<NetrunnerDetailSnapshot> loadNetrunnerDetail(int sessionId) async =>
      _detail;

  @override
  Future<ThreadMessagesSnapshot> loadThreadMessages(String threadId) async =>
      threadId == '019fixer-older' ? _olderThreadMessages : _threadMessages;

  @override
  Future<ThreadSendResult> sendThreadMessage(
    String threadId,
    String prompt,
  ) async => ThreadSendResult(
    threadId: threadId,
    turnId: 'turn-new',
    streamId: 'stream-new',
    turnStatusEndpoint: '/turn/status/stream-new',
  );

  @override
  Future<ThreadTurnStatusSnapshot> loadThreadTurnStatus(
    String streamId,
  ) async => const ThreadTurnStatusSnapshot(
    streamId: 'stream-new',
    threadId: '019fixer',
    turnId: 'turn-new',
    done: false,
    eventCount: 2,
    startedAt: '2026-04-28T10:47:00Z',
    completedAt: '',
    assistantText: 'Live assistant text',
    progressText: 'Live assistant text',
    events: [
      ThreadTurnEventRecord(
        sequence: 1,
        receivedAt: '2026-04-28T10:47:01Z',
        method: 'turn/started',
        phase: 'started',
        textDelta: '',
      ),
      ThreadTurnEventRecord(
        sequence: 2,
        receivedAt: '2026-04-28T10:47:02Z',
        method: 'turn/delta',
        phase: 'assistant_delta',
        textDelta: 'Live assistant text',
      ),
    ],
    expired: false,
  );

  @override
  Future<ProjectWorkspaceSnapshot> createTask(
    int projectId, {
    required String taskDescription,
    List<String> declaredWriteScope = const <String>[],
  }) async => _project;

  @override
  Future<NetrunnerDetailSnapshot> setProposalStatus(
    int proposalId,
    String status,
  ) async => _detail;

  @override
  Future<NetrunnerDetailSnapshot> setSessionAttachedDocs(
    int sessionId,
    List<int> projectDocIds,
  ) async => _detail;

  @override
  Future<NetrunnerDetailSnapshot> setSessionMcpServers(
    int sessionId,
    List<String> mcpServerNames,
  ) async => _detail;

  @override
  Future<NetrunnerDetailSnapshot> setSessionStatus(
    int sessionId,
    String status,
  ) async => _detail;
}

final _home = HomeSnapshot(
  currentProject: const ProjectBinding(
    id: 1,
    name: 'Fixer MCP',
    cwd: '/tmp/self_orchestration',
  ),
  defaultChatBinding: FixerChatBindingRecord(
    projectId: 1,
    supported: true,
    defaultSession: FixerChatSessionSummary(
      id: 0,
      localId: 0,
      externalId: '019overseer',
      codexSessionId: '019overseer',
      headline: 'Archived Overseer thread',
      status: 'resume_alias',
      agentRole: 'overseer',
      backend: 'codex',
      model: 'gpt-5.4',
      reasoning: 'medium',
      lastActivityAt: '2026-04-28T09:30:00Z',
      bindingSource: 'codex_session_log+fixer_resume_alias',
      sessionLogPath: '',
      sessionLog: true,
      transcriptAvailable: false,
    ),
    sessions: [
      FixerChatSessionSummary(
        id: 0,
        localId: 0,
        externalId: '019overseer',
        codexSessionId: '019overseer',
        headline: 'Archived Overseer thread',
        status: 'resume_alias',
        agentRole: 'overseer',
        backend: 'codex',
        model: 'gpt-5.4',
        reasoning: 'medium',
        lastActivityAt: '2026-04-28T09:30:00Z',
        bindingSource: 'codex_session_log+fixer_resume_alias',
        sessionLogPath: '',
        sessionLog: true,
        transcriptAvailable: false,
      ),
      FixerChatSessionSummary(
        id: 0,
        localId: 0,
        externalId: '019fixer-older',
        codexSessionId: '019fixer-older',
        headline: 'Earlier Fixer thread',
        status: 'history',
        agentRole: 'fixer',
        backend: 'codex',
        model: 'gpt-5.4',
        reasoning: 'medium',
        lastActivityAt: '2026-04-28T08:30:00Z',
        bindingSource: 'codex_session_log',
        sessionLogPath: '',
        sessionLog: true,
        transcriptAvailable: true,
      ),
    ],
    transcriptAvailability: 'metadata_only',
    residualRisk: 'metadata only',
  ),
  globalCounts: const StatusCounts(
    pending: 1,
    inProgress: 2,
    review: 1,
    completed: 4,
    other: 0,
    total: 8,
  ),
  projects: [
    const ProjectCardRecord(
      project: ProjectBinding(
        id: 1,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
      ),
      counts: StatusCounts(
        pending: 1,
        inProgress: 2,
        review: 1,
        completed: 4,
        other: 0,
        total: 8,
      ),
      latestActivityLabel: '#3 Flutter App Shell',
      latestSessionId: 102,
      latestLocalSessionId: 3,
      autonomous: null,
      hasPendingReview: true,
      hasActiveWorkers: true,
    ),
  ],
  activeWorkers: [
    const ActiveWorkerSummary(
      projectId: 1,
      projectName: 'Fixer MCP',
      sessionId: 102,
      localSessionId: 3,
      headline: 'Flutter App Shell',
      workerState: WorkerStateSummary(
        runningCount: 1,
        hasRunning: true,
        processes: [],
      ),
    ),
  ],
  autonomousSummary: const AutonomousSummary(
    projectsWithStatus: 1,
    runningProjects: 0,
    blockedProjects: 1,
    frozenProjects: 0,
    awaitingReviewProjects: 0,
  ),
);

final _project = ProjectWorkspaceSnapshot(
  project: const ProjectBinding(
    id: 1,
    name: 'Fixer MCP',
    cwd: '/tmp/self_orchestration',
  ),
  metrics: const OverviewMetrics(
    counts: StatusCounts(
      pending: 1,
      inProgress: 2,
      review: 1,
      completed: 4,
      other: 0,
      total: 8,
    ),
    attachedDocCount: 3,
    pendingProposalCount: 2,
    workerState: WorkerStateSummary(
      runningCount: 1,
      hasRunning: true,
      processes: [],
    ),
  ),
  autonomous: const AutonomousStatusRecord(
    projectId: 1,
    sessionId: 102,
    localSessionId: 3,
    state: 'blocked',
    summary: 'Waiting for review',
    focus: 'dashboard shell',
    blocker: '',
    evidence: 'seed',
    orchestrationEpoch: 2,
    orchestrationFrozen: false,
    notificationsEnabledForActiveRun: true,
    updatedAt: '2026-04-28 12:00:00',
  ),
  docs: const DocsSummaryRecord(
    totalDocs: 1,
    groups: [
      DocGroupRecord(
        docType: 'architecture',
        docs: [
          DocSummaryRecord(
            id: 11,
            title: 'Codex Hub Desktop Migration Brief',
            docType: 'architecture',
            contentPreview: 'Bridge-first GUI contract',
            targetedPendingProposals: 1,
          ),
        ],
        pendingProposalCount: 1,
        targetedPendingCount: 1,
        untargetedPendingCount: 0,
      ),
    ],
    pendingProposalCount: 1,
    targetedPendingProposalCount: 1,
    untargetedPendingProposalCount: 0,
  ),
  netrunners: const [
    NetrunnerSummaryRecord(
      id: 102,
      localId: 3,
      projectId: 1,
      headline: 'Flutter App Shell for the Fixer MCP GUI.',
      taskPreview: 'Bridge-backed operator shell',
      status: 'in_progress',
      backend: 'codex',
      model: 'gpt-5.4',
      reasoning: 'medium',
      writeScope: ['fixer_mcp/dashboard_app'],
      attachedDocCount: 2,
      mcpCount: 4,
      proposalCount: 1,
      pendingProposalCount: 1,
      workerState: WorkerStateSummary(
        runningCount: 1,
        hasRunning: true,
        processes: [],
      ),
      reworkCount: 0,
      forcedStopCount: 0,
      repairSourceSessionId: 0,
      localRepairSourceId: 0,
    ),
  ],
  fixerChat: FixerChatBindingRecord(
    projectId: 1,
    supported: true,
    defaultSession: FixerChatSessionSummary(
      id: 0,
      localId: 0,
      externalId: '019fixer',
      codexSessionId: '019fixer',
      headline: 'Active autonomous Fixer thread',
      status: 'active',
      agentRole: 'fixer',
      backend: 'codex',
      model: 'gpt-5.4',
      reasoning: 'medium',
      lastActivityAt: '2026-04-28T10:45:00Z',
      bindingSource: 'codex_session_log+autonomous_state',
      sessionLogPath: '',
      sessionLog: true,
      transcriptAvailable: false,
    ),
    sessions: [
      FixerChatSessionSummary(
        id: 0,
        localId: 0,
        externalId: '019fixer',
        codexSessionId: '019fixer',
        headline: 'Active autonomous Fixer thread',
        status: 'active',
        agentRole: 'fixer',
        backend: 'codex',
        model: 'gpt-5.4',
        reasoning: 'medium',
        lastActivityAt: '2026-04-28T10:45:00Z',
        bindingSource: 'codex_session_log+autonomous_state',
        sessionLogPath: '',
        sessionLog: true,
        transcriptAvailable: false,
      ),
      FixerChatSessionSummary(
        id: 0,
        localId: 0,
        externalId: '019fixer-older',
        codexSessionId: '019fixer-older',
        headline: 'Earlier Fixer thread',
        status: 'history',
        agentRole: 'fixer',
        backend: 'codex',
        model: 'gpt-5.4',
        reasoning: 'medium',
        lastActivityAt: '2026-04-28T08:30:00Z',
        bindingSource: 'codex_session_log',
        sessionLogPath: '',
        sessionLog: true,
        transcriptAvailable: true,
      ),
    ],
    transcriptAvailability: 'metadata_only',
    residualRisk: 'metadata only',
  ),
);

final _detail = NetrunnerDetailSnapshot(
  session: const SessionDetailRecord(
    id: 102,
    localId: 3,
    projectId: 1,
    taskDescription: 'Build the Flutter operator shell',
    status: 'in_progress',
    backend: 'codex',
    model: 'gpt-5.4',
    reasoning: 'medium',
    writeScope: ['fixer_mcp/dashboard_app'],
    reportRaw:
        '{"files_changed":["fixer_mcp/dashboard_app/lib/src/dashboard_view.dart"]}',
    structuredFinalReport: FinalReportRecord(
      filesChanged: ['fixer_mcp/dashboard_app/lib/src/dashboard_view.dart'],
      commandsRun: ['flutter test'],
      checksRun: ['flutter test passed'],
      blockers: [],
      residualRisks: ['Launch controls still intentionally absent'],
      cleanupClaims: {
        'workspace': ['No generated bridge files were modified'],
      },
    ),
    attachedDocs: [
      AttachedDocRecord(
        id: 11,
        title: 'Codex Hub Desktop Migration Brief',
        docType: 'architecture',
        summary: 'Bridge-first GUI contract',
      ),
    ],
    mcpServers: [
      MCPServerAssignmentRecord(
        id: 7,
        name: 'dart_flutter',
        shortDescription: 'Flutter tooling',
        category: 'Coding',
        howTo: 'Use for Flutter code generation and diagnostics.',
      ),
    ],
    proposals: [
      DocProposalSummaryRecord(
        id: 1,
        localId: 1,
        status: 'pending',
        proposedDocType: 'architecture',
        proposedContent: 'Update shell delivery status',
        targetProjectDocId: 11,
      ),
    ],
    workerState: WorkerStateSummary(
      runningCount: 1,
      hasRunning: true,
      processes: [
        WorkerProcessRecord(
          id: 1,
          sessionId: 102,
          localId: 3,
          pid: 4242,
          launchEpoch: 2,
          status: 'running',
          startedAt: '2026-04-28T10:45:00Z',
          updatedAt: '2026-04-28T11:00:00Z',
          stoppedAt: '',
          alive: true,
          stopReason: '',
        ),
      ],
    ),
    reworkCount: 0,
    forcedStopCount: 0,
    repairSourceSessionId: 0,
    localRepairSourceId: 0,
    availableDocs: [
      AttachedDocRecord(
        id: 11,
        title: 'Codex Hub Desktop Migration Brief',
        docType: 'architecture',
        summary: 'Bridge-first GUI contract',
      ),
    ],
    availableMcpServers: [
      MCPServerAssignmentRecord(
        id: 1,
        name: 'sqlite',
        shortDescription: 'SQLite DB',
        category: 'DB',
        howTo: 'Use for local database checks',
      ),
    ],
    allowedStatusTargets: ['in_progress', 'review', 'pending'],
    statusActionNote:
        'Session can move to review when operator validation is complete.',
  ),
);

const _threadMessages = ThreadMessagesSnapshot(
  threadId: '019fixer',
  transcriptAvailable: true,
  availability: 'codex_jsonl',
  unsupportedReason: '',
  sessionLogPath: '/tmp/rollout.jsonl',
  sendSupported: true,
  sendEndpoint: '/turn/start',
  messages: [
    ThreadMessageRecord(
      id: 'm0',
      role: 'user',
      text: '# AGENTS.md instructions for /tmp/project\n\n<INSTRUCTIONS />',
      createdAt: '2026-04-28T10:44:00Z',
      source: 'codex_jsonl',
      kind: 'internal_context',
      summary: 'Internal context: AGENTS.md and environment',
      collapsed: true,
    ),
    ThreadMessageRecord(
      id: 'm-tool',
      role: 'tool',
      text: 'Called fixer_mcp.get_project_handoff({})\n\nOutput:\n{}',
      createdAt: '2026-04-28T10:44:30Z',
      source: 'codex_jsonl',
      kind: 'tool_call',
      summary: 'Called fixer_mcp.get_project_handoff({})',
      collapsed: true,
    ),
    ThreadMessageRecord(
      id: 'm1',
      role: 'user',
      text: 'Please inspect the migration.',
      createdAt: '2026-04-28T10:45:00Z',
      source: 'codex_jsonl',
    ),
    ThreadMessageRecord(
      id: 'm2',
      role: 'assistant',
      text:
          'I am reading the dashboard surface now.\n\n'
          '- `fixer_wire.py` updated\n'
          '- **Tests passed**',
      createdAt: '2026-04-28T10:46:00Z',
      source: 'codex_jsonl',
    ),
  ],
);

const _olderThreadMessages = ThreadMessagesSnapshot(
  threadId: '019fixer-older',
  transcriptAvailable: true,
  availability: 'codex_jsonl',
  unsupportedReason: '',
  sessionLogPath: '/tmp/older-rollout.jsonl',
  sendSupported: true,
  sendEndpoint: '/turn/start',
  messages: [
    ThreadMessageRecord(
      id: 'm3',
      role: 'assistant',
      text: 'Earlier thread context is visible.',
      createdAt: '2026-04-28T08:45:00Z',
      source: 'codex_jsonl',
    ),
  ],
);

void main() {
  testWidgets('renders project workspace and opens netrunner detail', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      FixerDashboardApp(repository: FakeDashboardRepository()),
    );
    await tester.pumpAndSettle();

    expect(find.text('Codex Hub'), findsOneWidget);
    expect(find.text('Projects'), findsOneWidget);
    expect(find.text('Overseer'), findsOneWidget);

    await tester.tap(find.text('#3 Flutter App Shell'));
    await tester.pumpAndSettle();

    expect(find.text('Project'), findsOneWidget);
    expect(find.text('Overview'), findsOneWidget);
    expect(
      find.text('#3 Flutter App Shell for the Fixer MCP GUI.'),
      findsOneWidget,
    );

    await tester.tap(find.widgetWithText(Tab, 'Docs'));
    await tester.pumpAndSettle();
    expect(find.text('Codex Hub Desktop Migration Brief'), findsOneWidget);

    await tester.tap(find.widgetWithText(Tab, 'Netrunners'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('#3 Flutter App Shell for the Fixer MCP GUI.'));
    await tester.pumpAndSettle();

    expect(find.text('Netrunner #3'), findsOneWidget);
    expect(
      find.byKey(const ValueKey('session-task-description')),
      findsOneWidget,
    );
    expect(find.text('Build the Flutter operator shell'), findsWidgets);
    expect(find.text('Summary'), findsOneWidget);
    expect(find.text('Report'), findsOneWidget);
    expect(find.text('Workspace rail'), findsOneWidget);

    await tester.tap(find.widgetWithText(Tab, 'Report'));
    await tester.pumpAndSettle();
    expect(find.text('Files changed'), findsOneWidget);
    expect(find.text('Residual risks'), findsOneWidget);

    await tester.tap(find.widgetWithText(Tab, 'Proposals'));
    await tester.pumpAndSettle();
    expect(find.text('Approve'), findsOneWidget);
    expect(find.text('Reject'), findsOneWidget);
  });

  testWidgets('loads thread messages in the Fixer Chat tab', (tester) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      FixerDashboardApp(repository: FakeDashboardRepository()),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('#3 Flutter App Shell'));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(Tab, 'Fixer Chat'));
    await tester.pumpAndSettle();

    expect(find.text('Please inspect the migration.'), findsOneWidget);
    expect(
      find.text('Internal context: AGENTS.md and environment'),
      findsOneWidget,
    );
    expect(
      find.text('Called fixer_mcp.get_project_handoff({})'),
      findsOneWidget,
    );
    expect(find.byType(MarkdownWidget), findsWidgets);
    expect(find.text('Message this thread'), findsOneWidget);

    await tester.enterText(find.byType(TextField).last, 'Continue live');
    await tester.tap(find.byIcon(Icons.send));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(find.text('Continue live'), findsOneWidget);
    expect(find.text('Live assistant text'), findsOneWidget);

    await tester.tap(find.text('Earlier Fixer thread'));
    await tester.pumpAndSettle();
    expect(find.text('Earlier thread context is visible.'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });
}
