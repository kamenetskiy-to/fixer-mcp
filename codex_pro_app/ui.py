from __future__ import annotations

import curses
from dataclasses import dataclass, field
from typing import Any, Iterable, List, Optional, Set


class Option:
    def __init__(self, label: str, value: Any = None, *, disabled: bool = False, is_header: bool = False):
        self.label = label
        self.value = value if value is not None else label
        self.disabled = disabled
        self.is_header = is_header


@dataclass
class TreeNode:
    name: str
    path: Optional[str]
    is_dir: bool
    parent: Optional["TreeNode"] = None
    children: List["TreeNode"] = field(default_factory=list)
    expanded: bool = False
    selected: bool = False
    depth: int = 0


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


def multi_select(
    options: Iterable[str],
    *,
    title: str,
    preselected: Optional[Iterable[str]] = None,
) -> Optional[List[str]]:
    option_list = list(options)
    if not option_list:
        return []

    selected = {idx: False for idx in range(len(option_list))}
    if preselected:
        initial = set(preselected)
        for idx, name in enumerate(option_list):
            if name in initial:
                selected[idx] = True
    cursor = 0

    def _main(stdscr) -> List[str]:
        nonlocal cursor
        curses.curs_set(0)
        while True:
            stdscr.erase()
            max_y, max_x = stdscr.getmaxyx()
            stdscr.addnstr(0, 0, title, max_x - 1)

            max_visible = max(1, max_y - 3)
            start, end = _viewport(cursor, len(option_list), max_visible)
            for row, idx in enumerate(range(start, end), start=1):
                line = _render_option(option_list[idx], selected[idx], idx == cursor)
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
                selected[cursor] = not selected[cursor]
            elif key in (ord("a"), ord("A")):
                make_active = any(not flag for flag in selected.values())
                for idx in selected:
                    selected[idx] = make_active
            elif key in (10, 13, curses.KEY_ENTER):
                return [option_list[idx] for idx, flag in selected.items() if flag]
            elif key in (27, ord("q"), ord("Q")):
                raise KeyboardInterrupt

    try:
        return curses.wrapper(_main)
    except KeyboardInterrupt:
        return None


def multi_select_tree(
    files: Iterable[str],
    *,
    title: str,
    preselected: Optional[Iterable[str]] = None,
    force_expand_dirs: Optional[Iterable[str]] = None,
    reverse_sort_dirs: Optional[Iterable[str]] = None,
) -> Optional[List[str]]:
    file_list = sorted({path for path in files})
    if not file_list:
        return []

    preselected_set: Set[str] = set(preselected or [])
    force_expand_set: Set[str] = set(force_expand_dirs or [])
    reverse_sort_set: Set[str] = set(reverse_sort_dirs or [])

    def _build_tree(paths: List[str]) -> TreeNode:
        root = TreeNode(name="", path=None, is_dir=True, expanded=True, depth=0)
        for rel_path in paths:
            parts = rel_path.split("/")
            node = root
            for idx, part in enumerate(parts):
                is_last = idx == len(parts) - 1
                child: Optional[TreeNode] = None
                for existing in node.children:
                    if existing.name == part and existing.is_dir == (not is_last):
                        child = existing
                        break
                if child is None:
                    child = TreeNode(
                        name=part,
                        path="/".join(parts[: idx + 1]),
                        is_dir=not is_last,
                        parent=node,
                        depth=node.depth + 1,
                    )
                    node.children.append(child)
                node = child

        def _sort(node: TreeNode) -> None:
            reverse = node.path in reverse_sort_set if node.path else False
            node.children.sort(key=lambda n: (not n.is_dir, n.name.lower()), reverse=reverse)
            for child in node.children:
                child.parent = node
                child.depth = node.depth + 1
                if child.is_dir:
                    _sort(child)

        _sort(root)
        return root

    root = _build_tree(file_list)

    def _mark_preselected(node: TreeNode) -> None:
        for child in node.children:
            if child.is_dir:
                _mark_preselected(child)
                if child.path and child.path in force_expand_set:
                    child.expanded = True
                    parent = child.parent
                    while parent:
                        parent.expanded = True
                        parent = parent.parent
            else:
                if child.path and child.path in preselected_set:
                    child.selected = True
                    parent = child.parent
                    while parent:
                        parent.expanded = True
                        parent = parent.parent

    _mark_preselected(root)

    def _visible_nodes() -> List[TreeNode]:
        visible: List[TreeNode] = []

        def _walk(node: TreeNode) -> None:
            for child in node.children:
                visible.append(child)
                if child.is_dir and child.expanded:
                    _walk(child)

        _walk(root)
        return visible

    cursor = 0

    def _file_nodes() -> List[TreeNode]:
        files: List[TreeNode] = []
        stack = [root]
        while stack:
            node = stack.pop()
            for child in node.children:
                if child.is_dir:
                    stack.append(child)
                else:
                    files.append(child)
        return files

    def _selected_count() -> int:
        return sum(1 for node in _file_nodes() if node.selected)

    def _toggle_all() -> None:
        selectable = _file_nodes()
        if not selectable:
            return
        make_active = any(not node.selected for node in selectable)
        for node in selectable:
            node.selected = make_active

    def _main(stdscr) -> List[str]:
        nonlocal cursor
        curses.curs_set(0)
        color_enabled = False
        dir_attr = curses.A_NORMAL
        if curses.has_colors():
            try:
                curses.start_color()
            except curses.error:
                pass
            try:
                curses.use_default_colors()
            except curses.error:
                pass
            initialized_pair = False
            try:
                curses.init_pair(1, curses.COLOR_CYAN, -1)
                initialized_pair = True
            except curses.error:
                try:
                    curses.init_pair(1, curses.COLOR_CYAN, 0)
                    initialized_pair = True
                except curses.error:
                    pass
            if initialized_pair:
                try:
                    dir_attr = curses.color_pair(1)
                    color_enabled = True
                except curses.error:
                    dir_attr = curses.A_NORMAL

        while True:
            stdscr.erase()
            max_y, max_x = stdscr.getmaxyx()
            stdscr.addnstr(0, 0, title, max_x - 1)

            visible = _visible_nodes()
            if not visible:
                stdscr.addnstr(1, 0, "No files available.", max_x - 1)
                stdscr.refresh()
                key = stdscr.getch()
                if key in (27, ord("q"), ord("Q")):
                    raise KeyboardInterrupt
                continue

            cursor = max(0, min(cursor, len(visible) - 1))

            max_visible = max(1, max_y - 3)
            start, end = _viewport(cursor, len(visible), max_visible)
            for row, idx in enumerate(range(start, end), start=1):
                node = visible[idx]
                marker = ">" if idx == cursor else " "
                indent = "  " * max(node.depth - 1, 0)
                if node.is_dir:
                    icon = "[-]" if node.expanded else "[+]"
                    line = f"{marker} {indent}{icon} {node.name}/"
                    attr = dir_attr if color_enabled else curses.A_NORMAL
                    stdscr.addnstr(row, 0, line, max_x - 1, attr)
                else:
                    checkbox = "x" if node.selected else " "
                    line = f"{marker} {indent}[{checkbox}] {node.name}"
                    stdscr.addnstr(row, 0, line, max_x - 1)

            footer = f"{_selected_count()} files selected"
            stdscr.addnstr(max_y - 1, 0, footer, max_x - 1)
            stdscr.refresh()

            key = stdscr.getch()
            if key in (curses.KEY_UP, ord("k")):
                cursor = (cursor - 1) % len(visible)
            elif key in (curses.KEY_DOWN, ord("j")):
                cursor = (cursor + 1) % len(visible)
            elif key in (curses.KEY_RIGHT, ord("l")):
                node = visible[cursor]
                if node.is_dir and not node.expanded:
                    node.expanded = True
            elif key in (curses.KEY_LEFT, ord("h")):
                node = visible[cursor]
                if node.is_dir and node.expanded:
                    node.expanded = False
                elif node.parent and node.parent.parent is not None:
                    parent = node.parent
                    ancestors = _visible_nodes()
                    try:
                        cursor = ancestors.index(parent)
                    except ValueError:
                        cursor = cursor
            elif key == ord(" "):
                node = visible[cursor]
                if node.is_dir:
                    node.expanded = not node.expanded
                else:
                    node.selected = not node.selected
            elif key in (ord("a"), ord("A")):
                _toggle_all()
            elif key in (10, 13, curses.KEY_ENTER):
                result_paths: List[str] = []
                for node in _file_nodes():
                    if node.selected and node.path:
                        result_paths.append(node.path)
                return sorted(result_paths)
            elif key in (27, ord("q"), ord("Q")):
                raise KeyboardInterrupt

    try:
        return curses.wrapper(_main)
    except KeyboardInterrupt:
        return None


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
                # toggle only non-disabled, non-header
                make_active = any((not flag) and (not option_list[idx].disabled) and (not option_list[idx].is_header) for idx, flag in selected.items())
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
