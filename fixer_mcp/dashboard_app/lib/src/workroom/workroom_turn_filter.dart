// Technical-message classification rules live only in this file.
//
// Classify provider transcript turns only by exact raw-event metadata passed in
// `source`. Never add content matching or text heuristics here or elsewhere.

enum WorkroomTurnKind {
  conversation,
  skillActivation,
  skillDefinition,
  command,
  commandOutput,
  compaction,
  protocol,
  mcp,
  context,
  tool,
}

class WorkroomTurnPresentation {
  const WorkroomTurnPresentation(this.kind, this.label);

  final WorkroomTurnKind kind;
  final String label;

  bool get technical => kind != WorkroomTurnKind.conversation;
}

const providerTechnicalSources =
    <String, Map<String, WorkroomTurnPresentation>>{
      'claude': {
        'provider_transcript:skill_activation': WorkroomTurnPresentation(
          WorkroomTurnKind.skillActivation,
          'Skill activation',
        ),
        'provider_transcript:tool_result': WorkroomTurnPresentation(
          WorkroomTurnKind.tool,
          'Tool result',
        ),
        'provider_transcript:assistant_progress': WorkroomTurnPresentation(
          WorkroomTurnKind.context,
          'Intermediate progress',
        ),
        'provider_transcript:compaction': WorkroomTurnPresentation(
          WorkroomTurnKind.compaction,
          'Compaction context',
        ),
        'provider_transcript:meta': WorkroomTurnPresentation(
          WorkroomTurnKind.tool,
          'Runtime metadata',
        ),
        'provider_transcript:interruption': WorkroomTurnPresentation(
          WorkroomTurnKind.context,
          'Interrupted response',
        ),
        'provider_transcript:api_error': WorkroomTurnPresentation(
          WorkroomTurnKind.context,
          'Provider error',
        ),
        'provider_transcript:task_notification': WorkroomTurnPresentation(
          WorkroomTurnKind.tool,
          'Task notification',
        ),
        'provider_transcript:command': WorkroomTurnPresentation(
          WorkroomTurnKind.command,
          'Provider command',
        ),
      },
    };

WorkroomTurnPresentation classifyWorkroomTurn({
  required String provider,
  required String role,
  String source = '',
}) {
  if (role == 'tool' || role == 'system') {
    return const WorkroomTurnPresentation(WorkroomTurnKind.tool, 'Tool/system');
  }
  final technical = providerTechnicalSources[provider]?[source];
  if (technical != null) {
    return technical;
  }
  return const WorkroomTurnPresentation(
    WorkroomTurnKind.conversation,
    'Conversation',
  );
}
