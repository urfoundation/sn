#!/usr/bin/env python3
"""Seal an ended scratch chronology replay without changing its source inputs."""
import collections
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


root = Path(sys.argv[1]).resolve(strict=True)
capture_root = root / "retained-v3-evm-chronology"


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(4 * 1024 * 1024), b""):
            value.update(block)
    return "sha256:" + value.hexdigest()


def read(name):
    return json.loads((root / name).read_text())


def identity(path):
    return {"path": str(path.relative_to(root)), "bytes": path.stat().st_size,
            "sha256": digest(path)}


start = read("evm-run-start.json")
execution = read("evm-run.json")
preparation = read("evm-helper-preparation.json")
manifest = read("retained-v3-evm-chronology/manifest.json")
parameters = read("retained-v3-evm-chronology/capture-parameters.json")
receipt = read("retained-v3-evm-chronology/evm-chronology-receipt.json")
exit_code = int((root / "evm-run.exit").read_text())
expected_pass = receipt["status"] == "pass"
assert (exit_code == 0) == expected_pass
assert digest(capture_root / "manifest.json") == start["manifest_sha256"]
assert digest(capture_root / "capture-parameters.json") == start["parameters_sha256"]
assert digest(root / "evm-helper-preparation.json") == start["helper_preparation_sha256"]
assert receipt["manifest_sha256"] == start["manifest_sha256"]
assert receipt["source_revision"] == start["source_revision"] == parameters["source_revision"]
assert receipt["scope"] == "historical-coordinator-chronology-only"
assert receipt["read_only"] and not receipt["final_acceptance"]
assert not receipt["full_canonical_native_pass"] and receipt["validator_recaptures"] == 0
source = Path(start["cwd"])
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True).strip()
tree = subprocess.check_output(["git", "rev-parse", "HEAD^{tree}"], cwd=source, text=True).strip()
assert revision == start["source_revision"]
assert not subprocess.check_output(["git", "status", "--porcelain=v1"], cwd=source)
binary = Path(start["argv"][0])
assert digest(binary) == start["binary_sha256"]
for item in preparation["source_files"]:
    path = Path(item["path"])
    assert digest(path) == item["sha256"] and path.stat().st_size == item["bytes"]
for item in [manifest["report"]] + [a["file"] for a in manifest["artifacts"]]:
    path = capture_root / item["path"]
    assert digest(path) == item["sha256"] and path.stat().st_size == item["bytes"]
diagnostic_path = Path(preparation["staging_argv"][3])
original_report = diagnostic_path / "report.json"
assert digest(original_report) == manifest["report"]["sha256"]
transcript_path = capture_root / "evm-rpc-transcript.jsonl"
exchanges = [json.loads(line) for line in transcript_path.read_bytes().splitlines()]
methods = collections.Counter(item["method"] for item in exchanges)
allowed = {"eth_chainId", "eth_getBlockByNumber", "eth_getLogs",
           "eth_getTransactionReceipt", "eth_getStorageAt", "eth_getCode"}
assert set(methods).issubset(allowed)
assert sorted(item["sequence"] for item in exchanges) == list(range(1, len(exchanges) + 1))
assert len(exchanges) == receipt["rpc_reads"]
assert dict(methods) == receipt["rpc_method_counts"]
assert digest(transcript_path) == receipt["rpc_transcript_sha256"]
assert transcript_path.stat().st_size == receipt["rpc_transcript_bytes"]
transport_errors = [{"sequence": e["sequence"], "method": e["method"], "error": e["error"]}
                    for e in exchanges if e.get("error")]
rpc_errors = [{"sequence": e["sequence"], "method": e["method"], "error": e["response"]["error"]}
              for e in exchanges if isinstance(e.get("response"), dict) and e["response"].get("error")]
log_queries = set()
for exchange in exchanges:
    assert exchange["request"]["method"] == exchange["method"]
    if exchange["method"] == "eth_getLogs" and isinstance(exchange.get("response"), dict) and "result" in exchange["response"]:
        log_queries.add(json.dumps(exchange["request"]["params"], sort_keys=True))
if (capture_root / "evm-chronology-capture.json").exists():
    assert len(log_queries) == parameters["maximum_log_queries"] == receipt["upgrade_query_count"]
    assert digest(capture_root / "evm-chronology-capture.json") == receipt["evm_capture_sha256"]
if expected_pass:
    assert digest(capture_root / "replayed-timeline.json") == receipt["timeline_sha256"]

artifact_names = ["evm-helper-preparation.json", "retained_evm_chronology_replay_test.go",
                  "retained_evm_chronology_capture_test.go", "retained_evm_rpc_guard_test.go",
                  "retained-evm-92c14e3f.overlay.json", "evm-stage.log", "evm-stage.exit",
                  "evm-compile.log", "evm-compile.exit", "evm-compile.json",
                  "evm-binary-build-info.txt", "evm-guard-normal.log", "evm-guard-normal.exit",
                  "evm-guard-normal.json", "evm-run-start.json", "evm-run.log", "evm-run.exit",
                  "evm-run.json", "census-source-custody.json", "census-source-qualification-followup.json",
                  "seal_retained_evm.py", "retained_evm_proxy_census_test.go",
                  "proxy-control-red.log", "proxy-control-red.exit", "proxy-control-red.json",
                  "proxy-control-red-source.go.txt", "proxy-control-red-custody.json",
                  "retained-v3-evm-chronology/manifest.json",
                  "retained-v3-evm-chronology/capture-parameters.json",
                  "retained-v3-evm-chronology/offline-census-qualification.json",
                  "retained-v3-evm-chronology/evm-chronology-receipt.json",
                  "retained-v3-evm-chronology/evm-rpc-transcript.jsonl"]
for name in ["evm-chronology-capture.json", "replayed-timeline.json"]:
    if (capture_root / name).exists():
        artifact_names.append("retained-v3-evm-chronology/" + name)
artifacts = [identity(root / name) for name in artifact_names]
qualification = {
    "schema": "urnetwork-retained-evm-chronology-qualification-v1",
    "sealed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "status": receipt["status"], "scope": receipt["scope"],
    "source_revision": revision, "source_tree": tree, "source_worktree_clean": True,
    "binary_sha256": start["binary_sha256"], "binary_bytes": binary.stat().st_size,
    "manifest_sha256": start["manifest_sha256"], "parameters_sha256": start["parameters_sha256"],
    "diagnostic_report_sha256": manifest["report"]["sha256"],
    "run_exit": exit_code, "started_at": receipt["started_at"], "completed_at": receipt["completed_at"],
    "rpc_authority": parameters["authority"], "rpc_reads": len(exchanges),
    "rpc_method_counts": dict(sorted(methods.items())), "transport_errors": transport_errors,
    "rpc_errors": rpc_errors, "successful_unique_log_queries": len(log_queries),
    "from_block": parameters["from_block"], "through_block": parameters["through_block"],
    "source_normal_and_race_qualification": "passed", "native_rpc_methods": 0,
    "validator_recaptures": 0, "wallets_opened": 0, "chain_writes": 0,
    "read_only": True, "final_acceptance": False, "full_canonical_native_pass": False,
    "original_v3_report_changed": False, "original_R46_result_changed": False,
    "capture_complete": (capture_root / "evm-chronology-capture.json").exists(),
    "timeline_complete": (capture_root / "replayed-timeline.json").exists(),
    "prior_attempt": preparation["prior_attempt_qualification"],
    "zero_proxy_scope_proof": preparation["proxy_scope_proof"],
    "artifacts": artifacts,
}
for name in ["error", "plan_count", "journal_entry_count", "relay_request_count", "proxy_count",
             "upgrade_event_count", "baseline_count", "capture_upper_head", "node_finalized_head",
             "zero_proxy_plan_count", "planned_coordinator_action_count", "covered_finalized_transition_count",
             "timelines", "evm_capture_sha256", "timeline_sha256", "rpc_transcript_sha256"]:
    if name in receipt:
        qualification[name] = receipt[name]
destination = root / "evm-chronology-qualification.json"
with destination.open("x") as output:
    output.write(json.dumps(qualification, indent=2) + "\n")
for item in artifacts:
    os.chmod(root / item["path"], 0o400)
os.chmod(destination, 0o400)
print(json.dumps({"qualification": str(destination), "sha256": digest(destination),
                  "status": qualification["status"], "rpc_reads": len(exchanges)}, indent=2))
