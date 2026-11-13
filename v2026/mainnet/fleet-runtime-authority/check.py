#!/usr/bin/env python3
"""Compare a fleet runtime authority document with the chain and upstream's release.

Read-only. Every value comes from public JSON-RPC calls at the finalized head and
from upstream's GitHub release. A pass means the document matches what the network
runs. It is not a review and approves nothing.

    check.py <document.json> [--rpc URL]... [--repo OWNER/NAME]

Standard library only. Run it with python3 -I.
"""

import argparse
import hashlib
import json
import sys
import urllib.request

DEFAULT_RPCS = ["https://lite.chain.opentensor.ai", "https://archive.chain.opentensor.ai"]
DEFAULT_REPO = "RaoFoundation/subtensor"
CODE_KEY = "0x3a636f6465"
USER_AGENT = "urnetwork-fleet-authority-check"
# netuid()
NETUID_SELECTOR = "0xe78015b1"


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
    def github(path):
        request = urllib.request.Request(f"https://api.github.com/repos/{repo}/{path}", headers={"user-agent": USER_AGENT})
        return json.loads(fetch(request))

    release = github(f"releases/tags/v{spec_version}")
    assets = {asset["name"]: asset for asset in release["assets"]}
    deployment = json.loads(fetch(urllib.request.Request(assets["deployment.json"]["browser_download_url"], headers={"user-agent": USER_AGENT})))
    wasm = fetch(urllib.request.Request(assets["subtensor.wasm"]["browser_download_url"], headers={"user-agent": USER_AGENT}))
    return {
        "spec_version": deployment["spec_version"],
        "commit": deployment["commit"],
        "tag_target": github(f"git/ref/tags/v{spec_version}")["object"]["sha"],
        "deployment_code_hash": deployment["code_hash"],
        "wasm_code_hash": blake2b_256(wasm),
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

    def compare(label, have, want):
        good = have == want
        print(f"{'ok  ' if good else 'FAIL'} {label}: {have}")
        if not good:
            failures.append(f"{label}: {have} != {want}")

    for url in arguments.rpc or DEFAULT_RPCS:
        print("endpoint:", url)
        observed = observe_chain(url)
        for field in ("native_chain", "genesis_hash", "evm_chain_id", "runtime_version", "runtime_code_hash", "runtime_metadata_hash"):
            compare(field, observed[field], document[field])
        coordinator_netuid = int(rpc(url, "eth_call", [{"to": document["coordinator"], "data": NETUID_SELECTOR}, "finalized"]), 16)
        compare("coordinator netuid", coordinator_netuid, document["netuid"])

    release = observe_release(arguments.repo, document["runtime_version"]["specVersion"])
    print("release:", arguments.repo)
    compare("release spec version", release["spec_version"], document["runtime_version"]["specVersion"])
    compare("release deployment commit", release["commit"], document["runtime_source_commit"])
    compare("release tag target", release["tag_target"], document["runtime_source_commit"])
    compare("release deployment code hash", release["deployment_code_hash"], document["runtime_code_hash"])
    compare("release wasm code hash", release["wasm_code_hash"], document["runtime_code_hash"])

    if failures:
        print(f"{len(failures)} field(s) differ")
        return 1
    print("every field matches the chain and upstream's release. This is not a review and approves nothing.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
