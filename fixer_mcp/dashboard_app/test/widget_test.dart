import 'package:fixer_dashboard_app/main.dart';
import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:fixer_dashboard_app/src/dashboard_repository.dart';
import 'package:fixer_dashboard_app/src/hub/backlog/backlog_models.dart';
import 'package:fixer_dashboard_app/src/hub/backlog/backlog_repository.dart';
import 'package:fixer_dashboard_app/src/hub/docs/documents_explorer.dart';
import 'package:fixer_dashboard_app/src/hub/fixer_chat/fixer_chat.dart';
import 'package:fixer_dashboard_app/src/hub/netrunner_thread/netrunner_thread.dart';
import 'package:fixer_dashboard_app/src/hub/netrunners/netrunners.dart';
import 'package:fixer_dashboard_app/src/hub/overseer/overseer.dart';
import 'package:fixer_dashboard_app/src/hub/skills/skills_models.dart';
import 'package:fixer_dashboard_app/src/hub/skills/skills_repository.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_models.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:visibility_detector/visibility_detector.dart';

import 'workroom_test_fakes.dart';

class FakeBacklogRepository implements BacklogRepository {
  @override
  Future<ProjectBacklogSnapshot> loadProjectBacklog(int projectId) async {
    return const ProjectBacklogSnapshot(
      project: BacklogProjectRecord(
        id: 1,
        name: 'Fixer MCP',
        cwd: '/tmp/self_orchestration',
      ),
      items: [
        BacklogItemRecord(
          id: 1,
          projectId: 1,
          title: 'Integrate the hub',
          description: 'Wire every reviewed slice.',
          status: 'open',
          priority: 'high',
          createdAt: '2026-07-23T01:00:00Z',
          updatedAt: '2026-07-23T01:05:00Z',
        ),
      ],
      documents: [],
    );
  }
}

class FakeNetrunnerExplorerRepository implements NetrunnerExplorerRepository {
  @override
  Future<NetrunnerExplorerSnapshot> loadProjectNetrunners(
    int projectId,
  ) async => _netrunnerExplorer;
}

class FakeFixerChatService implements FixerChatService {
  @override
  Future<List<FixerThreadRecord>> loadFixerThreads(int projectId) async =>
      const [
        FixerThreadRecord(
          externalId: 'fixer-droid',
          headline: 'Droid Fixer thread',
          status: 'history',
          backend: 'droid',
          model: 'kimi-k2.7-code',
          reasoning: 'high',
          cwd: '/tmp/self_orchestration',
          lastActivityAt: '2026-07-23T01:00:00Z',
          transcriptAvailable: true,
        ),
      ];

  @override
  Future<void> createFixerChat(
    int projectId,
    FixerChatLaunchRequest request,
  ) async {}
}

class FakeNetrunnerThreadRepository implements NetrunnerThreadRepository {
  @override
  Future<NetrunnerThreadSnapshot> loadThread(int sessionId) async =>
      _netrunnerThread;

  @override
  Future<NetrunnerContinuationResult> sendFollowUp(
    int sessionId,
    String message,
  ) async => const NetrunnerContinuationResult(
    status: 'started',
    sessionId: 102,
    backend: 'codex',
    externalSessionId: 'netrunner-codex',
    processId: 4242,
    message: 'Follow-up started.',
  );
}

class FakeSkillsRepository implements SkillsRepository {
  static const summary = ManagedSkillSummary(
    name: 'init-fixer',
    description: 'Initialize a project Fixer.',
    locations: [
      SkillLocation(
        rootId: 'agents',
        rootLabel: 'Agents',
        relativePath: '.agents/skills/init-fixer/SKILL.md',
      ),
    ],
    relatedSkills: ['run-netrunner-wave'],
  );

  @override
  Future<ProjectSkillsCatalog> loadSkills(int projectId) async =>
      const ProjectSkillsCatalog(projectName: 'Fixer MCP', skills: [summary]);

  @override
  Future<ManagedSkillDetail> loadSkill(
    int projectId,
    String rootId,
    String name,
  ) async => const ManagedSkillDetail(
    summary: summary,
    rootId: 'agents',
    relativePath: '.agents/skills/init-fixer/SKILL.md',
    content: '# Init Fixer\n\nInitialize the Fixer role.',
  );

  @override
  Future<ManagedSkillDetail> updateSkill(
    int projectId,
    String name, {
    required String rootId,
    required String content,
  }) => loadSkill(projectId, rootId, name);
}

class FakeOverseerRepository implements OverseerManagerRepository {
  @override
  Future<List<OverseerThreadRecord>> loadThreads() async => [
    OverseerThreadRecord(
      backend: 'claude',
      model: 'sonnet',
      reasoning: 'medium',
      spawnCwd: '/tmp/self_orchestration',
      origin: 'claude_session_log',
      startedAt: DateTime.parse('2026-07-23T00:00:00Z'),
      lastActivityAt: DateTime.parse('2026-07-23T01:00:00Z'),
      externalSessionId: 'overseer-claude',
      preview: 'Architecture review',
    ),
  ];

  @override
  Future<OverseerLaunchPlanRecord> createOverseer(
    OverseerLaunchRequest request,
  ) async => _overseerPlan;

  @override
  Future<OverseerLaunchPlanRecord> resumeOverseer(
    OverseerThreadRecord thread,
  ) async => _overseerPlan;
}

class FakeMissionControlRepository implements MissionControlRepository {
  @override
  Future<MissionControlWavesSnapshot> loadWaves(int projectId) async =>
      MissionControlWavesSnapshot.fromJson(_missionControlPayload);

  @override
  Future<void> runWaveAction(int projectId, int waveId, String action) async {}
}

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
  Future<ThreadSendResult> sendThreadMessageWithConfig(
    String threadId,
    String prompt, {
    required String model,
    required String reasoning,
  }) => sendThreadMessage(threadId, prompt);

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
      activeWaveCount: 1,
      lastActivityAt: '2026-07-23T01:00:00Z',
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
    activeWaveCount: 1,
    totalWaveCount: 1,
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
  documentsTree: const ProjectDocumentsSnapshot(
    projectName: 'Fixer MCP',
    totalDocs: 1,
    roots: [
      HubDocument(
        id: 11,
        title: 'Codex Hub Desktop Migration Brief',
        docType: 'architecture',
        level: 0,
        slug: 'hub-migration',
        path: 'fixer-mcp/hub-migration',
        status: 'current',
        content: '# Hub migration\n\nProvider-neutral composition.',
      ),
    ],
  ),
  waveGroups: [_activeWave],
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

const _activeWave = NetrunnerWaveGroupRecord(
  waveId: 145,
  waveIdentity: 'wave-145',
  status: 'running',
  createdAt: '2026-07-23T00:30:00Z',
  updatedAt: '2026-07-23T01:00:00Z',
  launchedAt: '2026-07-23T00:35:00Z',
  completedAt: '',
  workerCount: 1,
  reviewerCount: 0,
  manualCount: 0,
  sessions: [_explorerSession],
);

const _explorerSession = NetrunnerExplorerRecord(
  id: 102,
  localId: 3,
  projectId: 1,
  waveId: 145,
  role: 'netrunner',
  kind: 'worker',
  headline: 'Flutter App Shell for the Fixer MCP GUI.',
  taskPreview: 'Bridge-backed operator shell',
  status: 'in_progress',
  membershipStatus: 'running',
  backend: 'codex',
  model: 'gpt-5.4',
  reasoning: 'medium',
  writeScope: ['fixer_mcp/dashboard_app'],
  createdAt: '2026-07-23T00:30:00Z',
  updatedAt: '2026-07-23T01:00:00Z',
  launchedAt: '2026-07-23T00:35:00Z',
  completedAt: '',
);

const _netrunnerExplorer = NetrunnerExplorerSnapshot(
  waveGroups: [_activeWave],
  ungroupedSessions: [],
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

const _netrunnerThread = NetrunnerThreadSnapshot(
  sessionId: 102,
  localId: 3,
  projectId: 1,
  status: 'in_progress',
  backend: 'codex',
  model: 'gpt-5.4',
  reasoning: 'medium',
  externalSessionId: 'netrunner-codex',
  launchState: 'launched',
  transcriptAvailability: 'available',
  transcriptPath: '/tmp/netrunner.jsonl',
  messages: [
    NetrunnerThreadMessageRecord(
      id: 'message-1',
      role: 'assistant',
      text: 'Netrunner thread is connected.',
      createdAt: '2026-07-23T01:00:00Z',
      source: 'codex_jsonl',
    ),
  ],
  continuation: NetrunnerContinuationCapabilityRecord(
    supported: true,
    mode: 'resume',
    reason: '',
  ),
);

const _overseerPlan = OverseerLaunchPlanRecord(
  mode: 'create',
  cwd: '/tmp/self_orchestration',
  backend: 'codex',
  model: 'gpt-5.6-luna',
  reasoning: 'high',
  command: ['codex'],
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

final _missionControlPayload = <String, dynamic>{
  'project_id': 1,
  'generated_at': '2026-07-29T12:00:00Z',
  'freshness': {
    'state': 'fresh',
    'stale': false,
    'source_updated_at': '2026-07-29T11:59:30Z',
    'age_seconds': 30,
    'stale_after_seconds': 300,
  },
  'waves': [
    {
      'wave_id': 145,
      'phase': 'implementation',
      'legacy_status': 'in_progress',
      'operator_state': 'workers_running',
      'label': 'Implementation workers running',
      'next_action': 'wait',
      'gate_state': 'implementation',
      'control_state': 'active',
      'failure_policy_state': 'passed',
      'updated_at': '2026-07-29T11:59:30Z',
      'worker_counts': {'total': 1, 'active': 1},
      'review': {'state': 'not_started'},
      'acceptance': {'state': 'not_started'},
      'repair': {'state': 'none', 'attempt_count': 0},
      'workers': [
        {
          'worker_id': 613,
          'session_id': 517,
          'local_session_id': 3,
          'status': 'running',
          'backend': 'codex',
          'model': 'gpt-5.6-sol',
          'reasoning': 'high',
        },
      ],
      'action_capabilities': <String, dynamic>{},
    },
  ],
};

Widget _testDashboard({FakeProjectWorkroomRepository? workroomRepository}) =>
    FixerDashboardApp(
      repository: FakeDashboardRepository(),
      backlogRepository: FakeBacklogRepository(),
      netrunnerExplorerRepository: FakeNetrunnerExplorerRepository(),
      fixerChatService: FakeFixerChatService(),
      netrunnerThreadRepository: FakeNetrunnerThreadRepository(),
      skillsRepository: FakeSkillsRepository(),
      overseerRepository: FakeOverseerRepository(),
      missionControlRepository: FakeMissionControlRepository(),
      workroomRepository: workroomRepository ?? FakeProjectWorkroomRepository(),
    );

void main() {
  setUp(() {
    TestWidgetsFlutterBinding.ensureInitialized();
    VisibilityDetectorController.instance.updateInterval = Duration.zero;
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  testWidgets('wires provider-neutral home, Overseers, and Skills Manager', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(_testDashboard());
    await tester.pumpAndSettle();

    expect(find.text('Fixer Studio'), findsOneWidget);
    expect(find.text('Projects'), findsOneWidget);
    expect(find.text('Overseers'), findsOneWidget);
    expect(find.text('Architecture review'), findsOneWidget);
    expect(find.textContaining('active wave'), findsOneWidget);

    await tester.tap(find.byTooltip('Skills Manager'));
    await tester.pumpAndSettle();
    expect(find.text('Skills Manager'), findsOneWidget);
    expect(find.text('init-fixer'), findsWidgets);
  });

  testWidgets('project workroom exposes exactly two tabs and typed surfaces', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(_testDashboard());
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fixer MCP'));
    await tester.pumpAndSettle();

    expect(find.byType(Tab), findsNWidgets(2));
    expect(find.widgetWithText(Tab, 'Fixer'), findsOneWidget);
    expect(find.widgetWithText(Tab, 'Hands'), findsOneWidget);
    for (final oldLabel in const [
      'Overview',
      'Mission Control',
      'Backlog',
      'Docs',
      'Netrunners',
      'Fixer Chat',
      'Client Orders Sandbox',
    ]) {
      expect(find.widgetWithText(Tab, oldLabel), findsNothing);
    }
    expect(
      find.descendant(
        of: find.byType(AppBar),
        matching: find.text('/tmp/self_orchestration'),
      ),
      findsOneWidget,
    );
    expect(find.text('Project state is ready.'), findsWidgets);

    Future<void> openSurface(String key, String expected) async {
      await tester.tap(find.byKey(const ValueKey('genui-surface-catalog')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(ValueKey('surface-request-$key')));
      await tester.pumpAndSettle();
      expect(find.text(expected), findsOneWidget);
    }

    await openSurface('wave.list.v1', 'Wave #145 · gpt-5.6-sol');
    await openSurface('backlog.list.v1', 'Integrate the hub');
    await openSurface('docs.tree.v1', 'Codex Hub Desktop Migration Brief');
    await openSurface(
      'execution.list.v1',
      'Flutter App Shell for the Fixer MCP GUI.',
    );
    await openSurface('skills.catalog.v1', 'init-fixer');
  });

  testWidgets('Fixer legal-research request opens the typed project surface', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final workroomRepository = FakeProjectWorkroomRepository();
    await tester.pumpWidget(
      _testDashboard(workroomRepository: workroomRepository),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fixer MCP'));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const ValueKey('fixer-message-composer')),
      'Покажи ресерч по юрке',
    );
    await tester.tap(find.byKey(const ValueKey('send-fixer-message')));
    await tester.pumpAndSettle();

    expect(find.text('Покажи ресерч по юрке'), findsOneWidget);
    expect(find.text('Юридический ресерч'), findsWidgets);
    expect(find.text('research/legal/legal_memo.md'), findsOneWidget);
    expect(find.textContaining('Канонический ресерч проекта'), findsOneWidget);
  });

  testWidgets('Hands remains one permanent channel with governed mailbox', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final workroomRepository = FakeProjectWorkroomRepository();
    await tester.pumpWidget(
      _testDashboard(workroomRepository: workroomRepository),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fixer MCP'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Hands'));
    await tester.pumpAndSettle();

    expect(find.text('Руки'), findsOneWidget);
    expect(find.textContaining('hands-project-1'), findsOneWidget);
    await tester.tap(find.text('Mailbox & history'));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('hands-mailbox-list')), findsOneWidget);
    expect(find.text('All focused checks passed.'), findsOneWidget);
    expect(find.text('3 files changed'), findsOneWidget);
    expect(find.byKey(const ValueKey('accept-hands-result')), findsOneWidget);

    await tester.enterText(
      find.byKey(const ValueKey('hands-instruction-composer')),
      'Run a deterministic accessibility audit.',
    );
    await tester.tap(find.byKey(const ValueKey('submit-hands-instruction')));
    await tester.pumpAndSettle();
    expect(
      workroomRepository.submittedInstructions,
      contains('Run a deterministic accessibility audit.'),
    );
  });

  testWidgets('narrow workroom keeps two tabs and opens GenUI as a sheet', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final semantics = tester.ensureSemantics();

    await tester.pumpWidget(_testDashboard());
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fixer MCP'));
    await tester.pumpAndSettle();

    expect(find.byType(Tab), findsNWidgets(2));
    expect(
      find.byKey(const ValueKey('open-genui-surface-sheet')),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('genui-surface-surface-overview')),
      findsNothing,
    );

    await tester.tap(find.byKey(const ValueKey('open-genui-surface-sheet')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('genui-surface-surface-overview')),
      findsOneWidget,
    );
    expect(find.bySemanticsLabel('Helpful'), findsOneWidget);
    expect(find.bySemanticsLabel('Not helpful'), findsOneWidget);
    semantics.dispose();
  });
}
