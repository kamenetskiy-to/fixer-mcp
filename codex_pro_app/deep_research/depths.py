from __future__ import annotations

from dataclasses import dataclass, asdict
from typing import Dict, Iterable, List, Optional

from ..ui import Option, single_select_items


@dataclass(frozen=True)
class DepthProfile:
    """Represents a preset for deep research cadence and limits."""

    key: str
    label: str
    description: str
    plan_template: str
    time_limit_minutes: int
    max_search_requests: int
    max_queries: int
    checkin_interval_minutes: Optional[int] = None
    yolo_enabled: bool = False

    def to_metadata(self) -> Dict[str, object]:
        data = asdict(self)
        return dict(data)


DEPTH_PROFILES: Dict[str, DepthProfile] = {
    "momentary": DepthProfile(
        key="momentary",
        label="Моментальный (≤10 минут)",
        description="Быстрый сбор ключевых фактов и ссылок. Подходит для проверки гипотез или коротких спринтов.",
        plan_template="plans/momentary.md",
        time_limit_minutes=10,
        max_search_requests=3,
        max_queries=6,
    ),
    "fast": DepthProfile(
        key="fast",
        label="Быстрый (≤25 минут)",
        description="Расширенное погружение с несколькими раундами поиска и анализом 2–3 источников на шаг.",
        plan_template="plans/fast.md",
        time_limit_minutes=25,
        max_search_requests=6,
        max_queries=10,
    ),
    "standard": DepthProfile(
        key="standard",
        label="Средний (≤45 минут)",
        description="Сбалансированное исследование с этапами сверки, синтеза и промежуточных выводов.",
        plan_template="plans/standard.md",
        time_limit_minutes=45,
        max_search_requests=10,
        max_queries=14,
        checkin_interval_minutes=20,
    ),
    "intense": DepthProfile(
        key="intense",
        label="Упоротый (≤90 минут)",
        description="Глубокое копание с насыщенным бэкапом источников, регулярными апдейтами и расширенной аналитикой.",
        plan_template="plans/intense.md",
        time_limit_minutes=90,
        max_search_requests=16,
        max_queries=20,
        checkin_interval_minutes=15,
        yolo_enabled=True,
    ),
}

DEPTH_ORDER: List[str] = ["momentary", "fast", "standard", "intense"]


def list_depths() -> Iterable[DepthProfile]:
    for key in DEPTH_ORDER:
        profile = DEPTH_PROFILES.get(key)
        if profile:
            yield profile


def depth_by_key(key: str) -> DepthProfile:
    profile = DEPTH_PROFILES.get(key)
    if not profile:
        raise KeyError(f"Unknown depth profile '{key}'")
    return profile


def select_depth() -> DepthProfile:
    options: List[Option] = []
    for profile in list_depths():
        description = profile.description
        footer_parts: List[str] = [
            f"лимит времени: {profile.time_limit_minutes} мин",
            f"лимит поисковых запросов: {profile.max_queries}",
            f"лимит обращений к tavily: {profile.max_search_requests}",
        ]
        if profile.checkin_interval_minutes:
            footer_parts.append(f"апдейты каждые {profile.checkin_interval_minutes} мин")
        subtitle = " | ".join(footer_parts)
        label = f"{profile.label}\n   {description}\n   {subtitle}"
        options.append(Option(label, profile.key))

    chosen = single_select_items(
        options,
        title="Выбери глубину Deep Research (стрелки + enter, q — отмена)",
        preselected_value="standard",
    )
    if chosen is None:
        raise KeyboardInterrupt
    return depth_by_key(chosen)
