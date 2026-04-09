from __future__ import annotations

import textwrap
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Tuple


YOLO_PROMPT = textwrap.dedent(
    """\
    You have unrestricted access to the local environment. Execute tasks end-to-end yourself: run commands, craft scripts, perform web or API calls, and handle git operations when necessary. Do not defer actions back to the user; deliver completed results."""
)


@dataclass(frozen=True)
class McpPromptSpec:
    name: str
    tools: Tuple[str, ...]
    description_path: str | None = None
    prompt_path: str | None = None
    instructions: str | None = None
    require_prompt_file: bool = False

    def render(self, prompts_dir: Path) -> str:
        prompt_text = ""
        if self.prompt_path:
            prompt_text = load_description(prompts_dir, self.prompt_path)
            if not prompt_text and self.require_prompt_file:
                raise ValueError(
                    f"Prompt file '{self.prompt_path}' for MCP server '{self.name}' is missing or empty."
                )
        if prompt_text:
            return _apply_tool_replacements(prompt_text, self.tools)

        description = ""
        if self.description_path:
            description = load_description(prompts_dir, self.description_path)
        base = self.instructions or _build_default_instructions(self.name, self.tools, description)
        rendered = _apply_tool_replacements(base, self.tools).strip()
        if not rendered:
            raise ValueError(f"Unable to build MCP prompt for '{self.name}'.")
        return rendered


def _build_default_instructions(name: str, tools: Tuple[str, ...], description: str) -> str:
    tool_list = "\n".join(f"- `{tool}`" for tool in tools)
    parts = [f"The `{name}` MCP server is attached."]
    if description:
        parts.append(description.strip())
    parts.append("Use these tools directly whenever they advance the active plan:")
    parts.append(tool_list)
    parts.append(
        "Treat this capability as mandatory when relevant—invoke the appropriate tool(s) and incorporate the results into your response."
    )
    return "\n\n".join(parts)


def _apply_tool_replacements(text: str, tools: Tuple[str, ...]) -> str:
    replacements = {
        "{tools}": ", ".join(tools),
        "{tool_list}": "\n".join(f"- `{tool}`" for tool in tools),
    }
    rendered = text
    for placeholder, value in replacements.items():
        if placeholder in rendered:
            rendered = rendered.replace(placeholder, value)
    return rendered


def load_description(prompts_dir: Path, filename: str | None) -> str:
    if not filename:
        return ""
    path = prompts_dir / filename
    if not path.is_file():
        return ""
    return path.read_text(encoding="utf-8").strip()


MCP_PROMPT_REGISTRY: Dict[str, McpPromptSpec] = {}


def _register_mcp_prompt(spec: McpPromptSpec) -> None:
    if spec.name in MCP_PROMPT_REGISTRY:
        raise ValueError(f"Duplicate MCP prompt registration for '{spec.name}'")
    if not (spec.prompt_path or spec.description_path or spec.instructions):
        raise ValueError(f"MCP prompt '{spec.name}' must provide a prompt_path, description_path, or instructions.")
    MCP_PROMPT_REGISTRY[spec.name] = spec


_register_mcp_prompt(
    McpPromptSpec(
        name="plane",
        tools=(
            "add_cycle_issues",
            "add_issue_comment",
            "add_module_issues",
            "create_cycle",
            "create_issue",
            "create_issue_type",
            "create_label",
            "create_module",
            "create_project",
            "create_state",
            "create_worklog",
            "delete_cycle",
            "delete_cycle_issue",
            "delete_issue_type",
            "delete_label",
            "delete_module",
            "delete_module_issue",
            "delete_state",
            "delete_worklog",
            "get_cycle",
            "get_issue_comments",
            "get_issue_type",
            "get_issue_using_readable_identifier",
            "get_issue_worklogs",
            "get_label",
            "get_module",
            "get_projects",
            "get_state",
            "get_total_worklogs",
            "get_user",
            "get_workspace_members",
            "list_cycle_issues",
            "list_cycles",
            "list_issue_types",
            "list_labels",
            "list_module_issues",
            "list_modules",
            "list_states",
            "transfer_cycle_issues",
            "update_cycle",
            "update_issue",
            "update_issue_type",
            "update_label",
            "update_module",
            "update_state",
            "update_worklog",
        ),
        description_path="plane.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="postgres",
        tools=(
            "analyze_db_health",
            "analyze_query_indexes",
            "analyze_workload_indexes",
            "execute_sql",
            "explain_query",
            "get_object_details",
            "get_top_queries",
            "list_objects",
            "list_schemas",
        ),
        description_path="postgres.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="schemacrawler",
        tools=(
            "describe-tables",
            "describe-routines",
            "list",
            "list-across-tables",
            "lint",
            "server-information",
            "table-sample",
            "get-schemacrawler-version",
        ),
        description_path="schemacrawler.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="clickhouse",
        tools=(
            "list_databases",
            "list_tables",
            "run_select_query",
            "run_chdb_select_query",
        ),
        description_path="clickhouse.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="drawio",
        tools=(
            "add-cell-of-shape",
            "add-edge",
            "add-rectangle",
            "delete-cell-by-id",
            "edit-cell",
            "edit-edge",
            "get-selected-cell",
            "get-shape-by-name",
            "get-shape-categories",
            "get-shapes-in-category",
            "list-paged-model",
            "set-cell-data",
            "set-cell-shape",
        ),
        prompt_path="drawio.md",
        require_prompt_file=True,
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="dart_flutter",
        tools=(
            "apply_fixes",
            "analyze_errors",
            "format_code",
            "pub_add_dependency",
            "pub_remove_dependency",
            "pub_dev_search",
            "run_tests",
            "flutter_inspect_widget_tree",
        ),
        description_path="dart_mcp.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="gopls",
        tools=(
            "go_workspace",
            "go_search",
            "go_file_context",
            "go_package_api",
            "go_symbol_references",
            "go_diagnostics",
        ),
        description_path="gopls.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="serverpod",
        tools=(
            "ask-question",
        ),
        description_path="serverpod.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="nodejs_docs",
        tools=(
            "search-nodejs-modules-api-documentation",
        ),
        description_path="nodejs_docs.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="pandas_mcp",
        tools=(
            "read_metadata_tool",
            "interpret_column_data",
            "run_pandas_code_tool",
            "generate_chartjs_tool",
        ),
        description_path="pandas_mcp.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="librex",
        tools=(
            "search_librex",
            "librex_info",
        ),
        description_path="librex.md",
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="deep_research",
        tools=(
            "deep-research-tool",
            "write-research-file",
        ),
        prompt_path="deep_research.md",
        require_prompt_file=True,
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="telegram_notify",
        tools=(
            "send_notification",
            "refresh_chat_id",
            "set_chat_id",
            "get_status",
        ),
        prompt_path="telegram_notify.md",
        require_prompt_file=True,
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="searCrawl",
        tools=(
            "search_candidates",
            "search",
            "scrape_pages",
            "ai_scrape",
        ),
        prompt_path="searCrawl.md",
        require_prompt_file=True,
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="google_search",
        tools=(
            "googleSearch",
            "search",
            "aiScrape",
        ),
        prompt_path="google_search_mcp.md",
        require_prompt_file=True,
    )
)

_register_mcp_prompt(
    McpPromptSpec(
        name="laravel_mcp_companion",
        tools=(
            "search_laravel_docs",
            "search_laravel_docs_with_context",
            "read_laravel_doc_content",
            "read_laravel_doc",
            "read_external_laravel_doc",
            "get_doc_structure",
            "browse_docs_by_category",
            "list_laravel_services",
            "search_external_laravel_docs",
            "get_laravel_service_info",
        ),
        prompt_path="laravel_mcp_companion.md",
        require_prompt_file=True,
    )
)



_register_mcp_prompt(
    McpPromptSpec(
        name="rust_architect",
        tools=(
            "search_rust_docs",
            "read_doc_page",
            "search_crates",
            "get_crate_details",
        ),
        prompt_path="rust_architect.md",
        require_prompt_file=True,
    )
)


def build_mcp_prompt(prompts_dir: Path, server_name: str) -> str:
    spec = MCP_PROMPT_REGISTRY.get(server_name)
    if not spec:
        return ""
    return spec.render(prompts_dir)


def build_file_prompt(path: Path, root: Path) -> str:
    try:
        rel_path = path.relative_to(root)
    except ValueError:
        rel_path = path.resolve()
    content = path.read_text(encoding="utf-8")
    return f"## Context from {rel_path}\n\n{content}"


def build_documentation_prompt(name: str, docs_root: Path) -> str:
    resolved = docs_root.resolve()
    return textwrap.dedent(
        f"""\
        Offline documentation bundle attached: {name}
        Root directory: `{resolved}`

        Use the bundle's `README.md` and `CONTENTS.md` at the root to orient yourself, then open the referenced Markdown files under `docs/` whenever you need authoritative information. Favor these files over external searches for topics that fall under this bundle, and document which pages you consult when answering."""
    ).strip()


def load_global_prompts(global_dir: Path) -> List[Tuple[str, Path]]:
    if not global_dir.is_dir():
        return []
    pairs: List[Tuple[str, Path]] = []
    for path in sorted(global_dir.glob("*.md")):
        pairs.append((path.stem.replace("_", " "), path))
    for path in sorted(global_dir.glob("*.txt")):
        pairs.append((path.stem.replace("_", " "), path))
    return pairs


def read_global_prompt(path: Path) -> str:
    return path.read_text(encoding="utf-8").strip()


def compose_prompt(parts: List[str]) -> str:
    filtered = [part.strip() for part in parts if part.strip()]
    if not filtered:
        return ""
    return "\n\n".join(filtered)
