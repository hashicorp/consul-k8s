"""UpstreamWatcher - Phase I discovery primitive.

Watches the Consul Envoy sidecar admin interface for the set of policy-allowed
upstreams and emits :class:`~consul_sam_kit.types.UpstreamEvent` callbacks on
change. Discovery scope is defined by Consul intentions; the sidecar only
resolves upstreams the caller is permitted to reach (RFC section 8).

The watcher polls Envoy's admin ``/clusters?format=json`` endpoint by default,
which reports resolved endpoint addresses per cluster. Callback exceptions are
isolated so a faulty handler never kills the watch loop (RFC section 13).
"""

from __future__ import annotations

import json
import threading
import urllib.request
from typing import Callable, Dict, Iterable, List, Optional

from ._logging import get_logger
from .types import Upstream, UpstreamEvent, UpstreamEventType

_LOG = get_logger("upstream_watcher")

UpstreamEventHandler = Callable[[UpstreamEvent], None]

# Envoy synthetic/self clusters that are never application upstreams.
_DEFAULT_IGNORED_PREFIXES = (
    "local_agent",
    "local_app",
    "self_admin",
    "prometheus_backend",
    "original-destination",
)


class UpstreamWatcher:
    """Watch Envoy sidecar upstreams and emit upstream events.

    Args:
        host: Envoy admin host. Defaults to ``localhost``.
        port: Envoy admin port. Defaults to ``19000``.
        poll_interval: Seconds between polls. Defaults to ``5.0``.
        path: Admin endpoint queried for cluster endpoints.
        ignored_prefixes: Cluster name prefixes to skip (Envoy internals).
        timeout: Per-request HTTP timeout in seconds.
    """

    def __init__(
        self,
        host: str = "localhost",
        port: int = 19000,
        poll_interval: float = 5.0,
        path: str = "/clusters?format=json",
        ignored_prefixes: Iterable[str] = _DEFAULT_IGNORED_PREFIXES,
        timeout: float = 3.0,
    ) -> None:
        self._host = host
        self._port = port
        self._poll_interval = poll_interval
        self._path = path if path.startswith("/") else f"/{path}"
        self._ignored_prefixes = tuple(ignored_prefixes)
        self._timeout = timeout

        self._handlers: List[UpstreamEventHandler] = []
        self._snapshot: Dict[str, Upstream] = {}
        self._thread: Optional[threading.Thread] = None
        self._stop = threading.Event()

    # ---- public identity -------------------------------------------------- #
    @property
    def name(self) -> str:
        return f"UpstreamWatcher({self._host}:{self._port})"

    @property
    def admin_url(self) -> str:
        return f"http://{self._host}:{self._port}{self._path}"

    # ---- registration ----------------------------------------------------- #
    def on_upstream_event(self, fn: UpstreamEventHandler) -> UpstreamEventHandler:
        """Register a callback for upstream changes. Usable as a decorator."""
        self._handlers.append(fn)
        return fn

    # ---- lifecycle -------------------------------------------------------- #
    def start_in_background(self) -> None:
        """Start the polling loop in a daemon thread."""
        if self._thread is not None and self._thread.is_alive():
            return
        self._stop.clear()
        self._thread = threading.Thread(
            target=self._run, name="upstream-watcher", daemon=True
        )
        self._thread.start()
        _LOG.info("started %s polling %s", self.name, self.admin_url)

    def stop_background(self) -> None:
        """Signal the loop to stop and wait briefly for the thread to exit."""
        self._stop.set()
        thread = self._thread
        if thread is not None:
            thread.join(timeout=self._timeout + 1.0)
        self._thread = None
        _LOG.info("stopped %s", self.name)

    # ---- internals -------------------------------------------------------- #
    def _run(self) -> None:
        while not self._stop.is_set():
            try:
                current = self._fetch_upstreams()
                self._emit_diff(current)
            except Exception as exc:  # network/parse errors must not kill loop
                _LOG.warning("upstream poll failed: %s", exc)
            # Interruptible sleep so shutdown is prompt.
            self._stop.wait(self._poll_interval)

    def poll_once(self) -> List[UpstreamEvent]:
        """Poll a single time and return the events produced (used in tests)."""
        current = self._fetch_upstreams()
        return self._emit_diff(current)

    def _fetch_upstreams(self) -> Dict[str, Upstream]:
        with urllib.request.urlopen(self.admin_url, timeout=self._timeout) as resp:
            raw = resp.read().decode("utf-8")
        return self._parse_clusters(raw)

    def _parse_clusters(self, raw: str) -> Dict[str, Upstream]:
        data = json.loads(raw)
        result: Dict[str, Upstream] = {}
        for cluster in data.get("cluster_statuses", []):
            name = cluster.get("name", "")
            if not name or name.startswith(self._ignored_prefixes):
                continue
            host_statuses = cluster.get("host_statuses") or []
            if not host_statuses:
                continue
            sock = (host_statuses[0].get("address") or {}).get("socket_address") or {}
            address = sock.get("address")
            port = sock.get("port_value")
            if address is None or port is None:
                continue
            result[name] = Upstream(
                name=name,
                address=str(address),
                port=int(port),
                metadata={"endpoints": str(len(host_statuses))},
            )
        return result

    def _emit_diff(self, current: Dict[str, Upstream]) -> List[UpstreamEvent]:
        events: List[UpstreamEvent] = []
        previous = self._snapshot

        for key, upstream in current.items():
            if key not in previous:
                events.append(UpstreamEvent(UpstreamEventType.ADDED, upstream))
            elif previous[key] != upstream:
                events.append(
                    UpstreamEvent(UpstreamEventType.UPDATED, upstream, previous[key])
                )

        for key, upstream in previous.items():
            if key not in current:
                events.append(UpstreamEvent(UpstreamEventType.REMOVED, upstream))

        self._snapshot = current
        for event in events:
            self._dispatch(event)
        return events

    def _dispatch(self, event: UpstreamEvent) -> None:
        for handler in self._handlers:
            try:
                handler(event)
            except Exception as exc:  # isolate handler faults from the loop
                _LOG.warning("upstream handler raised: %s", exc)
