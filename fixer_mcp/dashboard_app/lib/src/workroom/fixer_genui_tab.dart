import 'package:flutter/material.dart';

import '../app_localizations.dart';
import 'genui_renderer.dart';
import 'workroom_models.dart';
import 'workroom_store.dart';
import 'workroom_turn_filter.dart';

const _surfaceCatalog = <RegisteredSurfaceRequest>[
  RegisteredSurfaceRequest(surfaceType: 'project.overview'),
  RegisteredSurfaceRequest(surfaceType: 'research.legal'),
  RegisteredSurfaceRequest(surfaceType: 'wave.list'),
  RegisteredSurfaceRequest(surfaceType: 'backlog.list'),
  RegisteredSurfaceRequest(surfaceType: 'docs.tree'),
  RegisteredSurfaceRequest(surfaceType: 'execution.list'),
  RegisteredSurfaceRequest(surfaceType: 'skills.catalog'),
];

enum _TechnicalVisibility { conversationOnly, context, all }

enum WorkroomTechnicalVisibility { conversationOnly, context, all }

class FixerGenUiTab extends StatefulWidget {
  const FixerGenUiTab({
    super.key,
    required this.store,
    this.loadHistoricalTurns,
    this.onNewThread,
  });

  final ProjectWorkroomStore store;
  final Future<List<WorkroomFixerTurn>> Function(String threadId)?
  loadHistoricalTurns;
  final Future<void> Function()? onNewThread;

  @override
  State<FixerGenUiTab> createState() => _FixerGenUiTabState();
}

class _FixerGenUiTabState extends State<FixerGenUiTab> {
  final _composer = TextEditingController();
  bool _sending = false;
  bool _requestingSurface = false;
  bool _submittingFeedback = false;
  String _pendingActionRef = '';
  String _surfaceRequestError = '';
  String _loadingThreadId = '';
  _TechnicalVisibility _technicalVisibility =
      _TechnicalVisibility.conversationOnly;

  @override
  void initState() {
    super.initState();
    widget.store.addListener(_loadSelectedHistoricalThread);
  }

  @override
  void dispose() {
    widget.store.removeListener(_loadSelectedHistoricalThread);
    _composer.dispose();
    super.dispose();
  }

  @override
  void reassemble() {
    super.reassemble();
    widget.store.reloadSnapshot();
  }

  void _loadSelectedHistoricalThread() {
    final loader = widget.loadHistoricalTurns;
    final snapshot = widget.store.state.snapshot;
    final selectedTurns = snapshot.turns
        .where((turn) => turn.threadId == snapshot.selectedThreadId)
        .length;
    if (loader == null || snapshot.selectedThreadId.isEmpty) return;
    if (_loadingThreadId == snapshot.selectedThreadId || selectedTurns > 0) {
      if (selectedTurns > 0) {
        debugPrint(
          '[workroom] selected ${snapshot.selectedThreadId}: '
          '$selectedTurns turns already in snapshot',
        );
      }
      return;
    }
    _loadingThreadId = snapshot.selectedThreadId;
    debugPrint(
      '[workroom] loading historical thread ${snapshot.selectedThreadId}',
    );
    widget.store
        .loadHistoricalThread(snapshot.selectedThreadId, loader)
        .whenComplete(() {
          debugPrint(
            '[workroom] historical thread ${snapshot.selectedThreadId} load finished',
          );
          if (_loadingThreadId == snapshot.selectedThreadId) {
            _loadingThreadId = '';
          }
        });
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.store,
      builder: (context, _) {
        final state = widget.store.state;
        return LayoutBuilder(
          builder: (context, constraints) {
            final conversation = WorkroomConversationPane(
              state: state,
              composer: _composer,
              sending: _sending,
              onThreadSelected: _selectThread,
              onNewThread:
                  widget.onNewThread ??
                  () async {
                    widget.store.startNewFixerThread();
                  },
              onSend: _sendFixerTurn,
              onOpenSurface: () => _showSurfaceSheet(context),
              technicalVisibility: WorkroomTechnicalVisibility
                  .values[_technicalVisibility.index],
              onTechnicalVisibilityChanged: (value) {
                setState(
                  () => _technicalVisibility =
                      _TechnicalVisibility.values[value.index],
                );
              },
              compact: constraints.maxWidth < 1100,
            );
            if (constraints.maxWidth < 1100) return conversation;
            return Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Expanded(flex: 44, child: conversation),
                const VerticalDivider(width: 1),
                Expanded(
                  flex: 56,
                  child: _SurfacePane(
                    state: state,
                    requestingSurface: _requestingSurface,
                    requestError: _surfaceRequestError,
                    submittingFeedback: _submittingFeedback,
                    pendingActionRef: _pendingActionRef,
                    onRequestSurface: _requestSurface,
                    onActivateSurface: widget.store.activateSurface,
                    onFeedback: _submitFeedback,
                    onAction: _handleAction,
                  ),
                ),
              ],
            );
          },
        );
      },
    );
  }

  Future<void> _selectThread(String threadId) async {
    final loader = widget.loadHistoricalTurns;
    final hasTurns = widget.store.state.snapshot.turns.any(
      (turn) => turn.threadId == threadId,
    );
    if (loader == null || hasTurns) {
      widget.store.selectThread(threadId);
      return;
    }
    await widget.store.loadHistoricalThread(threadId, loader);
  }

  Future<void> _sendFixerTurn() async {
    final text = _composer.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() => _sending = true);
    try {
      await widget.store.sendFixerTurn(text);
      if (!mounted) return;
      _composer.clear();
    } on Object catch (error) {
      _showError(error);
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _requestSurface(RegisteredSurfaceRequest request) async {
    if (_requestingSurface) return;
    setState(() {
      _requestingSurface = true;
      _surfaceRequestError = '';
    });
    try {
      final receipt = await widget.store.requestSurface(request);
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isNotEmpty
              ? receipt.message
              : receipt.reasonCode.isNotEmpty
              ? receipt.reasonCode
              : 'Surface request was rejected.',
        );
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _surfaceRequestError = error.toString());
      }
    } finally {
      if (mounted) setState(() => _requestingSurface = false);
    }
  }

  Future<void> _submitFeedback(int vote) async {
    if (_submittingFeedback) return;
    setState(() => _submittingFeedback = true);
    final l10n = AppLocalizations.of(context);
    try {
      final receipt = await widget.store.submitFeedback(vote);
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isEmpty ? receipt.status : receipt.message,
        );
      }
      _showNotice(l10n.feedbackRecorded);
    } on Object catch (error) {
      _showNotice('${l10n.feedbackFailed} $error');
    } finally {
      if (mounted) setState(() => _submittingFeedback = false);
    }
  }

  Future<void> _handleAction(GenUiActionDescriptor action) async {
    if (_pendingActionRef.isNotEmpty) return;
    if (action.actionId == 'ui.surface.dismiss') {
      WorkroomSurfaceState? overview;
      for (final surface in widget.store.state.snapshot.surfaceHistory) {
        if (surface.document.surfaceType == 'project.overview' &&
            surface.document.surfaceVersion == 1) {
          overview = surface;
          break;
        }
      }
      if (overview != null) {
        widget.store.activateSurface(overview.document.instanceId);
        return;
      }
      await _requestSurface(_surfaceCatalog.first);
      return;
    }
    if (action.actionId == 'ui.surface.open') {
      if (!_handleLocalSurfaceOpen(action)) {
        _showError(
          WorkroomProtocolException(
            'action_target_unsupported',
            'Surface target "${action.target.type}" is not registered.',
          ),
        );
      }
      return;
    }
    var confirmed = !action.requiresConfirmation;
    if (!confirmed) {
      confirmed = await _confirmAction(action);
    }
    if (!confirmed || !mounted) return;
    setState(() => _pendingActionRef = action.ref);
    try {
      final receipt = await widget.store.invokeAction(action, confirmed: true);
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isNotEmpty ? receipt.message : receipt.reasonCode,
        );
      }
      if (receipt.message.isNotEmpty) _showNotice(receipt.message);
    } on Object catch (error) {
      _showError(error);
    } finally {
      if (mounted) setState(() => _pendingActionRef = '');
    }
  }

  bool _handleLocalSurfaceOpen(GenUiActionDescriptor action) {
    final target = action.target;
    final history = widget.store.state.snapshot.surfaceHistory;
    if (history.any((item) => item.document.instanceId == target.id)) {
      widget.store.activateSurface(target.id);
      return true;
    }
    RegisteredSurfaceRequest? request;
    switch (target.type) {
      case 'surface':
        final match = RegExp(r'^(.+)\.v([0-9]+)$').firstMatch(target.id);
        if (match != null) {
          request = RegisteredSurfaceRequest(
            surfaceType: match.group(1)!,
            surfaceVersion: int.parse(match.group(2)!),
          );
        }
      case 'wave':
      case 'planned_wave':
        final waveId = int.tryParse(target.id);
        if (waveId != null) {
          request = RegisteredSurfaceRequest(
            surfaceType: 'wave.detail',
            arguments: {'wave_id': waveId},
          );
        }
      case 'session':
        final sessionId = int.tryParse(target.id);
        if (sessionId != null) {
          request = RegisteredSurfaceRequest(
            surfaceType: 'execution.detail',
            arguments: {'session_id': sessionId},
          );
        }
      case 'execution_review':
        final sessionId = int.tryParse(target.id);
        if (sessionId != null) {
          request = RegisteredSurfaceRequest(
            surfaceType: 'execution.review',
            arguments: {'session_id': sessionId},
          );
        }
      case 'backlog_item':
        request = RegisteredSurfaceRequest(
          surfaceType: 'backlog.item',
          arguments: {'item_id': target.id},
        );
      case 'project_doc':
        final documentId = int.tryParse(target.id);
        if (documentId != null) {
          request = RegisteredSurfaceRequest(
            surfaceType: 'docs.viewer',
            arguments: {'project_doc_id': documentId},
          );
        }
      case 'skill':
        request = RegisteredSurfaceRequest(
          surfaceType: 'skills.detail',
          arguments: {'skill_id': target.id},
        );
      case 'runtime':
        request = RegisteredSurfaceRequest(
          surfaceType: 'runtime.evidence',
          arguments: {'ref_type': 'runtime', 'ref_id': target.id},
        );
      case 'project':
        request = const RegisteredSurfaceRequest(
          surfaceType: 'project.overview',
        );
      case 'genui_surface':
        return false;
    }
    if (request == null) return false;
    _requestSurface(request);
    return true;
  }

  Future<bool> _confirmAction(GenUiActionDescriptor action) async {
    final l10n = AppLocalizations.of(context);
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(l10n.governedAction),
            content: Text(l10n.confirmAction(action.label)),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: Text(l10n.cancel),
              ),
              FilledButton(
                key: const ValueKey('confirm-governed-action'),
                onPressed: () => Navigator.of(context).pop(true),
                child: Text(l10n.continueAction),
              ),
            ],
          ),
        ) ??
        false;
  }

  Future<void> _showSurfaceSheet(BuildContext context) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => FractionallySizedBox(
        heightFactor: 0.86,
        child: AnimatedBuilder(
          animation: widget.store,
          builder: (context, _) => _SurfacePane(
            state: widget.store.state,
            requestingSurface: _requestingSurface,
            requestError: _surfaceRequestError,
            submittingFeedback: _submittingFeedback,
            pendingActionRef: _pendingActionRef,
            onRequestSurface: _requestSurface,
            onActivateSurface: widget.store.activateSurface,
            onFeedback: _submitFeedback,
            onAction: _handleAction,
          ),
        ),
      ),
    );
  }

  bool _receiptFailed(GenUiActionReceipt receipt) {
    return const {'denied', 'failed', 'unsupported'}.contains(receipt.status);
  }

  void _showError(Object error) => _showNotice(error.toString());

  void _showNotice(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }
}

class WorkroomConversationPane extends StatelessWidget {
  const WorkroomConversationPane({
    super.key,
    required this.state,
    required this.composer,
    required this.sending,
    required this.onThreadSelected,
    this.onNewThread,
    required this.onSend,
    this.onOpenSurface,
    required this.technicalVisibility,
    required this.onTechnicalVisibilityChanged,
    required this.compact,
    this.title,
    this.selectorLabel,
    this.emptyLabel,
    this.composerHint,
    this.canSend,
    this.keyPrefix = 'fixer',
  });

  final ProjectWorkroomState state;
  final TextEditingController composer;
  final bool sending;
  final Future<void> Function(String threadId) onThreadSelected;
  final Future<void> Function()? onNewThread;
  final VoidCallback onSend;
  final VoidCallback? onOpenSurface;
  final WorkroomTechnicalVisibility technicalVisibility;
  final ValueChanged<WorkroomTechnicalVisibility> onTechnicalVisibilityChanged;
  final bool compact;
  final String? title;
  final String? selectorLabel;
  final String? emptyLabel;
  final String? composerHint;
  final bool? canSend;
  final String keyPrefix;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final snapshot = state.snapshot;
    final turns = snapshot.turns
        .where((turn) => turn.threadId == snapshot.selectedThreadId)
        .toList(growable: false);
    WorkroomFixerThread? selectedThread;
    for (final thread in snapshot.threads) {
      if (thread.id == snapshot.selectedThreadId) {
        selectedThread = thread;
        break;
      }
    }
    final provider = selectedThread?.provider ?? '';
    final presentedTurns = turns
        .map(
          (turn) => (
            turn: turn,
            presentation: classifyWorkroomTurn(
              provider: provider,
              role: turn.role,
              source: turn.source ?? '',
            ),
          ),
        )
        .toList(growable: false);
    final visibleTurns = presentedTurns
        .where((entry) {
          if (!entry.presentation.technical) return true;
          if (technicalVisibility == WorkroomTechnicalVisibility.all) {
            return true;
          }
          if (technicalVisibility == WorkroomTechnicalVisibility.context) {
            return entry.presentation.kind == WorkroomTurnKind.context ||
                entry.presentation.kind == WorkroomTurnKind.compaction;
          }
          return false;
        })
        .toList(growable: false);
    final bottomFirstTurns = visibleTurns.reversed.toList(growable: false);
    return FocusTraversalGroup(
      policy: WidgetOrderTraversalPolicy(),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _ConnectionBanner(state: state),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 10),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title ?? l10n.fixerConversation,
                        style: Theme.of(context).textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      Text(
                        '${snapshot.threads.length} · '
                        '${snapshot.project.name}',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
                if (compact && onOpenSurface != null)
                  OutlinedButton.icon(
                    key: const ValueKey('open-genui-surface-sheet'),
                    onPressed: onOpenSurface,
                    icon: const Icon(Icons.dashboard_customize_outlined),
                    label: Text(l10n.genuiSurface),
                  ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Expanded(
                  child: DropdownButtonFormField<String>(
                    key: ValueKey('$keyPrefix-thread-selector'),
                    isExpanded: true,
                    initialValue:
                        snapshot.threads.any(
                          (thread) => thread.id == snapshot.selectedThreadId,
                        )
                        ? snapshot.selectedThreadId
                        : null,
                    decoration: InputDecoration(
                      labelText: selectorLabel ?? l10n.chooseFixerThread,
                      isDense: true,
                    ),
                    items: [
                      for (final thread in snapshot.threads)
                        DropdownMenuItem(
                          value: thread.id,
                          child: Text(
                            '${thread.provider} · created '
                            '${workroomRelativeAge(thread.createdAt)} · updated '
                            '${workroomRelativeAge(thread.updatedAt)}',
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                    ],
                    onChanged: (value) {
                      if (value != null) onThreadSelected(value);
                    },
                  ),
                ),
                if (onNewThread != null) ...[
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: onNewThread,
                    child: const Text('New'),
                  ),
                ],
                const SizedBox(width: 4),
                PopupMenuButton<WorkroomTechnicalVisibility>(
                  tooltip: 'Technical messages',
                  initialValue: technicalVisibility,
                  onSelected: onTechnicalVisibilityChanged,
                  itemBuilder: (context) => const [
                    PopupMenuItem(
                      value: WorkroomTechnicalVisibility.conversationOnly,
                      child: Text('Conversation only'),
                    ),
                    PopupMenuItem(
                      value: WorkroomTechnicalVisibility.context,
                      child: Text('Conversation + context'),
                    ),
                    PopupMenuItem(
                      value: WorkroomTechnicalVisibility.all,
                      child: Text('All technical messages'),
                    ),
                  ],
                  icon: Icon(
                    technicalVisibility ==
                            WorkroomTechnicalVisibility.conversationOnly
                        ? Icons.filter_alt_off_outlined
                        : Icons.filter_alt_outlined,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 10),
          const Divider(height: 1),
          Expanded(
            child: visibleTurns.isEmpty
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(
                        turns.isEmpty
                            ? emptyLabel ?? l10n.noFixerTurns
                            : 'Technical messages are hidden. Use the filter to show them.',
                        textAlign: TextAlign.center,
                        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  )
                : ListView.builder(
                    key: ValueKey('$keyPrefix-turn-list'),
                    reverse: true,
                    padding: const EdgeInsets.all(16),
                    itemCount: bottomFirstTurns.length,
                    itemBuilder: (context, index) => _TurnBubble(
                      turn: bottomFirstTurns[index].turn,
                      presentation: bottomFirstTurns[index].presentation,
                    ),
                  ),
          ),
          const Divider(height: 1),
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: TextField(
                    key: ValueKey('$keyPrefix-message-composer'),
                    controller: composer,
                    minLines: 1,
                    maxLines: 5,
                    textInputAction: TextInputAction.newline,
                    decoration: InputDecoration(
                      hintText: composerHint ?? l10n.fixerConversationHint,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Semantics(
                  button: true,
                  label: l10n.sendMessage,
                  child: FilledButton.icon(
                    key: ValueKey('send-$keyPrefix-message'),
                    onPressed:
                        sending ||
                            !(canSend ?? snapshot.capabilities.canSendFixerTurn)
                        ? null
                        : onSend,
                    icon: sending
                        ? const SizedBox.square(
                            dimension: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.send_outlined, size: 18),
                    label: Text(l10n.sendMessage),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _TurnBubble extends StatelessWidget {
  const _TurnBubble({required this.turn, required this.presentation});

  final WorkroomFixerTurn turn;
  final WorkroomTurnPresentation presentation;

  @override
  Widget build(BuildContext context) {
    if (presentation.technical) {
      return _TechnicalTurn(turn: turn, presentation: presentation);
    }
    final isUser = turn.role == 'user';
    final theme = Theme.of(context);
    return Align(
      alignment: isUser
          ? AlignmentDirectional.centerEnd
          : AlignmentDirectional.centerStart,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 560),
        margin: const EdgeInsets.only(bottom: 10),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        decoration: BoxDecoration(
          color: isUser
              ? theme.colorScheme.primaryContainer
              : theme.colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              turn.role,
              style: theme.textTheme.labelSmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 3),
            SelectableText(turn.content),
            if (turn.status != 'complete') ...[
              const SizedBox(height: 6),
              Text(
                turn.status,
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.primary,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _TechnicalTurn extends StatelessWidget {
  const _TechnicalTurn({required this.turn, required this.presentation});

  final WorkroomFixerTurn turn;
  final WorkroomTurnPresentation presentation;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      color: theme.colorScheme.tertiaryContainer.withValues(alpha: 0.45),
      child: ExpansionTile(
        leading: Icon(
          Icons.data_object_outlined,
          color: theme.colorScheme.tertiary,
        ),
        title: Text(presentation.label),
        subtitle: Text(turn.role),
        childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 14),
        children: [SelectableText(turn.content)],
      ),
    );
  }
}

class _SurfacePane extends StatelessWidget {
  const _SurfacePane({
    required this.state,
    required this.requestingSurface,
    required this.requestError,
    required this.submittingFeedback,
    required this.pendingActionRef,
    required this.onRequestSurface,
    required this.onActivateSurface,
    required this.onFeedback,
    required this.onAction,
  });

  final ProjectWorkroomState state;
  final bool requestingSurface;
  final String requestError;
  final bool submittingFeedback;
  final String pendingActionRef;
  final ValueChanged<RegisteredSurfaceRequest> onRequestSurface;
  final ValueChanged<String> onActivateSurface;
  final ValueChanged<int> onFeedback;
  final Future<void> Function(GenUiActionDescriptor action) onAction;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final surface = state.snapshot.activeSurface;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 12, 8),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      surface?.document.title ?? l10n.genuiSurface,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    if (surface != null)
                      Text(
                        '${surface.document.surfaceType}.v'
                        '${surface.document.surfaceVersion} · '
                        'seq ${surface.document.sourceSeq}',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
              if (state.snapshot.surfaceHistory.length > 1)
                PopupMenuButton<String>(
                  key: const ValueKey('genui-surface-history'),
                  tooltip: l10n.isRussian
                      ? 'История поверхностей'
                      : 'Surface history',
                  onSelected: onActivateSurface,
                  itemBuilder: (context) => [
                    for (final item in state.snapshot.surfaceHistory)
                      PopupMenuItem(
                        value: item.document.instanceId,
                        child: Text(item.document.title),
                      ),
                  ],
                  icon: const Icon(Icons.history),
                ),
              PopupMenuButton<RegisteredSurfaceRequest>(
                key: const ValueKey('genui-surface-catalog'),
                tooltip: l10n.surfaceCatalog,
                onSelected: onRequestSurface,
                itemBuilder: (context) => [
                  for (final request in _surfaceCatalog)
                    PopupMenuItem(
                      key: ValueKey('surface-request-${request.key}'),
                      value: request,
                      child: Text(l10n.surfaceLabel(request.key)),
                    ),
                ],
                icon: const Icon(Icons.widgets_outlined),
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        if (requestingSurface)
          LinearProgressIndicator(
            key: const ValueKey('genui-surface-request-progress'),
            semanticsLabel: l10n.surfaceRequestPending,
          ),
        if (requestError.isNotEmpty)
          _InlineCallout(
            icon: Icons.error_outline,
            message: requestError,
            danger: true,
          ),
        if (state.surfaceError.isNotEmpty)
          _InlineCallout(
            icon: Icons.security_outlined,
            message: '${l10n.surfaceProtocolError}\n${state.surfaceError}',
            danger: true,
          ),
        Expanded(
          child: surface == null
              ? Center(child: Text(l10n.noActiveSurface))
              : GenUiRenderer(
                  document: surface.document,
                  pendingActionRef: pendingActionRef,
                  onAction: onAction,
                ),
        ),
        if (surface != null) ...[
          const Divider(height: 1),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
            child: Row(
              children: [
                Text(
                  l10n.isRussian
                      ? 'Эта поверхность полезна?'
                      : 'Is this useful?',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const Spacer(),
                Semantics(
                  button: true,
                  label: l10n.helpful,
                  excludeSemantics: true,
                  child: IconButton(
                    key: const ValueKey('genui-feedback-up'),
                    tooltip: l10n.helpful,
                    onPressed:
                        submittingFeedback ||
                            !state.snapshot.capabilities.canWriteFeedback
                        ? null
                        : () => onFeedback(1),
                    isSelected: surface.feedbackVote == 1,
                    selectedIcon: const Icon(Icons.thumb_up),
                    icon: const Icon(Icons.thumb_up_outlined),
                  ),
                ),
                Semantics(
                  button: true,
                  label: l10n.notHelpful,
                  excludeSemantics: true,
                  child: IconButton(
                    key: const ValueKey('genui-feedback-down'),
                    tooltip: l10n.notHelpful,
                    onPressed:
                        submittingFeedback ||
                            !state.snapshot.capabilities.canWriteFeedback
                        ? null
                        : () => onFeedback(-1),
                    isSelected: surface.feedbackVote == -1,
                    selectedIcon: const Icon(Icons.thumb_down),
                    icon: const Icon(Icons.thumb_down_outlined),
                  ),
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}

class _ConnectionBanner extends StatelessWidget {
  const _ConnectionBanner({required this.state});

  final ProjectWorkroomState state;

  @override
  Widget build(BuildContext context) {
    if (state.connectionStatus == WorkroomConnectionStatus.live) {
      return const SizedBox.shrink();
    }
    final l10n = AppLocalizations.of(context);
    final (icon, label, danger) = switch (state.connectionStatus) {
      WorkroomConnectionStatus.protocolError => (
        Icons.system_update_alt,
        l10n.connectionProtocolError,
        true,
      ),
      WorkroomConnectionStatus.authorizationLost => (
        Icons.lock_outline,
        l10n.connectionAuthorizationLost,
        true,
      ),
      WorkroomConnectionStatus.reconnecting => (
        Icons.sync,
        l10n.connectionReconnecting,
        false,
      ),
      _ => (Icons.downloading_outlined, l10n.connectionReplaying, false),
    };
    return Semantics(
      liveRegion: true,
      label: label,
      child: Container(
        key: const ValueKey('workroom-connection-state'),
        color: danger
            ? Theme.of(context).colorScheme.errorContainer
            : Theme.of(context).colorScheme.secondaryContainer,
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
        child: Row(
          children: [
            Icon(icon, size: 18),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                state.connectionMessage.isEmpty
                    ? label
                    : '$label · ${state.connectionMessage}',
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _InlineCallout extends StatelessWidget {
  const _InlineCallout({
    required this.icon,
    required this.message,
    required this.danger,
  });

  final IconData icon;
  final String message;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      color: danger ? scheme.errorContainer : scheme.secondaryContainer,
      padding: const EdgeInsets.all(10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 18),
          const SizedBox(width: 8),
          Expanded(child: Text(message)),
        ],
      ),
    );
  }
}
