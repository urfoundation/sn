"""Post-reveal verification for archived rounds.

This module is intentionally separate from the active-round public models so
seed material cannot accidentally enter a live submission flow.
"""

from __future__ import annotations

import hashlib
import hmac
import uuid


ROUND_DOMAIN = b"urnetwork-sim-latency-round-v1\x00"
GENERATOR_DOMAIN = b"urnetwork-sim-latency-generator-v1\x00"


def verify_reveal(
    *,
    round_id: str,
    seed_hex: str,
    workload_commitment: str,
    providers: bytes,
    providers_sha256: str,
) -> int:
    """Authenticate the revealed seed/file and return the simulator int seed."""

    try:
        round_uuid = uuid.UUID(round_id)
        seed = bytes.fromhex(seed_hex)
    except (ValueError, TypeError):
        raise ValueError("round_id or seed is malformed") from None
    if str(round_uuid) != round_id.lower() or len(seed) != 32:
        raise ValueError("round_id or seed is malformed")
    for name, value in (
        ("workload_commitment", workload_commitment),
        ("providers_sha256", providers_sha256),
    ):
        if len(value) != 64 or any(ch not in "0123456789abcdef" for ch in value):
            raise ValueError(f"{name} must be 64 lowercase hex characters")

    actual_commitment = hashlib.sha256(ROUND_DOMAIN + round_uuid.bytes + seed).hexdigest()
    actual_providers = hashlib.sha256(providers).hexdigest()
    if not hmac.compare_digest(actual_commitment, workload_commitment):
        raise ValueError("revealed seed does not match the round commitment")
    if not hmac.compare_digest(actual_providers, providers_sha256):
        raise ValueError("revealed providers file does not match its published digest")

    derived = int.from_bytes(hashlib.sha256(GENERATOR_DOMAIN + seed).digest()[:8], "big")
    derived &= (1 << 63) - 1
    return derived or 1
