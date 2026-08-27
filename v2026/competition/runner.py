"""Submit-and-poll runner that never retries a submission into a new identity."""

from __future__ import annotations

import pathlib
import time
from collections.abc import Callable

from .client import CompetitionClient
from .models import AcceptedScore, CompetitionApiError, ScoreJob


class CompetitionRunner:
    def __init__(
        self,
        base_url: str,
        submitter_token: str,
        request_timeout_seconds: float = 30.0,
        poll_interval_seconds: float = 2.0,
        monotonic: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
    ):
        if poll_interval_seconds <= 0:
            raise ValueError("poll_interval_seconds must be positive")
        self.client = CompetitionClient(base_url, submitter_token, request_timeout_seconds)
        self.poll_interval_seconds = poll_interval_seconds
        self._monotonic = monotonic
        self._sleep = sleep

    def submit_patch(self, round_id: str, patch: str) -> AcceptedScore:
        if not round_id or not patch:
            raise ValueError("round_id and patch are required")
        response = self.client.post("/competition/score", {"round_id": round_id, "patch": patch})
        return AcceptedScore.from_json(response)

    def submit_file(self, round_id: str, patch_path: str | pathlib.Path) -> AcceptedScore:
        path = pathlib.Path(patch_path)
        return self.submit_patch(round_id, path.read_text(encoding="utf-8"))

    def poll(self, accepted: AcceptedScore, timeout_seconds: float) -> ScoreJob:
        if timeout_seconds <= 0:
            raise ValueError("timeout_seconds must be positive")
        deadline = self._monotonic() + timeout_seconds
        while True:
            job = ScoreJob.from_json(self.client.get(accepted.status_url))
            if job.job_id != accepted.job_id or job.round_id != accepted.round_id or job.patch_sha256 != accepted.patch_sha256:
                raise CompetitionApiError(
                    "infrastructure", "job_identity_mismatch",
                    "poll response does not match the accepted job", True,
                )
            if job.state in {"succeeded", "failed", "canceled"}:
                return job
            remaining = deadline - self._monotonic()
            if remaining <= 0:
                raise TimeoutError(f"score job {job.job_id} did not complete before the polling deadline")
            self._sleep(min(self.poll_interval_seconds, remaining))

    def run(self, round_id: str, patch: str, timeout_seconds: float) -> ScoreJob:
        # A transport failure during submit is intentionally surfaced to the
        # caller. Blindly creating a second request can obscure whether the first
        # was durably accepted; the server cache makes an explicit caller retry
        # safe under the same round and canonical patch identity.
        return self.poll(self.submit_patch(round_id, patch), timeout_seconds)
