import 'package:flutter/material.dart';

import 'bridge_repository.dart';
import 'models.dart';

void main() {
  const bridgeUrl = String.fromEnvironment(
    'FIXER_DESKTOP_BRIDGE_URL',
    defaultValue: 'http://127.0.0.1:8765',
  );
  runApp(
    FixerDesktopApp(
      repository: HttpDesktopBridgeRepository(baseUri: Uri.parse(bridgeUrl)),
      bridgeUrl: bridgeUrl,
    ),
  );
}

class FixerDesktopApp extends StatelessWidget {
  const FixerDesktopApp({
    super.key,
    required this.repository,
    required this.bridgeUrl,
  });

  final DesktopBridgeRepository repository;
  final String bridgeUrl;

  @override
  Widget build(BuildContext context) {
    final baseTheme = ThemeData(
      useMaterial3: true,
      fontFamily: 'Georgia',
      colorScheme: const ColorScheme.light(
        brightness: Brightness.light,
        primary: Color(0xFF0D5C63),
        onPrimary: Colors.white,
        secondary: Color(0xFFF3A712),
        onSecondary: Color(0xFF1B1B1B),
        surface: Color(0xFFF8F4EC),
        onSurface: Color(0xFF1D2A30),
        error: Color(0xFFB3261E),
        onError: Colors.white,
      ),
    );

    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'Fixer Desktop',
      theme: baseTheme.copyWith(
        scaffoldBackgroundColor: const Color(0xFFEAE5DA),
        textTheme: baseTheme.textTheme.apply(
          bodyColor: const Color(0xFF1D2A30),
          displayColor: const Color(0xFF1D2A30),
        ),
        cardTheme: const CardThemeData(
          color: Color(0xCCFFFDF8),
          elevation: 0,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(28)),
          ),
        ),
      ),
      home: WorkspaceScreen(repository: repository, bridgeUrl: bridgeUrl),
    );
  }
}

class WorkspaceScreen extends StatefulWidget {
  const WorkspaceScreen({
    super.key,
    required this.repository,
    required this.bridgeUrl,
  });

  final DesktopBridgeRepository repository;
  final String bridgeUrl;

  @override
  State<WorkspaceScreen> createState() => _WorkspaceScreenState();
}

class _WorkspaceScreenState extends State<WorkspaceScreen> {
  List<ProjectSummary> _projects = const [];
  ProjectDashboard? _dashboard;
  SessionDetail? _sessionDetail;
  int? _selectedProjectId;
  int? _selectedSessionId;
  bool _loading = true;
  bool _refreshing = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    setState(() {
      _loading = true;
      _errorMessage = null;
    });
    await _loadProjects();
  }

  Future<void> _refresh() async {
    setState(() {
      _refreshing = true;
    });
    try {
      await _loadProjects(preferredProjectId: _selectedProjectId);
    } finally {
      if (mounted) {
        setState(() {
          _refreshing = false;
        });
      }
    }
  }

  Future<void> _loadProjects({int? preferredProjectId}) async {
    try {
      final projects = await widget.repository.fetchProjects();
      final projectId =
          preferredProjectId ??
          _selectedProjectId ??
          (projects.isNotEmpty ? projects.first.id : null);
      ProjectDashboard? dashboard;
      SessionDetail? detail;
      int? sessionId = _selectedSessionId;

      if (projectId != null) {
        dashboard = await widget.repository.fetchDashboard(projectId);
        if (dashboard.sessions.isNotEmpty) {
          final matchingSession = dashboard.sessions.any(
            (session) => session.id == sessionId,
          );
          sessionId = matchingSession ? sessionId : dashboard.sessions.first.id;
          if (sessionId != null) {
            detail = await widget.repository.fetchSession(sessionId);
          }
        } else {
          sessionId = null;
        }
      }

      if (!mounted) {
        return;
      }

      setState(() {
        _projects = projects;
        _selectedProjectId = projectId;
        _dashboard = dashboard;
        _selectedSessionId = sessionId;
        _sessionDetail = detail;
        _loading = false;
        _errorMessage = null;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _loading = false;
        _errorMessage = error.toString();
      });
    }
  }

  Future<void> _selectProject(int projectId) async {
    setState(() {
      _selectedProjectId = projectId;
      _selectedSessionId = null;
      _loading = true;
    });
    await _loadProjects(preferredProjectId: projectId);
  }

  Future<void> _selectSession(int sessionId) async {
    setState(() {
      _selectedSessionId = sessionId;
    });
    try {
      final detail = await widget.repository.fetchSession(sessionId);
      if (!mounted) {
        return;
      }
      setState(() {
        _sessionDetail = detail;
        _errorMessage = null;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _errorMessage = error.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          const _Atmosphere(),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                children: [
                  _TopBar(
                    bridgeUrl: widget.bridgeUrl,
                    loading: _refreshing,
                    onRefresh: _refresh,
                  ),
                  const SizedBox(height: 20),
                  Expanded(
                    child: _loading
                        ? const Center(child: CircularProgressIndicator())
                        : _errorMessage != null
                        ? _ErrorState(
                            message: _errorMessage!,
                            onRetry: _bootstrap,
                          )
                        : LayoutBuilder(
                            builder: (context, constraints) {
                              final compact = constraints.maxWidth < 1220;
                              if (compact) {
                                return ListView(
                                  children: [
                                    SizedBox(
                                      height: 360,
                                      child: _ProjectRail(
                                        projects: _projects,
                                        selectedProjectId: _selectedProjectId,
                                        onProjectSelected: _selectProject,
                                      ),
                                    ),
                                    const SizedBox(height: 16),
                                    SizedBox(
                                      height: 680,
                                      child: _DashboardPanel(
                                        dashboard: _dashboard,
                                        selectedSessionId: _selectedSessionId,
                                        onSessionSelected: _selectSession,
                                      ),
                                    ),
                                    const SizedBox(height: 16),
                                    SizedBox(
                                      height: 640,
                                      child: _SessionWorkspace(
                                        detail: _sessionDetail,
                                      ),
                                    ),
                                  ],
                                );
                              }
                              return Row(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  SizedBox(
                                    width: 290,
                                    child: _ProjectRail(
                                      projects: _projects,
                                      selectedProjectId: _selectedProjectId,
                                      onProjectSelected: _selectProject,
                                    ),
                                  ),
                                  const SizedBox(width: 16),
                                  Expanded(
                                    child: _DashboardPanel(
                                      dashboard: _dashboard,
                                      selectedSessionId: _selectedSessionId,
                                      onSessionSelected: _selectSession,
                                    ),
                                  ),
                                  const SizedBox(width: 16),
                                  SizedBox(
                                    width: 420,
                                    child: _SessionWorkspace(
                                      detail: _sessionDetail,
                                    ),
                                  ),
                                ],
                              );
                            },
                          ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Atmosphere extends StatelessWidget {
  const _Atmosphere();

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFF7F2E8), Color(0xFFE8E2D1), Color(0xFFD6E5E6)],
        ),
      ),
      child: Stack(
        children: const [
          Positioned(
            top: -80,
            left: -40,
            child: _GlowOrb(color: Color(0x55F3A712), size: 260),
          ),
          Positioned(
            right: -50,
            top: 80,
            child: _GlowOrb(color: Color(0x550D5C63), size: 320),
          ),
          Positioned(
            bottom: -40,
            left: 180,
            child: _GlowOrb(color: Color(0x40FFFFFF), size: 220),
          ),
        ],
      ),
    );
  }
}

class _GlowOrb extends StatelessWidget {
  const _GlowOrb({required this.color, required this.size});

  final Color color;
  final double size;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        borderRadius: BorderRadius.circular(size),
      ),
    );
  }
}

class _TopBar extends StatelessWidget {
  const _TopBar({
    required this.bridgeUrl,
    required this.loading,
    required this.onRefresh,
  });

  final String bridgeUrl;
  final bool loading;
  final VoidCallback onRefresh;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Fixer Desktop',
                style: Theme.of(
                  context,
                ).textTheme.displaySmall?.copyWith(fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 6),
              Text(
                'Overseer-first desktop slice wired to the local Fixer control plane.',
                style: Theme.of(context).textTheme.titleMedium,
              ),
            ],
          ),
        ),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          decoration: BoxDecoration(
            color: const Color(0xBBFFFFFF),
            borderRadius: BorderRadius.circular(20),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.hub_outlined, size: 18),
              const SizedBox(width: 8),
              Text(
                bridgeUrl,
                style: Theme.of(
                  context,
                ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
              ),
            ],
          ),
        ),
        const SizedBox(width: 12),
        FilledButton.icon(
          onPressed: loading ? null : onRefresh,
          icon: loading
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.sync),
          label: const Text('Refresh'),
        ),
      ],
    );
  }
}

class _ProjectRail extends StatelessWidget {
  const _ProjectRail({
    required this.projects,
    required this.selectedProjectId,
    required this.onProjectSelected,
  });

  final List<ProjectSummary> projects;
  final int? selectedProjectId;
  final ValueChanged<int> onProjectSelected;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Overseer',
              style: Theme.of(
                context,
              ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            Text(
              'Project selection and live orchestration status.',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 18),
            Expanded(
              child: ListView.separated(
                itemCount: projects.length,
                separatorBuilder: (context, index) =>
                    const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final project = projects[index];
                  final selected = project.id == selectedProjectId;
                  return AnimatedContainer(
                    duration: const Duration(milliseconds: 180),
                    decoration: BoxDecoration(
                      color: selected
                          ? const Color(0xFF0D5C63)
                          : const Color(0xFFFDF8EF),
                      borderRadius: BorderRadius.circular(22),
                    ),
                    child: InkWell(
                      borderRadius: BorderRadius.circular(22),
                      onTap: () => onProjectSelected(project.id),
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              project.name,
                              style: Theme.of(context).textTheme.titleLarge
                                  ?.copyWith(
                                    color: selected ? Colors.white : null,
                                    fontWeight: FontWeight.w700,
                                  ),
                            ),
                            const SizedBox(height: 6),
                            Text(
                              project.cwd,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    color: selected
                                        ? Colors.white70
                                        : const Color(0xFF556B73),
                                  ),
                            ),
                            const SizedBox(height: 14),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                _InfoChip(
                                  label:
                                      '${project.sessionCounts['in_progress'] ?? 0} active',
                                  selected: selected,
                                ),
                                _InfoChip(
                                  label:
                                      '${project.pendingDocProposals} proposals',
                                  selected: selected,
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DashboardPanel extends StatelessWidget {
  const _DashboardPanel({
    required this.dashboard,
    required this.selectedSessionId,
    required this.onSessionSelected,
  });

  final ProjectDashboard? dashboard;
  final int? selectedSessionId;
  final ValueChanged<int> onSessionSelected;

  @override
  Widget build(BuildContext context) {
    final activeDashboard = dashboard;
    if (activeDashboard == null) {
      return const Card(child: Center(child: Text('No project selected.')));
    }

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              activeDashboard.name,
              style: Theme.of(
                context,
              ).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            Text(
              activeDashboard.cwd,
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: const Color(0xFF556B73)),
            ),
            const SizedBox(height: 20),
            Wrap(
              spacing: 14,
              runSpacing: 14,
              children: [
                _MetricCard(
                  title: 'Active sessions',
                  value: '${activeDashboard.sessionCounts['in_progress'] ?? 0}',
                  accent: const Color(0xFF0D5C63),
                ),
                _MetricCard(
                  title: 'Review queue',
                  value: '${activeDashboard.pendingDocProposals}',
                  accent: const Color(0xFFF3A712),
                ),
                _MetricCard(
                  title: 'Live workers',
                  value: '${activeDashboard.activeWorkerCount}',
                  accent: const Color(0xFF3C7A89),
                ),
              ],
            ),
            const SizedBox(height: 18),
            if (activeDashboard.runStatus != null)
              _RunStatusBanner(runStatus: activeDashboard.runStatus!),
            const SizedBox(height: 18),
            Text(
              'Sessions',
              style: Theme.of(
                context,
              ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.separated(
                itemCount: activeDashboard.sessions.length,
                separatorBuilder: (context, index) =>
                    const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final session = activeDashboard.sessions[index];
                  return _SessionCard(
                    session: session,
                    selected: session.id == selectedSessionId,
                    onTap: () => onSessionSelected(session.id),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SessionWorkspace extends StatelessWidget {
  const _SessionWorkspace({required this.detail});

  final SessionDetail? detail;

  @override
  Widget build(BuildContext context) {
    final activeDetail = detail;
    if (activeDetail == null) {
      return const Card(
        child: Center(
          child: Text('Select a session to inspect the workspace.'),
        ),
      );
    }

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: ListView(
          children: [
            Text(
              'Session ${activeDetail.id}',
              style: Theme.of(
                context,
              ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            Text(
              activeDetail.taskTitle,
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _StatusPill(label: activeDetail.status),
                _StatusPill(label: activeDetail.cliBackend),
                _StatusPill(label: activeDetail.cliModel),
                _StatusPill(label: activeDetail.cliReasoning),
              ],
            ),
            const SizedBox(height: 16),
            _SectionCard(
              title: 'Task brief',
              child: Text(activeDetail.taskDescription),
            ),
            const SizedBox(height: 12),
            _SectionCard(
              title: 'Attached docs',
              child: Column(
                children: activeDetail.attachedDocs
                    .map(
                      (doc) =>
                          _InlineRow(title: doc.title, trailing: doc.docType),
                    )
                    .toList(),
              ),
            ),
            const SizedBox(height: 12),
            _SectionCard(
              title: 'MCP servers',
              child: Column(
                children: activeDetail.mcpServers
                    .map(
                      (server) => _InlineRow(
                        title: server.name,
                        subtitle: server.howTo.isEmpty
                            ? server.shortDescription
                            : server.howTo,
                        trailing: server.category,
                      ),
                    )
                    .toList(),
              ),
            ),
            const SizedBox(height: 12),
            _SectionCard(
              title: 'Doc proposals',
              child: Column(
                children: activeDetail.docProposals
                    .map(
                      (proposal) => _InlineRow(
                        title: 'Proposal ${proposal.id}',
                        subtitle: proposal.proposedDocType,
                        trailing: proposal.status,
                      ),
                    )
                    .toList(),
              ),
            ),
            const SizedBox(height: 12),
            _SectionCard(
              title: 'Worker processes',
              child: Column(
                children: activeDetail.workerProcesses
                    .map(
                      (process) => _InlineRow(
                        title: 'PID ${process.pid}',
                        subtitle: process.updatedAt,
                        trailing: process.status,
                      ),
                    )
                    .toList(),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _RunStatusBanner extends StatelessWidget {
  const _RunStatusBanner({required this.runStatus});

  final RunStatus runStatus;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          colors: [Color(0xFF0D5C63), Color(0xFF3C7A89)],
        ),
        borderRadius: BorderRadius.circular(24),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.motion_photos_on_outlined, color: Colors.white),
              const SizedBox(width: 10),
              Text(
                'Autonomous run ${runStatus.state}',
                style: Theme.of(context).textTheme.titleLarge?.copyWith(
                  color: Colors.white,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            runStatus.summary,
            style: Theme.of(
              context,
            ).textTheme.bodyLarge?.copyWith(color: Colors.white),
          ),
          if (runStatus.focus.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              'Focus: ${runStatus.focus}',
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: Colors.white70),
            ),
          ],
        ],
      ),
    );
  }
}

class _SessionCard extends StatelessWidget {
  const _SessionCard({
    required this.session,
    required this.selected,
    required this.onTap,
  });

  final DashboardSession session;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 180),
      decoration: BoxDecoration(
        color: selected ? const Color(0xFF102A43) : const Color(0xFFFDF8EF),
        borderRadius: BorderRadius.circular(24),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(24),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Session ${session.id}',
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        color: selected ? Colors.white : null,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  _StatusPill(label: session.status, inverted: selected),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                session.taskTitle,
                style: Theme.of(context).textTheme.titleMedium?.copyWith(
                  color: selected ? Colors.white : null,
                ),
              ),
              const SizedBox(height: 10),
              Text(
                session.taskDescription,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: selected ? Colors.white70 : const Color(0xFF556B73),
                ),
              ),
              const SizedBox(height: 14),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _InfoChip(label: session.cliBackend, selected: selected),
                  _InfoChip(label: session.cliModel, selected: selected),
                  _InfoChip(
                    label: '${session.attachedDocCount} docs',
                    selected: selected,
                  ),
                  _InfoChip(
                    label: '${session.pendingProposalCount} pending',
                    selected: selected,
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

class _MetricCard extends StatelessWidget {
  const _MetricCard({
    required this.title,
    required this.value,
    required this.accent,
  });

  final String title;
  final String value;
  final Color accent;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 180,
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: accent.withValues(alpha: 0.2)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: Theme.of(context).textTheme.bodyMedium),
          const SizedBox(height: 10),
          Text(
            value,
            style: Theme.of(context).textTheme.headlineMedium?.copyWith(
              fontWeight: FontWeight.w700,
              color: accent,
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: const Color(0xFFFDF8EF),
        borderRadius: BorderRadius.circular(22),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: Theme.of(
              context,
            ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 12),
          child,
        ],
      ),
    );
  }
}

class _InlineRow extends StatelessWidget {
  const _InlineRow({
    required this.title,
    this.subtitle = '',
    this.trailing = '',
  });

  final String title;
  final String subtitle;
  final String trailing;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: Theme.of(
                    context,
                  ).textTheme.bodyLarge?.copyWith(fontWeight: FontWeight.w600),
                ),
                if (subtitle.isNotEmpty) ...[
                  const SizedBox(height: 2),
                  Text(
                    subtitle,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: const Color(0xFF556B73),
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (trailing.isNotEmpty) _StatusPill(label: trailing, compact: true),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({
    required this.label,
    this.compact = false,
    this.inverted = false,
  });

  final String label;
  final bool compact;
  final bool inverted;

  @override
  Widget build(BuildContext context) {
    final background = inverted
        ? Colors.white.withValues(alpha: 0.18)
        : const Color(0xFFE2F0F1);
    final foreground = inverted ? Colors.white : const Color(0xFF0D5C63);
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: compact ? 10 : 12,
        vertical: compact ? 6 : 8,
      ),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelLarge?.copyWith(
          color: foreground,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _InfoChip extends StatelessWidget {
  const _InfoChip({required this.label, required this.selected});

  final String label;
  final bool selected;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: selected
            ? Colors.white.withValues(alpha: 0.14)
            : const Color(0xFFE7E0D0),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelLarge?.copyWith(
          color: selected ? Colors.white : const Color(0xFF1D2A30),
          fontWeight: FontWeight.w600,
        ),
      ),
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
      child: Card(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.error_outline, size: 38),
              const SizedBox(height: 12),
              Text(
                'Bridge load failed',
                style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 12),
              Text(message, textAlign: TextAlign.center),
              const SizedBox(height: 16),
              FilledButton(onPressed: onRetry, child: const Text('Retry')),
            ],
          ),
        ),
      ),
    );
  }
}
