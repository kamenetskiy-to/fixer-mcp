import 'dart:convert';

const projectWorkroomProtocolVersion = 1;

String workroomRelativeAge(String raw) {
  final value = DateTime.tryParse(raw)?.toLocal();
  if (value == null) return 'never';
  final delta = DateTime.now().difference(value);
  if (delta.inDays > 0) return '${delta.inDays}d ago';
  if (delta.inHours > 0) return '${delta.inHours}h ago';
  if (delta.inMinutes > 0) return '${delta.inMinutes}m ago';
  return 'now';
}

const registeredWorkroomSurfaceTypes = <String>{
  'project.overview.v1',
  'wave.list.v1',
  'wave.detail.v1',
  'execution.review.v1',
  'backlog.list.v1',
  'backlog.item.v1',
  'docs.tree.v1',
  'docs.viewer.v1',
  'execution.list.v1',
  'execution.detail.v1',
  'skills.catalog.v1',
  'skills.detail.v1',
  'runtime.evidence.v1',
  'research.legal.v1',
  'unsupported.request.v1',
};

const registeredWorkroomActionIds = <String>{
  'ui.surface.open',
  'ui.surface.dismiss',
  'genui.feedback.submit',
  'hands.instruction.submit',
  'hands.instruction.cancel',
  'hands.lane.select',
  'hands.review.accept',
  'hands.review.request_changes',
  'wave.plan.initialize',
};

const registeredWorkroomInputSchemas = <String>{
  'empty.v1',
  'feedback.v1',
  'hands_instruction.v1',
  'hands_cancel.v1',
  'hands_lane.v1',
  'hands_review.v1',
};

const registeredProjectUiEventKinds = <String>{
  'fixer.thread.created',
  'fixer.thread.changed',
  'fixer.turn.appended',
  'fixer.turn.changed',
  'genui.surface.presented',
  'genui.surface.revised',
  'genui.surface.revoked',
  'genui.feedback.recorded',
  'genui.demand.recorded',
  'genui.action.changed',
  'hands.identity.changed',
  'hands.lane.changed',
  'hands.instruction.created',
  'hands.instruction.changed',
  'hands.instruction.event_appended',
  'hands.generation.changed',
  'lease.changed',
  'planned_wave.changed',
  'wave.changed',
  'session.changed',
  'backlog.changed',
  'document.changed',
  'skill.changed',
  'project.changed',
};

class WorkroomProtocolException implements Exception {
  const WorkroomProtocolException(this.code, this.message);

  final String code;
  final String message;

  @override
  String toString() => '$code: $message';
}

enum WorkroomConnectionStatus {
  cold,
  loadingSnapshot,
  replaying,
  live,
  reconnecting,
  protocolError,
  authorizationLost,
}

enum GenUiTone { neutral, info, success, warning, danger }

enum GenUiComponentKind {
  text,
  markdown,
  statusBadge,
  metric,
  keyValue,
  dataTable,
  timeline,
  callout,
  actionGroup,
  divider,
}

class WorkroomProject {
  const WorkroomProject({
    required this.id,
    required this.name,
    required this.cwd,
  });

  final int id;
  final String name;
  final String cwd;

  factory WorkroomProject.fromJson(Map<String, dynamic> json) {
    return WorkroomProject(
      id: _readInt(json, const ['id', 'project_id', 'projectId']),
      name: _readString(json, const ['name', 'project_name', 'projectName']),
      cwd: _readString(json, const ['cwd', 'project_cwd', 'projectCwd']),
    );
  }

  WorkroomProject copyWith({String? name, String? cwd}) {
    return WorkroomProject(
      id: id,
      name: name ?? this.name,
      cwd: cwd ?? this.cwd,
    );
  }
}

class WorkroomFixerThread {
  const WorkroomFixerThread({
    required this.id,
    required this.headline,
    required this.provider,
    required this.state,
    required this.createdAt,
    required this.updatedAt,
    this.model = '',
    this.reasoning = '',
  });

  final String id;
  final String headline;
  final String provider;
  final String state;
  final String createdAt;
  final String updatedAt;
  final String model;
  final String reasoning;

  factory WorkroomFixerThread.fromJson(Map<String, dynamic> json) {
    return WorkroomFixerThread(
      id: _readString(json, const [
        'id',
        'thread_id',
        'threadId',
        'external_session_id',
      ]),
      headline: _readString(json, const [
        'headline',
        'title',
        'preview',
      ], fallback: 'Fixer'),
      provider: _readString(json, const [
        'provider',
        'backend',
      ], fallback: 'unknown'),
      state: _readString(json, const ['state', 'status'], fallback: 'active'),
      createdAt: _readString(json, const ['created_at', 'createdAt']),
      updatedAt: _readString(json, const [
        'updated_at',
        'updatedAt',
        'last_activity_at',
      ]),
      model: _readString(json, const ['model']),
      reasoning: _readString(json, const [
        'reasoning',
        'reasoning_effort',
        'reasoningEffort',
      ]),
    );
  }
}

class WorkroomFixerTurn {
  const WorkroomFixerTurn({
    required this.id,
    required this.threadId,
    required this.ordinal,
    required this.role,
    required this.content,
    this.source = '',
    required this.status,
    required this.createdAt,
  });

  final String id;
  final String threadId;
  final int ordinal;
  final String role;
  final String content;
  final String? source;
  final String status;
  final String createdAt;

  factory WorkroomFixerTurn.fromJson(Map<String, dynamic> json) {
    return WorkroomFixerTurn(
      id: _readString(json, const ['id', 'turn_id', 'turnId']),
      threadId: _readString(json, const ['thread_id', 'threadId']),
      ordinal: _readInt(json, const ['ordinal', 'sequence']),
      role: _readString(json, const ['role'], fallback: 'system'),
      content: _readString(json, const ['content', 'text']),
      source: _readString(json, const ['source']),
      status: _readString(json, const ['status'], fallback: 'complete'),
      createdAt: _readString(json, const ['created_at', 'createdAt']),
    );
  }
}

class GenUiActionTarget {
  const GenUiActionTarget({required this.type, required this.id});

  final String type;
  final String id;

  factory GenUiActionTarget.fromJson(Map<String, dynamic> json) {
    _rejectUnknownKeys(json, const {'type', 'id'}, 'action target');
    final type = _requiredString(json, 'type', maxLength: 80);
    final id = _requiredString(json, 'id', maxLength: 200);
    return GenUiActionTarget(type: type, id: id);
  }

  Map<String, dynamic> toJson() => {'type': type, 'id': id};
}

class GenUiActionDescriptor {
  const GenUiActionDescriptor({
    required this.ref,
    required this.actionId,
    required this.actionVersion,
    required this.label,
    required this.target,
    required this.enabled,
    required this.disabledReasonCode,
    required this.disabledReason,
    required this.confirmation,
    required this.inputSchema,
  });

  final String ref;
  final String actionId;
  final int actionVersion;
  final String label;
  final GenUiActionTarget target;
  final bool enabled;
  final String disabledReasonCode;
  final String disabledReason;
  final String confirmation;
  final String inputSchema;

  bool get requiresConfirmation => confirmation == 'always';

  factory GenUiActionDescriptor.fromJson(Map<String, dynamic> json) {
    _rejectUnknownKeys(json, const {
      'ref',
      'action_id',
      'action_version',
      'label',
      'target',
      'enabled',
      'disabled_reason_code',
      'disabled_reason',
      'confirmation',
      'input_schema',
    }, 'action descriptor');
    final actionId = _requiredString(json, 'action_id', maxLength: 120);
    if (!registeredWorkroomActionIds.contains(actionId)) {
      throw WorkroomProtocolException(
        'action_unsupported',
        'Action "$actionId" is not registered.',
      );
    }
    final version = _requiredInt(json, 'action_version');
    if (version != 1) {
      throw WorkroomProtocolException(
        'action_version_unsupported',
        'Action "$actionId" version $version is not supported.',
      );
    }
    final confirmation = _requiredString(json, 'confirmation', maxLength: 32);
    if (confirmation != 'always' && confirmation != 'none') {
      throw WorkroomProtocolException(
        'confirmation_invalid',
        'Action confirmation must be "always" or "none".',
      );
    }
    final inputSchema = _requiredString(json, 'input_schema', maxLength: 80);
    final expectedInputSchema = switch (actionId) {
      'ui.surface.open' ||
      'ui.surface.dismiss' ||
      'wave.plan.initialize' => 'empty.v1',
      'genui.feedback.submit' => 'feedback.v1',
      'hands.instruction.submit' => 'hands_instruction.v1',
      'hands.instruction.cancel' => 'hands_cancel.v1',
      'hands.lane.select' => 'hands_lane.v1',
      'hands.review.accept' ||
      'hands.review.request_changes' => 'hands_review.v1',
      _ => '',
    };
    if (!registeredWorkroomInputSchemas.contains(inputSchema) ||
        inputSchema != expectedInputSchema) {
      throw WorkroomProtocolException(
        'action_input_schema_invalid',
        'Action "$actionId" cannot use input schema "$inputSchema".',
      );
    }
    return GenUiActionDescriptor(
      ref: _requiredString(json, 'ref', maxLength: 120),
      actionId: actionId,
      actionVersion: version,
      label: _requiredString(json, 'label', maxLength: 160),
      target: GenUiActionTarget.fromJson(_asMap(json['target'])),
      enabled: _requiredBool(json, 'enabled'),
      disabledReasonCode: _readString(json, const [
        'disabled_reason_code',
      ], maxLength: 120),
      disabledReason: _readString(json, const [
        'disabled_reason',
      ], maxLength: 1000),
      confirmation: confirmation,
      inputSchema: inputSchema,
    );
  }
}

class GenUiComponent {
  const GenUiComponent({
    required this.kind,
    required this.id,
    required this.data,
  });

  final GenUiComponentKind kind;
  final String id;
  final Map<String, dynamic> data;

  factory GenUiComponent.fromJson(Map<String, dynamic> json) {
    final rawKind = _requiredString(json, 'kind', maxLength: 40);
    final kind = switch (rawKind) {
      'text' => GenUiComponentKind.text,
      'markdown' => GenUiComponentKind.markdown,
      'status_badge' => GenUiComponentKind.statusBadge,
      'metric' => GenUiComponentKind.metric,
      'key_value' => GenUiComponentKind.keyValue,
      'data_table' => GenUiComponentKind.dataTable,
      'timeline' => GenUiComponentKind.timeline,
      'callout' => GenUiComponentKind.callout,
      'action_group' => GenUiComponentKind.actionGroup,
      'divider' => GenUiComponentKind.divider,
      _ => throw WorkroomProtocolException(
        'component_unsupported',
        'Component "$rawKind" is not registered.',
      ),
    };
    final id = _requiredString(json, 'id', maxLength: 120);
    _validateComponent(kind, json);
    return GenUiComponent(kind: kind, id: id, data: Map.unmodifiable(json));
  }

  static void _validateComponent(
    GenUiComponentKind kind,
    Map<String, dynamic> json,
  ) {
    switch (kind) {
      case GenUiComponentKind.text:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'variant',
          'text',
        }, 'text component');
        final variant = _requiredString(json, 'variant', maxLength: 20);
        if (!const {'heading', 'body', 'caption', 'mono'}.contains(variant)) {
          throw const WorkroomProtocolException(
            'text_variant_invalid',
            'Text variant is not registered.',
          );
        }
        _requiredString(json, 'text', maxLength: 8192);
      case GenUiComponentKind.markdown:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'source',
        }, 'markdown component');
        _requiredString(json, 'source', maxLength: 65536);
      case GenUiComponentKind.statusBadge:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'label',
          'tone',
        }, 'status badge component');
        _requiredString(json, 'label', maxLength: 160);
        _parseTone(json['tone']);
      case GenUiComponentKind.metric:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'label',
          'value',
          'detail',
          'tone',
        }, 'metric component');
        _requiredString(json, 'label', maxLength: 160);
        _requiredString(json, 'value', maxLength: 500);
        _optionalString(json, 'detail', maxLength: 1000);
        if (json['tone'] != null) _parseTone(json['tone']);
      case GenUiComponentKind.keyValue:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'rows',
        }, 'key/value component');
        final rows = _asList(json['rows']);
        if (rows.length > 50) {
          throw const WorkroomProtocolException(
            'component_bounds_exceeded',
            'Key/value components may contain at most 50 rows.',
          );
        }
        for (final rawRow in rows) {
          final row = _asMap(rawRow);
          _rejectUnknownKeys(row, const {'label', 'value'}, 'key/value row');
          _requiredString(row, 'label', maxLength: 500);
          _requiredString(row, 'value', maxLength: 4000);
        }
      case GenUiComponentKind.dataTable:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'columns',
          'rows',
        }, 'data table component');
        final columns = _asList(json['columns']);
        final rows = _asList(json['rows']);
        if (columns.isEmpty || columns.length > 12 || rows.length > 100) {
          throw const WorkroomProtocolException(
            'component_bounds_exceeded',
            'Data tables require 1-12 columns and at most 100 rows.',
          );
        }
        for (final column in columns) {
          if (column is! String || column.length > 500) {
            throw const WorkroomProtocolException(
              'data_table_invalid',
              'Data table columns must be bounded strings.',
            );
          }
        }
        for (final rawRow in rows) {
          final cells = _asList(rawRow);
          if (cells.length != columns.length ||
              cells.any((cell) => cell is! String || cell.length > 4000)) {
            throw const WorkroomProtocolException(
              'data_table_invalid',
              'Every data table row must match the registered columns.',
            );
          }
        }
      case GenUiComponentKind.timeline:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'items',
        }, 'timeline component');
        final items = _asList(json['items']);
        if (items.length > 100) {
          throw const WorkroomProtocolException(
            'component_bounds_exceeded',
            'Timelines may contain at most 100 items.',
          );
        }
        for (final rawItem in items) {
          final item = _asMap(rawItem);
          _rejectUnknownKeys(item, const {
            'timestamp',
            'label',
            'tone',
            'detail',
          }, 'timeline item');
          _requiredString(item, 'timestamp', maxLength: 100);
          _requiredString(item, 'label', maxLength: 500);
          _optionalString(item, 'detail', maxLength: 4000);
          _parseTone(item['tone']);
        }
      case GenUiComponentKind.callout:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'tone',
          'title',
          'body',
        }, 'callout component');
        _parseTone(json['tone']);
        _requiredString(json, 'title', maxLength: 500);
        _requiredString(json, 'body', maxLength: 8000);
      case GenUiComponentKind.actionGroup:
        _rejectUnknownKeys(json, const {
          'kind',
          'id',
          'action_refs',
        }, 'action group component');
        final refs = _asList(json['action_refs']);
        if (refs.length > 8 || refs.any((ref) => ref is! String)) {
          throw const WorkroomProtocolException(
            'action_group_invalid',
            'Action groups may reference at most 8 actions.',
          );
        }
      case GenUiComponentKind.divider:
        _rejectUnknownKeys(json, const {'kind', 'id'}, 'divider component');
    }
  }
}

class GenUiSurfaceDocument {
  const GenUiSurfaceDocument({
    required this.instanceId,
    required this.surfaceType,
    required this.surfaceVersion,
    required this.projectId,
    required this.revision,
    required this.sourceSeq,
    required this.title,
    required this.generatedAt,
    required this.components,
    required this.actions,
    required this.demandExampleId,
  });

  final String instanceId;
  final String surfaceType;
  final int surfaceVersion;
  final int projectId;
  final int revision;
  final int sourceSeq;
  final String title;
  final String generatedAt;
  final List<GenUiComponent> components;
  final List<GenUiActionDescriptor> actions;
  final String demandExampleId;

  bool get isUnsupported =>
      surfaceType == 'unsupported.request' && surfaceVersion == 1;

  factory GenUiSurfaceDocument.fromJson(Map<String, dynamic> json) {
    _validateJsonDepth(json, 1);
    final encoded = jsonEncode(json);
    if (utf8.encode(encoded).length > 256 * 1024) {
      throw const WorkroomProtocolException(
        'document_too_large',
        'The surface document exceeds the 256 KiB protocol limit.',
      );
    }
    _rejectUnknownKeys(json, const {
      'protocol',
      'protocol_version',
      'instance_id',
      'surface_type',
      'surface_version',
      'project_id',
      'revision',
      'source_seq',
      'title',
      'generated_at',
      'components',
      'actions',
      'demand_example_id',
    }, 'surface document');
    final protocol = _requiredString(json, 'protocol', maxLength: 40);
    if (protocol != 'fixer.genui') {
      throw const WorkroomProtocolException(
        'protocol_invalid',
        'The GenUI protocol name is not supported.',
      );
    }
    final protocolVersion = _requiredInt(json, 'protocol_version');
    if (protocolVersion != projectWorkroomProtocolVersion) {
      throw WorkroomProtocolException(
        'protocol_version_unsupported',
        'GenUI protocol version $protocolVersion is not supported.',
      );
    }
    final surfaceType = _requiredString(json, 'surface_type', maxLength: 120);
    final surfaceVersion = _requiredInt(json, 'surface_version');
    final surfaceKey = '$surfaceType.v$surfaceVersion';
    if (!registeredWorkroomSurfaceTypes.contains(surfaceKey)) {
      throw WorkroomProtocolException(
        'surface_unsupported',
        'Surface "$surfaceKey" is not registered.',
      );
    }
    final components = _asList(json['components'])
        .map((item) => GenUiComponent.fromJson(_asMap(item)))
        .toList(growable: false);
    final actions = _asList(json['actions'])
        .map((item) => GenUiActionDescriptor.fromJson(_asMap(item)))
        .toList(growable: false);
    if (components.length > 64 || actions.length > 32) {
      throw const WorkroomProtocolException(
        'document_bounds_exceeded',
        'A surface may contain at most 64 components and 32 actions.',
      );
    }
    final componentIds = <String>{};
    for (final component in components) {
      if (!componentIds.add(component.id)) {
        throw WorkroomProtocolException(
          'component_id_duplicate',
          'Component id "${component.id}" is duplicated.',
        );
      }
    }
    final actionsByRef = <String, GenUiActionDescriptor>{};
    for (final action in actions) {
      if (actionsByRef[action.ref] != null) {
        throw WorkroomProtocolException(
          'action_ref_duplicate',
          'Action ref "${action.ref}" is duplicated.',
        );
      }
      actionsByRef[action.ref] = action;
    }
    for (final component in components.where(
      (component) => component.kind == GenUiComponentKind.actionGroup,
    )) {
      for (final ref in _asList(component.data['action_refs']).cast<String>()) {
        if (!actionsByRef.containsKey(ref)) {
          throw WorkroomProtocolException(
            'action_ref_unresolved',
            'Action ref "$ref" does not resolve within the document.',
          );
        }
      }
    }
    return GenUiSurfaceDocument(
      instanceId: _requiredString(json, 'instance_id', maxLength: 200),
      surfaceType: surfaceType,
      surfaceVersion: surfaceVersion,
      projectId: _requiredInt(json, 'project_id'),
      revision: _requiredInt(json, 'revision'),
      sourceSeq: _requiredInt(json, 'source_seq'),
      title: _requiredString(json, 'title', maxLength: 500),
      generatedAt: _readString(json, const ['generated_at'], maxLength: 100),
      components: List.unmodifiable(components),
      actions: List.unmodifiable(actions),
      demandExampleId: _readString(json, const [
        'demand_example_id',
      ], maxLength: 200),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'protocol': 'fixer.genui',
      'protocol_version': projectWorkroomProtocolVersion,
      'instance_id': instanceId,
      'surface_type': surfaceType,
      'surface_version': surfaceVersion,
      'project_id': projectId,
      'revision': revision,
      'source_seq': sourceSeq,
      'title': title,
      'generated_at': generatedAt,
      'components': components.map((component) => component.data).toList(),
      'actions': actions
          .map(
            (action) => {
              'ref': action.ref,
              'action_id': action.actionId,
              'action_version': action.actionVersion,
              'label': action.label,
              'target': action.target.toJson(),
              'enabled': action.enabled,
              'disabled_reason_code': action.disabledReasonCode,
              'disabled_reason': action.disabledReason,
              'confirmation': action.confirmation,
              'input_schema': action.inputSchema,
            },
          )
          .toList(),
      if (demandExampleId.isNotEmpty) 'demand_example_id': demandExampleId,
    };
  }
}

class WorkroomSurfaceState {
  const WorkroomSurfaceState({
    required this.document,
    required this.feedbackVote,
  });

  final GenUiSurfaceDocument document;
  final int feedbackVote;

  factory WorkroomSurfaceState.fromJson(Map<String, dynamic> json) {
    final rawDocument =
        json['document'] ??
        json['document_json'] ??
        json['surface_document'] ??
        json;
    final document = GenUiSurfaceDocument.fromJson(_decodeMap(rawDocument));
    return WorkroomSurfaceState(
      document: document,
      feedbackVote: _readInt(json, const [
        'feedback_vote',
        'feedbackVote',
        'vote',
      ]),
    );
  }

  WorkroomSurfaceState copyWith({
    GenUiSurfaceDocument? document,
    int? feedbackVote,
  }) {
    return WorkroomSurfaceState(
      document: document ?? this.document,
      feedbackVote: feedbackVote ?? this.feedbackVote,
    );
  }
}

class HandsProviderLane {
  const HandsProviderLane({
    required this.provider,
    required this.model,
    required this.reasoning,
  });

  final String provider;
  final String model;
  final String reasoning;

  factory HandsProviderLane.fromJson(Map<String, dynamic> json) {
    return HandsProviderLane(
      provider: _readString(json, const [
        'provider',
        'lane',
      ], fallback: 'unknown'),
      model: _readString(json, const ['model']),
      reasoning: _readString(json, const ['reasoning']),
    );
  }
}

class HandsInstructionEvent {
  const HandsInstructionEvent({
    required this.ordinal,
    required this.eventType,
    required this.fromState,
    required this.toState,
    required this.detail,
    required this.createdAt,
  });

  final int ordinal;
  final String eventType;
  final String fromState;
  final String toState;
  final String detail;
  final String createdAt;

  factory HandsInstructionEvent.fromJson(Map<String, dynamic> json) {
    return HandsInstructionEvent(
      ordinal: _readInt(json, const ['ordinal', 'sequence']),
      eventType: _readString(json, const ['event_type', 'eventType', 'type']),
      fromState: _readString(json, const ['from_state', 'fromState']),
      toState: _readString(json, const ['to_state', 'toState']),
      detail: _readString(json, const [
        'detail',
        'message',
        'summary',
        'reason_text',
      ]),
      createdAt: _readString(json, const ['created_at', 'createdAt']),
    );
  }
}

class HandsInstruction {
  const HandsInstruction({
    required this.id,
    required this.ordinal,
    required this.instructionText,
    required this.state,
    required this.stateReasonCode,
    required this.stateReasonText,
    required this.requestedLane,
    required this.declaredWriteScope,
    required this.issuer,
    required this.createdAt,
    required this.updatedAt,
    required this.events,
    required this.report,
    required this.repositoryDiff,
    required this.reviewReference,
    required this.generation,
  });

  final String id;
  final int ordinal;
  final String instructionText;
  final String state;
  final String stateReasonCode;
  final String stateReasonText;
  final String requestedLane;
  final List<String> declaredWriteScope;
  final String issuer;
  final String createdAt;
  final String updatedAt;
  final List<HandsInstructionEvent> events;
  final String report;
  final String repositoryDiff;
  final String reviewReference;
  final int generation;

  bool get isTerminal => const {
    'completed',
    'cancelled',
    'failed',
    'abandoned',
    'unsupported',
  }.contains(state);

  bool get canCancel => const {
    'queued',
    'waiting_for_lease',
    'starting',
    'running',
  }.contains(state);

  bool get awaitsReview => state == 'awaiting_review';

  factory HandsInstruction.fromJson(Map<String, dynamic> json) {
    return HandsInstruction(
      id: _readString(json, const ['id', 'instruction_id', 'instructionId']),
      ordinal: _readInt(json, const ['ordinal']),
      instructionText: _readString(json, const [
        'instruction_text',
        'instructionText',
        'text',
      ]),
      state: _readString(json, const ['state', 'status'], fallback: 'queued'),
      stateReasonCode: _readString(json, const [
        'state_reason_code',
        'reason_code',
      ]),
      stateReasonText: _readString(json, const [
        'state_reason_text',
        'reason_text',
        'reason',
      ]),
      requestedLane: _readString(json, const [
        'requested_lane',
        'lane',
        'provider',
      ], fallback: 'codex'),
      declaredWriteScope: _readStringList(json, const [
        'declared_write_scope',
        'declaredWriteScope',
        'write_scope',
      ]),
      issuer: _readString(json, const [
        'issuer',
        'issuer_principal_id',
        'issuerPrincipalId',
      ]),
      createdAt: _readString(json, const ['created_at', 'createdAt']),
      updatedAt: _readString(json, const [
        'updated_at',
        'updatedAt',
        'last_event_at',
      ]),
      events: _readMapList(json, const [
        'events',
        'timeline',
      ], HandsInstructionEvent.fromJson),
      report: _readString(json, const [
        'report',
        'final_report',
        'result_report',
      ]),
      repositoryDiff: _readString(json, const [
        'repository_diff',
        'diff',
        'diff_summary',
      ]),
      reviewReference: _readString(json, const [
        'review_reference',
        'review_url',
        'review_id',
      ]),
      generation: _readInt(json, const ['generation', 'generation_number']),
    );
  }

  HandsInstruction copyWith({
    String? state,
    String? stateReasonCode,
    String? stateReasonText,
    String? requestedLane,
    List<String>? declaredWriteScope,
    String? updatedAt,
    List<HandsInstructionEvent>? events,
    String? report,
    String? repositoryDiff,
    String? reviewReference,
    int? generation,
  }) {
    return HandsInstruction(
      id: id,
      ordinal: ordinal,
      instructionText: instructionText,
      state: state ?? this.state,
      stateReasonCode: stateReasonCode ?? this.stateReasonCode,
      stateReasonText: stateReasonText ?? this.stateReasonText,
      requestedLane: requestedLane ?? this.requestedLane,
      declaredWriteScope: declaredWriteScope ?? this.declaredWriteScope,
      issuer: issuer,
      createdAt: createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      events: events ?? this.events,
      report: report ?? this.report,
      repositoryDiff: repositoryDiff ?? this.repositoryDiff,
      reviewReference: reviewReference ?? this.reviewReference,
      generation: generation ?? this.generation,
    );
  }
}

class WorkroomHandsState {
  const WorkroomHandsState({
    required this.actorId,
    required this.displayName,
    required this.authorityState,
    required this.operationalState,
    required this.selectedLane,
    required this.queueDepth,
    required this.activeLeaseSummary,
    required this.lanes,
    required this.instructions,
    required this.selectedInstructionId,
  });

  final String actorId;
  final String displayName;
  final String authorityState;
  final String operationalState;
  final String selectedLane;
  final int queueDepth;
  final String activeLeaseSummary;
  final List<HandsProviderLane> lanes;
  final List<HandsInstruction> instructions;
  final String selectedInstructionId;

  HandsInstruction? get selectedInstruction {
    for (final instruction in instructions) {
      if (instruction.id == selectedInstructionId) return instruction;
    }
    return instructions.isEmpty ? null : instructions.first;
  }

  factory WorkroomHandsState.fromJson(Map<String, dynamic> json) {
    final identity = _asMap(json['identity'] ?? json['actor'] ?? json);
    final instructions = _readMapList(
      json,
      const ['instructions', 'mailbox'],
      HandsInstruction.fromJson,
    )..sort((left, right) => right.ordinal.compareTo(left.ordinal));
    return WorkroomHandsState(
      actorId: _readString(identity, const ['actor_id', 'actorId', 'id']),
      displayName: _readString(identity, const [
        'display_name',
        'displayName',
        'name',
      ], fallback: 'Руки'),
      authorityState: _readString(identity, const [
        'authority_state',
        'authorityState',
      ], fallback: 'enabled'),
      operationalState: _readString(json, const [
        'operational_state',
        'operationalState',
        'state',
      ], fallback: 'idle'),
      selectedLane: _readString(json, const [
        'selected_lane',
        'selectedLane',
        'default_lane',
      ], fallback: 'codex'),
      queueDepth: _readInt(json, const ['queue_depth', 'queueDepth']),
      activeLeaseSummary: _readString(json, const [
        'active_lease_summary',
        'lease_summary',
        'activeLeaseSummary',
      ]),
      lanes: _readMapList(json, const [
        'lanes',
        'provider_lanes',
      ], HandsProviderLane.fromJson),
      instructions: List.unmodifiable(instructions),
      selectedInstructionId: _readString(json, const [
        'selected_instruction_id',
        'selectedInstructionId',
      ], fallback: instructions.isEmpty ? '' : instructions.first.id),
    );
  }

  WorkroomHandsState copyWith({
    String? authorityState,
    String? operationalState,
    String? selectedLane,
    int? queueDepth,
    String? activeLeaseSummary,
    List<HandsProviderLane>? lanes,
    List<HandsInstruction>? instructions,
    String? selectedInstructionId,
  }) {
    return WorkroomHandsState(
      actorId: actorId,
      displayName: displayName,
      authorityState: authorityState ?? this.authorityState,
      operationalState: operationalState ?? this.operationalState,
      selectedLane: selectedLane ?? this.selectedLane,
      queueDepth: queueDepth ?? this.queueDepth,
      activeLeaseSummary: activeLeaseSummary ?? this.activeLeaseSummary,
      lanes: lanes ?? this.lanes,
      instructions: instructions ?? this.instructions,
      selectedInstructionId:
          selectedInstructionId ?? this.selectedInstructionId,
    );
  }
}

class WorkroomCapabilities {
  const WorkroomCapabilities({
    required this.canSendFixerTurn,
    required this.canRequestSurface,
    required this.canWriteFeedback,
    required this.canSubmitHandsInstruction,
    required this.canSelectHandsLane,
    required this.canCancelHandsInstruction,
    required this.canReviewHandsInstruction,
  });

  final bool canSendFixerTurn;
  final bool canRequestSurface;
  final bool canWriteFeedback;
  final bool canSubmitHandsInstruction;
  final bool canSelectHandsLane;
  final bool canCancelHandsInstruction;
  final bool canReviewHandsInstruction;

  factory WorkroomCapabilities.fromJson(Map<String, dynamic> json) {
    bool capability(String key) {
      final value = json[key];
      if (value is bool) return value;
      if (value is Map) {
        return value['enabled'] == true || value['allowed'] == true;
      }
      return false;
    }

    return WorkroomCapabilities(
      canSendFixerTurn: capability('fixer.turn.send'),
      canRequestSurface: capability('genui.surface.request'),
      canWriteFeedback: capability('genui.feedback.write'),
      canSubmitHandsInstruction: capability('hands.instruction.submit'),
      canSelectHandsLane: capability('hands.lane.admin'),
      canCancelHandsInstruction: capability('hands.instruction.cancel'),
      canReviewHandsInstruction: capability('hands.review'),
    );
  }

  static const none = WorkroomCapabilities(
    canSendFixerTurn: false,
    canRequestSurface: false,
    canWriteFeedback: false,
    canSubmitHandsInstruction: false,
    canSelectHandsLane: false,
    canCancelHandsInstruction: false,
    canReviewHandsInstruction: false,
  );

  static const permissiveLocal = WorkroomCapabilities(
    canSendFixerTurn: true,
    canRequestSurface: true,
    canWriteFeedback: true,
    canSubmitHandsInstruction: true,
    canSelectHandsLane: true,
    canCancelHandsInstruction: true,
    canReviewHandsInstruction: true,
  );
}

class ProjectWorkroomSnapshot {
  const ProjectWorkroomSnapshot({
    required this.project,
    required this.protocolVersion,
    required this.watermarkSeq,
    required this.threads,
    required this.selectedThreadId,
    required this.turns,
    required this.activeSurface,
    required this.surfaceHistory,
    required this.hands,
    required this.capabilities,
  });

  final WorkroomProject project;
  final int protocolVersion;
  final int watermarkSeq;
  final List<WorkroomFixerThread> threads;
  final String selectedThreadId;
  final List<WorkroomFixerTurn> turns;
  final WorkroomSurfaceState? activeSurface;
  final List<WorkroomSurfaceState> surfaceHistory;
  final WorkroomHandsState hands;
  final WorkroomCapabilities capabilities;

  factory ProjectWorkroomSnapshot.fromJson(Map<String, dynamic> json) {
    final protocolVersion = _readInt(json, const [
      'protocol_version',
      'protocolVersion',
    ], fallback: projectWorkroomProtocolVersion);
    if (protocolVersion != projectWorkroomProtocolVersion) {
      throw WorkroomProtocolException(
        'protocol_version_unsupported',
        'Project workroom protocol version $protocolVersion is not supported.',
      );
    }
    final fixer = _asMap(
      json['fixer'] ?? json['fixer_genui'] ?? json['fixerGenui'],
    );
    final rawSurface =
        json['active_surface'] ??
        json['activeSurface'] ??
        fixer['active_surface'] ??
        fixer['activeSurface'];
    final nestedProject = _asMap(json['project']);
    final project = nestedProject.isNotEmpty
        ? nestedProject
        : <String, dynamic>{
            'id': json['project_id'] ?? json['projectId'],
            'name': json['project_name'] ?? json['projectName'],
            'cwd': json['project_cwd'] ?? json['projectCwd'] ?? '',
          };
    final nestedHands = _asMap(json['hands'] ?? json['project_hands']);
    final handsMailbox =
        json['hands_mailbox'] ?? json['handsMailbox'] ?? const <dynamic>[];
    final activeInstruction = _asMap(
      json['active_instruction'] ?? json['activeInstruction'],
    );
    final hands = nestedHands.isNotEmpty
        ? nestedHands
        : <String, dynamic>{
            'identity': {
              'actor_id': json['hands_actor_id'] ?? json['handsActorId'],
              'display_name':
                  json['hands_display_name'] ?? json['handsDisplayName'],
              'authority_state':
                  json['hands_authority_state'] ?? json['handsAuthorityState'],
            },
            'operational_state': activeInstruction['state'] ?? 'idle',
            'selected_lane':
                json['hands_default_lane'] ?? json['handsDefaultLane'],
            'queue_depth': _asList(handsMailbox).where((instruction) {
              final state = _readString(_decodeMap(instruction), const [
                'state',
              ]);
              return !const {
                'completed',
                'cancelled',
                'failed',
                'abandoned',
                'unsupported',
              }.contains(state);
            }).length,
            'lanes': json['hands_lanes'] ?? json['handsLanes'],
            'instructions': handsMailbox,
            'selected_instruction_id': _readString(activeInstruction, const [
              'id',
              'instruction_id',
              'instructionId',
            ]),
          };
    final rawCapabilities =
        json['capabilities'] ?? json['authorization_capabilities'];
    final capabilities = rawCapabilities is List
        ? <String, dynamic>{
            for (final capability in rawCapabilities.whereType<String>())
              capability: true,
          }
        : _asMap(rawCapabilities);
    final rawSurfaceHistory =
        fixer['surface_history'] ??
        fixer['surfaceHistory'] ??
        fixer['surfaces'] ??
        json['surface_history'] ??
        json['surfaceHistory'] ??
        json['surfaces'];
    final surfaceHistory = rawSurfaceHistory is List
        ? rawSurfaceHistory
        : rawSurface == null
        ? const <dynamic>[]
        : <dynamic>[rawSurface];
    final threads = _readMapList(fixer.isEmpty ? json : fixer, const [
      'threads',
      'fixer_threads',
    ], WorkroomFixerThread.fromJson);
    final selectedThreadId = _readString(fixer.isEmpty ? json : fixer, const [
      'selected_thread_id',
      'selectedThreadId',
      'thread_id',
    ], fallback: threads.isEmpty ? '' : threads.first.id);
    return ProjectWorkroomSnapshot(
      project: WorkroomProject.fromJson(project),
      protocolVersion: protocolVersion,
      watermarkSeq: _readInt(json, const [
        'watermark_seq',
        'watermarkSeq',
        'project_seq',
      ]),
      threads: List.unmodifiable(threads),
      selectedThreadId: selectedThreadId,
      turns: List.unmodifiable(
        _readMapList(
          fixer.isEmpty ? json : fixer,
          const ['turns', 'fixer_turns'],
          WorkroomFixerTurn.fromJson,
        )..sort((left, right) => left.ordinal.compareTo(right.ordinal)),
      ),
      activeSurface: rawSurface == null
          ? null
          : WorkroomSurfaceState.fromJson(_decodeMap(rawSurface)),
      surfaceHistory: List.unmodifiable(
        surfaceHistory
            .map(
              (surface) => WorkroomSurfaceState.fromJson(_decodeMap(surface)),
            )
            .toList(growable: false),
      ),
      hands: WorkroomHandsState.fromJson(hands),
      capabilities: WorkroomCapabilities.fromJson(capabilities),
    );
  }

  ProjectWorkroomSnapshot copyWith({
    WorkroomProject? project,
    int? watermarkSeq,
    List<WorkroomFixerThread>? threads,
    String? selectedThreadId,
    List<WorkroomFixerTurn>? turns,
    Object? activeSurface = _absent,
    List<WorkroomSurfaceState>? surfaceHistory,
    WorkroomHandsState? hands,
    WorkroomCapabilities? capabilities,
  }) {
    return ProjectWorkroomSnapshot(
      project: project ?? this.project,
      protocolVersion: protocolVersion,
      watermarkSeq: watermarkSeq ?? this.watermarkSeq,
      threads: threads ?? this.threads,
      selectedThreadId: selectedThreadId ?? this.selectedThreadId,
      turns: turns ?? this.turns,
      activeSurface: identical(activeSurface, _absent)
          ? this.activeSurface
          : activeSurface as WorkroomSurfaceState?,
      surfaceHistory: surfaceHistory ?? this.surfaceHistory,
      hands: hands ?? this.hands,
      capabilities: capabilities ?? this.capabilities,
    );
  }
}

class ProjectUiEvent {
  const ProjectUiEvent({
    required this.projectId,
    required this.seq,
    required this.eventId,
    required this.schemaVersion,
    required this.kind,
    required this.aggregateType,
    required this.aggregateId,
    required this.aggregateRevision,
    required this.createdAt,
    required this.payload,
  });

  final int projectId;
  final int seq;
  final String eventId;
  final int schemaVersion;
  final String kind;
  final String aggregateType;
  final String aggregateId;
  final int aggregateRevision;
  final String createdAt;
  final Map<String, dynamic> payload;

  factory ProjectUiEvent.fromJson(Map<String, dynamic> json) {
    final schemaVersion = _readInt(json, const [
      'schema_version',
      'schemaVersion',
    ]);
    if (schemaVersion != 1) {
      throw WorkroomProtocolException(
        'event_schema_unsupported',
        'Event schema version $schemaVersion is not supported.',
      );
    }
    final kind = _readString(json, const ['kind']);
    if (!registeredProjectUiEventKinds.contains(kind)) {
      throw WorkroomProtocolException(
        'event_kind_unsupported',
        'Event kind "$kind" requires a newer application.',
      );
    }
    return ProjectUiEvent(
      projectId: _readInt(json, const ['project_id', 'projectId']),
      seq: _readInt(json, const ['seq', 'sequence']),
      eventId: _readString(json, const ['event_id', 'eventId']),
      schemaVersion: schemaVersion,
      kind: kind,
      aggregateType: _readString(json, const [
        'aggregate_type',
        'aggregateType',
      ]),
      aggregateId: _readString(json, const ['aggregate_id', 'aggregateId']),
      aggregateRevision: _readInt(json, const [
        'aggregate_revision',
        'aggregateRevision',
      ]),
      createdAt: _readString(json, const ['created_at', 'createdAt']),
      payload: _decodeMap(json['payload'] ?? json['payload_json']),
    );
  }
}

sealed class ProjectUiFrame {
  const ProjectUiFrame();

  factory ProjectUiFrame.fromTransport(dynamic value) {
    final json = _transportJson(value);
    final rawKind = _readString(json, const [
      'frame_type',
      'frameType',
      'type',
      'kind',
    ]);
    if (rawKind == 'event' ||
        rawKind == 'project_ui_event' ||
        json['event'] != null ||
        (json['seq'] != null && json['event_id'] != null)) {
      return ProjectUiEventFrame(
        ProjectUiEvent.fromJson(_decodeMap(json['event'] ?? json)),
      );
    }
    if (rawKind == 'event_batch' ||
        rawKind == 'batch' ||
        json['events'] is List) {
      final events = _asList(json['events']);
      if (events.length > 100) {
        throw const WorkroomProtocolException(
          'event_batch_too_large',
          'Project event batches may contain at most 100 events.',
        );
      }
      return ProjectUiEventBatchFrame(
        events
            .map((event) => ProjectUiEvent.fromJson(_decodeMap(event)))
            .toList(growable: false),
      );
    }
    if (rawKind == 'heartbeat') {
      return ProjectUiHeartbeatFrame(
        serverTime: _readString(json, const ['server_time', 'serverTime']),
        journalHead: _readInt(json, const ['journal_head', 'journalHead']),
      );
    }
    if (rawKind == 'protocol_error') {
      return ProjectUiProtocolErrorFrame(
        reasonCode: _readString(json, const [
          'reason_code',
          'reasonCode',
        ], fallback: 'protocol_error'),
        message: _readString(json, const ['message', 'reason']),
        minimumVersion: _readInt(json, const [
          'minimum_version',
          'minimumVersion',
        ]),
        maximumVersion: _readInt(json, const [
          'maximum_version',
          'maximumVersion',
        ]),
      );
    }
    throw const WorkroomProtocolException(
      'frame_unsupported',
      'The project stream returned an unknown frame.',
    );
  }
}

class ProjectUiEventFrame extends ProjectUiFrame {
  const ProjectUiEventFrame(this.event);

  final ProjectUiEvent event;
}

class ProjectUiEventBatchFrame extends ProjectUiFrame {
  const ProjectUiEventBatchFrame(this.events);

  final List<ProjectUiEvent> events;
}

class ProjectUiHeartbeatFrame extends ProjectUiFrame {
  const ProjectUiHeartbeatFrame({
    required this.serverTime,
    required this.journalHead,
  });

  final String serverTime;
  final int journalHead;
}

class ProjectUiProtocolErrorFrame extends ProjectUiFrame {
  const ProjectUiProtocolErrorFrame({
    required this.reasonCode,
    required this.message,
    required this.minimumVersion,
    required this.maximumVersion,
  });

  final String reasonCode;
  final String message;
  final int minimumVersion;
  final int maximumVersion;
}

class RegisteredSurfaceRequest {
  const RegisteredSurfaceRequest({
    required this.surfaceType,
    this.surfaceVersion = 1,
    this.arguments = const <String, dynamic>{},
  });

  final String surfaceType;
  final int surfaceVersion;
  final Map<String, dynamic> arguments;

  String get key => '$surfaceType.v$surfaceVersion';

  void validate() {
    if (!registeredWorkroomSurfaceTypes.contains(key)) {
      throw WorkroomProtocolException(
        'surface_unsupported',
        'Surface "$key" is not registered.',
      );
    }
    final allowedArguments = switch (key) {
      'project.overview.v1' ||
      'skills.catalog.v1' ||
      'research.legal.v1' => const <String>{},
      'wave.list.v1' => const {'filter'},
      'wave.detail.v1' => const {'wave_id'},
      'execution.review.v1' || 'execution.detail.v1' => const {'session_id'},
      'backlog.list.v1' => const {'status'},
      'backlog.item.v1' => const {'item_id'},
      'docs.tree.v1' => const {'level'},
      'docs.viewer.v1' => const {'project_doc_id'},
      'execution.list.v1' => const {'status', 'source'},
      'skills.detail.v1' => const {'skill_id'},
      'runtime.evidence.v1' => const {'ref_type', 'ref_id'},
      'unsupported.request.v1' => const {'demand_example_id'},
      _ => const <String>{},
    };
    final unknown = arguments.keys.toSet().difference(allowedArguments);
    if (unknown.isNotEmpty) {
      throw WorkroomProtocolException(
        'surface_arguments_invalid',
        'Unknown surface arguments: ${unknown.join(', ')}.',
      );
    }
    switch (key) {
      case 'project.overview.v1':
      case 'skills.catalog.v1':
      case 'research.legal.v1':
        return;
      case 'wave.detail.v1':
        _requirePositiveIntArgument('wave_id');
      case 'execution.review.v1':
      case 'execution.detail.v1':
        _requirePositiveIntArgument('session_id');
      case 'docs.viewer.v1':
        _requirePositiveIntArgument('project_doc_id');
      case 'docs.tree.v1':
        final level = arguments['level'];
        if (level != null && (level is! int || level < 0 || level > 3)) {
          throw const WorkroomProtocolException(
            'surface_arguments_invalid',
            'Argument "level" must be an integer from 0 through 3.',
          );
        }
      case 'wave.list.v1':
        _validateOptionalToken('filter');
      case 'backlog.list.v1':
        _validateOptionalToken('status');
      case 'execution.list.v1':
        _validateOptionalToken('status');
        _validateOptionalToken('source');
      case 'backlog.item.v1':
        _requireBoundedStringArgument('item_id');
      case 'skills.detail.v1':
        _requireBoundedStringArgument('skill_id');
      case 'unsupported.request.v1':
        _requireBoundedStringArgument('demand_example_id');
      case 'runtime.evidence.v1':
        _validateOptionalToken('ref_type', required: true);
        _requireBoundedStringArgument('ref_id');
    }
  }

  void _requirePositiveIntArgument(String key) {
    final value = arguments[key];
    if (value is! int || value <= 0) {
      throw WorkroomProtocolException(
        'surface_arguments_invalid',
        'Argument "$key" must be a positive integer.',
      );
    }
  }

  void _requireBoundedStringArgument(String key) {
    final value = arguments[key];
    if (value is! String ||
        value.trim().isEmpty ||
        value.length > 200 ||
        value.contains(RegExp(r'[\u0000-\u001f]'))) {
      throw WorkroomProtocolException(
        'surface_arguments_invalid',
        'Argument "$key" must be a non-empty bounded identifier.',
      );
    }
  }

  void _validateOptionalToken(String key, {bool required = false}) {
    final value = arguments[key];
    if (value == null && !required) return;
    if (value is! String ||
        value.isEmpty ||
        value.length > 80 ||
        !RegExp(r'^[a-zA-Z0-9_.-]+$').hasMatch(value)) {
      throw WorkroomProtocolException(
        'surface_arguments_invalid',
        'Argument "$key" must be a registered scalar token.',
      );
    }
  }

  Map<String, dynamic> toJson() => {
    'surface_type': surfaceType,
    'surface_version': surfaceVersion,
    'arguments': arguments,
  };
}

class FixerTurnReceipt {
  const FixerTurnReceipt({
    required this.turnId,
    required this.status,
    required this.projectSeq,
  });

  final String turnId;
  final String status;
  final int projectSeq;

  factory FixerTurnReceipt.fromJson(Map<String, dynamic> json) {
    return FixerTurnReceipt(
      turnId: _readString(json, const ['turn_id', 'turnId', 'id']),
      status: _readString(json, const ['status'], fallback: 'accepted'),
      projectSeq: _readInt(json, const ['project_seq', 'projectSeq']),
    );
  }
}

class GenUiActionReceipt {
  const GenUiActionReceipt({
    required this.status,
    required this.reasonCode,
    required this.message,
    required this.projectSeq,
    required this.vote,
  });

  final String status;
  final String reasonCode;
  final String message;
  final int projectSeq;
  final int vote;

  bool get succeeded => status == 'succeeded' || status == 'recorded';

  factory GenUiActionReceipt.fromJson(Map<String, dynamic> json) {
    return GenUiActionReceipt(
      status: _readString(json, const ['status'], fallback: 'received'),
      reasonCode: _readString(json, const ['reason_code', 'reasonCode']),
      message: _readString(json, const ['message']),
      projectSeq: _readInt(json, const ['project_seq', 'projectSeq']),
      vote: _readInt(json, const ['vote']),
    );
  }
}

class HandsInstructionReceipt {
  const HandsInstructionReceipt({
    required this.instructionId,
    required this.ordinal,
    required this.state,
    required this.lane,
    required this.projectSeq,
  });

  final String instructionId;
  final int ordinal;
  final String state;
  final String lane;
  final int projectSeq;

  factory HandsInstructionReceipt.fromJson(Map<String, dynamic> json) {
    return HandsInstructionReceipt(
      instructionId: _readString(json, const [
        'instruction_id',
        'instructionId',
        'id',
      ]),
      ordinal: _readInt(json, const ['ordinal']),
      state: _readString(json, const ['state'], fallback: 'queued'),
      lane: _readString(json, const [
        'lane',
        'requested_lane',
      ], fallback: 'codex'),
      projectSeq: _readInt(json, const ['project_seq', 'projectSeq']),
    );
  }
}

const _absent = Object();

Map<String, dynamic> _transportJson(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  try {
    final dynamic json = value.toJson();
    if (json is Map<String, dynamic>) return json;
    if (json is Map) return Map<String, dynamic>.from(json);
  } on Object {
    // Fall through to the protocol error below.
  }
  throw const WorkroomProtocolException(
    'transport_payload_invalid',
    'The Serverpod transport returned a non-serializable payload.',
  );
}

Map<String, dynamic> _decodeMap(dynamic value) {
  if (value == null) return <String, dynamic>{};
  if (value is String) {
    if (value.trim().isEmpty) return <String, dynamic>{};
    final decoded = jsonDecode(value);
    return _asMap(decoded);
  }
  return _transportJson(value);
}

Map<String, dynamic> _asMap(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  return <String, dynamic>{};
}

List<dynamic> _asList(dynamic value) {
  return value is List ? value : const <dynamic>[];
}

List<T> _readMapList<T>(
  Map<String, dynamic> json,
  List<String> keys,
  T Function(Map<String, dynamic>) convert,
) {
  dynamic value;
  for (final key in keys) {
    if (json.containsKey(key)) {
      value = json[key];
      break;
    }
  }
  return _asList(value).map((item) => convert(_decodeMap(item))).toList();
}

List<String> _readStringList(Map<String, dynamic> json, List<String> keys) {
  dynamic value;
  for (final key in keys) {
    if (json.containsKey(key)) {
      value = json[key];
      break;
    }
  }
  return _asList(
    value,
  ).whereType<Object>().map((item) => item.toString()).toList(growable: false);
}

String _readString(
  Map<String, dynamic> json,
  List<String> keys, {
  String fallback = '',
  int? maxLength,
}) {
  for (final key in keys) {
    final value = json[key];
    if (value is String) {
      if (maxLength != null && value.length > maxLength) {
        throw WorkroomProtocolException(
          'string_too_long',
          'Field "$key" exceeds its protocol limit.',
        );
      }
      return value;
    }
    if (value != null) return value.toString();
  }
  return fallback;
}

int _readInt(Map<String, dynamic> json, List<String> keys, {int fallback = 0}) {
  for (final key in keys) {
    final value = json[key];
    if (value is int) return value;
    if (value is num) return value.toInt();
    final parsed = int.tryParse(value?.toString() ?? '');
    if (parsed != null) return parsed;
  }
  return fallback;
}

bool _requiredBool(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is bool) return value;
  throw WorkroomProtocolException(
    'field_invalid',
    'Field "$key" must be a boolean.',
  );
}

String _requiredString(
  Map<String, dynamic> json,
  String key, {
  required int maxLength,
}) {
  final value = json[key];
  if (value is! String || value.isEmpty || value.length > maxLength) {
    throw WorkroomProtocolException(
      'field_invalid',
      'Field "$key" must be a non-empty bounded string.',
    );
  }
  return value;
}

String _optionalString(
  Map<String, dynamic> json,
  String key, {
  required int maxLength,
}) {
  final value = json[key];
  if (value == null) return '';
  if (value is! String || value.length > maxLength) {
    throw WorkroomProtocolException(
      'field_invalid',
      'Field "$key" must be a bounded string.',
    );
  }
  return value;
}

int _requiredInt(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is int) return value;
  if (value is num) return value.toInt();
  throw WorkroomProtocolException(
    'field_invalid',
    'Field "$key" must be an integer.',
  );
}

GenUiTone _parseTone(dynamic value) {
  return switch (value) {
    'neutral' => GenUiTone.neutral,
    'info' => GenUiTone.info,
    'success' => GenUiTone.success,
    'warning' => GenUiTone.warning,
    'danger' => GenUiTone.danger,
    _ => throw const WorkroomProtocolException(
      'tone_invalid',
      'Component tone is not registered.',
    ),
  };
}

void _rejectUnknownKeys(
  Map<String, dynamic> json,
  Set<String> allowed,
  String context,
) {
  final unknown = json.keys.toSet().difference(allowed);
  if (unknown.isNotEmpty) {
    throw WorkroomProtocolException(
      'additional_properties_forbidden',
      '$context contains unsupported fields: ${unknown.join(', ')}.',
    );
  }
}

void _validateJsonDepth(dynamic value, int depth) {
  if (depth > 6) {
    throw const WorkroomProtocolException(
      'document_nesting_exceeded',
      'The surface document exceeds the maximum nesting depth of 6.',
    );
  }
  if (value is Map) {
    for (final child in value.values) {
      _validateJsonDepth(child, depth + 1);
    }
  } else if (value is List) {
    for (final child in value) {
      _validateJsonDepth(child, depth + 1);
    }
  }
}
