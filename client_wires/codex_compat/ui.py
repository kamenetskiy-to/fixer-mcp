"""Terminal selection primitives used by the Fixer launcher."""

from __future__ import annotations

import curses
from typing import Any, Callable, Iterable, List, Optional


from client_wires.fixer_wire_navigation import BACK_VALUE, BackNavigation


class Option:
    def __init__(self, label: str, value: Any = None, *, disabled: bool = False, is_header: bool = False, instant: bool = False):
        self.label = label
        self.value = value if value is not None else label
        self.disabled = disabled
        self.is_header = is_header
        self.instant = instant


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


def _tree_action_for_key(key: int, option: Option) -> str | None:
    if option.is_header or option.disabled:
        return None
    value = option.value
    if not (isinstance(value, int) or (isinstance(value, str) and value.lstrip("-").isdigit())):
        return None
    doc_id = int(value)
    if key in (ord("t"), ord("T")):
        return f"branch:{doc_id}"
    if key == curses.KEY_RIGHT and "▸" in option.label:
        return f"expand:{doc_id}"
    if key == curses.KEY_LEFT and "▾" in option.label:
        return f"collapse:{doc_id}"
    return None


def multi_select_items(
    options: Iterable[Option],
    *,
    title: str,
    preselected_values: Optional[Iterable[Any]] = None,
    initial_cursor_value: Optional[Any] = None,
    refresh_options: Optional[Callable[[List[Any]], Optional[List[Option]]]] = None,
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
    if initial_cursor_value is not None:
        for idx, opt in enumerate(option_list):
            if not opt.is_header and not opt.disabled and opt.value == initial_cursor_value:
                cursor = idx
                break

    def _rebuild_options() -> None:
        """Live-rebuild labels/order from the current selection when a refresh
        callback is supplied (used by tree pickers with derived counters)."""
        nonlocal option_list, selected, cursor
        if refresh_options is None:
            return
        current_values = [
            option_list[idx].value
            for idx, flag in selected.items()
            if flag and not option_list[idx].is_header and not option_list[idx].disabled
        ]
        refreshed = refresh_options(current_values)
        if refreshed is None:
            return
        cursor_value = option_list[cursor].value if option_list else None
        new_list = list(refreshed)
        new_selected = {idx: False for idx in range(len(new_list))}
        for idx, opt in enumerate(new_list):
            if not (opt.is_header or opt.disabled) and any(
                opt.value == value for value in current_values
            ):
                new_selected[idx] = True
        option_list = new_list
        selected = new_selected
        if cursor_value is not None:
            for idx, opt in enumerate(option_list):
                if not opt.is_header and not opt.disabled and opt.value == cursor_value:
                    cursor = idx
                    break

    def _main(stdscr) -> List[Any]:
        nonlocal cursor, option_list, selected
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
                    if option_list[cursor].instant:
                        return [option_list[cursor].value]
                    selected[cursor] = not selected[cursor]
                    _rebuild_options()
            elif key in (ord("a"), ord("A")):
                make_active = any(
                    (not flag) and (not option_list[idx].disabled) and (not option_list[idx].is_header)
                    for idx, flag in selected.items()
                )
                for idx in selected:
                    if not (option_list[idx].disabled or option_list[idx].is_header):
                        selected[idx] = make_active
                _rebuild_options()
            elif key in (ord("t"), ord("T"), curses.KEY_RIGHT, curses.KEY_LEFT):
                cur_idx = cursor
                if cur_idx >= len(option_list):
                    continue
                cur_opt = option_list[cur_idx]
                action = _tree_action_for_key(key, cur_opt)
                if action is not None:
                    selected_values = [
                        option_list[idx].value
                        for idx, flag in selected.items()
                        if flag and not option_list[idx].is_header
                    ]
                    return [*selected_values, action]
            elif key in (10, 13, curses.KEY_ENTER):
                return [option_list[idx].value for idx, flag in selected.items() if flag and not option_list[idx].is_header]
            elif key in (127, 8, curses.KEY_BACKSPACE):
                for idx, opt in enumerate(option_list):
                    if not opt.is_header and not opt.disabled:
                        return BACK_VALUE
                return BACK_VALUE
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
            elif key in (127, 8, curses.KEY_BACKSPACE):
                return BACK_VALUE
            elif key in (27, ord("q"), ord("Q")):
                raise KeyboardInterrupt

    try:
        return curses.wrapper(_main)
    except KeyboardInterrupt:
        return None
