#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
if [ "${FIXER_STUDIO_BACKEND_DEBUG:-0}" = "1" ]; then
  set -x
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" >/dev/null 2>&1 && pwd -P)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd -P)"
SELF_PATH="$SCRIPT_DIR/$(basename "$0")"
COMMAND="${1:-}"
if [ -n "$COMMAND" ]; then
  shift
fi

: "${HOME:?HOME must be set}"
STATE_DIR="${FIXER_STUDIO_BACKEND_STATE_DIR:-$HOME/Library/Application Support/Fixer Studio Backend}"
PID_DIR="$STATE_DIR/pids"
LOG_DIR="$STATE_DIR/logs"
LOCK_DIR="$STATE_DIR/start.lock"
MANIFEST="$STATE_DIR/service.listeners"
MAKE_BIN="${FIXER_STUDIO_BACKEND_MAKE_BIN:-$(command -v make || true)}"
DASHBOARD_PORT="${FIXER_STUDIO_DASHBOARD_PORT:-18090}"
BRIDGE_PORT="${FIXER_STUDIO_BRIDGE_PORT:-14242}"
SERVERPOD_PORT="${FIXER_STUDIO_SERVERPOD_PORT:-28080}"
SERVERPOD_WEB_PORT="${FIXER_STUDIO_SERVERPOD_WEB_PORT:-28082}"
SERVERPOD_COMPATIBILITY_FINGERPRINT="fixer-studio-serverpod-rpc-v1"
READY_TIMEOUT="${FIXER_STUDIO_BACKEND_READY_TIMEOUT_SECONDS:-90}"
LOCK_TIMEOUT="${FIXER_STUDIO_BACKEND_LOCK_TIMEOUT_SECONDS:-100}"
LOCK_HELD=0
LOCK_TOKEN=""

usage() {
  cat <<'EOF'
Usage: fixer_studio_backend_service.sh COMMAND

Commands:
  run APP_PID  Ensure the shared local backend is ready for an app process.
  status       Print readiness and recorded listener ownership.
  self-test    Run a disposable shared-start lifecycle test.

The run command never stops the backend when APP_PID exits. Stable and
Experimental are clients of one shared service; only explicit operator
lifecycle commands outside the apps may stop that service.
EOF
}

die() {
  printf 'fixer-studio-backend: %s\n' "$*" >&2
  exit 1
}

timestamp() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

ensure_state_dirs() {
  mkdir -p "$STATE_DIR" "$PID_DIR" "$LOG_DIR"
  chmod 0700 "$STATE_DIR" "$PID_DIR" "$LOG_DIR" 2>/dev/null || true
}

process_start_identity() {
  local process_pid="$1"
  ps -p "$process_pid" -o lstart= 2>/dev/null | awk '{$1=$1; print}' || true
}

random_token() {
  LC_ALL=C od -An -N16 -tx1 /dev/urandom | tr -d ' \n'
}

owner_field() {
  local field="$1"
  local owner_file="$2"
  sed -n "s/^${field}=//p" "$owner_file" 2>/dev/null | sed -n '1p'
}

lock_owner_is_live() {
  local owner_file="$LOCK_DIR/owner"
  local owner_pid
  local recorded_start
  local current_start
  local process_state
  [ -f "$owner_file" ] || return 1
  owner_pid="$(owner_field pid "$owner_file")"
  recorded_start="$(owner_field start "$owner_file")"
  case "$owner_pid" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ -n "$recorded_start" ] || return 1
  kill -0 "$owner_pid" 2>/dev/null || return 1
  process_state="$(ps -p "$owner_pid" -o stat= 2>/dev/null | awk '{$1=$1; print}')"
  case "$process_state" in
    ''|*Z*) return 1 ;;
  esac
  current_start="$(process_start_identity "$owner_pid")"
  [ -n "$current_start" ] && [ "$current_start" = "$recorded_start" ]
}

write_lock_owner() {
  local owner_start
  local temporary
  LOCK_TOKEN="$(random_token)"
  owner_start="$(process_start_identity "$$")"
  [ -n "$owner_start" ] || die "cannot read launcher process start identity"
  temporary="$LOCK_DIR/.owner.$$.$LOCK_TOKEN"
  {
    printf 'pid=%s\n' "$$"
    printf 'start=%s\n' "$owner_start"
    printf 'token=%s\n' "$LOCK_TOKEN"
    printf 'created_at=%s\n' "$(timestamp)"
  } >"$temporary"
  chmod 0600 "$temporary"
  mv "$temporary" "$LOCK_DIR/owner"
  LOCK_HELD=1
}

try_acquire_start_lock() {
  ensure_state_dirs
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    write_lock_owner
    return 0
  fi

  # A creator may be between mkdir and the atomic owner-file rename.
  local attempt=0
  while [ "$attempt" -lt 20 ] && [ ! -f "$LOCK_DIR/owner" ]; do
    sleep 0.05
    attempt=$((attempt + 1))
  done
  if lock_owner_is_live; then
    return 75
  fi

  rm -f "$LOCK_DIR/owner" 2>/dev/null || true
  if ! rmdir "$LOCK_DIR" 2>/dev/null; then
    return 75
  fi
  if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    return 75
  fi
  write_lock_owner
}

acquire_start_lock() {
  local started_at=$SECONDS
  local status
  while true; do
    status=0
    try_acquire_start_lock || status=$?
    if [ "$status" -eq 0 ]; then
      return 0
    fi
    [ "$status" -eq 75 ] || return "$status"
    if [ $((SECONDS - started_at)) -ge "$LOCK_TIMEOUT" ]; then
      die "timed out waiting for the shared-backend startup owner"
    fi
    sleep 0.2
  done
}

release_start_lock() {
  if [ "$LOCK_HELD" -ne 1 ]; then
    return 0
  fi
  local recorded_token=""
  recorded_token="$(owner_field token "$LOCK_DIR/owner")"
  if [ -n "$LOCK_TOKEN" ] && [ "$recorded_token" = "$LOCK_TOKEN" ]; then
    rm -f "$LOCK_DIR/owner"
    rmdir "$LOCK_DIR" 2>/dev/null || true
  fi
  LOCK_HELD=0
  LOCK_TOKEN=""
}

cleanup_on_exit() {
  local status=$?
  trap - EXIT HUP INT TERM
  release_start_lock
  exit "$status"
}

trap cleanup_on_exit EXIT
trap 'exit 130' HUP INT TERM

endpoint_ready() {
  curl --noproxy '*' --connect-timeout 1 --max-time 2 -fsS "$1" >/dev/null 2>&1
}

serverpod_body_is_json() {
  python3 -c 'import json, sys; json.loads(sys.argv[1])' "$1" >/dev/null 2>&1
}

serverpod_response_is_compatible() {
  local status="$1"
  local body="$2"
  local normalized
  normalized="$(printf '%s' "$body" | tr '[:upper:]' '[:lower:]')"
  case "$normalized" in
    *'endpoint not found'*) return 1 ;;
  esac

  case "$status" in
    2??)
      [ -n "$body" ] && serverpod_body_is_json "$body"
      ;;
    400)
      serverpod_body_is_json "$body" ||
        case "$body" in
          'Missing required query parameter:'*|\
          'Invalid JSON in body'|\
          'Endpoint name is not valid'|\
          'Endpoint method is not of the expected type'|\
          'Request has invalid "authorization" header'*) return 0 ;;
          *) return 1 ;;
        esac
      ;;
    401|403)
      [ -z "$body" ] || serverpod_body_is_json "$body"
      ;;
    *) return 1 ;;
  esac
}

serverpod_witness_ready() {
  local path="$1"
  local payload="$2"
  local response
  local status
  local body
  response="$(curl --noproxy '*' --connect-timeout 1 --max-time 2 -sS \
    -H 'content-type: application/json; charset=utf-8' \
    --data "$payload" \
    --write-out $'\n%{http_code}' \
    "http://127.0.0.1:$SERVERPOD_PORT$path" 2>/dev/null)" || return 1
  status="${response##*$'\n'}"
  body="${response%$'\n'*}"
  case "$status" in
    [0-9][0-9][0-9]) ;;
    *) return 1 ;;
  esac
  serverpod_response_is_compatible "$status" "$body"
}

serverpod_compatibility_ready() {
  serverpod_witness_ready \
    '/clientAuth/login' \
    '{"email":"fixer-studio-compatibility-invalid"}' &&
    serverpod_witness_ready '/clientProfile/current' '{}' &&
    serverpod_witness_ready '/dashboardRuntime/health' '{}'
}

required_listener_snapshot() {
  local port
  local listener_pid
  local listeners
  local start
  for port in "$DASHBOARD_PORT" "$BRIDGE_PORT" "$SERVERPOD_PORT" "$SERVERPOD_WEB_PORT"; do
    listeners="$(listener_pids "$port")"
    if [ -z "$listeners" ]; then
      printf '%s|none|none\n' "$port"
      continue
    fi
    while IFS= read -r listener_pid; do
      [ -n "$listener_pid" ] || continue
      start="$(process_start_identity "$listener_pid")"
      if [ -z "$start" ]; then
        printf '%s|%s|missing\n' "$port" "$listener_pid"
      else
        printf '%s|%s|%s\n' "$port" "$listener_pid" "$start"
      fi
    done <<<"$listeners"
  done
}

LAST_PROBE_SNAPSHOT=""
backend_probe_once() {
  local before
  local after
  local ready=0
  before="$(required_listener_snapshot)"
  if endpoint_ready "http://127.0.0.1:$DASHBOARD_PORT/health" &&
    endpoint_ready "http://127.0.0.1:$BRIDGE_PORT/health" &&
    endpoint_ready "http://127.0.0.1:$SERVERPOD_PORT/livez" &&
    serverpod_compatibility_ready; then
    ready=1
  fi
  after="$(required_listener_snapshot)"
  LAST_PROBE_SNAPSHOT="$after"
  if [ "$before" != "$after" ]; then
    return 76
  fi
  [ "$ready" -eq 1 ]
}

backend_ready() {
  backend_probe_once
}

wait_until_live() {
  local started_at=$SECONDS
  while ! endpoint_ready "http://127.0.0.1:$DASHBOARD_PORT/health" ||
    ! endpoint_ready "http://127.0.0.1:$BRIDGE_PORT/health" ||
    ! endpoint_ready "http://127.0.0.1:$SERVERPOD_PORT/livez"; do
    if [ $((SECONDS - started_at)) -ge "$READY_TIMEOUT" ]; then
      return 1
    fi
    sleep 0.25
  done
}

listener_pids() {
  lsof -nP -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | sort -u
}

manifest_has_listener() {
  local port="$1"
  local listener_pid="$2"
  local start="$3"
  grep -Fqx "listener|$port|$listener_pid|$start" "$MANIFEST" 2>/dev/null
}

record_listener_manifest() {
  local proven_snapshot="$1"
  local temporary="$MANIFEST.tmp.$$"
  local current_snapshot
  local snapshot_line
  local port
  local listener_pid
  local start
  current_snapshot="$(required_listener_snapshot)"
  [ "$current_snapshot" = "$proven_snapshot" ] || \
    die "listener identity changed after compatibility proof; refusing to record ownership"
  {
    printf 'schema=2\n'
    printf 'project_root=%s\n' "$PROJECT_ROOT"
    printf 'compatibility_fingerprint=%s\n' "$SERVERPOD_COMPATIBILITY_FINGERPRINT"
    printf 'recorded_at=%s\n' "$(timestamp)"
    while IFS= read -r snapshot_line; do
      IFS='|' read -r port listener_pid start <<<"$snapshot_line"
      [ "$listener_pid" != "none" ] || continue
      [ "$start" != "missing" ] || \
        die "listener identity disappeared after compatibility proof"
      printf 'listener|%s|%s|%s\n' "$port" "$listener_pid" "$start"
    done <<<"$proven_snapshot"
  } >"$temporary"
  chmod 0600 "$temporary"
  mv "$temporary" "$MANIFEST"
}

partial_listeners_are_owned() {
  local recorded_root
  local current_snapshot
  local current_listeners
  local recorded_listeners
  [ -f "$MANIFEST" ] || return 1
  recorded_root="$(sed -n 's/^project_root=//p' "$MANIFEST" | sed -n '1p')"
  [ "$recorded_root" = "$PROJECT_ROOT" ] || return 1
  current_snapshot="$(required_listener_snapshot)"
  case "$current_snapshot" in
    *'|missing'*) return 1 ;;
  esac
  current_listeners="$(printf '%s\n' "$current_snapshot" | \
    sed -n '/|none|none$/!p' | sort)"
  [ -n "$current_listeners" ] || return 1
  recorded_listeners="$(sed -n 's/^listener|//p' "$MANIFEST" | sort)"
  [ "$current_listeners" = "$recorded_listeners" ]
}

owned_listener_identities() {
  local port
  local listener_pid
  local start
  for port in "$DASHBOARD_PORT" "$BRIDGE_PORT" "$SERVERPOD_PORT" "$SERVERPOD_WEB_PORT"; do
    while IFS= read -r listener_pid; do
      [ -n "$listener_pid" ] || continue
      start="$(process_start_identity "$listener_pid")"
      [ -n "$start" ] || continue
      printf '%s|%s\n' "$listener_pid" "$start"
    done < <(listener_pids "$port")
  done | sort -u
}

stop_owned_listeners() {
  local identities
  local listener_pid
  local recorded_start
  local current_start
  local live
  local attempt
  partial_listeners_are_owned || \
    die "a required backend port is occupied by an unowned or PID-reused process; refusing to stop it"
  identities="$(owned_listener_identities)"
  [ -n "$identities" ] || die "owned listener set disappeared before reconciliation"

  # The project lifecycle runner is invoked only after every occupied listener
  # has an exact project-root/PID/start-identity match in the manifest.
  make_backend stop-server

  while IFS='|' read -r listener_pid recorded_start; do
    [ -n "$listener_pid" ] || continue
    current_start="$(process_start_identity "$listener_pid")"
    if [ -n "$current_start" ] && [ "$current_start" != "$recorded_start" ]; then
      die "owned listener identity changed during reconciliation; refusing to signal PID $listener_pid"
    fi
    if [ "$current_start" = "$recorded_start" ]; then
      kill "$listener_pid" 2>/dev/null || true
    fi
  done <<<"$identities"

  for attempt in {1..20}; do
    live=0
    while IFS='|' read -r listener_pid recorded_start; do
      [ -n "$listener_pid" ] || continue
      current_start="$(process_start_identity "$listener_pid")"
      if [ -n "$current_start" ] && [ "$current_start" = "$recorded_start" ]; then
        live=1
      fi
    done <<<"$identities"
    [ "$live" -eq 1 ] || return 0
    sleep 0.2
  done

  while IFS='|' read -r listener_pid recorded_start; do
    [ -n "$listener_pid" ] || continue
    current_start="$(process_start_identity "$listener_pid")"
    if [ -n "$current_start" ] && [ "$current_start" = "$recorded_start" ]; then
      kill -9 "$listener_pid" 2>/dev/null || true
    fi
  done <<<"$identities"
}

any_managed_listener() {
  local port
  for port in "$DASHBOARD_PORT" "$BRIDGE_PORT" "$SERVERPOD_PORT" "$SERVERPOD_WEB_PORT"; do
    if [ -n "$(listener_pids "$port")" ]; then
      return 0
    fi
  done
  return 1
}

make_backend() {
  local target="$1"
  [ -n "$MAKE_BIN" ] && [ -x "$MAKE_BIN" ] || die "make is required to manage the local backend"
  FIXER_STUDIO_DASHBOARD_PORT="$DASHBOARD_PORT" \
  FIXER_STUDIO_BRIDGE_PORT="$BRIDGE_PORT" \
  FIXER_STUDIO_SERVERPOD_PORT="$SERVERPOD_PORT" \
  FIXER_STUDIO_SERVERPOD_WEB_PORT="$SERVERPOD_WEB_PORT" \
    "$MAKE_BIN" -C "$PROJECT_ROOT" "$target" \
      PID_DIR="$PID_DIR" \
      LOG_DIR="$LOG_DIR" \
      DASHBOARD_API_ADDR="127.0.0.1:$DASHBOARD_PORT" \
      SERVERPOD_API_PORT="$SERVERPOD_PORT" \
      SERVERPOD_WEB_PORT="$SERVERPOD_WEB_PORT" \
      CODEX_BRIDGE_PORT="$BRIDGE_PORT"
}

prepare_for_start() {
  if ! any_managed_listener; then
    return 0
  fi
  if ! partial_listeners_are_owned; then
    die "a required backend port is occupied by an unowned or PID-reused process; refusing to stop it"
  fi
  printf 'fixer-studio-backend: reconciling an incompatible or partial previously owned stack\n' >&2
  stop_owned_listeners
  local port
  for port in "$DASHBOARD_PORT" "$BRIDGE_PORT" "$SERVERPOD_PORT" "$SERVERPOD_WEB_PORT"; do
    [ -z "$(listener_pids "$port")" ] || die "owned listener on port $port did not stop"
  done
}

run_backend() {
  [ "$#" -eq 1 ] || die "run requires the calling app PID"
  local client_pid="$1"
  local initial_status=0
  local locked_status=0
  local retry_status=0
  local proven_snapshot
  case "$client_pid" in
    ''|*[!0-9]*) die "app PID must be numeric" ;;
  esac
  kill -0 "$client_pid" 2>/dev/null || die "calling app PID is not live: $client_pid"
  command -v curl >/dev/null 2>&1 || die "curl is required for backend readiness checks"
  command -v lsof >/dev/null 2>&1 || die "lsof is required for safe listener ownership checks"
  command -v python3 >/dev/null 2>&1 || die "Python 3 is required to classify Serverpod responses"

  backend_probe_once || initial_status=$?
  if [ "$initial_status" -eq 0 ]; then
    printf 'backend=ready ownership=shared action=attached client_pid=%s\n' "$client_pid"
    return 0
  fi

  acquire_start_lock

  # An identity change invalidates the unlocked result. It gets exactly one
  # full retry under the startup lock and can never lead to reconciliation.
  if [ "$initial_status" -eq 76 ]; then
    backend_probe_once || locked_status=$?
    if [ "$locked_status" -eq 0 ]; then
      printf 'backend=ready ownership=shared action=attached-after-identity-retry client_pid=%s\n' "$client_pid"
      release_start_lock
      return 0
    fi
    die "listener identity changed during compatibility proof; locked retry did not prove a stable compatible backend, refusing reconciliation"
  fi

  backend_probe_once || locked_status=$?
  if [ "$locked_status" -eq 0 ]; then
    printf 'backend=ready ownership=shared action=attached-after-wait client_pid=%s\n' "$client_pid"
    release_start_lock
    return 0
  fi
  if [ "$locked_status" -eq 76 ]; then
    backend_probe_once || retry_status=$?
    if [ "$retry_status" -eq 0 ]; then
      printf 'backend=ready ownership=shared action=attached-after-identity-retry client_pid=%s\n' "$client_pid"
      release_start_lock
      return 0
    fi
    die "listener identity changed during the locked compatibility proof; retry failed closed without reconciliation"
  fi

  prepare_for_start
  printf 'fixer-studio-backend: starting the canonical local backend for client %s\n' "$client_pid"
  make_backend start-server
  if ! wait_until_live; then
    die "local backend did not become live on ports $DASHBOARD_PORT/$BRIDGE_PORT/$SERVERPOD_PORT within ${READY_TIMEOUT}s"
  fi
  locked_status=0
  retry_status=0
  backend_probe_once || locked_status=$?
  if [ "$locked_status" -eq 76 ]; then
    backend_probe_once || retry_status=$?
    locked_status="$retry_status"
  fi
  if [ "$locked_status" -ne 0 ]; then
    die "fresh local backend failed the stable $SERVERPOD_COMPATIBILITY_FINGERPRINT compatibility proof"
  fi
  proven_snapshot="$LAST_PROBE_SNAPSHOT"
  record_listener_manifest "$proven_snapshot"
  printf 'backend=ready ownership=shared action=started client_pid=%s\n' "$client_pid"
  release_start_lock
}

status_backend() {
  ensure_state_dirs
  if backend_ready; then
    printf 'backend=ready\n'
  else
    printf 'backend=not_ready\n'
  fi
  printf 'dashboard=http://127.0.0.1:%s/health\n' "$DASHBOARD_PORT"
  printf 'bridge=http://127.0.0.1:%s/health\n' "$BRIDGE_PORT"
  printf 'serverpod=http://127.0.0.1:%s/livez\n' "$SERVERPOD_PORT"
  printf 'serverpod_compatibility=%s\n' "$SERVERPOD_COMPATIBILITY_FINGERPRINT"
  printf 'serverpod_witnesses=POST:/clientAuth/login,POST:/clientProfile/current,POST:/dashboardRuntime/health\n'
  if [ -f "$MANIFEST" ]; then
    printf 'listener_manifest=%s\n' "$MANIFEST"
  else
    printf 'listener_manifest=none\n'
  fi
}

self_test() {
  command -v python3 >/dev/null 2>&1 || die "Python 3 is required for the backend self-test"
  command -v curl >/dev/null 2>&1 || die "curl is required for the backend self-test"
  command -v lsof >/dev/null 2>&1 || die "lsof is required for the backend self-test"
  local temporary
  local fixture_server
  local fake_make
  local ports
  local first_pid
  local second_pid
  local first_status
  local second_status
  local fourth_pid
  local fourth_status
  local fifth_pid
  local fifth_status
  local foreign_pid
  local foreign_status
  local fixture_status
  local owned_pid
  local witness
  temporary="$(mktemp -d "${TMPDIR:-/tmp}/fixer-studio-backend.XXXXXX")"
  fixture_server="$temporary/server.py"
  fake_make="$temporary/make"

  cleanup_backend_self_test() {
    local fixture_pid=""
    local pid_file
    for pid_file in "$temporary/state/fixture.pid" "$temporary/state/foreign.pid"; do
      if [ -f "$pid_file" ]; then
        fixture_pid="$(sed -n '1p' "$pid_file")"
      fi
      if [ -n "$fixture_pid" ]; then
        kill "$fixture_pid" >/dev/null 2>&1 || true
        wait "$fixture_pid" 2>/dev/null || true
      fi
      fixture_pid=""
    done
    rm -rf -- "$temporary"
  }
  trap cleanup_backend_self_test EXIT
  trap 'exit 130' HUP INT TERM

  ports="$(python3 - <<'PY'
import socket

sockets = []
ports = []
for _ in range(4):
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    sockets.append(sock)
    ports.append(str(sock.getsockname()[1]))
print(" ".join(ports))
for sock in sockets:
    sock.close()
PY
)"
  IFS=' ' read -r dashboard_port bridge_port serverpod_port web_port <<<"$ports"

  cat >"$fixture_server" <<'PY'
import http.server
import signal
import socketserver
import sys
import threading
import time

class Handler(http.server.BaseHTTPRequestHandler):
    def _record(self, body=""):
        with open(sys.argv[6], "a", encoding="utf-8") as request_log:
            request_log.write(f"{self.command} {self.path} {body}\n")

    def _mode(self):
        try:
            with open(sys.argv[5], encoding="utf-8") as mode_file:
                return mode_file.read().strip()
        except FileNotFoundError:
            return "stale"

    def do_GET(self):
        self._record()
        if self.path == "/stableRuntime/health" and self._mode() == "experimental":
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.end_headers()
            self.wfile.write(b'"stable-ok"')
            return
        if self.path not in ("/health", "/livez"):
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def do_POST(self):
        length = int(self.headers.get("content-length", "0"))
        body = self.rfile.read(length).decode("utf-8")
        self._record(body)
        if self._mode() == "stale":
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b"Endpoint not found")
            return
        if self.path == "/clientAuth/login":
            self.send_response(400)
            self.end_headers()
            self.wfile.write(b"Missing required query parameter: password")
            return
        if self.path == "/clientProfile/current":
            self.send_response(401)
            self.end_headers()
            return
        if self.path == "/dashboardRuntime/health":
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.end_headers()
            self.wfile.write(b'"ok"')
            return
        self.send_response(404)
        self.end_headers()
        self.wfile.write(b"Endpoint not found")

    def log_message(self, _format, *_args):
        return


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True


servers = []
for raw_port in sys.argv[1:5]:
    server = Server(("127.0.0.1", int(raw_port)), Handler)
    servers.append(server)
    threading.Thread(target=server.serve_forever, daemon=True).start()

def stop(*_args):
    for server in servers:
        server.shutdown()
    raise SystemExit(0)

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
while True:
    time.sleep(1)
PY

  cat >"$fake_make" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
target=""
for argument in "$@"; do
  case "$argument" in
    start-server|run-server|stop-server) target="$argument" ;;
  esac
done
case "$target" in
  start-server|run-server)
    printf 'start-server\n' >>"$FIXER_STUDIO_BACKEND_STATE_DIR/lifecycle.log"
    sleep "${FIXER_STUDIO_BACKEND_TEST_START_DELAY:-0}"
    printf '%s\n' "${FIXER_STUDIO_BACKEND_TEST_START_MODE:-current}" \
      >"$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.mode"
    nohup python3 "$FIXER_STUDIO_BACKEND_TEST_SERVER" \
      "$FIXER_STUDIO_DASHBOARD_PORT" "$FIXER_STUDIO_BRIDGE_PORT" \
      "$FIXER_STUDIO_SERVERPOD_PORT" "$FIXER_STUDIO_SERVERPOD_WEB_PORT" \
      "$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.mode" \
      "$FIXER_STUDIO_BACKEND_STATE_DIR/requests.log" \
      >"$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.log" 2>&1 </dev/null &
    printf '%s\n' "$!" >"$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.pid"
    ;;
  stop-server)
    printf 'stop-server\n' >>"$FIXER_STUDIO_BACKEND_STATE_DIR/lifecycle.log"
    if [ -f "$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.pid" ]; then
      kill "$(sed -n '1p' "$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.pid")" 2>/dev/null || true
      rm -f "$FIXER_STUDIO_BACKEND_STATE_DIR/fixture.pid"
    fi
    ;;
  *)
    printf 'fake make received no lifecycle target\n' >&2
    exit 2
    ;;
esac
SH
  chmod 0755 "$fake_make"

  export FIXER_STUDIO_BACKEND_STATE_DIR="$temporary/state"
  export FIXER_STUDIO_BACKEND_MAKE_BIN="$fake_make"
  export FIXER_STUDIO_BACKEND_TEST_SERVER="$fixture_server"
  export FIXER_STUDIO_DASHBOARD_PORT="$dashboard_port"
  export FIXER_STUDIO_BRIDGE_PORT="$bridge_port"
  export FIXER_STUDIO_SERVERPOD_PORT="$serverpod_port"
  export FIXER_STUDIO_SERVERPOD_WEB_PORT="$web_port"
  export FIXER_STUDIO_BACKEND_READY_TIMEOUT_SECONDS=10
  export FIXER_STUDIO_BACKEND_LOCK_TIMEOUT_SECONDS=15
  export FIXER_STUDIO_BACKEND_TEST_START_DELAY=1

  # This process also exercises the helpers directly, so bind their globals to
  # the disposable fixture rather than the operator's default state/ports.
  STATE_DIR="$temporary/state"
  PID_DIR="$STATE_DIR/pids"
  LOG_DIR="$STATE_DIR/logs"
  LOCK_DIR="$STATE_DIR/start.lock"
  MANIFEST="$STATE_DIR/service.listeners"
  MAKE_BIN="$fake_make"
  DASHBOARD_PORT="$dashboard_port"
  BRIDGE_PORT="$bridge_port"
  SERVERPOD_PORT="$serverpod_port"
  SERVERPOD_WEB_PORT="$web_port"
  READY_TIMEOUT=10
  LOCK_TIMEOUT=15

  lifecycle_count() {
    local event="$1"
    if [ ! -f "$temporary/state/lifecycle.log" ]; then
      printf '0\n'
      return 0
    fi
    grep -Fxc "$event" "$temporary/state/lifecycle.log" || true
  }

  # C1/C4: Stable and Experimental attach to one already-compatible fixture.
  # The checked-in ownership bytes and lifecycle log must remain untouched.
  ensure_state_dirs
  printf 'current\n' >"$temporary/state/fixture.mode"
  nohup python3 "$fixture_server" \
    "$dashboard_port" "$bridge_port" "$serverpod_port" "$web_port" \
    "$temporary/state/fixture.mode" "$temporary/state/requests.log" \
    >"$temporary/state/fixture.log" 2>&1 </dev/null &
  printf '%s\n' "$!" >"$temporary/state/fixture.pid"
  fixture_status=1
  for _ in {1..40}; do
    fixture_status=0
    backend_probe_once || fixture_status=$?
    [ "$fixture_status" -eq 0 ] && break
    sleep 0.1
  done
  [ "$fixture_status" -eq 0 ] || die "compatible fixture did not become ready"
  record_listener_manifest "$LAST_PROBE_SNAPSHOT"
  cp "$MANIFEST" "$temporary/original.listeners"

  "$SELF_PATH" run "$$" >"$temporary/first.log" 2>&1 &
  first_pid=$!
  sleep 0.2
  "$SELF_PATH" run "$$" >"$temporary/second.log" 2>&1 &
  second_pid=$!
  first_status=0
  second_status=0
  wait "$first_pid" || first_status=$?
  wait "$second_pid" || second_status=$?
  [ "$first_status" -eq 0 ] || die "first concurrent launcher failed: $(sed -n '1,120p' "$temporary/first.log")"
  [ "$second_status" -eq 0 ] || die "second concurrent launcher failed: $(sed -n '1,120p' "$temporary/second.log"); lock owner: $(sed -n '1,20p' "$temporary/state/start.lock/owner" 2>/dev/null || true); first launcher: $(sed -n '1,40p' "$temporary/first.log")"
  [ "$(lifecycle_count start-server)" = "0" ] || \
    die "compatible shared backend unexpectedly started again"
  [ "$(lifecycle_count stop-server)" = "0" ] || \
    die "compatible shared backend was unexpectedly stopped"
  cmp -s "$MANIFEST" "$temporary/original.listeners" || \
    die "compatible no-op attachment mutated the ownership manifest"

  # Both launcher clients have exited; the shared service must still be ready.
  FIXER_STUDIO_BACKEND_STATE_DIR="$temporary/state" \
  FIXER_STUDIO_DASHBOARD_PORT="$dashboard_port" \
  FIXER_STUDIO_BRIDGE_PORT="$bridge_port" \
  FIXER_STUDIO_SERVERPOD_PORT="$serverpod_port" \
  FIXER_STUDIO_SERVERPOD_WEB_PORT="$web_port" \
    "$SELF_PATH" status | grep -F 'backend=ready' >/dev/null

  "$SELF_PATH" run "$$" >"$temporary/third.log" 2>&1
  [ "$(lifecycle_count start-server)" = "0" ] || \
    die "client retry started the compatible shared backend"
  [ "$(lifecycle_count stop-server)" = "0" ] || \
    die "client close or retry stopped the compatible shared backend"

  # C2/C6: a live but stale owned Serverpod is reconciled once while two app
  # processes race. The replacement models a newer Experimental fingerprint
  # that retains a Stable-supported route.
  owned_pid="$(sed -n '1p' "$temporary/state/fixture.pid")"
  printf 'stale\n' >"$temporary/state/fixture.mode"
  "$SELF_PATH" status | grep -F 'backend=not_ready' >/dev/null || \
    die "livez 200 with unknown Serverpod endpoints was treated as compatible"
  export FIXER_STUDIO_BACKEND_TEST_START_MODE=experimental
  fourth_status=0
  fifth_status=0
  "$SELF_PATH" run "$$" >"$temporary/fourth.log" 2>&1 &
  fourth_pid=$!
  sleep 0.2
  "$SELF_PATH" run "$$" >"$temporary/fifth.log" 2>&1 &
  fifth_pid=$!
  wait "$fourth_pid" || fourth_status=$?
  wait "$fifth_pid" || fifth_status=$?
  [ "$fourth_status" -eq 0 ] || \
    die "owned stale backend was not reconciled: $(sed -n '1,120p' "$temporary/fourth.log")"
  [ "$fifth_status" -eq 0 ] || \
    die "concurrent stale-backend client failed: $(sed -n '1,120p' "$temporary/fifth.log")"
  [ "$(lifecycle_count stop-server)" = "1" ] || \
    die "owned stale backend did not trigger exactly one verified stop"
  [ "$(lifecycle_count start-server)" = "1" ] || \
    die "owned stale backend did not trigger exactly one replacement start"
  [ "$(sed -n '1p' "$temporary/state/fixture.pid")" != "$owned_pid" ] || \
    die "owned stale backend retained the same listener PID"
  "$SELF_PATH" status | grep -F 'backend=ready' >/dev/null || \
    die "replacement backend did not pass the complete compatibility fingerprint"
  [ "$(curl --noproxy '*' --connect-timeout 1 --max-time 2 -fsS \
    "http://127.0.0.1:$serverpod_port/stableRuntime/health")" = '"stable-ok"' ] || \
    die "new Experimental fixture dropped a Stable-supported route"
  for witness in /clientAuth/login /clientProfile/current /dashboardRuntime/health; do
    grep -F "POST $witness " "$temporary/state/requests.log" >/dev/null || \
      die "compatibility proof omitted required witness $witness"
  done

  # C5: both previous clients are gone; retrying attaches and never stops the
  # shared replacement.
  "$SELF_PATH" run "$$" >"$temporary/retry.log" 2>&1
  [ "$(lifecycle_count stop-server)" = "1" ] || \
    die "client close/retry stopped the replacement backend"
  [ "$(lifecycle_count start-server)" = "1" ] || \
    die "client close/retry restarted the replacement backend"

  # A listener with the same stale behavior but no matching PID/start identity
  # in the lifecycle manifest must survive a failed-closed launch attempt.
  owned_pid="$(sed -n '1p' "$temporary/state/fixture.pid")"
  kill "$owned_pid" >/dev/null 2>&1 || true
  for _ in {1..40}; do
    [ -z "$(listener_pids "$dashboard_port")" ] && break
    sleep 0.1
  done
  [ -z "$(listener_pids "$dashboard_port")" ] || \
    die "compatible fixture did not stop before foreign-listener test"
  printf 'stale\n' >"$temporary/state/fixture.mode"
  nohup python3 "$fixture_server" \
    "$dashboard_port" "$bridge_port" "$serverpod_port" "$web_port" \
    "$temporary/state/fixture.mode" "$temporary/state/requests.log" \
    >"$temporary/state/foreign.log" 2>&1 </dev/null &
  foreign_pid=$!
  printf '%s\n' "$foreign_pid" >"$temporary/state/foreign.pid"
  for _ in {1..40}; do
    endpoint_ready "http://127.0.0.1:$serverpod_port/livez" && break
    sleep 0.1
  done
  foreign_status=0
  "$SELF_PATH" run "$$" >"$temporary/foreign-run.log" 2>&1 || foreign_status=$?
  [ "$foreign_status" -ne 0 ] || die "unowned stale backend was unexpectedly replaced"
  grep -F 'unowned or PID-reused process; refusing to stop it' \
    "$temporary/foreign-run.log" >/dev/null || \
    die "unowned stale backend failure was not actionable"
  kill -0 "$foreign_pid" 2>/dev/null || \
    die "unowned stale backend listener was killed"
  [ "$(lifecycle_count stop-server)" = "1" ] || \
    die "unowned stale listener triggered a lifecycle stop"
  [ "$(lifecycle_count start-server)" = "1" ] || \
    die "unowned stale listener triggered a lifecycle start"

  printf '%s\n' \
    'self-test: PASS' \
    '  compatible backend is a no-op' \
    '  stale owned listener reconciles once for concurrent clients' \
    '  stale unowned listener is never stopped' \
    '  client close and retry preserve the shared compatible listener' \
    '  new Experimental fingerprint preserves Stable-supported routes' \
    '  all three Serverpod witnesses reject Endpoint not found'
  cleanup_backend_self_test
  trap cleanup_on_exit EXIT
  trap 'exit 130' HUP INT TERM
}

case "$COMMAND" in
  run)
    run_backend "$@"
    ;;
  status)
    [ "$#" -eq 0 ] || die "status takes no arguments"
    status_backend
    ;;
  self-test)
    [ "$#" -eq 0 ] || die "self-test takes no arguments"
    self_test
    ;;
  -h|--help|help|'')
    usage
    ;;
  *)
    usage >&2
    die "unknown command: $COMMAND"
    ;;
esac
