#!/usr/bin/env python3
from __future__ import annotations

import argparse
import sqlite3
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from client_wires import fixer_wire

DEFAULT_POLL_INTERVAL_SEC = 5.0
DEFAULT_MAX_PARALLEL = 2
DEFAULT_MAX_RETRY_DELAY_SEC = 300.0


@dataclass(frozen=True)
class DispatchableSession:
    local_session_id: int
    global_session_id: int
    task_description: str
    status: str
    mcp_names: tuple[str, ...]
    attached_doc_count: int


@dataclass
class ActiveRun:
    session: DispatchableSession
    process: subprocess.Popen[str]
    attempt: int
    command: list[str]


@dataclass
class RetryEntry:
    attempt: int
    due_at_monotonic: float
    reason: str


def _parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Autonomous dispatcher for Fixer-backed Netrunner sessions."
    )
    parser.add_argument("--cwd", default=".", help="Project cwd to dispatch from.")
    parser.add_argument("--max-parallel", type=int, default=DEFAULT_MAX_PARALLEL)
    parser.add_argument("--poll-interval-sec", type=float, default=DEFAULT_POLL_INTERVAL_SEC)
    parser.add_argument("--max-retry-delay-sec", type=float, default=DEFAULT_MAX_RETRY_DELAY_SEC)
    parser.add_argument("--once", action="store_true", help="Run a single dispatch cycle and exit.")
    parser.add_argument("--dry-run", action="store_true", help="Print launch commands without starting Codex.")
    return parser.parse_args(argv)


def _attached_doc_count(conn: sqlite3.Connection, global_session_id: int) -> int:
    row = conn.execute(
        "SELECT COUNT(*) FROM netrunner_attached_doc WHERE session_id = ?",
        (global_session_id,),
    ).fetchone()
    return int(row[0]) if row else 0


def load_dispatchable_sessions(conn: sqlite3.Connection, project_id: int) -> list[DispatchableSession]:
    fixer_wire._ensure_wire_schema(conn)
    sessions = fixer_wire._load_session_rows(conn, project_id)
    out: list[DispatchableSession] = []
    for session in sessions:
        if session.status != "pending":
            continue
        mcp_names = tuple(fixer_wire._load_assigned_mcp_names(conn, session.global_session_id))
        attached_doc_count = _attached_doc_count(conn, session.global_session_id)
        out.append(
            DispatchableSession(
                local_session_id=session.session_id,
                global_session_id=session.global_session_id,
                task_description=session.task_description,
                status=session.status,
                mcp_names=mcp_names,
                attached_doc_count=attached_doc_count,
            )
        )
    return out


def _session_title(task_description: str) -> str:
    return fixer_wire._session_title(task_description, limit=80)


def build_netrunner_command(repo_root: Path, session: DispatchableSession) -> list[str]:
    command = [
        sys.executable,
        str((repo_root / "client_wires" / "fixer_wire.py").resolve()),
        "--role",
        "netrunner",
        "--netrunner-session-id",
        str(session.local_session_id),
    ]
    if session.mcp_names:
        command.extend(["--netrunner-mcp", ",".join(session.mcp_names)])
    return command


def _retry_delay_sec(attempt: int, max_retry_delay_sec: float) -> float:
    bounded_attempt = max(0, attempt - 1)
    return min(10.0 * (2**bounded_attempt), max_retry_delay_sec)


def reap_finished_runs(
    active_runs: dict[int, ActiveRun],
    retry_entries: dict[int, RetryEntry],
    *,
    max_retry_delay_sec: float,
    now_monotonic: float | None = None,
) -> None:
    now_value = time.monotonic() if now_monotonic is None else now_monotonic
    finished_ids: list[int] = []
    for global_session_id, run in active_runs.items():
        return_code = run.process.poll()
        if return_code is None:
            continue

        title = _session_title(run.session.task_description)
        if return_code == 0:
            print(
                f"[fixer-autopilot] session {run.session.local_session_id} finished cleanly: {title}"
            )
            retry_entries.pop(global_session_id, None)
        else:
            next_attempt = run.attempt + 1
            delay_sec = _retry_delay_sec(next_attempt, max_retry_delay_sec)
            retry_entries[global_session_id] = RetryEntry(
                attempt=next_attempt,
                due_at_monotonic=now_value + delay_sec,
                reason=f"process exited with code {return_code}",
            )
            print(
                "[fixer-autopilot] session "
                f"{run.session.local_session_id} failed (code={return_code}); "
                f"retry in {delay_sec:.1f}s"
            )
        finished_ids.append(global_session_id)

    for global_session_id in finished_ids:
        active_runs.pop(global_session_id, None)


def dispatch_pending_sessions(
    cwd: Path,
    *,
    max_parallel: int,
    active_runs: dict[int, ActiveRun],
    retry_entries: dict[int, RetryEntry],
    dry_run: bool,
    launcher: Callable[[list[str], Path], subprocess.Popen[str]] | None = None,
    now_monotonic: float | None = None,
) -> int:
    if max_parallel < 1:
        raise RuntimeError("--max-parallel must be >= 1")

    now_value = time.monotonic() if now_monotonic is None else now_monotonic
    db_path = fixer_wire._resolve_fixer_db_path(cwd)
    conn = sqlite3.connect(db_path)
    try:
        fixer_wire._ensure_wire_schema(conn)
        project_id = fixer_wire._resolve_project_id(conn, cwd)
        sessions = load_dispatchable_sessions(conn, project_id)
    finally:
        conn.close()

    repo_root = fixer_wire._repo_root()
    run_launcher = launcher or _default_launcher
    launched = 0
    slots_left = max_parallel - len(active_runs)
    for session in sessions:
        if slots_left <= 0:
            break
        if session.global_session_id in active_runs:
            continue
        if session.attached_doc_count <= 0:
            print(
                f"[fixer-autopilot] skip session {session.local_session_id}: no attached docs"
            )
            continue
        if not session.mcp_names:
            print(
                f"[fixer-autopilot] skip session {session.local_session_id}: no assigned MCP servers"
            )
            continue
        retry_entry = retry_entries.get(session.global_session_id)
        if retry_entry and retry_entry.due_at_monotonic > now_value:
            continue

        command = build_netrunner_command(repo_root, session)
        title = _session_title(session.task_description)
        if dry_run:
            print(
                f"[fixer-autopilot] dry-run launch session {session.local_session_id}: "
                f"{title} -> {command}"
            )
            launched += 1
            slots_left -= 1
            continue

        process = run_launcher(command, cwd)
        attempt = retry_entry.attempt if retry_entry else 1
        active_runs[session.global_session_id] = ActiveRun(
            session=session,
            process=process,
            attempt=attempt,
            command=command,
        )
        retry_entries.pop(session.global_session_id, None)
        launched += 1
        slots_left -= 1
        print(
            f"[fixer-autopilot] launched session {session.local_session_id} "
            f"(attempt {attempt}): {title}"
        )

    return launched


def _default_launcher(command: list[str], cwd: Path) -> subprocess.Popen[str]:
    return subprocess.Popen(
        command,
        cwd=str(cwd),
        text=True,
    )


def run_autopilot(
    cwd: Path,
    *,
    max_parallel: int,
    poll_interval_sec: float,
    max_retry_delay_sec: float,
    once: bool,
    dry_run: bool,
) -> int:
    active_runs: dict[int, ActiveRun] = {}
    retry_entries: dict[int, RetryEntry] = {}

    while True:
        reap_finished_runs(
            active_runs,
            retry_entries,
            max_retry_delay_sec=max_retry_delay_sec,
        )
        launched = dispatch_pending_sessions(
            cwd,
            max_parallel=max_parallel,
            active_runs=active_runs,
            retry_entries=retry_entries,
            dry_run=dry_run,
        )
        if once:
            if not dry_run:
                print(
                    f"[fixer-autopilot] once-mode launched {launched} session(s); "
                    f"active={len(active_runs)}"
                )
            return 0
        time.sleep(poll_interval_sec)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(sys.argv[1:] if argv is None else argv)
    cwd = Path(args.cwd).expanduser().resolve()
    try:
        return run_autopilot(
            cwd,
            max_parallel=args.max_parallel,
            poll_interval_sec=args.poll_interval_sec,
            max_retry_delay_sec=args.max_retry_delay_sec,
            once=args.once,
            dry_run=args.dry_run,
        )
    except RuntimeError as exc:
        print(f"[fixer-autopilot] {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
