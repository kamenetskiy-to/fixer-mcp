import 'package:fixer_dashboard_app/src/hub/overseer/overseer.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _FakeOverseerRepository implements OverseerManagerRepository {
  _FakeOverseerRepository({this.threads = const [], this.loadError});

  final List<OverseerThreadRecord> threads;
  final Object? loadError;
  OverseerLaunchRequest? created;
  OverseerThreadRecord? resumed;

  @override
  Future<List<OverseerThreadRecord>> loadThreads() async {
    if (loadError != null) throw loadError!;
    return threads;
  }

  @override
  Future<OverseerLaunchPlanRecord> createOverseer(
    OverseerLaunchRequest request,
  ) async {
    created = request;
    return OverseerLaunchPlanRecord(
      mode: 'create',
      cwd: request.cwd,
      backend: request.backend,
      model: request.model,
      reasoning: request.reasoning,
      command: const ['codex'],
    );
  }

  @override
  Future<OverseerLaunchPlanRecord> resumeOverseer(
    OverseerThreadRecord thread,
  ) async {
    resumed = thread;
    return OverseerLaunchPlanRecord(
      mode: 'resume',
      cwd: thread.spawnCwd,
      backend: thread.backend,
      model: thread.model,
      reasoning: thread.reasoning,
      command: ['droid', '--resume', thread.externalSessionId],
    );
  }
}

const _thread = OverseerThreadRecord(
  backend: 'droid',
  model: 'glm-5.1',
  reasoning: 'high',
  spawnCwd: '/workspace/project-a',
  origin: 'droid_session_log',
  startedAt: null,
  lastActivityAt: null,
  externalSessionId: 'droid-overseer-7',
  preview: 'Coordinate the active project portfolio',
);

void main() {
  test('OverseerThreadRecord parses durable provider metadata', () {
    final record = OverseerThreadRecord.fromJson({
      'backend': 'claude',
      'model': 'opus',
      'reasoning': 'medium',
      'spawn_cwd': '/workspace/alpha',
      'origin': 'claude_project_log',
      'started_at': '2026-07-20T10:00:00Z',
      'last_activity_at': '2026-07-20T12:00:00Z',
      'external_session_id': 'claude-42',
      'preview': 'Portfolio thread',
    });

    expect(record.backend, 'claude');
    expect(record.model, 'opus');
    expect(record.spawnCwd, '/workspace/alpha');
    expect(record.origin, 'claude_project_log');
    expect(record.externalSessionId, 'claude-42');
    expect(record.lastActivityAt, isNotNull);
  });

  testWidgets('renders source-backed history and resumes original provider', (
    tester,
  ) async {
    final repository = _FakeOverseerRepository(threads: const [_thread]);
    await tester.pumpWidget(_app(repository));
    await tester.pumpAndSettle();

    expect(
      find.text('Coordinate the active project portfolio'),
      findsOneWidget,
    );
    expect(find.text('/workspace/project-a'), findsOneWidget);
    expect(find.text('glm-5.1 · high'), findsOneWidget);
    expect(find.textContaining('droid-overseer-7'), findsOneWidget);

    await tester.tap(find.byKey(const Key('resume-droid-droid-overseer-7')));
    await tester.pumpAndSettle();

    expect(repository.resumed?.backend, 'droid');
    expect(repository.resumed?.externalSessionId, 'droid-overseer-7');
    expect(find.text('Resume prepared on droid'), findsOneWidget);
  });

  testWidgets('Create Overseer collects cwd backend model and reasoning', (
    tester,
  ) async {
    final repository = _FakeOverseerRepository();
    await tester.pumpWidget(_app(repository));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('create-overseer')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('overseer-cwd')),
      '/workspace/new-overseer',
    );
    await tester.tap(find.byKey(const Key('confirm-create-overseer')));
    await tester.pumpAndSettle();

    expect(repository.created?.cwd, '/workspace/new-overseer');
    expect(repository.created?.backend, 'codex');
    expect(repository.created?.model, 'gpt-5.6-sol');
    expect(repository.created?.reasoning, 'xhigh');
    expect(find.text('Create prepared on codex'), findsOneWidget);
  });

  test('Antigravity Overseer menu offers Claude 5.5 and no retired 4.6', () {
    final antigravity = overseerBackendOptions.singleWhere(
      (option) => option.id == 'antigravity',
    );

    expect(
      antigravity.models,
      containsAll([
        'Gemini 3.5 Flash',
        'Gemini 3.6 Flash',
        'Gemini 3.1 Pro',
        'Claude Opus 5.5',
        'Claude Sonnet 5.5',
      ]),
    );
    expect(antigravity.reasoningOptions, ['default', 'low', 'medium', 'high']);
    for (final option in overseerBackendOptions) {
      expect(
        option.models.where(
          (model) => model.startsWith('Claude') && model.contains('4.6'),
        ),
        isEmpty,
      );
      expect(option.reasoningOptions, isNot(contains('thinking')));
    }
  });

  testWidgets('Create Overseer launches Antigravity Claude Sonnet 5.5 high', (
    tester,
  ) async {
    final repository = _FakeOverseerRepository();
    await tester.pumpWidget(_app(repository));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('create-overseer')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('overseer-cwd')),
      '/workspace/agy-overseer',
    );
    await tester.tap(find.byKey(const Key('overseer-backend')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Antigravity').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('overseer-model-antigravity')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Claude Sonnet 5.5').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('confirm-create-overseer')));
    await tester.pumpAndSettle();

    expect(repository.created?.toJson(), {
      'cwd': '/workspace/agy-overseer',
      'backend': 'antigravity',
      'model': 'Claude Sonnet 5.5',
      'reasoning': 'high',
    });
    expect(find.text('Create prepared on antigravity'), findsOneWidget);
  });

  testWidgets('shows explicit empty and error states', (tester) async {
    await tester.pumpWidget(_app(_FakeOverseerRepository()));
    await tester.pumpAndSettle();
    expect(find.text('No Overseer threads yet'), findsOneWidget);

    await tester.pumpWidget(
      _app(_FakeOverseerRepository(loadError: StateError('offline'))),
    );
    await tester.pumpAndSettle();
    expect(find.text('Could not load Overseer history'), findsOneWidget);
    expect(find.textContaining('offline'), findsOneWidget);
  });
}

Widget _app(OverseerManagerRepository repository) {
  return MaterialApp(
    home: Scaffold(
      body: SizedBox(
        height: 760,
        child: OverseerManager(repository: repository),
      ),
    ),
  );
}
