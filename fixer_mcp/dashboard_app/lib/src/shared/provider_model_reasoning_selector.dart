import 'package:cascade_widget/cascade_widget.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

class ProviderModelReasoningOption {
  const ProviderModelReasoningOption({
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

class ProviderModelReasoningSelection {
  const ProviderModelReasoningSelection({
    required this.provider,
    required this.model,
    required this.reasoning,
  });

  final String provider;
  final String model;
  final String reasoning;
}

class ProviderModelReasoningSelector extends StatefulWidget {
  const ProviderModelReasoningSelector({
    required this.providers,
    required this.selectedProvider,
    required this.selectedModel,
    required this.selectedReasoning,
    required this.onSelected,
    super.key,
    this.width = 520,
    this.height = 56,
    this.hintText = 'Provider · model · reasoning',
    this.popupWidth = 220,
    this.popupHeight = 320,
    this.enabled = true,
    this.emptyText = 'No providers',
  });

  final List<ProviderModelReasoningOption> providers;
  final String selectedProvider;
  final String selectedModel;
  final String selectedReasoning;
  final ValueChanged<ProviderModelReasoningSelection> onSelected;
  final double width;
  final double height;
  final String hintText;
  final double popupWidth;
  final double popupHeight;
  final bool enabled;
  final String emptyText;

  @override
  State<ProviderModelReasoningSelector> createState() =>
      _ProviderModelReasoningSelectorState();
}

class _ProviderModelReasoningSelectorState
    extends State<ProviderModelReasoningSelector> {
  late List<DropDownMenuModel> _cascadeOptions;

  @override
  void initState() {
    super.initState();
    _cascadeOptions = _buildCascadeOptions();
  }

  @override
  void didUpdateWidget(ProviderModelReasoningSelector oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!_sameOptions(oldWidget.providers, widget.providers)) {
      _cascadeOptions = _buildCascadeOptions();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final selectedLeafId = _selectedLeafId();
    return SizedBox(
      width: widget.width,
      height: widget.height,
      child: SingleSelectCascadeWidget(
        list: _cascadeOptions,
        selectedIds: selectedLeafId.isEmpty
            ? const <String>[]
            : <String>[selectedLeafId],
        selectedCallBack: (selected) {
          widget.onSelected(_selectionFrom(selected));
        },
        fieldDecoration: FieldDecoration(
          hintText: widget.hintText,
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(8)),
          backgroundColor: theme.colorScheme.surface,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        ),
        chipDecoration: ChipDecoration(
          isShowFullPathFromSelectedTag: true,
          backgroundColor: theme.colorScheme.secondaryContainer,
          labelStyle: TextStyle(color: theme.colorScheme.onSecondaryContainer),
          borderRadius: BorderRadius.circular(8),
          deleteIcon: Icon(
            Icons.close,
            color: theme.colorScheme.onSecondaryContainer,
            size: 16,
          ),
        ),
        popupConfig: PopupConfig(
          popupHeight: widget.popupHeight,
          popupWidth: widget.popupWidth,
          emptyText: widget.emptyText,
          emptyTextStyle: theme.textTheme.bodyMedium,
        ),
        layoutConfig: const LayoutConfig(isShowAllSelectedLabel: false),
        enabled: widget.enabled,
      ),
    );
  }

  List<DropDownMenuModel> _buildCascadeOptions() {
    return [
      for (final provider in widget.providers)
        DropDownMenuModel(
          id: provider.id,
          name: provider.label,
          children: [
            for (final model in provider.models)
              DropDownMenuModel(
                id: '${provider.id}::$model',
                name: model,
                children: [
                  for (final reasoning in provider.reasoningOptions)
                    DropDownMenuModel(
                      id: '${provider.id}::$model::$reasoning',
                      name: reasoning,
                      children: const [],
                    ),
                ],
              ),
          ],
        ),
    ];
  }

  String _selectedLeafId() {
    if (widget.providers.isEmpty) return '';
    final provider = _providerById(widget.selectedProvider);
    final model = provider.models.contains(widget.selectedModel)
        ? widget.selectedModel
        : provider.models.first;
    final reasoning =
        provider.reasoningOptions.contains(widget.selectedReasoning)
        ? widget.selectedReasoning
        : provider.reasoningOptions.first;
    return '${provider.id}::$model::$reasoning';
  }

  ProviderModelReasoningSelection _selectionFrom(
    List<DropDownMenuModel> selected,
  ) {
    if (widget.providers.isEmpty) {
      return const ProviderModelReasoningSelection(
        provider: '',
        model: '',
        reasoning: '',
      );
    }
    if (selected.isEmpty) {
      return _defaultSelection();
    }
    final parts = selected.last.id.split('::');
    final provider = _providerById(
      parts.isNotEmpty ? parts[0] : widget.selectedProvider,
    );
    final model = parts.length > 1 && provider.models.contains(parts[1])
        ? parts[1]
        : provider.models.first;
    final reasoning =
        parts.length > 2 && provider.reasoningOptions.contains(parts[2])
        ? parts[2]
        : provider.reasoningOptions.first;
    return ProviderModelReasoningSelection(
      provider: provider.id,
      model: model,
      reasoning: reasoning,
    );
  }

  ProviderModelReasoningSelection _defaultSelection() {
    final provider = _providerById(widget.selectedProvider);
    return ProviderModelReasoningSelection(
      provider: provider.id,
      model: provider.models.first,
      reasoning: provider.reasoningOptions.first,
    );
  }

  ProviderModelReasoningOption _providerById(String id) {
    return widget.providers.firstWhere(
      (provider) => provider.id == id,
      orElse: () => widget.providers.first,
    );
  }

  bool _sameOptions(
    List<ProviderModelReasoningOption> left,
    List<ProviderModelReasoningOption> right,
  ) {
    if (left.length != right.length) return false;
    for (var index = 0; index < left.length; index += 1) {
      final a = left[index];
      final b = right[index];
      if (a.id != b.id ||
          a.label != b.label ||
          !listEquals(a.models, b.models) ||
          !listEquals(a.reasoningOptions, b.reasoningOptions)) {
        return false;
      }
    }
    return true;
  }
}
