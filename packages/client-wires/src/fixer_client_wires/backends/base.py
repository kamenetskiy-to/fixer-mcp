from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

DEFAULT_BACKEND = "codex"


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


class BackendAdapter:
    descriptor: BackendDescriptor

    @property
    def name(self) -> str:
        return self.descriptor.name

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip() or self.descriptor.default_model
        if candidate not in self.descriptor.model_options:
            supported = ", ".join(self.descriptor.model_options)
            raise RuntimeError(f"Unsupported model {candidate!r} for backend {self.name!r}. Supported models: {supported}")
        return candidate

    def normalize_reasoning(self, reasoning: str | None) -> str:
        candidate = (reasoning or "").strip() or self.descriptor.default_reasoning
        if candidate not in self.descriptor.reasoning_options:
            supported = ", ".join(self.descriptor.reasoning_options)
            raise RuntimeError(
                f"Unsupported reasoning {candidate!r} for backend {self.name!r}. Supported reasoning values: {supported}"
            )
        return candidate

    def build_fresh_command(
        self,
        *,
        model: str,
        reasoning: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        raise NotImplementedError

    def build_resume_command(
        self,
        *,
        external_session_id: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        raise NotImplementedError

    def runtime_side_effects(
        self,
        *,
        mode: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del mode
        del selected_mcp_servers
        return []


def normalize_backend_name(raw: str | None) -> str:
    normalized = (raw or "").strip().lower()
    return normalized or DEFAULT_BACKEND


def normalize_mcp_server_payload(source: Mapping[str, object]) -> dict[str, object]:
    if "url" in source:
        payload: dict[str, object] = {
            "type": "http",
            "url": source["url"],
            "disabled": bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = {str(key): str(value) for key, value in headers.items()}
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
