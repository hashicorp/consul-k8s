"""CardServer - Phase II optional A2A card hosting helper.

Serves an A2A-compatible agent card as JSON at ``/.well-known/agent.json`` with
minimal boilerplate. Card hosting is optional (RFC section 6); customers may
continue to host their own card endpoints instead. Card content is metadata
only - governance policy stays control-plane driven.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Dict, Optional, Union

from ._logging import get_logger
from .types import AgentCard

_LOG = get_logger("card_server")

_WELL_KNOWN = "/.well-known/agent.json"
_COMPAT_PATHS = ("/agent.json", "/.well-known/agent-card.json")
_HEALTH_PATH = "/healthz"


class CardServer:
    """Host an A2A agent card over HTTP.

    Args:
        card: An :class:`~consul_sam_kit.types.AgentCard` or a pre-built dict.
        host: Bind host. Defaults to ``0.0.0.0`` so peers can fetch the card.
        port: Bind port. Defaults to ``9090`` (the service port).
    """

    def __init__(
        self,
        card: Union[AgentCard, Dict[str, Any]],
        host: str = "0.0.0.0",
        port: int = 9090,
    ) -> None:
        self._card_dict = card.to_dict() if isinstance(card, AgentCard) else dict(card)
        self._host = host
        self._port = port
        self._server: Optional[ThreadingHTTPServer] = None
        self._thread: Optional[threading.Thread] = None

    @property
    def name(self) -> str:
        return f"CardServer({self._host}:{self._port})"

    @property
    def bound_port(self) -> int:
        if self._server is not None:
            return self._server.server_address[1]
        return self._port

    def _build_server(self) -> ThreadingHTTPServer:
        card_bytes = json.dumps(self._card_dict, indent=2).encode("utf-8")

        class _Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args: Any) -> None:
                pass

            def do_GET(self) -> None:  # noqa: N802 (stdlib naming)
                path = self.path.split("?", 1)[0]
                if path == _HEALTH_PATH:
                    self._respond(200, b'{"status":"ok"}')
                elif path == _WELL_KNOWN or path in _COMPAT_PATHS:
                    self._respond(200, card_bytes)
                else:
                    self._respond(404, b'{"error":"not found"}')

            def _respond(self, status: int, body: bytes) -> None:
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        return ThreadingHTTPServer((self._host, self._port), _Handler)

    def start_in_background(self) -> None:
        """Start the card server in a background daemon thread."""
        if self._server is not None:
            return
        self._server = self._build_server()
        self._thread = threading.Thread(
            target=self._server.serve_forever, name="card-server", daemon=True
        )
        self._thread.start()
        _LOG.info("started %s serving %s", self.name, _WELL_KNOWN)

    def stop_background(self) -> None:
        """Stop the card server."""
        if self._server is None:
            return
        self._server.shutdown()
        self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=5.0)
        self._server = None
        self._thread = None
        _LOG.info("stopped %s", self.name)

    def serve(self) -> None:
        """Start the server and block the calling thread until interrupted."""
        self.start_in_background()
        try:
            if self._thread is not None:
                while self._thread.is_alive():
                    self._thread.join(timeout=1.0)
        except KeyboardInterrupt:
            pass
        finally:
            self.stop_background()
