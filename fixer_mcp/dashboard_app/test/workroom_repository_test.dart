import 'dart:convert';
import 'dart:io';

import 'package:fixer_dashboard_app/src/client_order_repository.dart';
import 'package:fixer_dashboard_app/src/dashboard_runtime_client.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_repository.dart';
import 'package:flutter_test/flutter_test.dart';

import 'workroom_test_fakes.dart';

void main() {
  test('uses Serverpod workroom methods for snapshots and commands', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final requests =
        <({String path, Map<String, dynamic> body, String authorization})>[];

    server.listen((request) async {
      final decoded = jsonDecode(await utf8.decodeStream(request));
      final body = Map<String, dynamic>.from(decoded as Map);
      requests.add((
        path: request.uri.path,
        body: body,
        authorization:
            request.headers.value(HttpHeaders.authorizationHeader) ?? '',
      ));
      final result = switch (request.uri.path) {
        '/dashboardRuntime/getProjectWorkroomSnapshot' => _snapshotJson(),
        '/dashboardRuntime/requestGenuiSurface' => {
          'status': 'succeeded',
          'project_seq': 41,
        },
        '/dashboardRuntime/submitHandsInstruction' => {
          'instruction_id': 'instruction-2',
          'ordinal': 2,
          'state': 'queued',
          'lane': 'codex',
          'project_seq': 42,
        },
        _ => <String, dynamic>{'status': 'succeeded'},
      };
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({'result': result}));
      await request.response.close();
    });

    final baseUrl = 'http://${server.address.host}:${server.port}';
    final sessionStore = _MemoryClientSessionStore()
      ..session = const ClientSession(
        identity: ClientIdentity(
          clientId: 'client-1',
          email: 'architect@example.com',
          displayName: 'Architect',
        ),
        sessionToken: 'workroom-token',
      );
    final repository = ServerpodProjectWorkroomRepository(
      serverpodBaseUrl: baseUrl,
      runtimeClient: DashboardRuntimeClient(serverpodBaseUrl: baseUrl),
      authProvider: ClientSessionAuthProvider(sessionStore: sessionStore),
    );

    final snapshot = await repository.loadSnapshot(1);
    expect(snapshot.project.name, 'Fixer MCP');
    expect(snapshot.watermarkSeq, 40);

    final surfaceReceipt = await repository.requestSurface(
      projectId: 1,
      threadId: 'fixer-thread',
      request: const RegisteredSurfaceRequest(surfaceType: 'wave.list'),
      idempotencyKey: 'surface-key',
    );
    expect(surfaceReceipt.status, 'succeeded');

    final handsReceipt = await repository.submitHandsInstruction(
      projectId: 1,
      instructionText: 'Audit reconnect.',
      declaredWriteScope: const ['fixer_mcp/dashboard_app'],
      requestedLane: 'codex',
      requestedModel: 'gpt-5.5',
      requestedReasoning: 'high',
      idempotencyKey: 'hands-key',
    );
    expect(handsReceipt.instructionId, 'instruction-2');

    expect(
      requests.map((request) => request.path),
      containsAllInOrder([
        '/dashboardRuntime/getProjectWorkroomSnapshot',
        '/dashboardRuntime/requestGenuiSurface',
        '/dashboardRuntime/submitHandsInstruction',
      ]),
    );
    expect(requests[1].body['surfaceType'], 'wave.list');
    expect(requests[1].body['surfaceVersion'], 1);
    expect(jsonDecode(requests[1].body['argumentsJson'] as String), isEmpty);
    expect((requests[2].body['request'] as Map)['declared_write_scope'], [
      'fixer_mcp/dashboard_app',
    ]);
    expect((requests[2].body['request'] as Map)['requested_model'], 'gpt-5.5');
    expect((requests[2].body['request'] as Map)['requested_reasoning'], 'high');
    expect(requests.map((request) => request.authorization).toSet(), {
      'Bearer workroom-token',
    });
  });

  test(
    'replays project journal batches through authenticated long polling',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(() => server.close(force: true));
      var authorization = '';
      server.listen((request) async {
        authorization =
            request.headers.value(HttpHeaders.authorizationHeader) ?? '';
        await utf8.decodeStream(request);
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'result': {
              'project_id': 1,
              'protocol_version': 1,
              'after_seq': 40,
              'head_seq': 41,
              'timed_out': false,
              'server_time': '2026-08-03T10:00:00Z',
              'events': [
                {
                  'project_id': 1,
                  'seq': 41,
                  'event_id': 'event-41',
                  'schema_version': 1,
                  'kind': 'genui.surface.presented',
                  'aggregate_type': 'genui_surface',
                  'aggregate_id': 'surface-legal',
                  'aggregate_revision': 1,
                  'payload': <String, dynamic>{},
                  'created_at': '2026-08-03T10:00:00Z',
                },
              ],
            },
          }),
        );
        await request.response.close();
      });
      final sessionStore = _MemoryClientSessionStore()
        ..session = const ClientSession(
          identity: ClientIdentity(
            clientId: 'client-1',
            email: 'architect@example.com',
            displayName: 'Architect',
          ),
          sessionToken: 'stream-token',
        );
      final baseUrl = 'http://${server.address.host}:${server.port}';
      final repository = ServerpodProjectWorkroomRepository(
        serverpodBaseUrl: baseUrl,
        runtimeClient: DashboardRuntimeClient(serverpodBaseUrl: baseUrl),
        authProvider: ClientSessionAuthProvider(sessionStore: sessionStore),
      );

      final frame = await repository
          .watchProjectUi(1, afterSeq: 40)
          .first
          .timeout(const Duration(seconds: 2));

      expect(frame, isA<ProjectUiEventBatchFrame>());
      expect((frame as ProjectUiEventBatchFrame).events.single.seq, 41);
      expect(authorization, 'Bearer stream-token');
    },
  );

  test('memory cursor store persists and clears per project', () async {
    final store = MemoryProjectUiCursorStore();
    await store.write(1, 12);
    await store.write(2, 7);

    expect(await store.read(1), 12);
    expect(await store.read(2), 7);
    await store.clear(1);
    expect(await store.read(1), isNull);
    expect(await store.read(2), 7);
  });
}

class _MemoryClientSessionStore implements ClientSessionStore {
  ClientSession? session;

  @override
  Future<ClientSession?> read() async => session;

  @override
  Future<void> write(ClientSession value) async => session = value;

  @override
  Future<void> clear() async => session = null;
}

Map<String, dynamic> _snapshotJson() {
  final fixture = workroomFixture();
  return {
    'project_id': fixture.project.id,
    'project_name': fixture.project.name,
    'project_cwd': fixture.project.cwd,
    'protocol_version': 1,
    'watermark_seq': fixture.watermarkSeq,
    'threads': [
      {
        'id': 'fixer-thread',
        'headline': 'Production Fixer',
        'provider': 'codex',
        'state': 'active',
      },
    ],
    'selected_thread_id': 'fixer-thread',
    'turns': <Map<String, dynamic>>[],
    'active_surface': {
      'document': jsonEncode(fixture.activeSurface!.document.toJson()),
    },
    'hands_actor_id': 'hands-project-1',
    'hands_display_name': 'Руки',
    'hands_authority_state': 'enabled',
    'hands_default_lane': 'codex',
    'hands_lanes': [
      {'provider': 'codex', 'model': 'gpt-5.6-luna', 'reasoning': 'high'},
    ],
    'hands_mailbox': <Map<String, dynamic>>[],
    'capabilities': <String>[
      'fixer.turn.send',
      'genui.surface.request',
      'genui.feedback.write',
      'hands.instruction.submit',
    ],
  };
}
