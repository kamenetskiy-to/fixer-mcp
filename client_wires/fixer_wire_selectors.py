"""Interactive selector helpers for the Fixer launcher frontend."""

from __future__ import annotations

import re
import textwrap
from typing import Any, Callable, Sequence

from client_wires.backends import DEFAULT_BACKEND, available_backend_descriptors, normalize_backend_name
from client_wires import fixer_wire_db
from client_wires import fixer_wire_mcp
from client_wires import fixer_wire_prompts

SCAFFOLD_MVP_ACTION = "__scaffold_mvp__"
UNATTACHED_FIXER_ACTION = "__unattached_fixer__"
TOGGLE_ARCHIVED_VALUE = "__toggle_archived__"
FIXER_LAUNCH_NEW = "__fixer_launch_new__"
FIXER_LAUNCH_RESUME = "__fixer_launch_resume__"
OVERSEER_LAUNCH_NEW = "__overseer_launch_new__"
OVERSEER_LAUNCH_RESUME = "__overseer_launch_resume__"
RECENTLY_ACTIVE_STATUSES = {"in_progress"}
MCP_CATEGORY_ORDER = ("DB", "Web-search", "Design", "Productivity", "Coding", "Other")
MCP_FALLBACK_CATEGORY = "Other"
HIDDEN_MCP_SERVERS = fixer_wire_mcp.HIDDEN_MCP_SERVERS
ALWAYS_VISIBLE_MCP_NAMES = {fixer_wire_mcp.FIGMA_CONSOLE_MCP_NAME}
NETRUNNER_KIND_MANUAL = fixer_wire_prompts.NETRUNNER_KIND_MANUAL
NETRUNNER_KIND_ACCEPTANCE = fixer_wire_prompts.NETRUNNER_KIND_ACCEPTANCE

SessionRow = fixer_wire_db.SessionRow
RegistryMcpMetadata = fixer_wire_db.RegistryMcpMetadata


def _strip_md_prefix(text: str) -> str:
    return re.sub(r"^[#>*\-\s\d\.\)\(]+", "", text).strip()


def _session_title(task_description: str, *, limit: int = 110) -> str:
    stripped = task_description.strip()
    if not stripped:
        return "(empty task)"

    for line in stripped.splitlines():
        candidate = _strip_md_prefix(line)
        if not candidate:
            continue
        if candidate.lower() in {"goal", "цель"}:
            continue
        return textwrap.shorten(candidate, width=limit, placeholder="…")

    first_line = stripped.splitlines()[0]
    return textwrap.shorten(_strip_md_prefix(first_line) or first_line, width=limit, placeholder="…")


def _summary_provider(summary: Any) -> str:
    return normalize_backend_name(
        str(
            getattr(
                summary,
                "provider",
                getattr(summary, "backend", getattr(summary, "cli_backend", "codex")),
            )
            or "codex"
        )
    )


def _fixer_resume_value(summary: Any) -> str:
    provider = _summary_provider(summary)
    session_id = str(summary.session_id)
    if provider == DEFAULT_BACKEND:
        return session_id
    return f"{provider}:{session_id}"


def _provider_label(provider: str) -> str:
    descriptors = {descriptor.name: descriptor.label for descriptor in available_backend_descriptors()}
    return descriptors.get(provider, provider)


def _resume_session_label(summary: Any, *, preview_width: int = 42) -> str:
    provider = _summary_provider(summary)
    created_local = summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
    updated_local = summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
    preview = textwrap.shorten(
        _strip_md_prefix(getattr(summary, "preview", "") or "(no preview)"),
        width=preview_width,
        placeholder="…",
    )
    provider_text = textwrap.shorten(_provider_label(provider), width=12, placeholder="…")
    session_id = textwrap.shorten(str(summary.session_id), width=32, placeholder="…")
    return f"{provider_text:<12} | {preview:<42} | {created_local} | {updated_local} | {session_id}"


def _select_role_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        (UNATTACHED_FIXER_ACTION, "Unattached Fixer"),
        ("fixer", "Fixer (Project)"),
        ("overseer", "Overseer (Global)"),
        ("netrunner", "Netrunner (Worker)"),
    ]
    choice = single_select_items(
        [Option(label, value) for value, label in options],
        title="Select mode (enter confirm, q cancel)",
        preselected_value="fixer",
    )
    if choice is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(choice)


def _prompt_scaffold_value(prompt: str, *, default: str | None = None) -> str:
    while True:
        suffix = f" [{default}]" if default else ""
        raw = input(f"{prompt}{suffix}: ").strip()
        if raw.lower() in {"q", "quit", "exit"}:
            print("Cancelled.")
            raise SystemExit(130)
        if raw:
            return raw
        if default is not None:
            return default
        print("Value is required.")


def _select_scaffold_execution_mode_interactive(Option: Any, single_select_items: Any) -> bool:
    options = [
        Option("MVP scaffold mode", is_header=True),
        Option("Dry run only", "dry_run"),
        Option("Create scaffold", "create"),
    ]
    selected = single_select_items(
        options,
        title="Select scaffold mode (enter confirm, q cancel)",
        preselected_value="dry_run",
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text == "dry_run":
        return True
    if selected_text == "create":
        return False
    raise RuntimeError(f"Unexpected scaffold mode: {selected_text}")


def _select_fixer_launch_action_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Fixer global launch", is_header=True),
        Option("Start new Fixer", FIXER_LAUNCH_NEW),
        Option("Resume existing Fixer", FIXER_LAUNCH_RESUME),
    ]
    selected = single_select_items(
        options,
        title="Fixer global session mode (enter confirm, q cancel)",
        preselected_value=FIXER_LAUNCH_NEW,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text not in {FIXER_LAUNCH_NEW, FIXER_LAUNCH_RESUME}:
        raise RuntimeError(f"Unexpected Fixer launch mode: {selected_text}")
    return selected_text


def _select_overseer_launch_action_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Overseer project launch", is_header=True),
        Option("Start new Overseer", OVERSEER_LAUNCH_NEW),
        Option("Resume existing Overseer", OVERSEER_LAUNCH_RESUME),
    ]
    selected = single_select_items(
        options,
        title="Overseer project session mode (enter confirm, q cancel)",
        preselected_value=OVERSEER_LAUNCH_NEW,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text not in {OVERSEER_LAUNCH_NEW, OVERSEER_LAUNCH_RESUME}:
        raise RuntimeError(f"Unexpected Overseer launch mode: {selected_text}")
    return selected_text


def _select_manual_netrunner_kind_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Manual Netrunner mode", is_header=True),
        Option("Regular manual Netrunner [default]", NETRUNNER_KIND_MANUAL),
        Option("Acceptance manual Netrunner", NETRUNNER_KIND_ACCEPTANCE),
    ]
    selected = single_select_items(
        options,
        title="Select manual Netrunner type (enter confirm, q cancel)",
        preselected_value=NETRUNNER_KIND_MANUAL,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text in {NETRUNNER_KIND_MANUAL, NETRUNNER_KIND_ACCEPTANCE}:
        return selected_text
    raise RuntimeError(f"Unexpected manual Netrunner type: {selected_text}")


def _select_session_interactive(
    session_rows: Sequence[SessionRow],
    Option: Any,
    single_select_items: Any,
    *,
    session_title: Callable[[str], str] = _session_title,
) -> SessionRow:
    by_id = {row.session_id: row for row in session_rows}
    show_archived = not any(row.status in {"pending", "in_progress"} for row in session_rows)
    while True:
        if show_archived:
            visible = list(session_rows)
        else:
            visible = [row for row in session_rows if row.status in RECENTLY_ACTIVE_STATUSES]
            if not visible:
                visible = [row for row in session_rows if row.status in {"pending", "in_progress"}]

        if not visible:
            if not show_archived:
                show_archived = True
                continue
            raise RuntimeError("No sessions available for selection.")

        options = [Option("Netrunner sessions", is_header=True)]
        preselected: int | None = None
        for row in visible:
            external_suffix = ""
            if row.external_session_id:
                external_suffix = f" | {row.cli_backend}={row.external_session_id}"
            label = f"[{row.session_id}] {row.status:<11} | {session_title(row.task_description)}{external_suffix}"
            options.append(Option(label, row.session_id))
            if preselected is None and row.status == "in_progress":
                preselected = row.session_id

        toggle_label = "[+] Show archived statuses" if not show_archived else "[-] Hide archived statuses"
        options.append(Option(toggle_label, TOGGLE_ARCHIVED_VALUE))
        selected = single_select_items(
            options,
            title="Select netrunner session (enter confirm, q cancel)",
            preselected_value=preselected if preselected is not None else visible[0].session_id,
        )
        if selected is None:
            print("Cancelled.")
            raise SystemExit(130)
        if selected == TOGGLE_ARCHIVED_VALUE:
            show_archived = not show_archived
            continue

        session_id = int(selected)
        row = by_id.get(session_id)
        if row is None:
            raise RuntimeError(f"Selected session {session_id} is unavailable.")
        return row


def _select_mcp_interactive(
    registry_names: Sequence[str],
    assigned_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
    available_servers: dict[str, dict[str, object]],
    Option: Any,
    multi_select_items: Any,
    *,
    show_all_registry_names: bool = False,
    registry_metadata_with_fallback: Callable[
        [str, RegistryMcpMetadata | None],
        RegistryMcpMetadata | None,
    ] = fixer_wire_db._registry_metadata_with_fallback,
) -> list[str]:
    always_visible_names = ALWAYS_VISIBLE_MCP_NAMES.intersection(set(registry_names))
    if show_all_registry_names:
        names = sorted((set(registry_names) | set(assigned_names) | always_visible_names) - HIDDEN_MCP_SERVERS)
    else:
        default_names = {name for name in registry_names if registry_meta.get(name) and registry_meta[name].is_default}
        all_candidate_names = {*registry_names, *assigned_names}
        if default_names:
            names = sorted((default_names | set(assigned_names) | always_visible_names) - HIDDEN_MCP_SERVERS)
        else:
            names = sorted(all_candidate_names - HIDDEN_MCP_SERVERS)
    if not names:
        return []

    unavailable = {name for name in names if name not in available_servers}
    options = [Option("Session MCP defaults", is_header=True)]

    category_buckets: dict[str, list[str]] = {}
    for name in names:
        meta = registry_metadata_with_fallback(name, registry_meta.get(name))
        category = (meta.category if meta else "").strip() or MCP_FALLBACK_CATEGORY
        category_buckets.setdefault(category, []).append(name)

    def _category_sort_key(category: str) -> tuple[int, str]:
        try:
            return MCP_CATEGORY_ORDER.index(category), category.lower()
        except ValueError:
            return len(MCP_CATEGORY_ORDER), category.lower()

    for category in sorted(category_buckets.keys(), key=_category_sort_key):
        options.append(Option(category, is_header=True))
        for name in sorted(category_buckets[category]):
            meta = registry_metadata_with_fallback(name, registry_meta.get(name))
            label = name
            if meta and meta.is_default:
                label = f"{label} [default]"
            options.append(Option(label, name, disabled=name in unavailable))

    selected = multi_select_items(
        options,
        title="Select MCP servers (space toggle, enter confirm, a toggle all, q cancel)",
        preselected_values=[name for name in assigned_names if name in names and name not in unavailable],
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return [str(name) for name in selected if isinstance(name, str)]


def _select_fixer_resume_session_interactive(
    summaries: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> str:
    if not summaries:
        raise RuntimeError("No existing Fixer sessions were found for this project cwd.")

    options = [Option("Fixer sessions", is_header=True)]
    available_values = {_fixer_resume_value(summary) for summary in summaries}
    for summary in summaries:
        options.append(Option(_resume_session_label(summary), _fixer_resume_value(summary)))

    selected = single_select_items(
        options,
        title="Select Fixer session to resume (enter confirm, q cancel)",
        preselected_value=_fixer_resume_value(summaries[0]),
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)

    selected_text = str(selected)
    if selected_text not in available_values:
        raise RuntimeError(f"Selected Fixer session '{selected_text}' is unavailable.")
    return selected_text


def _select_overseer_resume_session_interactive(
    summaries: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> str:
    if not summaries:
        raise RuntimeError("No existing Overseer sessions were found for this project cwd.")

    options = [Option("Overseer sessions", is_header=True)]
    for summary in summaries:
        created_local = summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
        updated_local = summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
        preview = textwrap.shorten(
            _strip_md_prefix(getattr(summary, "preview", "") or "(no preview)"),
            width=66,
            placeholder="…",
        )
        label = f"[{summary.session_id}] started {created_local} | updated {updated_local} | {preview}"
        options.append(Option(label, summary.session_id))

    selected = single_select_items(
        options,
        title="Select Overseer session to resume (enter confirm, q cancel)",
        preselected_value=summaries[0].session_id,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)

    selected_text = str(selected)
    if not any(str(summary.session_id) == selected_text for summary in summaries):
        raise RuntimeError(f"Selected Codex session '{selected_text}' is unavailable.")
    return selected_text


def _select_netrunner_resume_session_interactive(
    summaries: Sequence[Any],
    session_id: int,
    Option: Any,
    single_select_items: Any,
    *,
    preferred_session_id: str | None = None,
) -> str:
    if not summaries:
        raise RuntimeError(f"No matching Codex sessions were found for netrunner session {session_id}.")

    options = [Option("Matching Codex sessions", is_header=True)]
    available_ids = {str(summary.session_id) for summary in summaries}
    for summary in summaries:
        created_local = summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
        updated_local = summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
        preview = textwrap.shorten(
            _strip_md_prefix(getattr(summary, "preview", "") or "(no preview)"),
            width=66,
            placeholder="…",
        )
        label = f"[{summary.session_id}] started {created_local} | updated {updated_local} | {preview}"
        options.append(Option(label, summary.session_id))

    selected = single_select_items(
        options,
        title=f"Select Codex session to resume for netrunner session {session_id} (enter confirm, q cancel)",
        preselected_value=preferred_session_id if preferred_session_id in available_ids else summaries[0].session_id,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)

    selected_text = str(selected)
    if selected_text not in available_ids:
        raise RuntimeError(f"Selected Codex session '{selected_text}' is unavailable.")
    return selected_text


def _select_backend_interactive(
    preferred_backend: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    descriptors = available_backend_descriptors()
    options = [Option("CLI backends", is_header=True)]
    for descriptor in descriptors:
        label = descriptor.label
        if descriptor.name == DEFAULT_BACKEND:
            label = f"{label} [default]"
        options.append(Option(f"{label} | {descriptor.description}", descriptor.name))

    selected = single_select_items(
        options,
        title="Select CLI backend (enter confirm, q cancel)",
        preselected_value=normalize_backend_name(preferred_backend),
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return normalize_backend_name(str(selected))


def _select_model_interactive(
    backend: str,
    preferred_model: str,
    Option: Any,
    single_select_items: Any,
    *,
    backend_descriptor: Callable[[str], Any] = fixer_wire_db._backend_descriptor,
) -> str:
    descriptor = backend_descriptor(backend)
    options = [Option(f"{descriptor.label} models", is_header=True)]
    for model in descriptor.model_options:
        label = model
        if model == descriptor.default_model:
            label = f"{label} [default]"
        options.append(Option(label, model))

    selected = single_select_items(
        options,
        title=f"Select {descriptor.label} model (enter confirm, q cancel)",
        preselected_value=preferred_model.strip() or descriptor.default_model,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(selected).strip()


def _select_reasoning_interactive(
    backend: str,
    preferred_reasoning: str,
    Option: Any,
    single_select_items: Any,
    *,
    backend_descriptor: Callable[[str], Any] = fixer_wire_db._backend_descriptor,
) -> str:
    descriptor = backend_descriptor(backend)
    options = [Option(f"{descriptor.label} reasoning", is_header=True)]
    for reasoning in descriptor.reasoning_options:
        label = reasoning
        if reasoning == descriptor.default_reasoning:
            label = f"{label} [default]"
        options.append(Option(label, reasoning))

    selected = single_select_items(
        options,
        title=f"Select {descriptor.label} reasoning (enter confirm, q cancel)",
        preselected_value=preferred_reasoning.strip() or descriptor.default_reasoning,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(selected).strip()
