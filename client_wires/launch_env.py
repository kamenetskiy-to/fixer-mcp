"""Child-process egress policy for launched provider CLIs.

The launcher builds a backend env (see ``client_wires/fixer_wire.py``) and then
normalizes the proxy layer here. Historically it always *cleared* every proxy
variable, which is the right default on a Mac whose direct route to the provider
works. On hosts whose only route is an operator proxy/VPN (a WSL2 box behind a
tunnel) that same clearing removed the only working egress and surfaced as a
provider geo ``403`` that is indistinguishable from a broken provider.

Cleared stays the default. Keeping the operator egress requires an explicit,
documented opt-in:

* ``FLEET_KEEP_PROXY`` - when truthy (``1``/``true``/``yes``/``on``) the proxy
  values already present in the child env are kept (after sanitation). When it is
  explicitly falsey (``0``/``false``/``no``/``off``) it is a kill switch and the
  proxy is cleared even if the VPN marker exists. When it is unset/empty the
  marker decides.
* The VPN marker file ``${XDG_CONFIG_HOME:-$HOME/.config}/fleet/vpn.env`` - when
  it exists the tunnel is considered up. Its proxy values describe the *live*
  egress and are therefore preferred over values that may have gone stale in
  ``~/.codex/llm.env``. ``FLEET_VPN_ENV_FILE`` overrides the marker path.

Keeping is never unconditional. The kept env is sanitized so a kept proxy cannot
be a dead end or a tunnel bypass:

* a proxy URL that points at a loopback host with nothing listening on the port
  is dropped (TCP reachability probe);
* ``NO_PROXY``/``no_proxy`` keeps only loopback, LAN/CGNAT, Tailscale
  (``*.ts.net``), single-label intranet and IPv6 local entries; public entries
  that would bypass the operator's tunnel are dropped.

Hosts that do not opt in take the original clear-only path unchanged.
"""

from __future__ import annotations

import ipaddress
import os
import socket
import urllib.parse
from pathlib import Path
from typing import Callable

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

#: Proxy URL variables (everything except the NO_PROXY bypass list).
PROXY_URL_ENV_NAMES: tuple[str, ...] = (
    "ALL_PROXY",
    "all_proxy",
    "HTTP_PROXY",
    "http_proxy",
    "HTTPS_PROXY",
    "https_proxy",
)

#: ``no_proxy`` bypass-list variables.
NO_PROXY_ENV_NAMES: tuple[str, ...] = ("NO_PROXY", "no_proxy")

#: Opt-in switch that keeps the child env proxies (see module docstring).
KEEP_PROXY_ENV_NAME = "FLEET_KEEP_PROXY"

#: Optional override for the VPN marker file path (test/automation hook).
VPN_ENV_FILE_ENV_NAME = "FLEET_VPN_ENV_FILE"

#: Relative marker location under ``$XDG_CONFIG_HOME`` (or ``$HOME/.config``).
VPN_ENV_RELATIVE_PATH: tuple[str, ...] = ("fleet", "vpn.env")

#: How long a single loopback reachability probe may take.
PROXY_PROBE_TIMEOUT_SECONDS = 0.5

_TRUTHY_TOKENS = frozenset({"1", "true", "yes", "on", "y"})
_FALSEY_TOKENS = frozenset({"0", "false", "no", "off", "n"})

_DEFAULT_PROXY_PORTS: dict[str, int] = {
    "http": 80,
    "https": 443,
    "socks": 1080,
    "socks4": 1080,
    "socks4a": 1080,
    "socks5": 1080,
    "socks5h": 1080,
}

#: Ranges an operator tunnel must never be bypassed for (loopback, RFC1918,
#: link-local, CGNAT, IPv6 loopback/ULA/link-local).
_OPERATOR_LOCAL_NETWORKS: tuple[ipaddress._BaseNetwork, ...] = tuple(
    ipaddress.ip_network(cidr)
    for cidr in (
        "0.0.0.0/8",
        "10.0.0.0/8",
        "100.64.0.0/10",
        "127.0.0.0/8",
        "169.254.0.0/16",
        "172.16.0.0/12",
        "192.168.0.0/16",
        "::1/128",
        "fc00::/7",
        "fe80::/10",
    )
)

#: Domain suffixes that belong to the operator's own network fabric.
_OPERATOR_LOCAL_SUFFIXES: tuple[str, ...] = (
    "localhost",
    "ts.net",
    "local",
    "lan",
    "internal",
    "home.arpa",
)


def _env_lookup(name: str, target_env: dict[str, str] | None) -> str | None:
    if target_env is not None:
        value = target_env.get(name)
        if value is not None:
            return value
    return os.environ.get(name)


def _parse_bool(raw: str) -> bool | None:
    token = raw.strip().lower()
    if token in _TRUTHY_TOKENS:
        return True
    if token in _FALSEY_TOKENS:
        return False
    return None


def vpn_env_path(target_env: dict[str, str] | None = None) -> Path:
    """Return the VPN marker file path for this environment."""

    override = _env_lookup(VPN_ENV_FILE_ENV_NAME, target_env)
    if override and override.strip():
        return Path(override.strip()).expanduser()
    xdg = _env_lookup("XDG_CONFIG_HOME", target_env)
    if xdg and xdg.strip():
        root = Path(xdg.strip()).expanduser()
    else:
        home = _env_lookup("HOME", target_env)
        if home and home.strip():
            root = Path(home.strip()).expanduser() / ".config"
        else:
            root = Path.home() / ".config"
    return root.joinpath(*VPN_ENV_RELATIVE_PATH)


def keep_proxy_env_enabled(target_env: dict[str, str] | None = None) -> bool:
    """Decide whether the operator egress is kept for this child env."""

    raw = _env_lookup(KEEP_PROXY_ENV_NAME, target_env)
    if raw and raw.strip():
        return bool(_parse_bool(raw))
    return vpn_env_path(target_env).is_file()


def load_vpn_env_proxies(target_env: dict[str, str] | None = None) -> dict[str, str]:
    """Read proxy values from the VPN marker file, if it exists."""

    try:
        text = vpn_env_path(target_env).read_text(encoding="utf-8")
    except OSError:
        return {}
    values: dict[str, str] = {}
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.startswith("export "):
            stripped = stripped[len("export "):].strip()
        if "=" not in stripped:
            continue
        key, raw_value = stripped.split("=", 1)
        key = key.strip()
        if key not in PROXY_ENV_NAMES:
            continue
        values[key] = raw_value.strip().strip('"').strip("'")
    return values


def _host_is_loopback_name(host: str) -> bool:
    normalized = host.strip().strip("[]").lower()
    if not normalized:
        return False
    if normalized == "localhost" or normalized.endswith(".localhost"):
        return True
    try:
        return ipaddress.ip_address(normalized).is_loopback
    except ValueError:
        return False


def _ip_is_operator_local(ip: ipaddress._BaseAddress) -> bool:
    return any(ip in network for network in _OPERATOR_LOCAL_NETWORKS)


def _cidr_is_operator_local(value: str) -> bool:
    try:
        network = ipaddress.ip_network(value, strict=False)
    except ValueError:
        return False
    return any(
        network.version == local.version and network.subnet_of(local)
        for local in _OPERATOR_LOCAL_NETWORKS
    )


def _strip_wildcard_prefix(token: str) -> str:
    if token.startswith("*."):
        return token[2:]
    if token.startswith("."):
        return token[1:]
    return token


def _split_no_proxy_host(token: str) -> str:
    """Reduce a ``no_proxy`` token to its host part (``[::1]:8080`` -> ``::1``)."""

    if token.startswith("["):
        end = token.find("]")
        if end != -1:
            return token[1:end]
    if token.count(":") == 1:
        host, _, port = token.partition(":")
        if port.isdigit():
            return host
    return token


def _host_is_operator_local_domain(host: str) -> bool:
    for suffix in _OPERATOR_LOCAL_SUFFIXES:
        if host == suffix or host.endswith("." + suffix):
            return True
    return False


def no_proxy_entry_allowed(entry: str) -> bool:
    """Whether a single ``no_proxy`` entry may stay in the kept env."""

    token = entry.strip().lower()
    if not token or token == "*":
        return False
    token = _strip_wildcard_prefix(token)
    host = _split_no_proxy_host(token)
    if not host:
        return False
    if "/" in host:
        return _cidr_is_operator_local(host)
    if _host_is_loopback_name(host):
        return True
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        pass
    else:
        return _ip_is_operator_local(ip)
    if "." not in host:
        # Single-label names resolve through the operator's own resolver.
        return True
    return _host_is_operator_local_domain(host)


def sanitize_no_proxy(value: str) -> str:
    """Drop ``no_proxy`` entries that would bypass the operator's tunnel."""

    kept: list[str] = []
    seen: set[str] = set()
    for raw_entry in value.split(","):
        entry = raw_entry.strip()
        if not entry or not no_proxy_entry_allowed(entry):
            continue
        lowered = entry.lower()
        if lowered in seen:
            continue
        seen.add(lowered)
        kept.append(entry)
    return ",".join(kept)


def parse_proxy_endpoint(value: str) -> tuple[str, int | None, str] | None:
    """Return ``(host, port, scheme)`` for a proxy URL, or ``None``."""

    raw = (value or "").strip()
    if not raw:
        return None
    if "://" not in raw:
        raw = "http://" + raw
    try:
        parts = urllib.parse.urlsplit(raw)
    except ValueError:
        return None
    host = parts.hostname
    if not host:
        return None
    try:
        port = parts.port
    except ValueError:
        port = None
    scheme = parts.scheme.lower()
    if port is None:
        port = _DEFAULT_PROXY_PORTS.get(scheme)
    return host, port, scheme


def _probe_tcp(host: str, port: int, timeout: float) -> bool:
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except OSError:
        return False


def loopback_proxy_unreachable(
    value: str,
    probe: Callable[[str, int, float], bool] | None = None,
) -> bool:
    """True when a proxy points at a dead loopback port and must be dropped."""

    endpoint = parse_proxy_endpoint(value)
    if endpoint is None:
        return False
    host, port, _scheme = endpoint
    if not _host_is_loopback_name(host) or port is None:
        return False
    probe_fn = probe or _probe_tcp
    return not probe_fn(host, port, PROXY_PROBE_TIMEOUT_SECONDS)


def clear_proxy_env(target_env: dict[str, str]) -> dict[str, str]:
    """Normalize the child env proxy layer.

    Default (no opt-in): remove every proxy variable. Opt-in: prefer the live
    VPN marker values, keep the remaining operator egress, but drop dead
    loopback proxies and tunnel-bypassing ``no_proxy`` entries.
    """

    if not keep_proxy_env_enabled(target_env):
        for name in PROXY_ENV_NAMES:
            target_env.pop(name, None)
        return target_env

    for name, value in load_vpn_env_proxies(target_env).items():
        target_env[name] = value

    for name in PROXY_URL_ENV_NAMES:
        value = target_env.get(name)
        if value is None:
            continue
        if loopback_proxy_unreachable(value):
            target_env.pop(name, None)

    for name in NO_PROXY_ENV_NAMES:
        value = target_env.get(name)
        if value is None:
            continue
        sanitized = sanitize_no_proxy(value)
        if sanitized:
            target_env[name] = sanitized
        else:
            target_env.pop(name, None)

    return target_env
