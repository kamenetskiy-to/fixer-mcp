from __future__ import annotations

import argparse
import importlib
import json
import os
import shutil
import subprocess
import tomllib
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Sequence


DEFAULT_VERSION = "0.1.0"
ASSEMBLY_ROOT_NAME = "github_repo"
ASSEMBLY_INCLUDE_PATHS = (
    Path("README.md"),
    Path("apps"),
    Path("docs"),
    Path("examples"),
    Path("packages"),
    Path("scripts"),
    Path("tests"),
)
ASSEMBLY_IGNORE_NAMES = ("__pycache__", "*.pyc", "dist")


@dataclass(frozen=True)
class ReleaseStep:
    name: str
    cwd: Path
    command: tuple[str, ...] = ()
    outputs: tuple[Path, ...] = ()
    kind: str = "command"
    build_backend: str | None = None


@dataclass(frozen=True)
class ReleasePlan:
    version: str
    repo_root: Path
    release_dir: Path
    assembly_dir: Path
    steps: tuple[ReleaseStep, ...]


def _package_dir(repo_root: Path, *parts: str) -> Path:
    return repo_root.joinpath("packages", *parts)


def _read_build_backend(package_dir: Path) -> str:
    pyproject_path = package_dir / "pyproject.toml"
    data = tomllib.loads(pyproject_path.read_text(encoding="utf-8"))
    build_backend = data.get("build-system", {}).get("build-backend")
    if not build_backend:
        raise ValueError(f"missing [build-system].build-backend in {pyproject_path}")
    return build_backend


def validate_repo_root(repo_root: Path) -> None:
    required_paths = [
        repo_root / "README.md",
        repo_root / "docs" / "architecture.md",
        _package_dir(repo_root, "client-wires", "pyproject.toml"),
        _package_dir(repo_root, "compat-bridge", "pyproject.toml"),
        _package_dir(repo_root, "fixer-mcp-server", "go.mod"),
    ]
    missing = [str(path) for path in required_paths if not path.exists()]
    if missing:
        raise FileNotFoundError(
            "repo root does not look like the staged GitHub repo; missing: "
            + ", ".join(missing)
        )


def build_release_plan(
    *,
    version: str,
    repo_root: Path,
    include_tests: bool = True,
) -> ReleasePlan:
    validate_repo_root(repo_root)
    release_dir = repo_root / "dist" / "releases" / version
    assembly_dir = release_dir / "assembly" / ASSEMBLY_ROOT_NAME
    python_dist = release_dir / "python"
    go_dist = release_dir / "bin"
    steps: list[ReleaseStep] = []
    client_wires_dir = _package_dir(repo_root, "client-wires")
    compat_bridge_dir = _package_dir(repo_root, "compat-bridge")
    client_wires_backend = _read_build_backend(client_wires_dir)
    compat_bridge_backend = _read_build_backend(compat_bridge_dir)

    if include_tests:
        steps.append(
            ReleaseStep(
                name="repo python tests",
                command=("python3", "-m", "unittest", "discover", "-s", "tests"),
                cwd=repo_root,
            )
        )
        steps.append(
            ReleaseStep(
                name="fixer-mcp-server go tests",
                command=("go", "test", "./..."),
                cwd=_package_dir(repo_root, "fixer-mcp-server"),
            )
        )

    steps.extend(
        [
            ReleaseStep(
                name="build fixer-client-wires package",
                cwd=client_wires_dir,
                command=(
                    "python3",
                    "-m",
                    "pep517-backend-build",
                    "--backend",
                    client_wires_backend,
                    "--outdir",
                    str(python_dist / "client-wires"),
                ),
                outputs=(python_dist / "client-wires",),
                kind="python-pep517-build",
                build_backend=client_wires_backend,
            ),
            ReleaseStep(
                name="build fixer-compat-bridge package",
                cwd=compat_bridge_dir,
                command=(
                    "python3",
                    "-m",
                    "pep517-backend-build",
                    "--backend",
                    compat_bridge_backend,
                    "--outdir",
                    str(python_dist / "compat-bridge"),
                ),
                outputs=(python_dist / "compat-bridge",),
                kind="python-pep517-build",
                build_backend=compat_bridge_backend,
            ),
            ReleaseStep(
                name="build fixer_mcp binary",
                cwd=_package_dir(repo_root, "fixer-mcp-server"),
                command=(
                    "go",
                    "build",
                    "-o",
                    str(go_dist / "fixer_mcp"),
                    ".",
                ),
                outputs=(go_dist / "fixer_mcp",),
            ),
        ]
    )

    return ReleasePlan(
        version=version,
        repo_root=repo_root.resolve(),
        release_dir=release_dir.resolve(),
        assembly_dir=assembly_dir.resolve(),
        steps=tuple(steps),
    )


def _assembly_sources(repo_root: Path) -> tuple[Path, ...]:
    return tuple(repo_root / relative_path for relative_path in ASSEMBLY_INCLUDE_PATHS)


def _copy_assembly_tree(plan: ReleasePlan) -> tuple[Path, ...]:
    if plan.assembly_dir.exists():
        shutil.rmtree(plan.assembly_dir)
    plan.assembly_dir.mkdir(parents=True, exist_ok=True)

    copied_paths: list[Path] = []
    ignore = shutil.ignore_patterns(*ASSEMBLY_IGNORE_NAMES)
    for source in _assembly_sources(plan.repo_root):
        destination = plan.assembly_dir / source.relative_to(plan.repo_root)
        if source.is_dir():
            shutil.copytree(source, destination, ignore=ignore, dirs_exist_ok=True)
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination)
        copied_paths.append(destination)
    return tuple(copied_paths)


def write_assembly_manifest(plan: ReleasePlan) -> Path:
    copied_paths = _copy_assembly_tree(plan)
    manifest_path = plan.release_dir / "assembly-manifest.json"
    payload = {
        "version": plan.version,
        "generated_at_utc": datetime.now(UTC).isoformat(),
        "repo_root": str(plan.repo_root),
        "assembly_dir": str(plan.assembly_dir),
        "included_paths": [str(path.relative_to(plan.assembly_dir)) for path in copied_paths],
    }
    manifest_path.write_text(
        json.dumps(payload, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return manifest_path


def _manifest_payload(plan: ReleasePlan) -> dict[str, object]:
    return {
        "version": plan.version,
        "generated_at_utc": datetime.now(UTC).isoformat(),
        "repo_root": str(plan.repo_root),
        "release_dir": str(plan.release_dir),
        "assembly_dir": str(plan.assembly_dir),
        "assembly_manifest": str(plan.release_dir / "assembly-manifest.json"),
        "assembly_included_paths": [str(path) for path in ASSEMBLY_INCLUDE_PATHS],
        "steps": [
            {
                "name": step.name,
                "cwd": str(step.cwd),
                "command": list(step.command),
                "outputs": [str(path) for path in step.outputs],
                "kind": step.kind,
                "build_backend": step.build_backend,
            }
            for step in plan.steps
        ],
    }


def _run_python_pep517_build(step: ReleaseStep) -> None:
    if step.build_backend is None:
        raise ValueError(f"missing build backend for {step.name}")
    if len(step.outputs) != 1:
        raise ValueError(f"{step.name} requires exactly one output directory")

    output_dir = step.outputs[0]
    output_dir.mkdir(parents=True, exist_ok=True)
    backend = importlib.import_module(step.build_backend)

    previous_cwd = Path.cwd()
    try:
        os.chdir(step.cwd)
        backend.build_sdist(str(output_dir), {})
        backend.build_wheel(str(output_dir), {})
    finally:
        os.chdir(previous_cwd)


def write_release_manifest(plan: ReleasePlan) -> Path:
    plan.release_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = plan.release_dir / "release-manifest.json"
    manifest_path.write_text(
        json.dumps(_manifest_payload(plan), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return manifest_path


def run_release_plan(plan: ReleasePlan) -> Path:
    plan.release_dir.mkdir(parents=True, exist_ok=True)
    write_assembly_manifest(plan)
    for step in plan.steps:
        for output in step.outputs:
            output.parent.mkdir(parents=True, exist_ok=True)
        if step.kind == "command":
            subprocess.run(step.command, cwd=step.cwd, check=True)
            continue
        if step.kind == "python-pep517-build":
            _run_python_pep517_build(step)
            continue
        raise ValueError(f"unsupported release step kind: {step.kind}")
    return write_release_manifest(plan)


def plan_as_json(plan: ReleasePlan) -> str:
    payload = asdict(plan)
    payload["repo_root"] = str(plan.repo_root)
    payload["release_dir"] = str(plan.release_dir)
    payload["assembly_dir"] = str(plan.assembly_dir)
    payload["assembly_included_paths"] = [str(path) for path in ASSEMBLY_INCLUDE_PATHS]
    payload["steps"] = [
        {
            "name": step.name,
            "cwd": str(step.cwd),
            "command": list(step.command),
            "outputs": [str(path) for path in step.outputs],
            "kind": step.kind,
            "build_backend": step.build_backend,
        }
        for step in plan.steps
    ]
    return json.dumps(payload, indent=2, sort_keys=True)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build a repo-native release plan for the GitHub-ready Fixer MCP track."
    )
    parser.add_argument("--version", default=DEFAULT_VERSION, help="release version label")
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=Path(__file__).resolve().parents[1],
        help="path to github_repo",
    )
    parser.add_argument(
        "--skip-tests",
        action="store_true",
        help="omit repo/unit validation steps from the release plan",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="print the release plan without executing build commands",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="print machine-readable plan or result metadata",
    )
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    plan = build_release_plan(
        version=args.version,
        repo_root=args.repo_root.resolve(),
        include_tests=not args.skip_tests,
    )

    if args.dry_run:
        print(plan_as_json(plan) if args.json else f"dry run release dir: {plan.release_dir}")
        if not args.json:
            for step in plan.steps:
                print(f"- {step.name}: {' '.join(step.command)}")
        return 0

    manifest_path = run_release_plan(plan)
    if args.json:
        print(
            json.dumps(
                {"manifest": str(manifest_path), "release_dir": str(plan.release_dir)},
                indent=2,
                sort_keys=True,
            )
        )
    else:
        print(f"release manifest: {manifest_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
