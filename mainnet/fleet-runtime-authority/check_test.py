"""Tests of check.py against a synthetic chain and upstream release.

Every endpoint and download is served in process by URL; nothing reaches a network,
and every value is synthetic. Upstream's releases v488 and v490 published no
deployment.json, so the fixture release has none either unless a test adds it.

    python3 -I -B mainnet/fleet-runtime-authority/check_test.py -v
"""

import contextlib
import hashlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

spec = importlib.util.spec_from_file_location("fleet_runtime_authority_check", Path(__file__).with_name("check.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)

REPO = "synthetic-upstream/subtensor"
RPCS = ["https://rpc-a.example", "https://rpc-b.example"]
SPEC_VERSION = 8001
CODE = b"\x52\xbc\x53\x76synthetic runtime code"
METADATA = b"meta\x0esynthetic runtime metadata"
COMMIT = "ab" * 20
HEAD = "0x" + "50" * 32
VERSION = {"specName": "synthetic-subtensor", "specVersion": SPEC_VERSION, "transactionVersion": 1, "stateVersion": 1}


def blake2b_256(data):
    return "0x" + hashlib.blake2b(data, digest_size=32).hexdigest()


DOCUMENT = {
    "schema": "urnetwork-mainnet-fleet-runtime-authority-v1",
    "native_chain": "Synthetic Main Network",
    "genesis_hash": "0x" + "51" * 32,
    "evm_chain_id": 964,
    "netuid": 25,
    "coordinator": "0x" + "55" * 20,
    "runtime_source_commit": COMMIT,
    "runtime_review_scope": "urnetwork-fleet-register-commitment-frontier-v1",
    "runtime_review_sha256": "cd" * 32,
    "runtime_version": VERSION,
    "runtime_code_hash": blake2b_256(CODE),
    "runtime_metadata_hash": blake2b_256(METADATA),
}


class Upstream:
    """The chain at each endpoint and the release at upstream, by URL. Assets
    maps a published asset's name to its bytes; no release answers 404."""

    def __init__(self, assets, release=True):
        self.assets = dict(assets)
        self.release = release

    def fetch(self, request, timeout=120):
        url = request.full_url
        if request.data is not None:
            call = json.loads(request.data)
            return json.dumps({"jsonrpc": "2.0", "id": call["id"], "result": self.rpc(url, call["method"], call["params"])}).encode()
        downloads = f"https://downloads.example/v{SPEC_VERSION}/"
        if url.startswith(downloads) and url[len(downloads):] in self.assets:
            return self.assets[url[len(downloads):]]
        if url.endswith(f"/repos/{REPO}/releases/tags/v{SPEC_VERSION}") and self.release:
            names = sorted(self.assets)
            return json.dumps({"tag_name": f"v{SPEC_VERSION}", "assets": [{"name": name, "browser_download_url": downloads + name} for name in names]}).encode()
        if url.endswith(f"/repos/{REPO}/git/ref/tags/v{SPEC_VERSION}"):
            return json.dumps({"ref": f"refs/tags/v{SPEC_VERSION}", "object": {"sha": COMMIT, "type": "commit"}}).encode()
        raise urllib.error.HTTPError(url, 404, "Not Found", {}, None)

    def rpc(self, url, method, params):
        if url not in RPCS:
            raise AssertionError(f"unexpected endpoint {url}")
        if method == "chain_getFinalizedHead":
            return HEAD
        if method == "state_getRuntimeVersion" and params == [HEAD]:
            return dict(VERSION, implName="synthetic-subtensor", authoringVersion=1, implVersion=1, apis=[])
        if method == "state_getStorage" and params == [checker.CODE_KEY, HEAD]:
            return "0x" + CODE.hex()
        if method == "state_getMetadata" and params == [HEAD]:
            return "0x" + METADATA.hex()
        if method == "system_chain":
            return DOCUMENT["native_chain"]
        if method == "chain_getBlockHash" and params == [0]:
            return DOCUMENT["genesis_hash"]
        if method == "eth_chainId":
            return hex(DOCUMENT["evm_chain_id"])
        if method == "eth_call" and params == [{"to": DOCUMENT["coordinator"], "data": checker.NETUID_SELECTOR}, "finalized"]:
            return "0x" + format(DOCUMENT["netuid"], "064x")
        raise AssertionError(f"unexpected rpc {method} {params}")


def deployment_json():
    return json.dumps({"spec_version": SPEC_VERSION, "commit": COMMIT, "code_hash": blake2b_256(CODE), "first_mainnet_block": 100}).encode()


def release_assets(deployment=False, wasm=CODE):
    """The asset set of upstream's v488 and v490: no deployment.json."""
    assets = {"subtensor-digest.json": b"{}", "upgrade-manifest.json": b"{}", "pending-release.json": b"{}"}
    if wasm is not None:
        assets["subtensor.wasm"] = wasm
    if deployment:
        assets["deployment.json"] = deployment_json()
    return assets


def run_check(upstream):
    """check.py's exit status and output for the synthetic document."""
    with tempfile.TemporaryDirectory() as directory:
        path = os.path.join(directory, "runtime-8001.json")
        with open(path, "w") as file:
            json.dump(DOCUMENT, file)
        argv = ["check.py", path, "--repo", REPO] + [argument for url in RPCS for argument in ("--rpc", url)]
        output = io.StringIO()
        with mock.patch.object(checker, "fetch", upstream.fetch), mock.patch("sys.argv", argv), contextlib.redirect_stdout(output):
            status = checker.main()
        return status, output.getvalue()


class Check(unittest.TestCase):
    def test_release_without_deployment_json_is_not_verified(self):
        status, output = run_check(Upstream(release_assets()))
        self.assertEqual(status, checker.EXIT_NOT_VERIFIED, output)
        for label in ("release spec version", "release deployment commit", "release deployment code hash"):
            self.assertIn(f"NOT VERIFIED {label}: upstream's v{SPEC_VERSION} release has no deployment.json", output)
        # the chain's :code against the release's Wasm stands in for the code hash
        for url in RPCS:
            self.assertIn(f"ok   release wasm code hash vs {url} :code: {blake2b_256(CODE)}", output)
        self.assertIn(f"ok   release tag target: {COMMIT}", output)
        self.assertNotIn("FAIL", output)
        self.assertNotIn("every field matches", output)
        self.assertIn("so this is not a pass", output)

    def test_release_with_deployment_json_passes(self):
        status, output = run_check(Upstream(release_assets(deployment=True)))
        self.assertEqual(status, 0, output)
        self.assertNotIn("NOT VERIFIED", output)
        self.assertNotIn(":code:", output)
        self.assertIn(f"ok   release deployment code hash: {blake2b_256(CODE)}", output)
        self.assertIn("every field matches", output)

    def test_release_wasm_other_than_chain_code_fails(self):
        other = b"\x52\xbc\x53\x76other runtime code"
        status, output = run_check(Upstream(release_assets(wasm=other)))
        self.assertEqual(status, 1, output)
        for url in RPCS:
            self.assertIn(f"FAIL release wasm code hash vs {url} :code: {blake2b_256(other)}", output)
        self.assertIn("NOT VERIFIED release deployment code hash", output)

    def test_release_without_any_code_asset_is_not_verified(self):
        status, output = run_check(Upstream(release_assets(wasm=None)))
        self.assertEqual(status, checker.EXIT_NOT_VERIFIED, output)
        self.assertIn(f"NOT VERIFIED release wasm code hash: upstream's v{SPEC_VERSION} release has no subtensor.wasm", output)
        self.assertNotIn("release wasm code hash vs", output)

    def test_unpublished_release_is_not_verified(self):
        status, output = run_check(Upstream(release_assets(), release=False))
        self.assertEqual(status, checker.EXIT_NOT_VERIFIED, output)
        self.assertIn(f"NOT VERIFIED release deployment commit: upstream published no v{SPEC_VERSION} release", output)
        self.assertIn(f"NOT VERIFIED release wasm code hash: upstream published no v{SPEC_VERSION} release", output)
        self.assertNotIn("every field matches", output)


if __name__ == "__main__":
    unittest.main()
