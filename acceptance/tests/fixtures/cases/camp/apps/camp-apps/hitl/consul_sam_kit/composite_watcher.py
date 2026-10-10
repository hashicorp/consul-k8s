"""CompositeWatcher - Phase I unified lifecycle for all watchers.

Provides one lifecycle for multiple background watchers. Watchers start in
registration order and stop in reverse order. Graceful-shutdown parameters are
forwarded to watchers that accept them (e.g. ``HITLWatcher``) and ignored by
those that do not (RFC section 13).
"""

from __future__ import annotations

import inspect
from typing import Any, List

from ._logging import get_logger

_LOG = get_logger("composite_watcher")


class CompositeWatcher:
    """Manage several watchers as a single unit.

    Args:
        *watchers: Watcher instances exposing ``start_in_background()`` and
            ``stop_background()``. Any object with that lifecycle works.
    """

    def __init__(self, *watchers: Any) -> None:
        self._watchers: List[Any] = list(watchers)
        self._started = False

    def add(self, watcher: Any) -> "CompositeWatcher":
        """Register an additional watcher before starting."""
        if self._started:
            raise RuntimeError("cannot add watchers after start_in_background()")
        self._watchers.append(watcher)
        return self

    def start_in_background(self) -> None:
        """Start all watchers in registration order.

        If a watcher fails to start, previously started watchers are stopped in
        reverse order before the error propagates.
        """
        started: List[Any] = []
        try:
            for watcher in self._watchers:
                watcher.start_in_background()
                started.append(watcher)
                _LOG.info("started %s", _label(watcher))
        except Exception:
            for watcher in reversed(started):
                _safe_stop(watcher)
            raise
        self._started = True

    def stop_background(
        self,
        graceful: bool = False,
        drain_timeout: float = 60.0,
        reject_new: bool = True,
    ) -> None:
        """Stop all watchers in reverse registration order.

        Graceful parameters are forwarded to any watcher whose
        ``stop_background`` accepts them (fail-closed drain for HITLWatcher).
        """
        for watcher in reversed(self._watchers):
            _safe_stop(
                watcher,
                graceful=graceful,
                drain_timeout=drain_timeout,
                reject_new=reject_new,
            )
            _LOG.info("stopped %s", _label(watcher))
        self._started = False

    # Allow use as a context manager for scoped lifecycles.
    def __enter__(self) -> "CompositeWatcher":
        self.start_in_background()
        return self

    def __exit__(self, exc_type: Any, exc: Any, tb: Any) -> None:
        self.stop_background()


def _label(watcher: Any) -> str:
    return getattr(watcher, "name", watcher.__class__.__name__)


def _supported_kwargs(fn: Any, candidate: dict) -> dict:
    """Return only the kwargs that ``fn`` actually accepts."""
    try:
        params = inspect.signature(fn).parameters
    except (TypeError, ValueError):
        return {}
    if any(p.kind is inspect.Parameter.VAR_KEYWORD for p in params.values()):
        return candidate
    return {k: v for k, v in candidate.items() if k in params}


def _safe_stop(watcher: Any, **kwargs: Any) -> None:
    """Stop a watcher, forwarding only supported kwargs, never raising."""
    stop = getattr(watcher, "stop_background", None)
    if stop is None:
        return
    try:
        stop(**_supported_kwargs(stop, kwargs))
    except Exception as exc:  # a failed stop must not block other watchers
        _LOG.warning("error stopping %s: %s", _label(watcher), exc)
