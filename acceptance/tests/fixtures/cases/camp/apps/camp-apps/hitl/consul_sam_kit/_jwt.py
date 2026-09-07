"""Minimal, dependency-free HS256 JWT support for signed approval evidence.

The RFC (section 9.1) requires the HITL approval service to return a signed
approval token bound to the interceptor challenge. This module provides a small
HS256 signer/verifier so the library has no third-party crypto dependency. In
production you may swap in a JOSE library and an asymmetric key by passing a
custom ``token_signer`` to :class:`~consul_sam_kit.hitl_watcher.HITLWatcher`.

Security note: HS256 is symmetric. The signing key must be shared only between
the approval authority and the interceptor that verifies the token.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import time
from typing import Any, Dict, Optional


def _b64url_encode(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def _b64url_decode(segment: str) -> bytes:
    padding = "=" * (-len(segment) % 4)
    return base64.urlsafe_b64decode(segment + padding)


def sign_hs256(claims: Dict[str, Any], key: str, kid: Optional[str] = None) -> str:
    """Return a compact HS256 JWT for ``claims`` signed with ``key``."""
    header: Dict[str, Any] = {"alg": "HS256", "typ": "JWT"}
    if kid:
        header["kid"] = kid
    header_seg = _b64url_encode(
        json.dumps(header, separators=(",", ":"), sort_keys=True).encode("utf-8")
    )
    payload_seg = _b64url_encode(
        json.dumps(claims, separators=(",", ":"), sort_keys=True).encode("utf-8")
    )
    signing_input = f"{header_seg}.{payload_seg}".encode("ascii")
    signature = hmac.new(key.encode("utf-8"), signing_input, hashlib.sha256).digest()
    return f"{header_seg}.{payload_seg}.{_b64url_encode(signature)}"


def verify_hs256(token: str, key: str) -> Dict[str, Any]:
    """Verify an HS256 JWT and return its claims.

    Raises:
        ValueError: if the token is malformed, the signature is invalid, or the
            token is expired.
    """
    try:
        header_seg, payload_seg, sig_seg = token.split(".")
    except ValueError as exc:  # not exactly three segments
        raise ValueError("malformed token") from exc

    signing_input = f"{header_seg}.{payload_seg}".encode("ascii")
    expected = hmac.new(
        key.encode("utf-8"), signing_input, hashlib.sha256
    ).digest()
    if not hmac.compare_digest(expected, _b64url_decode(sig_seg)):
        raise ValueError("invalid token signature")

    claims: Dict[str, Any] = json.loads(_b64url_decode(payload_seg))
    exp = claims.get("exp")
    if exp is not None and time.time() > float(exp):
        raise ValueError("token expired")
    return claims
