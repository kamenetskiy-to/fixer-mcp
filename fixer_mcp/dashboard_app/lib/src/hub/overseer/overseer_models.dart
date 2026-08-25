class OverseerThreadRecord {
  const OverseerThreadRecord({
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.spawnCwd,
    required this.origin,
    required this.startedAt,
    required this.lastActivityAt,
    required this.externalSessionId,
    required this.preview,
  });

  final String backend;
  final String model;
  final String reasoning;
  final String spawnCwd;
  final String origin;
  final DateTime? startedAt;
  final DateTime? lastActivityAt;
  final String externalSessionId;
  final String preview;

  factory OverseerThreadRecord.fromJson(Map<String, dynamic> json) {
    return OverseerThreadRecord(
      backend: (json['backend'] as String?)?.trim() ?? 'codex',
      model: (json['model'] as String?)?.trim() ?? '',
      reasoning: (json['reasoning'] as String?)?.trim() ?? '',
      spawnCwd: (json['spawn_cwd'] as String?)?.trim() ?? '',
      origin: (json['origin'] as String?)?.trim() ?? '',
      startedAt: DateTime.tryParse((json['started_at'] as String?) ?? ''),
      lastActivityAt: DateTime.tryParse(
        (json['last_activity_at'] as String?) ?? '',
      ),
      externalSessionId: (json['external_session_id'] as String?)?.trim() ?? '',
      preview: (json['preview'] as String?)?.trim() ?? '',
    );
  }
}

class OverseerLaunchRequest {
  const OverseerLaunchRequest({
    required this.cwd,
    required this.backend,
    required this.model,
    required this.reasoning,
    this.externalSessionId = '',
  });

  final String cwd;
  final String backend;
  final String model;
  final String reasoning;
  final String externalSessionId;

  Map<String, dynamic> toJson() => {
    'cwd': cwd,
    'backend': backend,
    'model': model,
    'reasoning': reasoning,
    if (externalSessionId.isNotEmpty) 'external_session_id': externalSessionId,
  };
}

class OverseerLaunchPlanRecord {
  const OverseerLaunchPlanRecord({
    required this.mode,
    required this.cwd,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.command,
  });

  final String mode;
  final String cwd;
  final String backend;
  final String model;
  final String reasoning;
  final List<String> command;

  factory OverseerLaunchPlanRecord.fromJson(Map<String, dynamic> json) {
    return OverseerLaunchPlanRecord(
      mode: (json['mode'] as String?) ?? '',
      cwd: (json['cwd'] as String?) ?? '',
      backend: (json['backend'] as String?) ?? '',
      model: (json['model'] as String?) ?? '',
      reasoning: (json['reasoning'] as String?) ?? '',
      command: (json['command'] as List<dynamic>? ?? const [])
          .map((value) => value.toString())
          .toList(growable: false),
    );
  }
}

class OverseerBackendOption {
  const OverseerBackendOption({
    required this.id,
    required this.label,
    required this.models,
    required this.reasoningOptions,
  });

  final String id;
  final String label;
  final List<String> models;
  final List<String> reasoningOptions;
}

const overseerBackendOptions = <OverseerBackendOption>[
  OverseerBackendOption(
    id: 'codex',
    label: 'Codex CLI',
    models: ['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna'],
    reasoningOptions: ['low', 'medium', 'high', 'xhigh'],
  ),
  OverseerBackendOption(
    id: 'droid',
    label: 'Factory Droid',
    models: ['kimi-k2.6', 'kimi-k2.7-code', 'glm-5.1'],
    reasoningOptions: ['none', 'low', 'medium', 'high'],
  ),
  OverseerBackendOption(
    id: 'claude',
    label: 'Claude Code',
    models: ['sonnet', 'opus', 'haiku'],
    reasoningOptions: ['medium'],
  ),
  OverseerBackendOption(
    id: 'antigravity',
    label: 'Antigravity',
    models: [
      'default',
      'Gemini 3.5 Flash',
      'Gemini 3.6 Flash',
      'Gemini 3.1 Pro',
      'Claude Sonnet 4.6',
      'Claude Opus 4.6',
      'GPT-OSS 120B',
    ],
    reasoningOptions: ['default', 'low', 'medium', 'high', 'thinking'],
  ),
  OverseerBackendOption(
    id: 'junie',
    label: 'Junie CLI',
    models: ['kimi-k2.6', 'kimi-k2.7-code', 'glm-5.1'],
    reasoningOptions: ['default'],
  ),
  OverseerBackendOption(
    id: 'kimi-code',
    label: 'Kimi Code',
    models: ['kimi-k2.7-code', 'kimi-k3-256k'],
    reasoningOptions: ['default', 'low', 'medium', 'high', 'xhigh'],
  ),
];
