import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';

import 'dashboard_models.dart';
import 'dashboard_repository.dart';

class DashboardShell extends StatefulWidget {
  const DashboardShell({super.key, required this.repository});

  final DashboardRepository repository;

  @override
  State<DashboardShell> createState() => _DashboardShellState();
}

class _DashboardShellState extends State<DashboardShell> {
  late Stream<DashboardSnapshot> _snapshotStream;
  int? _selectedProjectId;

  @override
  void initState() {
    super.initState();
    _snapshotStream = widget.repository.watchSnapshot();
  }

  void _refresh() {
    setState(() {
      _snapshotStream = widget.repository.watchSnapshot();
    });
  }

  void _selectProject(int projectId) {
    setState(() {
      _selectedProjectId = projectId;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Fixer MCP Dashboard'),
        actions: [
          IconButton(
            tooltip: 'Reload now',
            onPressed: _refresh,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: StreamBuilder<DashboardSnapshot>(
        stream: _snapshotStream,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting &&
              !snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _ErrorState(
              message: snapshot.error.toString(),
              onRetry: _refresh,
            );
          }

          final data = snapshot.data!;
          if (data.projects.isEmpty) {
            return const _EmptyState();
          }

          final selectedProject = _selectedProject(data);
          final isWide = MediaQuery.of(context).size.width >= 960;

          return LayoutBuilder(
            builder: (context, constraints) {
              final wide = constraints.maxWidth >= 960 && isWide;
              final projectList = _ProjectListPane(
                snapshot: data,
                selectedProjectId: selectedProject.project.id,
                onSelect: _selectProject,
              );
              final detail = _ProjectDetailPane(project: selectedProject);

              if (wide) {
                return Padding(
                  padding: const EdgeInsets.all(20),
                  child: SizedBox(
                    height: constraints.maxHeight,
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        SizedBox(width: 360, child: projectList),
                        const SizedBox(width: 20),
                        Expanded(child: detail),
                      ],
                    ),
                  ),
                );
              }

              return ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  _OverviewHeader(snapshot: data),
                  const SizedBox(height: 16),
                  SizedBox(height: 280, child: projectList),
                  const SizedBox(height: 16),
                  detail,
                ],
              );
            },
          );
        },
      ),
    );
  }

  ProjectDashboardData _selectedProject(DashboardSnapshot snapshot) {
    final selectedId = _selectedProjectId;
    if (selectedId != null) {
      for (final project in snapshot.projects) {
        if (project.project.id == selectedId) {
          return project;
        }
      }
    }
    return snapshot.projects.first;
  }
}

class _OverviewHeader extends StatelessWidget {
  const _OverviewHeader({required this.snapshot});

  final DashboardSnapshot snapshot;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Control plane snapshot',
              style: Theme.of(
                context,
              ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            Text(
              snapshot.databasePath,
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: Colors.black54),
            ),
            const SizedBox(height: 16),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              children: [
                _SummaryMetric(
                  label: 'Projects',
                  value: snapshot.projects.length.toString(),
                ),
                _SummaryMetric(
                  label: 'Active sessions',
                  value: snapshot.activeSessionCount.toString(),
                ),
                _SummaryMetric(
                  label: 'Autonomous runs',
                  value: snapshot.autonomousProjectCount.toString(),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _ProjectListPane extends StatelessWidget {
  const _ProjectListPane({
    required this.snapshot,
    required this.selectedProjectId,
    required this.onSelect,
  });

  final DashboardSnapshot snapshot;
  final int selectedProjectId;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Projects',
              style: Theme.of(
                context,
              ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.separated(
                itemCount: snapshot.projects.length,
                separatorBuilder: (context, _) => const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final project = snapshot.projects[index];
                  return _ProjectTile(
                    project: project,
                    selected: project.project.id == selectedProjectId,
                    onTap: () => onSelect(project.project.id),
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

class _ProjectTile extends StatelessWidget {
  const _ProjectTile({
    required this.project,
    required this.selected,
    required this.onTap,
  });

  final ProjectDashboardData project;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(16),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: selected ? const Color(0xFFE8F1EF) : const Color(0xFFF8F7F4),
          borderRadius: BorderRadius.circular(16),
          border: Border.all(
            color: selected ? const Color(0xFF2B6E68) : const Color(0xFFE3DDD0),
            width: selected ? 1.4 : 1,
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    project.project.name,
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              project.project.cwd,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: Colors.black54),
            ),
            const SizedBox(height: 10),
            Text(
              project.latestActivityLabel.isEmpty
                  ? 'Latest: none'
                  : 'Latest: ${project.latestActivityLabel}',
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: Colors.black54),
            ),
            const SizedBox(height: 10),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _TinyPill(label: 'P ${project.pendingCount}'),
                _TinyPill(label: 'R ${project.reviewCount}'),
                _TinyPill(label: 'I ${project.inProgressCount}'),
                _TinyPill(label: 'C ${project.completedCount}'),
                if (project.hasActiveWork) _TinyPill(label: 'active'),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _ProjectDetailPane extends StatelessWidget {
  const _ProjectDetailPane({required this.project});

  final ProjectDashboardData project;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        project.project.name,
                        style: Theme.of(context).textTheme.headlineSmall
                            ?.copyWith(fontWeight: FontWeight.w800),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        project.project.cwd,
                        style: Theme.of(
                          context,
                        ).textTheme.bodyMedium?.copyWith(color: Colors.black54),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              children: [
                _SummaryMetric(
                  label: 'Pending',
                  value: project.pendingCount.toString(),
                ),
                _SummaryMetric(
                  label: 'In progress',
                  value: project.inProgressCount.toString(),
                ),
                _SummaryMetric(
                  label: 'Review',
                  value: project.reviewCount.toString(),
                ),
                _SummaryMetric(
                  label: 'Completed',
                  value: project.completedCount.toString(),
                ),
              ],
            ),
            const SizedBox(height: 18),
            _SectionCard(
              title: 'Timeline',
              child: _ProjectTimeline(
                project: project,
                onRunTap: (group) =>
                    _showAutonomousRunDialog(context, project, group),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ProjectTimeline extends StatelessWidget {
  const _ProjectTimeline({required this.project, required this.onRunTap});

  final ProjectDashboardData project;
  final ValueChanged<AutonomousRunGroup> onRunTap;

  @override
  Widget build(BuildContext context) {
    final run = project.autonomousRun;
    final autonomousLocalIds = <int>{
      for (final group in run.groups)
        for (final session in group.sessions) session.localId,
    };
    final timelineEntries = <_TimelineEntry>[
      for (final group in run.groups)
        _TimelineEntry.autonomousGroup(
          sortKey: group.sessions.isEmpty ? 0 : group.sessions.last.localId,
          group: group,
        ),
      for (final session in project.sessions)
        if (!autonomousLocalIds.contains(session.localId))
          _TimelineEntry.session(sortKey: session.localId, session: session),
    ]..sort((left, right) => right.sortKey.compareTo(left.sortKey));

    if (timelineEntries.isEmpty) {
      return Text(
        'No timeline entries for this project yet.',
        style: Theme.of(context).textTheme.bodyMedium,
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final entry in timelineEntries)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: entry.group != null
                ? _AutonomousRunGroupCard(
                    key: ValueKey('autonomous-run-card-${entry.group!.index}'),
                    group: entry.group!,
                    onTap: () => onRunTap(entry.group!),
                  )
                : _SessionCard(
                    session: entry.session!,
                    highlighted: false,
                    collapseReportByDefault: true,
                  ),
          ),
      ],
    );
  }
}

class _TimelineEntry {
  const _TimelineEntry._({required this.sortKey, this.group, this.session});

  factory _TimelineEntry.autonomousGroup({
    required int sortKey,
    required AutonomousRunGroup group,
  }) => _TimelineEntry._(sortKey: sortKey, group: group);

  factory _TimelineEntry.session({
    required int sortKey,
    required SessionRecord session,
  }) => _TimelineEntry._(sortKey: sortKey, session: session);

  final int sortKey;
  final AutonomousRunGroup? group;
  final SessionRecord? session;
}

class _AutonomousRunGroupCard extends StatelessWidget {
  const _AutonomousRunGroupCard({
    super.key,
    required this.group,
    required this.onTap,
  });

  final AutonomousRunGroup group;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(16),
      child: Container(
        width: double.infinity,
        decoration: BoxDecoration(
          color: group.isActive
              ? const Color(0xFFE6F2EE)
              : const Color(0xFFF3F1EA),
          borderRadius: BorderRadius.circular(16),
          border: Border.all(
            color: group.isActive
                ? const Color(0xFF2B6E68)
                : const Color(0xFFE0D7C8),
          ),
        ),
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    '${group.label}${group.sessionSpan.isEmpty ? '' : ' · ${group.sessionSpan}'}',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                _StateChip(label: group.stateLabel, state: group.stateLabel),
              ],
            ),
            const SizedBox(height: 8),
            Text(group.summary, style: Theme.of(context).textTheme.bodyMedium),
            const SizedBox(height: 8),
            Text(
              '${group.sessions.length} netrunner session${group.sessions.length == 1 ? '' : 's'}',
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: Colors.black54),
            ),
            if (group.globalSessionSpan.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                'Global sessions ${group.globalSessionSpan}',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: Colors.black45),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

void _showAutonomousRunDialog(
  BuildContext context,
  ProjectDashboardData project,
  AutonomousRunGroup group,
) {
  showDialog<void>(
    context: context,
    builder: (dialogContext) {
      return Dialog(
        insetPadding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760, maxHeight: 760),
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${project.project.name} · ${group.label}',
                            style: Theme.of(dialogContext)
                                .textTheme
                                .headlineSmall
                                ?.copyWith(fontWeight: FontWeight.w800),
                          ),
                          const SizedBox(height: 6),
                          Text(
                            group.sessionSpan.isEmpty
                                ? 'Grouped autonomous run'
                                : 'Sessions ${group.sessionSpan}',
                            style: Theme.of(dialogContext).textTheme.bodyMedium
                                ?.copyWith(color: Colors.black54),
                          ),
                          if (group.globalSessionSpan.isNotEmpty) ...[
                            const SizedBox(height: 4),
                            Text(
                              'Global sessions ${group.globalSessionSpan}',
                              style: Theme.of(dialogContext).textTheme.bodySmall
                                  ?.copyWith(color: Colors.black45),
                            ),
                          ],
                        ],
                      ),
                    ),
                    _StateChip(
                      label: group.stateLabel,
                      state: group.stateLabel,
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                _LabeledValue(label: 'Summary', value: group.summary),
                const SizedBox(height: 10),
                _LabeledValue(label: 'Evidence', value: group.evidence),
                if (group.currentStep.isNotEmpty) ...[
                  const SizedBox(height: 10),
                  _LabeledValue(
                    label: 'Current step',
                    value: group.currentStep,
                  ),
                ],
                if (group.lastCompletedStep.isNotEmpty) ...[
                  const SizedBox(height: 10),
                  _LabeledValue(
                    label: 'Last completed',
                    value: group.lastCompletedStep,
                  ),
                ],
                if (group.nextStep.isNotEmpty) ...[
                  const SizedBox(height: 10),
                  _LabeledValue(label: 'Next', value: group.nextStep),
                ],
                const SizedBox(height: 16),
                Expanded(
                  child: ListView.separated(
                    itemCount: group.sessions.length,
                    separatorBuilder: (context, _) =>
                        const SizedBox(height: 10),
                    itemBuilder: (context, index) {
                      final session = group.sessions[index];
                      return _SessionCard(
                        session: session,
                        highlighted: true,
                        collapseReportByDefault: true,
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class _SessionCard extends StatefulWidget {
  const _SessionCard({
    required this.session,
    required this.highlighted,
    this.collapseReportByDefault = false,
  });

  final SessionRecord session;
  final bool highlighted;
  final bool collapseReportByDefault;

  @override
  State<_SessionCard> createState() => _SessionCardState();
}

class _SessionCardState extends State<_SessionCard> {
  late bool _reportExpanded;

  @override
  void initState() {
    super.initState();
    _reportExpanded = !widget.collapseReportByDefault;
  }

  @override
  void didUpdateWidget(covariant _SessionCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.session.id != widget.session.id ||
        oldWidget.collapseReportByDefault != widget.collapseReportByDefault) {
      _reportExpanded = !widget.collapseReportByDefault;
    }
  }

  @override
  Widget build(BuildContext context) {
    final hasReport = widget.session.report.trim().isNotEmpty;
    final card = Container(
      decoration: BoxDecoration(
        color: widget.highlighted
            ? const Color(0xFFEAF3F1)
            : const Color(0xFFF8F7F4),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(
          color: widget.highlighted
              ? const Color(0xFF2B6E68)
              : const Color(0xFFE6DFD1),
        ),
      ),
      padding: const EdgeInsets.all(14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  '#${widget.session.localId} ${widget.session.headline}',
                  style: Theme.of(
                    context,
                  ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
                ),
              ),
              _StateChip(
                label: widget.session.status,
                state: widget.session.status,
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            'Global #${widget.session.id}',
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: Colors.black45),
          ),
          if (widget.collapseReportByDefault && hasReport) ...[
            const SizedBox(height: 8),
            Text(
              _reportExpanded ? 'Hide report' : 'Show report',
              key: widget.collapseReportByDefault
                  ? ValueKey('session-report-toggle-${widget.session.id}')
                  : null,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: const Color(0xFF2B6E68),
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
          if (hasReport && _reportExpanded) ...[
            const SizedBox(height: 8),
            MarkdownBlock(
              data: widget.session.report.trim(),
              key: widget.collapseReportByDefault
                  ? ValueKey('session-report-${widget.session.id}')
                  : null,
            ),
          ],
        ],
      ),
    );

    if (!widget.collapseReportByDefault || !hasReport) {
      return card;
    }

    return InkWell(
      borderRadius: BorderRadius.circular(14),
      onTap: () => setState(() => _reportExpanded = !_reportExpanded),
      child: card,
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
      width: double.infinity,
      decoration: BoxDecoration(
        color: const Color(0xFFF8F7F4),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: const Color(0xFFE4DDD0)),
      ),
      padding: const EdgeInsets.all(16),
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

class _LabeledValue extends StatelessWidget {
  const _LabeledValue({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: Theme.of(context).textTheme.labelLarge?.copyWith(
            color: Colors.black54,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 2),
        Text(
          value.isEmpty ? 'None' : value,
          style: Theme.of(context).textTheme.bodyMedium?.copyWith(height: 1.25),
        ),
      ],
    );
  }
}

class _SummaryMetric extends StatelessWidget {
  const _SummaryMetric({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minWidth: 110),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: const Color(0xFFF2F0E8),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xFFE2D9CA)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: Theme.of(
              context,
            ).textTheme.labelMedium?.copyWith(color: Colors.black54),
          ),
          const SizedBox(height: 4),
          Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
          ),
        ],
      ),
    );
  }
}

class _TinyPill extends StatelessWidget {
  const _TinyPill({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: const Color(0xFFE9F1EE),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(
          context,
        ).textTheme.labelSmall?.copyWith(fontWeight: FontWeight.w700),
      ),
    );
  }
}

class _StateChip extends StatelessWidget {
  const _StateChip({required this.label, required this.state});

  final String label;
  final String state;

  @override
  Widget build(BuildContext context) {
    final background = _stateColor(state).withValues(alpha: 0.15);
    final foreground = _stateColor(state);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelMedium?.copyWith(
          color: foreground,
          fontWeight: FontWeight.w800,
        ),
      ),
    );
  }
}

Color _stateColor(String state) {
  switch (state) {
    case 'running':
      return const Color(0xFF1F7A4A);
    case 'blocked':
      return const Color(0xFFC84D4D);
    case 'awaiting_review':
      return const Color(0xFFB46B17);
    case 'awaiting_next_dispatch':
      return const Color(0xFF2B6E68);
    case 'completed':
      return const Color(0xFF5C6570);
    case 'idle':
      return const Color(0xFF68737F);
    case 'pending':
      return const Color(0xFF7A6A34);
    case 'in_progress':
      return const Color(0xFF2B6E68);
    case 'review':
      return const Color(0xFFB46B17);
    default:
      return const Color(0xFF2B6E68);
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 680),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Dashboard unavailable',
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Text(message),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: onRetry,
                    icon: const Icon(Icons.refresh),
                    label: const Text('Retry'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 680),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'No projects found',
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  const SizedBox(height: 8),
                  const Text(
                    'The dashboard reads directly from fixer.db. Register or seed projects, then refresh.',
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
