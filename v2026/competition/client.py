"""Small dependency-free HTTP transport with typed competition errors."""

from __future__ import annotations

import json
import hashlib
import hmac
import uuid
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Mapping

from .models import CompetitionApiError


MAX_JSON_RESPONSE_BYTES = 1024 * 1024


class CompetitionClient:
    def __init__(self, base_url: str, token: str, timeout_seconds: float = 30.0):
        parsed = urllib.parse.urlparse(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("base_url must be an absolute http(s) URL")
        if not token or any(ch.isspace() for ch in token):
            raise ValueError("token must be a nonempty opaque bearer without whitespace")
        if timeout_seconds <= 0:
            raise ValueError("timeout_seconds must be positive")
        self.base_url = base_url.rstrip("/")
        self._token = token
        self.timeout_seconds = timeout_seconds

    def get(self, path: str) -> Mapping[str, Any]:
        return self._request("GET", path, None)

    def post(self, path: str, body: Mapping[str, Any]) -> Mapping[str, Any]:
        return self._request("POST", path, body)

    def download_revealed_workload(self, round_id: str, expected_sha256: str) -> bytes:
        try:
            canonical_round_id = str(uuid.UUID(round_id))
        except (ValueError, AttributeError):
            raise ValueError("round_id must be a UUID") from None
        if canonical_round_id != round_id.lower():
            raise ValueError("round_id must use canonical UUID syntax")
        if len(expected_sha256) != 64 or any(ch not in "0123456789abcdef" for ch in expected_sha256):
            raise ValueError("expected_sha256 must be 64 lowercase hex characters")
        request = urllib.request.Request(
            self.base_url + f"/competition/round/{canonical_round_id}/providers.yml",
            method="GET",
            headers={
                "Accept": "application/yaml",
                "Authorization": f"Bearer {self._token}",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                raw = response.read((1 << 30) + 1)
                declared = response.headers.get("X-Content-SHA256", "")
        except urllib.error.HTTPError as error:
            try:
                with error:
                    value = _decode_object(error.read())
                raise CompetitionApiError.from_json(value, error.code) from None
            except CompetitionApiError:
                raise
            except Exception:
                raise CompetitionApiError(
                    "infrastructure", "invalid_error_response",
                    f"competition API returned HTTP {error.code} without a typed error",
                    error.code >= 500, error.code,
                ) from None
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            raise CompetitionApiError(
                "infrastructure", "transport_error",
                f"competition API transport failed: {type(error).__name__}", True,
            ) from None
        if len(raw) > 1 << 30:
            raise CompetitionApiError("infrastructure", "workload_too_large", "revealed workload exceeds the protocol limit", True)
        actual = hashlib.sha256(raw).hexdigest()
        if not hmac.compare_digest(declared, expected_sha256) or not hmac.compare_digest(actual, expected_sha256):
            raise CompetitionApiError("infrastructure", "workload_identity_mismatch", "revealed workload does not match its round commitment", True)
        return raw

    def _request(self, method: str, path: str, body: Mapping[str, Any] | None) -> Mapping[str, Any]:
        if not path.startswith("/") or path.startswith("//"):
            raise ValueError("API path must be an origin-relative path")
        payload = None if body is None else json.dumps(body, separators=(",", ":"), allow_nan=False).encode("utf-8")
        request = urllib.request.Request(
            self.base_url + path,
            data=payload,
            method=method,
            headers={
                "Accept": "application/json",
                "Authorization": f"Bearer {self._token}",
                **({"Content-Type": "application/json"} if payload is not None else {}),
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                raw = response.read(MAX_JSON_RESPONSE_BYTES + 1)
        except urllib.error.HTTPError as error:
            try:
                with error:
                    value = _decode_object(error.read(MAX_JSON_RESPONSE_BYTES + 1))
                raise CompetitionApiError.from_json(value, error.code) from None
            except CompetitionApiError:
                raise
            except Exception:
                raise CompetitionApiError(
                    "infrastructure", "invalid_error_response",
                    f"competition API returned HTTP {error.code} without a typed error",
                    error.code >= 500, error.code,
                ) from None
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            raise CompetitionApiError(
                "infrastructure", "transport_error",
                f"competition API transport failed: {type(error).__name__}", True,
            ) from None
        if len(raw) > MAX_JSON_RESPONSE_BYTES:
            raise CompetitionApiError(
                "infrastructure", "response_too_large",
                "competition API response exceeds the protocol limit", True,
            )
        try:
            value = _decode_object(raw)
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError):
            raise CompetitionApiError("infrastructure", "invalid_response", "competition API returned a non-object", True)
        return value


def _decode_object(raw: bytes) -> Mapping[str, Any]:
    value = json.loads(raw.decode("utf-8"))
    if not isinstance(value, dict):
        raise ValueError("JSON response is not an object")
    return value
