import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('GenUI v1 protocol', () {
    test('parses every allowlisted component and resolves action refs', () {
      final document = GenUiSurfaceDocument.fromJson(
        _documentJson(
          components: [
            {
              'kind': 'text',
              'id': 'text',
              'variant': 'heading',
              'text': 'Wave 358',
            },
            {'kind': 'markdown', 'id': 'markdown', 'source': '**Safe**'},
            {
              'kind': 'status_badge',
              'id': 'badge',
              'label': 'running',
              'tone': 'info',
            },
            {
              'kind': 'metric',
              'id': 'metric',
              'label': 'Workers',
              'value': '4',
              'detail': '3 running',
              'tone': 'success',
            },
            {
              'kind': 'key_value',
              'id': 'key-value',
              'rows': [
                {'label': 'Wave', 'value': '358'},
              ],
            },
            {
              'kind': 'data_table',
              'id': 'table',
              'columns': ['Session', 'State'],
              'rows': [
                ['550', 'running'],
              ],
            },
            {
              'kind': 'timeline',
              'id': 'timeline',
              'items': [
                {
                  'timestamp': '2026-07-30T12:00:00Z',
                  'label': 'Started',
                  'tone': 'info',
                  'detail': 'Worker checked out.',
                },
              ],
            },
            {
              'kind': 'callout',
              'id': 'callout',
              'tone': 'warning',
              'title': 'Governed',
              'body': 'Confirmation is required.',
            },
            {
              'kind': 'action_group',
              'id': 'actions',
              'action_refs': ['initialize'],
            },
            {'kind': 'divider', 'id': 'divider'},
          ],
          actions: [
            {
              'ref': 'initialize',
              'action_id': 'wave.plan.initialize',
              'action_version': 1,
              'label': 'Initialize',
              'target': {'type': 'planned_wave', 'id': '501'},
              'enabled': false,
              'disabled_reason_code': 'delegate_unavailable',
              'disabled_reason': 'The governed delegate is unavailable.',
              'confirmation': 'always',
              'input_schema': 'empty.v1',
            },
          ],
        ),
      );

      expect(
        document.components.map((component) => component.kind),
        GenUiComponentKind.values,
      );
      expect(document.actions.single.requiresConfirmation, isTrue);
    });

    test('fails closed for unknown components', () {
      expect(
        () => GenUiSurfaceDocument.fromJson(
          _documentJson(
            components: [
              {'kind': 'webview', 'id': 'unsafe', 'url': 'https://example.com'},
            ],
          ),
        ),
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'component_unsupported',
          ),
        ),
      );
    });

    test('fails closed for unregistered actions', () {
      expect(
        () => GenUiSurfaceDocument.fromJson(
          _documentJson(
            actions: [
              {
                'ref': 'shell',
                'action_id': 'runtime.shell.execute',
                'action_version': 1,
                'label': 'Run',
                'target': {'type': 'project', 'id': '2'},
                'enabled': true,
                'disabled_reason_code': '',
                'disabled_reason': '',
                'confirmation': 'always',
                'input_schema': 'empty.v1',
              },
            ],
          ),
        ),
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'action_unsupported',
          ),
        ),
      );
    });

    test(
      'fails closed when a registered action lies about its input schema',
      () {
        expect(
          () => GenUiSurfaceDocument.fromJson(
            _documentJson(
              actions: [
                {
                  'ref': 'feedback',
                  'action_id': 'genui.feedback.submit',
                  'action_version': 1,
                  'label': 'Helpful',
                  'target': {'type': 'genui_surface', 'id': 'surface-1'},
                  'enabled': true,
                  'disabled_reason_code': '',
                  'disabled_reason': '',
                  'confirmation': 'none',
                  'input_schema': 'empty.v1',
                },
              ],
            ),
          ),
          throwsA(
            isA<WorkroomProtocolException>().having(
              (error) => error.code,
              'code',
              'action_input_schema_invalid',
            ),
          ),
        );
      },
    );

    test('enforces the global document nesting bound', () {
      dynamic nested = 'leaf';
      for (var index = 0; index < 7; index++) {
        nested = <String, dynamic>{'child': nested};
      }
      expect(
        () => GenUiSurfaceDocument.fromJson({
          ..._documentJson(),
          'unregistered': nested,
        }),
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'document_nesting_exceeded',
          ),
        ),
      );
    });

    test('surface request schemas reject additional arguments', () {
      const request = RegisteredSurfaceRequest(
        surfaceType: 'wave.detail',
        arguments: {'wave_id': 358, 'shell': 'rm'},
      );
      expect(
        request.validate,
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'surface_arguments_invalid',
          ),
        ),
      );
    });

    test('surface request schemas enforce required scalar types', () {
      const missingWave = RegisteredSurfaceRequest(surfaceType: 'wave.detail');
      const invalidLevel = RegisteredSurfaceRequest(
        surfaceType: 'docs.tree',
        arguments: {'level': 'all'},
      );

      for (final request in [missingWave, invalidLevel]) {
        expect(
          request.validate,
          throwsA(
            isA<WorkroomProtocolException>().having(
              (error) => error.code,
              'code',
              'surface_arguments_invalid',
            ),
          ),
        );
      }
    });

    test('legal research is a closed registered surface with no arguments', () {
      const legal = RegisteredSurfaceRequest(surfaceType: 'research.legal');
      expect(legal.validate, returnsNormally);

      const injected = RegisteredSurfaceRequest(
        surfaceType: 'research.legal',
        arguments: {'path': '../../private'},
      );
      expect(
        injected.validate,
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'surface_arguments_invalid',
          ),
        ),
      );
    });

    test('decodes the flattened generated Serverpod snapshot contract', () {
      final snapshot = ProjectWorkroomSnapshot.fromJson({
        'project_id': 67,
        'project_name': 'Yandex Legal Entry',
        'project_cwd': '/projects/yandex-legal-entry',
        'protocol_version': 1,
        'watermark_seq': 12,
        'threads': <Map<String, dynamic>>[],
        'turns': <Map<String, dynamic>>[],
        'hands_actor_id': 'hands-67',
        'hands_display_name': 'Руки',
        'hands_authority_state': 'enabled',
        'hands_default_lane': 'codex',
        'hands_lanes': <Map<String, dynamic>>[],
        'hands_mailbox': <Map<String, dynamic>>[],
        'capabilities': <String>[
          'fixer.turn.send',
          'genui.surface.request',
          'genui.feedback.write',
        ],
      });

      expect(snapshot.project.id, 67);
      expect(snapshot.project.cwd, '/projects/yandex-legal-entry');
      expect(snapshot.hands.displayName, 'Руки');
      expect(snapshot.capabilities.canSendFixerTurn, isTrue);
      expect(snapshot.capabilities.canRequestSurface, isTrue);
      expect(snapshot.capabilities.canWriteFeedback, isTrue);
    });

    test('event batches are bounded to 100 entries', () {
      expect(
        () => ProjectUiFrame.fromTransport({
          'type': 'event_batch',
          'events': List<Map<String, dynamic>>.generate(
            101,
            (_) => <String, dynamic>{},
          ),
        }),
        throwsA(
          isA<WorkroomProtocolException>().having(
            (error) => error.code,
            'code',
            'event_batch_too_large',
          ),
        ),
      );
    });

    test('missing authorization capabilities fail closed', () {
      final capabilities = WorkroomCapabilities.fromJson(<String, dynamic>{});
      expect(capabilities.canSendFixerTurn, isFalse);
      expect(capabilities.canSubmitHandsInstruction, isFalse);
      expect(capabilities.canReviewHandsInstruction, isFalse);
    });
  });

  test('transport frame accepts generated-model shaped values', () {
    final frame = ProjectUiFrame.fromTransport(
      _SerializableFrame({
        'type': 'heartbeat',
        'server_time': '2026-07-30T12:00:00Z',
        'journal_head': 12,
      }),
    );

    expect(frame, isA<ProjectUiHeartbeatFrame>());
    expect((frame as ProjectUiHeartbeatFrame).journalHead, 12);
  });
}

Map<String, dynamic> _documentJson({
  List<Map<String, dynamic>> components = const [],
  List<Map<String, dynamic>> actions = const [],
}) {
  return {
    'protocol': 'fixer.genui',
    'protocol_version': 1,
    'instance_id': 'surface-1',
    'surface_type': 'wave.detail',
    'surface_version': 1,
    'project_id': 2,
    'revision': 1,
    'source_seq': 10,
    'title': 'Wave 358',
    'generated_at': '2026-07-30T12:00:00Z',
    'components': components,
    'actions': actions,
  };
}

class _SerializableFrame {
  const _SerializableFrame(this.value);

  final Map<String, dynamic> value;

  Map<String, dynamic> toJson() => value;
}
