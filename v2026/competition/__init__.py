"""Reference client package for the URnetwork sim-latency competition."""

from .generator import CompetitionGenerator
from .normalizer import normalize_score, takeover_eligible
from .runner import CompetitionRunner
from .models import SeasonLeaderboard

__all__ = [
    "CompetitionGenerator",
    "CompetitionRunner",
    "SeasonLeaderboard",
    "normalize_score",
    "takeover_eligible",
]
