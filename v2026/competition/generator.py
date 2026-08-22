"""Operator-side hidden-round generator client."""

from __future__ import annotations

from datetime import datetime, timezone

from .client import CompetitionClient
from .models import RoundCommitment


class CompetitionGenerator:
    def __init__(self, base_url: str, operator_token: str, timeout_seconds: float = 30.0):
        self.client = CompetitionClient(base_url, operator_token, timeout_seconds)

    def generate(self, opens_at: datetime, closes_at: datetime, reveal_at: datetime) -> RoundCommitment:
        values = [opens_at, closes_at, reveal_at]
        if any(value.tzinfo is None or value.utcoffset() is None for value in values):
            raise ValueError("round timestamps must be timezone-aware")
        if not opens_at < closes_at <= reveal_at:
            raise ValueError("require opens_at < closes_at <= reveal_at")
        response = self.client.post(
            "/competition/generate-round",
            {
                "opens_at": _rfc3339(opens_at),
                "closes_at": _rfc3339(closes_at),
                "reveal_at": _rfc3339(reveal_at),
            },
        )
        return RoundCommitment.from_json(response)


def _rfc3339(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")
