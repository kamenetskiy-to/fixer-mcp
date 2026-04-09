from __future__ import annotations

import json
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

from .store import FixerDesktopStore


class DesktopBridgeHandler(BaseHTTPRequestHandler):
    store: FixerDesktopStore

    def do_GET(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        path = parsed.path.rstrip("/") or "/"
        try:
            if path == "/health":
                self._write_json(HTTPStatus.OK, self.store.health())
                return
            if path == "/api/projects":
                self._write_json(HTTPStatus.OK, {"projects": self.store.list_projects()})
                return
            if path.startswith("/api/projects/") and path.endswith("/dashboard"):
                project_id = int(path.split("/")[3])
                self._write_json(
                    HTTPStatus.OK,
                    self.store.get_project_dashboard(project_id),
                )
                return
            if path.startswith("/api/sessions/"):
                session_id = int(path.split("/")[3])
                self._write_json(HTTPStatus.OK, self.store.get_session_detail(session_id))
                return
        except KeyError as error:
            self._write_json(HTTPStatus.NOT_FOUND, {"error": str(error)})
            return
        except ValueError:
            self._write_json(HTTPStatus.BAD_REQUEST, {"error": f"invalid request path: {path}"})
            return
        self._write_json(HTTPStatus.NOT_FOUND, {"error": f"unknown route: {path}"})

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        return

    def _write_json(self, status: HTTPStatus, payload: object) -> None:
        body = json.dumps(payload, indent=2, sort_keys=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)


def serve(*, host: str, port: int, store: FixerDesktopStore) -> None:
    handler_type = type(
        "ConfiguredDesktopBridgeHandler",
        (DesktopBridgeHandler,),
        {"store": store},
    )
    with ThreadingHTTPServer((host, port), handler_type) as server:
        print(f"fixer-desktop-bridge listening on http://{host}:{port}", flush=True)
        print(f"using SQLite state at {store.db_path}", flush=True)
        server.serve_forever()
