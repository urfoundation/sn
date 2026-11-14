#!/usr/bin/env python3
"""Compare a fleet runtime authority document with the chain and upstream's release.

Read-only. Every value comes from public JSON-RPC calls at the finalized head and
from upstream's GitHub release. A pass means the document matches what the network
runs. It is not a review and approves nothing.

    check.py <document.json> [--rpc URL]... [--repo OWNER/NAME]

Each check prints ok or FAIL, or NOT VERIFIED when it could not run because
upstream did not publish what it reads: the release, its tag, or a release asset.
Releases v488 and v490 have no deployment.json. Without it, the release's
subtensor.wasm is compared with each endpoint's :code instead, and the checks that
read deployment.json stay not verified.

Exit status: 0 only when every check ran and matched, 1 when a field differs, and
3 when no field differs but a check is not verified.

Standard library only. Run it with python3 -I.
"""

import argparse
import hashlib
import json
import sys
import urllib.error
import urllib.request

DEFAULT_RPCS = ["https://lite.chain.opentensor.ai", "https://archive.chain.opentensor.ai"]
DEFAULT_REPO = "RaoFoundation/subtensor"
CODE_KEY = "0x3a636f6465"
USER_AGENT = "urnetwork-fleet-authority-check"
# netuid()
NETUID_SELECTOR = "0xe78015b1"
# No field differs but a check did not run. argparse exits 2 on a usage error.
EXIT_NOT_VERIFIED = 3


def fetch(request, timeout=120):
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return response.read()


def rpc(url, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request(url, data=body, headers={"content-type": "application/json", "user-agent": USER_AGENT})
    reply = json.loads(fetch(request))
    if "error" in reply:
        raise RuntimeError(f"{method}: {reply['error']}")
    return reply["result"]


def blake2b_256(data):
    return "0x" + hashlib.blake2b(data, digest_size=32).hexdigest()


def observe_chain(url):
    head = rpc(url, "chain_getFinalizedHead", [])
    version = rpc(url, "state_getRuntimeVersion", [head])
    code = bytes.fromhex(rpc(url, "state_getStorage", [CODE_KEY, head])[2:])
    metadata = bytes.fromhex(rpc(url, "state_getMetadata", [head])[2:])
    return {
        "native_chain": rpc(url, "system_chain", []),
        "genesis_hash": rpc(url, "chain_getBlockHash", [0]),
        "evm_chain_id": int(rpc(url, "eth_chainId", []), 16),
        "runtime_version": {
            "specName": version["specName"],
            "specVersion": version["specVersion"],
            "transactionVersion": version["transactionVersion"],
            "stateVersion": version["stateVersion"],
        },
        "runtime_code_hash": blake2b_256(code),
        "runtime_metadata_hash": blake2b_256(metadata),
    }


def observe_release(repo, spec_version):
    """Upstream's release for the spec version. What upstream did not publish,
    the release, its tag or an asset read here, is None, never a guess."""

    def get(url):
        try:
            return fetch(urllib.request.Request(url, headers={"user-agent": USER_AGENT}))
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            raise

    def github(path):
        raw = get(f"https://api.github.com/repos/{repo}/{path}")
        return None if raw is None else json.loads(raw)

    release = github(f"releases/tags/v{spec_version}")
    assets = {} if release is None else {asset["name"]: asset["browser_download_url"] for asset in release["assets"]}

    def asset(name):
        return get(assets[name]) if name in assets else None

    deployment = asset("deployment.json")
    wasm = asset("subtensor.wasm")
    tag = github(f"git/ref/tags/v{spec_version}")
    return {
        "published": release is not None,
        "assets": sorted(assets),
        "deployment": None if deployment is None else json.loads(deployment),
        "tag_target": None if tag is None else tag["object"]["sha"],
        "wasm_code_hash": None if wasm is None else blake2b_256(wasm),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("document")
    parser.add_argument("--rpc", action="append", default=None)
    parser.add_argument("--repo", default=DEFAULT_REPO)
    arguments = parser.parse_args()

    with open(arguments.document, "rb") as file:
        raw = file.read()
    document = json.loads(raw)
    print("document sha256:", hashlib.sha256(raw).hexdigest())

    failures = []
    unverified = []

    def compare(label, have, want):
        good = have == want
        print(f"{'ok  ' if good else 'FAIL'} {label}: {have}")
        if not good:
            failures.append(f"{label}: {have} != {want}")

    def not_verified(label, reason):
        print(f"NOT VERIFIED {label}: {reason}")
        unverified.append(f"{label}: {reason}")

    code_hashes = {}
    for url in arguments.rpc or DEFAULT_RPCS:
        print("endpoint:", url)
        observed = observe_chain(url)
        code_hashes[url] = observed["runtime_code_hash"]
        for field in ("native_chain", "genesis_hash", "evm_chain_id", "runtime_version", "runtime_code_hash", "runtime_metadata_hash"):
            compare(field, observed[field], document[field])
        coordinator_netuid = int(rpc(url, "eth_call", [{"to": document["coordinator"], "data": NETUID_SELECTOR}, "finalized"]), 16)
        compare("coordinator netuid", coordinator_netuid, document["netuid"])

    spec_version = document["runtime_version"]["specVersion"]
    tag = f"v{spec_version}"
    release = observe_release(arguments.repo, spec_version)
    print("release:", arguments.repo, tag, "assets:", ", ".join(release["assets"]) or "none")

    def unpublished(name):
        if not release["published"]:
            return f"upstream published no {tag} release"
        return f"upstream's {tag} release has no {name}"

    deployment = release["deployment"]

    def from_deployment(label, key, want):
        if deployment is None:
            not_verified(label, unpublished("deployment.json"))
        elif key not in deployment:
            not_verified(label, f"upstream's {tag} deployment.json has no {key}")
        else:
            compare(label, deployment[key], want)

    from_deployment("release spec version", "spec_version", spec_version)
    from_deployment("release deployment commit", "commit", document["runtime_source_commit"])
    if release["tag_target"] is None:
        not_verified("release tag target", f"upstream published no {tag} tag")
    else:
        compare("release tag target", release["tag_target"], document["runtime_source_commit"])
    from_deployment("release deployment code hash", "code_hash", document["runtime_code_hash"])
    if release["wasm_code_hash"] is None:
        not_verified("release wasm code hash", unpublished("subtensor.wasm"))
    else:
        compare("release wasm code hash", release["wasm_code_hash"], document["runtime_code_hash"])
        if deployment is None or "code_hash" not in deployment:
            # Without upstream's stated code hash, each endpoint's :code is
            # compared with the release's Wasm itself.
            for url, code_hash in code_hashes.items():
                compare(f"release wasm code hash vs {url} :code", release["wasm_code_hash"], code_hash)

    if failures:
        print(f"{len(failures)} field(s) differ" + (f", and {len(unverified)} check(s) are not verified" if unverified else ""))
        return 1
    if unverified:
        print(f"no field differs, but {len(unverified)} check(s) are not verified, so this is not a pass.")
        return EXIT_NOT_VERIFIED
    print("every field matches the chain and upstream's release. This is not a review and approves nothing.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
