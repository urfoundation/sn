"""Reference client package for the URnetwork sim-latency competition."""

from .generator import CompetitionGenerator
from .normalizer import normalize_score, takeover_eligible
from .runner import CompetitionRunner

__all__ = [
    "CompetitionGenerator",
    "CompetitionRunner",
    "normalize_score",
    "takeover_eligible",
]
