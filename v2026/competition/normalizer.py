"""Finite-only mirror of sim-latency score_schema 1 normalization."""

from __future__ import annotations

import math


def _positive_finite(name: str, value: float) -> float:
    value = float(value)
    if not math.isfinite(value) or value <= 0:
        raise ValueError(f"{name} must be finite and positive")
    return value


def normalize_score(baseline_raw_score: float, candidate_raw_score: float) -> float:
    baseline = _positive_finite("baseline_raw_score", baseline_raw_score)
    candidate = _positive_finite("candidate_raw_score", candidate_raw_score)
    return min(200.0, max(1.0, 100.0 * baseline / candidate))


def takeover_threshold(baseline_raw_score: float, takeover_margin: float) -> float:
    baseline = _positive_finite("baseline_raw_score", baseline_raw_score)
    margin = float(takeover_margin)
    if not math.isfinite(margin) or not 0 < margin <= 0.5:
        raise ValueError("takeover_margin must be finite and in (0, 0.5]")
    return baseline * (1.0 - margin)


def takeover_eligible(
    baseline_raw_score: float,
    candidate_raw_score: float,
    takeover_margin: float,
    placeable: bool,
) -> bool:
    candidate = _positive_finite("candidate_raw_score", candidate_raw_score)
    return bool(placeable) and candidate <= takeover_threshold(baseline_raw_score, takeover_margin)
