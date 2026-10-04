import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import 'overseer_models.dart';
import 'overseer_repository.dart';

const _border = Color(0xFFD9E0EC);
const _muted = Color(0xFF68738A);
const _surfaceMuted = Color(0xFFF2F5FA);

class OverseerManager extends StatefulWidget {
  const OverseerManager({super.key, required this.repository});

  final OverseerManagerRepository repository;

  @override
  State<OverseerManager> createState() => _OverseerManagerState();
}

class _OverseerManagerState extends State<OverseerManager> {
  late Future<List<OverseerThreadRecord>> _threads;
  final _busyThreads = <String>{};

  @override
  void initState() {
    super.initState();
    _threads = widget.repository.loadThreads();
  }

  @override
  void didUpdateWidget(covariant OverseerManager oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.repository, widget.repository)) {
      _threads = widget.repository.loadThreads();
    }
  }

  void _reload() {
    setState(() => _threads = widget.repository.loadThreads());
  }

  Future<void> _create() async {
    final request = await showDialog<OverseerLaunchRequest>(
      context: context,
      builder: (context) => const _CreateOverseerDialog(),
    );
    if (request == null || !mounted) return;
    await _runLaunch(() => widget.repository.createOverseer(request));
  }

  Future<void> _resume(OverseerThreadRecord thread) async {
    final key = '${thread.backend}:${thread.externalSessionId}';
    setState(() => _busyThreads.add(key));
    try {
      await _runLaunch(() => widget.repository.resumeOverseer(thread));
    } finally {
      if (mounted) setState(() => _busyThreads.remove(key));
    }
  }

  Future<void> _runLaunch(
    Future<OverseerLaunchPlanRecord> Function() launch,
  ) async {
    try {
      final plan = await launch();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            '${plan.mode == 'resume' ? 'Resume' : 'Create'} prepared on ${plan.backend}',
          ),
        ),
      );
      _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Overseer launch failed: $error')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Wrap(
            spacing: 16,
            runSpacing: 12,
            alignment: WrapAlignment.spaceBetween,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              const Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Overseers',
                    style: TextStyle(fontSize: 24, fontWeight: FontWeight.w800),
                  ),
                  SizedBox(height: 4),
                  Text(
                    'Durable threads across every supported CLI provider.',
                    style: TextStyle(color: _muted),
                  ),
                ],
              ),
              FilledButton.icon(
                key: const Key('create-overseer'),
                onPressed: _create,
                icon: const Icon(Icons.add, size: 18),
                label: const Text('Create Overseer'),
              ),
            ],
          ),
          const SizedBox(height: 18),
          Expanded(
            child: FutureBuilder<List<OverseerThreadRecord>>(
              future: _threads,
              builder: (context, snapshot) {
                if (snapshot.connectionState != ConnectionState.done) {
                  return const Center(child: CircularProgressIndicator());
                }
                if (snapshot.hasError) {
                  return _MessageState(
                    icon: Icons.error_outline,
                    title: 'Could not load Overseer history',
                    detail: snapshot.error.toString(),
                    action: TextButton(
                      onPressed: _reload,
                      child: const Text('Retry'),
                    ),
                  );
                }
                final threads = snapshot.data ?? const [];
                if (threads.isEmpty) {
                  return const _MessageState(
                    icon: Icons.forum_outlined,
                    title: 'No Overseer threads yet',
                    detail:
                        'Create one and choose its working directory and provider.',
                  );
                }
                return LayoutBuilder(
                  builder: (context, constraints) {
                    final columns = constraints.maxWidth >= 1040 ? 2 : 1;
                    return GridView.builder(
                      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                        crossAxisCount: columns,
                        crossAxisSpacing: 12,
                        mainAxisSpacing: 12,
                        mainAxisExtent: 210,
                      ),
                      itemCount: threads.length,
                      itemBuilder: (context, index) {
                        final thread = threads[index];
                        final key =
                            '${thread.backend}:${thread.externalSessionId}';
                        return _ThreadCard(
                          thread: thread,
                          busy: _busyThreads.contains(key),
                          onResume: () => _resume(thread),
                        );
                      },
                    );
                  },
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _ThreadCard extends StatelessWidget {
  const _ThreadCard({
    required this.thread,
    required this.busy,
    required this.onResume,
  });

  final OverseerThreadRecord thread;
  final bool busy;
  final VoidCallback onResume;

  @override
  Widget build(BuildContext context) {
    final updated = thread.lastActivityAt;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                _Badge(label: thread.backend),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '${thread.model} · ${thread.reasoning}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                ),
                OutlinedButton(
                  key: Key(
                    'resume-${thread.backend}-${thread.externalSessionId}',
                  ),
                  onPressed: busy ? null : onResume,
                  child: busy
                      ? const SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('Resume'),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Text(
              thread.preview.isEmpty ? 'Overseer thread' : thread.preview,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 10),
            _MetadataLine(icon: Icons.folder_outlined, text: thread.spawnCwd),
            const SizedBox(height: 6),
            _MetadataLine(
              icon: Icons.schedule,
              text: updated == null
                  ? 'Update time unavailable'
                  : 'Updated ${DateFormat('yyyy-MM-dd HH:mm').format(updated.toLocal())}',
            ),
            const SizedBox(height: 6),
            _MetadataLine(
              icon: Icons.fingerprint,
              text: '${thread.externalSessionId} · ${thread.origin}',
            ),
          ],
        ),
      ),
    );
  }
}

class _Badge extends StatelessWidget {
  const _Badge({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: _surfaceMuted,
        border: Border.all(color: _border),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
        child: Text(label, style: const TextStyle(fontWeight: FontWeight.w700)),
      ),
    );
  }
}

class _MetadataLine extends StatelessWidget {
  const _MetadataLine({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 16, color: _muted),
        const SizedBox(width: 7),
        Expanded(
          child: Text(
            text,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(color: _muted, fontSize: 12),
          ),
        ),
      ],
    );
  }
}

class _MessageState extends StatelessWidget {
  const _MessageState({
    required this.icon,
    required this.title,
    required this.detail,
    this.action,
  });

  final IconData icon;
  final String title;
  final String detail;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 440),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 36, color: _muted),
            const SizedBox(height: 12),
            Text(title, style: const TextStyle(fontWeight: FontWeight.w800)),
            const SizedBox(height: 6),
            Text(
              detail,
              textAlign: TextAlign.center,
              style: const TextStyle(color: _muted),
            ),
            if (action != null) ...[const SizedBox(height: 8), action!],
          ],
        ),
      ),
    );
  }
}

class _CreateOverseerDialog extends StatefulWidget {
  const _CreateOverseerDialog();

  @override
  State<_CreateOverseerDialog> createState() => _CreateOverseerDialogState();
}

class _CreateOverseerDialogState extends State<_CreateOverseerDialog> {
  final _formKey = GlobalKey<FormState>();
  final _cwd = TextEditingController();
  OverseerBackendOption _backend = overseerBackendOptions.first;
  late String _model = _backend.models.first;
  late String _reasoning = _backend.reasoningOptions.last;

  @override
  void dispose() {
    _cwd.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Create Overseer'),
      content: SizedBox(
        width: 520,
        child: Form(
          key: _formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextFormField(
                key: const Key('overseer-cwd'),
                controller: _cwd,
                decoration: const InputDecoration(
                  labelText: 'Working directory',
                  hintText: '/absolute/path/to/workspace',
                ),
                validator: (value) {
                  final cwd = value?.trim() ?? '';
                  if (!cwd.startsWith('/')) return 'Enter an absolute path';
                  return null;
                },
              ),
              const SizedBox(height: 14),
              DropdownButtonFormField<OverseerBackendOption>(
                key: const Key('overseer-backend'),
                initialValue: _backend,
                decoration: const InputDecoration(labelText: 'Backend'),
                items: overseerBackendOptions
                    .map(
                      (option) => DropdownMenuItem(
                        value: option,
                        child: Text(option.label),
                      ),
                    )
                    .toList(growable: false),
                onChanged: (option) {
                  if (option == null) return;
                  setState(() {
                    _backend = option;
                    _model = option.models.first;
                    _reasoning = option.reasoningOptions.last;
                  });
                },
              ),
              const SizedBox(height: 14),
              Row(
                children: [
                  Expanded(
                    child: DropdownButtonFormField<String>(
                      key: ValueKey('overseer-model-${_backend.id}'),
                      initialValue: _model,
                      isExpanded: true,
                      decoration: const InputDecoration(labelText: 'Model'),
                      items: _backend.models
                          .map(
                            (value) => DropdownMenuItem(
                              value: value,
                              child: Text(value),
                            ),
                          )
                          .toList(growable: false),
                      onChanged: (value) =>
                          setState(() => _model = value ?? _model),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: DropdownButtonFormField<String>(
                      key: ValueKey('overseer-reasoning-${_backend.id}'),
                      initialValue: _reasoning,
                      decoration: const InputDecoration(labelText: 'Reasoning'),
                      items: _backend.reasoningOptions
                          .map(
                            (value) => DropdownMenuItem(
                              value: value,
                              child: Text(value),
                            ),
                          )
                          .toList(growable: false),
                      onChanged: (value) =>
                          setState(() => _reasoning = value ?? _reasoning),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('confirm-create-overseer'),
          onPressed: () {
            if (!_formKey.currentState!.validate()) return;
            Navigator.pop(
              context,
              OverseerLaunchRequest(
                cwd: _cwd.text.trim(),
                backend: _backend.id,
                model: _model,
                reasoning: _reasoning,
              ),
            );
          },
          child: const Text('Launch'),
        ),
      ],
    );
  }
}
