from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path


def _catalog_path() -> Path:
    return Path(__file__).resolve().parent / "data" / "backend-catalog.json"


@lru_cache(maxsize=1)
def load_backend_catalog() -> dict[str, dict[str, object]]:
    payload = json.loads(_catalog_path().read_text(encoding="utf-8"))
    raw_backends = payload.get("backends", {})
    if not isinstance(raw_backends, dict):
        raise RuntimeError(f"{_catalog_path()} does not contain an object-valued backends map")

    catalog: dict[str, dict[str, object]] = {}
    for name, entry in raw_backends.items():
        if not isinstance(entry, dict):
            raise RuntimeError(f"{_catalog_path()} entry for {name!r} must be an object")
        catalog[str(name)] = entry
    return catalog


def load_backend_entry(name: str) -> dict[str, object]:
    catalog = load_backend_catalog()
    try:
        return catalog[name]
    except KeyError as exc:
        supported = ", ".join(sorted(catalog))
        raise RuntimeError(f"Backend catalog missing entry for {name!r}. Available: {supported}") from exc
