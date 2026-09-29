from __future__ import annotations

import fcntl
import hashlib
import json
import os
import shutil
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping, Sequence

DEFAULT_BACKEND = "codex"
DEFAULT_MCP_BACKEND = "codex"
FIXER_ROLE_SKILL_NAMES = (
    "init-fixer",
    "init-unattached-fixer",
    "init-overseer",
    "maintain-project-docs",
    "fixer-backend-providers",
    "fixer-repo-cleanup",
    "hands-netrunner",
    "run-netrunner-wave",
    "netrunner-backend-models",
    "review-netrunner-session",
    "complete-netrunner-session",
    "inspect-netrunner-transcript",
    "bridge-overseer-fixer",
    "save-fixer-handoff",
    "refresh-project-overview",
    "run-fixer-image-job",
    "figma-frontend-works",
    "shadcn-ui-flutter",
    "design-system-works",
    "share-project",
    "export-project-doc-bundle",
    "triage-fixer-feedback",
)
FIXER_RETIRED_SKILL_NAMES = (
    "init-netrunner",
    "start-fixer",
    "start-netrunner",
    "start-overseer",
    "manual-resolution",
    "manual-acceptance",
    "session-completion",
    "fixer-handoff",
    "autonomous-resolution",
    "autonomous-resolution-one-task",
    "run-one-netrunner-task",
    "bootstrap-netrunners",
    "check-netrunner-statuses",
    "check-netrunner-statuses-autonomous",
    "start-netrunner-autonomous",
)
# One checked-in catalog; provider-specific directories are generated views.
CANONICAL_SKILLS_RELATIVE_ROOT = ".agents/skills"


# =============================================================================
# Persistent Model Catalog Overlay (~/.config/fixer/model-catalog-overlay.json)
# =============================================================================
# Schema Documentation:
# ---------------------
# The persistent user overlay survives release updates and symlink switches.
# Location: ~/.config/fixer/model-catalog-overlay.json (overridable via $FIXER_MODEL_CATALOG_OVERLAY).
#
# Root: JSON object. Supported top-level layouts:
# 1. Direct backend map:
#    {
#      "pi": { ... },
#      "commandcode": { ... }
#    }
# 2. Wrapped under "backends" (matching backend-catalog.json layout):
#    {
#      "backends": {
#        "pi": { ... }
#      }
#    }
#
# Inside each backend entry:
# - "models": dict mapping model_id -> dict/bool, or list of model IDs/dicts:
#     "models": {
#       "custom/new-model": { "retired": false },
#       "packaged/old-model": { "retired": true }
#     }
#     OR
#     "models": [
#       "custom/new-model",
#       { "id": "packaged/old-model", "retired": true }
#     ]
# - "model_options": list of model IDs (strings or objects with id/retired):
#     "model_options": [
#       "custom/new-model",
#       { "id": "packaged/old-model", "retired": true }
#     ]
# - "retired": list of model ID strings to suppress:
#     "retired": ["packaged/old-model"]
#
# Semantics:
# - Overlay entries win on conflict.
# - Explicit "retired": true suppresses packaged entries; updates will NOT resurrect them.
# - Malformed overlay raises ModelCatalogOverlayError with an actionable message (never silently ignored).
# - Missing overlay file returns packaged catalog unchanged.
# =============================================================================


class ModelCatalogOverlayError(RuntimeError):
    """Raised when the model catalog overlay is malformed, has invalid JSON, or invalid schema."""


OVERLAY_ENV_VAR = "FIXER_MODEL_CATALOG_OVERLAY"
DEFAULT_OVERLAY_PATH = Path.home() / ".config" / "fixer" / "model-catalog-overlay.json"


def get_model_catalog_overlay_path() -> Path:
    override = os.environ.get(OVERLAY_ENV_VAR, "").strip()
    if override:
        return Path(override).expanduser()
    return DEFAULT_OVERLAY_PATH.expanduser()


def _validate_model_list(path: Path, backend_name: str, field_name: str, items: list[Any]) -> None:
    for idx, item in enumerate(items):
        if isinstance(item, str):
            if not item.strip():
                raise ModelCatalogOverlayError(
                    f"Malformed model catalog overlay at {path}: {field_name} item at index {idx} in backend {backend_name!r} must be a non-empty string"
                )
        elif isinstance(item, dict):
            model_id = item.get("id") or item.get("name")
            if not model_id or not isinstance(model_id, str):
                raise ModelCatalogOverlayError(
                    f"Malformed model catalog overlay at {path}: {field_name} object item at index {idx} in backend {backend_name!r} missing string 'id' or 'name'"
                )
            if "retired" in item and not isinstance(item["retired"], bool):
                raise ModelCatalogOverlayError(
                    f"Malformed model catalog overlay at {path}: 'retired' marker for {model_id!r} in backend {backend_name!r} must be a boolean"
                )
        else:
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {path}: {field_name} item at index {idx} in backend {backend_name!r} must be a string or object, got {type(item).__name__}"
            )


def _validate_backend_overlay_entry(path: Path, backend_name: str, entry: dict[str, Any]) -> None:
    if "models" in entry:
        models = entry["models"]
        if isinstance(models, dict):
            for model_id, model_spec in models.items():
                if not isinstance(model_id, str) or not model_id.strip():
                    raise ModelCatalogOverlayError(
                        f"Malformed model catalog overlay at {path}: model ID in backend {backend_name!r} must be a non-empty string"
                    )
                if isinstance(model_spec, dict):
                    if "retired" in model_spec and not isinstance(model_spec["retired"], bool):
                        raise ModelCatalogOverlayError(
                            f"Malformed model catalog overlay at {path}: 'retired' marker for model {model_id!r} in backend {backend_name!r} must be a boolean"
                        )
                elif isinstance(model_spec, bool):
                    pass
                else:
                    raise ModelCatalogOverlayError(
                        f"Malformed model catalog overlay at {path}: model spec for {model_id!r} in backend {backend_name!r} must be an object or boolean"
                    )
        elif isinstance(models, list):
            _validate_model_list(path, backend_name, "models", models)
        else:
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {path}: 'models' in backend {backend_name!r} must be an object or list, got {type(models).__name__}"
            )

    if "model_options" in entry:
        model_options = entry["model_options"]
        if not isinstance(model_options, list):
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {path}: 'model_options' in backend {backend_name!r} must be a list, got {type(model_options).__name__}"
            )
        _validate_model_list(path, backend_name, "model_options", model_options)

    if "retired" in entry:
        retired = entry["retired"]
        if not isinstance(retired, list):
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {path}: 'retired' in backend {backend_name!r} must be a list, got {type(retired).__name__}"
            )
        for item in retired:
            if not isinstance(item, str) or not item.strip():
                raise ModelCatalogOverlayError(
                    f"Malformed model catalog overlay at {path}: 'retired' list items in backend {backend_name!r} must be non-empty strings"
                )


def load_model_catalog_overlay(path: Path | str | None = None) -> dict[str, Any]:
    """Load and validate the model catalog overlay.

    Returns an empty dict if the file does not exist.
    Raises ModelCatalogOverlayError with an actionable message if the file exists but is malformed.
    """
    target_path = Path(path).expanduser() if path is not None else get_model_catalog_overlay_path()
    if not target_path.is_file():
        return {}

    try:
        content = target_path.read_text(encoding="utf-8")
    except Exception as exc:
        raise ModelCatalogOverlayError(
            f"Failed to read model catalog overlay at {target_path}: {exc}"
        ) from exc

    try:
        data = json.loads(content)
    except json.JSONDecodeError as exc:
        raise ModelCatalogOverlayError(
            f"Malformed model catalog overlay at {target_path}: invalid JSON: {exc}"
        ) from exc

    if not isinstance(data, dict):
        raise ModelCatalogOverlayError(
            f"Malformed model catalog overlay at {target_path}: root must be a JSON object, got {type(data).__name__}"
        )

    backends_map: dict[str, Any]
    if "backends" in data:
        if not isinstance(data["backends"], dict):
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {target_path}: 'backends' must be an object, got {type(data['backends']).__name__}"
            )
        backends_map = data["backends"]
    else:
        backends_map = data

    for backend_key, backend_val in backends_map.items():
        if backend_key == "backends":
            continue
        if not isinstance(backend_val, dict):
            raise ModelCatalogOverlayError(
                f"Malformed model catalog overlay at {target_path}: entry for backend {backend_key!r} must be an object, got {type(backend_val).__name__}"
            )
        _validate_backend_overlay_entry(target_path, str(backend_key), backend_val)

    return data


def apply_model_catalog_overlay(
    backend_name: str,
    model_options: Sequence[str],
    overlay_path: Path | str | None = None,
) -> tuple[str, ...]:
    """Merge overlay additions and retired suppressions over packaged model_options for a backend.

    - Overlay entries win on conflict.
    - Explicit "retired": true suppresses packaged entries and updates will not resurrect them.
    - Malformed overlay raises ModelCatalogOverlayError with actionable error details.
    - If no overlay exists, returns tuple(model_options).
    """
    overlay = load_model_catalog_overlay(overlay_path)
    if not overlay:
        return tuple(model_options)

    normalized = normalize_backend_name(backend_name)
    backends_map = overlay.get("backends") if isinstance(overlay.get("backends"), dict) else overlay
    backend_entry = backends_map.get(normalized) or backends_map.get(backend_name)
    if not isinstance(backend_entry, dict):
        return tuple(model_options)

    retired_set: set[str] = set()
    added_models: list[str] = []

    # 1. Process "retired" list
    if "retired" in backend_entry and isinstance(backend_entry["retired"], list):
        for item in backend_entry["retired"]:
            if isinstance(item, str) and item.strip():
                retired_set.add(item.strip())

    # 2. Process "models"
    if "models" in backend_entry:
        models = backend_entry["models"]
        if isinstance(models, dict):
            for model_id, spec in models.items():
                model_id_clean = str(model_id).strip()
                if isinstance(spec, dict):
                    if spec.get("retired") is True:
                        retired_set.add(model_id_clean)
                    else:
                        if model_id_clean not in added_models:
                            added_models.append(model_id_clean)
                elif spec is False:
                    retired_set.add(model_id_clean)
                elif spec is True:
                    if model_id_clean not in added_models:
                        added_models.append(model_id_clean)
        elif isinstance(models, list):
            for item in models:
                if isinstance(item, str):
                    clean = item.strip()
                    if clean not in added_models:
                        added_models.append(clean)
                elif isinstance(item, dict):
                    m_id = str(item.get("id") or item.get("name") or "").strip()
                    if item.get("retired") is True:
                        retired_set.add(m_id)
                    elif m_id and m_id not in added_models:
                        added_models.append(m_id)

    # 3. Process "model_options"
    if "model_options" in backend_entry and isinstance(backend_entry["model_options"], list):
        for item in backend_entry["model_options"]:
            if isinstance(item, str):
                clean = item.strip()
                if clean not in added_models:
                    added_models.append(clean)
            elif isinstance(item, dict):
                m_id = str(item.get("id") or item.get("name") or "").strip()
                if item.get("retired") is True:
                    retired_set.add(m_id)
                elif m_id and m_id not in added_models:
                    added_models.append(m_id)

    # Merge: retain packaged options that are not retired
    result: list[str] = [m for m in model_options if m not in retired_set]

    # Add overlay models not in retired_set and not already present
    for m in added_models:
        if m not in retired_set and m not in result:
            result.append(m)

    return tuple(result)


def overlay_manifest(manifest: Any, overlay_path: Path | str | None = None) -> Any:
    """Merge overlay additions and retirements into a ProviderManifest instance or dict."""
    if hasattr(manifest, "provider") and hasattr(manifest, "models"):
        provider_name = getattr(manifest, "provider")
        models_obj = getattr(manifest, "models")
        if hasattr(models_obj, "options"):
            merged = apply_model_catalog_overlay(provider_name, models_obj.options, overlay_path)
            if hasattr(models_obj, "__dict__"):
                try:
                    object.__setattr__(models_obj, "options", list(merged))
                except Exception:
                    pass
    elif isinstance(manifest, dict):
        provider_name = str(manifest.get("provider", ""))
        models_dict = manifest.get("models")
        if isinstance(models_dict, dict) and "options" in models_dict:
            merged = apply_model_catalog_overlay(provider_name, models_dict["options"], overlay_path)
            models_dict["options"] = list(merged)
    return manifest


@dataclass(frozen=True)
class BackendDescriptor:
    name: str
    label: str
    description: str
    default_model: str
    default_reasoning: str
    model_options: tuple[str, ...]
    reasoning_options: tuple[str, ...]
    fresh_launch_supported: bool = True
    resume_supported: bool = True
    available: bool = True

    def __post_init__(self) -> None:
        if self.name:
            merged = apply_model_catalog_overlay(self.name, self.model_options)
            object.__setattr__(self, "model_options", merged)


def normalize_backend_name(raw: str | None) -> str:
    normalized = (raw or "").strip().lower()
    if not normalized:
        return DEFAULT_BACKEND
    if normalized == "agy":
        return "antigravity"
    return normalized


def is_codex_backend(raw: str | None) -> bool:
    return normalize_backend_name(raw) == "codex"


def normalize_mcp_server_for_factory(source: Mapping[str, object]) -> dict[str, object]:
    if "url" in source:
        payload: dict[str, object] = {
            "type": "http",
            "url": source["url"],
            "disabled": bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = dict(headers)
        return payload

    payload = {
        "type": "stdio",
        "command": str(source.get("command", "")).strip(),
        "disabled": bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    return payload


def materialize_factory_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, ".factory/skills", skill_names)


def materialize_antigravity_workspace_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, CANONICAL_SKILLS_RELATIVE_ROOT, skill_names)


def materialize_codex_project_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, CANONICAL_SKILLS_RELATIVE_ROOT, skill_names)


def materialize_claude_workspace_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, ".claude/skills", skill_names)


def materialize_junie_workspace_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, ".junie/fixer-runtime/skills", skill_names)


def materialize_kimi_workspace_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    _materialize_provider_skills(cwd, ".kimi-code/skills", skill_names)


def _materialize_provider_skills(cwd: Path, relative_root: str, skill_names: Sequence[str]) -> None:
    lock_id = hashlib.sha256(str(cwd.resolve()).encode()).hexdigest()[:16]
    lock_path = Path("/tmp") / f"fixer-skill-materialize-{lock_id}.lock"
    with lock_path.open("a+") as lock_file:
        fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
        try:
            skill_root = cwd / relative_root
            skill_root.mkdir(parents=True, exist_ok=True)
            _prune_retired_fixer_skills(skill_root)
            for normalized_name, source_dir in _iter_available_skill_sources(cwd, skill_names):
                legacy_flat_file = skill_root / f"{normalized_name}.md"
                try:
                    legacy_flat_file.unlink()
                except FileNotFoundError:
                    pass
                destination = skill_root / normalized_name
                if _same_path(source_dir, destination):
                    continue
                shutil.rmtree(destination, ignore_errors=True)
                shutil.copytree(source_dir, destination)
        finally:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)


def _prune_retired_fixer_skills(skill_root: Path) -> None:
    for skill_name in FIXER_RETIRED_SKILL_NAMES:
        retired_dir = skill_root / skill_name
        if retired_dir.is_dir() and not retired_dir.is_symlink():
            shutil.rmtree(retired_dir, ignore_errors=True)


def _same_path(left: Path, right: Path) -> bool:
    try:
        return left.resolve() == right.resolve()
    except OSError:
        return False


def _iter_available_skill_sources(cwd: Path, skill_names: Sequence[str]) -> list[tuple[str, Path]]:
    repo_root = Path(__file__).resolve().parents[2]
    candidate_roots = [
        repo_root / CANONICAL_SKILLS_RELATIVE_ROOT,
    ]
    found: list[tuple[str, Path]] = []
    for skill_name in skill_names:
        normalized_name = skill_name.strip()
        if not normalized_name:
            continue
        source_dir: Path | None = None
        for root in candidate_roots:
            if root is None:
                continue
            candidate = root / normalized_name
            if (candidate / "SKILL.md").is_file():
                source_dir = candidate
                break
        if source_dir is None:
            continue
        found.append((normalized_name, source_dir))
    return found


def normalize_mcp_server_for_antigravity(source: Mapping[str, object]) -> dict[str, object]:
    if "serverUrl" in source or "url" in source:
        payload: dict[str, object] = {
            "serverUrl": source.get("serverUrl", source.get("url", "")),
            "disabled": bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = dict(headers)
        return payload

    payload = {
        "command": str(source.get("command", "")).strip(),
        "disabled": bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    return payload


def normalize_mcp_server_for_junie(source: Mapping[str, object]) -> dict[str, object]:
    if "serverUrl" in source or "url" in source:
        payload: dict[str, object] = {
            "url": source.get("url", source.get("serverUrl", "")),
            "disabled": bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = dict(headers)
        return payload

    payload = {
        "command": str(source.get("command", "")).strip(),
        "disabled": bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    return payload


class BackendAdapter(ABC):
    descriptor: BackendDescriptor
    command: str
    supports_resume: bool

    @property
    def name(self) -> str:
        return self.descriptor.name

    @property
    def default_model(self) -> str:
        return self.descriptor.default_model

    @property
    def default_reasoning(self) -> str:
        return self.descriptor.default_reasoning

    @property
    def model_options(self) -> tuple[str, ...]:
        return apply_model_catalog_overlay(self.name, self.descriptor.model_options)

    @property
    def reasoning_options(self) -> tuple[str, ...]:
        return self.descriptor.reasoning_options

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip() or self.default_model
        if candidate not in self.model_options:
            supported = ", ".join(self.model_options)
            raise RuntimeError(
                f"Unsupported model {candidate!r} for backend {self.name!r}. Supported models: {supported}"
            )
        return candidate

    def normalize_reasoning(self, reasoning: str | None) -> str:
        candidate = (reasoning or "").strip() or self.default_reasoning
        if candidate not in self.reasoning_options:
            supported = ", ".join(self.reasoning_options)
            raise RuntimeError(
                f"Unsupported reasoning {candidate!r} for backend {self.name!r}. Supported reasoning values: {supported}"
            )
        return candidate

    @abstractmethod
    def build_llm_args(self, selection: Any) -> list[str]:
        """Translate LLM selection into CLI args for interactive launches."""

    def build_execution_args(self, prefs: Any) -> list[str]:
        return []

    def build_interactive_execution_args(self, prefs: Any) -> list[str]:
        return self.build_execution_args(prefs)

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        trimmed = prompt.strip()
        if not trimmed:
            return []
        return [trimmed]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del env, selection
        return

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del cwd, selection, selected, available
        return

    @abstractmethod
    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        """Build the backend-specific resume command."""

    @abstractmethod
    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        """Build the backend-specific detached/headless command."""

    def build_headless_resume_command(
        self,
        *,
        external_session_id: str,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        """Build a non-interactive resume command for a durable worker generation.

        Interactive resume and headless resume are separate provider contracts.
        Providers must opt in explicitly instead of silently reusing a TUI
        command in a detached process.
        """
        del external_session_id, model, reasoning, selected, available, prompt
        raise RuntimeError(f"Backend {self.name!r} has not implemented headless resume.")
