"""Command-line interface and launcher entrypoint for Fixer MCP."""

import argparse
import os
import sys
from typing import List, Optional

from installer.doctor import run_doctor
from installer.engine import InstallEngine
from installer.metadata import InstallMetadata, is_development_checkout
from installer.paths import (
    current_link_path,
    resolve_cache_dir,
    resolve_managed_root,
)
from installer.update_check import (
    check_for_update,
    prompt_update_decision,
    record_skip_version,
    record_snooze,
)


def print_version(managed_root: Optional[str] = None) -> None:
    """Print Fixer MCP version identity."""
    m_root = resolve_managed_root(managed_root)
    meta = InstallMetadata.load(m_root)
    is_dev = is_development_checkout(m_root)

    if meta:
        rev_str = f", commit {meta.source_revision[:8]}" if meta.source_revision else ""
        plat_str = f", {meta.platform}" if meta.platform else ""
        mode_str = f", {meta.mode}"
        print(f"Fixer MCP version {meta.version} ({mode_str}{plat_str}{rev_str})")
    elif is_dev:
        print("Fixer MCP version dev (development checkout)")
    else:
        print("Fixer MCP (uninitialized managed installation)")


def handle_update_command(args: List[str], engine: InstallEngine) -> int:
    """Handle explicit 'fixer update' or 'fixer update --check'."""
    parser = argparse.ArgumentParser(prog="fixer update", description="Update Fixer MCP to latest stable release.")
    parser.add_argument("--check", action="store_true", help="Only check for available updates without applying.")
    parser.add_argument("--descriptor", default=None, help="Explicit release descriptor source (URL or file path).")
    parser.add_argument("--payload", default=None, help="Explicit release payload archive source.")
    parser.add_argument("--force", action="store_true", help="Force update even if deferred or active.")

    parsed_args = parser.parse_args(args)

    meta = InstallMetadata.load(engine.managed_root)
    if is_development_checkout(engine.managed_root):
        sys.stderr.write("Auto-update is disabled for development checkouts.\n")
        return 1

    current_ver = meta.version if meta else "0.0.0"
    descriptor_source = parsed_args.descriptor or (meta.descriptor_source if meta else None)

    if not descriptor_source:
        sys.stderr.write("No release descriptor source configured. Provide --descriptor <source>.\n")
        return 1

    if parsed_args.check:
        print(f"Checking for updates (current version: {current_ver})...")
        res = check_for_update(descriptor_source, current_ver, engine.cache_dir, timeout=5.0, force=True)
        if res.offline_or_error:
            sys.stderr.write(f"Unable to check for updates: {res.error_message}\n")
            return 1
        if res.update_available:
            print(f"Update available: {res.latest_version} (installed: {current_ver})")
            if res.descriptor and res.descriptor.get("changelog"):
                print(f"Changelog: {res.descriptor['changelog']}")
        else:
            print(f"Fixer MCP is up to date ({current_ver}).")
        return 0

    print(f"Updating Fixer MCP from {descriptor_source}...")
    try:
        apply_res = engine.install_or_update(
            descriptor_source=descriptor_source,
            payload_source=parsed_args.payload,
            force_defer_override=parsed_args.force,
        )
        if apply_res.status == "deferred":
            print(f"\n[!] {apply_res.message}")
            return 0
        print(f"\n[OK] Fixer MCP updated to version {apply_res.version}.")
        ensure_control_plane_on_path(engine)
        return 0
    except Exception as e:
        sys.stderr.write(f"Update failed: {e}\n")
        return 1


def ensure_control_plane_on_path(engine: InstallEngine) -> None:
    """Install/refresh the fixerctl shim after an install or update (track б).

    The self-update path does not reinstall shims, so a release that ships the
    control plane must publish it here as well; payloads without it are a no-op.
    """

    try:
        # Prefer the shim implementation from the newly active payload. The
        # installer process itself is imported from the previous release while
        # an update is being applied; importing that stale template would
        # recreate an old `fixer` entry point.
        import importlib.util

        shim_module = None
        active_root = current_link_path(engine.managed_root)
        if os.path.isdir(os.path.join(active_root, "payload")):
            active_root = os.path.join(active_root, "payload")
        active_shim = os.path.join(active_root, "installer", "shim.py")
        if os.path.isfile(active_shim):
            spec = importlib.util.spec_from_file_location("fixer_active_shim", active_shim)
            if spec and spec.loader:
                shim_module = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(shim_module)
        if shim_module is None:
            from installer import shim as shim_module

        # Refresh both names after a self-update. The active Python installer
        # may predate the release that introduced the native console, so the
        # canonical `fixer` shim must be regenerated from the newly active
        # payload before the next shell invocation.
        command_shim = shim_module.install_command_shim(engine.user_bin_dir, engine.managed_root)
        if command_shim:
            print(f"[OK] Command shim available at {command_shim}")
        installed = shim_module.install_fixerctl_shim(engine.user_bin_dir, engine.managed_root)
        if installed:
            print(f"[OK] Control plane available at {installed}")
    except Exception as exc:  # never fail an update because of the extra binary
        sys.stderr.write(f"[!] Could not install the control plane: {exc}\n")


def is_bypass_command(args: List[str]) -> bool:
    """Check if command should bypass update check/prompts."""
    if not args:
        return False
    cmd0 = args[0].lower()
    if cmd0 in ("--help", "-h", "-help", "help", "--version", "-v", "-version", "version", "doctor"):
        return True
    if cmd0 in ("mcp", "--mcp", "worker", "--worker"):
        return True
    return False


def is_known_wire_flag(arg: str) -> bool:
    """Explicit wire flags stay invocable; an unknown *word* must not.

    The Super-TUI owns operator work. An unrecognised positional argument used
    to be forwarded straight into `client_wires/fixer_wire.py`, which dropped
    the operator into the legacy line palette (``Register project name``,
    ``Project Hands workspace mode``). Flag-driven automation keeps working;
    stray words now fail loudly instead.
    """

    if not arg.startswith("-"):
        return False
    return True


def maybe_prompt_and_update(engine: InstallEngine) -> None:
    """
    Check for update before normal interactive entry and prompt user if appropriate.
    Bypasses when non-TTY, offline, disabled, or development checkout.
    """
    # Only prompt on interactive terminals
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        return

    # No auto-update for development checkouts
    if is_development_checkout(engine.managed_root):
        return

    meta = InstallMetadata.load(engine.managed_root)
    if not meta or not meta.descriptor_source:
        return

    # Check for update within 2.0s foreground network budget
    res = check_for_update(
        descriptor_source=meta.descriptor_source,
        current_version=meta.version,
        cache_dir=engine.cache_dir,
        timeout=2.0,
        force=False,
    )

    if not res.should_prompt or not res.latest_version:
        return

    decision = prompt_update_decision(res.latest_version, res.current_version)
    if decision == "update":
        print(f"Updating to {res.latest_version}...")
        try:
            apply_res = engine.install_or_update(meta.descriptor_source)
            if apply_res.status == "deferred":
                print(f"[!] {apply_res.message}")
            else:
                print(f"[OK] Updated to {apply_res.version}.\n")
                ensure_control_plane_on_path(engine)
        except Exception as e:
            sys.stderr.write(f"Update failed: {e}\nContinuing with installed version...\n")
    elif decision == "skip":
        record_skip_version(res.latest_version, engine.cache_dir)
    else:  # later
        record_snooze(engine.cache_dir, duration_seconds=86400)


def print_help() -> None:
    """Print Fixer MCP usage and commands."""
    print(
        "Usage: fixer [COMMAND | OPTIONS]\n\n"
        "Fixer MCP control plane and launcher.\n\n"
        "Commands:\n"
        "  doctor              Run environment, runtime binary, and database diagnostics\n"
        "  update              Update Fixer MCP to the latest stable release\n"
        "  update --check      Check for available updates without applying\n\n"
        "Options:\n"
        "  --version, -V       Show version and installation metadata\n"
        "  --help, -h          Show this help message\n"
        "  --wire-info         Display client wire bootstrap information\n"
    )


def launch_wire_entrypoint(managed_root: str, args: List[str]) -> None:
    """Launch the client_wires fixer_wire.py entrypoint from the current active release."""
    # Repository root of this installer checkout. In the module-source layout the
    # wires live next to installer/; inside an assembled release the payload root
    # holds installer/, packaging/ and client_wires/ side by side.
    repo_root = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    current_dir = current_link_path(managed_root)
    release_root = (
        os.path.join(current_dir, "payload")
        if os.path.isdir(os.path.join(current_dir, "payload"))
        else current_dir
    )

    wire_script = os.path.join(release_root, "client_wires", "fixer_wire.py")
    if not os.path.isfile(wire_script):
        # Development checkout: run the wires from this repository instead of the
        # managed root, which is not a checkout at all.
        release_root = repo_root
        wire_script = os.path.join(release_root, "client_wires", "fixer_wire.py")

    if not os.path.isfile(wire_script):
        sys.stderr.write(f"Error: Could not locate client wire entrypoint at {wire_script}\n")
        sys.exit(1)

    os.environ["FIXER_RUNTIME_ROOT"] = release_root
    cur_pp = os.environ.get("PYTHONPATH", "")
    if release_root not in cur_pp.split(os.pathsep):
        os.environ["PYTHONPATH"] = f"{release_root}{os.pathsep}{cur_pp}" if cur_pp else release_root

    cmd = [sys.executable, wire_script] + args
    os.execv(sys.executable, cmd)


def main(argv: Optional[List[str]] = None) -> None:
    if argv is None:
        argv = sys.argv[1:]

    engine = InstallEngine()

    if not argv:
        # Default launcher entry
        maybe_prompt_and_update(engine)
        launch_wire_entrypoint(engine.managed_root, argv)
        return

    first = argv[0]

    if first in ("--help", "-h", "-help", "help") and len(argv) == 1:
        print_help()
        sys.exit(0)

    if first in ("--version", "-V", "-version", "version"):
        print_version(engine.managed_root)
        sys.exit(0)

    if first == "doctor":
        rep = run_doctor(
            managed_root=engine.managed_root,
            state_dir=engine.state_dir,
            config_dir=engine.config_dir,
            cache_dir=engine.cache_dir,
            user_bin_dir=engine.user_bin_dir,
            db_path=engine.db_path,
        )
        print(rep.format_text())
        sys.exit(0 if rep.is_healthy else 1)

    if first == "update":
        code = handle_update_command(argv[1:], engine)
        sys.exit(code)

    if is_bypass_command(argv):
        # Pass directly without update check
        launch_wire_entrypoint(engine.managed_root, argv)
        return

    if not is_known_wire_flag(argv[0]):
        sys.stderr.write(
            f"Unknown command {argv[0]!r}. Open the operator console with `fixer`, "
            "or run `fixer help`.\n"
        )
        sys.exit(2)

    # Normal command invocation: prompt if interactive then launch
    maybe_prompt_and_update(engine)
    launch_wire_entrypoint(engine.managed_root, argv)


if __name__ == "__main__":
    main()
