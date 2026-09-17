"""Update availability checking, 24h caching, snooze/skip logic, and interactive decision prompts."""

import json
import os
import sys
import time
from typing import Any, Dict, NamedTuple, Optional

from installer.fetcher import fetch_descriptor
from installer.paths import resolve_cache_dir, update_cache_path


CACHE_TTL_SECONDS = 86400  # 24 hours
FOREGROUND_TIMEOUT_SECONDS = 2.0


class UpdateCheckResult(NamedTuple):
    update_available: bool
    current_version: str
    latest_version: Optional[str] = None
    descriptor: Optional[Dict[str, Any]] = None
    should_prompt: bool = False
    offline_or_error: bool = False
    error_message: Optional[str] = None


def parse_version_tuple(v: str):
    """Simple semver-like comparison tuple."""
    parts = []
    for part in v.strip().lstrip("v").split("."):
        try:
            parts.append(int(part))
        except ValueError:
            parts.append(0)
    return tuple(parts)


def is_version_newer(candidate: str, current: str) -> bool:
    """Return True if candidate is newer than current."""
    try:
        return parse_version_tuple(candidate) > parse_version_tuple(current)
    except Exception:
        return candidate != current


def load_update_cache(cache_dir: str) -> Dict[str, Any]:
    """Load cached update status from update_check.json."""
    c_path = update_cache_path(cache_dir)
    if not os.path.isfile(c_path):
        return {}
    try:
        with open(c_path, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return {}


def save_update_cache(cache_dir: str, data: Dict[str, Any]) -> None:
    """Atomically save update cache to update_check.json."""
    os.makedirs(cache_dir, exist_ok=True)
    c_path = update_cache_path(cache_dir)
    tmp_path = f"{c_path}.tmp.{os.getpid()}"
    try:
        with open(tmp_path, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2)
            f.write("\n")
        os.replace(tmp_path, c_path)
    except OSError:
        pass


def check_for_update(
    descriptor_source: str,
    current_version: str,
    cache_dir: Optional[str] = None,
    timeout: float = FOREGROUND_TIMEOUT_SECONDS,
    force: bool = False,
) -> UpdateCheckResult:
    """
    Check if a newer stable release is available.
    Adheres to:
    - 2-second foreground network budget
    - At most once per 24 hours caching unless force=True
    - Graceful offline and network failure handling
    - Snooze and Skip-version suppression
    """
    if not cache_dir:
        cache_dir = resolve_cache_dir()

    now = time.time()
    cache = load_update_cache(cache_dir)

    last_checked = cache.get("last_checked_at", 0)
    cached_descriptor = cache.get("latest_descriptor")
    snooze_until = cache.get("snooze_until", 0)
    skipped_version = cache.get("skipped_version", "")

    # Check if cache is still fresh and can be used
    if not force and (now - last_checked < CACHE_TTL_SECONDS) and cached_descriptor:
        latest_version = cached_descriptor.get("version", current_version)
        update_available = is_version_newer(latest_version, current_version)
        should_prompt = update_available and (now > snooze_until) and (latest_version != skipped_version)
        return UpdateCheckResult(
            update_available=update_available,
            current_version=current_version,
            latest_version=latest_version,
            descriptor=cached_descriptor,
            should_prompt=should_prompt,
            offline_or_error=False,
        )

    # Perform network check within timeout budget
    try:
        descriptor = fetch_descriptor(descriptor_source, timeout=timeout)
        latest_version = descriptor.get("version", current_version)
        update_available = is_version_newer(latest_version, current_version)

        # Update cache
        cache["last_checked_at"] = now
        cache["latest_descriptor"] = descriptor
        save_update_cache(cache_dir, cache)

        should_prompt = update_available and (now > snooze_until) and (latest_version != skipped_version)

        return UpdateCheckResult(
            update_available=update_available,
            current_version=current_version,
            latest_version=latest_version,
            descriptor=descriptor,
            should_prompt=should_prompt,
            offline_or_error=False,
        )

    except Exception as e:
        # Graceful fallback: return offline/error result without interrupting caller
        err_msg = str(e)
        # If we had a cached descriptor, fall back to it
        if cached_descriptor:
            latest_version = cached_descriptor.get("version", current_version)
            update_available = is_version_newer(latest_version, current_version)
            should_prompt = update_available and (now > snooze_until) and (latest_version != skipped_version)
            return UpdateCheckResult(
                update_available=update_available,
                current_version=current_version,
                latest_version=latest_version,
                descriptor=cached_descriptor,
                should_prompt=should_prompt,
                offline_or_error=True,
                error_message=err_msg,
            )

        return UpdateCheckResult(
            update_available=False,
            current_version=current_version,
            latest_version=None,
            descriptor=None,
            should_prompt=False,
            offline_or_error=True,
            error_message=err_msg,
        )


def record_snooze(cache_dir: Optional[str] = None, duration_seconds: int = 86400) -> None:
    """Snooze update prompts for duration_seconds (default 24h)."""
    if not cache_dir:
        cache_dir = resolve_cache_dir()
    cache = load_update_cache(cache_dir)
    cache["snooze_until"] = time.time() + duration_seconds
    save_update_cache(cache_dir, cache)


def record_skip_version(version: str, cache_dir: Optional[str] = None) -> None:
    """Record a version to skip prompting until a newer release appears."""
    if not cache_dir:
        cache_dir = resolve_cache_dir()
    cache = load_update_cache(cache_dir)
    cache["skipped_version"] = version
    save_update_cache(cache_dir, cache)


def prompt_update_decision(
    available_version: str,
    current_version: str,
    input_stream=None,
    output_stream=None,
) -> str:
    """
    Prompt user before normal launcher entry:
    Update now / Later / Skip this version
    Returns "update", "later", or "skip".
    """
    if input_stream is None:
        input_stream = sys.stdin
    if output_stream is None:
        output_stream = sys.stdout

    output_stream.write(
        f"\nNew Fixer v{available_version} available (installed v{current_version}):\n"
        f"  1) Update now\n"
        f"  2) Later (snooze 24h)\n"
        f"  3) Skip this version\n"
        f"Select [1-3] (default 2): "
    )
    output_stream.flush()

    try:
        choice = input_stream.readline().strip()
    except (OSError, EOFError):
        choice = "2"

    if choice in ("1", "update", "now", "Update now"):
        return "update"
    elif choice in ("3", "skip", "Skip this version"):
        return "skip"
    else:
        return "later"
