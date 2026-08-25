class FixerProviderOption {
  const FixerProviderOption({
    required this.backend,
    required this.label,
    required this.defaultModel,
    required this.defaultReasoning,
    required this.models,
    required this.reasoningOptions,
  });

  final String backend;
  final String label;
  final String defaultModel;
  final String defaultReasoning;
  final List<String> models;
  final List<String> reasoningOptions;
}

const supportedFixerProviders = <FixerProviderOption>[
  FixerProviderOption(
    backend: 'codex',
    label: 'Codex CLI',
    defaultModel: 'gpt-5.6-luna',
    defaultReasoning: 'high',
    models: [
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna',
      'gpt-5.5',
      'gpt-5.4',
      'gpt-5.4-mini',
      'gpt-5.3-codex',
      'gpt-5.3-codex-spark',
      'gpt-5.2',
      'deepseek/deepseek-v4-flash-0731',
      'deepseek-v4-flash',
      'deepseek/deepseek-v4-pro-0813',
    ],
    reasoningOptions: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'],
  ),
  FixerProviderOption(
    backend: 'antigravity',
    label: 'Google Antigravity CLI',
    defaultModel: 'Gemini 3.6 Flash',
    defaultReasoning: 'high',
    models: [
      'Gemini 3.6 Flash',
      'Gemini 3.1 Pro',
      'Claude Sonnet 4.6 (Thinking)',
      'Claude Opus 4.6 (Thinking)',
    ],
    reasoningOptions: ['default', 'low', 'medium', 'high'],
  ),
  FixerProviderOption(
    backend: 'claude',
    label: 'Claude Code CLI',
    defaultModel: 'sonnet',
    defaultReasoning: 'high',
    models: ['sonnet', 'opus'],
    reasoningOptions: ['low', 'medium', 'high', 'xhigh', 'max'],
  ),
  FixerProviderOption(
    backend: 'kimi-code',
    label: 'Kimi Code',
    defaultModel: 'kimi-k3-256k',
    defaultReasoning: 'default',
    models: [
      'kimi-k2.7-code',
      'kimi-k2.7-code-highspeed',
      'kimi-k3',
      'kimi-k3-256k',
    ],
    reasoningOptions: ['default'],
  ),
  FixerProviderOption(
    backend: 'droid',
    label: 'Factory Droid CLI',
    defaultModel: 'kimi-k2.6',
    defaultReasoning: 'high',
    models: ['kimi-k2.6', 'kimi-k2.7-code', 'glm-5.1'],
    reasoningOptions: ['none', 'low', 'medium', 'high'],
  ),
  FixerProviderOption(
    backend: 'junie',
    label: 'Junie CLI',
    defaultModel: 'kimi-k2.6',
    defaultReasoning: 'default',
    models: [
      'kimi-k2.6',
      'kimi-k2.7-code',
      'glm-5.1',
      'deepseek-v4-flash-0731',
    ],
    reasoningOptions: ['default'],
  ),
];

final supportedFixerProvidersByBackend = {
  for (final provider in supportedFixerProviders) provider.backend: provider,
};

class FixerThreadRecord {
  const FixerThreadRecord({
    required this.externalId,
    required this.headline,
    required this.status,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.cwd,
    required this.lastActivityAt,
    required this.transcriptAvailable,
    this.startedAt = '',
  });

  final String externalId;
  final String headline;
  final String status;
  final String backend;
  final String model;
  final String reasoning;
  final String cwd;
  final String lastActivityAt;
  final bool transcriptAvailable;
  final String startedAt;

  factory FixerThreadRecord.fromJson(Map<String, dynamic> json) {
    return FixerThreadRecord(
      externalId: _string(json['external_id']),
      headline: _string(json['headline'], fallback: 'Fixer thread'),
      status: _string(json['status'], fallback: 'history'),
      backend: _string(json['backend'], fallback: 'codex'),
      model: _string(json['model']),
      reasoning: _string(json['reasoning']),
      cwd: _string(json['cwd']),
      lastActivityAt: _string(json['last_activity_at']),
      transcriptAvailable: json['transcript_available'] == true,
      startedAt: _string(json['started_at']),
    );
  }
}

class FixerChatLaunchRequest {
  const FixerChatLaunchRequest({
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.cwd,
  });

  final String backend;
  final String model;
  final String reasoning;
  final String cwd;

  Map<String, dynamic> toJson() => <String, dynamic>{
    'backend': backend,
    'model': model,
    'reasoning': reasoning,
    'cwd': cwd,
  };
}

String _string(Object? value, {String fallback = ''}) {
  final text = value?.toString().trim() ?? '';
  return text.isEmpty ? fallback : text;
}
