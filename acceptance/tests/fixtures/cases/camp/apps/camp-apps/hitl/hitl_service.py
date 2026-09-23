"""HITL approver for the CAMP mesh, compatible with Envoy's ext_proc
`http_service` HITL filter.

CAMP wires the gateway's HITL as an Envoy `ext_proc` filter in **http_service**
mode: for a tool call marked `x-mcp-hitl`, Envoy POSTs to
`http://127.0.0.1:<port>/` (path `/`) and treats the HTTP status as the verdict:

    HTTP 200  -> APPROVED  (Envoy continues the tool call)
    non-200   -> the ext_proc filter fails closed

This server listens on 127.0.0.1:<HITL_PORT> and answers `POST /` with 200
(approve) or a non-200 (reject), driven by HITL_DECISION. It also exposes a
`/approvals` endpoint that speaks the sam-kit ApprovalRequest contract (for the
alternative interceptor-driven model), and `/healthz`.

Every request and response — including ALL request and response headers — is
logged, each line prefixed with [*****consul-ai-hitl*****].
"""

from __future__ import annotations

import json
import os
import random
import signal
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

# Vendored consul_sam_kit lives next to this file (used for /approvals only).
sys.path.insert(0, str(Path(__file__).resolve().parent))
try:
    from consul_sam_kit import ApprovalRequest, Decision  # noqa: F401
    _HAVE_SAMKIT = True
except Exception as _exc:  # pragma: no cover - sam-kit optional
    _HAVE_SAMKIT = False
    _SAMKIT_IMPORT_ERR = _exc

LOG_PREFIX = "[*****consul-ai-hitl*****]"
HITL_PORT = int(os.environ.get("HITL_PORT", "16101"))
HITL_HOST = os.environ.get("HITL_HOST", "127.0.0.1")
# Headless verdict for the Envoy ext_proc http_service path (approve|reject).
DECISION_MODE = os.environ.get("HITL_DECISION", "approve").lower()
# Fraction of tool calls to randomly REJECT at the request-headers decision
# phase (0.0-1.0). Simulates a human denying approval. Default 25%.
try:
    HITL_FAIL_RATE = max(0.0, min(1.0, float(os.environ.get("HITL_FAIL_RATE", "0.25"))))
except ValueError:
    HITL_FAIL_RATE = 0.25

HITL_FAIL_RATE = 0.0


def log(msg: str) -> None:
    ts = time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime())
    print(f"{LOG_PREFIX} {ts} {msg}", flush=True)


class Handler(BaseHTTPRequestHandler):
    server_version = "consul-ai-hitl/1.0"

    # ---- helpers -----------------------------------------------------------
    def _read_body(self) -> bytes:
        te = (self.headers.get("transfer-encoding") or "").lower()
        if "chunked" in te:
            data = b""
            while True:
                line = self.rfile.readline()
                if not line:  # EOF
                    break
                line = line.strip()
                if line == b"":
                    continue
                try:
                    size = int(line.split(b";")[0], 16)
                except ValueError:
                    break
                if size == 0:
                    self.rfile.readline()  # trailing CRLF
                    break
                data += self.rfile.read(size)
                self.rfile.readline()  # CRLF after chunk
            return data
        n = int(self.headers.get("content-length", "0") or 0)
        return self.rfile.read(n) if n else b""

    def _log_request_headers(self, body: bytes) -> None:
        log(f"--> {self.command} {self.path} from {self.client_address[0]}:{self.client_address[1]}")
        log(f"    request: {len(self.headers)} header(s):")
        for name, value in self.headers.items():
            log(f"      req  header {name}: {value}")
        if body:
            log(f"    request body ({len(body)} bytes): {body[:1000].decode('latin1')!r}")
        else:
            log("    request body: (empty)")

    def _send(self, code: int, body: bytes, headers: dict[str, str]) -> None:
        self.send_response(code)
        # Merge in content-length/type and log every outgoing header.
        out = dict(headers)
        out.setdefault("content-length", str(len(body)))
        log(f"<-- {code} {self.command} {self.path} -> {len(out)} response header(s):")
        for name, value in out.items():
            self.send_header(name, value)
            log(f"      resp header {name}: {value}")
        self.end_headers()
        if body:
            self.wfile.write(body)
        log(f"    response body ({len(body)} bytes): {body[:1000].decode('latin1')!r}")

    # ---- HTTP verbs --------------------------------------------------------
    def do_GET(self):  # noqa: N802
        body = self._read_body()
        self._log_request_headers(body)
        if self.path.split("?", 1)[0].rstrip("/") in ("", "/healthz"):
            self._send(200, b'{"status":"ok"}', {"content-type": "application/json"})
        else:
            self._send(404, b'{"error":"not found"}', {"content-type": "application/json"})

    def do_POST(self):  # noqa: N802
        body = self._read_body()
        self._log_request_headers(body)
        path = self.path.split("?", 1)[0]

        # sam-kit interceptor-driven model: POST /approvals with an ApprovalRequest.
        if path.rstrip("/") == "/approvals":
            return self._handle_samkit_approval(body)

        # Envoy ext_proc http_service model: the body is a proto-JSON
        # ProcessingRequest. The HITL is invoked once per enabled phase
        # (request/response headers/body), and MUST reply with the matching
        # ProcessingResponse type (else Envoy fails with "spurious_message").
        # HTTP status is always 200; the verdict is carried in the body:
        #   request_headers + reject -> immediateResponse{403}
        #   otherwise               -> the matching phase response (continue).
        try:
            pr = json.loads(body.decode("utf-8")) if body else {}
        except Exception:
            pr = {}
        if any(k in pr for k in ("responseHeaders", "response_headers")):
            phase = "response_headers"
        elif any(k in pr for k in ("responseBody", "response_body")):
            phase = "response_body"
        elif any(k in pr for k in ("requestBody", "request_body")):
            phase = "request_body"
        else:
            phase = "request_headers"

        approve = DECISION_MODE == "approve"
        if phase == "request_headers":
            # Randomly reject a fraction of calls to simulate a human denial.
            roll = random.random()
            if approve and roll < HITL_FAIL_RATE:
                approve = False
                log(f"random denial: roll={roll:.3f} < HITL_FAIL_RATE={HITL_FAIL_RATE} -> REJECT")
            verdict = "APPROVE (requestHeaders continue)" if approve else "REJECT (immediate 403)"
            resp = (b'{"requestHeaders":{}}' if approve
                    else b'{"immediateResponse":{"status":{"code":403},"details":"denied by HITL"}}')
        elif phase == "response_headers":
            verdict = "continue (responseHeaders)"
            resp = b'{"responseHeaders":{}}'
        elif phase == "request_body":
            verdict = "continue (requestBody)"
            resp = b'{"requestBody":{}}'
        else:  # response_body
            verdict = "continue (responseBody)"
            resp = b'{"responseBody":{}}'
        log(f"ext_proc http_service phase={phase} verdict={verdict} "
            f"[headless HITL_DECISION={DECISION_MODE}]")
        self._send(200, resp, {"content-type": "application/json"})

    # ---- /approvals (sam-kit) ---------------------------------------------
    def _handle_samkit_approval(self, body: bytes) -> None:
        if not _HAVE_SAMKIT:
            log(f"/approvals: sam-kit unavailable ({_SAMKIT_IMPORT_ERR}); fail-closed REJECT")
            self._send(403, b'{"decision":"REJECT"}', {"content-type": "application/json"})
            return
        try:
            payload = json.loads(body.decode("utf-8"))
            req = ApprovalRequest.from_json(payload)
        except Exception as exc:
            log(f"/approvals REJECT: malformed challenge ({exc})")
            self._send(403, b'{"decision":"REJECT"}', {"content-type": "application/json"})
            return
        log(f"/approvals challenge: tool={req.tool} dest={req.destination} "
            f"method={req.method} trace={req.trace_id} approval_id={req.approval_id}")
        if req.computed_arguments_hash() != req.arguments_hash:
            log(f"/approvals REJECT {req.tool}: arguments_hash mismatch "
                f"(got {req.arguments_hash}, computed {req.computed_arguments_hash()})")
            self._send(403, b'{"decision":"REJECT"}', {"content-type": "application/json"})
            return
        approve = DECISION_MODE == "approve"
        log(f"/approvals {req.tool} -> {'APPROVE' if approve else 'REJECT'} "
            f"[headless HITL_DECISION={DECISION_MODE}]")
        self._send(
            200 if approve else 403,
            json.dumps({"decision": "APPROVE" if approve else "REJECT"}).encode(),
            {"content-type": "application/json"},
        )

    def log_message(self, *args):  # silence BaseHTTPRequestHandler default logging
        pass


def main() -> None:
    srv = ThreadingHTTPServer((HITL_HOST, HITL_PORT), Handler)
    log(f"starting HITL on {HITL_HOST}:{HITL_PORT}")
    log(f"  ext_proc http_service verdict endpoint: POST /  (200=APPROVE)")
    log(f"  sam-kit approval endpoint:               POST /approvals")
    log(f"  health endpoint:                         GET  /healthz")
    log(f"  headless decision: HITL_DECISION={DECISION_MODE}  sam-kit={'yes' if _HAVE_SAMKIT else 'no'}")
    stop = threading.Event()
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    signal.signal(signal.SIGTERM, lambda *_: (log("received signal; shutting down"), stop.set()))
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    log("HITL ready and serving")
    try:
        stop.wait()
    finally:
        srv.shutdown()
        log("HITL stopped")


if __name__ == "__main__":
    main()
