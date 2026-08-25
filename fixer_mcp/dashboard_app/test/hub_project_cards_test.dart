import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:fixer_dashboard_app/src/hub/project_cards/project_cards.dart';

void main() {
  test('parses explicit wave count and activity timestamp', () {
    final card = HubProjectCard.fromJson({
      'project': {'id': 7, 'name': 'Fixer MCP', 'cwd': '/workspace/fixer-mcp'},
      'active_wave_count': 3,
      'last_activity_at': '2026-07-23T12:30:00Z',
    });

    expect(card.projectId, 7);
    expect(card.name, 'Fixer MCP');
    expect(card.activeWaveCount, 3);
    expect(card.lastActivityAt, '2026-07-23T12:30:00Z');
  });

  test('sorts newest activity first and falls back to project id', () {
    final cards = HubProjectCard.sortByActivity([
      const HubProjectCard(
        projectId: 4,
        name: 'No activity',
        cwd: '/tmp/4',
        activeWaveCount: 0,
        lastActivityAt: '',
      ),
      const HubProjectCard(
        projectId: 9,
        name: 'Older',
        cwd: '/tmp/9',
        activeWaveCount: 1,
        lastActivityAt: '2026-07-23T08:00:00Z',
      ),
      const HubProjectCard(
        projectId: 2,
        name: 'Newer',
        cwd: '/tmp/2',
        activeWaveCount: 2,
        lastActivityAt: '2026-07-23T09:00:00Z',
      ),
    ]);

    expect(cards.map((card) => card.projectId), [2, 9, 4]);
  });

  test('sorts mixed ISO and space-formatted timestamps correctly', () {
    final cards = HubProjectCard.sortByActivity([
      const HubProjectCard(
        projectId: 11,
        name: 'Older space',
        cwd: '/tmp/11',
        activeWaveCount: 0,
        lastActivityAt: '2026-07-23 08:00:00',
      ),
      const HubProjectCard(
        projectId: 12,
        name: 'Newer iso',
        cwd: '/tmp/12',
        activeWaveCount: 0,
        lastActivityAt: '2026-07-23T09:00:00Z',
      ),
    ]);

    expect(cards.map((card) => card.projectId), [12, 11]);
  });

  testWidgets('renders active waves and timestamp instead of P/I/R', (
    tester,
  ) async {
    var tappedProjectId = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ProjectCards(
          projects: const [
            HubProjectCard(
              projectId: 7,
              name: 'Fixer MCP',
              cwd: '/workspace/fixer-mcp',
              activeWaveCount: 2,
              lastActivityAt: '2026-07-23T12:30:00Z',
            ),
          ],
          onProjectTap: (projectId) => tappedProjectId = projectId,
        ),
      ),
    );

    expect(find.text('2 active waves'), findsOneWidget);
    expect(find.text('2026-07-23T12:30:00Z'), findsOneWidget);
    expect(find.textContaining('P '), findsNothing);
    expect(find.textContaining('I '), findsNothing);
    expect(find.textContaining('R '), findsNothing);

    await tester.tap(find.text('Fixer MCP'));
    expect(tappedProjectId, 7);
  });

  testWidgets('filters by activity source', (tester) async {
    const cards = [
      HubProjectCard(
        projectId: 1,
        name: 'Fixer MCP',
        cwd: '/workspace/fixer-mcp',
        activeWaveCount: 0,
        lastActivityAt: '2026-07-23T12:00:00Z',
        hasFixerActivity: true,
        hasHandsActivity: false,
        hasAutonomousActivity: false,
      ),
      HubProjectCard(
        projectId: 2,
        name: 'Hands MCP',
        cwd: '/workspace/hands-mcp',
        activeWaveCount: 0,
        lastActivityAt: '2026-07-23T11:00:00Z',
        hasFixerActivity: false,
        hasHandsActivity: true,
        hasAutonomousActivity: false,
      ),
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: ProjectCards(
          projects: cards,
          onProjectTap: (_) {},
          sourceFilter: ProjectActivitySourceFilter.fixer,
          emptyLabel: 'No projects.',
        ),
      ),
    );

    expect(find.text('Fixer MCP'), findsOneWidget);
    expect(find.text('Hands MCP'), findsNothing);
  });
}
