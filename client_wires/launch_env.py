from __future__ import annotations

PROXY_ENV_NAMES: tuple[str, ...] = (
    "ALL_PROXY",
    "all_proxy",
    "HTTP_PROXY",
    "http_proxy",
    "HTTPS_PROXY",
    "https_proxy",
    "NO_PROXY",
    "no_proxy",
)


def clear_proxy_env(target_env: dict[str, str]) -> dict[str, str]:
    for name in PROXY_ENV_NAMES:
        target_env.pop(name, None)
    return target_env
