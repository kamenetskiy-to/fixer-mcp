"""Runtime helpers for Playwright MCP and SQLite MCP setup."""

from __future__ import annotations

import os
from pathlib import Path
import sys
import textwrap
from typing import Dict, List, Optional, Tuple

from client_wires.codex_compat.ui import Option, single_select_items


PLAYWRIGHT_MCP_NAME = "playwright"
PLAYWRIGHT_MESH_MCP_NAME = "playwright-mesh"
PLAYWRIGHT_MESH_TARGET_ENV = "PW_MESH_TARGET"
PLAYWRIGHT_MESH_BROWSER_ENV = "PW_MESH_BROWSER"
PLAYWRIGHT_MESH_DEVICES_ENV = "PW_MESH_DEVICES"
PLAYWRIGHT_MESH_DEFAULT = "default"
PLAYWRIGHT_MODE_ENV = "CODEX_PRO_PLAYWRIGHT_MODE"
PLAYWRIGHT_CHROME_PROFILE_ENV = "CODEX_PRO_PLAYWRIGHT_CHROME_PROFILE"
PLAYWRIGHT_CHROME_VIEWPORT_ENV = "CODEX_PRO_PLAYWRIGHT_CHROME_VIEWPORT"
PLAYWRIGHT_MODE_DEFAULT = "default"
PLAYWRIGHT_MODE_HEADLESS = "headless"
PLAYWRIGHT_MODE_CHROME = "chrome"
PLAYWRIGHT_MODE_HEADLESS_PROFILE = "headless-profile"
PLAYWRIGHT_MODE_VALUES = {
    PLAYWRIGHT_MODE_DEFAULT,
    PLAYWRIGHT_MODE_HEADLESS,
    PLAYWRIGHT_MODE_CHROME,
    PLAYWRIGHT_MODE_HEADLESS_PROFILE,
}
PLAYWRIGHT_CHROME_PROFILE_ROOT = Path.home() / ".codex" / "playwright-profiles"
PLAYWRIGHT_CHROME_PROFILE_DEFAULT = PLAYWRIGHT_CHROME_PROFILE_ROOT / "operator-default"
DEFAULT_SQLITE_DB_NAME = "db/dev.sqlite3"
SQLITE_DB_PATH_ENV = "CODEX_PRO_SQLITE_DB_PATH"


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
        "persistent-headless": PLAYWRIGHT_MODE_HEADLESS_PROFILE,
        "headless-persistent": PLAYWRIGHT_MODE_HEADLESS_PROFILE,
        "profile-headless": PLAYWRIGHT_MODE_HEADLESS_PROFILE,
        "chrome-headless": PLAYWRIGHT_MODE_HEADLESS_PROFILE,
        "headless_profile": PLAYWRIGHT_MODE_HEADLESS_PROFILE,
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


def playwright_persistent_profile_args(*, headless: bool) -> List[str]:
    args = [
        str(playwright_chrome_cdp_wrapper_path()),
        "--user-data-dir",
        str(playwright_chrome_profile_dir()),
    ]
    if headless:
        args.append("--headless")
    else:
        args.append("--shared-context")
    viewport = playwright_chrome_viewport()
    if viewport:
        args.extend(["--viewport-size", viewport])
    return args


def playwright_command_and_args_for_mode(mode: str) -> Optional[Tuple[str, List[str]]]:
    if mode == PLAYWRIGHT_MODE_DEFAULT:
        return None
    if mode == PLAYWRIGHT_MODE_HEADLESS:
        return "npx", ["-y", "@playwright/mcp@latest", "--isolated", "--headless"]
    if mode == PLAYWRIGHT_MODE_CHROME:
        return sys.executable, playwright_persistent_profile_args(headless=False)
    if mode == PLAYWRIGHT_MODE_HEADLESS_PROFILE:
        return sys.executable, playwright_persistent_profile_args(headless=True)
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
            Option("Persistent headless profile", PLAYWRIGHT_MODE_HEADLESS_PROFILE),
        ],
        title="Select Playwright runtime (enter confirm, q cancel)",
        preselected_value=PLAYWRIGHT_MODE_DEFAULT,
    )
    if selected is None:
        print("Cancelled.")
        sys.exit(130)
    return apply_playwright_runtime_mode(available_servers, selected_servers, mode=str(selected))


def playwright_mesh_devices(
    available_servers: Dict[str, Dict[str, object]],
) -> Dict[str, Dict[str, object]]:
    """Load the mesh device matrix from the Playwright Mesh server definition.

    The matrix is resolved from the launcher path recorded in the MCP server entry
    (mesh_browser/devices.json next to pw_mesh_mcp.py), so the picker offers exactly
    the devices the launcher will honour.  PW_MESH_DEVICES overrides it.
    """
    import json

    candidates: List[Path] = []
    env_path = os.environ.get(PLAYWRIGHT_MESH_DEVICES_ENV)
    if env_path and env_path.strip():
        candidates.append(Path(env_path).expanduser())

    cfg = available_servers.get(PLAYWRIGHT_MESH_MCP_NAME)
    if isinstance(cfg, dict):
        raw_command = cfg.get("command")
        raw_args = cfg.get("args") or []
        tokens = [str(raw_command)] if isinstance(raw_command, str) else []
        tokens.extend(str(item) for item in raw_args)
        for token in tokens:
            if token.endswith("pw_mesh_mcp.py"):
                candidates.append(Path(token).expanduser().parent / "devices.json")
                break

    for path in candidates:
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        devices = data.get("devices")
        if isinstance(devices, dict) and devices:
            return {str(key): value for key, value in devices.items() if isinstance(value, dict)}
    return {}


def set_command_flag(args: object, flag: str, value: str) -> List[str]:
    out = [str(item) for item in (args or [])]  # type: ignore[union-attr]
    if flag in out:
        index = out.index(flag)
        if index + 1 < len(out):
            out[index + 1] = value
        else:
            out.append(value)
    else:
        out.extend([flag, value])
    return out


def apply_playwright_mesh_target(
    available_servers: Dict[str, Dict[str, object]],
    selected_servers: Dict[str, Dict[str, object]],
    *,
    device: Optional[str],
    browser: Optional[str] = None,
) -> Optional[str]:
    """Pin the single Playwright Mesh server to one device/browser for this launch."""
    if PLAYWRIGHT_MESH_MCP_NAME not in selected_servers:
        return None
    source = available_servers.get(PLAYWRIGHT_MESH_MCP_NAME)
    if not isinstance(source, dict) or not device:
        return None

    # Mutate the shared config object in place: build_mcp_flags() reads its
    # command/args overrides from the *available* servers mapping, so a copy
    # here would silently drop the selected target at launch time.
    server_cfg = source
    args = set_command_flag(server_cfg.get("args"), "--target", str(device))
    if browser:
        args = set_command_flag(args, "--browser", str(browser))
    server_cfg["args"] = args
    server_cfg["transport"] = "stdio"
    server_cfg["startup_timeout_sec"] = max(int(server_cfg.get("startup_timeout_sec") or 0), 120)
    server_cfg["tool_timeout_sec"] = max(int(server_cfg.get("tool_timeout_sec") or 0), 600)
    server_cfg["timeout"] = max(int(server_cfg.get("timeout") or 0), 600)
    # Mark the entry as runtime-overridden so the launcher emits command/args.
    server_cfg["_source"] = "preset_mcp"
    selected_servers[PLAYWRIGHT_MESH_MCP_NAME] = server_cfg
    return f"{device}" + (f"/{browser}" if browser else "")


def maybe_configure_playwright_mesh(
    selected_servers: Dict[str, Dict[str, object]],
    available_servers: Dict[str, Dict[str, object]],
    *,
    interactive: bool = True,
) -> Optional[str]:
    """Ask for a target device and then a browser when Playwright Mesh is selected."""
    if PLAYWRIGHT_MESH_MCP_NAME not in selected_servers:
        return None

    env_device = os.environ.get(PLAYWRIGHT_MESH_TARGET_ENV)
    if env_device and env_device.strip():
        return apply_playwright_mesh_target(
            available_servers,
            selected_servers,
            device=env_device,
            browser=os.environ.get(PLAYWRIGHT_MESH_BROWSER_ENV),
        )

    devices = playwright_mesh_devices(available_servers)
    if not interactive or not devices:
        return None

    device_options: List[object] = [
        Option("Playwright Mesh target device", is_header=True),
        Option("Use config default", PLAYWRIGHT_MESH_DEFAULT),
    ]
    for key in sorted(devices, key=lambda name: (not devices[name].get("local"), name)):
        cfg = devices[key]
        label = str(cfg.get("label") or key)
        browsers = [str(item) for item in (cfg.get("browsers") or [])]
        detail = ", ".join(browsers) if browsers else "no browser detected"
        if cfg.get("local"):
            detail += ", local"
        device_options.append(Option(f"{label}  [{detail}]", key))

    chosen_device = single_select_items(
        device_options,
        title="Select Playwright Mesh target device (enter confirm, q cancel)",
        preselected_value=PLAYWRIGHT_MESH_DEFAULT,
    )
    if chosen_device is None:
        print("Cancelled.")
        sys.exit(130)
    if str(chosen_device) == PLAYWRIGHT_MESH_DEFAULT:
        return None

    browsers = [
        str(item) for item in ((devices.get(str(chosen_device)) or {}).get("browsers") or [])
    ]
    browser_options: List[object] = [
        Option(f"Playwright Mesh browser on {chosen_device}", is_header=True),
        Option("Auto (device default)", "auto"),
    ]
    for name in browsers:
        browser_options.append(Option(name, name))

    chosen_browser = single_select_items(
        browser_options,
        title="Select Playwright Mesh browser (enter confirm, q cancel)",
        preselected_value="auto",
    )
    if chosen_browser is None:
        print("Cancelled.")
        sys.exit(130)

    return apply_playwright_mesh_target(
        available_servers,
        selected_servers,
        device=str(chosen_device),
        browser=str(chosen_browser),
    )


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


def _streams_are_tty() -> bool:
    try:
        return bool(sys.stdin.isatty() and sys.stdout.isatty())
    except Exception:
        return False


def _write_sqlite_scaffold(
    cwd: Path,
    config_path: Path,
    db_path: Path,
    *,
    created_db: bool,
) -> Optional[Path]:
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
        print(f"Не удалось записать sqliteMCP.toml: {exc}.", file=sys.stderr)
        return None
    if created_db:
        print(f"Созданы {config_path} и база {db_path}")
    else:
        print(f"Создан {config_path}; используется существующая база {db_path}")
    return config_path


def _resolve_sqlite_db_path_from_env(cwd: Path) -> Optional[Path]:
    raw = os.environ.get(SQLITE_DB_PATH_ENV)
    if not raw or not raw.strip():
        return None
    expanded = Path(os.path.expandvars(os.path.expanduser(raw.strip())))
    return expanded if expanded.is_absolute() else (cwd / expanded).resolve()


def _scaffold_sqlite_noninteractive(cwd: Path, config_path: Path) -> Optional[Path]:
    db_path = _resolve_sqlite_db_path_from_env(cwd)
    if db_path is None:
        db_path = (cwd / DEFAULT_SQLITE_DB_NAME).resolve()
    try:
        db_path.parent.mkdir(parents=True, exist_ok=True)
        created_db = not db_path.exists()
        db_path.touch(exist_ok=True)
    except OSError as exc:
        print(
            f"Не удалось создать файл базы {db_path}: {exc}. "
            f"Укажите {SQLITE_DB_PATH_ENV} или создайте sqliteMCP.toml вручную.",
            file=sys.stderr,
        )
        return None
    return _write_sqlite_scaffold(cwd, config_path, db_path, created_db=created_db)


def ensure_sqlite_scaffold(cwd: Path, *, interactive: bool = True) -> Optional[Path]:
    config_path = cwd / "sqliteMCP.toml"
    if config_path.exists():
        return config_path

    if not (interactive and _streams_are_tty()):
        return _scaffold_sqlite_noninteractive(cwd, config_path)

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
            return _write_sqlite_scaffold(cwd, config_path, selection.resolve(), created_db=False)

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

        return _write_sqlite_scaffold(cwd, config_path, db_path, created_db=True)


_normalize_playwright_runtime_mode = normalize_playwright_runtime_mode
_playwright_chrome_profile_dir = playwright_chrome_profile_dir
_playwright_chrome_viewport = playwright_chrome_viewport
_playwright_chrome_cdp_wrapper_path = playwright_chrome_cdp_wrapper_path
_playwright_command_and_args_for_mode = playwright_command_and_args_for_mode
_maybe_configure_playwright_runtime = maybe_configure_playwright_runtime
_relative_to_cwd = relative_to_cwd
_discover_sqlite_files = discover_sqlite_files
_ensure_sqlite_scaffold = ensure_sqlite_scaffold
