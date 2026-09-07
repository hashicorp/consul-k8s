"""HITLWatcher - Phase I human-in-the-loop approval gate (fail-closed).

Hosts a loopback approval endpoint (``POST /approvals``) that the sidecar
interceptor calls for critical operations. The gate is fail-closed: only an
explicit, valid, positive :class:`~consul_sam_kit.types.Decision.APPROVE`
allows a critical operation to proceed. Every other outcome - reject, handler
error, timeout, malformed payload, or no registered handler - denies the call
(RFC section 9).

Approved responses return signed approval evidence bound to the interceptor's
challenge claims (RFC section 9.1).
"""

from __future__ import annotations

import json
import threading
import time
from concurrent.futures import Future, ThreadPoolExecutor, TimeoutError as FutureTimeout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable, Dict, Optional

from ._jwt import sign_hs256
from ._logging import get_logger
from .types import ApprovalRequest, Decision

_LOG = get_logger("hitl_watcher")

ApprovalHandler = Callable[[ApprovalRequest], Decision]
TokenSigner = Callable[[ApprovalRequest], str]

_APPROVALS_PATH = "/approvals"
_HEALTH_PATH = "/healthz"


class HITLWatcher:
    """Host the loopback HITL approval endpoint for critical operations.

    Args:
        host: Bind host. Must remain loopback (``127.0.0.1``) in production.
        port: Bind port. Defaults to ``16101``.
        approval_timeout: Seconds to wait for the handler before failing closed.
        agent: Agent identity used in log/audit lines.
        signing_key: Optional HS256 key to mint approval tokens. If neither this
            nor ``token_signer`` is provided, approvals return an empty token and
            a warning is logged (the interceptor will then fail closed).
        token_signer: Optional custom callable that returns a signed token for a
            request (e.g. RS256 via a JOSE library). Overrides ``signing_key``.
        token_ttl: Approval token lifetime in seconds. Defaults to
            ``approval_timeout``.
        issuer: ``iss`` claim identifying the trusted approval authority.
    """

    def __init__(
        self,
        host: str = "127.0.0.1",
        port: int = 16101,
        approval_timeout: float = 120.0,
        agent: str = "serviceagent",
        signing_key: Optional[str] = None,
        token_signer: Optional[TokenSigner] = None,
        token_ttl: Optional[float] = None,
        issuer: str = "consul-sam-kit-hitl",
    ) -> None:
        self._host = host
        self._port = port
        self._approval_timeout = approval_timeout
        self._agent = agent
        self._signing_key = signing_key
        self._token_signer = token_signer
        self._token_ttl = token_ttl if token_ttl is not None else approval_timeout
        self._issuer = issuer

        self._handler: Optional[ApprovalHandler] = None
        self._server: Optional[ThreadingHTTPServer] = None
        self._server_thread: Optional[threading.Thread] = None
        self._executor: Optional[ThreadPoolExecutor] = None

        # Drain / shutdown coordination.
        self._reject_new = False
        self._inflight = 0
        self._inflight_lock = threading.Lock()
        self._inflight_zero = threading.Condition(self._inflight_lock)

    # ---- public identity -------------------------------------------------- #
    @property
    def name(self) -> str:
        return f"HITLWatcher({self._host}:{self._port})"

    @property
    def bound_port(self) -> int:
        """Actual bound port (useful when constructed with port ``0``)."""
        if self._server is not None:
            return self._server.server_address[1]
        return self._port

    # ---- registration ----------------------------------------------------- #
    def on_approval_request(self, fn: ApprovalHandler) -> ApprovalHandler:
        """Register the approval handler. Usable as a decorator."""
        self._handler = fn
        return fn

    # ---- lifecycle -------------------------------------------------------- #
    def start_in_background(self) -> None:
        """Start the HITL HTTP server in a background thread."""
        if self._server is not None:
            return
        self._reject_new = False
        self._executor = ThreadPoolExecutor(
            max_workers=16, thread_name_prefix="hitl-handler"
        )
        watcher = self

        class _Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args: Any) -> None:  # silence default logging
                pass

            def do_GET(self) -> None:  # noqa: N802 (stdlib naming)
                if self.path == _HEALTH_PATH:
                    watcher._write_json(self, 200, {"status": "ok"})
                else:
                    watcher._write_json(self, 404, {"error": "not found"})

            def do_POST(self) -> None:  # noqa: N802 (stdlib naming)
                if self.path.rstrip("/") != _APPROVALS_PATH:
                    watcher._write_json(self, 404, {"error": "not found"})
                    return
                watcher._handle_approval(self)

        self._server = ThreadingHTTPServer((self._host, self._port), _Handler)
        self._server_thread = threading.Thread(
            target=self._server.serve_forever,
            name="hitl-watcher",
            daemon=True,
        )
        self._server_thread.start()
        _LOG.info("started %s on %s:%d", self.name, self._host, self.bound_port)

    def stop_background(
        self,
        graceful: bool = False,
        drain_timeout: float = 60.0,
        reject_new: bool = True,
    ) -> None:
        """Stop the HITL server.

        When ``graceful`` is true, stop accepting new approvals (if
        ``reject_new``) and allow already-accepted in-flight approvals up to
        ``drain_timeout`` seconds to resolve before closing the listener
        (RFC section 13.1). In-flight approvals that exceed the drain window are
        rejected fail-closed by the timeout logic already in place.
        """
        if self._server is None:
            return

        if graceful:
            if reject_new:
                self._reject_new = True
            deadline = time.monotonic() + drain_timeout
            with self._inflight_lock:
                while self._inflight > 0 and time.monotonic() < deadline:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        break
                    self._inflight_zero.wait(timeout=remaining)
            _LOG.info(
                "%s drain complete, in-flight remaining=%d", self.name, self._inflight
            )

        self._server.shutdown()
        self._server.server_close()
        if self._server_thread is not None:
            self._server_thread.join(timeout=5.0)
        if self._executor is not None:
            self._executor.shutdown(wait=False, cancel_futures=True)
        self._server = None
        self._server_thread = None
        self._executor = None
        _LOG.info("stopped %s", self.name)

    # ---- request handling ------------------------------------------------- #
    def _handle_approval(self, http_handler: BaseHTTPRequestHandler) -> None:
        # 1. Reject new requests during graceful drain (fail-closed).
        if self._reject_new:
            self._deny(http_handler, "draining")
            return

        # 2. No handler registered -> fail closed.
        if self._handler is None:
            self._deny(http_handler, "no approval handler registered")
            return

        # 3. Parse + validate payload (malformed -> fail closed).
        try:
            length = int(http_handler.headers.get("Content-Length", "0"))
            body = http_handler.rfile.read(length) if length > 0 else b""
            payload = json.loads(body.decode("utf-8"))
            request = ApprovalRequest.from_json(payload)
        except Exception as exc:
            self._deny(http_handler, f"malformed approval request: {exc}")
            return

        # 4. Run the handler with a fail-closed timeout, tracking in-flight.
        self._enter_inflight()
        try:
            decision = self._run_handler(request)
        except FutureTimeout:
            self._deny(http_handler, f"approval timed out for {request.tool}")
            return
        except Exception as exc:
            self._deny(http_handler, f"approval handler error: {exc}")
            return
        finally:
            self._exit_inflight()

        # 5. Only an explicit APPROVE allows the operation.
        if decision is Decision.APPROVE:
            token = self._mint_token(request)
            self._write_json(
                http_handler,
                200,
                {"decision": Decision.APPROVE.value, "approval_token": token},
            )
            _LOG.info(
                "APPROVE tool=%s dest=%s trace=%s",
                request.tool,
                request.destination,
                request.trace_id,
            )
        else:
            self._deny(http_handler, f"reviewer rejected {request.tool}", request)

    def _run_handler(self, request: ApprovalRequest) -> Decision:
        assert self._handler is not None
        executor = self._executor
        if executor is None:
            # Fallback: run inline (should not happen while server is up).
            return self._handler(request)
        future: Future = executor.submit(self._handler, request)
        return future.result(timeout=self._approval_timeout)

    def _mint_token(self, request: ApprovalRequest) -> str:
        if self._token_signer is not None:
            try:
                return self._token_signer(request)
            except Exception as exc:  # signing failure must not fail open
                _LOG.error("token_signer failed: %s", exc)
                return ""
        if self._signing_key is None:
            _LOG.warning(
                "approval granted without signing key; interceptor will fail closed"
            )
            return ""
        now = int(time.time())
        claims: Dict[str, Any] = {
            "iss": self._issuer,
            "iat": now,
            "exp": now + int(self._token_ttl),
            "decision": Decision.APPROVE.value,
            "trace_id": request.trace_id,
            "approval_id": request.approval_id,
            "nonce": request.nonce,
            "agent": request.agent,
            "tool": request.tool,
            "method": request.method,
            "destination": request.destination,
            "arguments_hash": request.arguments_hash,
        }
        return sign_hs256(claims, self._signing_key, kid=self._issuer)

    # ---- helpers ---------------------------------------------------------- #
    def _enter_inflight(self) -> None:
        with self._inflight_lock:
            self._inflight += 1

    def _exit_inflight(self) -> None:
        with self._inflight_lock:
            self._inflight -= 1
            if self._inflight <= 0:
                self._inflight_zero.notify_all()

    def _deny(
        self,
        http_handler: BaseHTTPRequestHandler,
        reason: str,
        request: Optional[ApprovalRequest] = None,
    ) -> None:
        tool = request.tool if request is not None else "unknown"
        trace = request.trace_id if request is not None else "-"
        _LOG.warning("DENY tool=%s trace=%s reason=%s", tool, trace, reason)
        self._write_json(
            http_handler, 403, {"decision": Decision.REJECT.value, "reason": reason}
        )

    @staticmethod
    def _write_json(
        http_handler: BaseHTTPRequestHandler, status: int, body: Dict[str, Any]
    ) -> None:
        data = json.dumps(body).encode("utf-8")
        http_handler.send_response(status)
        http_handler.send_header("Content-Type", "application/json")
        http_handler.send_header("Content-Length", str(len(data)))
        http_handler.end_headers()
        http_handler.wfile.write(data)
