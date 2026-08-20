from __future__ import annotations

import hashlib
import io
import math
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from unittest import mock

from competition.client import CompetitionClient, MAX_JSON_RESPONSE_BYTES
from competition.generator import CompetitionGenerator
from competition.models import AcceptedScore, CompetitionApiError, RoundCommitment, ScoreJob
from competition.normalizer import normalize_score, takeover_eligible, takeover_threshold
from competition.reveal import GENERATOR_DOMAIN, ROUND_DOMAIN, verify_reveal
from competition.runner import CompetitionRunner
from competition.screener import screen_patch


VALID_PATCH = """diff --git a/connect/example.go b/connect/example.go
index 1111111..2222222 100644
--- a/connect/example.go
+++ b/connect/example.go
@@ -1 +1 @@
-old
+new
"""

ROUND_ID = "12345678-1234-1234-1234-123456789abc"
JOB_ID = "abcdefab-cdef-abcd-efab-cdefabcdefab"
PATCH_SHA256 = "a" * 64
CACHE_KEY = "b" * 64


def round_result(**overrides):
    value = {
        "round_id": ROUND_ID,
        "status": "scheduled",
        "workload_commitment": "c" * 64,
        "providers_sha256": "d" * 64,
        "score_schema": 1,
        "opens_at": "2026-08-17T09:00:00Z",
        "closes_at": "2026-08-17T10:00:00Z",
        "reveal_at": "2026-08-17T11:00:00Z",
        "created_at": "2026-08-17T08:00:00Z",
    }
    value.update(overrides)
    return value


def accepted_result(**overrides):
    value = {
        "job_id": JOB_ID,
        "round_id": ROUND_ID,
        "patch_sha256": PATCH_SHA256,
        "state": "queued",
        "cache_hit": False,
        "status_url": f"/competition/score/{JOB_ID}",
    }
    value.update(overrides)
    return value


def job_result(**overrides):
    value = {
        "job_id": JOB_ID,
        "round_id": ROUND_ID,
        "patch_sha256": PATCH_SHA256,
        "state": "queued",
        "submitted_at": "2026-08-17T08:00:00Z",
        "cache_key": CACHE_KEY,
        "score": None,
        "eval_error": None,
    }
    value.update(overrides)
    return value


class FakeClock:
    def __init__(self):
        self.now = 0.0
        self.sleeps = []

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.sleeps.append(seconds)
        self.now += seconds


class BytesResponse:
    def __init__(self, raw, headers=None):
        self.raw = raw
        self.headers = headers or {}

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, limit=-1):
        return self.raw if limit < 0 else self.raw[:limit]


class NormalizerTest(unittest.TestCase):
    def test_schema_one_formula_and_bounds(self) -> None:
        self.assertEqual(normalize_score(100, 100), 100)
        self.assertEqual(normalize_score(100, 80), 125)
        self.assertEqual(normalize_score(100, 10000), 1)
        self.assertEqual(normalize_score(100, 1), 200)
        self.assertEqual(takeover_threshold(100, 0.12), 88)
        self.assertTrue(takeover_eligible(100, 88, 0.12, True))
        self.assertFalse(takeover_eligible(100, 88, 0.12, False))

    def test_nan_inf_and_nonpositive_fail_closed(self) -> None:
        for value in (math.nan, math.inf, -math.inf, 0, -1):
            with self.subTest(value=value), self.assertRaises(ValueError):
                normalize_score(100, value)


class ScreenerTest(unittest.TestCase):
    def test_valid_and_forbidden(self) -> None:
        result = screen_patch(
            VALID_PATCH,
            max_patch_bytes=262144,
            allowed_paths=("connect/**",),
            forbidden_paths=("connect/payment/**",),
        )
        self.assertEqual(result.paths, ("connect/example.go",))
        self.assertEqual(len(result.sha256), 64)
        with self.assertRaises(ValueError):
            screen_patch(
                VALID_PATCH.replace("connect/example.go", "connect/sim-latency/main.go"),
                max_patch_bytes=262144,
                allowed_paths=("connect/**",),
                forbidden_paths=(),
            )
        for malformed in (
            VALID_PATCH.replace("@@ -1 +1 @@", "@@ -1,2 +1 @@"),
            VALID_PATCH + "new file mode 100644\n",
        ):
            with self.subTest(malformed=malformed), self.assertRaises(ValueError):
                screen_patch(
                    malformed,
                    max_patch_bytes=262144,
                    allowed_paths=("connect/**",),
                    forbidden_paths=(),
                )


class ActiveRoundModelTest(unittest.TestCase):
    def test_seed_field_is_rejected(self) -> None:
        value = {
            "round_id": "r", "status": "open", "workload_commitment": "a" * 64,
            "providers_sha256": "b" * 64,
            "score_schema": 1, "opens_at": "o", "closes_at": "c",
            "reveal_at": "x", "created_at": "n", "revealed_seed": "b" * 64,
        }
        with self.assertRaises(ValueError):
            RoundCommitment.from_json(value)

    def test_round_model_enforces_openapi_identity_and_enums(self) -> None:
        for changed in (
            {"round_id": "not-a-uuid"},
            {"status": "mystery"},
            {"workload_commitment": "A" * 64},
            {"score_schema": 2},
            {"closes_at": "2026-08-17T08:30:00Z"},
            {"created_at": "not-a-time"},
        ):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                RoundCommitment.from_json(round_result(**changed))

    def test_job_models_enforce_openapi_identity_and_enums(self) -> None:
        for changed in (
            {"job_id": "not-a-uuid"},
            {"patch_sha256": "A" * 64},
            {"state": "mystery"},
            {"status_url": "/competition/score/not-the-job"},
        ):
            with self.subTest(model="accepted", changed=changed), self.assertRaises(ValueError):
                AcceptedScore.from_json(accepted_result(**changed))
        for changed in (
            {"round_id": "not-a-uuid"},
            {"state": "mystery"},
            {"submitted_at": "not-a-time"},
            {"cache_key": "short"},
        ):
            with self.subTest(model="job", changed=changed), self.assertRaises(ValueError):
                ScoreJob.from_json(job_result(**changed))


class GeneratorTest(unittest.TestCase):
    def test_generate_posts_canonical_utc_times_and_parses_commitment(self) -> None:
        generator = CompetitionGenerator("https://api.example", "operator-token")
        generator.client = mock.Mock()
        generator.client.post.return_value = round_result()
        opens_at = datetime(2026, 8, 17, 11, 0, tzinfo=timezone(timedelta(hours=2)))
        closes_at = opens_at + timedelta(hours=1)
        reveal_at = closes_at + timedelta(hours=1)

        result = generator.generate(opens_at, closes_at, reveal_at)

        self.assertEqual(result.round_id, ROUND_ID)
        generator.client.post.assert_called_once_with(
            "/competition/generate-round",
            {
                "opens_at": "2026-08-17T09:00:00Z",
                "closes_at": "2026-08-17T10:00:00Z",
                "reveal_at": "2026-08-17T11:00:00Z",
            },
        )

    def test_generate_rejects_naive_or_misordered_times_without_request(self) -> None:
        generator = CompetitionGenerator("https://api.example", "operator-token")
        generator.client = mock.Mock()
        aware = datetime(2026, 8, 17, 9, 0, tzinfo=timezone.utc)
        for values in (
            (aware.replace(tzinfo=None), aware + timedelta(hours=1), aware + timedelta(hours=2)),
            (aware, aware, aware + timedelta(hours=1)),
            (aware, aware + timedelta(hours=2), aware + timedelta(hours=1)),
        ):
            with self.subTest(values=values), self.assertRaises(ValueError):
                generator.generate(*values)
        generator.client.post.assert_not_called()


class RunnerTest(unittest.TestCase):
    def new_runner(self, clock=None):
        clock = clock or FakeClock()
        runner = CompetitionRunner(
            "https://api.example",
            "submitter-token",
            poll_interval_seconds=2.0,
            monotonic=clock.monotonic,
            sleep=clock.sleep,
        )
        runner.client = mock.Mock()
        return runner, clock

    def test_submit_preserves_server_cache_identity(self) -> None:
        runner, _clock = self.new_runner()
        runner.client.post.return_value = accepted_result(cache_hit=True, state="succeeded")

        accepted = runner.submit_patch(ROUND_ID, VALID_PATCH)

        self.assertTrue(accepted.cache_hit)
        self.assertEqual(accepted.patch_sha256, PATCH_SHA256)
        runner.client.post.assert_called_once_with(
            "/competition/score", {"round_id": ROUND_ID, "patch": VALID_PATCH},
        )

    def test_poll_follows_one_identity_to_success_with_bounded_sleeps(self) -> None:
        runner, clock = self.new_runner()
        accepted = AcceptedScore.from_json(accepted_result())
        runner.client.get.side_effect = [
            job_result(state="queued"),
            job_result(state="running"),
            job_result(
                state="succeeded",
                score={"score_schema": 1, "placeable": True, "gates": {}},
            ),
        ]

        result = runner.poll(accepted, timeout_seconds=10.0)

        self.assertEqual(result.state, "succeeded")
        self.assertEqual(clock.sleeps, [2.0, 2.0])
        self.assertEqual(
            runner.client.get.call_args_list,
            [mock.call(accepted.status_url), mock.call(accepted.status_url), mock.call(accepted.status_url)],
        )

    def test_poll_returns_typed_terminal_failure(self) -> None:
        runner, clock = self.new_runner()
        accepted = AcceptedScore.from_json(accepted_result())
        runner.client.get.return_value = job_result(
            state="failed",
            eval_error={
                "kind": "submission",
                "code": "candidate_build_failed",
                "message": "candidate did not compile",
                "retriable": False,
            },
        )

        result = runner.poll(accepted, timeout_seconds=10.0)

        self.assertEqual(result.eval_error.code, "candidate_build_failed")
        self.assertEqual(clock.sleeps, [])

    def test_poll_fails_closed_on_job_identity_mismatch(self) -> None:
        runner, _clock = self.new_runner()
        accepted = AcceptedScore.from_json(accepted_result())
        runner.client.get.return_value = job_result(patch_sha256="e" * 64)

        with self.assertRaises(CompetitionApiError) as caught:
            runner.poll(accepted, timeout_seconds=10.0)
        self.assertEqual(caught.exception.code, "job_identity_mismatch")
        self.assertTrue(caught.exception.retriable)

    def test_poll_timeout_uses_remaining_interval_without_resubmit(self) -> None:
        runner, clock = self.new_runner()
        runner.client.post.return_value = accepted_result()
        runner.client.get.return_value = job_result(state="running")

        with self.assertRaises(TimeoutError):
            runner.run(ROUND_ID, VALID_PATCH, timeout_seconds=5.0)

        runner.client.post.assert_called_once()
        self.assertEqual(runner.client.get.call_count, 4)
        self.assertEqual(clock.sleeps, [2.0, 2.0, 1.0])


class ClientErrorTest(unittest.TestCase):
    def test_http_typed_error_is_preserved(self) -> None:
        raw = b'{"kind":"submission","code":"invalid_patch","message":"bad","retriable":false}'
        error = urllib.error.HTTPError(
            "https://api.example/competition/score", 400, "bad request", {}, io.BytesIO(raw),
        )
        client = CompetitionClient("https://api.example", "secret")
        with mock.patch("urllib.request.urlopen", side_effect=error):
            with self.assertRaises(CompetitionApiError) as caught:
                client.get("/competition/score")
        self.assertEqual(caught.exception.kind, "submission")
        self.assertEqual(caught.exception.status, 400)
        self.assertFalse(caught.exception.retriable)

    def test_transport_and_invalid_success_are_typed_infrastructure_errors(self) -> None:
        client = CompetitionClient("https://api.example", "secret")
        cases = (
            (urllib.error.URLError("offline"), "transport_error"),
            (BytesResponse(b"not-json"), "invalid_response"),
            (BytesResponse(b"x" * (MAX_JSON_RESPONSE_BYTES + 1)), "response_too_large"),
        )
        for response, code in cases:
            kwargs = {"side_effect": response} if isinstance(response, Exception) else {"return_value": response}
            with self.subTest(code=code), mock.patch("urllib.request.urlopen", **kwargs):
                with self.assertRaises(CompetitionApiError) as caught:
                    client.get("/competition/info")
                self.assertEqual(caught.exception.code, code)
                self.assertTrue(caught.exception.retriable)


class RevealedWorkloadTest(unittest.TestCase):
    def test_download_authenticates_header_and_bytes(self) -> None:
        raw = b"seed: 48\nfleet: []\n"
        digest = hashlib.sha256(raw).hexdigest()

        class Response:
            headers = {"X-Content-SHA256": digest}

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return raw

        client = CompetitionClient("https://api.example", "secret")
        with mock.patch("urllib.request.urlopen", return_value=Response()):
            got = client.download_revealed_workload(
                "12345678-1234-1234-1234-123456789abc", digest,
            )
        self.assertEqual(got, raw)

    def test_reveal_verifies_seed_file_and_derivation(self) -> None:
        round_id = "12345678-1234-1234-1234-123456789abc"
        seed = bytes(range(32))
        providers = b"seed: derived\n"
        commitment = hashlib.sha256(
            ROUND_DOMAIN + bytes.fromhex(round_id.replace("-", "")) + seed,
        ).hexdigest()
        providers_sha256 = hashlib.sha256(providers).hexdigest()
        got = verify_reveal(
            round_id=round_id,
            seed_hex=seed.hex(),
            workload_commitment=commitment,
            providers=providers,
            providers_sha256=providers_sha256,
        )
        want = int.from_bytes(hashlib.sha256(GENERATOR_DOMAIN + seed).digest()[:8], "big") & ((1 << 63) - 1)
        self.assertEqual(got, want or 1)


if __name__ == "__main__":
    unittest.main()
