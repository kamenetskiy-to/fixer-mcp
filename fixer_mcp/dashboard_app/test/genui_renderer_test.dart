import 'package:fixer_dashboard_app/src/workroom/genui_renderer.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:visibility_detector/visibility_detector.dart';

void main() {
  setUp(() {
    VisibilityDetectorController.instance.updateInterval = Duration.zero;
  });

  testWidgets('renders allowlisted content and keeps external links inert', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final invoked = <String>[];
    final document = GenUiSurfaceDocument.fromJson({
      'protocol': 'fixer.genui',
      'protocol_version': 1,
      'instance_id': 'surface-renderer',
      'surface_type': 'unsupported.request',
      'surface_version': 1,
      'project_id': 2,
      'revision': 1,
      'source_seq': 22,
      'title': 'Unsupported request',
      'generated_at': '2026-07-30T12:00:00Z',
      'demand_example_id': 'demand-123',
      'components': [
        {
          'kind': 'text',
          'id': 'heading',
          'variant': 'heading',
          'text': 'Unsupported request',
        },
        {
          'kind': 'markdown',
          'id': 'markdown',
          'source': '[Unsafe](https://example.com) **request**',
        },
        {
          'kind': 'status_badge',
          'id': 'status',
          'label': 'running',
          'tone': 'info',
        },
        {
          'kind': 'metric',
          'id': 'metric',
          'label': 'Workers',
          'value': '4',
          'detail': '3 active',
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
          'id': 'demand',
          'tone': 'warning',
          'title': 'Demand recorded',
          'body': 'Reference demand-123',
        },
        {'kind': 'divider', 'id': 'divider'},
        {
          'kind': 'action_group',
          'id': 'actions',
          'action_refs': ['open', 'disabled'],
        },
      ],
      'actions': [
        {
          'ref': 'open',
          'action_id': 'ui.surface.open',
          'action_version': 1,
          'label': 'Open overview',
          'target': {'type': 'surface', 'id': 'project.overview.v1'},
          'enabled': true,
          'disabled_reason_code': '',
          'disabled_reason': '',
          'confirmation': 'none',
          'input_schema': 'empty.v1',
        },
        {
          'ref': 'disabled',
          'action_id': 'wave.plan.initialize',
          'action_version': 1,
          'label': 'Initialize',
          'target': {'type': 'planned_wave', 'id': '501'},
          'enabled': false,
          'disabled_reason_code': 'governed_delegate_unavailable',
          'disabled_reason': 'The governed delegate is unavailable.',
          'confirmation': 'always',
          'input_schema': 'empty.v1',
        },
      ],
    });

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: GenUiRenderer(
            document: document,
            onAction: (action) async => invoked.add(action.ref),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Unsupported request'), findsOneWidget);
    expect(find.textContaining('external link disabled'), findsOneWidget);
    expect(find.text('Workers'), findsOneWidget);
    expect(find.text('358'), findsOneWidget);
    expect(find.text('550'), findsOneWidget);
    expect(find.text('Started'), findsOneWidget);
    expect(find.byType(DataTable), findsOneWidget);
    expect(find.text('Reference demand-123'), findsOneWidget);
    expect(find.byKey(const ValueKey('genui-action-open')), findsOneWidget);
    final disabled = tester.widget<FilledButton>(
      find.byKey(const ValueKey('genui-action-disabled')),
    );
    expect(disabled.onPressed, isNull);
    expect(
      find.textContaining('governed_delegate_unavailable'),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const ValueKey('genui-action-open')));
    await tester.pump();
    expect(invoked, ['open']);
  });
}
