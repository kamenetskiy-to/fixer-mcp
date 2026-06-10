import 'dart:convert';
import 'dart:io';

import 'package:fixer_dashboard_app/src/dashboard_repository.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('loads bridge-backed home, project, and session payloads', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);

    server.listen((request) async {
      final payload = switch (request.uri.path) {
        '/api/home' => {
          'current_project': {
            'id': 1,
            'name': 'Fixer MCP',
            'cwd': '/tmp/self_orchestration',
          },
          'default_chat_binding': {
            'project_id': 1,
            'supported': true,
            'default_session': {
              'external_id': '019overseer',
              'codex_session_id': '019overseer',
              'headline': 'Archived Overseer thread',
              'status': 'resume_alias',
              'agent_role': 'overseer',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'last_activity_at': '2026-04-28T09:30:00Z',
              'binding_source': 'codex_session_log+fixer_resume_alias',
              'session_log': true,
              'transcript_available': false,
            },
            'sessions': [
              {
                'external_id': '019overseer',
                'codex_session_id': '019overseer',
                'headline': 'Archived Overseer thread',
                'status': 'resume_alias',
                'agent_role': 'overseer',
                'backend': 'codex',
                'model': 'gpt-5.4',
                'reasoning': 'medium',
                'last_activity_at': '2026-04-28T09:30:00Z',
                'binding_source': 'codex_session_log+fixer_resume_alias',
                'session_log': true,
                'transcript_available': false,
              },
            ],
            'transcript_availability': 'metadata_only',
            'residual_risk': 'metadata only',
          },
          'global_counts': {
            'pending': 1,
            'in_progress': 2,
            'review': 1,
            'completed': 4,
            'other': 0,
            'total': 8,
          },
          'projects': [
            {
              'project': {
                'id': 1,
                'name': 'Fixer MCP',
                'cwd': '/tmp/self_orchestration',
              },
              'counts': {
                'pending': 1,
                'in_progress': 2,
                'review': 1,
                'completed': 4,
                'other': 0,
                'total': 8,
              },
              'latest_activity_label': '#3 Flutter app shell',
              'latest_session_id': 102,
              'latest_local_session_id': 3,
              'has_pending_review': true,
              'has_active_workers': true,
            },
          ],
          'active_workers': [
            {
              'project_id': 1,
              'project_name': 'Fixer MCP',
              'session_id': 102,
              'local_session_id': 3,
              'headline': 'Flutter app shell',
              'worker_state': {
                'running_count': 1,
                'has_running': true,
                'processes': [],
              },
            },
          ],
          'autonomous_summary': {
            'projects_with_status': 1,
            'running_projects': 0,
            'blocked_projects': 1,
            'frozen_projects': 0,
            'awaiting_review_projects': 0,
          },
        },
        '/api/projects/1/overview' => {
          'project': {
            'id': 1,
            'name': 'Fixer MCP',
            'cwd': '/tmp/self_orchestration',
          },
          'metrics': {
            'counts': {
              'pending': 1,
              'in_progress': 2,
              'review': 1,
              'completed': 4,
              'other': 0,
              'total': 8,
            },
            'attached_doc_count': 3,
            'pending_proposal_count': 2,
            'worker_state': {
              'running_count': 1,
              'has_running': true,
              'processes': [],
            },
          },
          'autonomous': {
            'project_id': 1,
            'session_id': 102,
            'local_session_id': 3,
            'state': 'blocked',
            'summary': 'Waiting for review',
            'focus': 'dashboard shell',
            'blocker': '',
            'evidence': 'seed',
            'orchestration_epoch': 2,
            'orchestration_frozen': false,
            'notifications_enabled_for_active_run': true,
            'updated_at': '2026-04-28 12:00:00',
          },
          'docs': {'total_docs': 0, 'groups': []},
          'netrunners': [
            {
              'id': 102,
              'local_id': 3,
              'project_id': 1,
              'headline': 'Flutter App Shell for the Fixer MCP GUI.',
              'task_preview': 'Bridge-backed operator shell',
              'status': 'in_progress',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'write_scope': ['fixer_mcp/dashboard_app'],
              'attached_doc_count': 2,
              'mcp_count': 4,
              'proposal_count': 1,
              'pending_proposal_count': 1,
              'worker_state': {
                'running_count': 1,
                'has_running': true,
                'processes': [],
              },
              'rework_count': 0,
              'forced_stop_count': 0,
            },
          ],
          'fixer_chat': {
            'project_id': 1,
            'supported': true,
            'default_session': {
              'external_id': '019fixer',
              'codex_session_id': '019fixer',
              'headline': 'Active autonomous Fixer thread',
              'status': 'active',
              'agent_role': 'fixer',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'last_activity_at': '2026-04-28T10:45:00Z',
              'binding_source': 'codex_session_log+autonomous_state',
              'session_log': true,
              'transcript_available': false,
            },
            'sessions': [
              {
                'external_id': '019fixer',
                'codex_session_id': '019fixer',
                'headline': 'Active autonomous Fixer thread',
                'status': 'active',
                'agent_role': 'fixer',
                'backend': 'codex',
                'model': 'gpt-5.4',
                'reasoning': 'medium',
                'last_activity_at': '2026-04-28T10:45:00Z',
                'binding_source': 'codex_session_log+autonomous_state',
                'session_log': true,
                'transcript_available': false,
              },
            ],
            'transcript_availability': 'metadata_only',
            'residual_risk': 'metadata only',
          },
        },
        '/api/projects/1/docs' => {
          'project': {
            'id': 1,
            'name': 'Fixer MCP',
            'cwd': '/tmp/self_orchestration',
          },
          'docs': {
            'total_docs': 1,
            'pending_proposal_count': 1,
            'targeted_pending_proposal_count': 1,
            'untargeted_pending_proposal_count': 0,
            'groups': [
              {
                'doc_type': 'architecture',
                'pending_proposal_count': 1,
                'targeted_pending_count': 1,
                'untargeted_pending_count': 0,
                'docs': [
                  {
                    'id': 11,
                    'title': 'Codex Hub Desktop Migration Brief',
                    'doc_type': 'architecture',
                    'content_preview': 'Bridge-first GUI contract',
                    'targeted_pending_proposals': 1,
                  },
                ],
              },
            ],
          },
        },
        '/api/projects/1/fixer-chat-binding' => {
          'project_id': 1,
          'supported': true,
          'default_session': {
            'external_id': '019fixer',
            'codex_session_id': '019fixer',
            'headline': 'Active autonomous Fixer thread',
            'status': 'active',
            'agent_role': 'fixer',
            'backend': 'codex',
            'model': 'gpt-5.4',
            'reasoning': 'medium',
            'last_activity_at': '2026-04-28T10:45:00Z',
            'binding_source': 'codex_session_log+autonomous_state',
            'session_log': true,
            'transcript_available': false,
          },
          'sessions': [
            {
              'external_id': '019fixer',
              'codex_session_id': '019fixer',
              'headline': 'Active autonomous Fixer thread',
              'status': 'active',
              'agent_role': 'fixer',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'last_activity_at': '2026-04-28T10:45:00Z',
              'binding_source': 'codex_session_log+autonomous_state',
              'session_log': true,
              'transcript_available': false,
            },
          ],
          'transcript_availability': 'metadata_only',
          'residual_risk': 'metadata only',
        },
        '/api/projects/1/overseer-chat-binding' => {
          'project_id': 1,
          'supported': true,
          'default_session': {
            'external_id': '019overseer',
            'codex_session_id': '019overseer',
            'headline': 'Archived Overseer thread',
            'status': 'resume_alias',
            'agent_role': 'overseer',
            'backend': 'codex',
            'model': 'gpt-5.4',
            'reasoning': 'medium',
            'last_activity_at': '2026-04-28T09:30:00Z',
            'binding_source': 'codex_session_log+fixer_resume_alias',
            'session_log': true,
            'transcript_available': false,
          },
          'sessions': [
            {
              'external_id': '019overseer',
              'codex_session_id': '019overseer',
              'headline': 'Archived Overseer thread',
              'status': 'resume_alias',
              'agent_role': 'overseer',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'last_activity_at': '2026-04-28T09:30:00Z',
              'binding_source': 'codex_session_log+fixer_resume_alias',
              'session_log': true,
              'transcript_available': false,
            },
          ],
          'transcript_availability': 'metadata_only',
          'residual_risk': 'metadata only',
        },
        '/api/sessions/102' => {
          'session': {
            'id': 102,
            'local_id': 3,
            'project_id': 1,
            'task_description': 'Build the Flutter operator shell',
            'status': 'in_progress',
            'backend': 'codex',
            'model': 'gpt-5.4',
            'reasoning': 'medium',
            'write_scope': ['fixer_mcp/dashboard_app'],
            'report_raw': '',
            'structured_final_report': {
              'files_changed': [
                'fixer_mcp/dashboard_app/lib/src/dashboard_view.dart',
              ],
              'commands_run': ['flutter test'],
              'checks_run': ['flutter test passed'],
              'blockers': [],
            },
            'attached_docs': [
              {
                'id': 11,
                'title': 'Codex Hub Desktop Migration Brief',
                'doc_type': 'architecture',
                'summary': 'Bridge-first GUI contract',
              },
            ],
            'mcp_servers': [
              {
                'id': 7,
                'name': 'dart_flutter',
                'short_description': 'Flutter tooling',
                'category': 'Coding',
                'how_to': 'Use for Flutter code generation and diagnostics.',
              },
            ],
            'proposals': [
              {
                'id': 4,
                'local_id': 1,
                'status': 'pending',
                'proposed_doc_type': 'architecture',
                'proposed_content': 'Update shell delivery status',
                'target_project_doc_id': 11,
              },
            ],
            'worker_state': {
              'running_count': 1,
              'has_running': true,
              'processes': [],
            },
            'rework_count': 0,
            'forced_stop_count': 0,
            'available_docs': [
              {
                'id': 11,
                'title': 'Codex Hub Desktop Migration Brief',
                'doc_type': 'architecture',
                'summary': 'Bridge-first GUI contract',
              },
            ],
            'available_mcp_servers': [
              {
                'id': 7,
                'name': 'dart_flutter',
                'short_description': 'Flutter tooling',
                'category': 'Coding',
                'how_to': 'Use for Flutter code generation and diagnostics.',
              },
            ],
            'allowed_status_targets': ['in_progress', 'review', 'pending'],
            'status_action_note': '',
          },
        },
        '/api/actions/sessions/102/status' => {
          'status': 'success',
          'session': {
            'session': {
              'id': 102,
              'local_id': 3,
              'project_id': 1,
              'task_description': 'Build the Flutter operator shell',
              'status': 'review',
              'backend': 'codex',
              'model': 'gpt-5.4',
              'reasoning': 'medium',
              'write_scope': ['fixer_mcp/dashboard_app'],
              'report_raw': '',
              'structured_final_report': null,
              'attached_docs': [],
              'mcp_servers': [],
              'proposals': [],
              'worker_state': {
                'running_count': 1,
                'has_running': true,
                'processes': [],
              },
              'rework_count': 0,
              'forced_stop_count': 0,
              'available_docs': [],
              'available_mcp_servers': [],
              'allowed_status_targets': [
                'review',
                'completed',
                'pending',
                'in_progress',
              ],
              'status_action_note': '',
            },
          },
        },
        '/dashboardRuntime/threadMessages' => {
          'threadId': '019fixer',
          'transcriptAvailable': true,
          'availability': 'codex_jsonl',
          'unsupportedReason': '',
          'sessionLogPath': '/tmp/rollout.jsonl',
          'sendSupported': true,
          'sendEndpoint': '/turn/start',
          'streamEndpointTemplate': '/turn/stream/{streamId}',
          'turnStatusEndpointTemplate': '/turn/status/{streamId}',
          'messages': [
            {
              'id': 'm1',
              'role': 'user',
              'text': 'Hello from the session log',
              'createdAt': '2026-04-28T12:00:00Z',
              'source': 'codex_jsonl',
            },
          ],
        },
        '/dashboardRuntime/sendThreadMessage' => {
          'threadId': '019fixer',
          'turnId': 'turn-123',
          'streamId': 'stream-123',
          'streamEndpoint': '/turn/stream/stream-123',
          'turnStatusEndpoint': '/turn/status/stream-123',
        },
        '/dashboardRuntime/threadTurnStatus' => {
          'streamId': 'stream-123',
          'threadId': '019fixer',
          'turnId': 'turn-123',
          'done': false,
          'eventCount': 2,
          'startedAt': '2026-04-28T12:01:00Z',
          'completedAt': '',
          'assistantText': 'Streaming text',
          'progressText': 'Streaming text',
          'events': [
            {
              'sequence': 1,
              'receivedAt': '2026-04-28T12:01:01Z',
              'method': 'turn/started',
              'phase': 'started',
              'textDelta': '',
            },
            {
              'sequence': 2,
              'receivedAt': '2026-04-28T12:01:02Z',
              'method': 'turn/delta',
              'phase': 'assistant_delta',
              'textDelta': 'Streaming text',
            },
          ],
          'expired': false,
        },
        _ => {'error': 'not found'},
      };

      final statusCode = request.uri.path == '/missing' ? 404 : 200;
      request.response.statusCode = statusCode;
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode(payload));
      await request.response.close();
    });

    final repository = BridgeDashboardRepository(
      baseUrl: 'http://${server.address.host}:${server.port}',
      serverpodBaseUrl: 'http://${server.address.host}:${server.port}',
    );

    final home = await repository.loadHomeSnapshot();
    expect(home.projects.single.project.name, 'Fixer MCP');
    expect(home.activeWorkers.single.localSessionId, 3);

    final project = await repository.loadProjectWorkspace(1);
    expect(project.project.name, 'Fixer MCP');
    expect(project.docs.groups.single.docs.single.title, contains('Migration'));
    expect(project.netrunners.single.model, 'gpt-5.4');

    final fixerBinding = await repository.loadFixerChatBinding(1);
    expect(fixerBinding.defaultSession?.codexSessionId, '019fixer');

    final overseerBinding = await repository.loadOverseerChatBinding(1);
    expect(overseerBinding.defaultSession?.agentRole, 'overseer');

    final detail = await repository.loadNetrunnerDetail(102);
    expect(detail.session.taskDescription, contains('Flutter operator shell'));
    expect(
      detail.session.structuredFinalReport?.filesChanged.single,
      contains('dashboard_view.dart'),
    );
    expect(detail.session.availableDocs.single.title, contains('Migration'));

    final updated = await repository.setSessionStatus(102, 'review');
    expect(updated.session.status, 'review');

    final threadMessages = await repository.loadThreadMessages('019fixer');
    expect(threadMessages.transcriptAvailable, isTrue);
    expect(threadMessages.messages.single.text, contains('session log'));

    final sendResult = await repository.sendThreadMessage(
      '019fixer',
      'Continue',
    );
    expect(sendResult.turnId, 'turn-123');
    expect(sendResult.turnStatusEndpoint, '/turn/status/stream-123');

    final turnStatus = await repository.loadThreadTurnStatus('stream-123');
    expect(turnStatus.assistantText, 'Streaming text');
    expect(turnStatus.events.last.phase, 'assistant_delta');
  });
}
