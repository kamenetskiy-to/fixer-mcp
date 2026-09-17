"""DRY global back-navigation for the Fixer TUI.

Every interactive stage must return to the *previous* stage on backspace,
restoring its prior state. This module provides a tiny stack-based
navigator so the wire never needs ad-hoc BackNavigation handling
in each caller.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable


BACK_VALUE = "__back__"


class BackNavigation(Exception):
    """Requested return to the previous TUI stage."""


@dataclass
class TuiStage:
    name: str
    run: Callable[[], Any]
    on_back: str = "pop"


@dataclass
class FixerTuiNavigator:
    stages: list[TuiStage] = field(default_factory=list)
    state: dict[str, Any] = field(default_factory=dict)
    index: int = 0

    def add(self, name: str, run: Callable[[], Any]) -> None:
        self.stages.append(TuiStage(name=name, run=run))

    def run(self) -> dict[str, Any]:
        idx = 0
        while idx < len(self.stages):
            stage = self.stages[idx]
            # For the top-level role router: skip non-matching branches
            # without consuming them as stages, so BackNavigation lands
            # on the immediate previous stage.
            if stage.name in ("netrunner", "fixer", "overseer") and "role" in self.state:
                picked = str(self.state["role"])
                if picked == "netrunner" and stage.name != "netrunner":
                    idx += 1
                    continue
                if picked == "fixer" and stage.name != "fixer":
                    idx += 1
                    continue
                if picked == "overseer" and stage.name != "overseer":
                    idx += 1
                    continue
                if picked not in ("netrunner", "fixer", "overseer"):
                    idx += 1
                    continue
            try:
                result = stage.run()
                self.state[stage.name] = result
                idx += 1
            except BackNavigation:
                if idx == 0:
                    raise
                for j in range(idx, len(self.stages)):
                    self.state.pop(self.stages[j].name, None)
                idx -= 1
                # skip back over non-matching top-level branches
                while idx >= 0 and self.stages[idx].name in ("netrunner", "fixer", "overseer") and "role" in self.state:
                    picked = str(self.state["role"])
                    if (picked == "netrunner" and self.stages[idx].name == "netrunner") or (
                        picked == "fixer" and self.stages[idx].name == "fixer"
                    ) or (picked == "overseer" and self.stages[idx].name == "overseer"):
                        break
                    idx -= 1
                if idx < 0:
                    raise BackNavigation()
                continue
            except Exception as exc:
                if exc.__class__.__name__ == "BackNavigation":
                    if idx == 0:
                        raise BackNavigation() from exc
                    for j in range(idx, len(self.stages)):
                        self.state.pop(self.stages[j].name, None)
                    idx -= 1
                    while idx >= 0 and self.stages[idx].name in ("netrunner", "fixer", "overseer") and "role" in self.state:
                        picked = str(self.state["role"])
                        if (picked == "netrunner" and self.stages[idx].name == "netrunner") or (
                            picked == "fixer" and self.stages[idx].name == "fixer"
                        ) or (picked == "overseer" and self.stages[idx].name == "overseer"):
                            break
                        idx -= 1
                    if idx < 0:
                        raise BackNavigation()
                    continue
                raise
        return dict(self.state)

    def reset(self) -> None:
        self.state.clear()
        self.index = 0
