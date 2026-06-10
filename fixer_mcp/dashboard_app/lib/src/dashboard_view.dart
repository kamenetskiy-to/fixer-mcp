import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:markdown_widget/markdown_widget.dart';

import 'dashboard_models.dart';
import 'dashboard_repository.dart';

const _chromeBorder = Color(0xFFD9E0EC);
const _sidebarFill = Color(0xFFF1F4F9);

class DashboardShell extends StatefulWidget {
  const DashboardShell({super.key, required this.repository});

  final DashboardRepository repository;

  @override
  State<DashboardShell> createState() => _DashboardShellState();
}

class _DashboardShellState extends State<DashboardShell> {
  late Future<HomeSnapshot> _homeFuture;

  @override
  void initState() {
    super.initState();
    _homeFuture = widget.repository.loadHomeSnapshot();
  }

  void _reload() {
    setState(() {
      _homeFuture = widget.repository.loadHomeSnapshot();
    });
  }

  Future<void> _openProject(int projectId) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        settings: RouteSettings(name: '/project/$projectId'),
        builder: (_) => _ProjectRouteScreen(
          repository: widget.repository,
          projectId: projectId,
        ),
      ),
    );
    if (mounted) {
      _reload();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.hub_outlined, size: 20),
            SizedBox(width: 10),
            Text('Codex Hub'),
          ],
        ),
        actions: [
          IconButton(
            onPressed: _reload,
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: FutureBuilder<HomeSnapshot>(
        future: _homeFuture,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting &&
              !snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _ErrorState(
              message: snapshot.error.toString(),
              onRetry: _reload,
            );
          }
          final home = snapshot.data!;
          if (home.projects.isEmpty) {
            return _ErrorState(
              message: 'No Fixer MCP projects were returned by the bridge.',
              onRetry: _reload,
            );
          }

          final selectedProjectId =
              home.currentProject?.id ?? home.projects.first.project.id;
          return LayoutBuilder(
            builder: (context, constraints) {
              final wide = constraints.maxWidth >= 1040;
              final rail = _HomeProjectRail(
                home: home,
                selectedProjectId: selectedProjectId,
                onOpenProject: _openProject,
              );
              final chat = _HomeChatWorkspace(
                home: home,
                loadOverseerChatBinding:
                    widget.repository.loadOverseerChatBinding,
                loadThreadMessages: widget.repository.loadThreadMessages,
                sendThreadMessage: widget.repository.sendThreadMessage,
                loadThreadTurnStatus: widget.repository.loadThreadTurnStatus,
              );

              if (wide) {
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SizedBox(width: 360, child: rail),
                    const VerticalDivider(width: 1),
                    Expanded(child: chat),
                  ],
                );
              }

              return ListView(
                padding: const EdgeInsets.all(12),
                children: [
                  SizedBox(height: 560, child: rail),
                  const SizedBox(height: 12),
                  SizedBox(height: 760, child: chat),
                ],
              );
            },
          );
        },
      ),
    );
  }
}

class _ProjectRouteScreen extends StatefulWidget {
  const _ProjectRouteScreen({
    required this.repository,
    required this.projectId,
  });

  final DashboardRepository repository;
  final int projectId;

  @override
  State<_ProjectRouteScreen> createState() => _ProjectRouteScreenState();
}

class _ProjectRouteScreenState extends State<_ProjectRouteScreen> {
  late Future<ProjectWorkspaceSnapshot> _projectFuture;

  @override
  void initState() {
    super.initState();
    _projectFuture = widget.repository.loadProjectWorkspace(widget.projectId);
  }

  void _reload() {
    setState(() {
      _projectFuture = widget.repository.loadProjectWorkspace(widget.projectId);
    });
  }

  Future<void> _openSession(int sessionId) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        settings: RouteSettings(name: '/netrunner/$sessionId'),
        builder: (_) => _NetrunnerRouteScreen(
          repository: widget.repository,
          sessionId: sessionId,
        ),
      ),
    );
    if (mounted) {
      _reload();
    }
  }

  Future<void> _createTask(ProjectWorkspaceSnapshot project) async {
    final input = await showDialog<_TaskDraft>(
      context: context,
      builder: (context) => const _CreateTaskDialog(),
    );
    if (input == null || !mounted) {
      return;
    }
    try {
      final snapshot = await widget.repository.createTask(
        project.project.id,
        taskDescription: input.taskDescription,
        declaredWriteScope: input.writeScope,
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _projectFuture = Future.value(snapshot);
      });
      _showNotice('Created a new pending Netrunner task.');
    } catch (error) {
      _showNotice(error.toString());
    }
  }

  void _showNotice(String message) {
    if (!mounted) {
      return;
    }
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Project'),
        actions: [
          IconButton(
            onPressed: _reload,
            tooltip: 'Refresh project',
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: FutureBuilder<ProjectWorkspaceSnapshot>(
        future: _projectFuture,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting &&
              !snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _ErrorState(
              message: snapshot.error.toString(),
              onRetry: _reload,
            );
          }
          final project = snapshot.data!;
          return _ProjectWorkspace(
            project: project,
            onOpenSession: _openSession,
            onCreateTask: () => _createTask(project),
            loadFixerChatBinding: widget.repository.loadFixerChatBinding,
            loadThreadMessages: widget.repository.loadThreadMessages,
            sendThreadMessage: widget.repository.sendThreadMessage,
            loadThreadTurnStatus: widget.repository.loadThreadTurnStatus,
          );
        },
      ),
    );
  }
}

class _NetrunnerRouteScreen extends StatefulWidget {
  const _NetrunnerRouteScreen({
    required this.repository,
    required this.sessionId,
  });

  final DashboardRepository repository;
  final int sessionId;

  @override
  State<_NetrunnerRouteScreen> createState() => _NetrunnerRouteScreenState();
}

class _NetrunnerRouteScreenState extends State<_NetrunnerRouteScreen> {
  late Future<NetrunnerDetailSnapshot> _detailFuture;

  @override
  void initState() {
    super.initState();
    _detailFuture = widget.repository.loadNetrunnerDetail(widget.sessionId);
  }

  void _reload() {
    setState(() {
      _detailFuture = widget.repository.loadNetrunnerDetail(widget.sessionId);
    });
  }

  Future<void> _updateAttachedDocs(SessionDetailRecord session) async {
    final selectedDocIds = await showDialog<List<int>>(
      context: context,
      builder: (context) => _MultiSelectDialog<int>(
        title: 'Attach internal docs',
        items: session.availableDocs.map((doc) => doc.id).toList(),
        initiallySelected: session.attachedDocs.map((doc) => doc.id).toSet(),
        labelBuilder: (id) {
          final doc = session.availableDocs.firstWhere((item) => item.id == id);
          return '#${doc.id} ${doc.title}';
        },
        detailBuilder: (id) {
          final doc = session.availableDocs.firstWhere((item) => item.id == id);
          return '${doc.docType} - ${doc.summary}';
        },
      ),
    );
    if (selectedDocIds == null) {
      return;
    }
    await _runMutation(
      () =>
          widget.repository.setSessionAttachedDocs(session.id, selectedDocIds),
      successMessage: 'Updated attached docs.',
    );
  }

  Future<void> _updateMcpServers(SessionDetailRecord session) async {
    final selectedNames = await showDialog<List<String>>(
      context: context,
      builder: (context) => _MultiSelectDialog<String>(
        title: 'Assign MCP servers',
        items: session.availableMcpServers
            .map((server) => server.name)
            .toList(),
        initiallySelected: session.mcpServers
            .map((server) => server.name)
            .toSet(),
        labelBuilder: (name) => name,
        detailBuilder: (name) {
          final server = session.availableMcpServers.firstWhere(
            (item) => item.name == name,
          );
          return server.howTo;
        },
      ),
    );
    if (selectedNames == null) {
      return;
    }
    await _runMutation(
      () => widget.repository.setSessionMcpServers(session.id, selectedNames),
      successMessage: 'Updated MCP assignments.',
    );
  }

  Future<void> _updateSessionStatus(SessionDetailRecord session) async {
    final selectedStatus = await showDialog<String>(
      context: context,
      builder: (context) => _ChoiceDialog<String>(
        title: 'Move session status',
        items: session.allowedStatusTargets,
        labelBuilder: (status) => status,
      ),
    );
    if (selectedStatus == null) {
      return;
    }
    await _runMutation(
      () => widget.repository.setSessionStatus(session.id, selectedStatus),
      successMessage: 'Updated session status.',
    );
  }

  Future<void> _updateProposalStatus(
    SessionDetailRecord session,
    DocProposalSummaryRecord proposal,
    String status,
  ) async {
    await _runMutation(
      () => widget.repository.setProposalStatus(proposal.id, status),
      successMessage: 'Updated proposal #${proposal.localId}.',
    );
  }

  Future<void> _runMutation(
    Future<NetrunnerDetailSnapshot> Function() action, {
    required String successMessage,
  }) async {
    try {
      final snapshot = await action();
      if (!mounted) {
        return;
      }
      setState(() {
        _detailFuture = Future.value(snapshot);
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(successMessage)));
    } catch (error) {
      if (!mounted) {
        return;
      }
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(error.toString())));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: FutureBuilder<NetrunnerDetailSnapshot>(
        future: _detailFuture,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting &&
              !snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _ErrorState(
              message: snapshot.error.toString(),
              onRetry: _reload,
            );
          }
          return _SessionWorkspace(
            detail: snapshot.data!,
            onBack: () => Navigator.of(context).maybePop(),
            onAttachDocs: _updateAttachedDocs,
            onAssignMcpServers: _updateMcpServers,
            onChangeStatus: _updateSessionStatus,
            onSetProposalStatus: _updateProposalStatus,
          );
        },
      ),
    );
  }
}

class _HomeProjectRail extends StatelessWidget {
  const _HomeProjectRail({
    required this.home,
    required this.selectedProjectId,
    required this.onOpenProject,
  });

  final HomeSnapshot home;
  final int selectedProjectId;
  final ValueChanged<int> onOpenProject;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: _sidebarFill,
        border: const Border(right: BorderSide(color: _chromeBorder)),
      ),
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _SectionTitle(
            title: 'Projects',
            subtitle:
                home.currentProject?.cwd ??
                'Bridge-backed Fixer MCP project state.',
          ),
          const SizedBox(height: 16),
          for (final project in home.projects) ...[
            _ProjectCard(
              project: project,
              selected: project.project.id == selectedProjectId,
              onTap: () => onOpenProject(project.project.id),
            ),
            const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class _HomeChatWorkspace extends StatelessWidget {
  const _HomeChatWorkspace({
    required this.home,
    required this.loadOverseerChatBinding,
    required this.loadThreadMessages,
    required this.sendThreadMessage,
    required this.loadThreadTurnStatus,
  });

  final HomeSnapshot home;
  final Future<FixerChatBindingRecord> Function(int projectId)
  loadOverseerChatBinding;
  final Future<ThreadMessagesSnapshot> Function(String threadId)
  loadThreadMessages;
  final Future<ThreadSendResult> Function(String threadId, String prompt)
  sendThreadMessage;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)
  loadThreadTurnStatus;

  @override
  Widget build(BuildContext context) {
    final projectId = home.currentProject?.id;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 10),
          child: Row(
            children: [
              Expanded(
                child: _SectionTitle(
                  title: 'Overseer',
                  subtitle: projectId == null
                      ? 'No current project binding'
                      : 'Global chat binding',
                  compact: true,
                ),
              ),
              _StatusPill(
                label: home.defaultChatBinding.transcriptAvailability.isEmpty
                    ? 'deferred'
                    : home.defaultChatBinding.transcriptAvailability,
              ),
            ],
          ),
        ),
        Expanded(
          child: projectId == null
              ? _FixerChatPanel(
                  binding: home.defaultChatBinding,
                  loadThreadMessages: loadThreadMessages,
                  sendThreadMessage: sendThreadMessage,
                  loadThreadTurnStatus: loadThreadTurnStatus,
                )
              : _AsyncChatBindingPanel(
                  projectId: projectId,
                  loadBinding: loadOverseerChatBinding,
                  loadThreadMessages: loadThreadMessages,
                  sendThreadMessage: sendThreadMessage,
                  loadThreadTurnStatus: loadThreadTurnStatus,
                ),
        ),
      ],
    );
  }
}

class _ProjectWorkspace extends StatelessWidget {
  const _ProjectWorkspace({
    required this.project,
    required this.onOpenSession,
    required this.onCreateTask,
    required this.loadFixerChatBinding,
    required this.loadThreadMessages,
    required this.sendThreadMessage,
    required this.loadThreadTurnStatus,
  });

  final ProjectWorkspaceSnapshot project;
  final ValueChanged<int> onOpenSession;
  final VoidCallback onCreateTask;
  final Future<FixerChatBindingRecord> Function(int projectId)
  loadFixerChatBinding;
  final Future<ThreadMessagesSnapshot> Function(String threadId)
  loadThreadMessages;
  final Future<ThreadSendResult> Function(String threadId, String prompt)
  sendThreadMessage;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)
  loadThreadTurnStatus;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return DefaultTabController(
      length: 4,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 0),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        project.project.name,
                        style: theme.textTheme.headlineSmall?.copyWith(
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                      Text(
                        project.project.cwd,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
                FilledButton.icon(
                  onPressed: onCreateTask,
                  icon: const Icon(Icons.add_task),
                  label: const Text('Create task'),
                ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          const TabBar(
            isScrollable: true,
            tabs: [
              Tab(text: 'Overview'),
              Tab(text: 'Docs'),
              Tab(text: 'Netrunners'),
              Tab(text: 'Fixer Chat'),
            ],
          ),
          Expanded(
            child: TabBarView(
              children: [
                _ProjectOverviewTab(
                  project: project,
                  onOpenSession: onOpenSession,
                ),
                _ProjectDocsTab(project: project),
                _ProjectNetrunnersTab(
                  project: project,
                  onOpenSession: onOpenSession,
                ),
                _AsyncChatBindingPanel(
                  projectId: project.project.id,
                  loadBinding: loadFixerChatBinding,
                  loadThreadMessages: loadThreadMessages,
                  sendThreadMessage: sendThreadMessage,
                  loadThreadTurnStatus: loadThreadTurnStatus,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ProjectOverviewTab extends StatelessWidget {
  const _ProjectOverviewTab({
    required this.project,
    required this.onOpenSession,
  });

  final ProjectWorkspaceSnapshot project;
  final ValueChanged<int> onOpenSession;

  @override
  Widget build(BuildContext context) {
    final hotSessions = project.netrunners
        .where(
          (session) =>
              session.status == 'in_progress' || session.status == 'review',
        )
        .toList();
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        _MetricStrip(
          entries: [
            ('Pending', project.metrics.counts.pending.toString()),
            ('Running', project.metrics.counts.inProgress.toString()),
            ('Review', project.metrics.counts.review.toString()),
            ('Docs', project.metrics.attachedDocCount.toString()),
            ('Proposals', project.metrics.pendingProposalCount.toString()),
          ],
        ),
        const SizedBox(height: 16),
        if (project.autonomous != null)
          _Panel(
            title: 'Autonomous status',
            trailing: _StatusPill(label: project.autonomous!.state),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(project.autonomous!.summary),
                if (project.autonomous!.focus.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text('Focus: ${project.autonomous!.focus}'),
                ],
                if (project.autonomous!.blocker.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text('Blocker: ${project.autonomous!.blocker}'),
                ],
                if (project.autonomous!.evidence.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(project.autonomous!.evidence),
                ],
              ],
            ),
          ),
        const SizedBox(height: 16),
        _Panel(
          title: 'Worker activity',
          child: Text(
            project.metrics.workerState.hasRunning
                ? '${project.metrics.workerState.runningCount} active worker process${project.metrics.workerState.runningCount == 1 ? '' : 'es'}'
                : 'No active worker processes reported.',
          ),
        ),
        const SizedBox(height: 16),
        _Panel(
          title: 'Review-needed netrunners',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (hotSessions.isEmpty)
                const Text(
                  'No in-progress or review sessions in this snapshot.',
                ),
              for (final session in hotSessions) ...[
                _SessionRow(
                  session: session,
                  onTap: () => onOpenSession(session.id),
                ),
                const SizedBox(height: 10),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _ProjectDocsTab extends StatelessWidget {
  const _ProjectDocsTab({required this.project});

  final ProjectWorkspaceSnapshot project;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        _MetricStrip(
          entries: [
            ('Doc groups', project.docs.groups.length.toString()),
            ('Total docs', project.docs.totalDocs.toString()),
            ('Pending', project.docs.pendingProposalCount.toString()),
          ],
        ),
        const SizedBox(height: 16),
        for (final group in project.docs.groups) ...[
          _Panel(
            title: group.docType,
            trailing: _StatusPill(
              label: '${group.pendingProposalCount} pending',
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final doc in group.docs) ...[
                  _ReadableRecordCard(
                    title: doc.title,
                    badges: [doc.docType],
                    body: doc.contentPreview,
                    caption: doc.targetedPendingProposals > 0
                        ? '${doc.targetedPendingProposals} targeted proposal${doc.targetedPendingProposals == 1 ? '' : 's'}'
                        : null,
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(height: 16),
        ],
      ],
    );
  }
}

class _ProjectNetrunnersTab extends StatelessWidget {
  const _ProjectNetrunnersTab({
    required this.project,
    required this.onOpenSession,
  });

  final ProjectWorkspaceSnapshot project;
  final ValueChanged<int> onOpenSession;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        _MetricStrip(
          entries: [
            ('Sessions', project.netrunners.length.toString()),
            ('Review', project.metrics.counts.review.toString()),
            ('Workers', project.metrics.workerState.runningCount.toString()),
          ],
        ),
        const SizedBox(height: 16),
        for (final session in project.netrunners) ...[
          _SessionRow(session: session, onTap: () => onOpenSession(session.id)),
          const SizedBox(height: 10),
        ],
      ],
    );
  }
}

class _SessionWorkspace extends StatelessWidget {
  const _SessionWorkspace({
    required this.detail,
    required this.onBack,
    required this.onAttachDocs,
    required this.onAssignMcpServers,
    required this.onChangeStatus,
    required this.onSetProposalStatus,
  });

  final NetrunnerDetailSnapshot detail;
  final VoidCallback onBack;
  final ValueChanged<SessionDetailRecord> onAttachDocs;
  final ValueChanged<SessionDetailRecord> onAssignMcpServers;
  final ValueChanged<SessionDetailRecord> onChangeStatus;
  final Future<void> Function(
    SessionDetailRecord session,
    DocProposalSummaryRecord proposal,
    String status,
  )
  onSetProposalStatus;

  @override
  Widget build(BuildContext context) {
    final session = detail.session;
    final theme = Theme.of(context);
    final tabs = const [
      Tab(text: 'Summary'),
      Tab(text: 'Report'),
      Tab(text: 'Docs'),
      Tab(text: 'MCPs'),
      Tab(text: 'Proposals'),
      Tab(text: 'Execution'),
    ];
    return DefaultTabController(
      length: tabs.length,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SafeArea(
            bottom: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(8, 10, 20, 0),
              child: Row(
                children: [
                  IconButton(
                    onPressed: onBack,
                    tooltip: 'Back to project',
                    icon: const Icon(Icons.arrow_back),
                  ),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Netrunner #${session.localId}',
                          style: theme.textTheme.headlineSmall?.copyWith(
                            fontWeight: FontWeight.w900,
                          ),
                        ),
                        Text(
                          session.headlineOrFallback,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                  _StatusPill(label: session.status),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
            child: Text(
              session.taskDescription.trim(),
              key: const ValueKey('session-task-description'),
              maxLines: 4,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(height: 8),
          TabBar(isScrollable: true, tabs: tabs),
          Expanded(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final wide = constraints.maxWidth >= 1060;
                final tabView = _SessionTabView(
                  session: session,
                  onSetProposalStatus: (proposal, status) =>
                      onSetProposalStatus(session, proposal, status),
                );
                final rail = _SessionSummaryRail(
                  session: session,
                  onAttachDocs: () => onAttachDocs(session),
                  onAssignMcpServers: () => onAssignMcpServers(session),
                  onChangeStatus: session.allowedStatusTargets.length <= 1
                      ? null
                      : () => onChangeStatus(session),
                );
                if (!wide) {
                  return tabView;
                }
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Expanded(child: tabView),
                    const VerticalDivider(width: 1),
                    SizedBox(width: 340, child: rail),
                  ],
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

extension on SessionDetailRecord {
  String get headlineOrFallback {
    final lines = taskDescription
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty);
    return lines.isEmpty ? 'Session detail' : lines.first;
  }
}

class _SessionTabView extends StatelessWidget {
  const _SessionTabView({
    required this.session,
    required this.onSetProposalStatus,
  });

  final SessionDetailRecord session;
  final Future<void> Function(DocProposalSummaryRecord proposal, String status)
  onSetProposalStatus;

  @override
  Widget build(BuildContext context) {
    final report = session.structuredFinalReport;
    return TabBarView(
      children: [
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _Panel(
              title: 'Session summary',
              child: _FactGrid(
                entries: [
                  ('Status', session.status),
                  ('Backend', session.backend),
                  ('Model', session.model),
                  ('Reasoning', session.reasoning),
                  (
                    'Write scope',
                    session.writeScope.isEmpty
                        ? 'None declared'
                        : session.writeScope.join(', '),
                  ),
                  ('Rework loops', session.reworkCount.toString()),
                  ('Forced stops', session.forcedStopCount.toString()),
                ],
              ),
            ),
            const SizedBox(height: 16),
            _WorkspaceNotice(
              title: 'Review posture',
              message:
                  session.proposals.any(
                    (proposal) => proposal.status == 'pending',
                  )
                  ? 'Pending proposals need an explicit approve or reject decision before closure.'
                  : 'No pending doc proposals are blocking review right now.',
              tone:
                  session.proposals.any(
                    (proposal) => proposal.status == 'pending',
                  )
                  ? _WorkspaceNoticeTone.warning
                  : _WorkspaceNoticeTone.info,
            ),
          ],
        ),
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _Panel(
              title: 'Structured report',
              child: report == null
                  ? Text(
                      session.reportRaw.trim().isEmpty
                          ? 'No structured final report has been stored yet.'
                          : 'Only a raw report is available right now.',
                    )
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _LabeledList(
                          title: 'Files changed',
                          values: report.filesChanged,
                        ),
                        const SizedBox(height: 12),
                        _LabeledList(
                          title: 'Checks run',
                          values: report.checksRun,
                        ),
                        const SizedBox(height: 12),
                        _LabeledList(
                          title: 'Commands run',
                          values: report.commandsRun,
                        ),
                        const SizedBox(height: 12),
                        _LabeledList(
                          title: 'Blockers',
                          values: report.blockers,
                          emptyLabel: 'No blockers recorded.',
                        ),
                        const SizedBox(height: 12),
                        _LabeledList(
                          title: 'Residual risks',
                          values: report.residualRisks,
                          emptyLabel: 'No residual risks recorded.',
                        ),
                      ],
                    ),
            ),
            if (session.reportRaw.trim().isNotEmpty) ...[
              const SizedBox(height: 16),
              _Panel(
                title: report == null ? 'Raw report' : 'Raw report fallback',
                child: SelectableText(session.reportRaw.trim()),
              ),
            ],
          ],
        ),
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _Panel(
              title: 'Attached docs',
              child: session.attachedDocs.isEmpty
                  ? const Text('No attached docs.')
                  : Column(
                      children: [
                        for (final doc in session.attachedDocs)
                          _ReadableRecordCard(
                            title: doc.title,
                            badges: [doc.docType, 'attached'],
                            body: doc.summary,
                          ),
                      ],
                    ),
            ),
            const SizedBox(height: 16),
            _Panel(
              title: 'Available docs',
              child: session.availableDocs.isEmpty
                  ? const Text('No project docs available for attachment.')
                  : Column(
                      children: [
                        for (final doc in session.availableDocs)
                          _ReadableRecordCard(
                            title: '#${doc.id} ${doc.title}',
                            badges: [doc.docType],
                            body: doc.summary,
                            emphasized: session.attachedDocs.any(
                              (attached) => attached.id == doc.id,
                            ),
                          ),
                      ],
                    ),
            ),
          ],
        ),
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _Panel(
              title: 'Assigned servers',
              child: session.mcpServers.isEmpty
                  ? const Text('No MCP assignments.')
                  : Column(
                      children: [
                        for (final server in session.mcpServers)
                          _ReadableRecordCard(
                            title: server.name,
                            badges: [
                              if (server.category.isNotEmpty) server.category,
                              'assigned',
                            ],
                            body: server.howTo,
                            caption: server.shortDescription,
                          ),
                      ],
                    ),
            ),
            const SizedBox(height: 16),
            _Panel(
              title: 'Available servers',
              child: session.availableMcpServers.isEmpty
                  ? const Text('No project MCP server catalog is available.')
                  : Column(
                      children: [
                        for (final server in session.availableMcpServers)
                          _ReadableRecordCard(
                            title: server.name,
                            badges: [
                              if (server.category.isNotEmpty) server.category,
                            ],
                            body: server.howTo,
                            caption: server.shortDescription,
                            emphasized: session.mcpServers.any(
                              (assigned) => assigned.name == server.name,
                            ),
                          ),
                      ],
                    ),
            ),
          ],
        ),
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _Panel(
              title: 'Review proposals',
              child: session.proposals.isEmpty
                  ? const Text('No proposals recorded.')
                  : Column(
                      children: [
                        for (final proposal in session.proposals)
                          _ProposalCard(
                            proposal: proposal,
                            onApprove: proposal.status == 'pending'
                                ? () =>
                                      onSetProposalStatus(proposal, 'approved')
                                : null,
                            onReject: proposal.status == 'pending'
                                ? () =>
                                      onSetProposalStatus(proposal, 'rejected')
                                : null,
                          ),
                      ],
                    ),
            ),
          ],
        ),
        ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (session.statusActionNote.isNotEmpty) ...[
              _WorkspaceNotice(
                title: 'Status transition note',
                message: session.statusActionNote,
                tone: session.statusActionNote.toLowerCase().contains('frozen')
                    ? _WorkspaceNoticeTone.warning
                    : _WorkspaceNoticeTone.info,
              ),
              const SizedBox(height: 16),
            ],
            _Panel(
              title: 'Worker/process state',
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    session.workerState.hasRunning
                        ? '${session.workerState.runningCount} active worker process${session.workerState.runningCount == 1 ? '' : 'es'}'
                        : 'No active worker processes.',
                  ),
                  const SizedBox(height: 12),
                  if (session.workerState.processes.isEmpty)
                    Text(
                      session.workerState.hasRunning
                          ? 'The bridge reports active work but no per-process detail rows.'
                          : 'No worker process detail rows are available.',
                    )
                  else
                    for (final process in session.workerState.processes)
                      _ReadableRecordCard(
                        title:
                            'PID ${process.pid} - launch epoch ${process.launchEpoch}',
                        badges: [
                          process.status,
                          process.alive ? 'alive' : 'stopped',
                          if (process.localId > 0)
                            'session #${process.localId}',
                        ],
                        body:
                            'Started ${process.startedAt}\nUpdated ${process.updatedAt}',
                        caption: process.stopReason.isNotEmpty
                            ? 'Stop reason: ${process.stopReason}'
                            : process.stoppedAt.isNotEmpty
                            ? 'Stopped at ${process.stoppedAt}'
                            : null,
                      ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            const _WorkspaceNotice(
              title: 'Launch controls',
              message:
                  'Launch and resume controls remain intentionally absent here until the GUI can truthfully route them into the explicit Fixer MCP launch flow.',
              tone: _WorkspaceNoticeTone.info,
            ),
          ],
        ),
      ],
    );
  }
}

class _SessionSummaryRail extends StatelessWidget {
  const _SessionSummaryRail({
    required this.session,
    required this.onAttachDocs,
    required this.onAssignMcpServers,
    required this.onChangeStatus,
  });

  final SessionDetailRecord session;
  final VoidCallback onAttachDocs;
  final VoidCallback onAssignMcpServers;
  final VoidCallback? onChangeStatus;

  @override
  Widget build(BuildContext context) {
    final pendingProposals = session.proposals
        .where((proposal) => proposal.status == 'pending')
        .length;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: Theme.of(
          context,
        ).colorScheme.surfaceContainerHighest.withValues(alpha: 0.42),
      ),
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _Panel(
            title: 'Workspace rail',
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    _StatusPill(label: session.status),
                    if (session.backend.isNotEmpty)
                      _StatusPill(label: session.backend),
                    if (session.model.isNotEmpty)
                      _StatusPill(label: session.model),
                  ],
                ),
                const SizedBox(height: 12),
                _FactGrid(
                  entries: [
                    ('Docs', session.attachedDocs.length.toString()),
                    ('MCPs', session.mcpServers.length.toString()),
                    ('Pending proposals', pendingProposals.toString()),
                    (
                      'Running workers',
                      session.workerState.runningCount.toString(),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: onAttachDocs,
                      icon: const Icon(Icons.library_books_outlined),
                      label: const Text('Attach docs'),
                    ),
                    OutlinedButton.icon(
                      onPressed: onAssignMcpServers,
                      icon: const Icon(Icons.extension_outlined),
                      label: const Text('Assign MCPs'),
                    ),
                    OutlinedButton.icon(
                      onPressed: onChangeStatus,
                      icon: const Icon(Icons.swap_horiz),
                      label: Text(
                        onChangeStatus == null
                            ? 'Status locked'
                            : 'Change status',
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          _WorkspaceNotice(
            title: pendingProposals > 0 ? 'Review needed' : 'Review state',
            message: pendingProposals > 0
                ? '$pendingProposals proposal${pendingProposals == 1 ? ' is' : 's are'} waiting for an explicit decision.'
                : 'No pending proposal decisions are waiting in this session.',
            tone: pendingProposals > 0
                ? _WorkspaceNoticeTone.warning
                : _WorkspaceNoticeTone.info,
          ),
        ],
      ),
    );
  }
}

class _FixerChatPanel extends StatefulWidget {
  const _FixerChatPanel({
    required this.binding,
    required this.loadThreadMessages,
    required this.sendThreadMessage,
    required this.loadThreadTurnStatus,
  });

  final FixerChatBindingRecord binding;
  final Future<ThreadMessagesSnapshot> Function(String threadId)
  loadThreadMessages;
  final Future<ThreadSendResult> Function(String threadId, String prompt)
  sendThreadMessage;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)
  loadThreadTurnStatus;

  @override
  State<_FixerChatPanel> createState() => _FixerChatPanelState();
}

class _FixerChatPanelState extends State<_FixerChatPanel> {
  String? _selectedThreadId;

  @override
  void didUpdateWidget(_FixerChatPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!widget.binding.sessions.any(
      (session) => _sessionThreadId(session) == _selectedThreadId,
    )) {
      _selectedThreadId = null;
    }
  }

  @override
  Widget build(BuildContext context) {
    final sessions = widget.binding.sessions;
    final selected = _selectedSession(sessions);
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 780;
        final threadPicker = _ChatThreadPicker(
          sessions: sessions,
          selectedThreadId: selected == null ? '' : _sessionThreadId(selected),
          onSelect: (session) {
            setState(() {
              _selectedThreadId = _sessionThreadId(session);
            });
          },
          compact: !wide,
        );
        final transcript = Expanded(
          child: selected == null
              ? const Center(child: Text('No truthful chat binding available.'))
              : _ChatTranscriptPane(
                  key: ValueKey(_sessionThreadId(selected)),
                  binding: widget.binding,
                  session: selected,
                  loadThreadMessages: widget.loadThreadMessages,
                  sendThreadMessage: widget.sendThreadMessage,
                  loadThreadTurnStatus: widget.loadThreadTurnStatus,
                ),
        );

        if (wide) {
          return Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SizedBox(width: 312, child: threadPicker),
              const VerticalDivider(width: 1),
              transcript,
            ],
          );
        }

        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SizedBox(height: 148, child: threadPicker),
            const Divider(height: 1),
            transcript,
          ],
        );
      },
    );
  }

  FixerChatSessionSummary? _selectedSession(
    List<FixerChatSessionSummary> sessions,
  ) {
    if (sessions.isEmpty) {
      return null;
    }
    final selectedThreadId = _selectedThreadId;
    if (selectedThreadId != null && selectedThreadId.isNotEmpty) {
      for (final session in sessions) {
        if (_sessionThreadId(session) == selectedThreadId) {
          return session;
        }
      }
    }
    return widget.binding.defaultSession ?? sessions.first;
  }
}

String _sessionThreadId(FixerChatSessionSummary session) {
  return session.codexSessionId.isNotEmpty
      ? session.codexSessionId
      : session.externalId;
}

class _AsyncChatBindingPanel extends StatefulWidget {
  const _AsyncChatBindingPanel({
    required this.projectId,
    required this.loadBinding,
    required this.loadThreadMessages,
    required this.sendThreadMessage,
    required this.loadThreadTurnStatus,
  });

  final int projectId;
  final Future<FixerChatBindingRecord> Function(int projectId) loadBinding;
  final Future<ThreadMessagesSnapshot> Function(String threadId)
  loadThreadMessages;
  final Future<ThreadSendResult> Function(String threadId, String prompt)
  sendThreadMessage;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)
  loadThreadTurnStatus;

  @override
  State<_AsyncChatBindingPanel> createState() => _AsyncChatBindingPanelState();
}

class _AsyncChatBindingPanelState extends State<_AsyncChatBindingPanel> {
  late Future<FixerChatBindingRecord> _bindingFuture;

  @override
  void initState() {
    super.initState();
    _bindingFuture = widget.loadBinding(widget.projectId);
  }

  @override
  void didUpdateWidget(_AsyncChatBindingPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.projectId != widget.projectId) {
      _bindingFuture = widget.loadBinding(widget.projectId);
    }
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<FixerChatBindingRecord>(
      future: _bindingFuture,
      builder: (context, snapshot) {
        if (snapshot.connectionState == ConnectionState.waiting &&
            !snapshot.hasData) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snapshot.hasError) {
          return Padding(
            padding: const EdgeInsets.all(20),
            child: _WorkspaceNotice(
              title: 'Chat binding unavailable',
              message: snapshot.error.toString(),
              tone: _WorkspaceNoticeTone.warning,
            ),
          );
        }
        return _FixerChatPanel(
          binding: snapshot.data!,
          loadThreadMessages: widget.loadThreadMessages,
          sendThreadMessage: widget.sendThreadMessage,
          loadThreadTurnStatus: widget.loadThreadTurnStatus,
        );
      },
    );
  }
}

class _ChatThreadPicker extends StatelessWidget {
  const _ChatThreadPicker({
    required this.sessions,
    required this.selectedThreadId,
    required this.onSelect,
    required this.compact,
  });

  final List<FixerChatSessionSummary> sessions;
  final String selectedThreadId;
  final ValueChanged<FixerChatSessionSummary> onSelect;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final background = Theme.of(context).colorScheme.surface;
    if (sessions.isEmpty) {
      return DecoratedBox(
        decoration: const BoxDecoration(
          color: _sidebarFill,
          border: Border(right: BorderSide(color: _chromeBorder)),
        ),
        child: const _EmptyChatState(
          icon: Icons.forum_outlined,
          title: 'No threads',
          message: 'No truthful chat binding is available yet.',
        ),
      );
    }
    final children = [
      for (final session in sessions)
        SizedBox(
          width: compact ? 280 : double.infinity,
          child: _ChatThreadTile(
            session: session,
            selected: selectedThreadId == _sessionThreadId(session),
            onTap: () => onSelect(session),
          ),
        ),
    ];
    return DecoratedBox(
      decoration: BoxDecoration(
        color: compact ? background : _sidebarFill,
        border: compact
            ? null
            : const Border(right: BorderSide(color: _chromeBorder)),
      ),
      child: compact
          ? ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.all(12),
              children: children,
            )
          : ListView(padding: const EdgeInsets.all(12), children: children),
    );
  }
}

class _ChatTranscriptPane extends StatefulWidget {
  const _ChatTranscriptPane({
    super.key,
    required this.binding,
    required this.session,
    required this.loadThreadMessages,
    required this.sendThreadMessage,
    required this.loadThreadTurnStatus,
  });

  final FixerChatBindingRecord binding;
  final FixerChatSessionSummary session;
  final Future<ThreadMessagesSnapshot> Function(String threadId)
  loadThreadMessages;
  final Future<ThreadSendResult> Function(String threadId, String prompt)
  sendThreadMessage;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)
  loadThreadTurnStatus;

  @override
  State<_ChatTranscriptPane> createState() => _ChatTranscriptPaneState();
}

class _ChatTranscriptPaneState extends State<_ChatTranscriptPane> {
  late Future<ThreadMessagesSnapshot> _messagesFuture;
  ThreadMessagesSnapshot? _latestTranscript;
  Timer? _turnPollTimer;
  ThreadSendResult? _activeTurn;
  ThreadTurnStatusSnapshot? _liveTurnStatus;
  String _pendingPrompt = '';
  String _liveTurnError = '';

  @override
  void initState() {
    super.initState();
    _messagesFuture = _loadMessages();
  }

  @override
  void didUpdateWidget(_ChatTranscriptPane oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_threadId != _threadIdFor(oldWidget.session)) {
      _stopTurnPolling();
      _latestTranscript = null;
      _activeTurn = null;
      _liveTurnStatus = null;
      _pendingPrompt = '';
      _liveTurnError = '';
      _messagesFuture = _loadMessages();
    }
  }

  @override
  void dispose() {
    _stopTurnPolling();
    super.dispose();
  }

  String get _threadId => _threadIdFor(widget.session);

  String _threadIdFor(FixerChatSessionSummary session) =>
      session.codexSessionId.isNotEmpty
      ? session.codexSessionId
      : session.externalId;

  Future<ThreadMessagesSnapshot> _loadMessages() async {
    final threadId = _threadId;
    final transcript = await widget.loadThreadMessages(threadId);
    if (mounted && threadId == _threadId) {
      setState(() {
        _latestTranscript = transcript;
      });
    }
    return transcript;
  }

  Future<void> _send(String prompt) async {
    _stopTurnPolling();
    setState(() {
      _pendingPrompt = prompt;
      _activeTurn = null;
      _liveTurnStatus = null;
      _liveTurnError = '';
    });

    final result = await widget.sendThreadMessage(_threadId, prompt);
    if (!mounted) {
      return;
    }
    if (result.streamId.isEmpty) {
      setState(() {
        _pendingPrompt = '';
        _activeTurn = null;
        _messagesFuture = _loadMessages();
      });
      return;
    }
    setState(() {
      _activeTurn = result;
    });
    await _pollTurnStatus();
    if (mounted && _activeTurn?.streamId == result.streamId) {
      _turnPollTimer = Timer.periodic(
        const Duration(milliseconds: 850),
        (_) => _pollTurnStatus(),
      );
    }
  }

  void _reloadMessages() {
    setState(() {
      _messagesFuture = _loadMessages();
    });
  }

  void _stopTurnPolling() {
    _turnPollTimer?.cancel();
    _turnPollTimer = null;
  }

  Future<void> _pollTurnStatus() async {
    final activeTurn = _activeTurn;
    if (activeTurn == null || activeTurn.streamId.isEmpty) {
      return;
    }
    try {
      final status = await widget.loadThreadTurnStatus(activeTurn.streamId);
      if (!mounted || _activeTurn?.streamId != activeTurn.streamId) {
        return;
      }
      setState(() {
        _liveTurnStatus = status;
        _liveTurnError = '';
      });
      if (status.done || status.expired) {
        _stopTurnPolling();
        if (mounted) {
          setState(() {
            _pendingPrompt = '';
            _activeTurn = null;
            _messagesFuture = _loadMessages();
          });
        }
      }
    } catch (error) {
      if (!mounted || _activeTurn?.streamId != activeTurn.streamId) {
        return;
      }
      setState(() {
        _liveTurnError = error.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final session = widget.session;
    final sessionId = _threadId;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 8),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _SectionTitle(
                      title: session.headline.isEmpty
                          ? 'Codex thread'
                          : session.headline,
                      subtitle: sessionId.isEmpty ? 'No session id' : sessionId,
                      compact: true,
                    ),
                  ],
                ),
              ),
              if (session.agentRole.isNotEmpty)
                _StatusPill(label: session.agentRole),
              const SizedBox(width: 8),
              _StatusPill(label: widget.binding.transcriptAvailability),
              const SizedBox(width: 4),
              IconButton(
                onPressed: sessionId.isEmpty ? null : _reloadMessages,
                tooltip: 'Reload transcript',
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
        ),
        Expanded(
          child: sessionId.isEmpty
              ? const _EmptyChatState(
                  icon: Icons.link_off,
                  title: 'No thread id',
                  message: 'This binding does not expose a thread id yet.',
                )
              : FutureBuilder<ThreadMessagesSnapshot>(
                  future: _messagesFuture,
                  builder: (context, snapshot) {
                    if (snapshot.connectionState == ConnectionState.waiting &&
                        !snapshot.hasData) {
                      return const Center(child: CircularProgressIndicator());
                    }
                    if (snapshot.hasError) {
                      return Padding(
                        padding: const EdgeInsets.all(20),
                        child: _WorkspaceNotice(
                          title: 'Transcript unavailable',
                          message: snapshot.error.toString(),
                          tone: _WorkspaceNoticeTone.warning,
                        ),
                      );
                    }
                    _latestTranscript = snapshot.data;
                    return _ThreadMessagesList(
                      transcript: snapshot.data!,
                      pendingUserText: _pendingPrompt,
                      liveTurnStatus: _liveTurnStatus,
                      liveTurnInProgress: _activeTurn != null,
                      liveTurnError: _liveTurnError,
                    );
                  },
                ),
        ),
        _ChatComposer(
          enabled:
              sessionId.isNotEmpty &&
              (_latestTranscript?.sendSupported ?? false),
          disabledReason: _composerDisabledReason(_latestTranscript),
          onSend: _send,
        ),
      ],
    );
  }
}

String _composerDisabledReason(ThreadMessagesSnapshot? transcript) {
  if (transcript == null) {
    return 'Composer waiting for thread capabilities';
  }
  if (!transcript.sendSupported) {
    return transcript.sendEndpoint.isEmpty
        ? 'Composer disabled: send is not exposed for this thread'
        : 'Composer disabled: ${transcript.sendEndpoint} is unavailable';
  }
  return '';
}

class _ThreadMessagesList extends StatelessWidget {
  const _ThreadMessagesList({
    required this.transcript,
    required this.pendingUserText,
    required this.liveTurnStatus,
    required this.liveTurnInProgress,
    required this.liveTurnError,
  });

  final ThreadMessagesSnapshot transcript;
  final String pendingUserText;
  final ThreadTurnStatusSnapshot? liveTurnStatus;
  final bool liveTurnInProgress;
  final String liveTurnError;

  @override
  Widget build(BuildContext context) {
    final liveMessages = _liveMessages();
    if (!transcript.transcriptAvailable && liveMessages.isEmpty) {
      return Padding(
        padding: const EdgeInsets.all(20),
        child: _WorkspaceNotice(
          title: 'Transcript unavailable',
          message: transcript.unsupportedReason.isNotEmpty
              ? transcript.unsupportedReason
              : 'No message transcript is available from the Serverpod bridge.',
          tone: _WorkspaceNoticeTone.info,
        ),
      );
    }
    if (transcript.messages.isEmpty && liveMessages.isEmpty) {
      return const _EmptyChatState(
        icon: Icons.chat_bubble_outline,
        title: 'Empty transcript',
        message: 'The bound thread is available but has no stored messages.',
      );
    }
    final messages = [...transcript.messages, ...liveMessages];
    return ListView.builder(
      reverse: true,
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
      itemCount: messages.length,
      itemBuilder: (context, index) {
        final message = messages[messages.length - index - 1];
        return _ChatBubble(message: message);
      },
    );
  }

  List<ThreadMessageRecord> _liveMessages() {
    final messages = <ThreadMessageRecord>[];
    if (pendingUserText.isNotEmpty) {
      messages.add(
        ThreadMessageRecord(
          id: 'live-user',
          role: 'user',
          text: pendingUserText,
          createdAt: '',
          source: 'pending_turn',
        ),
      );
    }
    if (liveTurnInProgress ||
        liveTurnStatus != null ||
        liveTurnError.isNotEmpty) {
      final status = liveTurnStatus;
      final text = liveTurnError.isNotEmpty
          ? liveTurnError
          : status?.assistantText.isNotEmpty == true
          ? status!.assistantText
          : status?.progressText.isNotEmpty == true
          ? status!.progressText
          : 'Turn started; waiting for Codex events.';
      final source = status == null
          ? 'live_turn'
          : 'live_turn ${status.eventCount} event(s)';
      messages.add(
        ThreadMessageRecord(
          id: 'live-assistant',
          role: 'assistant',
          text: text,
          createdAt: status?.startedAt ?? '',
          source: source,
        ),
      );
    }
    return messages;
  }
}

class _ChatComposer extends StatefulWidget {
  const _ChatComposer({
    required this.enabled,
    required this.disabledReason,
    required this.onSend,
  });

  final bool enabled;
  final String disabledReason;
  final Future<void> Function(String prompt) onSend;

  @override
  State<_ChatComposer> createState() => _ChatComposerState();
}

class _ChatComposerState extends State<_ChatComposer> {
  final _controller = TextEditingController();
  bool _sending = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final prompt = _controller.text.trim();
    if (!widget.enabled || _sending || prompt.isEmpty) {
      return;
    }
    setState(() {
      _sending = true;
    });
    try {
      await widget.onSend(prompt);
      if (mounted) {
        _controller.clear();
      }
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(error.toString())));
      }
    } finally {
      if (mounted) {
        setState(() {
          _sending = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final enabled = widget.enabled && !_sending;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(top: BorderSide(color: theme.dividerColor)),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          children: [
            Expanded(
              child: TextField(
                controller: _controller,
                enabled: enabled,
                minLines: 1,
                maxLines: 3,
                decoration: InputDecoration(
                  hintText: widget.enabled
                      ? 'Message this thread'
                      : widget.disabledReason,
                  prefixIcon: Icon(
                    widget.enabled
                        ? Icons.chat_bubble_outline
                        : Icons.lock_outline,
                  ),
                  filled: true,
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(8),
                  ),
                ),
                onSubmitted: (_) => _submit(),
              ),
            ),
            const SizedBox(width: 8),
            IconButton.filled(
              onPressed: enabled ? _submit : null,
              tooltip: widget.enabled ? 'Send' : 'Send unavailable',
              icon: _sending
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.send),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({required this.message});

  final ThreadMessageRecord message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isUser = message.role == 'user';
    final isTool = message.role == 'tool';
    final isInternal = message.kind == 'internal_context';
    final roleLabel = message.role.isEmpty ? 'message' : message.role;
    final headerLabel = isInternal
        ? 'internal'
        : isTool
        ? 'tool'
        : roleLabel;
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 820),
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
        decoration: BoxDecoration(
          color: isUser
              ? theme.colorScheme.primaryContainer.withValues(alpha: 0.72)
              : theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: theme.colorScheme.outlineVariant),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  isInternal
                      ? Icons.integration_instructions_outlined
                      : isTool
                      ? Icons.build_circle_outlined
                      : isUser
                      ? Icons.person_outline
                      : Icons.auto_awesome,
                  size: 16,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 6),
                Text(
                  headerLabel,
                  style: theme.textTheme.labelMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    fontWeight: FontWeight.w800,
                  ),
                ),
                const Spacer(),
                if (message.createdAt.isNotEmpty)
                  Text(
                    message.createdAt,
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                IconButton(
                  onPressed: () => _copyMessage(context, message.text),
                  tooltip: 'Copy message',
                  visualDensity: VisualDensity.compact,
                  iconSize: 18,
                  icon: const Icon(Icons.copy),
                ),
              ],
            ),
            const SizedBox(height: 8),
            message.collapsed
                ? _CollapsedMessage(message: message)
                : isUser || isTool || isInternal
                ? SelectableText(message.text)
                : _MarkdownMessage(text: message.text),
            if (message.source.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                message.source,
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Future<void> _copyMessage(BuildContext context, String text) async {
    await Clipboard.setData(ClipboardData(text: text));
    if (!context.mounted) {
      return;
    }
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Copied message.')));
  }
}

class _CollapsedMessage extends StatelessWidget {
  const _CollapsedMessage({required this.message});

  final ThreadMessageRecord message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final summary = message.summary.isNotEmpty
        ? message.summary
        : message.text.split('\n').first;
    return Theme(
      data: theme.copyWith(dividerColor: Colors.transparent),
      child: ExpansionTile(
        tilePadding: EdgeInsets.zero,
        childrenPadding: const EdgeInsets.only(top: 8),
        initiallyExpanded: false,
        title: Text(
          summary,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.bodyMedium?.copyWith(
            fontWeight: FontWeight.w700,
          ),
        ),
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: SelectableText(
              message.text,
              style: theme.textTheme.bodySmall?.copyWith(
                height: 1.35,
                fontFamily: message.role == 'tool' ? 'monospace' : null,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _MarkdownMessage extends StatelessWidget {
  const _MarkdownMessage({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bodyStyle =
        theme.textTheme.bodyMedium?.copyWith(
          color: theme.colorScheme.onSurface,
          height: 1.4,
        ) ??
        TextStyle(color: theme.colorScheme.onSurface, height: 1.4);
    final codeStyle =
        theme.textTheme.bodyMedium?.copyWith(
          fontFamily: 'monospace',
          color: theme.colorScheme.onSurface,
          backgroundColor: theme.colorScheme.surfaceContainerHighest,
        ) ??
        TextStyle(
          fontFamily: 'monospace',
          color: theme.colorScheme.onSurface,
          backgroundColor: theme.colorScheme.surfaceContainerHighest,
        );

    return MarkdownWidget(
      data: text,
      shrinkWrap: true,
      selectable: true,
      physics: const NeverScrollableScrollPhysics(),
      padding: EdgeInsets.zero,
      config: MarkdownConfig.defaultConfig.copy(
        configs: [
          PConfig(textStyle: bodyStyle),
          H1Config(style: theme.textTheme.titleLarge ?? bodyStyle),
          H2Config(style: theme.textTheme.titleMedium ?? bodyStyle),
          H3Config(style: theme.textTheme.titleSmall ?? bodyStyle),
          CodeConfig(style: codeStyle),
          PreConfig(
            textStyle: codeStyle.copyWith(backgroundColor: null),
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: const BorderRadius.all(Radius.circular(8)),
              border: Border.all(color: theme.colorScheme.outlineVariant),
            ),
            padding: const EdgeInsets.all(12),
            margin: const EdgeInsets.symmetric(vertical: 8),
          ),
          LinkConfig(
            style: bodyStyle.copyWith(
              color: theme.colorScheme.primary,
              decoration: TextDecoration.underline,
            ),
          ),
          const ListConfig(marginLeft: 20, marginBottom: 4),
        ],
      ),
    );
  }
}

class _ChatThreadTile extends StatelessWidget {
  const _ChatThreadTile({
    required this.session,
    required this.selected,
    required this.onTap,
  });

  final FixerChatSessionSummary session;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(8),
        child: Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: selected
                ? theme.colorScheme.primaryContainer.withValues(alpha: 0.78)
                : theme.colorScheme.surface,
            borderRadius: BorderRadius.circular(8),
            border: Border.all(
              color: selected
                  ? theme.colorScheme.primary.withValues(alpha: 0.45)
                  : theme.colorScheme.outlineVariant,
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    session.transcriptAvailable
                        ? Icons.forum_outlined
                        : Icons.info_outline,
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      session.headline.isEmpty
                          ? 'Codex thread'
                          : session.headline,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                session.lastActivityAt,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  if (session.agentRole.isNotEmpty)
                    _StatusPill(label: session.agentRole),
                  if (session.backend.isNotEmpty)
                    _StatusPill(label: session.backend),
                  _StatusPill(
                    label: session.transcriptAvailable
                        ? 'transcript'
                        : 'metadata',
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle({
    required this.title,
    required this.subtitle,
    this.compact = false,
  });

  final String title;
  final String subtitle;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          title,
          maxLines: compact ? 1 : 2,
          overflow: TextOverflow.ellipsis,
          style:
              (compact
                      ? theme.textTheme.titleLarge
                      : theme.textTheme.headlineSmall)
                  ?.copyWith(fontWeight: FontWeight.w900),
        ),
        if (subtitle.isNotEmpty) ...[
          const SizedBox(height: 4),
          Text(
            subtitle,
            maxLines: compact ? 1 : 2,
            overflow: TextOverflow.ellipsis,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }
}

class _EmptyChatState extends StatelessWidget {
  const _EmptyChatState({
    required this.icon,
    required this.title,
    required this.message,
  });

  final IconData icon;
  final String title;
  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 380),
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: 36, color: theme.colorScheme.onSurfaceVariant),
              const SizedBox(height: 12),
              Text(
                title,
                textAlign: TextAlign.center,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                message,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _MetricStrip extends StatelessWidget {
  const _MetricStrip({required this.entries});

  final List<(String, String)> entries;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 10,
      runSpacing: 10,
      children: [
        for (final entry in entries)
          _MetricChip(label: entry.$1, value: entry.$2),
      ],
    );
  }
}

class _MetricChip extends StatelessWidget {
  const _MetricChip({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      constraints: const BoxConstraints(minWidth: 96),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            value,
            style: theme.textTheme.titleLarge?.copyWith(
              fontWeight: FontWeight.w900,
            ),
          ),
          Text(
            label,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: theme.textTheme.labelMedium?.copyWith(
          color: theme.colorScheme.onSecondaryContainer,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _Panel extends StatelessWidget {
  const _Panel({required this.title, required this.child, this.trailing});

  final String title;
  final Widget child;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    title,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                ),
                ?trailing,
              ],
            ),
            const SizedBox(height: 12),
            child,
          ],
        ),
      ),
    );
  }
}

class _FactGrid extends StatelessWidget {
  const _FactGrid({required this.entries});

  final List<(String, String)> entries;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 10,
      runSpacing: 10,
      children: [
        for (final entry in entries)
          SizedBox(
            width: 170,
            child: _MetricChip(
              label: entry.$1,
              value: entry.$2.isEmpty ? 'Not recorded' : entry.$2,
            ),
          ),
      ],
    );
  }
}

class _ReadableRecordCard extends StatelessWidget {
  const _ReadableRecordCard({
    required this.title,
    required this.badges,
    required this.body,
    this.caption,
    this.emphasized = false,
    this.child,
  });

  final String title;
  final List<String> badges;
  final String body;
  final String? caption;
  final bool emphasized;
  final Widget? child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: emphasized
            ? theme.colorScheme.primaryContainer.withValues(alpha: 0.55)
            : theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.55),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: theme.textTheme.titleSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          if (badges.where((badge) => badge.isNotEmpty).isNotEmpty) ...[
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final badge in badges)
                  if (badge.isNotEmpty) _StatusPill(label: badge),
              ],
            ),
          ],
          const SizedBox(height: 10),
          Text(body),
          if (caption != null && caption!.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              caption!,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (child != null) ...[const SizedBox(height: 12), child!],
        ],
      ),
    );
  }
}

class _ProposalCard extends StatelessWidget {
  const _ProposalCard({required this.proposal, this.onApprove, this.onReject});

  final DocProposalSummaryRecord proposal;
  final VoidCallback? onApprove;
  final VoidCallback? onReject;

  @override
  Widget build(BuildContext context) {
    return _ReadableRecordCard(
      title: '#${proposal.localId} ${proposal.proposedDocType}',
      badges: [
        proposal.status,
        if (proposal.targetProjectDocId > 0)
          'targets doc #${proposal.targetProjectDocId}',
      ],
      body: proposal.proposedContent,
      caption: proposal.status == 'pending'
          ? null
          : 'Review decision already recorded.',
      emphasized: proposal.status == 'pending',
      child: proposal.status == 'pending'
          ? Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  onPressed: onApprove,
                  child: const Text('Approve'),
                ),
                OutlinedButton(
                  onPressed: onReject,
                  child: const Text('Reject'),
                ),
              ],
            )
          : null,
    );
  }
}

enum _WorkspaceNoticeTone { info, warning }

class _WorkspaceNotice extends StatelessWidget {
  const _WorkspaceNotice({
    required this.title,
    required this.message,
    required this.tone,
  });

  final String title;
  final String message;
  final _WorkspaceNoticeTone tone;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: tone == _WorkspaceNoticeTone.warning
            ? scheme.errorContainer.withValues(alpha: 0.55)
            : scheme.surfaceContainerHighest.withValues(alpha: 0.75),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: Theme.of(
              context,
            ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w800),
          ),
          const SizedBox(height: 6),
          Text(message),
        ],
      ),
    );
  }
}

class _LabeledList extends StatelessWidget {
  const _LabeledList({
    required this.title,
    required this.values,
    this.emptyLabel = 'None recorded.',
  });

  final String title;
  final List<String> values;
  final String emptyLabel;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          title,
          style: Theme.of(
            context,
          ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 6),
        if (values.isEmpty)
          Text(emptyLabel)
        else
          for (final value in values) ...[
            Text('- $value'),
            const SizedBox(height: 4),
          ],
      ],
    );
  }
}

class _ProjectCard extends StatelessWidget {
  const _ProjectCard({
    required this.project,
    required this.selected,
    required this.onTap,
  });

  final ProjectCardRecord project;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      color: selected
          ? theme.colorScheme.primaryContainer
          : theme.colorScheme.surface,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(8),
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      project.project.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  if (project.hasPendingReview)
                    const _StatusPill(label: 'review'),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                project.project.cwd,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 10),
              Text(
                project.latestActivityLabel.isEmpty
                    ? 'No recent activity'
                    : project.latestActivityLabel,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _StatusPill(label: 'P ${project.counts.pending}'),
                  _StatusPill(label: 'I ${project.counts.inProgress}'),
                  _StatusPill(label: 'R ${project.counts.review}'),
                  if (project.hasActiveWorkers)
                    const _StatusPill(label: 'workers'),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SessionRow extends StatelessWidget {
  const _SessionRow({required this.session, required this.onTap});

  final NetrunnerSummaryRecord session;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: ListTile(
        onTap: onTap,
        title: Text(
          '#${session.localId} ${session.headline}',
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.titleSmall?.copyWith(
            fontWeight: FontWeight.w800,
          ),
        ),
        subtitle: Text(
          session.taskPreview,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: _StatusPill(label: session.status),
      ),
    );
  }
}

class _TaskDraft {
  const _TaskDraft({required this.taskDescription, required this.writeScope});

  final String taskDescription;
  final List<String> writeScope;
}

class _CreateTaskDialog extends StatefulWidget {
  const _CreateTaskDialog();

  @override
  State<_CreateTaskDialog> createState() => _CreateTaskDialogState();
}

class _CreateTaskDialogState extends State<_CreateTaskDialog> {
  final _descriptionController = TextEditingController();
  final _scopeController = TextEditingController();

  @override
  void dispose() {
    _descriptionController.dispose();
    _scopeController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Create Netrunner task'),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _descriptionController,
              maxLines: 5,
              decoration: const InputDecoration(
                labelText: 'Task description',
                hintText:
                    'Describe the operator action task for the new session.',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _scopeController,
              decoration: const InputDecoration(
                labelText: 'Write scope',
                hintText: 'Comma-separated paths, optional',
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () {
            final taskDescription = _descriptionController.text.trim();
            if (taskDescription.isEmpty) {
              return;
            }
            final writeScope = _scopeController.text
                .split(',')
                .map((item) => item.trim())
                .where((item) => item.isNotEmpty)
                .toList();
            Navigator.of(context).pop(
              _TaskDraft(
                taskDescription: taskDescription,
                writeScope: writeScope,
              ),
            );
          },
          child: const Text('Create'),
        ),
      ],
    );
  }
}

class _ChoiceDialog<T> extends StatelessWidget {
  const _ChoiceDialog({
    required this.title,
    required this.items,
    required this.labelBuilder,
  });

  final String title;
  final List<T> items;
  final String Function(T item) labelBuilder;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(title),
      content: SizedBox(
        width: 320,
        child: ListView(
          shrinkWrap: true,
          children: [
            for (final item in items)
              ListTile(
                title: Text(labelBuilder(item)),
                onTap: () => Navigator.of(context).pop(item),
              ),
          ],
        ),
      ),
    );
  }
}

class _MultiSelectDialog<T> extends StatefulWidget {
  const _MultiSelectDialog({
    required this.title,
    required this.items,
    required this.initiallySelected,
    required this.labelBuilder,
    required this.detailBuilder,
  });

  final String title;
  final List<T> items;
  final Set<T> initiallySelected;
  final String Function(T item) labelBuilder;
  final String Function(T item) detailBuilder;

  @override
  State<_MultiSelectDialog<T>> createState() => _MultiSelectDialogState<T>();
}

class _MultiSelectDialogState<T> extends State<_MultiSelectDialog<T>> {
  late final Set<T> _selected = {...widget.initiallySelected};

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 520,
        child: ListView(
          shrinkWrap: true,
          children: [
            for (final item in widget.items)
              CheckboxListTile(
                value: _selected.contains(item),
                title: Text(widget.labelBuilder(item)),
                subtitle: Text(widget.detailBuilder(item)),
                controlAffinity: ListTileControlAffinity.leading,
                onChanged: (selected) {
                  setState(() {
                    if (selected ?? false) {
                      _selected.add(item);
                    } else {
                      _selected.remove(item);
                    }
                  });
                },
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_selected.toList()),
          child: const Text('Apply'),
        ),
      ],
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 480),
        child: Card(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.warning_amber_rounded, size: 40),
                const SizedBox(height: 12),
                Text(message, textAlign: TextAlign.center),
                const SizedBox(height: 16),
                FilledButton(onPressed: onRetry, child: const Text('Retry')),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
