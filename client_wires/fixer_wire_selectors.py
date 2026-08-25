"""Interactive selector helpers for the Fixer launcher frontend."""

from __future__ import annotations

import re
import textwrap
from dataclasses import replace
from pathlib import Path
from typing import Any, Callable, Sequence

from client_wires.backends import (
    DEFAULT_BACKEND,
    available_backend_descriptors,
    normalize_backend_name,
    subscribed_backend_descriptors,
)
from client_wires.backends.codex_adapter import (
    codex_default_model_for_family,
    codex_model_display_label,
    codex_model_family_for_model,
    codex_model_family_label,
    codex_model_options_for_family,
)
from client_wires.codex_compat.ui import BACK_VALUE, BackNavigation
from client_wires import fixer_wire_db
from client_wires import fixer_wire_mcp
from client_wires import fixer_wire_prompts
from client_wires import ai_limits

SCAFFOLD_MVP_ACTION = "__scaffold_mvp__"
UNATTACHED_FIXER_ACTION = "__unattached_fixer__"
TOGGLE_ARCHIVED_VALUE = "__toggle_archived__"
FIXER_LAUNCH_NEW = "__fixer_launch_new__"
FIXER_LAUNCH_RESUME = "__fixer_launch_resume__"
OVERSEER_LAUNCH_NEW = "__overseer_launch_new__"
OVERSEER_LAUNCH_RESUME = "__overseer_launch_resume__"
HANDS_LAUNCH_NEW = "__hands_launch_new__"
HANDS_LAUNCH_RESUME = "__hands_launch_resume__"
HANDS_WORKSPACE_SAFE = "__hands_workspace_safe__"
HANDS_WORKSPACE_HOTFIX = "__hands_workspace_hotfix__"
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
        ("fixer", "Fixer (Оркестратор)"),
        ("netrunner", "Hands (Исполнитель)"),
        ("overseer", "Overseer (Глобальный Fixer-помощник)"),
    ]
    choice = single_select_items(
        [Option(label, value) for value, label in options],
        title="Select mode (enter confirm, q cancel)",
        preselected_value="fixer",
    )
    if choice is None:
        print("Cancelled.")
        raise SystemExit(130)
    if choice == BACK_VALUE:
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
    if selected == BACK_VALUE:
        raise BackNavigation()
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
        Option("Start Unattached Fixer", UNATTACHED_FIXER_ACTION),
    ]
    selected = single_select_items(
        options,
        title="Fixer global session mode (enter confirm, q cancel)",
        preselected_value=FIXER_LAUNCH_NEW,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    if selected == BACK_VALUE:
        return FIXER_LAUNCH_NEW
    selected_text = str(selected)
    if selected_text not in {FIXER_LAUNCH_NEW, FIXER_LAUNCH_RESUME, UNATTACHED_FIXER_ACTION}:
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
    if selected == BACK_VALUE:
        raise BackNavigation()
    selected_text = str(selected)
    if selected_text not in {OVERSEER_LAUNCH_NEW, OVERSEER_LAUNCH_RESUME}:
        raise RuntimeError(f"Unexpected Overseer launch mode: {selected_text}")
    return selected_text


def _select_hands_launch_action_interactive(Option: Any, single_select_items: Any, *, has_resume: bool) -> str:
    options = [Option("Project Hands launch", is_header=True)]
    if has_resume:
        options.append(Option("Resume a saved Руки client", HANDS_LAUNCH_RESUME))
    options.append(Option("Start a new Руки client", HANDS_LAUNCH_NEW))
    selected = single_select_items(
        options,
        title="Project Hands session mode (enter confirm, q cancel)",
        preselected_value=HANDS_LAUNCH_RESUME if has_resume else HANDS_LAUNCH_NEW,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text not in {HANDS_LAUNCH_NEW, HANDS_LAUNCH_RESUME}:
        raise RuntimeError(f"Unexpected Project Hands launch mode: {selected_text}")
    return selected_text


def _select_hands_workspace_mode_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Project Hands workspace", is_header=True),
        Option("Safe — isolated worktree", HANDS_WORKSPACE_SAFE),
        Option("Hotfix — current live worktree", HANDS_WORKSPACE_HOTFIX),
    ]
    selected = single_select_items(
        options,
        title="Project Hands workspace mode (enter confirm, q cancel)",
        preselected_value=HANDS_WORKSPACE_SAFE,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text not in {HANDS_WORKSPACE_SAFE, HANDS_WORKSPACE_HOTFIX}:
        raise RuntimeError(f"Unexpected Project Hands workspace mode: {selected_text}")
    return selected_text


def _select_hands_docs_interactive(
    doc_entries: Sequence[Any],
    preselected_ids: Sequence[int],
    Option: Any,
    multi_select_items: Any,
) -> list[int]:
    if not doc_entries:
        return []
    by_id = {int(entry.doc_id): entry for entry in doc_entries}
    children: dict[int, list[int]] = {}
    for entry in doc_entries:
        parent = int(getattr(entry, "parent_id", 0) or 0)
        if parent not in by_id:
            parent = 0
        children.setdefault(parent, []).append(int(entry.doc_id))
    roots = children.get(0, [])

    def _subtree(doc_id: int) -> list[int]:
        collected = [doc_id]
        for child in children.get(doc_id, []):
            collected.extend(_subtree(child))
        return collected

    selected = {int(doc_id) for doc_id in preselected_ids if int(doc_id) in by_id}
    expanded: set[int] = set()
    while True:
        options = [Option("Project documentation tree", is_header=True)]
        visible_doc_ids: list[int] = []

        def _walk(doc_id: int, depth: int) -> None:
            entry = by_id[doc_id]
            kids = children.get(doc_id, [])
            indent = "  " * depth
            label = f"{indent}{entry.title}  ({entry.path or entry.slug or entry.status})"
            options.append(Option(label, doc_id))
            visible_doc_ids.append(doc_id)
            if not kids:
                return
            branch_ids = _subtree(doc_id)
            if doc_id in expanded:
                options.append(Option(f"{indent}  ▸ collapse branch", f"collapse:{doc_id}", instant=True))
                options.append(Option(
                    f"{indent}  ▸ toggle whole branch ({len(branch_ids)} docs)",
                    f"branch:{doc_id}",
                    instant=True,
                ))
                for kid in kids:
                    _walk(kid, depth + 1)
            else:
                options.append(Option(
                    f"{indent}  ▸ expand branch ({len(branch_ids)} docs)",
                    f"expand:{doc_id}",
                    instant=True,
                ))
                options.append(Option(
                    f"{indent}  ▸ toggle whole branch ({len(branch_ids)} docs)",
                    f"branch:{doc_id}",
                    instant=True,
                ))

        for root in roots:
            _walk(root, 0)
        result = multi_select_items(
            options,
            title="Attach project docs to Руки (space toggle, enter confirm, a toggle all, q cancel)",
            preselected_values=sorted(selected),
        )
        if result is None:
            print("Cancelled.")
            raise SystemExit(130)
        actions = [value for value in result if isinstance(value, str)]
        chosen_docs = {int(value) for value in result if not isinstance(value, str)}
        selected = (selected - set(visible_doc_ids)) | chosen_docs
        if not actions:
            return sorted(selected)
        for action in actions:
            verb, _, raw_id = action.partition(":")
            doc_id = int(raw_id)
            if verb == "expand":
                expanded.add(doc_id)
            elif verb == "collapse":
                expanded.discard(doc_id)
            elif verb == "branch":
                branch_ids = set(_subtree(doc_id))
                if branch_ids <= selected:
                    selected -= branch_ids
                else:
                    selected |= branch_ids


def _select_hands_resume_context_interactive(
    contexts: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> Any:
    if not contexts:
        raise RuntimeError("No saved Руки launch contexts were found for this project.")
    options = [Option("Руки launch contexts", is_header=True)]
    for index, context in enumerate(contexts):
        external = str(getattr(context, "external_session_id", "") or "").strip()
        resume_mark = "resume" if external else "fresh"
        docs_count = len(getattr(context, "doc_ids", ()) or ())
        mcp_count = len(getattr(context, "mcp_names", ()) or ())
        created = str(getattr(context, "created_at", "") or "")[:16]
        label = (
            f"{context.provider:<12} | {created} | {resume_mark} | "
            f"docs={docs_count} mcp={mcp_count} | {Path(str(context.worktree_path)).name}"
        )
        options.append(Option(label, index))
    selected = single_select_items(
        options,
        title="Select Руки client to resume (enter confirm, q cancel)",
        preselected_value=0,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return contexts[int(selected)]


def _select_manual_netrunner_kind_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Compatibility execution mode", is_header=True),
        Option("Project Hands execution generation [default]", NETRUNNER_KIND_MANUAL),
        Option("Governed acceptance generation", NETRUNNER_KIND_ACCEPTANCE),
    ]
    selected = single_select_items(
        options,
        title="Select compatibility execution type (enter confirm, q cancel)",
        preselected_value=NETRUNNER_KIND_MANUAL,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text in {NETRUNNER_KIND_MANUAL, NETRUNNER_KIND_ACCEPTANCE}:
        return selected_text
    raise RuntimeError(f"Unexpected compatibility execution type: {selected_text}")


def _select_project_hands_lane_interactive(
    lanes: Sequence[Any],
    default_lane: str,
    Option: Any,
    single_select_items: Any,
    *,
    select_backend_interactive: Callable[..., str] | None = None,
    select_model_interactive: Callable[..., str] | None = None,
) -> Any:
    if not lanes:
        raise RuntimeError("No provider lanes are registered for the current project's `Руки` actor.")

    by_provider = {str(lane.provider): lane for lane in lanes}
    backend_for_provider = {
        "codex": "codex",
        "commandcode": "commandcode",
        "claude": "claude",
        "kimi": "kimi-code",
        "antigravity": "antigravity",
        "grok": "grok",
    }
    provider_for_backend = {backend: provider for provider, backend in backend_for_provider.items()}

    select_backend = select_backend_interactive or _select_backend_interactive
    select_model = select_model_interactive or _select_model_interactive
    preferred_provider = default_lane if default_lane in by_provider else str(lanes[0].provider)
    selected_backend = normalize_backend_name(
        select_backend(
            backend_for_provider.get(preferred_provider, preferred_provider),
            Option,
            single_select_items,
        )
    )
    selected_provider = provider_for_backend.get(selected_backend, selected_backend)
    lane = by_provider.get(selected_provider)
    if lane is None:
        raise RuntimeError(f"Selected Project Hands lane {selected_backend!r} is unavailable.")

    preferred_model = str(getattr(lane, "model", "") or "").strip()
    selected_model = select_model(
        selected_backend,
        preferred_model,
        Option,
        single_select_items,
        **({"require_codex_model_family": True} if selected_backend == "codex" else {}),
    )
    return replace(lane, model=selected_model)


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

        options = [Option("Compatibility execution envelopes", is_header=True)]
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
            title="Select compatibility execution envelope (enter confirm, q cancel)",
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
        raise RuntimeError(f"No matching provider generations were found for compatibility session {session_id}.")

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
        title=f"Select disposable Codex generation for compatibility session {session_id} (enter confirm, q cancel)",
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
    descriptors = {descriptor.name: descriptor for descriptor in subscribed_backend_descriptors()}
    order = ("codex", "commandcode", "antigravity", "grok", "claude", "kimi-code")
    ordered = [name for name in order if name in descriptors]
    ordered += [name for name in descriptors if name not in order]
    options = [Option("CLI backends", is_header=True)]
    for name in ordered:
        descriptor = descriptors[name]
        label = descriptor.label
        if name == DEFAULT_BACKEND:
            label = f"{label} [default]"
        detail = ai_limits.backend_subscription_limits(name) or descriptor.description
        options.append(Option(f"{label} | {detail}", name))

    selected = single_select_items(
        options,
        title="Select CLI backend (enter confirm, q cancel)",
        preselected_value=normalize_backend_name(preferred_backend),
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    if selected == BACK_VALUE:
        raise BackNavigation()
    return normalize_backend_name(str(selected))


def _select_model_interactive(
    backend: str,
    preferred_model: str,
    Option: Any,
    single_select_items: Any,
    *,
    backend_descriptor: Callable[[str], Any] = fixer_wire_db._backend_descriptor,
    require_codex_model_family: bool = False,
) -> str:
    descriptor = backend_descriptor(backend)
    preferred_model = preferred_model.strip()
    if backend == "codex" and require_codex_model_family:
        preferred_family = codex_model_family_for_model(preferred_model or descriptor.default_model)
        family_options = [Option("Codex subscriptions", is_header=True)]
        for family in ("openai", "opencode-go", "commandcode"):
            family_options.append(
                Option(
                    f"{codex_model_family_label(family)} "
                    f"[default: {codex_default_model_for_family(family)}]",
                    family,
                )
            )
        family_choice = single_select_items(
            family_options,
            title="Select Codex subscription (enter confirm, q cancel)",
            preselected_value=preferred_family,
        )
        if family_choice is None:
            print("Cancelled.")
            raise SystemExit(130)
        if family_choice == BACK_VALUE:
            raise BackNavigation()
        family = str(family_choice).strip().lower()
        family_model_options = codex_model_options_for_family(descriptor.model_options, family)
        if not family_model_options:
            raise RuntimeError(f"No Codex model options are registered for family {family!r}.")
        if preferred_model not in family_model_options:
            preferred_model = codex_default_model_for_family(family)
        options = [Option(f"{codex_model_family_label(family)} models", is_header=True)]
        for model in family_model_options:
            label = codex_model_display_label(model)
            if model == preferred_model:
                label = f"{label} [default]"
            options.append(Option(label, model))
        selected = single_select_items(
            options,
            title=f"Select {codex_model_family_label(family)} model (enter confirm, q cancel)",
            preselected_value=preferred_model,
        )
        if selected is None:
            print("Cancelled.")
            raise SystemExit(130)
        if selected == BACK_VALUE:
            raise BackNavigation()
        selected_model = str(selected).strip()
        if selected_model not in family_model_options:
            raise RuntimeError(f"Selected Codex model {selected_model!r} is not part of family {family!r}.")
        return selected_model

    options = [Option(f"{descriptor.label} models", is_header=True)]
    for model in descriptor.model_options:
        label = codex_model_display_label(model) if backend == "commandcode" else model
        if model == descriptor.default_model:
            label = f"{label} [default]"
        options.append(Option(label, model))

    selected = single_select_items(
        options,
        title=f"Select {descriptor.label} model (enter confirm, q cancel)",
        preselected_value=preferred_model or descriptor.default_model,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    if selected == BACK_VALUE:
        raise BackNavigation()
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
    if selected == BACK_VALUE:
        raise BackNavigation()
    return str(selected).strip()
