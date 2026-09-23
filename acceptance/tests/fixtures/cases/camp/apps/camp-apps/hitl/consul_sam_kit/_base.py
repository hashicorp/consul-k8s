"""Watcher lifecycle protocol shared by all background watchers."""

from __future__ import annotations

from typing import Protocol, runtime_checkable


@runtime_checkable
class Watcher(Protocol):
    """A background component with a uniform start/stop lifecycle.

    :class:`~consul_sam_kit.composite_watcher.CompositeWatcher` starts watchers
    in registration order and stops them in reverse order.
    """

    @property
    def name(self) -> str:
        """Human-readable identifier used in lifecycle logs."""

    def start_in_background(self) -> None:
        """Start the watcher loop/server without blocking the caller."""

    def stop_background(self) -> None:
        """Stop the watcher and release resources."""
