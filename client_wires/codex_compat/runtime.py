"""Runtime helpers for Playwright MCP and SQLite MCP setup."""

from __future__ import annotations

from datetime import datetime
import os
from pathlib import Path
import sys
import textwrap
from typing import Dict, List, Optional, Tuple

from client_wires.codex_compat.ui import Option, single_select_items


PLAYWRIGHT_MCP_NAME = "playwright"
PLAYWRIGHT_MODE_ENV = "CODEX_PRO_PLAYWRIGHT_MODE"
PLAYWRIGHT_CHROME_PROFILE_ENV = "CODEX_PRO_PLAYWRIGHT_CHROME_PROFILE"
PLAYWRIGHT_CHROME_VIEWPORT_ENV = "CODEX_PRO_PLAYWRIGHT_CHROME_VIEWPORT"
PLAYWRIGHT_MODE_DEFAULT = "default"
PLAYWRIGHT_MODE_HEADLESS = "headless"
PLAYWRIGHT_MODE_CHROME = "chrome"
PLAYWRIGHT_MODE_VALUES = {
    PLAYWRIGHT_MODE_DEFAULT,
    PLAYWRIGHT_MODE_HEADLESS,
    PLAYWRIGHT_MODE_CHROME,
}
PLAYWRIGHT_CHROME_PROFILE_ROOT = Path.home() / ".codex" / "browser-profiles" / "playwright-chrome"
PLAYWRIGHT_CHROME_PROFILE_DEFAULT = (
    PLAYWRIGHT_CHROME_PROFILE_ROOT
    / "sessions"
    / f"{datetime.now().strftime('%Y%m%d-%H%M%S')}-{os.getpid()}"
)
DEFAULT_SQLITE_DB_NAME = "db/dev.sqlite3"


def normalize_playwright_runtime_mode(raw_mode: Optional[str]) -> Optional[str]:
    if raw_mode is None:
        return None
    mode = raw_mode.strip().lower()
    if not mode:
        return None
    aliases = {
        "existing": PLAYWRIGHT_MODE_DEFAULT,
        "config": PLAYWRIGHT_MODE_DEFAULT,
        "current": PLAYWRIGHT_MODE_DEFAULT,
        "headed": PLAYWRIGHT_MODE_CHROME,
        "visible": PLAYWRIGHT_MODE_CHROME,
        "chromium": PLAYWRIGHT_MODE_CHROME,
        "chrome-headed": PLAYWRIGHT_MODE_CHROME,
        "chrome_visible": PLAYWRIGHT_MODE_CHROME,
    }
    mode = aliases.get(mode, mode)
    if mode not in PLAYWRIGHT_MODE_VALUES:
        print(
            f"[warning] unsupported {PLAYWRIGHT_MODE_ENV}={raw_mode!r}; using existing Playwright config.",
            file=sys.stderr,
        )
        return PLAYWRIGHT_MODE_DEFAULT
    return mode


def playwright_chrome_profile_dir() -> Path:
    raw_path = os.environ.get(PLAYWRIGHT_CHROME_PROFILE_ENV)
    if raw_path and raw_path.strip():
        return Path(raw_path).expanduser()
    return PLAYWRIGHT_CHROME_PROFILE_DEFAULT


def playwright_chrome_viewport() -> Optional[str]:
    raw_viewport = os.environ.get(PLAYWRIGHT_CHROME_VIEWPORT_ENV)
    if raw_viewport and raw_viewport.strip():
        return raw_viewport.strip()
    return None


def playwright_chrome_cdp_wrapper_path() -> Path:
    return Path(__file__).with_name("playwright_chrome_cdp.py")


def playwright_command_and_args_for_mode(mode: str) -> Optional[Tuple[str, List[str]]]:
    if mode == PLAYWRIGHT_MODE_DEFAULT:
        return None
    if mode == PLAYWRIGHT_MODE_HEADLESS:
        return "npx", ["-y", "@playwright/mcp@latest", "--isolated", "--headless"]
    if mode == PLAYWRIGHT_MODE_CHROME:
        args = [
            str(playwright_chrome_cdp_wrapper_path()),
            "--user-data-dir",
            str(playwright_chrome_profile_dir()),
        ]
        viewport = playwright_chrome_viewport()
        if viewport:
            args.extend(["--viewport-size", viewport])
        return sys.executable, args
    return None


def apply_playwright_runtime_mode(
    available_servers: Dict[str, Dict[str, object]],
    selected_servers: Dict[str, Dict[str, object]],
    *,
    mode: Optional[str],
) -> Optional[str]:
    normalized = normalize_playwright_runtime_mode(mode)
    if normalized in (None, PLAYWRIGHT_MODE_DEFAULT):
        return normalized
    if PLAYWRIGHT_MCP_NAME not in selected_servers:
        return normalized
    server_cfg = available_servers.get(PLAYWRIGHT_MCP_NAME)
    if not isinstance(server_cfg, dict):
        return normalized

    command_and_args = playwright_command_and_args_for_mode(normalized)
    if command_and_args is None:
        return normalized
    command, args = command_and_args

    server_cfg["command"] = command
    server_cfg["args"] = args
    server_cfg["transport"] = "stdio"
    server_cfg["startup_timeout_sec"] = max(int(server_cfg.get("startup_timeout_sec") or 0), 60)
    server_cfg["tool_timeout_sec"] = max(int(server_cfg.get("tool_timeout_sec") or 0), 600)
    server_cfg["timeout"] = max(int(server_cfg.get("timeout") or 0), 600)
    server_cfg["_source"] = "preset_mcp"
    selected_servers[PLAYWRIGHT_MCP_NAME] = server_cfg
    return normalized


def maybe_configure_playwright_runtime(
    selected_servers: Dict[str, Dict[str, object]],
    available_servers: Dict[str, Dict[str, object]],
    *,
    interactive: bool = True,
) -> Optional[str]:
    if PLAYWRIGHT_MCP_NAME not in selected_servers:
        return None

    env_mode = normalize_playwright_runtime_mode(os.environ.get(PLAYWRIGHT_MODE_ENV))
    if env_mode:
        return apply_playwright_runtime_mode(available_servers, selected_servers, mode=env_mode)
    if not interactive:
        return None

    selected = single_select_items(
        [
            Option("Playwright runtime", is_header=True),
            Option("Use existing config", PLAYWRIGHT_MODE_DEFAULT),
            Option("Headless isolated", PLAYWRIGHT_MODE_HEADLESS),
            Option("Chrome headed profile", PLAYWRIGHT_MODE_CHROME),
        ],
        title="Select Playwright runtime (enter confirm, q cancel)",
        preselected_value=PLAYWRIGHT_MODE_DEFAULT,
    )
    if selected is None:
        print("Cancelled.")
        sys.exit(130)
    return apply_playwright_runtime_mode(available_servers, selected_servers, mode=str(selected))


def relative_to_cwd(path: Path, cwd: Path) -> str:
    try:
        return path.relative_to(cwd).as_posix()
    except ValueError:
        return path.as_posix()


def discover_sqlite_files(root: Path, limit: int = 20) -> List[Path]:
    exts = {".sqlite", ".sqlite3", ".db"}
    ignore_dirs = {
        ".git",
        ".idea",
        ".vscode",
        "node_modules",
        "dist",
        "build",
        ".venv",
        ".env",
        ".tox",
        "__pycache__",
        ".mypy_cache",
        ".pytest_cache",
    }
    found: List[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            name
            for name in dirnames
            if name not in ignore_dirs and not name.startswith(".git")
        ]
        for filename in filenames:
            if len(found) >= limit:
                return found
            suffix = Path(filename).suffix.lower()
            if suffix in exts:
                found.append(Path(dirpath) / filename)
                if len(found) >= limit:
                    return found
    return found


def ensure_sqlite_scaffold(cwd: Path) -> Optional[Path]:
    config_path = cwd / "sqliteMCP.toml"
    if config_path.exists():
        return config_path

    def _finalize_with_path(db_path: Path, *, created_db: bool) -> Path:
        config_value = relative_to_cwd(db_path, cwd)
        content = textwrap.dedent(
            f"""\
            # Auto-generated by codex-pro (SQLite MCP)
            [sqlite]
            db_path = "{config_value}"
            create_if_missing = false

            [mcp]
            extra_args = []
            env = {{}}
            """
        ).strip() + "\n"
        try:
            config_path.write_text(content, encoding="utf-8")
        except OSError as exc:
            print(f"Не удалось записать sqliteMCP.toml: {exc}.")
            return config_path
        if created_db:
            print(f"Созданы {config_path} и база {db_path}")
        else:
            print(f"Создан {config_path}; используется существующая база {db_path}")
        return config_path

    candidates = discover_sqlite_files(cwd, limit=20)
    if candidates:
        options: List[Option] = [Option("Найденные SQLite файлы", is_header=True)]
        for path in candidates:
            label = relative_to_cwd(path, cwd)
            options.append(Option(label, path))
        options.append(Option("Создать новую SQLite базу", "create_new"))
        selection = single_select_items(
            options,
            title="Выберите существующую SQLite базу или создайте новую",
        )
        if selection is None:
            print("Отмена настройки SQLite MCP.")
            return None
        if isinstance(selection, Path):
            return _finalize_with_path(selection.resolve(), created_db=False)

    print(
        "\n[SQLite MCP] В корне проекта не найден sqliteMCP.toml — нужно создать базу и конфиг.\n"
        "Введите имя файла для SQLite (относительно корня) или абсолютный путь.\n"
        "Нажмите Enter, чтобы использовать значение по умолчанию."
    )
    default_choice = DEFAULT_SQLITE_DB_NAME
    while True:
        try:
            raw = input(f"Имя файла SQLite [{default_choice}]: ").strip()
        except KeyboardInterrupt:
            print("\nОтмена настройки SQLite MCP.")
            return None
        if not raw:
            raw = default_choice
        expanded = Path(os.path.expandvars(os.path.expanduser(raw)))
        if expanded.is_absolute():
            db_path = expanded
        else:
            db_path = (cwd / expanded).resolve()
        try:
            db_path.parent.mkdir(parents=True, exist_ok=True)
            db_path.touch(exist_ok=True)
        except OSError as exc:
            print(f"Не удалось создать файл базы {db_path}: {exc}. Попробуйте другой путь.")
            continue

        return _finalize_with_path(db_path, created_db=True)


_normalize_playwright_runtime_mode = normalize_playwright_runtime_mode
_playwright_chrome_profile_dir = playwright_chrome_profile_dir
_playwright_chrome_viewport = playwright_chrome_viewport
_playwright_chrome_cdp_wrapper_path = playwright_chrome_cdp_wrapper_path
_playwright_command_and_args_for_mode = playwright_command_and_args_for_mode
_maybe_configure_playwright_runtime = maybe_configure_playwright_runtime
_relative_to_cwd = relative_to_cwd
_discover_sqlite_files = discover_sqlite_files
_ensure_sqlite_scaffold = ensure_sqlite_scaffold

