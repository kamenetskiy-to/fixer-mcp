"""Fixer MCP Managed Installer and Update Engine.

Minimum Python Policy:
Fixer MCP supports Python 3.9 or newer (minimum supported floor: Python 3.9).
All installer, packaging, and client wire modules must be compatible with
Python 3.9. Python 3.12-only syntax (such as PEP 695 type statements or
PEP 701 backslashes in f-string expressions) is forbidden in released code.
The command shim enforces this floor at launch and fails cleanly if executed
with an older interpreter.
"""

MIN_PYTHON_VERSION = (3, 9)

from installer.doctor import DoctorReport, run_doctor
from installer.engine import ApplyResult, InstallEngine, StageResult
from installer.errors import (
    ActiveRuntimeError,
    CandidateBootstrapError,
    ConcurrentInstallError,
    DescriptorFetchError,
    DevelopmentCheckoutError,
    InstallerError,
    PayloadFetchError,
    VerificationError,
)
from installer.lock import InstallLock, is_runtime_active
from installer.metadata import InstallMetadata, is_development_checkout
from installer.paths import (
    resolve_cache_dir,
    resolve_config_dir,
    resolve_db_path,
    resolve_managed_root,
    resolve_state_dir,
    resolve_user_bin_dir,
)
from installer.shim import configure_path_in_shell_rc, detect_shadowing, install_command_shim, install_fixerctl_shim
from installer.update_check import check_for_update, prompt_update_decision

__all__ = [
    "InstallEngine",
    "StageResult",
    "ApplyResult",
    "InstallMetadata",
    "InstallLock",
    "is_runtime_active",
    "is_development_checkout",
    "DoctorReport",
    "run_doctor",
    "check_for_update",
    "prompt_update_decision",
    "install_command_shim",
    "install_fixerctl_shim",
    "detect_shadowing",
    "configure_path_in_shell_rc",
    "resolve_managed_root",
    "resolve_state_dir",
    "resolve_config_dir",
    "resolve_cache_dir",
    "resolve_user_bin_dir",
    "resolve_db_path",
    "InstallerError",
    "ConcurrentInstallError",
    "VerificationError",
    "CandidateBootstrapError",
    "ActiveRuntimeError",
    "DevelopmentCheckoutError",
    "DescriptorFetchError",
    "PayloadFetchError",
]
