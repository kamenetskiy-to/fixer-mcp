import 'package:flutter/material.dart';

/// The small, explicit data contract needed by the home project rail.
///
/// This model intentionally does not expose the legacy status-count metrics.
/// The downstream dashboard composition can map bridge responses into this
/// self-contained module without coupling the card to the larger dashboard
/// model tree.
enum ProjectActivitySourceFilter { all, fixer, hands, autonomous, project }

class HubProjectCard {
  const HubProjectCard({
    required this.projectId,
    required this.name,
    required this.cwd,
    required this.activeWaveCount,
    required this.lastActivityAt,
    this.primaryActivitySource = '',
    this.hasFixerActivity = false,
    this.hasHandsActivity = false,
    this.hasAutonomousActivity = false,
  });

  final int projectId;
  final String name;
  final String cwd;
  final int activeWaveCount;
  final String lastActivityAt;
  final String primaryActivitySource;
  final bool hasFixerActivity;
  final bool hasHandsActivity;
  final bool hasAutonomousActivity;

  factory HubProjectCard.fromJson(Map<String, dynamic> json) {
    final project = _asMap(json['project']);
    return HubProjectCard(
      projectId: _asInt(project['id'] ?? json['project_id']),
      name: _asString(project['name'] ?? json['project_name']),
      cwd: _asString(project['cwd'] ?? json['cwd']),
      activeWaveCount: _asInt(
        json['active_wave_count'] ?? json['active_waves'],
      ),
      lastActivityAt: _asString(
        json['last_activity_at'] ??
            json['latest_activity_at'] ??
            json['activity_timestamp'],
      ),
      primaryActivitySource: _asString(json['primary_activity_source']),
      hasFixerActivity: _asBool(json['has_fixer_activity']),
      hasHandsActivity: _asBool(json['has_hands_activity']),
      hasAutonomousActivity: _asBool(json['has_autonomous_activity']),
    );
  }

  bool hasSource(ProjectActivitySourceFilter filter) {
    switch (filter) {
      case ProjectActivitySourceFilter.fixer:
        return hasFixerActivity;
      case ProjectActivitySourceFilter.hands:
        return hasHandsActivity;
      case ProjectActivitySourceFilter.autonomous:
        return hasAutonomousActivity;
      case ProjectActivitySourceFilter.project:
        return !hasFixerActivity &&
            !hasHandsActivity &&
            !hasAutonomousActivity &&
            primaryActivitySource == 'project';
      case ProjectActivitySourceFilter.all:
        return true;
    }
  }

  static List<HubProjectCard> sortByActivity(Iterable<HubProjectCard> cards) {
    final sorted = cards.toList();
    sorted.sort((left, right) {
      final leftActivity = left.lastActivityAt.trim();
      final rightActivity = right.lastActivityAt.trim();
      final leftParsed = _parseActivityTimestamp(leftActivity);
      final rightParsed = _parseActivityTimestamp(rightActivity);

      if (leftActivity.isEmpty && rightActivity.isNotEmpty) return 1;
      if (leftActivity.isNotEmpty && rightActivity.isEmpty) return -1;
      if (leftParsed != null && rightParsed != null) {
        final byActivity = rightParsed.compareTo(leftParsed);
        if (byActivity != 0) {
          return byActivity;
        }
      }
      if (leftParsed == null && rightParsed != null) return 1;
      if (leftParsed != null && rightParsed == null) return -1;
      final byActivity = rightActivity.compareTo(leftActivity);
      if (byActivity != 0) return byActivity;
      return left.projectId.compareTo(right.projectId);
    });
    return sorted;
  }
}

DateTime? _parseActivityTimestamp(String raw) {
  final normalized = raw.trim();
  if (normalized.isEmpty) {
    return null;
  }
  return DateTime.tryParse(normalized);
}

class ProjectCards extends StatelessWidget {
  const ProjectCards({
    super.key,
    required this.projects,
    required this.onProjectTap,
    this.emptyLabel = 'No projects available',
    this.sourceFilter = ProjectActivitySourceFilter.all,
  });

  final List<HubProjectCard> projects;
  final ValueChanged<int> onProjectTap;
  final String emptyLabel;
  final ProjectActivitySourceFilter sourceFilter;

  @override
  Widget build(BuildContext context) {
    final visible = projects
        .where((project) => project.hasSource(sourceFilter))
        .toList(growable: false);
    final sorted = HubProjectCard.sortByActivity(visible);
    if (sorted.isEmpty) {
      return Center(child: Text(emptyLabel));
    }
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: sorted.length,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        final project = sorted[index];
        return ProjectCardTile(
          project: project,
          onTap: () => onProjectTap(project.projectId),
        );
      },
    );
  }
}

class ProjectCardTile extends StatelessWidget {
  const ProjectCardTile({
    super.key,
    required this.project,
    required this.onTap,
  });

  final HubProjectCard project;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final hasActivity = project.lastActivityAt.trim().isNotEmpty;
    final waveLabel = project.activeWaveCount == 1
        ? '1 active wave'
        : '${project.activeWaveCount} active waves';
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                project.name,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                project.cwd,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 14),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  Chip(
                    avatar: const Icon(Icons.alt_route, size: 16),
                    label: Text(waveLabel),
                    visualDensity: VisualDensity.compact,
                  ),
                  Chip(
                    avatar: const Icon(Icons.schedule, size: 16),
                    label: Text(
                      hasActivity ? project.lastActivityAt : 'No activity yet',
                    ),
                    visualDensity: VisualDensity.compact,
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

Map<String, dynamic> _asMap(Object? value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  return <String, dynamic>{};
}

int _asInt(Object? value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse('$value') ?? 0;
}

String _asString(Object? value) => value is String ? value : '${value ?? ''}';

bool _asBool(Object? value) {
  if (value is bool) return value;
  if (value is int) return value != 0;
  if (value is num) return value != 0;
  final normalized = '$value'.trim().toLowerCase();
  return normalized == '1' || normalized == 'true' || normalized == 'yes';
}
