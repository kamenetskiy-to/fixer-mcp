import 'dart:async';

import 'package:flutter/material.dart';

import '../app_localizations.dart';
import '../dashboard_models.dart';
import '../hub/fixer_chat/fixer_chat_models.dart';
import '../hub/fixer_chat/fixer_chat_panel.dart';
import '../hub/fixer_chat/fixer_chat_service.dart';
import 'fixer_genui_tab.dart';
import 'hands_tab.dart';
import 'workroom_models.dart';
import 'workroom_repository.dart';
import 'workroom_store.dart';

class ProjectWorkroom extends StatefulWidget {
  const ProjectWorkroom({
    super.key,
    required this.legacySnapshot,
    required this.repository,
    this.cursorStore,
    this.initialTabId = 'fixer_genui',
    this.initialSurfaceId = '',
    this.initialInstructionId = '',
    this.loadHistoricalTurns,
    this.fixerChatService,
    this.sendThreadMessage,
    this.sendThreadMessageWithConfig,
    this.loadThreadTurnStatus,
  });

  final ProjectWorkspaceSnapshot legacySnapshot;
  final ProjectWorkroomRepository repository;
  final ProjectUiCursorStore? cursorStore;
  final String initialTabId;
  final String initialSurfaceId;
  final String initialInstructionId;
  final Future<List<WorkroomFixerTurn>> Function(String threadId)?
  loadHistoricalTurns;
  final FixerChatService? fixerChatService;
  final Future<ThreadSendResult> Function(String threadId, String prompt)?
  sendThreadMessage;
  final Future<ThreadSendResult> Function(
    String threadId,
    String prompt, {
    required String model,
    required String reasoning,
  })?
  sendThreadMessageWithConfig;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)?
  loadThreadTurnStatus;

  @override
  State<ProjectWorkroom> createState() => _ProjectWorkroomState();
}

class _ProjectWorkroomState extends State<ProjectWorkroom>
    with SingleTickerProviderStateMixin {
  late final TabController _tabController;
  late final ProjectWorkroomStore _store;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(
      length: 2,
      vsync: this,
      initialIndex: widget.initialTabId == 'hands' ? 1 : 0,
    );
    _store = ProjectWorkroomStore(
      projectId: widget.legacySnapshot.project.id,
      repository: widget.repository,
      cursorStore:
          widget.cursorStore ?? SharedPreferencesProjectUiCursorStore(),
      seedSnapshot: seedWorkroomSnapshot(widget.legacySnapshot),
    );
    _store.addListener(_applyInitialDeepLinks);
    unawaited(_store.start());
  }

  bool _initialLinksApplied = false;

  void _applyInitialDeepLinks() {
    if (_initialLinksApplied) return;
    final state = _store.state;
    if (state.connectionStatus == WorkroomConnectionStatus.cold ||
        state.connectionStatus == WorkroomConnectionStatus.loadingSnapshot) {
      return;
    }
    _initialLinksApplied = true;
    if (widget.initialSurfaceId.isNotEmpty) {
      _store.activateSurface(widget.initialSurfaceId);
    }
    if (widget.initialInstructionId.isNotEmpty) {
      _store.selectHandsInstruction(widget.initialInstructionId);
    }
  }

  @override
  void dispose() {
    _store.removeListener(_applyInitialDeepLinks);
    _store.dispose();
    _tabController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Material(
          color: Theme.of(context).colorScheme.surface,
          child: TabBar(
            controller: _tabController,
            tabs: [
              Tab(
                key: const ValueKey('workroom-tab-fixer_genui'),
                icon: const Icon(Icons.auto_awesome_outlined, size: 19),
                text: l10n.workroomFixerTab,
              ),
              Tab(
                key: const ValueKey('workroom-tab-hands'),
                icon: const Icon(Icons.back_hand_outlined, size: 19),
                text: l10n.workroomHandsTab,
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        Expanded(
          child: TabBarView(
            controller: _tabController,
            children: [
              FixerGenUiTab(
                store: _store,
                loadHistoricalTurns: widget.loadHistoricalTurns,
                onNewThread: _createFixerThread,
              ),
              HandsTab(
                store: _store,
                loadThreads: _loadHandsThreads,
                loadHistoricalTurns: widget.loadHistoricalTurns,
                sendThreadMessage: widget.sendThreadMessage,
                sendThreadMessageWithConfig: widget.sendThreadMessageWithConfig,
                loadThreadTurnStatus: widget.loadThreadTurnStatus,
              ),
            ],
          ),
        ),
      ],
    );
  }

  Future<void> _createFixerThread() async {
    final service = widget.fixerChatService;
    if (service == null) {
      _store.startNewFixerThread();
      return;
    }
    final request = await showDialog<FixerChatLaunchRequest>(
      context: context,
      builder: (context) => CreateFixerChatDialog(
        cwd: widget.legacySnapshot.project.cwd,
        providers: supportedFixerProviders,
      ),
    );
    if (request == null || !mounted) return;
    await service.createFixerChat(widget.legacySnapshot.project.id, request);
    await _store.reloadSnapshot();
  }

  Future<List<WorkroomFixerThread>> _loadHandsThreads() async {
    final service = widget.fixerChatService;
    if (service is! HandsChatService) return const [];
    final threads = await (service as HandsChatService).loadHandsThreads(
      widget.legacySnapshot.project.id,
    );
    return threads
        .map(
          (thread) => WorkroomFixerThread(
            id: thread.externalId,
            headline: thread.headline,
            provider: thread.backend,
            state: thread.status,
            createdAt: thread.startedAt,
            updatedAt: thread.lastActivityAt,
            model: thread.model,
            reasoning: thread.reasoning,
          ),
        )
        .toList(growable: false);
  }
}

ProjectWorkroomSnapshot seedWorkroomSnapshot(ProjectWorkspaceSnapshot legacy) {
  final sessions = <FixerChatSessionSummary>[
    ...legacy.fixerChat.sessions,
    if (legacy.fixerChat.defaultSession != null &&
        !legacy.fixerChat.sessions.any(
          (session) =>
              session.externalId == legacy.fixerChat.defaultSession!.externalId,
        ))
      legacy.fixerChat.defaultSession!,
  ];
  final threads = sessions
      .map(
        (session) => WorkroomFixerThread(
          id: session.externalId.isNotEmpty
              ? session.externalId
              : 'legacy-fixer-${session.id}',
          headline: session.headline.isNotEmpty ? session.headline : 'Fixer',
          provider: session.backend.isNotEmpty ? session.backend : 'unknown',
          state: session.status.isNotEmpty ? session.status : 'unavailable',
          createdAt: session.lastActivityAt,
          updatedAt: session.lastActivityAt,
          model: session.model,
          reasoning: session.reasoning,
        ),
      )
      .toList(growable: true);
  if (threads.isEmpty) {
    threads.add(
      const WorkroomFixerThread(
        id: 'fixer-unavailable',
        headline: 'Fixer',
        provider: 'unknown',
        state: 'unavailable',
        createdAt: '',
        updatedAt: '',
      ),
    );
  }
  final overview = WorkroomSurfaceState(
    document: GenUiSurfaceDocument.fromJson({
      'protocol': 'fixer.genui',
      'protocol_version': 1,
      'instance_id': 'local-project-overview-${legacy.project.id}',
      'surface_type': 'project.overview',
      'surface_version': 1,
      'project_id': legacy.project.id,
      'revision': 1,
      'source_seq': 0,
      'title': legacy.project.name,
      'generated_at': '',
      'components': [
        {
          'kind': 'text',
          'id': 'overview-heading',
          'variant': 'heading',
          'text': legacy.project.name,
        },
        {
          'kind': 'key_value',
          'id': 'overview-identity',
          'rows': [
            {'label': 'CWD', 'value': legacy.project.cwd},
            {
              'label': 'Active waves',
              'value': '${legacy.metrics.activeWaveCount}',
            },
            {
              'label': 'Total waves',
              'value': '${legacy.metrics.totalWaveCount}',
            },
            {
              'label': 'Running workers',
              'value': '${legacy.metrics.workerState.runningCount}',
            },
            {
              'label': 'Documents',
              'value': '${legacy.metrics.attachedDocCount}',
            },
            {
              'label': 'Pending proposals',
              'value': '${legacy.metrics.pendingProposalCount}',
            },
          ],
        },
      ],
      'actions': <Map<String, dynamic>>[],
    }),
    feedbackVote: 0,
  );
  return ProjectWorkroomSnapshot(
    project: WorkroomProject(
      id: legacy.project.id,
      name: legacy.project.name,
      cwd: legacy.project.cwd,
    ),
    protocolVersion: projectWorkroomProtocolVersion,
    watermarkSeq: 0,
    threads: List.unmodifiable(threads),
    selectedThreadId: threads.first.id,
    turns: const <WorkroomFixerTurn>[],
    activeSurface: overview,
    surfaceHistory: [overview],
    hands: WorkroomHandsState(
      actorId: '',
      displayName: 'Руки',
      authorityState: 'loading',
      operationalState: 'idle',
      selectedLane: 'codex',
      queueDepth: 0,
      activeLeaseSummary: '',
      lanes: [
        for (final backend in const [
          'codex',
          'claude',
          'kimi-code',
          'antigravity',
        ])
          _defaultHandsLane(backend),
      ],
      instructions: const <HandsInstruction>[],
      selectedInstructionId: '',
    ),
    capabilities: WorkroomCapabilities.none,
  );
}

HandsProviderLane _defaultHandsLane(String backend) {
  final provider = supportedFixerProvidersByBackend[backend];
  return HandsProviderLane(
    provider: backend,
    model: provider?.defaultModel ?? '',
    reasoning: provider?.defaultReasoning ?? '',
  );
}
