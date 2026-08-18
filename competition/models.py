"""Strict public models for active competition rounds and score jobs.

No workload seed is represented here. During an active round miners receive
the seed commitment and the exact generated providers-file digest. Reveal data
is deliberately consumed as an archive artifact, outside this active model.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Any, Mapping
import re
import uuid


ROUND_STATES = frozenset({"scheduled", "open", "closed", "revealed", "canceled"})
JOB_STATES = frozenset({"queued", "running", "succeeded", "failed", "canceled"})
ERROR_KINDS = frozenset({"auth", "submission", "infrastructure"})
ERROR_CODE = re.compile(r"^[a-z][a-z0-9_]{1,63}$")


@dataclass(frozen=True)
class CompetitionApiError(Exception):
    kind: str
    code: str
    message: str
    retriable: bool
    status: int | None = None

    def __str__(self) -> str:
        return f"{self.code}: {self.message}"

    @classmethod
    def from_json(cls, value: Mapping[str, Any], status: int | None = None) -> "CompetitionApiError":
        kind = _required_enum(value, "kind", ERROR_KINDS)
        code = _required_str(value, "code")
        message = _required_str(value, "message")
        if ERROR_CODE.fullmatch(code) is None:
            raise ValueError("code must match the competition error-code contract")
        if len(message) > 1024:
            raise ValueError("message exceeds the competition error-message limit")
        return cls(
            kind=kind,
            code=code,
            message=message,
            retriable=_required_bool(value, "retriable"),
            status=status,
        )


@dataclass(frozen=True)
class RoundCommitment:
    round_id: str
    status: str
    workload_commitment: str
    providers_sha256: str
    score_schema: int
    opens_at: str
    closes_at: str
    reveal_at: str
    created_at: str

    @classmethod
    def from_json(cls, value: Mapping[str, Any]) -> "RoundCommitment":
        if "revealed_seed" in value:
            raise ValueError("active-round model refuses seed material")
        score_schema = _required_int(value, "score_schema")
        if score_schema != 1:
            raise ValueError("score_schema must equal 1")
        opens_at = _required_datetime(value, "opens_at")
        closes_at = _required_datetime(value, "closes_at")
        reveal_at = _required_datetime(value, "reveal_at")
        _required_datetime(value, "created_at")
        if not opens_at < closes_at <= reveal_at:
            raise ValueError("round timestamps violate opens_at < closes_at <= reveal_at")
        return cls(
            round_id=_required_uuid(value, "round_id"),
            status=_required_enum(value, "status", ROUND_STATES),
            workload_commitment=_required_sha256(value, "workload_commitment"),
            providers_sha256=_required_sha256(value, "providers_sha256"),
            score_schema=score_schema,
            opens_at=_required_str(value, "opens_at"),
            closes_at=_required_str(value, "closes_at"),
            reveal_at=_required_str(value, "reveal_at"),
            created_at=_required_str(value, "created_at"),
        )


@dataclass(frozen=True)
class AcceptedScore:
    job_id: str
    round_id: str
    patch_sha256: str
    state: str
    cache_hit: bool
    status_url: str

    @classmethod
    def from_json(cls, value: Mapping[str, Any]) -> "AcceptedScore":
        job_id = _required_uuid(value, "job_id")
        status_url = _required_str(value, "status_url")
        if status_url != f"/competition/score/{job_id}":
            raise ValueError("status_url does not match job_id")
        return cls(
            job_id=job_id,
            round_id=_required_uuid(value, "round_id"),
            patch_sha256=_required_sha256(value, "patch_sha256"),
            state=_required_enum(value, "state", JOB_STATES),
            cache_hit=_required_bool(value, "cache_hit"),
            status_url=status_url,
        )


@dataclass(frozen=True)
class ScoreJob:
    job_id: str
    round_id: str
    patch_sha256: str
    state: str
    submitted_at: str
    cache_key: str
    score: Mapping[str, Any] | None
    eval_error: CompetitionApiError | None

    @classmethod
    def from_json(cls, value: Mapping[str, Any]) -> "ScoreJob":
        score = value.get("score")
        if score is not None and not isinstance(score, Mapping):
            raise ValueError("score must be an object or null")
        raw_error = value.get("eval_error")
        eval_error = None
        if raw_error is not None:
            if not isinstance(raw_error, Mapping):
                raise ValueError("eval_error must be an object or null")
            eval_error = CompetitionApiError.from_json(raw_error)
        return cls(
            job_id=_required_uuid(value, "job_id"),
            round_id=_required_uuid(value, "round_id"),
            patch_sha256=_required_sha256(value, "patch_sha256"),
            state=_required_enum(value, "state", JOB_STATES),
            submitted_at=_required_datetime_string(value, "submitted_at"),
            cache_key=_required_sha256(value, "cache_key"),
            score=score,
            eval_error=eval_error,
        )


def _required_str(value: Mapping[str, Any], key: str) -> str:
    result = value.get(key)
    if not isinstance(result, str) or not result:
        raise ValueError(f"{key} must be a nonempty string")
    return result


def _required_bool(value: Mapping[str, Any], key: str) -> bool:
    result = value.get(key)
    if not isinstance(result, bool):
        raise ValueError(f"{key} must be a boolean")
    return result


def _required_int(value: Mapping[str, Any], key: str) -> int:
    result = value.get(key)
    if not isinstance(result, int) or isinstance(result, bool):
        raise ValueError(f"{key} must be an integer")
    return result


def _required_enum(value: Mapping[str, Any], key: str, allowed: frozenset[str]) -> str:
    result = _required_str(value, key)
    if result not in allowed:
        raise ValueError(f"{key} is outside the allowed enum")
    return result


def _required_uuid(value: Mapping[str, Any], key: str) -> str:
    result = _required_str(value, key)
    try:
        canonical = str(uuid.UUID(result))
    except ValueError:
        raise ValueError(f"{key} must be a canonical UUID") from None
    if result != canonical:
        raise ValueError(f"{key} must be a canonical UUID")
    return result


def _required_sha256(value: Mapping[str, Any], key: str) -> str:
    result = _required_str(value, key)
    if len(result) != 64 or any(ch not in "0123456789abcdef" for ch in result):
        raise ValueError(f"{key} must be 64 lowercase hex characters")
    return result


def _required_datetime(value: Mapping[str, Any], key: str) -> datetime:
    raw = _required_str(value, key)
    try:
        parsed = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        raise ValueError(f"{key} must be an RFC3339 timestamp") from None
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError(f"{key} must include a timezone")
    return parsed


def _required_datetime_string(value: Mapping[str, Any], key: str) -> str:
    _required_datetime(value, key)
    return _required_str(value, key)
