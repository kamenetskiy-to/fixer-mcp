import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';

import 'workroom_models.dart';

class GenUiRenderer extends StatelessWidget {
  const GenUiRenderer({
    super.key,
    required this.document,
    required this.onAction,
    this.pendingActionRef = '',
  });

  final GenUiSurfaceDocument document;
  final Future<void> Function(GenUiActionDescriptor action) onAction;
  final String pendingActionRef;

  @override
  Widget build(BuildContext context) {
    final actionsByRef = {
      for (final action in document.actions) action.ref: action,
    };
    return FocusTraversalGroup(
      policy: WidgetOrderTraversalPolicy(),
      child: ListView.separated(
        key: ValueKey('genui-surface-${document.instanceId}'),
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
        itemCount: document.components.length,
        separatorBuilder: (_, _) => const SizedBox(height: 12),
        itemBuilder: (context, index) {
          final component = document.components[index];
          return KeyedSubtree(
            key: ValueKey('genui-component-${component.id}'),
            child: _buildComponent(context, component, actionsByRef),
          );
        },
      ),
    );
  }

  Widget _buildComponent(
    BuildContext context,
    GenUiComponent component,
    Map<String, GenUiActionDescriptor> actionsByRef,
  ) {
    final theme = Theme.of(context);
    switch (component.kind) {
      case GenUiComponentKind.text:
        final text = component.data['text'] as String;
        final variant = component.data['variant'] as String;
        final style = switch (variant) {
          'heading' => theme.textTheme.titleLarge?.copyWith(
            fontWeight: FontWeight.w800,
          ),
          'caption' => theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
          'mono' => theme.textTheme.bodyMedium?.copyWith(
            fontFamily: 'monospace',
          ),
          _ => theme.textTheme.bodyMedium,
        };
        return SelectableText(text, style: style);
      case GenUiComponentKind.markdown:
        return _SafeMarkdown(source: component.data['source'] as String);
      case GenUiComponentKind.statusBadge:
        final tone = _tone(component.data['tone']);
        return Align(
          alignment: AlignmentDirectional.centerStart,
          child: _ToneBadge(
            label: component.data['label'] as String,
            tone: tone,
          ),
        );
      case GenUiComponentKind.metric:
        final tone = component.data['tone'] == null
            ? GenUiTone.neutral
            : _tone(component.data['tone']);
        return _MetricCard(
          label: component.data['label'] as String,
          value: component.data['value'] as String,
          detail: component.data['detail']?.toString() ?? '',
          tone: tone,
        );
      case GenUiComponentKind.keyValue:
        final rows = (component.data['rows'] as List)
            .map((row) => Map<String, dynamic>.from(row as Map))
            .toList(growable: false);
        return _KeyValueCard(rows: rows);
      case GenUiComponentKind.dataTable:
        final columns = (component.data['columns'] as List).cast<String>();
        final rows = (component.data['rows'] as List)
            .map((row) => (row as List).cast<String>())
            .toList(growable: false);
        return _SurfaceDataTable(columns: columns, rows: rows);
      case GenUiComponentKind.timeline:
        final items = (component.data['items'] as List)
            .map((item) => Map<String, dynamic>.from(item as Map))
            .toList(growable: false);
        return _Timeline(items: items);
      case GenUiComponentKind.callout:
        return _Callout(
          tone: _tone(component.data['tone']),
          title: component.data['title'] as String,
          body: component.data['body'] as String,
        );
      case GenUiComponentKind.actionGroup:
        final refs = (component.data['action_refs'] as List).cast<String>();
        return _ActionGroup(
          actions: refs.map((ref) => actionsByRef[ref]!).toList(),
          pendingActionRef: pendingActionRef,
          onAction: onAction,
        );
      case GenUiComponentKind.divider:
        return const Divider(height: 1);
    }
  }

  GenUiTone _tone(dynamic value) {
    return switch (value) {
      'info' => GenUiTone.info,
      'success' => GenUiTone.success,
      'warning' => GenUiTone.warning,
      'danger' => GenUiTone.danger,
      _ => GenUiTone.neutral,
    };
  }
}

class _MetricCard extends StatelessWidget {
  const _MetricCard({
    required this.label,
    required this.value,
    required this.detail,
    required this.tone,
  });

  final String label;
  final String value;
  final String detail;
  final GenUiTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = _toneColors(context, tone);
    return Card(
      margin: EdgeInsets.zero,
      color: colors.background,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: Theme.of(
                context,
              ).textTheme.labelLarge?.copyWith(color: colors.foreground),
            ),
            const SizedBox(height: 6),
            SelectableText(
              value,
              style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                color: colors.foreground,
                fontWeight: FontWeight.w800,
              ),
            ),
            if (detail.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                detail,
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: colors.foreground),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _KeyValueCard extends StatelessWidget {
  const _KeyValueCard({required this.rows});

  final List<Map<String, dynamic>> rows;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            for (var index = 0; index < rows.length; index++) ...[
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(
                    width: 150,
                    child: Text(
                      rows[index]['label'] as String,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: SelectableText(rows[index]['value'] as String),
                  ),
                ],
              ),
              if (index != rows.length - 1) const Divider(height: 20),
            ],
          ],
        ),
      ),
    );
  }
}

class _SurfaceDataTable extends StatefulWidget {
  const _SurfaceDataTable({required this.columns, required this.rows});

  final List<String> columns;
  final List<List<String>> rows;

  @override
  State<_SurfaceDataTable> createState() => _SurfaceDataTableState();
}

class _SurfaceDataTableState extends State<_SurfaceDataTable> {
  final _scrollController = ScrollController();

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
      child: Scrollbar(
        controller: _scrollController,
        thumbVisibility: true,
        child: SingleChildScrollView(
          controller: _scrollController,
          scrollDirection: Axis.horizontal,
          child: DataTable(
            columns: [
              for (final column in widget.columns)
                DataColumn(label: Text(column)),
            ],
            rows: [
              for (final row in widget.rows)
                DataRow(
                  cells: [for (final value in row) DataCell(Text(value))],
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Timeline extends StatelessWidget {
  const _Timeline({required this.items});

  final List<Map<String, dynamic>> items;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            for (var index = 0; index < items.length; index++)
              IntrinsicHeight(
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SizedBox(
                      width: 20,
                      child: Column(
                        children: [
                          Container(
                            width: 10,
                            height: 10,
                            decoration: BoxDecoration(
                              shape: BoxShape.circle,
                              color: _toneColors(
                                context,
                                _toneFromRaw(items[index]['tone']),
                              ).foreground,
                            ),
                          ),
                          if (index != items.length - 1)
                            Expanded(
                              child: Container(
                                width: 1,
                                color: Theme.of(
                                  context,
                                ).colorScheme.outlineVariant,
                              ),
                            ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.only(bottom: 16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              items[index]['label'] as String,
                              style: Theme.of(context).textTheme.titleSmall,
                            ),
                            Text(
                              items[index]['timestamp'] as String,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    color: Theme.of(
                                      context,
                                    ).colorScheme.onSurfaceVariant,
                                  ),
                            ),
                            if ((items[index]['detail']?.toString() ?? '')
                                .isNotEmpty) ...[
                              const SizedBox(height: 4),
                              Text(items[index]['detail'] as String),
                            ],
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _Callout extends StatelessWidget {
  const _Callout({required this.tone, required this.title, required this.body});

  final GenUiTone tone;
  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final colors = _toneColors(context, tone);
    return Semantics(
      container: true,
      liveRegion: tone == GenUiTone.danger || tone == GenUiTone.warning,
      label: '$title. $body',
      child: Container(
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: colors.background,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: colors.border),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(_toneIcon(tone), color: colors.foreground, size: 20),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: Theme.of(
                      context,
                    ).textTheme.titleSmall?.copyWith(color: colors.foreground),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    body,
                    style: Theme.of(
                      context,
                    ).textTheme.bodyMedium?.copyWith(color: colors.foreground),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ActionGroup extends StatelessWidget {
  const _ActionGroup({
    required this.actions,
    required this.pendingActionRef,
    required this.onAction,
  });

  final List<GenUiActionDescriptor> actions;
  final String pendingActionRef;
  final Future<void> Function(GenUiActionDescriptor action) onAction;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 10,
      runSpacing: 10,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        for (final action in actions)
          _ActionButton(
            action: action,
            pending: pendingActionRef == action.ref,
            onPressed: () => onAction(action),
          ),
      ],
    );
  }
}

class _ActionButton extends StatelessWidget {
  const _ActionButton({
    required this.action,
    required this.pending,
    required this.onPressed,
  });

  final GenUiActionDescriptor action;
  final bool pending;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final button = FilledButton.icon(
      key: ValueKey('genui-action-${action.ref}'),
      onPressed: action.enabled && !pending ? onPressed : null,
      icon: pending
          ? const SizedBox.square(
              dimension: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : const Icon(Icons.shield_outlined, size: 18),
      label: Text(action.label),
    );
    if (action.enabled || action.disabledReason.isEmpty) return button;
    final reason = [
      action.disabledReason,
      if (action.disabledReasonCode.isNotEmpty)
        '(${action.disabledReasonCode})',
    ].join(' ');
    return Semantics(
      container: true,
      label: '${action.label}. $reason',
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 360),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Tooltip(message: reason, child: button),
            const SizedBox(height: 4),
            Text(
              reason,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ToneBadge extends StatelessWidget {
  const _ToneBadge({required this.label, required this.tone});

  final String label;
  final GenUiTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = _toneColors(context, tone);
    return Semantics(
      label: label,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
        decoration: BoxDecoration(
          color: colors.background,
          borderRadius: BorderRadius.circular(999),
          border: Border.all(color: colors.border),
        ),
        child: Text(
          label,
          style: Theme.of(context).textTheme.labelMedium?.copyWith(
            color: colors.foreground,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
    );
  }
}

class _SafeMarkdown extends StatelessWidget {
  const _SafeMarkdown({required this.source});

  final String source;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bodyStyle =
        theme.textTheme.bodyMedium?.copyWith(
          color: theme.colorScheme.onSurface,
          height: 1.45,
        ) ??
        TextStyle(color: theme.colorScheme.onSurface, height: 1.45);
    return MarkdownWidget(
      data: _sanitize(source),
      shrinkWrap: true,
      selectable: true,
      physics: const NeverScrollableScrollPhysics(),
      padding: EdgeInsets.zero,
      config: MarkdownConfig.defaultConfig.copy(
        configs: [
          PConfig(textStyle: bodyStyle),
          H1Config(style: theme.textTheme.titleLarge ?? bodyStyle),
          H2Config(style: theme.textTheme.titleMedium ?? bodyStyle),
          H3Config(style: theme.textTheme.titleSmall ?? bodyStyle),
          CodeConfig(
            style: bodyStyle.copyWith(
              fontFamily: 'monospace',
              backgroundColor: theme.colorScheme.surfaceContainerHighest,
            ),
          ),
        ],
      ),
    );
  }

  String _sanitize(String value) {
    final withoutHtml = value.replaceAll(RegExp(r'<[^>]*>'), '');
    return withoutHtml.replaceAllMapped(
      RegExp(r'\[([^\]]+)\]\((?:https?|file|javascript):[^)]*\)'),
      (match) => '${match.group(1)} (external link disabled)',
    );
  }
}

({Color background, Color foreground, Color border}) _toneColors(
  BuildContext context,
  GenUiTone tone,
) {
  final scheme = Theme.of(context).colorScheme;
  return switch (tone) {
    GenUiTone.info => (
      background: scheme.primaryContainer,
      foreground: scheme.onPrimaryContainer,
      border: scheme.primary.withValues(alpha: 0.35),
    ),
    GenUiTone.success => (
      background: const Color(0xFFE9F7EF),
      foreground: const Color(0xFF155B33),
      border: const Color(0xFF91D3AD),
    ),
    GenUiTone.warning => (
      background: const Color(0xFFFFF4DB),
      foreground: const Color(0xFF6E4A00),
      border: const Color(0xFFE7C66E),
    ),
    GenUiTone.danger => (
      background: scheme.errorContainer,
      foreground: scheme.onErrorContainer,
      border: scheme.error.withValues(alpha: 0.35),
    ),
    GenUiTone.neutral => (
      background: scheme.surfaceContainerHighest,
      foreground: scheme.onSurfaceVariant,
      border: scheme.outlineVariant,
    ),
  };
}

IconData _toneIcon(GenUiTone tone) {
  return switch (tone) {
    GenUiTone.info => Icons.info_outline,
    GenUiTone.success => Icons.check_circle_outline,
    GenUiTone.warning => Icons.warning_amber_rounded,
    GenUiTone.danger => Icons.error_outline,
    GenUiTone.neutral => Icons.notes_outlined,
  };
}

GenUiTone _toneFromRaw(dynamic value) {
  return switch (value) {
    'info' => GenUiTone.info,
    'success' => GenUiTone.success,
    'warning' => GenUiTone.warning,
    'danger' => GenUiTone.danger,
    _ => GenUiTone.neutral,
  };
}
