import 'dart:async';

import 'package:flutter/material.dart';

import '../app_localizations.dart';
import '../dashboard_models.dart';
import '../hub/fixer_chat/fixer_chat_models.dart';
import '../shared/provider_model_reasoning_selector.dart';
import 'fixer_genui_tab.dart';
import 'workroom_models.dart';
import 'workroom_store.dart';

const _providerOrder = <String>['codex', 'commandcode', 'claude', 'kimi-code', 'antigravity'];

List<ProviderModelReasoningOption> _laneSelectorOptions(
  List<HandsProviderLane> lanes,
) {
  return [
    for (final lane in lanes)
      ProviderModelReasoningOption(
        id: lane.provider,
        label:
            supportedFixerProvidersByBackend[lane.provider]?.label ??
            lane.provider,
        models:
            supportedFixerProvidersByBackend[lane.provider]?.models ??
            [_laneModel(lane)],
        reasoningOptions:
            supportedFixerProvidersByBackend[lane.provider]?.reasoningOptions ??
            [_laneReasoning(lane)],
      ),
  ];
}

String _laneModel(HandsProviderLane lane) => lane.model.isEmpty
    ? supportedFixerProvidersByBackend[lane.provider]?.defaultModel ??
          lane.provider
    : lane.model;

String _laneReasoning(HandsProviderLane lane) => lane.reasoning.isEmpty
    ? supportedFixerProvidersByBackend[lane.provider]?.defaultReasoning ??
          'default'
    : lane.reasoning;

/// Builds the lane selector options constrained to a real Hands thread's
/// actual execution config. A running provider process is bound to the
/// (provider, model) it was launched with; it cannot be switched mid-thread to
/// another lane or to a different model of the same lane (e.g. a codex thread
/// launched with gpt models cannot become claude/agy or codex+deepseek). The
/// selector must therefore only ever offer the thread's real provider+model.
List<ProviderModelReasoningOption> _threadConstrainedSelectorOptions(
  WorkroomFixerThread thread,
) {
  final spec = supportedFixerProvidersByBackend[thread.provider];
  return [
    ProviderModelReasoningOption(
      id: thread.provider,
      label: spec?.label ?? thread.provider,
      models: [thread.model],
      reasoningOptions:
          spec?.reasoningOptions ?? [thread.reasoning.isNotEmpty ? thread.reasoning : 'default'],
    ),
  ];
}

/// True when a real Hands thread with a known provider+model is selected, so
/// the header selector must be locked to the thread's actual execution config.
bool _hasThreadExecutionConfig(WorkroomFixerThread? thread) =>
    thread != null && thread.provider.isNotEmpty && thread.model.isNotEmpty;

class HandsTab extends StatefulWidget {
  const HandsTab({
    super.key,
    required this.store,
    required this.loadThreads,
    this.loadHistoricalTurns,
    this.sendThreadMessage,
    this.sendThreadMessageWithConfig,
    this.loadThreadTurnStatus,
  });

  final ProjectWorkroomStore store;
  final Future<List<WorkroomFixerThread>> Function() loadThreads;
  final Future<List<WorkroomFixerTurn>> Function(String threadId)?
  loadHistoricalTurns;
  final Future<ThreadSendResult> Function(String threadId, String prompt)?
  sendThreadMessage;
  final Future<ThreadSendResult> Function(
    String threadId,
    String prompt, {
    required String model,
    required String reasoning,
  })?
  sendThreadMessageWithConfig;
  final Future<ThreadTurnStatusSnapshot> Function(String streamId)?
  loadThreadTurnStatus;

  @override
  State<HandsTab> createState() => _HandsTabState();
}

class _HandsTabState extends State<HandsTab> {
  final _messageController = TextEditingController();
  final _instructionController = TextEditingController();
  final _scopeController = TextEditingController();
  bool _submitting = false;
  bool _laneChanging = false;
  String _instructionAction = '';
  List<WorkroomFixerThread> _threads = const [];
  List<WorkroomFixerTurn> _turns = const [];
  String _selectedThreadId = '';
  bool _loadingConversation = true;
  bool _sendingMessage = false;
  Timer? _turnPollTimer;
  ThreadSendResult? _activeTurn;
  bool _showMailbox = false;
  ProviderModelReasoningSelection? _handsSelectionOverride;
  WorkroomTechnicalVisibility _technicalVisibility =
      WorkroomTechnicalVisibility.conversationOnly;

  @override
  void initState() {
    super.initState();
    _loadConversations();
  }

  @override
  void reassemble() {
    super.reassemble();
    _loadConversations();
  }

  @override
  void dispose() {
    _turnPollTimer?.cancel();
    _messageController.dispose();
    _instructionController.dispose();
    _scopeController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.store,
      builder: (context, _) {
        final snapshot = widget.store.state.snapshot;
        final hands = snapshot.hands;
        final lanes = _normalizedLanes(hands.lanes);
        final selectedThread = _threads
            .where((thread) => thread.id == _selectedThreadId)
            .firstOrNull;
        final conversationState = widget.store.state.copyWith(
          snapshot: snapshot.copyWith(
            threads: _threads,
            selectedThreadId: _selectedThreadId,
            turns: _turns,
          ),
        );
        return FocusTraversalGroup(
          policy: WidgetOrderTraversalPolicy(),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _HandsActorHeader(
                hands: hands,
                lanes: lanes,
                laneChanging: _laneChanging,
                canSelectLane: snapshot.capabilities.canSelectHandsLane,
                selectedThread: selectedThread,
                selectionOverride: _handsSelectionOverride,
                onSelectConfiguration: (selection) {
                  // A running thread is bound to its launch (provider, model);
                  // its header selector is constrained to the real config, so
                  // re-selecting it must never attempt a lane switch.
                  if (_hasThreadExecutionConfig(selectedThread)) {
                    setState(() => _handsSelectionOverride = selection);
                    return;
                  }
                  if (selection.provider != hands.selectedLane) {
                    _handsSelectionOverride = null;
                    _selectLane(selection.provider);
                    return;
                  }
                  setState(() => _handsSelectionOverride = selection);
                },
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
                child: SegmentedButton<bool>(
                  segments: const [
                    ButtonSegment(
                      value: false,
                      icon: Icon(Icons.chat_bubble_outline, size: 18),
                      label: Text('Conversation'),
                    ),
                    ButtonSegment(
                      value: true,
                      icon: Icon(Icons.inbox_outlined, size: 18),
                      label: Text('Mailbox & history'),
                    ),
                  ],
                  selected: {_showMailbox},
                  onSelectionChanged: (value) {
                    setState(() => _showMailbox = value.first);
                  },
                ),
              ),
              const Divider(height: 1),
              Expanded(
                child: _showMailbox
                    ? LayoutBuilder(
                        builder: (context, constraints) {
                          final mailbox = _HandsMailbox(
                            hands: hands,
                            onSelect: widget.store.selectHandsInstruction,
                          );
                          final detail = _HandsInstructionDetail(
                            instruction: hands.selectedInstruction,
                            actionInProgress: _instructionAction,
                            canCancel:
                                snapshot.capabilities.canCancelHandsInstruction,
                            canReview:
                                snapshot.capabilities.canReviewHandsInstruction,
                            onCancel: _cancelInstruction,
                            onReview: _reviewInstruction,
                          );
                          if (constraints.maxWidth >= 1000) {
                            return Row(
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: [
                                SizedBox(width: 360, child: mailbox),
                                const VerticalDivider(width: 1),
                                Expanded(child: detail),
                              ],
                            );
                          }
                          return Column(
                            children: [
                              SizedBox(height: 220, child: mailbox),
                              const Divider(height: 1),
                              Expanded(child: detail),
                            ],
                          );
                        },
                      )
                    : _loadingConversation
                    ? const Center(child: CircularProgressIndicator())
                    : WorkroomConversationPane(
                        state: conversationState,
                        composer: _messageController,
                        sending: _sendingMessage,
                        onThreadSelected: _selectConversation,
                        onSend: _sendMessage,
                        technicalVisibility: _technicalVisibility,
                        onTechnicalVisibilityChanged: (value) {
                          setState(() => _technicalVisibility = value);
                        },
                        compact: true,
                        title: 'Hands conversation',
                        selectorLabel: 'Choose Hands thread',
                        emptyLabel: 'No activated Hands threads yet.',
                        composerHint: 'Message these Hands…',
                        canSend:
                            widget.sendThreadMessage != null &&
                            _selectedThreadId.isNotEmpty,
                        keyPrefix: 'hands',
                      ),
              ),
              if (_showMailbox) ...[
                const Divider(height: 1),
                _HandsComposer(
                  instructionController: _instructionController,
                  scopeController: _scopeController,
                  selectedLane: hands.selectedLane,
                  lanes: lanes,
                  submitting: _submitting,
                  canSubmit: snapshot.capabilities.canSubmitHandsInstruction,
                  onSubmit: _submitInstruction,
                  initialSelection: _handsSelectionOverride,
                ),
              ],
            ],
          ),
        );
      },
    );
  }

  Future<void> _loadConversations() async {
    try {
      final threads = await widget.loadThreads();
      if (!mounted) return;
      final selected = threads.any((thread) => thread.id == _selectedThreadId)
          ? _selectedThreadId
          : threads.isEmpty
          ? ''
          : threads.first.id;
      setState(() {
        _threads = threads;
        _selectedThreadId = selected;
        _loadingConversation = false;
      });
      if (selected.isNotEmpty) await _loadConversation(selected);
    } on Object catch (error) {
      if (!mounted) return;
      setState(() => _loadingConversation = false);
      _showNotice(error.toString());
    }
  }

  Future<void> _selectConversation(String threadId) async {
    _stopTurnPolling();
    setState(() => _selectedThreadId = threadId);
    await _loadConversation(threadId);
  }

  Future<void> _loadConversation(String threadId) async {
    final loader = widget.loadHistoricalTurns;
    if (loader == null) return;
    final turns = await loader(threadId);
    if (!mounted || threadId != _selectedThreadId) return;
    setState(() => _turns = turns);
  }

  Future<void> _sendMessage() async {
    final send = widget.sendThreadMessage;
    final message = _messageController.text.trim();
    if (send == null ||
        message.isEmpty ||
        _selectedThreadId.isEmpty ||
        _sendingMessage) {
      return;
    }
    setState(() => _sendingMessage = true);
    try {
      final threadId = _selectedThreadId;
      final optimistic = WorkroomFixerTurn(
        id: 'pending-${DateTime.now().microsecondsSinceEpoch}',
        threadId: threadId,
        ordinal: _turns.length + 1,
        role: 'user',
        content: message,
        source: 'app',
        status: 'active',
        createdAt: DateTime.now().toUtc().toIso8601String(),
      );
      setState(() => _turns = [..._turns, optimistic]);
      _messageController.clear();
      final result = widget.sendThreadMessageWithConfig == null
          ? await send(threadId, message)
          : await widget.sendThreadMessageWithConfig!(
              threadId,
              message,
              model: _handsSelectionOverride?.model ?? '',
              reasoning: _handsSelectionOverride?.reasoning ?? '',
            );
      if (!mounted) return;
      _activeTurn = result;
      await _pollTurnStatus();
      if (mounted && _activeTurn?.streamId == result.streamId) {
        _turnPollTimer = Timer.periodic(
          const Duration(milliseconds: 850),
          (_) => _pollTurnStatus(),
        );
      }
    } on Object catch (error) {
      _showNotice(error.toString());
    } finally {
      if (mounted) setState(() => _sendingMessage = false);
    }
  }

  Future<void> _pollTurnStatus() async {
    final loadStatus = widget.loadThreadTurnStatus;
    final active = _activeTurn;
    if (loadStatus == null || active == null || active.streamId.isEmpty) {
      if (active != null) await _loadConversation(active.threadId);
      return;
    }
    try {
      final status = await loadStatus(active.streamId);
      if (!mounted || _activeTurn?.streamId != active.streamId) return;
      final liveId = 'live-${active.turnId}';
      final withoutLive = _turns.where((turn) => turn.id != liveId).toList();
      if (status.assistantText.trim().isNotEmpty) {
        withoutLive.add(
          WorkroomFixerTurn(
            id: liveId,
            threadId: active.threadId,
            ordinal: withoutLive.length + 1,
            role: 'assistant',
            content: status.assistantText,
            source: 'codex_app_server',
            status: status.done ? 'complete' : 'active',
            createdAt: status.startedAt,
          ),
        );
      }
      setState(() => _turns = withoutLive);
      if (status.done || status.expired) {
        _stopTurnPolling();
        _activeTurn = null;
        await _loadConversation(active.threadId);
      }
    } on Object catch (error) {
      if (mounted) _showNotice(error.toString());
    }
  }

  void _stopTurnPolling() {
    _turnPollTimer?.cancel();
    _turnPollTimer = null;
    _activeTurn = null;
  }

  Future<void> _submitInstruction(
    ProviderModelReasoningSelection selection,
  ) async {
    final l10n = AppLocalizations.of(context);
    final instruction = _instructionController.text.trim();
    if (instruction.isEmpty || _submitting) return;
    final scope = _scopeController.text
        .split('\n')
        .map((path) => path.trim())
        .where((path) => path.isNotEmpty)
        .toList(growable: false);
    final invalidPath = scope.where(
      (path) =>
          path.startsWith('/') ||
          path == '..' ||
          path.startsWith('../') ||
          path.contains('/../'),
    );
    if (invalidPath.isNotEmpty) {
      _showNotice(
        l10n.isRussian
            ? 'Область записи должна содержать только пути внутри проекта.'
            : 'Write scope must contain project-relative paths only.',
      );
      return;
    }
    if (scope.isNotEmpty) {
      final confirmed = await _confirm(
        title: l10n.governedAction,
        message: l10n.isRussian
            ? 'Это поручение запрашивает запись в репозиторий. '
                  'Продолжить с обязательной проверкой результата?'
            : 'This instruction requests repository writes. '
                  'Continue with governed result review?',
        confirmLabel: l10n.continueAction,
      );
      if (!confirmed || !mounted) return;
    }
    setState(() => _submitting = true);
    try {
      await widget.store.submitHandsInstruction(
        instructionText: instruction,
        declaredWriteScope: scope,
        requestedLane: selection.provider,
        requestedModel: selection.model,
        requestedReasoning: selection.reasoning,
      );
      if (!mounted) return;
      _instructionController.clear();
      _scopeController.clear();
      _showNotice(l10n.handsSubmitted);
    } on Object catch (error) {
      _showNotice(error.toString());
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  Future<void> _selectLane(String provider) async {
    if (_laneChanging ||
        provider == widget.store.state.snapshot.hands.selectedLane) {
      return;
    }
    final l10n = AppLocalizations.of(context);
    final confirmed = await _confirm(
      title: l10n.handsSelectLane,
      message: l10n.isRussian
          ? 'Сделать $provider линией исполнения для следующих поручений?'
          : 'Use $provider for subsequent instructions?',
      confirmLabel: l10n.continueAction,
    );
    if (!confirmed || !mounted) return;
    setState(() => _laneChanging = true);
    try {
      final receipt = await widget.store.selectHandsLane(provider);
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isNotEmpty ? receipt.message : receipt.reasonCode,
        );
      }
    } on Object catch (error) {
      _showNotice(error.toString());
    } finally {
      if (mounted) setState(() => _laneChanging = false);
    }
  }

  Future<void> _cancelInstruction(HandsInstruction instruction) async {
    final l10n = AppLocalizations.of(context);
    final confirmed = await _confirm(
      title: l10n.handsCancelInstruction,
      message: l10n.isRussian
          ? 'Отменить поручение №${instruction.ordinal}? '
                'Запущенный процесс получит управляемый сигнал остановки.'
          : 'Cancel instruction #${instruction.ordinal}? '
                'A running process will receive a governed stop request.',
      confirmLabel: l10n.handsCancelInstruction,
    );
    if (!confirmed || !mounted) return;
    setState(() => _instructionAction = 'cancel');
    try {
      final receipt = await widget.store.cancelHandsInstruction(instruction.id);
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isNotEmpty ? receipt.message : receipt.reasonCode,
        );
      }
    } on Object catch (error) {
      _showNotice(error.toString());
    } finally {
      if (mounted) setState(() => _instructionAction = '');
    }
  }

  Future<void> _reviewInstruction(
    HandsInstruction instruction,
    String decision,
  ) async {
    final note = await _reviewNote(decision);
    if (note == null || !mounted) return;
    setState(() => _instructionAction = decision);
    try {
      final receipt = await widget.store.reviewHandsInstruction(
        instructionId: instruction.id,
        decision: decision,
        reviewNote: note,
      );
      if (_receiptFailed(receipt)) {
        throw StateError(
          receipt.message.isNotEmpty ? receipt.message : receipt.reasonCode,
        );
      }
      if (receipt.message.isNotEmpty) _showNotice(receipt.message);
    } on Object catch (error) {
      _showNotice(error.toString());
    } finally {
      if (mounted) setState(() => _instructionAction = '');
    }
  }

  Future<String?> _reviewNote(String decision) async {
    final l10n = AppLocalizations.of(context);
    final controller = TextEditingController();
    final result = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(
          decision == 'accept'
              ? l10n.handsAcceptResult
              : l10n.handsRequestChanges,
        ),
        content: TextField(
          key: const ValueKey('hands-review-note'),
          controller: controller,
          minLines: 3,
          maxLines: 8,
          decoration: InputDecoration(labelText: l10n.handsReviewNote),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text(l10n.cancel),
          ),
          FilledButton(
            key: const ValueKey('confirm-hands-review'),
            onPressed: () => Navigator.of(context).pop(controller.text.trim()),
            child: Text(l10n.continueAction),
          ),
        ],
      ),
    );
    controller.dispose();
    return result;
  }

  Future<bool> _confirm({
    required String title,
    required String message,
    required String confirmLabel,
  }) async {
    final l10n = AppLocalizations.of(context);
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(title),
            content: Text(message),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: Text(l10n.cancel),
              ),
              FilledButton(
                key: const ValueKey('confirm-hands-action'),
                onPressed: () => Navigator.of(context).pop(true),
                child: Text(confirmLabel),
              ),
            ],
          ),
        ) ??
        false;
  }

  bool _receiptFailed(GenUiActionReceipt receipt) {
    return const {'denied', 'failed', 'unsupported'}.contains(receipt.status);
  }

  List<HandsProviderLane> _normalizedLanes(List<HandsProviderLane> source) {
    final byProvider = <String, HandsProviderLane>{
      for (final lane in source)
        (lane.provider == 'kimi'
            ? 'kimi-code'
            : lane.provider): lane.provider == 'kimi'
            ? HandsProviderLane(
                provider: 'kimi-code',
                model: lane.model,
                reasoning: lane.reasoning,
              )
            : lane,
    };
    return [
      for (final provider in _providerOrder)
        byProvider[provider] ??
            HandsProviderLane(
              provider: provider,
              model:
                  supportedFixerProvidersByBackend[provider]?.defaultModel ??
                  '',
              reasoning:
                  supportedFixerProvidersByBackend[provider]
                      ?.defaultReasoning ??
                  '',
            ),
    ];
  }

  void _showNotice(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }
}

class _HandsActorHeader extends StatelessWidget {
  const _HandsActorHeader({
    required this.hands,
    required this.lanes,
    required this.laneChanging,
    required this.canSelectLane,
    required this.selectedThread,
    required this.selectionOverride,
    required this.onSelectConfiguration,
  });

  final WorkroomHandsState hands;
  final List<HandsProviderLane> lanes;
  final bool laneChanging;
  final bool canSelectLane;
  final WorkroomFixerThread? selectedThread;
  final ProviderModelReasoningSelection? selectionOverride;
  final ValueChanged<ProviderModelReasoningSelection> onSelectConfiguration;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final selectedLane = lanes.firstWhere(
      (lane) => lane.provider == hands.selectedLane,
      orElse: () => lanes.first,
    );
    final lockedThread = _hasThreadExecutionConfig(selectedThread)
        ? selectedThread
        : null;
    // A running thread's (provider, model) are fixed at launch; the display
    // must always reflect the thread's real config and never a stale override
    // (the cascade widget emits its initial selection before the thread list
    // loads, which must not mask the real execution config). Only the reasoning
    // knob stays adjustable per message.
    final displayedModel = lockedThread != null
        ? lockedThread.model
        : selectionOverride?.model.isNotEmpty == true
        ? selectionOverride!.model
        : selectedThread?.model.isNotEmpty == true
        ? selectedThread!.model
        : selectedLane.model;
    final displayedReasoning = selectionOverride?.reasoning.isNotEmpty == true
        ? selectionOverride!.reasoning
        : lockedThread?.reasoning.isNotEmpty == true
        ? lockedThread!.reasoning
        : selectedThread?.reasoning.isNotEmpty == true
        ? selectedThread!.reasoning
        : selectedLane.reasoning;
    final selectedProvider = lockedThread?.provider ?? selectedLane.provider;
    return Semantics(
      container: true,
      label:
          '${hands.displayName}. ${_stateLabel(l10n, hands.operationalState)}',
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 14, 18, 12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            CircleAvatar(
              radius: 23,
              backgroundColor: Theme.of(context).colorScheme.primaryContainer,
              child: Icon(
                Icons.back_hand_outlined,
                color: Theme.of(context).colorScheme.onPrimaryContainer,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Wrap(
                    spacing: 8,
                    runSpacing: 6,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      Text(
                        hands.displayName,
                        style: Theme.of(context).textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                      _HandsStatusBadge(
                        state: hands.operationalState,
                        label: _stateLabel(l10n, hands.operationalState),
                      ),
                      Chip(
                        avatar: const Icon(Icons.inbox_outlined, size: 16),
                        label: Text('${l10n.handsQueue}: ${hands.queueDepth}'),
                      ),
                    ],
                  ),
                  const SizedBox(height: 2),
                  Text(
                    '${l10n.handsPermanentActor} · '
                    '${hands.actorId.isEmpty ? l10n.unknownValue : hands.actorId}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                  if (hands.activeLeaseSummary.isNotEmpty)
                    Text(
                      '${l10n.handsLease}: ${hands.activeLeaseSummary}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            ProviderModelReasoningSelector(
              key: const ValueKey('hands-provider-lane-selector'),
              width: 380,
              height: 56,
              hintText: l10n.handsLane,
              enabled: !laneChanging && canSelectLane,
              providers: lockedThread != null
                  ? _threadConstrainedSelectorOptions(lockedThread)
                  : _laneSelectorOptions([
                      for (final lane in lanes)
                        lane.provider == selectedLane.provider
                            ? HandsProviderLane(
                                provider: lane.provider,
                                model: displayedModel,
                                reasoning: displayedReasoning,
                              )
                            : lane,
                    ]),
              selectedProvider: selectedProvider,
              selectedModel: displayedModel,
              selectedReasoning: displayedReasoning,
              onSelected: (selection) {
                if (laneChanging || !canSelectLane) {
                  return;
                }
                onSelectConfiguration(selection);
              },
            ),
          ],
        ),
      ),
    );
  }

  String _stateLabel(AppLocalizations l10n, String state) {
    return switch (state) {
      'running' => l10n.handsRunning,
      'queued' || 'waiting_for_lease' => l10n.handsBusy,
      'awaiting_review' => l10n.handsAwaitingReview,
      'failed' || 'error' || 'abandoned' => l10n.handsError,
      _ => l10n.handsIdle,
    };
  }
}

class _HandsMailbox extends StatelessWidget {
  const _HandsMailbox({required this.hands, required this.onSelect});

  final WorkroomHandsState hands;
  final ValueChanged<String> onSelect;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
          child: Text(
            l10n.handsMailbox,
            style: Theme.of(
              context,
            ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w800),
          ),
        ),
        Expanded(
          child: hands.instructions.isEmpty
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(20),
                    child: Text(
                      l10n.handsNoInstructions,
                      textAlign: TextAlign.center,
                    ),
                  ),
                )
              : ListView.builder(
                  key: const ValueKey('hands-mailbox-list'),
                  padding: const EdgeInsets.fromLTRB(8, 0, 8, 12),
                  itemCount: hands.instructions.length,
                  itemBuilder: (context, index) {
                    final instruction = hands.instructions[index];
                    final selected =
                        hands.selectedInstruction?.id == instruction.id;
                    return Card(
                      color: selected
                          ? Theme.of(context).colorScheme.secondaryContainer
                          : null,
                      child: ListTile(
                        key: ValueKey('hands-instruction-${instruction.id}'),
                        selected: selected,
                        title: Text(
                          '#${instruction.ordinal} '
                          '${instruction.instructionText}',
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          [
                            '${instruction.requestedLane} · '
                                '${workroomRelativeAge(instruction.updatedAt)}',
                          ].join('\n'),
                          maxLines: 4,
                          overflow: TextOverflow.ellipsis,
                        ),
                        trailing: Icon(_instructionIcon(instruction.state)),
                        onTap: () => onSelect(instruction.id),
                      ),
                    );
                  },
                ),
        ),
      ],
    );
  }
}

class _HandsInstructionDetail extends StatelessWidget {
  const _HandsInstructionDetail({
    required this.instruction,
    required this.actionInProgress,
    required this.canCancel,
    required this.canReview,
    required this.onCancel,
    required this.onReview,
  });

  final HandsInstruction? instruction;
  final String actionInProgress;
  final bool canCancel;
  final bool canReview;
  final ValueChanged<HandsInstruction> onCancel;
  final void Function(HandsInstruction instruction, String decision) onReview;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final value = instruction;
    if (value == null) {
      return Center(child: Text(l10n.handsNoInstructions));
    }
    return ListView(
      key: ValueKey('hands-instruction-detail-${value.id}'),
      padding: const EdgeInsets.all(18),
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${l10n.handsInstruction} #${value.ordinal}',
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w900,
                    ),
                  ),
                  Text(
                    '${value.requestedLane} · ${value.state} · '
                    'generation ${value.generation}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            _HandsStatusBadge(state: value.state, label: value.state),
          ],
        ),
        const SizedBox(height: 14),
        SelectableText(value.instructionText),
        if (value.declaredWriteScope.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(
            l10n.handsWriteScope,
            style: Theme.of(context).textTheme.titleSmall,
          ),
          SelectableText(value.declaredWriteScope.join('\n')),
        ],
        if (value.stateReasonText.isNotEmpty) ...[
          const SizedBox(height: 12),
          _DetailCallout(
            title: value.stateReasonCode.isEmpty
                ? value.state
                : value.stateReasonCode,
            body: value.stateReasonText,
            danger: const {
              'failed',
              'unsupported',
              'abandoned',
            }.contains(value.state),
          ),
        ],
        if (value.events.isNotEmpty) ...[
          const SizedBox(height: 18),
          Text(
            l10n.handsTimeline,
            style: Theme.of(
              context,
            ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w800),
          ),
          const SizedBox(height: 8),
          for (final event in value.events)
            ListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.circle, size: 10),
              title: Text(event.eventType),
              subtitle: Text(
                [
                  if (event.toState.isNotEmpty) event.toState,
                  if (event.detail.isNotEmpty) event.detail,
                  if (event.createdAt.isNotEmpty) event.createdAt,
                ].join(' · '),
              ),
            ),
        ],
        if (value.report.isNotEmpty) ...[
          const SizedBox(height: 18),
          _EvidenceCard(title: l10n.handsResult, content: value.report),
        ],
        if (value.repositoryDiff.isNotEmpty) ...[
          const SizedBox(height: 12),
          _EvidenceCard(
            title: l10n.handsRepositoryDiff,
            content: value.repositoryDiff,
            monospace: true,
          ),
        ],
        if (value.reviewReference.isNotEmpty) ...[
          const SizedBox(height: 12),
          _EvidenceCard(
            title: l10n.isRussian ? 'Ссылка проверки' : 'Review reference',
            content: value.reviewReference,
            monospace: true,
          ),
        ],
        if (value.canCancel && canCancel) ...[
          const SizedBox(height: 18),
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: OutlinedButton.icon(
              key: const ValueKey('cancel-hands-instruction'),
              onPressed: actionInProgress.isEmpty
                  ? () => onCancel(value)
                  : null,
              icon: const Icon(Icons.stop_circle_outlined),
              label: Text(l10n.handsCancelInstruction),
            ),
          ),
        ],
        if (value.awaitsReview && canReview) ...[
          const SizedBox(height: 18),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              FilledButton.icon(
                key: const ValueKey('accept-hands-result'),
                onPressed: actionInProgress.isEmpty
                    ? () => onReview(value, 'accept')
                    : null,
                icon: const Icon(Icons.check),
                label: Text(l10n.handsAcceptResult),
              ),
              OutlinedButton.icon(
                key: const ValueKey('request-hands-changes'),
                onPressed: actionInProgress.isEmpty
                    ? () => onReview(value, 'request_changes')
                    : null,
                icon: const Icon(Icons.replay_outlined),
                label: Text(l10n.handsRequestChanges),
              ),
            ],
          ),
        ],
      ],
    );
  }
}

class _HandsComposer extends StatefulWidget {
  const _HandsComposer({
    required this.instructionController,
    required this.scopeController,
    required this.selectedLane,
    required this.lanes,
    required this.submitting,
    required this.canSubmit,
    required this.onSubmit,
    this.initialSelection,
  });

  final TextEditingController instructionController;
  final TextEditingController scopeController;
  final String selectedLane;
  final List<HandsProviderLane> lanes;
  final bool submitting;
  final bool canSubmit;
  final ValueChanged<ProviderModelReasoningSelection> onSubmit;
  final ProviderModelReasoningSelection? initialSelection;

  @override
  State<_HandsComposer> createState() => _HandsComposerState();
}

class _HandsComposerState extends State<_HandsComposer> {
  bool _advanced = false;
  late String _requestedLane;
  late String _requestedModel;
  late String _requestedReasoning;

  @override
  void initState() {
    super.initState();
    final selection = widget.initialSelection;
    if (selection == null) {
      _selectRequestedLane(widget.selectedLane);
    } else {
      _requestedLane = selection.provider;
      _requestedModel = selection.model;
      _requestedReasoning = selection.reasoning;
    }
  }

  @override
  void didUpdateWidget(_HandsComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!_advanced &&
        (oldWidget.initialSelection != widget.initialSelection ||
            oldWidget.selectedLane != widget.selectedLane ||
            oldWidget.lanes != widget.lanes)) {
      final selection = widget.initialSelection;
      if (selection == null) {
        _selectRequestedLane(widget.selectedLane);
      } else {
        _requestedLane = selection.provider;
        _requestedModel = selection.model;
        _requestedReasoning = selection.reasoning;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLowest,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: TextField(
                    key: const ValueKey('hands-instruction-composer'),
                    controller: widget.instructionController,
                    minLines: 1,
                    maxLines: 4,
                    decoration: InputDecoration(
                      labelText: l10n.handsInstructionComposer,
                      hintText: l10n.handsInstructionHint,
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                FilledButton.icon(
                  key: const ValueKey('submit-hands-instruction'),
                  onPressed: widget.submitting || !widget.canSubmit
                      ? null
                      : () => widget.onSubmit(
                          ProviderModelReasoningSelection(
                            provider: _requestedLane,
                            model: _requestedModel,
                            reasoning: _requestedReasoning,
                          ),
                        ),
                  icon: widget.submitting
                      ? const SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.back_hand_outlined, size: 18),
                  label: Text(l10n.handsSubmit),
                ),
              ],
            ),
            Align(
              alignment: AlignmentDirectional.centerStart,
              child: TextButton.icon(
                key: const ValueKey('hands-advanced-toggle'),
                onPressed: widget.canSubmit
                    ? () => setState(() => _advanced = !_advanced)
                    : null,
                icon: Icon(
                  _advanced ? Icons.expand_less : Icons.tune_outlined,
                  size: 18,
                ),
                label: Text(l10n.handsAdvanced),
              ),
            ),
            if (_advanced)
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  ProviderModelReasoningSelector(
                    key: const ValueKey('hands-requested-lane'),
                    width: 380,
                    height: 56,
                    hintText: l10n.handsLane,
                    enabled: widget.canSubmit,
                    providers: _laneSelectorOptions(widget.lanes),
                    selectedProvider: _requestedLane,
                    selectedModel: _requestedModel,
                    selectedReasoning: _requestedReasoning,
                    onSelected: (selection) {
                      if (!widget.canSubmit ||
                          (_requestedLane == selection.provider &&
                              _requestedModel == selection.model &&
                              _requestedReasoning == selection.reasoning)) {
                        return;
                      }
                      setState(() {
                        _requestedLane = selection.provider;
                        _requestedModel = selection.model;
                        _requestedReasoning = selection.reasoning;
                      });
                    },
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: TextField(
                      key: const ValueKey('hands-write-scope'),
                      controller: widget.scopeController,
                      minLines: 1,
                      maxLines: 3,
                      style: const TextStyle(fontFamily: 'monospace'),
                      decoration: InputDecoration(
                        labelText: l10n.handsWriteScope,
                        hintText: l10n.handsWriteScopeHint,
                        isDense: true,
                      ),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }

  void _selectRequestedLane(String provider) {
    final lane = widget.lanes.firstWhere(
      (lane) => lane.provider == provider,
      orElse: () => widget.lanes.first,
    );
    _requestedLane = lane.provider;
    _requestedModel = _laneModel(lane);
    _requestedReasoning = _laneReasoning(lane);
  }
}

class _HandsStatusBadge extends StatelessWidget {
  const _HandsStatusBadge({required this.state, required this.label});

  final String state;
  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final danger = const {
      'failed',
      'error',
      'abandoned',
      'unsupported',
    }.contains(state);
    final success = state == 'completed';
    final background = danger
        ? scheme.errorContainer
        : success
        ? const Color(0xFFE9F7EF)
        : scheme.secondaryContainer;
    final foreground = danger
        ? scheme.onErrorContainer
        : success
        ? const Color(0xFF155B33)
        : scheme.onSecondaryContainer;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
          color: foreground,
          fontWeight: FontWeight.w800,
        ),
      ),
    );
  }
}

class _DetailCallout extends StatelessWidget {
  const _DetailCallout({
    required this.title,
    required this.body,
    required this.danger,
  });

  final String title;
  final String body;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: danger ? scheme.errorContainer : scheme.secondaryContainer,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 4),
          Text(body),
        ],
      ),
    );
  }
}

class _EvidenceCard extends StatelessWidget {
  const _EvidenceCard({
    required this.title,
    required this.content,
    this.monospace = false,
  });

  final String title;
  final String content;
  final bool monospace;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 8),
            SelectableText(
              content,
              style: monospace
                  ? const TextStyle(fontFamily: 'monospace')
                  : null,
            ),
          ],
        ),
      ),
    );
  }
}

IconData _instructionIcon(String state) {
  return switch (state) {
    'completed' => Icons.check_circle_outline,
    'running' || 'starting' => Icons.play_circle_outline,
    'awaiting_review' => Icons.rate_review_outlined,
    'failed' || 'abandoned' || 'unsupported' => Icons.error_outline,
    'waiting_for_lease' => Icons.lock_clock_outlined,
    _ => Icons.schedule_outlined,
  };
}
