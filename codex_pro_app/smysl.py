from __future__ import annotations

import base64
import getpass
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, Iterable, List, Sequence, Tuple

from .prompts import build_file_prompt, compose_prompt


PROMPT_DIR = Path("docs/process/codex/prompts")
INIT_PROMPT_FILE = "SMYSL_INITIALIZATION_PROMPT.md"
RECON_PROMPT_FILE = "SMYSL_RECON_PROMPT.md"
SESSION_METADATA_NAME = "codex_session.json"
DEFAULT_API_BASE_URL = "http://109.71.241.141:8080"
DEFAULT_PHASE1_INPUT_JSON = Path("docs/briefings/_defaults/phase1_inputs.json")
_ACTIVE_TUNNELS: set[str] = set()


def _rel_path(path: Path | None, root: Path) -> str:
    if path is None:
        return "<missing>"
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def _fill_template(template: str, mapping: Dict[str, str]) -> str:
    text = template
    for key, value in mapping.items():
        text = text.replace(f"{{{{{key}}}}}", value)
    return text


def _slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.lower())
    slug = slug.strip("-")
    return slug or "tenant"


def _read_prompt(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8").strip()
    except FileNotFoundError:
        raise SystemExit(f"Required prompt file not found: {path}")


def _load_context_files(root: Path, relative_paths: Sequence[str]) -> List[str]:
    parts: List[str] = []
    for rel in relative_paths:
        path = root / rel
        if path.is_file():
            parts.append(build_file_prompt(path, root))
    return parts


def _common_context(root: Path) -> List[str]:
    files = [
        "docs/process/codex/CODEX_RECONNAISSANCE.md",
        "docs/process/codex/GREAT_CODEX_RECONNASIISANCE.md",
    ]
    return _load_context_files(root, files)




def _list_mcp_configs(
    root: Path,
    *,
    keywords: Sequence[str],
    max_depth: int = 5,
) -> List[Path]:
    search_roots = [
        root,
        root / "docs" / "briefings",
        root.parent if root.parent.exists() else root,
        Path.home() / "Desktop" / "projects",
    ]

    ignore_folders = {".git", "__pycache__", "node_modules", "build", "python_script_tests"}
    seen: set[Path] = set()
    results: List[Path] = []

    def should_ignore(path: Path) -> bool:
        return any(part.lower() in ignore_folders for part in path.parts)

    def search_directory(directory: Path, depth: int):
        if depth > max_depth:
            return
        try:
            entries = list(directory.iterdir())
        except (PermissionError, FileNotFoundError):
            return
        for entry in entries:
            name_lower = entry.name.lower()
            if entry.is_symlink():
                continue
            if entry.is_file():
                if entry.suffix == ".toml" and "mcp" in name_lower and any(
                    key in name_lower for key in keywords
                ):
                    resolved = entry.resolve()
                    if should_ignore(resolved):
                        continue
                    if resolved not in seen:
                        seen.add(resolved)
                        results.append(resolved)
            elif entry.is_dir():
                if name_lower in ignore_folders:
                    continue
                search_directory(entry, depth + 1)

    for root_dir in search_roots:
        if root_dir.exists():
            search_directory(root_dir, 0)

    return sorted(results)



def _prompt_select_path(paths: List[Path], description: str) -> Path | None:
    if not paths:
        print(f'No {description} configs found.')
        return None

    print(f'Available {description} configs:')
    for idx, path in enumerate(paths, start=1):
        try:
            rel = path.relative_to(Path.cwd())
        except ValueError:
            rel = path
        print(f'  {idx}. {rel}')

    while True:
        selection = _prompt_text(f'Select {description} config by number (leave blank to cancel)', allow_empty=True)
        if not selection:
            return None
        if selection.isdigit():
            idx = int(selection)
            if 1 <= idx <= len(paths):
                return paths[idx - 1]
        print('Invalid selection. Try again.')


def _extract_ssh_tunnel_from_config(config_path: Path) -> str | None:
    try:
        text = config_path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return None
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        lower = stripped.lower()
        if stripped.startswith("#") and "ssh" in stripped and "-l" in lower:
            content = stripped.lstrip("# ")
            if ":" in content:
                content = content.split(":", 1)[1].strip()
            return content
        if "ssh_tunnel" in lower:
            parts = stripped.split("=", 1)
            if len(parts) == 2:
                candidate = parts[1].strip().strip('"')
                if candidate:
                    return candidate
        if stripped.startswith("ssh -"):
            return stripped
    return None

def _prompt_choice(prompt: str, allowed: Sequence[str]) -> str:
    allowed_set = set(allowed)
    while True:
        try:
            choice = input(f"{prompt} ").strip()
        except (KeyboardInterrupt, EOFError):
            print("\nAborted.")
            sys.exit(130)
        if choice in allowed_set:
            return choice
        print(f"Enter one of: {', '.join(allowed)}")


def _prompt_text(prompt: str, *, default: str | None = None, allow_empty: bool = False) -> str:
    while True:
        label = prompt
        if default is not None and default != "":
            label = f"{prompt} [{default}]"
        if label.endswith(":"):
            label = label + " "
        elif not label.endswith(": "):
            label = label + ": "
        try:
            value = input(label).strip()
        except (KeyboardInterrupt, EOFError):
            print("\nAborted.")
            sys.exit(130)
        if value:
            return value
        if default is not None:
            return default
        if allow_empty:
            return ""
        print("Value required.")


def _prompt_optional_int(prompt: str) -> int | None:
    while True:
        raw = _prompt_text(prompt, allow_empty=True)
        if not raw:
            return None
        try:
            return int(raw)
        except ValueError:
            print("Enter an integer or leave blank.")


def _prompt_bool(prompt: str, *, default: bool = False) -> bool:
    options = "Y/n" if default else "y/N"
    while True:
        try:
            raw = input(f"{prompt} [{options}]: ").strip().lower()
        except (KeyboardInterrupt, EOFError):
            print("\nAborted.")
            sys.exit(130)
        if not raw:
            return default
        if raw in {"y", "yes"}:
            return True
        if raw in {"n", "no"}:
            return False
        print("Enter 'y' or 'n'.")


def _prompt_secret(prompt: str) -> str:
    while True:
        try:
            value = getpass.getpass(f"{prompt}: ").strip()
        except (KeyboardInterrupt, EOFError):
            print("\nAborted.")
            sys.exit(130)
        if value:
            return value
        print("Value required.")


def _prompt_tables(prompt: str, *, default: str = "*") -> List[str]:
    raw = _prompt_text(prompt, default=default)
    if raw.strip() == "*":
        return ["*"]
    tables = [item.strip() for item in raw.split(",") if item.strip()]
    return tables or ["*"]


def _prompt_domain_briefing() -> tuple[str, str | None]:
    print("Domain briefing: provide a file path or type 'paste' to enter text manually. When pasting, finish with a line containing only 'EOF'.")
    source = _prompt_text("Domain briefing source (file path or 'paste')", default="paste").strip()
    if source.lower() != "paste":
        if source.lower().startswith("path:"):
            source = source[5:].strip()
        path = Path(source).expanduser()
        try:
            text = path.read_text(encoding="utf-8").strip()
        except FileNotFoundError:
            print(f"File not found: {path}")
            return _prompt_domain_briefing()
        if not text:
            print("Domain briefing file is empty.")
            return _prompt_domain_briefing()
        return text, str(path.resolve())
    print("Paste domain briefing below. End with 'EOF' on its own line.")
    lines: List[str] = []
    while True:
        try:
            line = input()
        except (KeyboardInterrupt, EOFError):
            print("\nAborted.")
            sys.exit(130)
        if line.strip() == "EOF":
            break
        lines.append(line)
    text = "\n".join(lines).strip()
    if not text:
        print("Domain briefing cannot be empty.")
        return _prompt_domain_briefing()
    return text, None


def _format_input_summary(details: Dict[str, Any]) -> str:
    lines: List[str] = []
    lines.append(f"- Tenant name: {details['tenant_name']}")
    lines.append(f"- Tenant slug: {details['tenant_slug']}")
    lines.append(f"- API base URL: {details['api_base_url']}")
    business_client_id = details.get("business_client_id")
    if business_client_id is not None:
        lines.append(f"- Business client id: {business_client_id}")
    data_source_name = details.get("data_source_name")
    if data_source_name:
        lines.append(f"- Data source name override: {data_source_name}")
    lines.append(f"- Source DB type: {details['db_type']}")
    lines.append(f"- Source DB host: {details['db_host']}:{details['db_port']}")
    lines.append(f"- Source DB user: {details['db_user']}")
    lines.append(f"- Source DB password: {details['db_password']}")
    lines.append(f"- Source DB name: {details['db_name']}")
    tables = details.get("tables") or ["*"]
    if tables == ["*"]:
        lines.append("- Tables to sync: * (all tables)")
    else:
        lines.append(f"- Tables to sync: {', '.join(tables)}")
    lines.append(f"- Skip initial sync: {'yes' if details.get('skip_initial_sync') else 'no'}")
    clickhouse_meta = details.get('clickhouse') or {}
    if clickhouse_meta.get('host'):
        host = clickhouse_meta['host']
        port = clickhouse_meta.get('port')
        lines.append(f"- ClickHouse host: {host}{':' + str(port) if port else ''}")
        user = clickhouse_meta.get('user')
        if user:
            lines.append(f"- ClickHouse user: {user}")
        if clickhouse_meta.get('password') is not None:
            lines.append(f"- ClickHouse password: {clickhouse_meta['password']}")
        password_env = clickhouse_meta.get('password_env')
        if password_env:
            lines.append(f"- ClickHouse password env: {password_env}")
        database = clickhouse_meta.get('database')
        if database:
            lines.append(f"- ClickHouse database: {database}")
        secure = clickhouse_meta.get('secure')
        if secure is not None:
            lines.append(f"- ClickHouse secure: {secure}")
        verify = clickhouse_meta.get('verify')
        if verify is not None:
            lines.append(f"- ClickHouse verify: {verify}")
        access_mode = clickhouse_meta.get('access_mode')
        if access_mode:
            lines.append(f"- ClickHouse access mode: {access_mode}")
        extra_args = clickhouse_meta.get('extra_args')
        if extra_args:
            lines.append(f"- ClickHouse extra args: {extra_args}")
        ssh_tunnel = clickhouse_meta.get('ssh_tunnel')
        if ssh_tunnel:
            lines.append(f"- ClickHouse SSH tunnel: {ssh_tunnel}")
    return "\n".join(lines)


def _probe_clickhouse_connection(details: Dict[str, Any]) -> tuple[bool, str]:
    host = details.get("host")
    if not host:
        return False, "skipped: host not provided"
    port = details.get("port")
    try:
        port_int = int(port) if port is not None else (443 if details.get("secure") else 8123)
    except (TypeError, ValueError):
        return False, f"invalid port value: {port!r}"
    scheme = "https" if details.get("secure") else "http"
    query = urllib.parse.quote_plus("SELECT 1")
    url = f"{scheme}://{host}:{port_int}/?query={query}"

    password = details.get("password")
    password_env = details.get("password_env")
    if password is None and password_env:
        password = os.getenv(str(password_env))

    user = details.get("user")
    request = urllib.request.Request(url)
    if user and password is not None:
        token = base64.b64encode(f"{user}:{password}".encode("utf-8")).decode("ascii")
        request.add_header("Authorization", f"Basic {token}")

    context = None
    if scheme == "https" and not details.get("verify", True):
        import ssl

        context = ssl._create_unverified_context()

    try:
        if context is None:
            response_cm = urllib.request.urlopen(request, timeout=5)
        else:
            response_cm = urllib.request.urlopen(request, timeout=5, context=context)
        with response_cm as response:
            status = getattr(response, "status", None) or getattr(response, "code", None)
            return True, f"reachable (HTTP {status})" if status else "reachable"
    except urllib.error.URLError as exc:
        return False, f"unreachable: {exc}"
    except Exception as exc:  # pragma: no cover - defensive
        return False, f"error: {exc}"


def _meta_get(meta: Dict[str, Any], *keys: str) -> Any:
    for key in keys:
        if key in meta:
            return meta[key]
    return None


def _format_recon_summary(metadata: Dict[str, Any]) -> str:
    lines: List[str] = []
    tenant_name = _meta_get(metadata, "tenant_name", "clientName")
    if tenant_name:
        lines.append(f"- Tenant name: {tenant_name}")
    tenant_slug = _meta_get(metadata, "tenant_slug", "tenantSlug")
    if tenant_slug:
        lines.append(f"- Tenant slug: {tenant_slug}")
    api_base_url = _meta_get(metadata, "api_base_url", "apiBaseUrl", "base_url", "baseUrl")
    if api_base_url:
        lines.append(f"- API base URL: {api_base_url}")
    session_id = _meta_get(metadata, "session_id", "sessionId")
    if session_id:
        lines.append(f"- Session id: {session_id}")
    registration = metadata.get("registration") or {}
    reg_lines: List[str] = []
    if registration:
        business_client_id = registration.get("businessClientId") or registration.get("business_client_id")
        if business_client_id is not None:
            reg_lines.append(f"    • businessClientId: {business_client_id}")
        data_source_id = registration.get("dataSourceId") or registration.get("data_source_id")
        if data_source_id is not None:
            reg_lines.append(f"    • dataSourceId: {data_source_id}")
        tenant_db = registration.get("tenantDbName") or registration.get("tenant_db_name")
        if tenant_db:
            reg_lines.append(f"    • tenantDbName: {tenant_db}")
        status = registration.get("status")
        if status:
            reg_lines.append(f"    • status: {status}")
        synced_tables_count = registration.get("syncedTablesCount") or registration.get("synced_tables_count")
        if synced_tables_count is not None:
            reg_lines.append(f"    • syncedTablesCount: {synced_tables_count}")
        sync_started = registration.get("syncStartedAt") or registration.get("sync_started_at")
        sync_finished = registration.get("syncFinishedAt") or registration.get("sync_finished_at")
        if sync_started:
            reg_lines.append(f"    • syncStartedAt: {sync_started}")
        if sync_finished:
            reg_lines.append(f"    • syncFinishedAt: {sync_finished}")
    if reg_lines:
        lines.append("- Registration:")
        lines.extend(reg_lines)
    inputs = metadata.get("inputs") or {}
    input_lines: List[str] = []
    if inputs:
        db_type = inputs.get("db_type")
        if db_type:
            input_lines.append(f"    • source DB type: {db_type}")
        host = inputs.get("db_host")
        port = inputs.get("db_port")
        if host and port is not None:
            input_lines.append(f"    • source DB host: {host}:{port}")
        db_user = inputs.get("db_user")
        if db_user:
            input_lines.append(f"    • source DB user: {db_user}")
        db_name = inputs.get("db_name")
        if db_name:
            input_lines.append(f"    • source DB name: {db_name}")
        tables = inputs.get("tables")
        if tables:
            if tables == ["*"]:
                input_lines.append("    • tables: * (all)")
            else:
                input_lines.append(f"    • tables: {', '.join(tables)}")
        skip_initial = inputs.get("skip_initial_sync")
        if skip_initial is not None:
            input_lines.append(f"    • skipInitialSync: {'true' if skip_initial else 'false'}")
    if input_lines:
        lines.append("- Source DB inputs:")
        lines.extend(input_lines)
    return "\n".join(lines) if lines else "- No launcher metadata available."


def _read_text_file(path: Path | None) -> str:
    if path is None:
        return "_Domain briefing not available._"
    try:
        content = path.read_text(encoding="utf-8").strip()
        return content or "_Domain briefing file is empty._"
    except FileNotFoundError:
        return f"_Domain briefing file missing at {path}_"


def _resolve_path(root: Path, raw: str | None) -> Path | None:
    if not raw:
        return None
    path = Path(raw)
    if not path.is_absolute():
        path = (root / path).resolve()
    return path


def _history_entries(history_path: Path) -> List[Tuple[str, int]]:
    if not history_path.is_file():
        return []
    entries: List[Tuple[str, int]] = []
    with history_path.open(encoding="utf-8") as fh:
        for idx, line in enumerate(fh):
            line = line.strip()
            if not line:
                continue
            try:
                payload = json.loads(line)
            except json.JSONDecodeError:
                continue
            session_id = payload.get("session_id") or payload.get("sessionId")
            if isinstance(session_id, str):
                entries.append((session_id, idx))
    return entries


def _latest_session_id(before: Iterable[Tuple[str, int]], after: Iterable[Tuple[str, int]]) -> str | None:
    before_set = {sid for sid, _ in before}
    filtered = [(sid, idx) for sid, idx in after if sid not in before_set]
    if filtered:
        return filtered[-1][0]
    if after:
        return list(after)[-1][0]
    return None


def _write_metadata(path: Path, data: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True), encoding="utf-8")


def _load_metadata_safely(path: Path) -> Dict[str, Any]:
    if not path.is_file():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}


def _deep_update(target: Dict[str, Any], updates: Dict[str, Any]) -> Dict[str, Any]:
    for key, value in updates.items():
        if isinstance(value, dict):
            current = target.get(key)
            if isinstance(current, dict):
                target[key] = _deep_update(current, value)
            else:
                target[key] = _deep_update({}, value)
        else:
            target[key] = value
    return target


def _write_clickhouse_config(
    *,
    tenant_slug: str,
    config_path: Path,
    description_path: Path,
    clickhouse_details: Dict[str, Any],
    tenant_db_name: str | None,
) -> None:
    host = clickhouse_details.get("host")
    if not host:
        return

    clickhouse_details.setdefault("secure", False)
    clickhouse_details.setdefault("verify", False)

    config_path.parent.mkdir(parents=True, exist_ok=True)
    description_path.parent.mkdir(parents=True, exist_ok=True)

    lines: List[str] = []
    lines.append(f"# ClickHouse MCP config for {tenant_slug}")
    ssh_tunnel = clickhouse_details.get("ssh_tunnel")
    if ssh_tunnel:
        lines.append(f"# Suggested tunnel: {ssh_tunnel}")
    lines.append("")

    rel_description = os.path.relpath(description_path, config_path.parent)

    lines.append("[meta]")
    lines.append(f'description_file = "{rel_description}"')
    lines.append("")

    lines.append("[database]")
    lines.append(f'host = "{host}"')
    port = clickhouse_details.get("port")
    if port is not None:
        lines.append(f"port = {int(port)}")
    user = clickhouse_details.get("user")
    if user:
        lines.append(f'user = "{user}"')
    password = clickhouse_details.get("password")
    if password is not None:
        lines.append(f'password = "{password}"')
    password_env = clickhouse_details.get("password_env")
    if password_env:
        lines.append(f'password_env = "{password_env}"')
    database = tenant_db_name or clickhouse_details.get("database")
    if database:
        lines.append(f'database = "{database}"')
    secure = clickhouse_details.get("secure")
    lines.append(f"secure = {'true' if secure else 'false'}")
    verify = clickhouse_details.get("verify")
    lines.append(f"verify = {'true' if verify else 'false'}")
    connect_timeout = clickhouse_details.get("connect_timeout", 30)
    if connect_timeout is not None:
        lines.append(f"connect_timeout = {int(connect_timeout)}")
    send_receive_timeout = clickhouse_details.get("send_receive_timeout", 30)
    if send_receive_timeout is not None:
        lines.append(f"send_receive_timeout = {int(send_receive_timeout)}")
    access_mode = clickhouse_details.get("access_mode") or "restricted"
    lines.append(f'access_mode = "{access_mode}"')

    lines.append("")
    lines.append("[chdb]")
    chdb_enabled = clickhouse_details.get("chdb_enabled", False)
    lines.append(f"enabled = {'true' if chdb_enabled else 'false'}")
    lines.append("")

    lines.append("[mcp]")
    extra_args = clickhouse_details.get("extra_args") or []
    if extra_args:
        formatted = ", ".join(f'"{arg}"' for arg in extra_args)
        lines.append(f"extra_args = [{formatted}]")
    else:
        lines.append("extra_args = []")

    config_path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def _load_metadata(path: Path) -> Dict[str, Any]:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise SystemExit(f"Session metadata not found at {path}. Have you completed Phase 1?")
    except json.JSONDecodeError as exc:
        raise SystemExit(f"Failed to parse metadata {path}: {exc}") from exc


def _metadata_candidates(root: Path) -> List[Tuple[str, Path]]:
    briefing_root = root / "docs" / "briefings"
    entries: List[Tuple[str, Path]] = []
    if not briefing_root.is_dir():
        return entries
    for tenant_dir in sorted(p for p in briefing_root.iterdir() if p.is_dir()):
        meta = tenant_dir / SESSION_METADATA_NAME
        if meta.is_file():
            entries.append((tenant_dir.name, meta))
    return entries


def _json_override(key: str, value: object) -> str:
    return f"{key}={json.dumps(value)}"


def _ensure_ssh_tunnel(command: str | None) -> None:
    if not command:
        return
    normalized = command.strip()
    if not normalized or normalized in _ACTIVE_TUNNELS:
        return
    print(f"Opening SSH tunnel: {normalized}")
    try:
        subprocess.Popen(
            ["bash", "-lc", normalized],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        _ACTIVE_TUNNELS.add(normalized)
    except Exception as exc:  # pragma: no cover - defensive
        print(f"Warning: failed to launch SSH tunnel ({exc}). Proceeding without it.")


def _run_initialization(argv: Sequence[str]) -> int:
    root = Path.cwd()
    prompt_dir = root / PROMPT_DIR
    prompt_text = _read_prompt(prompt_dir / INIT_PROMPT_FILE)
    default_json_prompt = str(DEFAULT_PHASE1_INPUT_JSON)
    config_path_raw = _prompt_text(
        "Load registration details from JSON file (leave blank for manual entry)",
        default=default_json_prompt,
        allow_empty=True,
    ).strip()

    config_data: Dict[str, Any] | None = None
    if config_path_raw:
        config_path = Path(config_path_raw).expanduser()
        if not config_path.is_file():
            raise SystemExit(f"Registration JSON not found at {config_path}")
        try:
            config_data = json.loads(config_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            raise SystemExit(f"Failed to parse registration JSON: {exc}") from exc

    clickhouse_details: Dict[str, Any] = {}

    if config_data is not None:
        input_section = config_data.get("input") or {}
        if not input_section:
            raise SystemExit("Registration JSON missing 'input' section")
        meta_section = config_data.get("meta") or {}

        tenant_name = input_section.get("clientName") or meta_section.get("tenantName")
        if not tenant_name:
            raise SystemExit("Registration JSON must include input.clientName")
        tenant_slug = meta_section.get("tenantSlug") or _slugify(tenant_name)
        api_base_url = meta_section.get("apiBaseUrl") or DEFAULT_API_BASE_URL

        business_client_id = meta_section.get("businessClientId")
        data_source_name = meta_section.get("dataSourceName")

        db_type = (input_section.get("dbType") or "mysql").lower()
        db_host = input_section.get("host") or meta_section.get("host")
        if not db_host:
            raise SystemExit("Registration JSON must include input.host")
        port_raw = input_section.get("port") or meta_section.get("port") or 3306
        try:
            db_port = int(port_raw)
        except (TypeError, ValueError):
            raise SystemExit("Database port must be an integer")
        db_user = input_section.get("user") or meta_section.get("user")
        if not db_user:
            raise SystemExit("Registration JSON must include input.user")
        db_password = input_section.get("password") or meta_section.get("password")
        if db_password is None:
            raise SystemExit("Registration JSON must include input.password")
        db_name = input_section.get("database") or meta_section.get("database")
        if not db_name:
            raise SystemExit("Registration JSON must include input.database")

        tables_value = input_section.get("tables") or meta_section.get("tables")
        if not tables_value:
            tables = ["*"]
        elif isinstance(tables_value, str):
            tables = [item.strip() for item in tables_value.split(",") if item.strip()] or ["*"]
        else:
            tables = list(tables_value)

        skip_initial_sync = bool(meta_section.get("skipInitialSync") or False)

        clickhouse_meta = meta_section.get("clickhouse") or {}
        if clickhouse_meta:
            ch_host = clickhouse_meta.get("host")
            if ch_host:
                clickhouse_details["host"] = str(ch_host)
            ch_port = clickhouse_meta.get("port")
            if ch_port is not None:
                try:
                    clickhouse_details["port"] = int(ch_port)
                except (TypeError, ValueError) as exc:
                    raise SystemExit(f"ClickHouse port must be an integer (got {ch_port!r})") from exc
            ch_user = clickhouse_meta.get("user")
            if ch_user:
                clickhouse_details["user"] = str(ch_user)
            if "password" in clickhouse_meta:
                clickhouse_details["password"] = clickhouse_meta.get("password")
            if "password_env" in clickhouse_meta:
                clickhouse_details["password_env"] = clickhouse_meta.get("password_env")
            if "database" in clickhouse_meta:
                clickhouse_details["database"] = clickhouse_meta.get("database")
            if "secure" in clickhouse_meta:
                clickhouse_details["secure"] = bool(clickhouse_meta.get("secure"))
            if "verify" in clickhouse_meta:
                clickhouse_details["verify"] = bool(clickhouse_meta.get("verify"))
            if "access_mode" in clickhouse_meta:
                clickhouse_details["access_mode"] = clickhouse_meta.get("access_mode")
            if "extra_args" in clickhouse_meta:
                clickhouse_details["extra_args"] = clickhouse_meta.get("extra_args")
            if "transport" in clickhouse_meta:
                clickhouse_details["transport"] = clickhouse_meta.get("transport")
            if "description_file" in clickhouse_meta:
                clickhouse_details["description_file"] = clickhouse_meta.get("description_file")
            if "ssh_tunnel" in clickhouse_meta:
                clickhouse_details["ssh_tunnel"] = clickhouse_meta.get("ssh_tunnel")
            if "sshTunnel" in clickhouse_meta and "ssh_tunnel" not in clickhouse_details:
                clickhouse_details["ssh_tunnel"] = clickhouse_meta.get("sshTunnel")
            if "sshTunnelCommand" in clickhouse_meta and "ssh_tunnel" not in clickhouse_details:
                clickhouse_details["ssh_tunnel"] = clickhouse_meta.get("sshTunnelCommand")
            if "ssh" in clickhouse_meta and "ssh_tunnel" not in clickhouse_details:
                clickhouse_details["ssh_tunnel"] = clickhouse_meta.get("ssh")

        clickhouse_details.setdefault("secure", False)
        clickhouse_details.setdefault("verify", False)

        domain_briefing_text: str | None = None
        domain_briefing_source = meta_section.get("domainBriefingPath")
        if domain_briefing_source:
            domain_path = Path(domain_briefing_source).expanduser()
            try:
                domain_briefing_text = domain_path.read_text(encoding="utf-8").strip()
            except FileNotFoundError:
                raise SystemExit(f"Domain briefing file not found at {domain_path}")
            if not domain_briefing_text:
                raise SystemExit(f"Domain briefing file at {domain_path} is empty.")
        else:
            domain_briefing_text = input_section.get("domainBriefing")
            if domain_briefing_text is None:
                raise SystemExit("Registration JSON must include input.domainBriefing or meta.domainBriefingPath")
        domain_briefing_source = domain_briefing_source or f"json:{config_path_raw}"
    else:
        tenant_name = _prompt_text("Tenant display name")
        tenant_slug = _prompt_text("Tenant slug (lower-kebab-case)")
        api_base_url = _prompt_text(
            "Prod API base URL",
            default=DEFAULT_API_BASE_URL,
        )

        business_client_id = _prompt_optional_int("Existing business client id (leave blank if new)")
        data_source_name_raw = _prompt_text("Data source name override (leave blank for default)", allow_empty=True)
        data_source_name = data_source_name_raw or None

        db_type = _prompt_text("Source DB type", default="mysql").lower()
        db_host = _prompt_text("Source DB host")
        while True:
            db_port_str = _prompt_text("Source DB port", default="3306")
            try:
                db_port = int(db_port_str)
                break
            except ValueError:
                print("Port must be an integer.")
        db_user = _prompt_text("Source DB user")
        db_password = _prompt_secret("Source DB password (hidden input)")
        db_name = _prompt_text("Source DB name")
        tables = _prompt_tables("Tables to sync (comma separated, or * for all)")
        skip_initial_sync = _prompt_bool("Skip initial sync?", default=False)
        domain_briefing_text, domain_briefing_source = _prompt_domain_briefing()

        clickhouse_host = _prompt_text(
            "ClickHouse host for connectivity check (leave blank to skip)",
            allow_empty=True,
        )
        if clickhouse_host:
            clickhouse_details["host"] = clickhouse_host
            while True:
                ch_port_str = _prompt_text("ClickHouse port", default="8123")
                try:
                    clickhouse_details["port"] = int(ch_port_str)
                    break
                except ValueError:
                    print("ClickHouse port must be an integer.")
            clickhouse_details["user"] = _prompt_text("ClickHouse user")
            clickhouse_details["password"] = _prompt_secret("ClickHouse password (hidden input)")
            ch_db = _prompt_text("ClickHouse database (leave blank for none)", allow_empty=True)
            if ch_db:
                clickhouse_details["database"] = ch_db
            use_secure = _prompt_bool("Connect with TLS?", default=False)
            clickhouse_details["secure"] = use_secure
            verify_tls = _prompt_bool("Verify TLS certificates?", default=False)
            clickhouse_details["verify"] = verify_tls
            clickhouse_details["access_mode"] = _prompt_text(
                "ClickHouse access mode",
                default="restricted",
            )
            extra_args_raw = _prompt_text(
                "ClickHouse extra MCP args (comma separated, leave blank for default)",
                allow_empty=True,
            )
            if extra_args_raw:
                clickhouse_details["extra_args"] = [
                    item.strip() for item in extra_args_raw.split(",") if item.strip()
                ]
            ssh_command = _prompt_text(
                "SSH tunnel command (leave blank if not needed)",
                allow_empty=True,
            )
            if ssh_command:
                clickhouse_details["ssh_tunnel"] = ssh_command

        clickhouse_details.setdefault("secure", False)
        clickhouse_details.setdefault("verify", False)

    briefing_dir = root / "docs" / "briefings" / tenant_slug
    datasource_notes = briefing_dir / f"{tenant_slug}_datasource.txx"
    domain_briefing_path = briefing_dir / "domain_briefing.md"
    clickhouse_config = briefing_dir / f"{tenant_slug}clickhouseMCP.toml"
    description_md = briefing_dir / f"{tenant_slug}clickhouseMCPDBDescription.md"

    input_details = {
        "tenant_name": tenant_name,
        "tenant_slug": tenant_slug,
        "api_base_url": api_base_url,
        "business_client_id": business_client_id,
        "data_source_name": data_source_name,
        "db_type": db_type,
        "db_host": db_host,
        "db_port": db_port,
        "db_user": db_user,
        "db_password": db_password,
        "db_name": db_name,
        "tables": tables,
        "skip_initial_sync": skip_initial_sync,
        "clickhouse": clickhouse_details,
    }
    input_summary = _format_input_summary(input_details)

    registration_input: Dict[str, Any] = {
        "clientName": tenant_name,
        "dbType": db_type,
        "host": db_host,
        "port": db_port,
        "user": db_user,
        "password": db_password,
        "database": db_name,
        "skipInitialSync": skip_initial_sync,
        "domainBriefing": domain_briefing_text,
    }
    if business_client_id is not None:
        registration_input["businessClientId"] = business_client_id
    if data_source_name:
        registration_input["dataSourceName"] = data_source_name
    if tables != ["*"]:
        registration_input["tables"] = tables
    registration_payload = {"input": registration_input}
    registration_payload_json = json.dumps(registration_payload, indent=2)

    clickhouse_connection_lines: list[str] = []
    if clickhouse_details.get("host"):
        host = clickhouse_details["host"]
        port = clickhouse_details.get("port")
        clickhouse_connection_lines.append(f"- host: {host}{':' + str(port) if port else ''}")
        user = clickhouse_details.get("user")
        if user:
            clickhouse_connection_lines.append(f"- user: {user}")
        if clickhouse_details.get("password") is not None:
            clickhouse_connection_lines.append(f"- password: {clickhouse_details['password']}")
        password_env = clickhouse_details.get("password_env")
        if password_env:
            clickhouse_connection_lines.append(f"- password_env: {password_env}")
        database = clickhouse_details.get("database")
        if database:
            clickhouse_connection_lines.append(f"- database: {database}")
        secure = clickhouse_details.get("secure")
        if secure is not None:
            clickhouse_connection_lines.append(f"- secure: {secure}")
        verify = clickhouse_details.get("verify")
        if verify is not None:
            clickhouse_connection_lines.append(f"- verify: {verify}")
        access_mode = clickhouse_details.get("access_mode")
        if access_mode:
            clickhouse_connection_lines.append(f"- access_mode: {access_mode}")
        extra_args = clickhouse_details.get("extra_args")
        if extra_args:
            clickhouse_connection_lines.append(f"- extra_args: {extra_args}")
        ssh_tunnel = clickhouse_details.get("ssh_tunnel")
        if ssh_tunnel:
            clickhouse_connection_lines.append(f"- ssh_tunnel: {ssh_tunnel}")
    clickhouse_connection_summary = "\n".join(clickhouse_connection_lines) if clickhouse_connection_lines else "ClickHouse connectivity check disabled (no host provided)."
    clickhouse_probe_ok, clickhouse_probe_message = _probe_clickhouse_connection(clickhouse_details)
    clickhouse_ssh_instruction = clickhouse_details.get("ssh_tunnel") or "No SSH tunnel command provided; create one if required."

    mapping = {
        "TENANT_NAME": tenant_name,
        "TENANT_SLUG": tenant_slug,
        "BRIEFING_DIR": _rel_path(briefing_dir, root),
        "DATASOURCE_NOTES_PATH": _rel_path(datasource_notes, root),
        "DOMAIN_BRIEFING_PATH": _rel_path(domain_briefing_path, root),
        "CLICKHOUSE_CONFIG_PATH": _rel_path(clickhouse_config, root),
        "DESCRIPTION_MD_PATH": _rel_path(description_md, root),
        "DESCRIPTION_MD_RELATIVE": _rel_path(description_md, clickhouse_config.parent),
        "API_BASE_URL": api_base_url,
        "INPUT_SUMMARY": input_summary,
        "CLICKHOUSE_CONNECTION_SUMMARY": clickhouse_connection_summary,
        "CLICKHOUSE_SSH_TUNNEL": clickhouse_ssh_instruction,
        "CLICKHOUSE_PROBE_STATUS": clickhouse_probe_message,
        "DOMAIN_BRIEFING": domain_briefing_text,
        "REGISTRATION_PAYLOAD": registration_payload_json,
    }

    phase_prompt = _fill_template(prompt_text, mapping)
    prompt_parts = [phase_prompt, *_common_context(root)]
    final_prompt = compose_prompt(prompt_parts)

    postgres_config = root / "postgresMCP.toml"
    if not postgres_config.is_file():
        raise SystemExit(f"Postgres MCP config not found at {postgres_config}")

    history_path = Path.home() / ".codex" / "history.jsonl"
    history_before = _history_entries(history_path)

    base_cmd = [
        "codex",
        "--sandbox",
        "danger-full-access",
        "--ask-for-approval",
        "never",
        "-c",
        _json_override(
            "mcp_servers.postgres.args",
            ["--config-file", str(postgres_config.resolve())],
        ),
        "-c",
        "mcp_servers.postgres.enabled=true",
        "-c",
        "mcp_servers.clickhouse.enabled=false",
    ]
    base_cmd.extend(argv)
    cmd = base_cmd.copy()
    if final_prompt:
        cmd.append(final_prompt)

    if clickhouse_details.get("host"):
        status = "OK" if clickhouse_probe_ok else "FAILED"
        print(f"ClickHouse probe ({status}): {clickhouse_probe_message}")

    _ensure_ssh_tunnel(clickhouse_details.get("ssh_tunnel"))

    print("Launching Codex (Phase 1 — Initialization)")
    print("Command:", cmd[:-1] if final_prompt else cmd)
    rc = subprocess.call(cmd, cwd=root)

    if rc != 0:
        print(f"Codex exited with code {rc}; skipping metadata recording.")
        return rc

    history_after = _history_entries(history_path)
    session_id = _latest_session_id(history_before, history_after)
    if not session_id:
        print("Warning: unable to detect new Codex session id. Proceed step may need manual session selection.")
        return rc

    briefing_dir.mkdir(parents=True, exist_ok=True)
    metadata_path = briefing_dir / SESSION_METADATA_NAME
    metadata = _load_metadata_safely(metadata_path)

    metadata_updates: Dict[str, Any] = {
        "tenant_name": tenant_name,
        "tenant_slug": tenant_slug,
        "session_id": session_id,
        "sessionId": session_id,
        "api_base_url": api_base_url,
        "apiBaseUrl": api_base_url,
        "base_url": api_base_url,
        "baseUrl": api_base_url,
        "phase": "initialization",
        "status": "ready-for-reconnaissance",
        "created_at": datetime.utcnow().isoformat() + "Z",
        "options": {
            "sandbox": "danger-full-access",
            "approval": "never",
        },
        "mcp_configs": {
            "postgres": str(postgres_config.resolve()),
            "clickhouse": str(clickhouse_config.resolve()),
            "clickhouse_description": str(description_md.resolve()),
        },
        "briefings": {
            "domain": str(domain_briefing_path.resolve()),
            "datasource": str(datasource_notes.resolve()),
        },
        "clickhouse_probe": {"ok": clickhouse_probe_ok, "message": clickhouse_probe_message},
        "clickhouseProbe": {"ok": clickhouse_probe_ok, "message": clickhouse_probe_message},
    }
    metadata = _deep_update(metadata, metadata_updates)

    inputs = metadata.setdefault("inputs", {})
    inputs.update(
        {
            "business_client_id": business_client_id,
            "data_source_name": data_source_name,
            "db_type": db_type,
            "db_host": db_host,
            "db_port": db_port,
            "db_user": db_user,
            "db_password": db_password,
            "db_name": db_name,
            "tables": tables,
            "skip_initial_sync": skip_initial_sync,
            "domain_briefing_source": domain_briefing_source or "inline",
        }
    )
    ch_inputs = inputs.setdefault("clickhouse", {})
    for key, value in clickhouse_details.items():
        if value is not None:
            ch_inputs[key] = value

    registration = metadata.get("registration") or {}
    tenant_db_name = registration.get("tenantDbName") or registration.get("tenant_db_name")
    if tenant_db_name:
        ch_inputs["database"] = tenant_db_name

    metadata["inputs"] = inputs
    metadata["registrationInputs"] = dict(inputs)
    payload_template = dict(registration_input)
    payload_template["password"] = "<redacted>"
    metadata["registration_payload_template"] = payload_template
    metadata["input_summary"] = input_summary
    metadata["inputSummary"] = input_summary
    metadata["mcpConfigs"] = metadata.get("mcp_configs", metadata.get("mcpConfigs", {}))
    metadata["postgres_config"] = str(postgres_config.resolve())
    metadata["clickhouse_config"] = str(clickhouse_config.resolve())
    metadata["description_md"] = str(description_md.resolve())
    metadata["datasource_notes"] = str(datasource_notes.resolve())
    metadata["domain_briefing"] = str(domain_briefing_path.resolve())

    _write_metadata(metadata_path, metadata)

    effective_clickhouse = dict(ch_inputs)
    effective_clickhouse.setdefault("ssh_tunnel", clickhouse_details.get("ssh_tunnel"))
    _write_clickhouse_config(
        tenant_slug=tenant_slug,
        config_path=clickhouse_config,
        description_path=description_md,
        clickhouse_details=effective_clickhouse,
        tenant_db_name=effective_clickhouse.get("database"),
    )

    print(f"Recorded session metadata at {metadata_path}")
    print(f"Session id: {session_id}")
    print("Next: review generated artifacts, then run this launcher again and choose 'Proceed with reconnaissance'.")
    return rc


def _choose_metadata(root: Path) -> Tuple[str, Path]:
    candidates = _metadata_candidates(root)
    if not candidates:
        raise SystemExit("No tenant metadata found. Run Phase 1 first.")
    print("Available tenants with recorded Codex sessions:")
    for idx, (slug, path) in enumerate(candidates, start=1):
        print(f"  {idx}. {slug} ({path})")
    choice = _prompt_text("Select tenant by number or enter slug")
    if choice.isdigit():
        idx = int(choice)
        if 1 <= idx <= len(candidates):
            return candidates[idx - 1]
        raise SystemExit("Invalid selection.")
    for slug, path in candidates:
        if slug == choice:
            return slug, path
    raise SystemExit(f"No metadata found for slug '{choice}'.")


def _run_reconnaissance(argv: Sequence[str]) -> int:
    root = Path.cwd()
    prompt_dir = root / PROMPT_DIR
    prompt_text = _read_prompt(prompt_dir / RECON_PROMPT_FILE)

    tenant_slug, metadata_path = _choose_metadata(root)
    metadata = _load_metadata(metadata_path)

    session_id = _meta_get(metadata, "session_id", "sessionId")
    if not isinstance(session_id, str) or not session_id:
        raise SystemExit(f"metadata missing session_id in {metadata_path}")

    tenant_name = _meta_get(metadata, "tenant_name", "clientName") or tenant_slug.replace("-", " ").title()
    api_base_url = _meta_get(metadata, "api_base_url", "apiBaseUrl", "base_url", "baseUrl") or DEFAULT_API_BASE_URL
    registration = metadata.get("registration") or {}
    tenant_db = registration.get("tenantDbName") or registration.get("tenant_db_name") or "<unknown>"

    mcp_configs = metadata.get("mcp_configs") or metadata.get("mcpConfigs") or {}
    postgres_config_path = _resolve_path(root, mcp_configs.get("postgres") or metadata.get("postgres_config"))
    clickhouse_config_path = _resolve_path(root, mcp_configs.get("clickhouse") or metadata.get("clickhouse_config"))
    description_md_path = _resolve_path(root, mcp_configs.get("clickhouse_description") or mcp_configs.get("clickhouseDescription") or metadata.get("description_md"))

    if postgres_config_path is None or not postgres_config_path.is_file():
        raise SystemExit("Postgres MCP config not found. Ensure Phase 1 completed successfully.")
    if clickhouse_config_path is None or not clickhouse_config_path.is_file():
        raise SystemExit("Tenant ClickHouse MCP config not found. Ensure Phase 1 completed successfully.")

    briefing_refs = metadata.get("briefings") or {}
    datasource_notes_path = _resolve_path(root, metadata.get("datasource_notes") or briefing_refs.get("datasource"))
    domain_briefing_path = _resolve_path(root, metadata.get("domain_briefing") or briefing_refs.get("domain"))

    recon_root = root / "docs" / "codex_reconnaissances" / tenant_slug

    metadata["session_id"] = session_id
    metadata["sessionId"] = session_id
    metadata["api_base_url"] = api_base_url
    metadata["base_url"] = api_base_url
    metadata["phase"] = "reconnaissance"
    metadata["status"] = "in-progress"
    metadata["last_resumed_at"] = datetime.utcnow().isoformat() + "Z"
    if postgres_config_path:
        mcp_configs["postgres"] = str(postgres_config_path)
    if clickhouse_config_path:
        mcp_configs["clickhouse"] = str(clickhouse_config_path)
    if description_md_path:
        mcp_configs["clickhouse_description"] = str(description_md_path)
    metadata["mcp_configs"] = mcp_configs
    metadata["mcpConfigs"] = mcp_configs
    if domain_briefing_path:
        briefing_refs["domain"] = str(domain_briefing_path)
    if datasource_notes_path:
        briefing_refs["datasource"] = str(datasource_notes_path)
    metadata["briefings"] = briefing_refs
    _write_metadata(metadata_path, metadata)

    recon_summary = _format_recon_summary(metadata)
    domain_briefing_text = _read_text_file(domain_briefing_path)

    mapping = {
        "TENANT_NAME": tenant_name,
        "TENANT_SLUG": tenant_slug,
        "TENANT_DB_NAME": tenant_db,
        "POSTGRES_CONFIG_PATH": str(postgres_config_path),
        "CLICKHOUSE_CONFIG_PATH": str(clickhouse_config_path),
        "DESCRIPTION_MD_PATH": _rel_path(description_md_path, root),
        "RECON_ROOT": _rel_path(recon_root, root),
        "SESSION_ID": session_id,
        "API_BASE_URL": api_base_url,
        "RECON_INPUT_SUMMARY": recon_summary,
        "DOMAIN_BRIEFING": domain_briefing_text,
    }

    phase_prompt = _fill_template(prompt_text, mapping)
    prompt_parts = [phase_prompt, *_common_context(root)]

    extra_context: List[str] = []
    if domain_briefing_path and domain_briefing_path.is_file():
        extra_context.append(build_file_prompt(domain_briefing_path, root))
    if datasource_notes_path and datasource_notes_path.is_file():
        extra_context.append(build_file_prompt(datasource_notes_path, root))
    prompt_parts.extend(extra_context)

    final_prompt = compose_prompt(prompt_parts)

    clickhouse_inputs = metadata.get("inputs", {}).get("clickhouse", {})
    _ensure_ssh_tunnel(clickhouse_inputs.get("ssh_tunnel"))

    base_cmd = [
        "codex",
        "resume",
        "--sandbox",
        "danger-full-access",
        "--ask-for-approval",
        "never",
        "-c",
        _json_override(
            "mcp_servers.postgres.args",
            ["--config-file", str(postgres_config_path)],
        ),
        "-c",
        "mcp_servers.postgres.enabled=true",
        "-c",
        _json_override(
            "mcp_servers.clickhouse.args",
            ["--config-file", str(clickhouse_config_path)],
        ),
        "-c",
        "mcp_servers.clickhouse.enabled=true",
    ]
    base_cmd.extend(argv)
    cmd = base_cmd + [session_id]
    if final_prompt:
        cmd.append(final_prompt)

    print("Resuming Codex (Phase 2 — Reconnaissance)")
    print("Session id:", session_id)
    print("Command:", cmd[:-1] if final_prompt else cmd)
    return subprocess.call(cmd, cwd=root)


def run(argv: Sequence[str]) -> int:
    print("SMYSL Codex launcher")
    print("  1. Start new recon session (Phase 1 – Initialization)")
    print("  2. Proceed with reconnaissance in recorded session")
    print("  3. Launch freeform session (manual prompt)")
    phase = _prompt_choice("Select option [1/2/3]:", ["1", "2", "3"])
    if phase == "1":
        return _run_initialization(argv)
    if phase == "2":
        return _run_reconnaissance(argv)
    return _run_freeform(argv)


def _run_freeform(argv: Sequence[str]) -> int:
    root = Path.cwd()

    postgres_configs = _list_mcp_configs(root, keywords=("postgres",))
    clickhouse_configs = _list_mcp_configs(root, keywords=("clickhouse",))

    enable_postgres_default = bool(postgres_configs)
    enable_postgres = _prompt_bool("Enable Postgres MCP?", default=enable_postgres_default)
    postgres_config_path: Path | None = None
    if enable_postgres:
        if not postgres_configs:
            print("No Postgres configs detected.")
            enable_postgres = False
        else:
            selected = _prompt_select_path(postgres_configs, "Postgres MCP")
            if selected is None:
                enable_postgres = False
            else:
                postgres_config_path = selected

    enable_clickhouse_default = bool(clickhouse_configs)
    enable_clickhouse = _prompt_bool("Enable ClickHouse MCP?", default=enable_clickhouse_default)
    clickhouse_config_path: Path | None = None
    if enable_clickhouse:
        if not clickhouse_configs:
            print("No ClickHouse configs detected.")
            enable_clickhouse = False
        else:
            selected = _prompt_select_path(clickhouse_configs, "ClickHouse MCP")
            if selected is None:
                enable_clickhouse = False
            else:
                clickhouse_config_path = selected

    prompt_file_raw = _prompt_text("Path to prompt file (leave blank for manual input inside Codex)", allow_empty=True).strip()
    prompt_payload: str | None = None
    if prompt_file_raw:
        prompt_path = Path(prompt_file_raw).expanduser()
        if not prompt_path.is_file():
            raise SystemExit(f"Prompt file not found at {prompt_path}")
        prompt_payload = prompt_path.read_text(encoding="utf-8")

    cmd: List[str] = [
        "codex",
        "--sandbox",
        "danger-full-access",
        "--ask-for-approval",
        "never",
    ]
    cmd.extend(argv)

    if enable_postgres and postgres_config_path:
        cmd.extend([
            "-c",
            _json_override("mcp_servers.postgres.args", ["--config-file", str(postgres_config_path.resolve())]),
            "-c",
            "mcp_servers.postgres.enabled=true",
        ])
    else:
        cmd.extend(["-c", "mcp_servers.postgres.enabled=false"])

    if enable_postgres and postgres_config_path:
        tunnel_cmd = _extract_ssh_tunnel_from_config(postgres_config_path)
        _ensure_ssh_tunnel(tunnel_cmd)

    if enable_clickhouse and clickhouse_config_path:
        cmd.extend([
            "-c",
            _json_override("mcp_servers.clickhouse.args", ["--config-file", str(clickhouse_config_path.resolve())]),
            "-c",
            "mcp_servers.clickhouse.enabled=true",
        ])
    else:
        cmd.extend(["-c", "mcp_servers.clickhouse.enabled=false"])

    if enable_clickhouse and clickhouse_config_path:
        tunnel_cmd = _extract_ssh_tunnel_from_config(clickhouse_config_path)
        _ensure_ssh_tunnel(tunnel_cmd)

    print("Launching Codex (freeform mode)")
    print("Command:", cmd[:-1] if prompt_payload else cmd)

    if prompt_payload:
        cmd.append(prompt_payload)

    return subprocess.call(cmd, cwd=root)
