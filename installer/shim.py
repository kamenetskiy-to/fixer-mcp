"""Command shim installation, shadowing detection, and idempotent PATH configuration."""

import os
import re
import shutil
import stat
import subprocess
from typing import Dict, List, Optional


PATH_BLOCK_START = "# >>> fixer managed path >>>"
PATH_BLOCK_END = "# <<< fixer managed path <<<"


SHIM_SCRIPT_TEMPLATE = '''#!/usr/bin/env python3
# Fixer MCP managed command shim
import os
import sys

MANAGED_ROOT = "__MANAGED_ROOT_PLACEHOLDER__"
USER_MANAGED_ROOT = os.environ.get("FIXER_MANAGED_ROOT", MANAGED_ROOT)

current_link = os.path.join(USER_MANAGED_ROOT, "current")
if not (os.path.isdir(current_link) or os.path.islink(current_link)):
    sys.stderr.write(
        f"Fixer MCP is not currently installed or 'current' link is missing at {USER_MANAGED_ROOT}.\\n"
        f"Run fixer installer or check your installation.\\n"
    )
    sys.exit(1)

# In the released payload layout, current_link may contain payload/ or be extracted directly
release_root = current_link
if os.path.isdir(os.path.join(current_link, "payload")):
    release_root = os.path.join(current_link, "payload")

# Build self-contained portable runtime path: release_root has installer, packaging, client_wires
if release_root not in sys.path:
    sys.path.insert(0, release_root)

os.environ["FIXER_RUNTIME_ROOT"] = release_root
os.environ["FIXER_MANAGED_ROOT"] = USER_MANAGED_ROOT

# Ensure PYTHONPATH carries release_root for child processes
cur_pp = os.environ.get("PYTHONPATH", "")
if release_root not in cur_pp.split(os.pathsep):
    os.environ["PYTHONPATH"] = f"{release_root}{os.pathsep}{cur_pp}" if cur_pp else release_root

try:
    from installer.cli import main as cli_main
    cli_main()
except Exception as e:
    sys.stderr.write(f"Fixer MCP execution failed: {e}\\n")
    sys.exit(1)
'''


def generate_shim_content(managed_root: str) -> str:
    """Generate the portable command shim script content."""
    root_str = os.path.abspath(managed_root).replace("\\", "\\\\").replace('"', '\\"')
    return SHIM_SCRIPT_TEMPLATE.replace("__MANAGED_ROOT_PLACEHOLDER__", root_str)


def install_command_shim(user_bin_dir: str, managed_root: str, shim_name: str = "fixer") -> str:
    """
    Idempotently install the command shim in user_bin_dir.
    Returns the absolute path to the installed shim.
    """
    user_bin_dir = os.path.abspath(os.path.expanduser(user_bin_dir))
    os.makedirs(user_bin_dir, exist_ok=True)
    shim_path = os.path.join(user_bin_dir, shim_name)

    content = generate_shim_content(managed_root)
    tmp_path = f"{shim_path}.tmp.{os.getpid()}"
    with open(tmp_path, "w", encoding="utf-8") as f:
        f.write(content)

    os.chmod(tmp_path, 0o755)
    os.replace(tmp_path, shim_path)
    return shim_path


def detect_shadowing(shim_path: str, shim_name: str = "fixer") -> List[Dict[str, str]]:
    """
    Detect if an alias, shell function, or conflicting binary shadows the shim.
    Returns list of diagnostic dictionaries with repair instructions.
    """
    issues: List[Dict[str, str]] = []
    shim_path = os.path.abspath(os.path.expanduser(shim_path))

    # 1. Check which binary resolution
    resolved_bin = shutil.which(shim_name)
    if resolved_bin:
        resolved_bin = os.path.abspath(resolved_bin)
        if resolved_bin != shim_path:
            issues.append({
                "type": "path_precedence",
                "detail": f"Command '{shim_name}' resolves to '{resolved_bin}' instead of '{shim_path}'.",
                "repair": (
                    f"Ensure '{os.path.dirname(shim_path)}' appears before "
                    f"'{os.path.dirname(resolved_bin)}' in your PATH."
                ),
            })

    # 2. Check shell configuration files for shadowing aliases or functions
    home = os.path.expanduser("~")
    rc_candidates = [
        os.path.join(home, ".zshrc"),
        os.path.join(home, ".bashrc"),
        os.path.join(home, ".bash_profile"),
        os.path.join(home, ".profile"),
    ]

    alias_pattern = re.compile(rf"^\s*alias\s+{re.escape(shim_name)}=", re.MULTILINE)
    func_pattern = re.compile(rf"^\s*(function\s+)?{re.escape(shim_name)}\s*\(\)\s*\{{", re.MULTILINE)

    for rc_file in rc_candidates:
        if not os.path.isfile(rc_file):
            continue
        try:
            with open(rc_file, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()

            if alias_pattern.search(content):
                issues.append({
                    "type": "alias",
                    "file": rc_file,
                    "detail": f"Shell alias for '{shim_name}' found in {rc_file}.",
                    "repair": f"Remove or comment out 'alias {shim_name}=...' in {rc_file} or run 'unalias {shim_name}'.",
                })
            elif func_pattern.search(content):
                issues.append({
                    "type": "function",
                    "file": rc_file,
                    "detail": f"Shell function '{shim_name}()' found in {rc_file}.",
                    "repair": f"Remove or rename the '{shim_name}()' function definition in {rc_file}.",
                })
        except OSError:
            pass

    return issues


def configure_path_in_shell_rc(
    shell_rc_path: Optional[str] = None,
    bin_dir: Optional[str] = None,
) -> bool:
    """
    Add narrowly-marked idempotent PATH block to shell configuration file.
    Does not edit arbitrary dotfiles unless explicitly requested.
    Returns True if file was updated, False if block was already present.
    """
    if not bin_dir:
        bin_dir = os.path.expanduser("~/.local/bin")
    bin_dir = os.path.abspath(bin_dir)

    if not shell_rc_path:
        home = os.path.expanduser("~")
        # Default to ~/.zshrc on macOS or ~/.bashrc
        zshrc = os.path.join(home, ".zshrc")
        bashrc = os.path.join(home, ".bashrc")
        shell_rc_path = zshrc if os.path.exists(zshrc) else bashrc

    shell_rc_path = os.path.abspath(shell_rc_path)
    os.makedirs(os.path.dirname(shell_rc_path), exist_ok=True)

    block_lines = [
        PATH_BLOCK_START,
        f'export PATH="{bin_dir}:$PATH"',
        PATH_BLOCK_END,
    ]
    block_str = "\n".join(block_lines) + "\n"

    existing_content = ""
    if os.path.isfile(shell_rc_path):
        with open(shell_rc_path, "r", encoding="utf-8", errors="ignore") as f:
            existing_content = f.read()

    # Check if block is already present
    if PATH_BLOCK_START in existing_content:
        # Idempotent: already configured
        return False

    # Append cleanly
    separator = "\n" if existing_content and not existing_content.endswith("\n") else ""
    with open(shell_rc_path, "a", encoding="utf-8") as f:
        f.write(f"{separator}\n{block_str}")

    return True
