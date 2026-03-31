from __future__ import annotations

import json
import re
import shutil
import subprocess
import textwrap
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

SERVERPOD_COMMAND = "serverpod"
DEFAULT_CODEX_MODEL = "gpt-5.4"
DEFAULT_CODEX_EFFORT = "medium"


@dataclass(frozen=True)
class MVPScaffoldSpec:
    raw_name: str
    project_slug: str
    target_root: Path
    destination: Path
    dry_run: bool = False

    @property
    def display_name(self) -> str:
        return self.project_slug.replace("_", " ").title()

    @property
    def server_dir_name(self) -> str:
        return f"{self.project_slug}_server"

    @property
    def client_dir_name(self) -> str:
        return f"{self.project_slug}_client"

    @property
    def flutter_dir_name(self) -> str:
        return f"{self.project_slug}_flutter"

    @property
    def ai_service_name(self) -> str:
        return f"{self.project_slug}_ai_service"


def normalize_project_slug(raw_name: str) -> str:
    lowered = raw_name.strip().lower()
    if not lowered:
        raise ValueError("Project name cannot be empty.")
    slug = re.sub(r"[^a-z0-9]+", "_", lowered).strip("_")
    slug = re.sub(r"_+", "_", slug)
    if not slug:
        raise ValueError("Project name must contain at least one ASCII letter or digit.")
    if slug[0].isdigit():
        slug = f"mvp_{slug}"
    return slug


def build_scaffold_spec(
    raw_name: str,
    *,
    target_dir: str | None = None,
    dry_run: bool = False,
) -> MVPScaffoldSpec:
    project_slug = normalize_project_slug(raw_name)
    root = Path(target_dir).expanduser() if target_dir else Path.cwd()
    resolved_root = root.resolve()
    destination = resolved_root / project_slug
    return MVPScaffoldSpec(
        raw_name=raw_name,
        project_slug=project_slug,
        target_root=resolved_root,
        destination=destination,
        dry_run=dry_run,
    )


def run_scaffold_cli(raw_name: str, *, target_dir: str | None = None, dry_run: bool = False) -> int:
    try:
        spec = build_scaffold_spec(raw_name, target_dir=target_dir, dry_run=dry_run)
        scaffold_mvp_project(spec)
    except (RuntimeError, ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(f"[fixer-wire] {exc}")
        return 2
    return 0


def scaffold_mvp_project(
    spec: MVPScaffoldSpec,
    *,
    command_runner: Callable[[list[str], Path], None] | None = None,
) -> None:
    if spec.destination.exists():
        raise RuntimeError(
            f"Destination already exists: {spec.destination}. "
            "Choose a different project name or target directory."
        )

    print(f"[fixer-wire] MVP scaffold slug: {spec.project_slug}")
    print(f"[fixer-wire] MVP scaffold target: {spec.destination}")
    print("[fixer-wire] stack: Serverpod backend + Flutter client + Codex app-server AI runtime")

    if spec.dry_run:
        _print_dry_run(spec)
        return

    _ensure_serverpod_available()
    spec.target_root.mkdir(parents=True, exist_ok=True)
    _run_serverpod_create(spec, command_runner=command_runner)
    _write_scaffold_files(spec)
    print("[fixer-wire] scaffold complete")


def _ensure_serverpod_available() -> None:
    if shutil.which(SERVERPOD_COMMAND):
        return
    raise RuntimeError(
        "Missing required `serverpod` CLI. Install Serverpod first, then rerun "
        "`fixer --scaffold-mvp <project_slug>`."
    )


def _run_serverpod_create(
    spec: MVPScaffoldSpec,
    *,
    command_runner: Callable[[list[str], Path], None] | None = None,
) -> None:
    runner = command_runner or _default_command_runner
    command = [SERVERPOD_COMMAND, "create", spec.project_slug]
    print(f"[fixer-wire] running: {' '.join(command)}")
    try:
        runner(command, spec.target_root)
    except (OSError, subprocess.CalledProcessError) as exc:
        raise RuntimeError(f"Failed to run `{' '.join(command)}`: {exc}") from exc
    if not spec.destination.is_dir():
        raise RuntimeError(
            f"`serverpod create` finished without creating the expected directory: {spec.destination}"
        )


def _default_command_runner(command: list[str], cwd: Path) -> None:
    subprocess.run(command, cwd=str(cwd), check=True)


def _print_dry_run(spec: MVPScaffoldSpec) -> None:
    print("[fixer-wire] dry-run only; no files were written")
    for line in planned_layout_lines(spec):
        print(line)


def planned_layout_lines(spec: MVPScaffoldSpec) -> list[str]:
    return [
        "[fixer-wire] planned layout:",
        f"  - {spec.destination.name}/",
        f"  - {spec.destination.name}/{spec.server_dir_name}/",
        f"  - {spec.destination.name}/{spec.client_dir_name}/",
        f"  - {spec.destination.name}/{spec.flutter_dir_name}/",
        f"  - {spec.destination.name}/llm_pipeline/",
        f"  - {spec.destination.name}/WORKFLOW.md",
        f"  - {spec.destination.name}/Makefile",
        f"  - {spec.destination.name}/README.md",
    ]


def _write_scaffold_files(spec: MVPScaffoldSpec) -> None:
    files = _render_scaffold_files(spec)
    for relative_path, content in files.items():
        path = spec.destination / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        if path.suffix == ".sh":
            path.chmod(0o755)


def _render_scaffold_files(spec: MVPScaffoldSpec) -> dict[str, str]:
    metadata = {
        "project_slug": spec.project_slug,
        "display_name": spec.display_name,
        "serverpod_root": spec.project_slug,
        "serverpod_server_dir": spec.server_dir_name,
        "serverpod_client_dir": spec.client_dir_name,
        "flutter_dir": spec.flutter_dir_name,
        "ai_runtime_dir": "llm_pipeline",
        "ai_service_name": spec.ai_service_name,
        "codex_model": DEFAULT_CODEX_MODEL,
        "codex_reasoning_effort": DEFAULT_CODEX_EFFORT,
    }
    return {
        ".gitignore": _render_gitignore(spec),
        ".env.example": _render_root_env_example(spec),
        "Makefile": _render_makefile(spec),
        "README.md": _render_root_readme(spec),
        "WORKFLOW.md": _render_workflow_md(spec),
        "mvp_scaffold.json": json.dumps(metadata, indent=2) + "\n",
        "llm_pipeline/README.md": _render_ai_readme(spec),
        "llm_pipeline/config.example.yaml": _render_ai_config(spec),
        "llm_pipeline/codex_model_layer.yaml": _render_codex_model_layer(),
        "llm_pipeline/go.mod": _render_ai_go_mod(spec),
        "llm_pipeline/cmd/{name}/main.go".format(name=spec.ai_service_name): _render_ai_main_go(spec),
        "llm_pipeline/llm.env.example": _render_ai_env_example(),
        "llm_pipeline/prompts/chat_system.md": _render_ai_prompt(spec),
        "llm_pipeline/scripts/run_codex_app_server.sh": _render_ai_app_server_script(spec),
    }


def _render_gitignore(spec: MVPScaffoldSpec) -> str:
    _ = spec
    return textwrap.dedent(
        """
        .DS_Store
        .env
        llm_pipeline/llm.env
        .dart_tool/
        build/
        .flutter-plugins
        .flutter-plugins-dependencies
        .packages
        pubspec.lock
        .idea/
        .vscode/
        llm_pipeline/bin/
        llm_pipeline/tmp/
        """
    ).lstrip()


def _render_root_env_example(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        # Shared local defaults for {spec.display_name}
        LLM_PIPELINE_URL=http://127.0.0.1:8095
        SERVERPOD_PUBLIC_API_URL=http://127.0.0.1:8080
        CODEX_MODEL={DEFAULT_CODEX_MODEL}
        CODEX_REASONING_EFFORT={DEFAULT_CODEX_EFFORT}
        """
    ).lstrip()


def _render_makefile(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        PROJECT_SLUG := {spec.project_slug}
        SERVER_DIR := $(PROJECT_SLUG)/{spec.server_dir_name}
        FLUTTER_DIR := $(PROJECT_SLUG)/{spec.flutter_dir_name}
        AI_DIR := llm_pipeline
        AI_SERVICE := {spec.ai_service_name}

        .PHONY: doctor serverpod-generate serverpod-up ai-run flutter-run

        doctor:
        \t@command -v serverpod >/dev/null || (echo "serverpod CLI missing" && exit 1)
        \t@command -v go >/dev/null || (echo "go missing" && exit 1)
        \t@command -v flutter >/dev/null || echo "flutter missing: scaffold is ready, but Flutter cannot run yet"
        \t@command -v codex >/dev/null || echo "codex missing: AI runtime scaffold is ready, but app-server cannot run yet"

        serverpod-generate:
        \tcd "$(SERVER_DIR)" && serverpod generate

        serverpod-up:
        \tcd "$(SERVER_DIR)" && docker compose up -d postgres redis serverpod

        ai-run:
        \tcd "$(AI_DIR)" && go run ./cmd/$(AI_SERVICE)

        flutter-run:
        \tcd "$(FLUTTER_DIR)" && flutter pub get && flutter run
        """
    ).lstrip()


def _render_root_readme(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        # {spec.display_name}

        Deterministic MVP scaffold generated through the Fixer wire.

        ## Stack

        - `Serverpod` backend scaffolded via `serverpod create`
        - `Flutter` client scaffolded by Serverpod
        - `llm_pipeline/` for Codex app-server driven AI flows
        - repo-owned `WORKFLOW.md` inspired by Symphony's in-repo workflow contract

        ## Layout

        - `{spec.project_slug}/{spec.server_dir_name}`: Serverpod server
        - `{spec.project_slug}/{spec.client_dir_name}`: generated client package
        - `{spec.project_slug}/{spec.flutter_dir_name}`: Flutter application
        - `llm_pipeline/`: AI runtime scaffold and Codex runtime config
        - `WORKFLOW.md`: repo-local automation and agent policy contract

        ## Why this shape

        The scaffold mirrors the successful split used in the Philologists reference:
        product app code stays in Serverpod + Flutter, while AI-facing execution stays in a
        dedicated service boundary. It also adopts Symphony's repo-owned workflow pattern so
        Codex app-server behavior lives in versioned project files instead of ad-hoc operator prompts.

        ## First-run sequence

        1. `make doctor`
        2. `make serverpod-generate`
        3. `make serverpod-up`
        4. `cp llm_pipeline/llm.env.example llm_pipeline/llm.env`
        5. `cp .env.example .env`
        6. `make ai-run`
        7. `make flutter-run`

        ## Operator flow

        From any parent directory, scaffold a fresh MVP with:

        ```bash
        fixer --scaffold-mvp {spec.project_slug}
        ```

        Or target an explicit parent directory:

        ```bash
        fixer --scaffold-mvp {spec.project_slug} --scaffold-target-dir ~/code/mvps
        ```

        The wire normalizes the project slug, runs `serverpod create`, and overlays the AI runtime files.
        """
    ).lstrip()


def _render_workflow_md(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        ---
        workspace:
          root: ./.codex/workspaces
        hooks:
          after_create: |
            echo "Workspace ready for {spec.project_slug}"
        agent:
          max_concurrent_agents: 2
          max_turns: 12
        codex:
          command: codex --config shell_environment_policy.inherit=all --model {DEFAULT_CODEX_MODEL} app-server
          approval_policy: never
          thread_sandbox: workspace-write
          turn_sandbox_policy:
            type: workspaceWrite
        ---

        You are working inside the `{spec.display_name}` MVP repository.

        Guardrails:
        - Preserve the split between Serverpod application code and `llm_pipeline` AI runtime code.
        - Keep Codex-facing runtime behavior versioned in repo files, not hidden in shell history.
        - Prefer deterministic setup scripts and checked-in config over undocumented local state.
        - Treat `{spec.server_dir_name}` and `{spec.flutter_dir_name}` as product surfaces and
          `llm_pipeline` as the AI execution surface.
        """
    ).lstrip()


def _render_ai_readme(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        # llm_pipeline

        This service owns AI-facing runtime behavior for `{spec.display_name}`.

        ## Intent

        - isolate Codex app-server execution from the product app
        - keep model/runtime configuration in repo
        - give future Netrunners a stable place to add prompt contracts, status streaming, and tool policy

        ## Included scaffold

        - `cmd/{spec.ai_service_name}/main.go`: minimal HTTP runtime with `/healthz` and `/v1/chat`
        - `codex_model_layer.yaml`: default Codex model profile contract
        - `llm.env.example`: local runtime env template
        - `prompts/chat_system.md`: system prompt seed
        - `scripts/run_codex_app_server.sh`: direct local Codex app-server launcher

        ## Next implementation slice

        Replace the placeholder `/v1/chat` response path with a real app-server session manager.
        The intended ownership is:

        1. accept a product request from Serverpod
        2. launch or reuse a Codex app-server session
        3. stream status and tool events
        4. return a normalized product response contract to the app
        """
    ).lstrip()


def _render_ai_config(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        server:
          listen_address: "127.0.0.1:8095"

        workflow:
          agent_name: "{spec.ai_service_name}"
          description: "Codex app-server runtime for {spec.display_name}."
          global_instruction: ""

        codex:
          command: "./scripts/run_codex_app_server.sh"
          model: "${{CODEX_MODEL:-{DEFAULT_CODEX_MODEL}}}"
          reasoning_effort: "${{CODEX_REASONING_EFFORT:-{DEFAULT_CODEX_EFFORT}}}"
        """
    ).lstrip()


def _render_codex_model_layer() -> str:
    return textwrap.dedent(
        f"""
        version: 1
        default_provider: codex_native
        default_effort: {DEFAULT_CODEX_EFFORT}

        providers:
          codex_native:
            codex_model_provider: openai
            default_model: {DEFAULT_CODEX_MODEL}
            allowed_efforts: [low, medium, high, extra_high]
            models:
              - {DEFAULT_CODEX_MODEL}
              - gpt-5.3-codex
              - gpt-5.3-codex-spark
        """
    ).lstrip()


def _render_ai_go_mod(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        module {spec.project_slug}_llm_pipeline

        go 1.24
        """
    ).lstrip()


def _render_ai_main_go(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        package main

        import (
        \t"encoding/json"
        \t"log"
        \t"net/http"
        \t"os"
        )

        type chatRequest struct {{
        \tMessage   string `json:"message"`
        \tSessionID string `json:"session_id,omitempty"`
        }}

        type chatResponse struct {{
        \tStatus  string `json:"status"`
        \tMode    string `json:"mode"`
        \tMessage string `json:"message"`
        }}

        func main() {{
        \tmux := http.NewServeMux()
        \tmux.HandleFunc("/healthz", healthzHandler)
        \tmux.HandleFunc("/v1/chat", chatHandler)

        \taddr := getenv("LLM_PIPELINE_LISTEN_ADDR", "127.0.0.1:8095")
        \tlog.Printf("{spec.ai_service_name} listening on %s", addr)
        \tif err := http.ListenAndServe(addr, mux); err != nil {{
        \t\tlog.Fatal(err)
        \t}}
        }}

        func healthzHandler(w http.ResponseWriter, _ *http.Request) {{
        \twriteJSON(w, http.StatusOK, map[string]string{{
        \t\t"status": "ok",
        \t\t"service": "{spec.ai_service_name}",
        \t}})
        }}

        func chatHandler(w http.ResponseWriter, r *http.Request) {{
        \tif r.Method != http.MethodPost {{
        \t\tw.WriteHeader(http.StatusMethodNotAllowed)
        \t\treturn
        \t}}

        \tvar req chatRequest
        \tif err := json.NewDecoder(r.Body).Decode(&req); err != nil {{
        \t\twriteJSON(w, http.StatusBadRequest, map[string]string{{"error": "invalid_json"}})
        \t\treturn
        \t}}

        \tresp := chatResponse{{
        \t\tStatus: "stub",
        \t\tMode: "codex_app_server_pending",
        \t\tMessage: "Replace this stub with a Codex app-server backed product flow. Incoming message: " + req.Message,
        \t}}
        \twriteJSON(w, http.StatusOK, resp)
        }}

        func writeJSON(w http.ResponseWriter, status int, payload any) {{
        \tw.Header().Set("Content-Type", "application/json")
        \tw.WriteHeader(status)
        \t_ = json.NewEncoder(w).Encode(payload)
        }}

        func getenv(name string, fallback string) string {{
        \tif value := os.Getenv(name); value != "" {{
        \t\treturn value
        \t}}
        \treturn fallback
        }}
        """
    ).lstrip()


def _render_ai_env_example() -> str:
    return textwrap.dedent(
        f"""
        # Local Codex runtime config for llm_pipeline
        CODEX_MODEL={DEFAULT_CODEX_MODEL}
        CODEX_REASONING_EFFORT={DEFAULT_CODEX_EFFORT}
        OPENAI_API_KEY=
        """
    ).lstrip()


def _render_ai_prompt(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        You are the AI runtime for `{spec.display_name}`.

        Priorities:
        - keep answers grounded in product state supplied by the caller
        - prefer deterministic tool use and structured outputs
        - emit concise progress updates when long-running work is introduced
        - preserve a clean boundary between user-facing app logic and AI execution internals
        """
    ).lstrip()


def _render_ai_app_server_script(spec: MVPScaffoldSpec) -> str:
    return textwrap.dedent(
        f"""
        #!/usr/bin/env bash
        set -euo pipefail

        MODEL="${{CODEX_MODEL:-{DEFAULT_CODEX_MODEL}}}"
        EFFORT="${{CODEX_REASONING_EFFORT:-{DEFAULT_CODEX_EFFORT}}}"

        exec codex \\
          --config shell_environment_policy.inherit=all \\
          --config model_reasoning_effort="${{EFFORT}}" \\
          --model "${{MODEL}}" \\
          app-server
        """
    ).lstrip()
