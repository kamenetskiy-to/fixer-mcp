import 'dart:convert';
import 'dart:io';

import 'package:path/path.dart' as path;
import 'package:sqlite3/sqlite3.dart' as sqlite;

import 'dashboard_logic.dart';
import 'dashboard_models.dart';

abstract class DashboardRepository {
  Future<DashboardSnapshot> loadSnapshot();
  Stream<DashboardSnapshot> watchSnapshot({Duration interval});
}

class SqliteFixerDashboardRepository implements DashboardRepository {
  SqliteFixerDashboardRepository({this.databasePath});

  final String? databasePath;

  @override
  Future<DashboardSnapshot> loadSnapshot() async {
    final dbPath = _resolveDatabasePath(databasePath);
    final logPath = _resolveLogPath(dbPath);
    final file = File(dbPath);
    if (!file.existsSync()) {
      throw StateError('Fixer MCP database not found at $dbPath');
    }

    final db = sqlite.sqlite3.open(dbPath, mode: sqlite.OpenMode.readOnly);
    try {
      db.execute('PRAGMA busy_timeout = 5000;');
      db.execute('PRAGMA query_only = ON;');

      final projectRows = db.select(
        'SELECT id, name, cwd FROM project ORDER BY id',
      );
      final sessionRows = db.select(
        "SELECT id, project_id, task_description, status, COALESCE(report, '') AS report FROM session ORDER BY project_id, id",
      );
      final projects = <int, ProjectRecord>{};
      for (final row in projectRows) {
        final id = row['id'] as int;
        projects[id] = ProjectRecord(
          id: id,
          name: row['name'] as String,
          cwd: row['cwd'] as String,
        );
      }

      final sessionsByProject = <int, List<SessionRecord>>{};
      for (final row in sessionRows) {
        final projectId = row['project_id'] as int;
        final projectSessions = sessionsByProject.putIfAbsent(
          projectId,
          () => <SessionRecord>[],
        );
        final record = SessionRecord(
          id: row['id'] as int,
          localId: projectSessions.length + 1,
          projectId: projectId,
          taskDescription: row['task_description'] as String,
          status: row['status'] as String,
          report: row['report'] as String,
        );
        projectSessions.add(record);
      }

      final statusesByProject = <int, AutonomousStatusRecord>{};
      final statusTableExists = db
          .select(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'autonomous_run_status' LIMIT 1",
          )
          .isNotEmpty;
      if (statusTableExists) {
        final statusRows = db.select(
          "SELECT project_id, COALESCE(session_id, 0) AS session_id, state, summary, COALESCE(focus, '') AS focus, COALESCE(blocker, '') AS blocker, COALESCE(evidence, '') AS evidence, updated_at FROM autonomous_run_status ORDER BY updated_at DESC, id DESC",
        );
        for (final row in statusRows) {
          final projectId = row['project_id'] as int;
          statusesByProject.putIfAbsent(
            projectId,
            () => AutonomousStatusRecord(
              projectId: projectId,
              sessionId: row['session_id'] as int,
              state: row['state'] as String,
              summary: row['summary'] as String,
              focus: row['focus'] as String,
              blocker: row['blocker'] as String,
              evidence: row['evidence'] as String,
              updatedAt: row['updated_at'] as String,
            ),
          );
        }
      }

      final workflowSessionIdsByProject = _loadAutonomousLogHistory(logPath);
      final workflowsByProject = <int, AutonomousWorkflowRecord>{};
      for (final project in projects.values) {
        final workflow = _loadAutonomousWorkflowRecord(
          project,
          workflowSessionIdsByProject[project.id] ?? const <int>[],
        );
        if (workflow != null) {
          workflowsByProject[project.id] = workflow;
        }
      }

      final dashboardProjects = <ProjectDashboardData>[];
      for (final project in projects.values) {
        dashboardProjects.add(
          buildProjectDashboardData(
            project,
            sessionsByProject[project.id] ?? const <SessionRecord>[],
            statusesByProject[project.id],
            workflowsByProject[project.id],
          ),
        );
      }
      dashboardProjects.sort((left, right) {
        final activity = right.activitySortKey.compareTo(left.activitySortKey);
        if (activity != 0) {
          return activity;
        }
        return left.project.name.compareTo(right.project.name);
      });

      return DashboardSnapshot(
        databasePath: dbPath,
        projects: dashboardProjects,
      );
    } finally {
      db.close();
    }
  }

  @override
  Stream<DashboardSnapshot> watchSnapshot({
    Duration interval = const Duration(seconds: 2),
  }) async* {
    yield await loadSnapshot();
    while (true) {
      await Future.delayed(interval);
      yield await loadSnapshot();
    }
  }

  static String _resolveLogPath(String resolvedDbPath) {
    return path.normalize(
      path.join(path.dirname(resolvedDbPath), 'fixer_mcp.log'),
    );
  }

  static AutonomousWorkflowRecord? _loadAutonomousWorkflowRecord(
    ProjectRecord project,
    List<int> loggedSessionLocalIds,
  ) {
    final workflowFile = File(
      path.join(project.cwd, '.codex', 'autonomous_resolution.json'),
    );
    Map<String, dynamic> payload = const <String, dynamic>{};
    if (workflowFile.existsSync()) {
      try {
        final decoded = jsonDecode(workflowFile.readAsStringSync());
        if (decoded is Map<String, dynamic>) {
          payload = decoded;
        } else if (decoded is Map) {
          payload = decoded.map(
            (key, value) => MapEntry(key.toString(), value),
          );
        }
      } catch (_) {
        payload = const <String, dynamic>{};
      }
    }

    final record = AutonomousWorkflowRecord(
      mode: (payload['mode'] as String?)?.trim() ?? '',
      workflowType: (payload['workflow_type'] as String?)?.trim() ?? '',
      workflowLabel: (payload['workflow_label'] as String?)?.trim() ?? '',
      activeSessionLocalId: _asInt(payload['active_netrunner_session_id']),
      lastCompletedSessionLocalId: _asInt(
        payload['last_completed_netrunner_session_id'],
      ),
      loggedSessionLocalIds: loggedSessionLocalIds,
      lastHandoffSummary:
          (payload['last_handoff_summary'] as String?)?.trim() ?? '',
      updatedAtEpoch: _asInt(payload['updated_at_epoch']),
    );
    return record.hasWorkflow ? record : null;
  }

  static Map<int, List<int>> _loadAutonomousLogHistory(String logPath) {
    final logFile = File(logPath);
    if (!logFile.existsSync()) {
      return const <int, List<int>>{};
    }

    final pattern = RegExp(
      r'wake_fixer_autonomous project_id=(\d+) session_id=(\d+)',
    );
    final projectSessions = <int, List<int>>{};
    for (final line in logFile.readAsLinesSync()) {
      final match = pattern.firstMatch(line);
      if (match == null) {
        continue;
      }
      final projectId = int.tryParse(match.group(1) ?? '');
      final localSessionId = int.tryParse(match.group(2) ?? '');
      if (projectId == null || localSessionId == null) {
        continue;
      }
      final sessions = projectSessions.putIfAbsent(projectId, () => <int>[]);
      if (!sessions.contains(localSessionId)) {
        sessions.add(localSessionId);
      }
    }
    return projectSessions;
  }

  static int _asInt(Object? value) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    if (value is String) {
      return int.tryParse(value.trim()) ?? 0;
    }
    return 0;
  }

  static String _resolveDatabasePath(String? overridePath) {
    final candidates = <String>[
      if (overridePath != null && overridePath.trim().isNotEmpty)
        overridePath.trim(),
      if (Platform.environment['FIXER_MCP_DB_PATH']?.trim().isNotEmpty ?? false)
        Platform.environment['FIXER_MCP_DB_PATH']!.trim(),
      if (Platform.environment['FIXER_DB_PATH']?.trim().isNotEmpty ?? false)
        Platform.environment['FIXER_DB_PATH']!.trim(),
      path.normalize(path.join(Directory.current.path, 'fixer.db')),
      path.normalize(path.join(Directory.current.path, '..', 'fixer.db')),
      path.normalize(path.join(Directory.current.path, '..', '..', 'fixer.db')),
      path.normalize(
        path.join(Directory.current.path, '..', '..', '..', 'fixer.db'),
      ),
    ];

    for (final candidate in candidates) {
      if (candidate.isEmpty) {
        continue;
      }
      if (File(candidate).existsSync()) {
        return candidate;
      }
    }

    return candidates.firstWhere(
      (candidate) => candidate.isNotEmpty,
      orElse: () => 'fixer.db',
    );
  }
}
