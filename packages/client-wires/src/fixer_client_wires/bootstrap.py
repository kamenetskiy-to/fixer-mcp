from __future__ import annotations

import argparse
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping

PUBLIC_RUNTIME_ROOT_ENV = "FIXER_CLIENT_WIRES_RUNTIME_ROOT"
PUBLIC_CONFIG_PATH_ENV = "FIXER_CLIENT_WIRES_CONFIG_PATH"
PUBLIC_CONFIG_ROOT_ENV = "FIXER_CLIENT_WIRES_CONFIG_ROOT"
LEGACY_RUNTIME_ROOT_ENV = "MCP_SERVERS_ROOT"
PUBLIC_RUNTIME_PACKAGE = "fixer_runtime"
LEGACY_RUNTIME_PACKAGE = "codex_pro_app"
PUBLIC_CONFIG_FILENAME = "mcp-config.json"
PUBLIC_EXAMPLE_CONFIG_FILENAME = "mcp-config.example.json"
LEGACY_CONFIG_FILENAME = "mcp_config.json"


@dataclass(frozen=True)
class RuntimeResolution:
    root: Path
    package_name: str
    source: str


@dataclass(frozen=True)
class ConfigResolution:
    path: Path
    source: str
    kind: str


def resolve_package_root(from_file: Path | None = None) -> Path:
    source_file = from_file or Path(__file__).resolve()
    return source_file.parents[2]


def resolve_repo_root(from_file: Path | None = None) -> Path:
    source_file = from_file or Path(__file__).resolve()
    return source_file.parents[4]


def _has_runtime_package(root: Path, package_name: str) -> bool:
    return (root / package_name / "__init__.py").is_file()


def resolve_runtime_root(
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> RuntimeResolution:
    env = environ or os.environ
    resolved_repo_root = (repo_root or resolve_repo_root()).resolve()
    resolved_package_root = (package_root or resolve_package_root()).resolve()

    explicit_public_root = env.get(PUBLIC_RUNTIME_ROOT_ENV)
    if explicit_public_root:
        candidate = Path(explicit_public_root).expanduser().resolve()
        if _has_runtime_package(candidate, PUBLIC_RUNTIME_PACKAGE):
            return RuntimeResolution(
                root=candidate,
                package_name=PUBLIC_RUNTIME_PACKAGE,
                source=f"env:{PUBLIC_RUNTIME_ROOT_ENV}",
            )
        raise RuntimeError(
            f"{PUBLIC_RUNTIME_ROOT_ENV}={candidate} does not contain "
            f"{PUBLIC_RUNTIME_PACKAGE}/__init__.py"
        )

    packaged_root = (resolved_package_root / "runtime").resolve()
    if _has_runtime_package(packaged_root, PUBLIC_RUNTIME_PACKAGE):
        return RuntimeResolution(
            root=packaged_root,
            package_name=PUBLIC_RUNTIME_PACKAGE,
            source="package-local runtime",
        )

    legacy_env_root = env.get(LEGACY_RUNTIME_ROOT_ENV)
    if legacy_env_root:
        candidate = Path(legacy_env_root).expanduser().resolve()
        if _has_runtime_package(candidate, LEGACY_RUNTIME_PACKAGE):
            return RuntimeResolution(
                root=candidate,
                package_name=LEGACY_RUNTIME_PACKAGE,
                source=f"compat env:{LEGACY_RUNTIME_ROOT_ENV}",
            )
        raise RuntimeError(
            f"{LEGACY_RUNTIME_ROOT_ENV}={candidate} does not contain "
            f"{LEGACY_RUNTIME_PACKAGE}/__init__.py"
        )

    sibling_root = (resolved_repo_root.parent / "mcp_servers").resolve()
    if _has_runtime_package(sibling_root, LEGACY_RUNTIME_PACKAGE):
        return RuntimeResolution(
            root=sibling_root,
            package_name=LEGACY_RUNTIME_PACKAGE,
            source="compat sibling checkout",
        )

    raise RuntimeError(
        "Could not resolve a client-wires runtime. Checked package-local staged runtime, "
        f"{PUBLIC_RUNTIME_ROOT_ENV}, {LEGACY_RUNTIME_ROOT_ENV}, and sibling ../mcp_servers."
    )


def bootstrap_runtime_import_path(
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> RuntimeResolution:
    resolution = resolve_runtime_root(
        repo_root=repo_root,
        package_root=package_root,
        environ=environ,
    )
    runtime_root = str(resolution.root)
    if runtime_root not in sys.path:
        sys.path.insert(0, runtime_root)
    return resolution


def resolve_package_config_root(package_root: Path | None = None) -> Path:
    return (package_root or resolve_package_root()).resolve() / "config"


def resolve_package_examples_root(package_root: Path | None = None) -> Path:
    return (package_root or resolve_package_root()).resolve() / "examples"


def resolve_repo_examples_root(repo_root: Path | None = None) -> Path:
    return (repo_root or resolve_repo_root()).resolve() / "examples"


def resolve_example_config_path(
    repo_root: Path | None = None,
    package_root: Path | None = None,
) -> Path:
    package_example = resolve_package_examples_root(package_root) / PUBLIC_EXAMPLE_CONFIG_FILENAME
    if package_example.is_file():
        return package_example
    return resolve_repo_examples_root(repo_root) / PUBLIC_EXAMPLE_CONFIG_FILENAME


def resolve_config_path(
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> ConfigResolution:
    env = environ or os.environ
    resolved_repo_root = (repo_root or resolve_repo_root()).resolve()
    resolved_package_root = (package_root or resolve_package_root()).resolve()

    explicit_config_path = env.get(PUBLIC_CONFIG_PATH_ENV)
    if explicit_config_path:
        candidate = Path(explicit_config_path).expanduser().resolve()
        if candidate.is_file():
            return ConfigResolution(
                path=candidate,
                source=f"env:{PUBLIC_CONFIG_PATH_ENV}",
                kind="active config",
            )
        raise RuntimeError(f"{PUBLIC_CONFIG_PATH_ENV}={candidate} does not exist")

    explicit_config_root = env.get(PUBLIC_CONFIG_ROOT_ENV)
    if explicit_config_root:
        candidate = Path(explicit_config_root).expanduser().resolve() / PUBLIC_CONFIG_FILENAME
        if candidate.is_file():
            return ConfigResolution(
                path=candidate,
                source=f"env:{PUBLIC_CONFIG_ROOT_ENV}",
                kind="active config",
            )
        raise RuntimeError(
            f"{PUBLIC_CONFIG_ROOT_ENV}={candidate.parent} does not contain {PUBLIC_CONFIG_FILENAME}"
        )

    package_config = resolved_package_root / "config" / PUBLIC_CONFIG_FILENAME
    if package_config.is_file():
        return ConfigResolution(
            path=package_config,
            source="package-local config",
            kind="active config",
        )

    package_example = resolved_package_root / "examples" / PUBLIC_EXAMPLE_CONFIG_FILENAME
    if package_example.is_file():
        return ConfigResolution(
            path=package_example,
            source="package-local example",
            kind="example config",
        )

    repo_example = resolved_repo_root / "examples" / PUBLIC_EXAMPLE_CONFIG_FILENAME
    if repo_example.is_file():
        return ConfigResolution(
            path=repo_example,
            source="repo example fallback",
            kind="example config",
        )

    legacy_repo_config = resolved_repo_root / LEGACY_CONFIG_FILENAME
    if legacy_repo_config.is_file():
        return ConfigResolution(
            path=legacy_repo_config,
            source=f"compat repo-root:{LEGACY_CONFIG_FILENAME}",
            kind="legacy config",
        )

    raise RuntimeError(
        "Could not resolve a client-wires config. Checked "
        f"{PUBLIC_CONFIG_PATH_ENV}, {PUBLIC_CONFIG_ROOT_ENV}, package-local config, "
        "package-local example, repo examples, and legacy repo-root mcp_config.json."
    )


def wire_info_lines(
    resolution: RuntimeResolution,
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> list[str]:
    resolved_repo_root = (repo_root or resolve_repo_root()).resolve()
    resolved_package_root = (package_root or resolve_package_root()).resolve()
    config_resolution = resolve_config_path(
        repo_root=resolved_repo_root,
        package_root=resolved_package_root,
        environ=environ,
    )
    return [
        "Fixer client-wires bootstrap resolved:",
        f"- repo root: {resolved_repo_root}",
        f"- package root: {resolved_package_root}",
        f"- runtime root: {resolution.root}",
        f"- runtime package: {resolution.package_name}",
        f"- runtime source: {resolution.source}",
        f"- config path: {config_resolution.path}",
        f"- config source: {config_resolution.source}",
        f"- config kind: {config_resolution.kind}",
        f"- example config: {resolve_example_config_path(resolved_repo_root, resolved_package_root)}",
    ]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Inspect or bootstrap the client-wires runtime.")
    parser.add_argument(
        "--wire-info",
        action="store_true",
        help="Print the resolved runtime contract for the current checkout.",
    )
    args = parser.parse_args(argv)

    resolution = bootstrap_runtime_import_path()
    if args.wire_info:
        print("\n".join(wire_info_lines(resolution)))
        return 0

    print(str(resolution.root))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
