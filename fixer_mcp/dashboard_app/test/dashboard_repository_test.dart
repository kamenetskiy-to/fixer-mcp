import 'dart:io';

import 'package:fixer_dashboard_app/src/dashboard_repository.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as path;
import 'package:sqlite3/sqlite3.dart' as sqlite;

void main() {
  test('loads project and autonomous status data from fixer.db', () async {
    final tempDir = await Directory.systemTemp.createTemp(
      'fixer-dashboard-repo-',
    );
    final dbPath = path.join(tempDir.path, 'fixer.db');
    final db = sqlite.sqlite3.open(dbPath);
    try {
      db.execute('''
        CREATE TABLE project (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          name TEXT NOT NULL,
          cwd TEXT UNIQUE NOT NULL
        );
        CREATE TABLE session (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          project_id INTEGER,
          task_description TEXT NOT NULL,
          status TEXT NOT NULL,
          report TEXT
        );
        CREATE TABLE autonomous_run_status (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          project_id INTEGER NOT NULL UNIQUE,
          session_id INTEGER,
          state TEXT NOT NULL,
          summary TEXT NOT NULL,
          focus TEXT,
          blocker TEXT,
          evidence TEXT,
          created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
          updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        );
      ''');
      db.execute(
        "INSERT INTO project (id, name, cwd) VALUES (1, 'Fixer MCP', '/tmp/self_orchestration')",
      );
      db.execute(
        "INSERT INTO project (id, name, cwd) VALUES (2, 'Project B', '/tmp/project-b')",
      );
      db.execute("""
        INSERT INTO session (id, project_id, task_description, status, report)
        VALUES (11, 1, 'Autonomous dashboard session', 'completed', '')
      """);
      db.execute("""
        INSERT INTO session (id, project_id, task_description, status, report)
        VALUES (20, 1, 'Autonomous follow-up session', 'in_progress', '')
      """);
      db.execute("""
        INSERT INTO session (id, project_id, task_description, status, report)
        VALUES (18, 2, 'Routine maintenance', 'in_progress', '')
      """);
      db.execute("""
        INSERT INTO autonomous_run_status (project_id, session_id, state, summary, focus, blocker, evidence)
        VALUES (1, 20, 'running', 'Reading current status', 'project list', '', 'seeded row')
      """);

      final repository = SqliteFixerDashboardRepository(databasePath: dbPath);
      final snapshot = await repository.loadSnapshot();

      expect(snapshot.projects, hasLength(2));
      expect(snapshot.projects.first.project.name, 'Fixer MCP');
      expect(snapshot.projects.first.latestActivitySessionId, 20);
      expect(snapshot.projects.first.latestActivityLocalSessionId, 2);
      expect(snapshot.projects.first.sessions.first.localId, 1);
      expect(snapshot.projects.first.autonomousRun.stateLabel, 'running');
      expect(
        snapshot.projects.first.autonomousRun.summary,
        'Reading current status',
      );
      expect(snapshot.projects.last.project.name, 'Project B');
    } finally {
      db.close();
      await tempDir.delete(recursive: true);
    }
  });

  test(
    'prefers repo workflow metadata and wake log history for autonomous runs',
    () async {
      final tempDir = await Directory.systemTemp.createTemp(
        'fixer-dashboard-workflow-',
      );
      final projectDir = Directory(path.join(tempDir.path, 'french_exam'))
        ..createSync(recursive: true);
      final codexDir = Directory(path.join(projectDir.path, '.codex'))
        ..createSync(recursive: true);
      final workflowFile = File(
        path.join(codexDir.path, 'autonomous_resolution.json'),
      );
      workflowFile.writeAsStringSync('''
{
  "mode": "serial_autonomous_resolution",
  "workflow_type": "ghost_run",
  "workflow_label": "Ghost Run",
  "project_cwd": "${projectDir.path}",
  "active_netrunner_session_id": null,
  "last_completed_netrunner_session_id": 3,
  "last_handoff_summary": "Session 3 complete"
}
''');
      final logFile = File(path.join(tempDir.path, 'fixer_mcp.log'));
      logFile.writeAsStringSync('''
2026/03/20 00:01:00 wake_fixer_autonomous project_id=8 session_id=1 summary="Session 1 complete"
2026/03/20 00:02:00 wake_fixer_autonomous project_id=8 session_id=2 summary="Session 2 complete"
2026/03/20 00:03:00 wake_fixer_autonomous project_id=8 session_id=3 summary="Session 3 complete"
''');

      final dbPath = path.join(tempDir.path, 'fixer.db');
      final db = sqlite.sqlite3.open(dbPath);
      try {
        db.execute('''
          CREATE TABLE project (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            cwd TEXT UNIQUE NOT NULL
          );
          CREATE TABLE session (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            project_id INTEGER,
            task_description TEXT NOT NULL,
            status TEXT NOT NULL,
            report TEXT
          );
        ''');
        db.execute(
          "INSERT INTO project (id, name, cwd) VALUES (8, 'French Exam', '${projectDir.path.replaceAll("'", "''")}')",
        );
        db.execute("""
          INSERT INTO session (id, project_id, task_description, status, report)
          VALUES (229, 8, 'Lecture artifacts', 'completed', '')
        """);
        db.execute("""
          INSERT INTO session (id, project_id, task_description, status, report)
          VALUES (233, 8, 'Inline tasks', 'completed', '')
        """);
        db.execute("""
          INSERT INTO session (id, project_id, task_description, status, report)
          VALUES (241, 8, 'Screenshot captures without autonomous intervention', 'completed', '')
        """);

        final repository = SqliteFixerDashboardRepository(databasePath: dbPath);
        final snapshot = await repository.loadSnapshot();
        final project = snapshot.projects.single;

        expect(project.autonomousRun.hasRun, isTrue);
        expect(
          project.autonomousRun.source,
          'repo workflow metadata + fixer handoff log',
        );
        expect(project.autonomousRun.groups, hasLength(1));
        expect(project.autonomousRun.groups.single.sessionSpan, '#1-#3');
        expect(
          project.autonomousRun.groups.single.globalSessionSpan,
          '#229-#241',
        );
        expect(
          project.autonomousRun.evidence,
          contains('.codex/autonomous_resolution.json'),
        );
        expect(
          project.autonomousRun.evidence,
          contains('wake log sessions: #1, #2, #3'),
        );
      } finally {
        db.close();
        await tempDir.delete(recursive: true);
      }
    },
  );
}
