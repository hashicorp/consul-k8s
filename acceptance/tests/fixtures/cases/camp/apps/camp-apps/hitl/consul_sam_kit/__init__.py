"""consul-sam-kit - Consul ServiceAgent MeshKit.

One import to build governed, discoverable, human-gated service-agents on the
Consul mesh. The public API surface follows RFC SAMKIT-RFC-0001-v1.0
(Appendix A).

Phase I (required baseline):
    UpstreamWatcher, HITLWatcher, CompositeWatcher

Phase II (optional convenience):
    CardServer
"""

from __future__ import annotations

from .card_server import CardServer
from .composite_watcher import CompositeWatcher
from .hitl_watcher import HITLWatcher
from .types import (
    AgentCard,
    ApprovalRequest,
    Decision,
    Upstream,
    UpstreamEvent,
    UpstreamEventType,
)
from .upstream_watcher import UpstreamWatcher

__version__ = "0.1.0"

__all__ = [
    # Discovery
    "UpstreamWatcher",
    "UpstreamEvent",
    "UpstreamEventType",
    "Upstream",
    # Governance
    "HITLWatcher",
    "ApprovalRequest",
    "Decision",
    # Composition
    "CompositeWatcher",
    # Optional metadata hosting
    "CardServer",
    "AgentCard",
    "__version__",
]
