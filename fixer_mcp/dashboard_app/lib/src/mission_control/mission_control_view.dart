import 'dart:async';

import 'package:flutter/material.dart';

import '../app_localizations.dart';
import 'mission_control_models.dart';
import 'mission_control_repository.dart';

enum MissionControlWaveFilter {
  all,
  planned,
  active,
  architectNeeded,
  review,
  completed,
}

class MissionControlWavesView extends StatefulWidget {
  const MissionControlWavesView({
    super.key,
    required this.projectId,
    required this.repository,
    this.pollInterval = const Duration(seconds: 15),
  });

  final int projectId;
  final MissionControlRepository repository;
  final Duration pollInterval;

  @override
  State<MissionControlWavesView> createState() =>
      _MissionControlWavesViewState();
}

class _MissionControlWavesViewState extends State<MissionControlWavesView> {
  MissionControlWavesSnapshot? _snapshot;
  MissionControlWaveFilter _filter = MissionControlWaveFilter.all;
  Timer? _pollTimer;
  Object? _error;
  bool _loading = false;
  int? _selectedPlanId;
  int? _selectedWaveId;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
    _pollTimer = Timer.periodic(widget.pollInterval, (_) => unawaited(_load()));
  }

  @override
  void didUpdateWidget(covariant MissionControlWavesView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.projectId != widget.projectId ||
        oldWidget.repository != widget.repository) {
      _snapshot = null;
      _selectedPlanId = null;
      _selectedWaveId = null;
      unawaited(_load());
    }
    if (oldWidget.pollInterval != widget.pollInterval) {
      _pollTimer?.cancel();
      _pollTimer = Timer.periodic(
        widget.pollInterval,
        (_) => unawaited(_load()),
      );
    }
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    if (mounted) setState(() {});
    try {
      final next = await widget.repository.loadWaves(widget.projectId);
      if (!mounted) return;
      setState(() {
        _snapshot = next;
        _error = null;
        final visiblePlanIds = next.plannedWaves
            .map((plan) => plan.planId)
            .toSet();
        final visibleIds = next.waves.map((wave) => wave.waveId).toSet();
        if (_selectedPlanId != null &&
            !visiblePlanIds.contains(_selectedPlanId)) {
          _selectedPlanId = null;
        }
        if (_selectedWaveId != null && !visibleIds.contains(_selectedWaveId)) {
          _selectedWaveId = null;
        }
        if (_selectedPlanId == null && _selectedWaveId == null) {
          if (next.waves.isNotEmpty) {
            _selectedWaveId = next.waves.first.waveId;
          } else if (next.plannedWaves.isNotEmpty) {
            _selectedPlanId = next.plannedWaves.first.planId;
          }
        }
      });
    } catch (error) {
      if (mounted) setState(() => _error = error);
    } finally {
      _loading = false;
      if (mounted) setState(() {});
    }
  }

  Future<void> _runAction(
    MissionControlWave wave,
    MissionControlActionCapability capability,
  ) async {
    if (!capability.enabled) return;
    final l10n = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(
          l10n.isRussian ? 'Подтвердить действие волны' : 'Confirm wave action',
        ),
        content: Text(
          l10n.isRussian
              ? '${_actionLabel(l10n, capability.action)} для волны #${wave.waveId}. '
                    'Действие будет отправлено в управляемый Wave Engine.'
              : '${_actionLabel(l10n, capability.action)} wave #${wave.waveId}. '
                    'The request will be delegated to the governed Wave Engine.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(l10n.isRussian ? 'Отмена' : 'Cancel'),
          ),
          FilledButton(
            key: ValueKey('confirm-wave-action-${capability.action}'),
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(l10n.isRussian ? 'Подтвердить' : 'Confirm'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.repository.runWaveAction(
        widget.projectId,
        wave.waveId,
        capability.action,
      );
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(error.toString())));
    }
  }

  Future<void> _initializePlan(MissionControlPlannedWave plan) async {
    final capability = plan.actionCapabilities.initialize;
    if (!capability.enabled) return;
    final l10n = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(
          l10n.isRussian
              ? 'Инициализировать плановую волну?'
              : 'Initialize planned wave?',
        ),
        content: Text(
          l10n.isRussian
              ? 'План #${plan.planId} создаст обычные pending-сессии и волну через управляемый Wave Engine. Только после подтверждения появятся worktree и зафиксированный base SHA.'
              : 'Plan #${plan.planId} will create normal pending sessions and a wave through the governed Wave Engine. Worktrees and a resolved base SHA only begin after confirmation.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(l10n.isRussian ? 'Отмена' : 'Cancel'),
          ),
          FilledButton(
            key: const ValueKey('confirm-planned-wave-initialize'),
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(l10n.isRussian ? 'Инициализировать' : 'Initialize'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await widget.repository.runWaveAction(
        widget.projectId,
        plan.planId,
        'initialize',
      );
      final actionResult =
          widget.repository is MissionControlWaveActionResultSource
          ? (widget.repository as MissionControlWaveActionResultSource)
                .lastWaveActionResult
          : const MissionControlWaveActionResult();
      await _load();
      if (!mounted) return;
      MissionControlPlannedWave? refreshed;
      for (final item in _snapshot?.plannedWaves ?? const []) {
        if (item.planId == plan.planId) {
          refreshed = item;
          break;
        }
      }
      final initializedWaveId = actionResult.initializedWaveId > 0
          ? actionResult.initializedWaveId
          : refreshed?.initializedWaveId ?? 0;
      if (initializedWaveId > 0) {
        setState(() {
          _filter = MissionControlWaveFilter.all;
          _selectedPlanId = null;
          _selectedWaveId = initializedWaveId;
        });
      }
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(error.toString())));
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_snapshot == null) {
      if (_error != null) {
        return _MissionControlLoadError(error: _error!, onRetry: _load);
      }
      return const Center(child: CircularProgressIndicator());
    }

    final snapshot = _snapshot!;
    final plannedWaves = _filteredPlannedWaves(snapshot.plannedWaves);
    final waves = _filteredWaves(snapshot.waves);
    final selectedPlan = _selectedPlan(plannedWaves);
    final selected = selectedPlan == null ? _selectedWave(waves) : null;

    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 1050;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _MissionControlHeader(
              snapshot: snapshot,
              refreshing: _loading,
              refreshError: _error,
              onRefresh: _load,
            ),
            _MissionControlFilters(
              selected: _filter,
              allPlannedWaves: snapshot.plannedWaves,
              allWaves: snapshot.waves,
              onSelected: (filter) => setState(() {
                _filter = filter;
                _selectedPlanId = null;
                _selectedWaveId = null;
              }),
            ),
            Expanded(
              child: plannedWaves.isEmpty && waves.isEmpty
                  ? _EmptyFilterState(filter: _filter)
                  : wide
                  ? Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        SizedBox(
                          width: 390,
                          child: _WaveList(
                            plannedWaves: plannedWaves,
                            waves: waves,
                            selectedPlanId: selectedPlan?.planId,
                            selectedWaveId: selected?.waveId,
                            onPlanSelected: (plan) => setState(() {
                              _selectedPlanId = plan.planId;
                              _selectedWaveId = null;
                            }),
                            onSelected: (wave) => setState(() {
                              _selectedPlanId = null;
                              _selectedWaveId = wave.waveId;
                            }),
                          ),
                        ),
                        const VerticalDivider(width: 1),
                        Expanded(
                          child: selectedPlan != null
                              ? _PlannedWaveDetail(
                                  plan: selectedPlan,
                                  onInitialize: () =>
                                      _initializePlan(selectedPlan),
                                )
                              : selected == null
                              ? const SizedBox.shrink()
                              : _WaveDetail(
                                  wave: selected,
                                  onRunAction: (capability) =>
                                      _runAction(selected, capability),
                                ),
                        ),
                      ],
                    )
                  : _CompactWaveList(
                      plannedWaves: plannedWaves,
                      waves: waves,
                      selectedPlanId: selectedPlan?.planId,
                      selectedWaveId: selected?.waveId,
                      onPlanSelected: (plan) => setState(() {
                        _selectedPlanId = plan.planId;
                        _selectedWaveId = null;
                      }),
                      onSelected: (wave) => setState(() {
                        _selectedPlanId = null;
                        _selectedWaveId = wave.waveId;
                      }),
                      onInitializePlan: _initializePlan,
                      onRunAction: _runAction,
                    ),
            ),
          ],
        );
      },
    );
  }

  List<MissionControlWave> _filteredWaves(List<MissionControlWave> waves) {
    return waves
        .where((wave) {
          return switch (_filter) {
            MissionControlWaveFilter.all => true,
            MissionControlWaveFilter.planned => false,
            MissionControlWaveFilter.active => wave.isActiveState,
            MissionControlWaveFilter.architectNeeded => wave.needsArchitect,
            MissionControlWaveFilter.review => wave.isReviewState,
            MissionControlWaveFilter.completed => wave.isCompleted,
          };
        })
        .toList(growable: false);
  }

  List<MissionControlPlannedWave> _filteredPlannedWaves(
    List<MissionControlPlannedWave> plans,
  ) {
    return plans
        .where((plan) {
          return switch (_filter) {
            MissionControlWaveFilter.all ||
            MissionControlWaveFilter.planned => true,
            MissionControlWaveFilter.architectNeeded => plan.needsArchitect,
            MissionControlWaveFilter.active ||
            MissionControlWaveFilter.review ||
            MissionControlWaveFilter.completed => false,
          };
        })
        .toList(growable: false);
  }

  MissionControlPlannedWave? _selectedPlan(
    List<MissionControlPlannedWave> plans,
  ) {
    if (plans.isEmpty || _selectedWaveId != null) return null;
    if (_selectedPlanId == null) return plans.first;
    return plans.cast<MissionControlPlannedWave?>().firstWhere(
      (plan) => plan?.planId == _selectedPlanId,
      orElse: () => null,
    );
  }

  MissionControlWave? _selectedWave(List<MissionControlWave> waves) {
    if (waves.isEmpty) return null;
    return waves.cast<MissionControlWave?>().firstWhere(
      (wave) => wave?.waveId == _selectedWaveId,
      orElse: () => waves.first,
    );
  }
}

class _MissionControlHeader extends StatelessWidget {
  const _MissionControlHeader({
    required this.snapshot,
    required this.refreshing,
    required this.refreshError,
    required this.onRefresh,
  });

  final MissionControlWavesSnapshot snapshot;
  final bool refreshing;
  final Object? refreshError;
  final VoidCallback onRefresh;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final freshness = snapshot.freshness;
    final showAlert = freshness.stale || refreshError != null;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(bottom: BorderSide(color: theme.dividerColor)),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 16, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Wrap(
              spacing: 12,
              runSpacing: 10,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.account_tree_outlined,
                      color: theme.colorScheme.primary,
                    ),
                    const SizedBox(width: 10),
                    Text(
                      l10n.missionControl,
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                  ],
                ),
                _StateBadge(
                  label: _humanize(freshness.state),
                  tone: freshness.stale
                      ? _BadgeTone.danger
                      : freshness.state == 'fresh'
                      ? _BadgeTone.success
                      : _BadgeTone.neutral,
                ),
                Text(
                  l10n.isRussian
                      ? '${snapshot.plannedWaves.length} плановых · ${snapshot.waves.length} волн · снимок ${_formatTimestamp(snapshot.generatedAt)}'
                      : '${snapshot.plannedWaves.length} planned · ${snapshot.waves.length} waves · snapshot ${_formatTimestamp(snapshot.generatedAt)}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                OutlinedButton.icon(
                  key: const ValueKey('mission-control-refresh'),
                  onPressed: refreshing ? null : onRefresh,
                  icon: refreshing
                      ? const SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.refresh, size: 18),
                  label: Text(l10n.refresh),
                ),
              ],
            ),
            if (showAlert) ...[
              const SizedBox(height: 12),
              _AttentionBanner(
                icon: refreshError != null
                    ? Icons.cloud_off_outlined
                    : Icons.schedule_outlined,
                title: refreshError != null
                    ? (l10n.isRussian
                          ? 'Автообновление не удалось'
                          : 'Automatic refresh failed')
                    : (l10n.isRussian
                          ? 'Данные волн устарели'
                          : 'Wave data is stale'),
                message: refreshError?.toString() ?? freshness.reason,
                danger: true,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _MissionControlFilters extends StatelessWidget {
  const _MissionControlFilters({
    required this.selected,
    required this.allPlannedWaves,
    required this.allWaves,
    required this.onSelected,
  });

  final MissionControlWaveFilter selected;
  final List<MissionControlPlannedWave> allPlannedWaves;
  final List<MissionControlWave> allWaves;
  final ValueChanged<MissionControlWaveFilter> onSelected;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    int count(MissionControlWaveFilter filter) {
      final waveCount = allWaves.where((wave) {
        return switch (filter) {
          MissionControlWaveFilter.all => true,
          MissionControlWaveFilter.planned => false,
          MissionControlWaveFilter.active => wave.isActiveState,
          MissionControlWaveFilter.architectNeeded => wave.needsArchitect,
          MissionControlWaveFilter.review => wave.isReviewState,
          MissionControlWaveFilter.completed => wave.isCompleted,
        };
      }).length;
      final planCount = allPlannedWaves.where((plan) {
        return switch (filter) {
          MissionControlWaveFilter.all ||
          MissionControlWaveFilter.planned => true,
          MissionControlWaveFilter.architectNeeded => plan.needsArchitect,
          MissionControlWaveFilter.active ||
          MissionControlWaveFilter.review ||
          MissionControlWaveFilter.completed => false,
        };
      }).length;
      return waveCount + planCount;
    }

    String label(MissionControlWaveFilter filter) => switch (filter) {
      MissionControlWaveFilter.all => l10n.isRussian ? 'Все' : 'All',
      MissionControlWaveFilter.planned =>
        l10n.isRussian ? 'Плановые' : 'Planned',
      MissionControlWaveFilter.active => l10n.isRussian ? 'Активные' : 'Active',
      MissionControlWaveFilter.architectNeeded =>
        l10n.isRussian ? 'Нужен Архитектор' : 'Architect needed',
      MissionControlWaveFilter.review => l10n.isRussian ? 'Проверка' : 'Review',
      MissionControlWaveFilter.completed =>
        l10n.isRussian ? 'Завершённые' : 'Completed',
    };

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      child: Row(
        children: [
          for (final filter in MissionControlWaveFilter.values) ...[
            FilterChip(
              key: ValueKey('mission-filter-${filter.name}'),
              selected: selected == filter,
              onSelected: (_) => onSelected(filter),
              label: Text('${label(filter)} ${count(filter)}'),
            ),
            const SizedBox(width: 8),
          ],
        ],
      ),
    );
  }
}

class _WaveList extends StatelessWidget {
  const _WaveList({
    required this.plannedWaves,
    required this.waves,
    required this.selectedPlanId,
    required this.selectedWaveId,
    required this.onPlanSelected,
    required this.onSelected,
  });

  final List<MissionControlPlannedWave> plannedWaves;
  final List<MissionControlWave> waves;
  final int? selectedPlanId;
  final int? selectedWaveId;
  final ValueChanged<MissionControlPlannedWave> onPlanSelected;
  final ValueChanged<MissionControlWave> onSelected;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(16, 8, 12, 20),
      itemCount: plannedWaves.length + waves.length,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        if (index < waves.length) {
          final wave = waves[index];
          return _WaveSummaryCard(
            wave: wave,
            selected: wave.waveId == selectedWaveId,
            onTap: () => onSelected(wave),
          );
        }
        final plan = plannedWaves[index - waves.length];
        return _PlannedWaveSummaryCard(
          plan: plan,
          selected: plan.planId == selectedPlanId,
          onTap: () => onPlanSelected(plan),
        );
      },
    );
  }
}

class _CompactWaveList extends StatelessWidget {
  const _CompactWaveList({
    required this.plannedWaves,
    required this.waves,
    required this.selectedPlanId,
    required this.selectedWaveId,
    required this.onPlanSelected,
    required this.onSelected,
    required this.onInitializePlan,
    required this.onRunAction,
  });

  final List<MissionControlPlannedWave> plannedWaves;
  final List<MissionControlWave> waves;
  final int? selectedPlanId;
  final int? selectedWaveId;
  final ValueChanged<MissionControlPlannedWave> onPlanSelected;
  final ValueChanged<MissionControlWave> onSelected;
  final ValueChanged<MissionControlPlannedWave> onInitializePlan;
  final Future<void> Function(
    MissionControlWave wave,
    MissionControlActionCapability capability,
  )
  onRunAction;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(12, 6, 12, 24),
      itemCount: plannedWaves.length + waves.length,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        if (index < waves.length) {
          final wave = waves[index];
          final selected = wave.waveId == selectedWaveId;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _WaveSummaryCard(
                wave: wave,
                selected: selected,
                onTap: () => onSelected(wave),
              ),
              if (selected)
                _WaveDetail(
                  wave: wave,
                  compact: true,
                  onRunAction: (capability) => onRunAction(wave, capability),
                ),
            ],
          );
        }
        final plan = plannedWaves[index - waves.length];
        final selected = plan.planId == selectedPlanId;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _PlannedWaveSummaryCard(
              plan: plan,
              selected: selected,
              onTap: () => onPlanSelected(plan),
            ),
            if (selected)
              _PlannedWaveDetail(
                plan: plan,
                compact: true,
                onInitialize: () => onInitializePlan(plan),
              ),
          ],
        );
      },
    );
  }
}

class _PlannedWaveSummaryCard extends StatelessWidget {
  const _PlannedWaveSummaryCard({
    required this.plan,
    required this.selected,
    required this.onTap,
  });

  final MissionControlPlannedWave plan;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      key: ValueKey('mission-plan-${plan.planId}'),
      color: selected
          ? theme.colorScheme.secondaryContainer.withValues(alpha: 0.4)
          : null,
      shape: RoundedRectangleBorder(
        side: BorderSide(
          color: selected ? theme.colorScheme.secondary : theme.dividerColor,
          width: selected ? 1.5 : 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Plan #${plan.planId}',
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                  ),
                  _StateBadge(
                    label: _humanize(plan.operatorState),
                    tone: plan.needsArchitect
                        ? _BadgeTone.danger
                        : plan.isInitialized
                        ? _BadgeTone.success
                        : _BadgeTone.warning,
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(plan.title, style: theme.textTheme.bodyMedium),
              const SizedBox(height: 10),
              Wrap(
                spacing: 10,
                runSpacing: 4,
                children: [
                  _IconFact(
                    icon: Icons.task_alt_outlined,
                    text: '${plan.taskCounts.total} planned tasks',
                  ),
                  const _IconFact(icon: Icons.bolt_outlined, text: '0 active'),
                  _IconFact(
                    icon: plan.readinessState == 'ready'
                        ? Icons.check_circle_outline
                        : Icons.rule_outlined,
                    text: _humanize(plan.readinessState),
                    danger: plan.readinessState == 'blocked',
                  ),
                ],
              ),
              const SizedBox(height: 10),
              _AttentionBanner(
                icon: Icons.inventory_2_outlined,
                title: 'PLANNED DEFINITION',
                message: plan.isInitialized
                    ? 'Materialized as normal wave #${plan.initializedWaveId}; runtime ownership is shown on that wave.'
                    : 'No sessions, worktrees, or resolved base SHA exist yet.',
                compact: true,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _PlannedWaveDetail extends StatelessWidget {
  const _PlannedWaveDetail({
    required this.plan,
    required this.onInitialize,
    this.compact = false,
  });

  final MissionControlPlannedWave plan;
  final VoidCallback onInitialize;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final content = <Widget>[
      Row(
        children: [
          Expanded(
            child: Text(
              'Plan #${plan.planId} · ${plan.title}',
              style: Theme.of(
                context,
              ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w900),
            ),
          ),
          _StateBadge(
            label: _humanize(plan.operatorState),
            tone: plan.needsArchitect
                ? _BadgeTone.danger
                : plan.isInitialized
                ? _BadgeTone.success
                : _BadgeTone.warning,
          ),
        ],
      ),
      const SizedBox(height: 14),
      _AttentionBanner(
        icon: Icons.inventory_2_outlined,
        title: plan.isInitialized
            ? 'PLANNED DEFINITION MATERIALIZED'
            : 'PLANNED, NOT INITIALIZED',
        message: plan.isInitialized
            ? 'This definition created normal wave #${plan.initializedWaveId}. Runtime sessions, worktrees, and base SHA belong to the normal wave record.'
            : 'This is future work metadata only. It owns no Netrunner sessions, worktrees, or resolved base SHA until governed Initialize succeeds.',
      ),
      const SizedBox(height: 14),
      _PlannedWaveEvidencePanel(plan: plan),
      const SizedBox(height: 14),
      _PlannedWaveReadinessPanel(plan: plan),
      const SizedBox(height: 14),
      _PlannedWaveTasksPanel(plan: plan),
      const SizedBox(height: 14),
      _PlannedWaveActionsPanel(plan: plan, onInitialize: onInitialize),
    ];
    if (compact) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(4, 10, 4, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: content,
        ),
      );
    }
    return ListView(
      key: ValueKey('mission-plan-detail-${plan.planId}'),
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 28),
      children: content,
    );
  }
}

class _PlannedWaveEvidencePanel extends StatelessWidget {
  const _PlannedWaveEvidencePanel({required this.plan});

  final MissionControlPlannedWave plan;

  @override
  Widget build(BuildContext context) {
    final provider = [
      plan.backend,
      plan.model,
      plan.reasoning,
    ].where((value) => value.isNotEmpty).join(' · ');
    return _SectionCard(
      title: 'Planned definition',
      subtitle:
          'Stored intent is visible here; runtime evidence begins only after initialization.',
      child: _FactWrap(
        facts: [
          ('Status', _humanize(plan.status)),
          ('Readiness', _humanize(plan.readinessState)),
          ('Tasks', '${plan.taskCounts.total}'),
          ('Materialized', '${plan.taskCounts.materialized}'),
          ('Next action', _humanize(plan.nextAction)),
          (
            'Requested base ref',
            plan.baseRef.isEmpty ? 'default' : plan.baseRef,
          ),
          ('Provider / model', provider.isEmpty ? 'assigned later' : provider),
          (
            'MCP servers',
            plan.mcpServers.isEmpty
                ? 'assigned later'
                : plan.mcpServers.join(', '),
          ),
          ('Created', _formatTimestamp(plan.createdAt)),
          ('Updated', _formatTimestamp(plan.updatedAt)),
          if (plan.initializedWaveId > 0)
            ('Initialized wave', '#${plan.initializedWaveId}'),
        ],
        notes: [if (plan.reason.isNotEmpty) plan.reason],
      ),
    );
  }
}

class _PlannedWaveReadinessPanel extends StatelessWidget {
  const _PlannedWaveReadinessPanel({required this.plan});

  final MissionControlPlannedWave plan;

  @override
  Widget build(BuildContext context) {
    final errors = plan.readinessErrors;
    return _SectionCard(
      title: 'Initialization readiness',
      subtitle:
          'Validation and capability evidence comes from the canonical API contract.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: _StateBadge(
              label: _humanize(plan.readinessState),
              tone: errors.isNotEmpty
                  ? _BadgeTone.danger
                  : plan.readinessState == 'ready'
                  ? _BadgeTone.success
                  : _BadgeTone.neutral,
            ),
          ),
          if (errors.isEmpty) ...[
            const SizedBox(height: 10),
            Text(
              plan.actionCapabilities.initialize.enabled
                  ? 'The API currently allows governed initialization.'
                  : plan.actionCapabilities.initialize.disabledReason,
            ),
          ] else
            for (final error in errors) ...[
              const SizedBox(height: 10),
              _AttentionBanner(
                icon: Icons.error_outline,
                title: 'VALIDATION ERROR',
                message: error,
                danger: true,
                compact: true,
              ),
            ],
        ],
      ),
    );
  }
}

class _PlannedWaveTasksPanel extends StatelessWidget {
  const _PlannedWaveTasksPanel({required this.plan});

  final MissionControlPlannedWave plan;

  @override
  Widget build(BuildContext context) {
    return _SectionCard(
      title: 'Planned tasks (${plan.tasks.length})',
      child: plan.tasks.isEmpty
          ? const Text('No planned task definitions were returned.')
          : Column(
              children: [
                for (var index = 0; index < plan.tasks.length; index++) ...[
                  _PlannedTaskRow(plan: plan, task: plan.tasks[index]),
                  if (index != plan.tasks.length - 1) const Divider(height: 17),
                ],
              ],
            ),
    );
  }
}

class _PlannedTaskRow extends StatelessWidget {
  const _PlannedTaskRow({required this.plan, required this.task});

  final MissionControlPlannedWave plan;
  final MissionControlPlannedWaveTask task;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final provider = [
      task.backend.isEmpty ? plan.backend : task.backend,
      task.model.isEmpty ? plan.model : task.model,
      task.reasoning.isEmpty ? plan.reasoning : task.reasoning,
    ].where((value) => value.isNotEmpty).join(' · ');
    final mcpServers = task.mcpServers.isEmpty
        ? plan.mcpServers
        : task.mcpServers;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(
              task.key,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: FontWeight.w900,
              ),
            ),
            _StateBadge(
              label: task.materializedSessionId > 0
                  ? 'materialized'
                  : 'planned',
              tone: task.materializedSessionId > 0
                  ? _BadgeTone.success
                  : _BadgeTone.warning,
            ),
          ],
        ),
        if (task.taskDescription.isNotEmpty) ...[
          const SizedBox(height: 5),
          Text(task.taskDescription),
        ],
        const SizedBox(height: 7),
        Text(
          'Provider / model: ${provider.isEmpty ? 'assigned at initialization' : provider}',
          style: theme.textTheme.bodySmall,
        ),
        Text(
          'MCP: ${mcpServers.isEmpty ? 'assigned at initialization' : mcpServers.join(', ')}',
          style: theme.textTheme.bodySmall,
        ),
        Text(
          'Depends on: ${task.dependsOn.isEmpty ? 'none' : task.dependsOn.join(', ')}',
          style: theme.textTheme.bodySmall,
        ),
      ],
    );
  }
}

class _PlannedWaveActionsPanel extends StatelessWidget {
  const _PlannedWaveActionsPanel({
    required this.plan,
    required this.onInitialize,
  });

  final MissionControlPlannedWave plan;
  final VoidCallback onInitialize;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return _SectionCard(
      title: l10n.isRussian ? 'Управление планом' : 'Plan controls',
      subtitle: l10n.isRussian
          ? 'Initialize доступен только по capability API; Launch остаётся отдельным управляемым шагом.'
          : 'Initialize is enabled only by the API capability; Launch remains a separate governed step.',
      child: Wrap(
        spacing: 10,
        runSpacing: 12,
        children: [
          for (final capability in plan.actionCapabilities.values)
            SizedBox(
              width: 240,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Tooltip(
                    message: capability.enabled
                        ? _actionLabel(l10n, capability.action)
                        : capability.disabledReason,
                    child: OutlinedButton(
                      key: ValueKey(
                        'plan-${plan.planId}-action-${capability.action}',
                      ),
                      onPressed:
                          capability.enabled &&
                              capability.action == 'initialize'
                          ? onInitialize
                          : null,
                      child: Text(_actionLabel(l10n, capability.action)),
                    ),
                  ),
                  if (!capability.enabled) ...[
                    const SizedBox(height: 4),
                    Text(
                      capability.disabledReason,
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _WaveSummaryCard extends StatelessWidget {
  const _WaveSummaryCard({
    required this.wave,
    required this.selected,
    required this.onTap,
  });

  final MissionControlWave wave;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      key: ValueKey('mission-wave-${wave.waveId}'),
      color: selected
          ? theme.colorScheme.primaryContainer.withValues(alpha: 0.28)
          : null,
      shape: RoundedRectangleBorder(
        side: BorderSide(
          color: selected ? theme.colorScheme.primary : theme.dividerColor,
          width: selected ? 1.5 : 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Wave #${wave.waveId}',
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                  ),
                  _StateBadge(
                    label: _humanize(wave.operatorState),
                    tone: wave.needsArchitect
                        ? _BadgeTone.danger
                        : wave.isCompleted
                        ? _BadgeTone.success
                        : _BadgeTone.primary,
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(wave.label, style: theme.textTheme.bodyMedium),
              const SizedBox(height: 10),
              _LifecycleStrip(wave: wave, compact: true),
              const SizedBox(height: 10),
              Wrap(
                spacing: 10,
                runSpacing: 4,
                children: [
                  _IconFact(
                    icon: Icons.groups_2_outlined,
                    text: '${wave.workerCounts.total} workers',
                  ),
                  _IconFact(
                    icon: Icons.bolt_outlined,
                    text: '${wave.workerCounts.active} active',
                  ),
                  if (wave.workerCounts.failed > 0)
                    _IconFact(
                      icon: Icons.error_outline,
                      text: '${wave.workerCounts.failed} failed',
                      danger: true,
                    ),
                ],
              ),
              if (wave.needsArchitect) ...[
                const SizedBox(height: 10),
                const _AttentionBanner(
                  icon: Icons.priority_high,
                  title: 'ARCHITECT NEEDED',
                  message: 'This wave is waiting at a governed decision gate.',
                  danger: true,
                  compact: true,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _WaveDetail extends StatelessWidget {
  const _WaveDetail({
    required this.wave,
    required this.onRunAction,
    this.compact = false,
  });

  final MissionControlWave wave;
  final ValueChanged<MissionControlActionCapability> onRunAction;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final content = <Widget>[
      Row(
        children: [
          Expanded(
            child: Text(
              'Wave #${wave.waveId} · ${wave.label}',
              style: Theme.of(
                context,
              ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w900),
            ),
          ),
          _StateBadge(label: _humanize(wave.phase), tone: _BadgeTone.neutral),
        ],
      ),
      const SizedBox(height: 14),
      _LifecycleStrip(wave: wave),
      if (wave.needsArchitect) ...[
        const SizedBox(height: 14),
        _AttentionBanner(
          icon: Icons.gpp_maybe_outlined,
          title: 'ARCHITECT NEEDED',
          message: '${wave.label}. Next: ${_humanize(wave.nextAction)}.',
          danger: true,
        ),
      ],
      const SizedBox(height: 14),
      _EvidencePanel(wave: wave),
      const SizedBox(height: 14),
      _LinkedLifecyclePanel(wave: wave),
      const SizedBox(height: 14),
      _WorkersPanel(workers: wave.workers),
      const SizedBox(height: 14),
      _WaveActionsPanel(wave: wave, onRunAction: onRunAction),
    ];
    if (compact) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(4, 10, 4, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: content,
        ),
      );
    }
    return ListView(
      key: ValueKey('mission-wave-detail-${wave.waveId}'),
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 28),
      children: content,
    );
  }
}

class _LifecycleStrip extends StatelessWidget {
  const _LifecycleStrip({required this.wave, this.compact = false});

  final MissionControlWave wave;
  final bool compact;

  int get _activeIndex {
    if (wave.isCompleted) return 4;
    if (wave.phase == 'acceptance') return 3;
    if (wave.isReviewState) return 2;
    if (wave.phase == 'initialized') return 0;
    return 1;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final labels = compact
        ? const ['Init', 'Run', 'Review', 'Accept', 'Done']
        : const ['Initialized', 'Workers', 'Review', 'Acceptance', 'Complete'];
    return Semantics(
      label: 'Wave lifecycle: ${labels[_activeIndex]}',
      child: Row(
        children: [
          for (var index = 0; index < labels.length; index++) ...[
            Expanded(
              child: Column(
                children: [
                  Container(
                    height: compact ? 5 : 7,
                    decoration: BoxDecoration(
                      color: index <= _activeIndex
                          ? theme.colorScheme.primary
                          : theme.colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(99),
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    labels[index],
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: index == _activeIndex
                          ? theme.colorScheme.primary
                          : theme.colorScheme.onSurfaceVariant,
                      fontWeight: index == _activeIndex
                          ? FontWeight.w800
                          : FontWeight.w500,
                    ),
                  ),
                ],
              ),
            ),
            if (index != labels.length - 1) const SizedBox(width: 5),
          ],
        ],
      ),
    );
  }
}

class _EvidencePanel extends StatelessWidget {
  const _EvidencePanel({required this.wave});

  final MissionControlWave wave;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return _SectionCard(
      title: l10n.isRussian ? 'Состояние и доказательства' : 'State & evidence',
      subtitle: l10n.isRussian
          ? 'Проекция управляемого Wave Engine; не редактируется вручную.'
          : 'Projected from the governed Wave Engine; never manually edited.',
      child: _FactWrap(
        facts: [
          ('Gate', _humanize(wave.gateState)),
          ('Control', _humanize(wave.controlState)),
          ('Failure policy', _humanize(wave.failurePolicyState)),
          ('Legacy status', _humanize(wave.legacyStatus)),
          ('Next action', _humanize(wave.nextAction)),
          ('Created', _formatTimestamp(wave.createdAt)),
          ('Updated', _formatTimestamp(wave.updatedAt)),
          if (wave.launchedAt.isNotEmpty)
            ('Launched', _formatTimestamp(wave.launchedAt)),
          if (wave.completedAt.isNotEmpty)
            ('Completed', _formatTimestamp(wave.completedAt)),
        ],
        notes: [
          if (wave.controlReason.isNotEmpty) wave.controlReason,
          if (wave.failureReason.isNotEmpty) wave.failureReason,
        ],
      ),
    );
  }
}

class _LinkedLifecyclePanel extends StatelessWidget {
  const _LinkedLifecyclePanel({required this.wave});

  final MissionControlWave wave;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return _SectionCard(
      title: l10n.isRussian
          ? 'Проверка, приёмка и ремонт'
          : 'Review, acceptance & repair',
      child: Wrap(
        spacing: 10,
        runSpacing: 10,
        children: [
          _LinkedStateCard(
            title: 'Implementation review',
            state: wave.review.state,
            detail: _sessionLabel(wave.review),
          ),
          _LinkedStateCard(
            title: 'Acceptance',
            state: wave.acceptance.state,
            detail: _sessionLabel(wave.acceptance),
          ),
          _LinkedStateCard(
            title: 'Repair',
            state: wave.repair.state,
            detail: [
              if (wave.repair.localSessionId > 0)
                'Netrunner #${wave.repair.localSessionId}',
              if (wave.repair.workerId > 0) 'worker ${wave.repair.workerId}',
              '${wave.repair.attemptCount} attempts',
            ].join(' · '),
          ),
        ],
      ),
    );
  }
}

class _WorkersPanel extends StatelessWidget {
  const _WorkersPanel({required this.workers});

  final List<MissionControlWorker> workers;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return _SectionCard(
      title: '${l10n.workers} (${workers.length})',
      child: workers.isEmpty
          ? Text(l10n.isRussian ? 'Воркеров нет.' : 'No workers recorded.')
          : Column(
              children: [
                for (var index = 0; index < workers.length; index++) ...[
                  _WorkerRow(worker: workers[index]),
                  if (index != workers.length - 1) const Divider(height: 17),
                ],
              ],
            ),
    );
  }
}

class _WorkerRow extends StatelessWidget {
  const _WorkerRow({required this.worker});

  final MissionControlWorker worker;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final identity = worker.localSessionId > 0
        ? 'Netrunner #${worker.localSessionId}'
        : worker.sessionId > 0
        ? 'Session ${worker.sessionId}'
        : 'Worker ${worker.workerId}';
    final provider = [
      worker.backend,
      worker.model,
      worker.reasoning,
    ].where((value) => value.isNotEmpty).join(' · ');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(
              identity,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            _StateBadge(
              label: _humanize(worker.status),
              tone: worker.failureReason.isNotEmpty
                  ? _BadgeTone.danger
                  : _BadgeTone.neutral,
            ),
            if (worker.outcome.isNotEmpty)
              _StateBadge(
                label: _humanize(worker.outcome),
                tone: _BadgeTone.primary,
              ),
            if (worker.retryPending)
              const _StateBadge(
                label: 'retry pending',
                tone: _BadgeTone.warning,
              ),
          ],
        ),
        if (provider.isNotEmpty) ...[
          const SizedBox(height: 5),
          Text(provider, style: theme.textTheme.bodySmall),
        ],
        if (worker.failureReason.isNotEmpty) ...[
          const SizedBox(height: 5),
          Text(
            worker.failureReason,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.error,
            ),
          ),
        ],
      ],
    );
  }
}

class _WaveActionsPanel extends StatelessWidget {
  const _WaveActionsPanel({required this.wave, required this.onRunAction});

  final MissionControlWave wave;
  final ValueChanged<MissionControlActionCapability> onRunAction;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return _SectionCard(
      title: l10n.isRussian ? 'Управление волной' : 'Wave controls',
      subtitle: l10n.isRussian
          ? 'Доступность приходит из API; обход state machine запрещён.'
          : 'Capabilities come from the API; the state machine is never bypassed.',
      child: Wrap(
        spacing: 10,
        runSpacing: 12,
        children: [
          for (final capability in wave.actionCapabilities.values)
            SizedBox(
              width: 220,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Tooltip(
                    message: capability.enabled
                        ? _actionLabel(l10n, capability.action)
                        : capability.disabledReason,
                    child: OutlinedButton(
                      key: ValueKey(
                        'wave-${wave.waveId}-action-${capability.action}',
                      ),
                      onPressed: capability.enabled
                          ? () => onRunAction(capability)
                          : null,
                      child: Text(_actionLabel(l10n, capability.action)),
                    ),
                  ),
                  if (!capability.enabled) ...[
                    const SizedBox(height: 4),
                    Text(
                      capability.disabledReason,
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.title, required this.child, this.subtitle});

  final String title;
  final String? subtitle;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              title,
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w900,
              ),
            ),
            if (subtitle != null) ...[
              const SizedBox(height: 3),
              Text(
                subtitle!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            const SizedBox(height: 14),
            child,
          ],
        ),
      ),
    );
  }
}

class _FactWrap extends StatelessWidget {
  const _FactWrap({required this.facts, this.notes = const []});

  final List<(String, String)> facts;
  final List<String> notes;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          spacing: 10,
          runSpacing: 10,
          children: [
            for (final fact in facts)
              Container(
                constraints: const BoxConstraints(minWidth: 150),
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerLowest,
                  border: Border.all(color: theme.dividerColor),
                  borderRadius: BorderRadius.circular(7),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      fact.$1,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      fact.$2,
                      style: theme.textTheme.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
        for (final note in notes) ...[
          const SizedBox(height: 10),
          Text(note, style: theme.textTheme.bodySmall),
        ],
      ],
    );
  }
}

class _LinkedStateCard extends StatelessWidget {
  const _LinkedStateCard({
    required this.title,
    required this.state,
    required this.detail,
  });

  final String title;
  final String state;
  final String detail;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 210,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(color: Theme.of(context).dividerColor),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: const TextStyle(fontWeight: FontWeight.w800)),
          const SizedBox(height: 8),
          _StateBadge(label: _humanize(state), tone: _BadgeTone.neutral),
          if (detail.isNotEmpty) ...[
            const SizedBox(height: 7),
            Text(detail, style: Theme.of(context).textTheme.bodySmall),
          ],
        ],
      ),
    );
  }
}

enum _BadgeTone { neutral, primary, success, warning, danger }

class _StateBadge extends StatelessWidget {
  const _StateBadge({required this.label, required this.tone});

  final String label;
  final _BadgeTone tone;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (background, foreground) = switch (tone) {
      _BadgeTone.neutral => (
        scheme.surfaceContainerHighest,
        scheme.onSurfaceVariant,
      ),
      _BadgeTone.primary => (
        scheme.primaryContainer,
        scheme.onPrimaryContainer,
      ),
      _BadgeTone.success => (const Color(0xFFDCFCE7), const Color(0xFF166534)),
      _BadgeTone.warning => (const Color(0xFFFEF3C7), const Color(0xFF92400E)),
      _BadgeTone.danger => (scheme.errorContainer, scheme.onErrorContainer),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(99),
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

class _AttentionBanner extends StatelessWidget {
  const _AttentionBanner({
    required this.icon,
    required this.title,
    required this.message,
    this.danger = false,
    this.compact = false,
  });

  final IconData icon;
  final String title;
  final String message;
  final bool danger;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final foreground = danger ? scheme.onErrorContainer : scheme.onSurface;
    return Container(
      padding: EdgeInsets.all(compact ? 9 : 12),
      decoration: BoxDecoration(
        color: danger ? scheme.errorContainer : scheme.secondaryContainer,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(
          color: danger
              ? scheme.error.withValues(alpha: 0.45)
              : scheme.outlineVariant,
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: compact ? 17 : 20, color: foreground),
          const SizedBox(width: 9),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: TextStyle(
                    color: foreground,
                    fontWeight: FontWeight.w900,
                    fontSize: compact ? 11 : null,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  message,
                  style: TextStyle(
                    color: foreground,
                    fontSize: compact ? 11 : null,
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

class _IconFact extends StatelessWidget {
  const _IconFact({
    required this.icon,
    required this.text,
    this.danger = false,
  });

  final IconData icon;
  final String text;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final color = danger
        ? Theme.of(context).colorScheme.error
        : Theme.of(context).colorScheme.onSurfaceVariant;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 15, color: color),
        const SizedBox(width: 4),
        Text(text, style: TextStyle(color: color, fontSize: 12)),
      ],
    );
  }
}

class _EmptyFilterState extends StatelessWidget {
  const _EmptyFilterState({required this.filter});

  final MissionControlWaveFilter filter;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.filter_alt_off_outlined, size: 42),
            const SizedBox(height: 12),
            Text(
              filter == MissionControlWaveFilter.all
                  ? (l10n.isRussian
                        ? 'Для проекта ещё нет волн.'
                        : 'No waves are recorded for this project.')
                  : (l10n.isRussian
                        ? 'Нет волн для выбранного фильтра.'
                        : 'No waves match this filter.'),
            ),
          ],
        ),
      ),
    );
  }
}

class _MissionControlLoadError extends StatelessWidget {
  const _MissionControlLoadError({required this.error, required this.onRetry});

  final Object error;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _AttentionBanner(
              icon: Icons.cloud_off_outlined,
              title: l10n.isRussian
                  ? 'Mission Control недоступен'
                  : 'Mission Control is unavailable',
              message: error.toString(),
              danger: true,
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: Text(l10n.tryAgain),
            ),
          ],
        ),
      ),
    );
  }
}

String _sessionLabel(MissionControlLinkedSession session) {
  if (session.localSessionId > 0) return 'Netrunner #${session.localSessionId}';
  if (session.sessionId > 0) return 'Session ${session.sessionId}';
  return '';
}

String _humanize(String value) {
  if (value.trim().isEmpty) return '—';
  return value.trim().replaceAll('_', ' ');
}

String _formatTimestamp(String value) {
  if (value.trim().isEmpty) return '—';
  final parsed = DateTime.tryParse(value);
  if (parsed == null) return value;
  final utc = parsed.toUtc();
  String two(int number) => number.toString().padLeft(2, '0');
  return '${utc.year}-${two(utc.month)}-${two(utc.day)} '
      '${two(utc.hour)}:${two(utc.minute)} UTC';
}

String _actionLabel(AppLocalizations l10n, String action) {
  final english = switch (action) {
    'initialize' => 'Initialize',
    'launch' => 'Launch',
    'wait' => 'Wait for workers',
    'authorize_repair' => 'Authorize repair',
    'pause' => 'Pause for Architect',
    'resume' => 'Resume',
    'transition_to_acceptance' => 'Start acceptance',
    'complete' => 'Complete wave',
    'cleanup' => 'Clean up',
    _ => _humanize(action),
  };
  if (!l10n.isRussian) return english;
  return switch (action) {
    'initialize' => 'Инициализировать',
    'launch' => 'Запустить',
    'wait' => 'Ждать воркеров',
    'authorize_repair' => 'Разрешить ремонт',
    'pause' => 'Пауза для Архитектора',
    'resume' => 'Продолжить',
    'transition_to_acceptance' => 'Начать приёмку',
    'complete' => 'Завершить волну',
    'cleanup' => 'Очистить',
    _ => english,
  };
}
