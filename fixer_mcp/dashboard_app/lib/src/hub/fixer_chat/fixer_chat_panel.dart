import 'package:flutter/material.dart';

import '../../shared/provider_model_reasoning_selector.dart';
import 'fixer_chat_models.dart';
import 'fixer_chat_service.dart';

class FixerChatPanel extends StatefulWidget {
  const FixerChatPanel({
    required this.projectId,
    required this.projectCwd,
    required this.service,
    this.providers = supportedFixerProviders,
    super.key,
  });

  final int projectId;
  final String projectCwd;
  final FixerChatService service;
  final List<FixerProviderOption> providers;

  @override
  State<FixerChatPanel> createState() => _FixerChatPanelState();
}

class _FixerChatPanelState extends State<FixerChatPanel> {
  List<FixerThreadRecord> _threads = const [];
  bool _loading = true;
  bool _launching = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final threads = await widget.service.loadFixerThreads(widget.projectId);
      if (!mounted) return;
      setState(() {
        _threads = threads;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = error.toString();
      });
    }
  }

  Future<void> _createChat() async {
    final request = await showDialog<FixerChatLaunchRequest>(
      context: context,
      builder: (context) => CreateFixerChatDialog(
        cwd: widget.projectCwd,
        providers: widget.providers,
      ),
    );
    if (request == null || !mounted) return;

    setState(() {
      _launching = true;
      _error = null;
    });
    try {
      await widget.service.createFixerChat(widget.projectId, request);
      await _refresh();
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _launching = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          alignment: WrapAlignment.spaceBetween,
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: 16,
          runSpacing: 12,
          children: [
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Fixer Chat', style: theme.textTheme.headlineSmall),
                const SizedBox(height: 4),
                Text(
                  'Threads from every supported CLI provider',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            FilledButton.icon(
              key: const Key('create-fixer-chat'),
              onPressed: _launching || widget.providers.isEmpty
                  ? null
                  : _createChat,
              icon: _launching
                  ? const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.add_comment_outlined),
              label: Text(_launching ? 'Launching…' : 'Create new Fixer chat'),
            ),
          ],
        ),
        if (_error != null) ...[
          const SizedBox(height: 12),
          MaterialBanner(
            content: Text(_error!),
            actions: [
              TextButton(onPressed: _refresh, child: const Text('Retry')),
            ],
          ),
        ],
        const SizedBox(height: 16),
        Expanded(child: _buildBody()),
      ],
    );
  }

  Widget _buildBody() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_threads.isEmpty) {
      return const Center(
        child: Text('No Fixer threads yet. Create the first one.'),
      );
    }
    return ListView.separated(
      itemCount: _threads.length,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) =>
          _FixerThreadCard(thread: _threads[index]),
    );
  }
}

class _FixerThreadCard extends StatelessWidget {
  const _FixerThreadCard({required this.thread});

  final FixerThreadRecord thread;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final providerDetails = [
      thread.backend,
      if (thread.model.isNotEmpty) thread.model,
      if (thread.reasoning.isNotEmpty) thread.reasoning,
    ].join(' · ');
    return Card.outlined(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    thread.headline,
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                _StatusChip(status: thread.status),
              ],
            ),
            const SizedBox(height: 8),
            Text(providerDetails, key: Key('provider-${thread.externalId}')),
            const SizedBox(height: 4),
            SelectableText(
              thread.cwd,
              key: Key('cwd-${thread.externalId}'),
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              'Session ${thread.externalId}',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.status});

  final String status;

  @override
  Widget build(BuildContext context) {
    return Chip(visualDensity: VisualDensity.compact, label: Text(status));
  }
}

class CreateFixerChatDialog extends StatefulWidget {
  const CreateFixerChatDialog({
    required this.cwd,
    required this.providers,
    super.key,
  });

  final String cwd;
  final List<FixerProviderOption> providers;

  @override
  State<CreateFixerChatDialog> createState() => _CreateFixerChatDialogState();
}

class _CreateFixerChatDialogState extends State<CreateFixerChatDialog> {
  late FixerProviderOption _provider;
  late String _model;
  late String _reasoning;

  @override
  void initState() {
    super.initState();
    _selectProvider(widget.providers.first);
  }

  void _selectProvider(FixerProviderOption provider) {
    _provider = provider;
    _model = provider.defaultModel;
    _reasoning = provider.defaultReasoning;
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Create new Fixer chat'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ProviderModelReasoningSelector(
                key: const ValueKey('fixer-provider-select'),
                providers: [
                  for (final provider in widget.providers)
                    ProviderModelReasoningOption(
                      id: provider.backend,
                      label: provider.label,
                      models: provider.models,
                      reasoningOptions: provider.reasoningOptions,
                    ),
                ],
                selectedProvider: _provider.backend,
                selectedModel: _model,
                selectedReasoning: _reasoning,
                onSelected: (selection) {
                  final provider = widget.providers.firstWhere(
                    (item) => item.backend == selection.provider,
                    orElse: () => widget.providers.first,
                  );
                  if (_provider.backend == provider.backend &&
                      _model == selection.model &&
                      _reasoning == selection.reasoning) {
                    return;
                  }
                  setState(() {
                    _provider = provider;
                    _model = selection.model;
                    _reasoning = selection.reasoning;
                  });
                },
                width: 520,
                height: 56,
              ),
              const SizedBox(height: 12),
              Text('CWD', style: Theme.of(context).textTheme.labelMedium),
              const SizedBox(height: 4),
              SelectableText(widget.cwd),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('launch-fixer-chat'),
          onPressed: () => Navigator.of(context).pop(
            FixerChatLaunchRequest(
              backend: _provider.backend,
              model: _model,
              reasoning: _reasoning,
              cwd: widget.cwd,
            ),
          ),
          child: const Text('Launch Fixer'),
        ),
      ],
    );
  }
}
