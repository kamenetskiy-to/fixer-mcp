"""Terminal selection primitives used by the Fixer launcher."""

from __future__ import annotations

import curses
from typing import Any, Iterable, List, Optional


class Option:
    def __init__(self, label: str, value: Any = None, *, disabled: bool = False, is_header: bool = False):
        self.label = label
        self.value = value if value is not None else label
        self.disabled = disabled
        self.is_header = is_header


def _render_option(name: str, selected: bool, active: bool) -> str:
    marker = ">" if active else " "
    checkbox = "x" if selected else " "
    return f"{marker} [{checkbox}] {name}"


def _viewport(cursor: int, option_count: int, max_lines: int) -> tuple[int, int]:
    if option_count <= max_lines:
        return 0, option_count
    start = min(max(cursor - max_lines + 1, 0), option_count - max_lines)
    end = start + max_lines
    return start, end


def multi_select_items(
    options: Iterable[Option],
    *,
    title: str,
    preselected_values: Optional[Iterable[Any]] = None,
) -> Optional[List[Any]]:
    option_list: List[Option] = list(options)
    if not option_list:
        return []

    selected = {idx: False for idx in range(len(option_list))}
    if preselected_values is not None:
        initial = list(preselected_values)
        for idx, opt in enumerate(option_list):
            if any(opt.value == v for v in initial) and not (opt.disabled or opt.is_header):
                selected[idx] = True
    cursor = 0

    def _main(stdscr) -> List[Any]:
        nonlocal cursor
        curses.curs_set(0)
        while True:
            stdscr.erase()
            max_y, max_x = stdscr.getmaxyx()
            stdscr.addnstr(0, 0, title, max_x - 1)

            max_visible = max(1, max_y - 3)
            start, end = _viewport(cursor, len(option_list), max_visible)
            for row, idx in enumerate(range(start, end), start=1):
                opt = option_list[idx]
                if opt.is_header:
                    header = f"-- {opt.label} --"
                    stdscr.addnstr(row, 0, header, max_x - 1)
                else:
                    label = opt.label
                    if opt.disabled:
                        label = f"{label} (unavailable)"
                    line = _render_option(label, selected[idx], idx == cursor)
                    stdscr.addnstr(row, 0, line, max_x - 1)

            footer = f"{sum(selected.values())} selected"
            stdscr.addnstr(max_y - 1, 0, footer, max_x - 1)
            stdscr.refresh()

            key = stdscr.getch()
            if key in (curses.KEY_UP, ord("k")):
                cursor = (cursor - 1) % len(option_list)
            elif key in (curses.KEY_DOWN, ord("j")):
                cursor = (cursor + 1) % len(option_list)
            elif key == ord(" "):
                if not (option_list[cursor].disabled or option_list[cursor].is_header):
                    selected[cursor] = not selected[cursor]
            elif key in (ord("a"), ord("A")):
                make_active = any(
                    (not flag) and (not option_list[idx].disabled) and (not option_list[idx].is_header)
                    for idx, flag in selected.items()
                )
                for idx in selected:
                    if not (option_list[idx].disabled or option_list[idx].is_header):
                        selected[idx] = make_active
            elif key in (10, 13, curses.KEY_ENTER):
                return [option_list[idx].value for idx, flag in selected.items() if flag and not option_list[idx].is_header]
            elif key in (27, ord("q"), ord("Q")):
                raise KeyboardInterrupt

    try:
        return curses.wrapper(_main)
    except KeyboardInterrupt:
        return None


def single_select_items(
    options: Iterable[Option],
    *,
    title: str,
    preselected_value: Optional[Any] = None,
) -> Optional[Any]:
    option_list: List[Option] = list(options)
    if not option_list:
        return None

    def _preferred_index() -> int:
        if preselected_value is not None:
            for idx, opt in enumerate(option_list):
                if opt.is_header or opt.disabled:
                    continue
                if opt.value == preselected_value:
                    return idx
        for idx, opt in enumerate(option_list):
            if opt.is_header or opt.disabled:
                continue
            return idx
        return 0

    cursor = _preferred_index()

    def _main(stdscr) -> Any:
        nonlocal cursor
        curses.curs_set(0)
        while True:
            stdscr.erase()
            max_y, max_x = stdscr.getmaxyx()
            stdscr.addnstr(0, 0, title, max_x - 1)

            max_visible = max(1, max_y - 3)
            start, end = _viewport(cursor, len(option_list), max_visible)
            for row, idx in enumerate(range(start, end), start=1):
                opt = option_list[idx]
                if opt.is_header:
                    header = f"-- {opt.label} --"
                    stdscr.addnstr(row, 0, header, max_x - 1)
                else:
                    selected = idx == cursor
                    label = opt.label
                    if opt.disabled:
                        label = f"{label} (unavailable)"
                        selected = False
                    line = _render_option(label, selected, idx == cursor)
                    stdscr.addnstr(row, 0, line, max_x - 1)

            footer = "Enter confirm, q cancel"
            stdscr.addnstr(max_y - 1, 0, footer, max_x - 1)
            stdscr.refresh()

            key = stdscr.getch()
            if key in (curses.KEY_UP, ord("k")):
                cursor = (cursor - 1) % len(option_list)
                while option_list[cursor].is_header or option_list[cursor].disabled:
                    cursor = (cursor - 1) % len(option_list)
            elif key in (curses.KEY_DOWN, ord("j")):
                cursor = (cursor + 1) % len(option_list)
                while option_list[cursor].is_header or option_list[cursor].disabled:
                    cursor = (cursor + 1) % len(option_list)
            elif key in (10, 13, curses.KEY_ENTER):
                opt = option_list[cursor]
                if opt.is_header or opt.disabled:
                    continue
                return opt.value
            elif key in (27, ord("q"), ord("Q")):
                raise KeyboardInterrupt

    try:
        return curses.wrapper(_main)
    except KeyboardInterrupt:
        return None

