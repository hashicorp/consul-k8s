"""Internal logging helpers for consul-sam-kit.

The library never configures the root logger for the host application; it only
obtains named child loggers under the ``consul_sam_kit`` namespace so callers
can control formatting and levels.
"""

from __future__ import annotations

import logging


def get_logger(name: str) -> logging.Logger:
    """Return a namespaced logger for a library submodule."""
    logger = logging.getLogger(f"consul_sam_kit.{name}")
    # Avoid "No handlers could be found" warnings on older Pythons without
    # forcing a configuration on the host application.
    if not logger.handlers:
        logger.addHandler(logging.NullHandler())
    return logger
