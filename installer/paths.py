"""Path resolution and layout helpers for Fixer MCP managed installation."""

import os
from typing import Optional


DEFAULT_MANAGED_ROOT_DIRNAME = ".fixer"
DEFAULT_STATE_SUBDIR = "fixer-client-wires"
DEFAULT_CONFIG_SUBDIR = "fixer"
DEFAULT_CACHE_SUBDIR = "fixer"
DEFAULT_DB_FILENAME = "fixer.db"


def resolve_managed_root(override: Optional[str] = None) -> str:
    """
    Resolve the managed root directory.
    Priority:
    1. Explicit override argument
    2. FIXER_MANAGED_ROOT environment variable
    3. ~/.local/share/fixer or ~/.fixer
    """
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_root = os.environ.get("FIXER_MANAGED_ROOT")
    if env_root and env_root.strip():
        return os.path.abspath(os.path.expanduser(env_root.strip()))
    xdg_data = os.environ.get("XDG_DATA_HOME")
    if xdg_data and xdg_data.strip():
        return os.path.abspath(os.path.join(os.path.expanduser(xdg_data.strip()), "fixer"))
    return os.path.abspath(os.path.expanduser(f"~/{DEFAULT_MANAGED_ROOT_DIRNAME}"))


def resolve_state_dir(override: Optional[str] = None) -> str:
    """
    Resolve the user state directory outside release directories.
    Defaults to $XDG_STATE_HOME/fixer-client-wires or ~/.local/state/fixer-client-wires.
    """
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_state = os.environ.get("FIXER_STATE_DIR")
    if env_state and env_state.strip():
        return os.path.abspath(os.path.expanduser(env_state.strip()))
    xdg_state = os.environ.get("XDG_STATE_HOME")
    if xdg_state and xdg_state.strip():
        base = os.path.expanduser(xdg_state.strip())
    else:
        base = os.path.expanduser("~/.local/state")
    return os.path.abspath(os.path.join(base, DEFAULT_STATE_SUBDIR))


def resolve_config_dir(override: Optional[str] = None) -> str:
    """Resolve configuration directory (default $XDG_CONFIG_HOME/fixer or ~/.config/fixer)."""
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_cfg = os.environ.get("FIXER_CONFIG_DIR")
    if env_cfg and env_cfg.strip():
        return os.path.abspath(os.path.expanduser(env_cfg.strip()))
    xdg_cfg = os.environ.get("XDG_CONFIG_HOME")
    if xdg_cfg and xdg_cfg.strip():
        base = os.path.expanduser(xdg_cfg.strip())
    else:
        base = os.path.expanduser("~/.config")
    return os.path.abspath(os.path.join(base, DEFAULT_CONFIG_SUBDIR))


def resolve_cache_dir(override: Optional[str] = None) -> str:
    """Resolve cache directory (default $XDG_CACHE_HOME/fixer or ~/.cache/fixer)."""
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_cache = os.environ.get("FIXER_CACHE_DIR")
    if env_cache and env_cache.strip():
        return os.path.abspath(os.path.expanduser(env_cache.strip()))
    xdg_cache = os.environ.get("XDG_CACHE_HOME")
    if xdg_cache and xdg_cache.strip():
        base = os.path.expanduser(xdg_cache.strip())
    else:
        base = os.path.expanduser("~/.cache")
    return os.path.abspath(os.path.join(base, DEFAULT_CACHE_SUBDIR))


def resolve_user_bin_dir(override: Optional[str] = None) -> str:
    """Resolve user bin directory for the command shim (default ~/.local/bin)."""
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_bin = os.environ.get("FIXER_USER_BIN")
    if env_bin and env_bin.strip():
        return os.path.abspath(os.path.expanduser(env_bin.strip()))
    return os.path.abspath(os.path.expanduser("~/.local/bin"))


def resolve_db_path(state_dir: Optional[str] = None, override: Optional[str] = None) -> str:
    """
    Resolve SQLite database path.
    Preserves explicit absolute FIXER_DB_PATH, otherwise uses state_dir/fixer.db.
    """
    if override and override.strip():
        return os.path.abspath(os.path.expanduser(override.strip()))
    env_db = os.environ.get("FIXER_DB_PATH")
    if env_db and env_db.strip():
        return os.path.abspath(os.path.expanduser(env_db.strip()))
    if not state_dir:
        state_dir = resolve_state_dir()
    return os.path.abspath(os.path.join(state_dir, DEFAULT_DB_FILENAME))


# Layout paths inside managed_root
def releases_dir(managed_root: str) -> str:
    return os.path.join(managed_root, "releases")


def release_version_dir(managed_root: str, version: str) -> str:
    return os.path.join(managed_root, "releases", version)


def current_link_path(managed_root: str) -> str:
    return os.path.join(managed_root, "current")


def staging_dir(managed_root: str) -> str:
    return os.path.join(managed_root, "staging")


def metadata_path(managed_root: str) -> str:
    return os.path.join(managed_root, "install.json")


def lock_path(managed_root: str) -> str:
    return os.path.join(managed_root, "install.lock")


def update_cache_path(cache_dir: str) -> str:
    return os.path.join(cache_dir, "update_check.json")


def runtime_lock_path(state_dir: str) -> str:
    return os.path.join(state_dir, "runtime.lock")


def active_pids_dir(state_dir: str) -> str:
    return os.path.join(state_dir, "active_pids")
