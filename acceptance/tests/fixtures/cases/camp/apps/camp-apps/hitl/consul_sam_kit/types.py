"""Common taxonomy and schema for consul-sam-kit.

These types implement the language-neutral wire schema described in the RFC
(section 7). The JSON wire shape is consistent across languages; only the local
naming style is Python-idiomatic here.
"""

from __future__ import annotations

import enum
import hashlib
import json
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


# --------------------------------------------------------------------------- #
# Discovery types
# --------------------------------------------------------------------------- #
@dataclass(frozen=True)
class Upstream:
    """A policy-allowed upstream service resolved by the Consul sidecar.

    Attributes:
        name: Logical upstream/cluster name as reported by the sidecar.
        address: Resolved network address (host or IP).
        port: Resolved port.
        metadata: Arbitrary key/value metadata attached to the endpoint.
    """

    name: str
    address: str
    port: int
    metadata: Dict[str, str] = field(default_factory=dict)

    def key(self) -> str:
        """Stable identity used to diff snapshots across polls."""
        return self.name


class UpstreamEventType(str, enum.Enum):
    """Lifecycle transitions emitted by :class:`UpstreamWatcher`."""

    ADDED = "ADDED"
    UPDATED = "UPDATED"
    REMOVED = "REMOVED"


@dataclass(frozen=True)
class UpstreamEvent:
    """Event describing a change to the resolved upstream set.

    Attributes:
        event_type: One of ADDED, UPDATED or REMOVED.
        upstream: The current upstream state (for REMOVED this is the last
            known state).
        previous: The prior upstream state for UPDATED/REMOVED, else ``None``.
    """

    event_type: UpstreamEventType
    upstream: Upstream
    previous: Optional[Upstream] = None


# --------------------------------------------------------------------------- #
# Governance / HITL types
# --------------------------------------------------------------------------- #
class Decision(str, enum.Enum):
    """Human-in-the-loop verdict for a critical operation."""

    APPROVE = "APPROVE"
    REJECT = "REJECT"


@dataclass(frozen=True)
class ApprovalRequest:
    """The HITL POST body received from the sidecar interceptor.

    Mirrors the wire shape in RFC section 10. ``arguments`` is untrusted data
    and must be treated as such (RFC section 14.6).
    """

    trace_id: str
    approval_id: str
    nonce: str
    agent: str
    tool: str
    method: str
    arguments: Dict[str, Any]
    arguments_hash: str
    destination: str
    expires_at: str
    impact: Optional[str] = None

    @classmethod
    def from_json(cls, payload: Dict[str, Any]) -> "ApprovalRequest":
        """Build an :class:`ApprovalRequest` from a decoded JSON object.

        Raises:
            ValueError: if a required field is missing.
        """
        required = (
            "trace_id",
            "approval_id",
            "nonce",
            "agent",
            "tool",
            "method",
            "arguments",
            "arguments_hash",
            "destination",
            "expires_at",
        )
        missing = [f for f in required if f not in payload]
        if missing:
            raise ValueError(f"malformed approval request, missing: {missing}")
        if not isinstance(payload["arguments"], dict):
            raise ValueError("approval request 'arguments' must be an object")
        return cls(
            trace_id=str(payload["trace_id"]),
            approval_id=str(payload["approval_id"]),
            nonce=str(payload["nonce"]),
            agent=str(payload["agent"]),
            tool=str(payload["tool"]),
            method=str(payload["method"]),
            arguments=dict(payload["arguments"]),
            arguments_hash=str(payload["arguments_hash"]),
            destination=str(payload["destination"]),
            expires_at=str(payload["expires_at"]),
            impact=payload.get("impact"),
        )

    def computed_arguments_hash(self) -> str:
        """Recompute a ``sha256:<hex>`` digest of the canonical arguments.

        Useful for the HITL service to independently verify the interceptor's
        ``arguments_hash`` before rendering arguments to a reviewer.
        """
        canonical = json.dumps(
            self.arguments, sort_keys=True, separators=(",", ":")
        ).encode("utf-8")
        return "sha256:" + hashlib.sha256(canonical).hexdigest()


# --------------------------------------------------------------------------- #
# A2A card type (optional, Phase II)
# --------------------------------------------------------------------------- #
@dataclass
class AgentCard:
    """A2A-compatible agent card metadata (RFC section 6).

    Only ``name``, ``description``, ``url`` and ``version`` are required; the
    rest default to sensible A2A-compatible values.
    """

    name: str
    description: str
    url: str
    version: str
    provider: Optional[Dict[str, str]] = None
    documentation_url: Optional[str] = None
    capabilities: Dict[str, bool] = field(default_factory=dict)
    authentication: Dict[str, Any] = field(
        default_factory=lambda: {"schemes": ["none"]}
    )
    default_input_modes: List[str] = field(default_factory=lambda: ["text"])
    default_output_modes: List[str] = field(default_factory=lambda: ["text"])
    skills: List[Dict[str, Any]] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        """Serialize to the A2A card JSON wire shape (camelCase keys)."""
        card: Dict[str, Any] = {
            "name": self.name,
            "description": self.description,
            "url": self.url,
            "version": self.version,
            "capabilities": self.capabilities,
            "authentication": self.authentication,
            "defaultInputModes": self.default_input_modes,
            "defaultOutputModes": self.default_output_modes,
            "skills": self.skills,
        }
        if self.provider is not None:
            card["provider"] = self.provider
        if self.documentation_url is not None:
            card["documentationUrl"] = self.documentation_url
        return card
