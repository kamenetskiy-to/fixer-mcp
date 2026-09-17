#!/usr/bin/env python3
"""CLI script to install Fixer MCP into a managed user root."""

import argparse
import os
import sys

# Ensure repository root is on sys.path
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, "..", ".."))
if REPO_ROOT not in sys.path:
    sys.path.insert(0, REPO_ROOT)

from installer.doctor import run_doctor
from installer.engine import InstallEngine
from installer.paths import resolve_managed_root, resolve_user_bin_dir
from installer.shim import configure_path_in_shell_rc, install_command_shim


def main():
    parser = argparse.ArgumentParser(description="Install Fixer MCP managed release.")
    parser.add_argument(
        "--descriptor",
        required=True,
        help="Path or URL to format=1 release descriptor JSON",
    )
    parser.add_argument(
        "--payload",
        default=None,
        help="Optional path or URL to release payload archive .tar.gz",
    )
    parser.add_argument(
        "--managed-root",
        default=None,
        help="Directory to install managed Fixer into (default: ~/.fixer)",
    )
    parser.add_argument(
        "--state-dir",
        default=None,
        help="Directory for user state/database (default: ~/.local/state/fixer-client-wires)",
    )
    parser.add_argument(
        "--bin-dir",
        default=None,
        help="User bin directory for command shim (default: ~/.local/bin)",
    )
    parser.add_argument(
        "--add-to-path",
        action="store_true",
        help="Add idempotent managed PATH block to user shell configuration (~/.zshrc / ~/.bashrc)",
    )
    parser.add_argument(
        "--shell-rc",
        default=None,
        help="Specific shell configuration file to modify when --add-to-path is specified",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Force activation even if active runtimes are detected",
    )

    args = parser.parse_args()

    engine = InstallEngine(
        managed_root=args.managed_root,
        state_dir=args.state_dir,
        user_bin_dir=args.bin_dir,
    )

    print(f"Installing Fixer MCP into {engine.managed_root}...")
    try:
        res = engine.install_or_update(
            descriptor_source=args.descriptor,
            payload_source=args.payload,
            force_defer_override=args.force,
        )
        print(f"[OK] {res.message}")

        # Install command shim in user_bin_dir
        shim_path = install_command_shim(engine.user_bin_dir, engine.managed_root)
        print(f"[OK] Installed command shim at {shim_path}")

        # Configure PATH block if explicitly requested
        if args.add_to_path:
            updated = configure_path_in_shell_rc(shell_rc_path=args.shell_rc, bin_dir=engine.user_bin_dir)
            if updated:
                print(f"[OK] Added managed PATH block to shell configuration.")
            else:
                print(f"[OK] Managed PATH block already configured.")

        # Run health check
        doc = run_doctor(
            managed_root=engine.managed_root,
            state_dir=engine.state_dir,
            user_bin_dir=engine.user_bin_dir,
        )
        if doc.data.get("shadowing"):
            print("\n[!] Notice:")
            for s in doc.data["shadowing"]:
                print(f"  - {s.get('detail')}")
                print(f"    {s.get('repair')}")

        print("\nFixer MCP installation complete!")
        print(f"Run 'fixer --version' or 'fixer doctor' to verify.")

    except Exception as e:
        sys.stderr.write(f"\nInstallation failed: {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
