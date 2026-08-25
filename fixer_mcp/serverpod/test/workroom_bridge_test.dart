import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_bridge_client.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_codec.dart';
import 'package:fixer_dashboard_server/src/workroom/workroom_stream.dart';
import 'package:test/test.dart';

void main() {
  group('WorkroomBridgeClient', () {
    test(
      'signs the exact method, path, query, principal, and body hash',
      () async {
        final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
        addTearDown(() => server.close(force: true));
        const secret = 'test-secret';
        final now = DateTime.utc(2026, 7, 30, 12);
        final handled = server.first.then((request) async {
          final body = await utf8.decodeStream(request);
          expect(body, isEmpty);
          expect(request.headers.value('X-Workroom-Service'), 'serverpod');
          expect(
            request.headers.value('X-Workroom-Roles'),
            'project_member,viewer',
          );
          expect(request.headers.value('X-Workroom-Project-ID'), '7');
          expect(request.headers.value('X-Workroom-Principal'), 'client:abc');
          final bodyHash = sha256.convert(const <int>[]).toString();
          expect(request.headers.value('X-Workroom-Body-SHA256'), bodyHash);
          final canonical = <String>[
            'GET',
            request.uri.path,
            request.uri.query,
            'serverpod',
            'client:abc',
            'project_member,viewer',
            '7',
            'request-1',
            '${now.millisecondsSinceEpoch ~/ 1000}',
            '${now.millisecondsSinceEpoch ~/ 1000 + 60}',
            bodyHash,
          ].join('\n');
          final expected = Hmac(
            sha256,
            utf8.encode(secret),
          ).convert(utf8.encode(canonical)).toString();
          expect(request.headers.value('X-Workroom-Signature'), expected);
          request.response
            ..headers.contentType = ContentType.json
            ..write('{"ok":true}');
          await request.response.close();
        });
        final client = WorkroomBridgeClient(
          baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
          secret: secret,
          now: () => now,
        );
        addTearDown(() => client.close(force: true));

        final response = await client.getJson(
          '/internal/v1/projects/7/events',
          const WorkroomBridgePrincipal(
            principalId: 'client:abc',
            roles: ['viewer', 'project_member', 'viewer'],
            projectId: 7,
            requestId: 'request-1',
          ),
          queryParameters: const {
            'after_seq': '12',
            'limit': '100',
            'wait_ms': '20000',
          },
        );

        expect(response, {'ok': true});
        await handled;
      },
    );

    test('fails closed when a bridge response exceeds one MiB', () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(() => server.close(force: true));
      final handled = server.first.then((request) async {
        await request.drain<void>();
        request.response
          ..headers.contentType = ContentType.json
          ..write(jsonEncode({'payload': 'x' * workroomPendingByteLimit}));
        await request.response.close();
      });
      final client = WorkroomBridgeClient(
        baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
        secret: 'test-secret',
      );
      addTearDown(() => client.close(force: true));

      await expectLater(
        client.getJson(
          '/internal/v1/projects/7/events',
          const WorkroomBridgePrincipal(
            principalId: 'client:abc',
            roles: ['project_member'],
            projectId: 7,
            requestId: 'request-large',
          ),
        ),
        throwsA(
          isA<WorkroomBridgeFailure>().having(
            (failure) => failure.reasonCode,
            'reasonCode',
            'response_too_large',
          ),
        ),
      );
      await handled;
    });
  });

  test('decodes opaque snapshot and event JSON into typed models', () {
    final snapshot = decodeProjectWorkroomSnapshot({
      'project_id': 7,
      'project_name': 'Workroom',
      'project_cwd': '/projects/workroom',
      'protocol_version': 1,
      'watermark_seq': 12,
      'threads': [
        {
          'id': 'thread-1',
          'provider': 'fixer',
          'headline': 'Primary',
          'state': 'active',
          'created_at': '2026-07-30T12:00:00Z',
          'updated_at': '2026-07-30T12:00:00Z',
        },
      ],
      'selected_thread_id': 'thread-1',
      'turns': <Object>[],
      'active_surface': {
        'id': 'surface-1',
        'thread_id': 'thread-1',
        'caused_by_turn_id': 'turn-1',
        'surface_type': 'project.overview',
        'surface_version': 1,
        'state': 'presented',
        'current_revision': 1,
        'source_seq': 12,
        'document': {'protocol': 'fixer.genui'},
        'updated_at': '2026-07-30T12:00:00Z',
      },
      'hands_actor_id': 'hands:project:7',
      'hands_display_name': 'Руки',
      'hands_authority_state': 'enabled',
      'hands_default_lane': 'codex',
      'hands_lanes': <Object>[],
      'hands_mailbox': <Object>[],
      'capabilities': ['project.view'],
    });
    final event = decodeProjectUiEvent({
      'project_id': 7,
      'seq': 13,
      'event_id': 'event-13',
      'schema_version': 1,
      'kind': 'fixer.turn.changed',
      'aggregate_type': 'fixer_turn',
      'aggregate_id': 'turn-1',
      'aggregate_revision': 1,
      'payload': {'status': 'complete'},
      'actor_kind': 'principal',
      'actor_id': 'client:abc',
      'causation_id': 'cause-1',
      'correlation_id': 'request-1',
      'created_at': '2026-07-30T12:00:01Z',
    });

    expect(snapshot.watermarkSeq, 12);
    expect(snapshot.projectCwd, '/projects/workroom');
    expect(snapshot.activeSurface?.documentJson, '{"protocol":"fixer.genui"}');
    expect(event.seq, 13);
    expect(event.payloadJson, '{"status":"complete"}');
  });

  group('WorkroomStreamPump', () {
    test('replays an event committed after the snapshot watermark', () async {
      final fetchedAfter = <int>[];
      var cancelled = false;
      final frame = await WorkroomStreamPump()
          .open(
            projectId: 7,
            afterSeq: 12,
            protocolVersion: 1,
            authorize: () async {},
            fetchBatch: (afterSeq) async {
              fetchedAfter.add(afterSeq);
              return _batch(afterSeq, [_event(afterSeq + 1)]);
            },
            cancelWait: () => cancelled = true,
          )
          .first;

      expect(fetchedAfter, [12]);
      expect(frame, isA<ProjectUiEventFrame>());
      expect((frame as ProjectUiEventFrame).event.seq, 13);
      expect(cancelled, isTrue);
    });

    test('reconnects from each committed sequence without loss', () async {
      final journal = [_event(1), _event(2), _event(3), _event(4)];
      for (var afterSeq = 0; afterSeq < journal.length; afterSeq++) {
        final frame = await WorkroomStreamPump()
            .open(
              projectId: 7,
              afterSeq: afterSeq,
              protocolVersion: 1,
              authorize: () async {},
              fetchBatch: (cursor) async => _batch(
                cursor,
                journal.where((event) => event.seq > cursor).toList(),
              ),
              cancelWait: () {},
            )
            .first;
        final delivered = switch (frame) {
          ProjectUiEventFrame() => [frame.event],
          ProjectUiEventBatchFrame() => frame.events,
          _ => <ProjectUiEvent>[],
        };
        expect(
          delivered.map((event) => event.seq),
          journal
              .where((event) => event.seq > afterSeq)
              .map((event) => event.seq),
          reason: 'cursor $afterSeq',
        );
      }
    });

    test('cancellation releases an active bridge waiter promptly', () async {
      final fetchStarted = Completer<void>();
      final pendingFetch = Completer<WorkroomJournalBatch>();
      final cancelled = Completer<void>();
      final stream = WorkroomStreamPump().open(
        projectId: 7,
        afterSeq: 4,
        protocolVersion: 1,
        authorize: () async {},
        fetchBatch: (_) {
          fetchStarted.complete();
          return pendingFetch.future;
        },
        cancelWait: () {
          if (!cancelled.isCompleted) cancelled.complete();
        },
      );
      final subscription = stream.listen((_) {});
      await fetchStarted.future;

      await subscription.cancel();

      await expectLater(
        cancelled.future.timeout(const Duration(milliseconds: 100)),
        completes,
      );
      pendingFetch.completeError(
        const WorkroomBridgeFailure('cancelled', 'cancelled'),
      );
    });

    test('unknown protocol returns one typed error without fetching', () async {
      var authorizationChecks = 0;
      var fetches = 0;
      final frames = await WorkroomStreamPump()
          .open(
            projectId: 7,
            afterSeq: 0,
            protocolVersion: 99,
            authorize: () async => authorizationChecks++,
            fetchBatch: (_) async {
              fetches++;
              return _batch(0, const []);
            },
            cancelWait: () {},
          )
          .toList();

      expect(authorizationChecks, 1);
      expect(fetches, 0);
      expect(frames, hasLength(1));
      expect(frames.single, isA<ProjectUiProtocolErrorFrame>());
      expect(
        (frames.single as ProjectUiProtocolErrorFrame).reasonCode,
        'unsupported_protocol_version',
      );
    });

    test('principal revocation closes the stream with an auth error', () async {
      var authorizationChecks = 0;
      final stream = WorkroomStreamPump().open(
        projectId: 7,
        afterSeq: 0,
        protocolVersion: 1,
        authorize: () async {
          authorizationChecks++;
          if (authorizationChecks >= 3) {
            throw StateError('membership revoked');
          }
        },
        fetchBatch: (cursor) async => _batch(cursor, const []),
        cancelWait: () {},
      );

      await expectLater(
        stream,
        emitsInOrder([
          isA<ProjectUiHeartbeatFrame>(),
          emitsError(isA<StateError>()),
          emitsDone,
        ]),
      );
    });

    test(
      'oversized delivery closes safely without mutating replay truth',
      () async {
        final journal = [_event(1)];
        final frames = await WorkroomStreamPump()
            .open(
              projectId: 7,
              afterSeq: 0,
              protocolVersion: 1,
              authorize: () async {},
              fetchBatch: (_) async => throw const WorkroomBridgeFailure(
                'response_too_large',
                'bounded',
              ),
              cancelWait: () {},
            )
            .toList();

        expect(frames.single, isA<ProjectUiProtocolErrorFrame>());
        expect(
          (frames.single as ProjectUiProtocolErrorFrame).reasonCode,
          'consumer_too_slow',
        );
        expect(journal.map((event) => event.seq), [1]);
      },
    );

    test('rejects non-contiguous journal batches', () {
      expect(
        validateWorkroomJournalBatch(
          _batch(2, [_event(4)]),
          projectId: 7,
          afterSeq: 2,
        ),
        'journal_sequence_corrupt',
      );
    });
  });
}

ProjectUiEvent _event(int seq) {
  return ProjectUiEvent(
    projectId: 7,
    seq: seq,
    eventId: 'event-$seq',
    schemaVersion: 1,
    kind: 'test.changed',
    aggregateType: 'test',
    aggregateId: 'aggregate',
    aggregateRevision: seq,
    payloadJson: '{}',
    actorKind: 'system',
    actorId: 'test',
    causationId: 'cause-$seq',
    correlationId: 'correlation-$seq',
    createdAt: '2026-07-30T12:00:00Z',
  );
}

WorkroomJournalBatch _batch(int afterSeq, List<ProjectUiEvent> events) {
  final headSeq = events.isEmpty ? afterSeq : events.last.seq;
  return WorkroomJournalBatch(
    projectId: 7,
    afterSeq: afterSeq,
    headSeq: headSeq,
    events: events,
    timedOut: events.isEmpty,
  );
}
