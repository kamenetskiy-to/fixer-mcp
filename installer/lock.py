"""Concurrency locks and runtime usage guards for Fixer MCP installer."""

import fcntl
import os
import time
from typing import List, Optional, Tuple

from installer.errors import ActiveRuntimeError, ConcurrentInstallError
from installer.paths import active_pids_dir, lock_path, runtime_lock_path


class InstallLock:
    """
    Exclusive lock preventing concurrent installations or updates.
    Uses non-blocking fcntl.flock on <managed_root>/install.lock.
    """

    def __init__(self, managed_root: str):
        self.managed_root = managed_root
        self.lock_file = lock_path(managed_root)
        self._fd: Optional[int] = None

    def __enter__(self):
        os.makedirs(os.path.dirname(self.lock_file), exist_ok=True)
        try:
            self._fd = os.open(self.lock_file, os.O_CREAT | os.O_RDWR, 0o644)
            fcntl.flock(self._fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except (BlockingIOError, OSError) as exc:
            if self._fd is not None:
                try:
                    os.close(self._fd)
                except OSError:
                    pass
                self._fd = None
            raise ConcurrentInstallError(
                f"Another Fixer installer or updater is currently running (lock file: {self.lock_file})."
            ) from exc
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self._fd is not None:
            try:
                fcntl.flock(self._fd, fcntl.LOCK_UN)
            except OSError:
                pass
            try:
                os.close(self._fd)
            except OSError:
                pass
            self._fd = None


def is_pid_running(pid: int) -> bool:
    """Check if process with given PID is still alive."""
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        # Process exists, but owned by another user or restricted
        return True
    except OSError:
        return False


def register_runtime_process(state_dir: str, pid: Optional[int] = None) -> str:
    """Record an active runtime PID in state_dir/active_pids/<pid>."""
    if pid is None:
        pid = os.getpid()
    pids_dir = active_pids_dir(state_dir)
    os.makedirs(pids_dir, exist_ok=True)
    pid_file = os.path.join(pids_dir, str(pid))
    with open(pid_file, "w", encoding="utf-8") as f:
        f.write(f"{pid}\n{time.time()}\n")
    return pid_file


def unregister_runtime_process(state_dir: str, pid: Optional[int] = None) -> None:
    """Remove active runtime PID record."""
    if pid is None:
        pid = os.getpid()
    pid_file = os.path.join(active_pids_dir(state_dir), str(pid))
    if os.path.isfile(pid_file):
        try:
            os.unlink(pid_file)
        except OSError:
            pass


def is_runtime_active(state_dir: str) -> Tuple[bool, List[int]]:
    """
    Check if any Fixer runtime process is active against the shared state.
    Returns (is_active, list_of_active_pids).
    """
    active_pids: List[int] = []
    pids_dir = active_pids_dir(state_dir)
    if os.path.isdir(pids_dir):
        for entry in os.listdir(pids_dir):
            try:
                candidate_pid = int(entry)
            except ValueError:
                continue
            if is_pid_running(candidate_pid):
                active_pids.append(candidate_pid)
            else:
                # Clean up stale pid file
                try:
                    os.unlink(os.path.join(pids_dir, entry))
                except OSError:
                    pass

    # Also test runtime file lock if it exists
    lock_file = runtime_lock_path(state_dir)
    lock_held = False
    if os.path.isfile(lock_file):
        test_fd = None
        try:
            test_fd = os.open(lock_file, os.O_RDWR, 0o644)
            # Try to acquire exclusive lock; if fails with blocking/EAGAIN, someone holds shared or exclusive lock
            fcntl.flock(test_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            fcntl.flock(test_fd, fcntl.LOCK_UN)
        except (BlockingIOError, OSError):
            lock_held = True
        finally:
            if test_fd is not None:
                try:
                    os.close(test_fd)
                except OSError:
                    pass

    is_active = (len(active_pids) > 0) or lock_held
    return is_active, active_pids


def check_active_runtime(state_dir: str) -> None:
    """Raise ActiveRuntimeError if active Fixer runtime processes are detected."""
    active, pids = is_runtime_active(state_dir)
    if active:
        pids_str = ", ".join(str(p) for p in pids) if pids else "process lock held"
        raise ActiveRuntimeError(
            f"Active Fixer runtime detected ({pids_str}). Activation is deferred until running sessions exit."
        )
