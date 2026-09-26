#!/usr/bin/env python3
"""Seal the ended zero-network timeline replay and immutable input custody."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

root = Path(sys.argv[1]).resolve(strict=True)
inputs = root / "retained-v3-offline-chronology"


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(4 * 1024 * 1024), b""):
            value.update(block)
    return "sha256:" + value.hexdigest()


def read(path):
    return json.loads(path.read_text())


def identity(path):
    return {"path": str(path.relative_to(root)), "bytes": path.stat().st_size,
            "sha256": digest(path)}


start = read(root / "offline-run-start.json")
execution = read(root / "offline-run.json")
custody = read(root / "offline-source-custody.json")
receipt = read(inputs / "offline-chronology-receipt.json")
manifest = read(inputs / "manifest.json")
parameters = read(inputs / "offline-replay-parameters.json")
capture_qualification = read(inputs / "capture-qualification.json")
exit_code = int((root / "offline-run.exit").read_text())
assert (exit_code == 0) == (receipt["status"] == "pass")
assert receipt["rpc_reads"] == receipt["network_attempts"] == 0
assert receipt["read_only"] and not receipt["final_acceptance"]
assert not receipt["full_canonical_native_pass"] and receipt["validator_recaptures"] == 0
assert digest(inputs / "manifest.json") == start["manifest_sha256"] == receipt["manifest_sha256"]
assert digest(inputs / "offline-replay-parameters.json") == start["parameters_sha256"]
assert digest(inputs / "capture-qualification.json") == parameters["capture_qualification_sha256"]
assert start["source_revision"] == custody["source_revision"] == parameters["verification_source_revision"]
source = Path(custody["source_worktree"])
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True).strip()
assert revision == start["source_revision"]
assert not subprocess.check_output(["git", "status", "--porcelain=v1"], cwd=source)
assert digest(Path(start["argv"][0])) == start["binary_sha256"]
assert digest(root / "offline-source-custody.json") == start["source_custody_sha256"]
assert digest(Path(custody["modfile"])) == custody["modfile_sha256"]
assert digest(Path(custody["modfile"]).with_suffix(".sum")) == custody["modfile_sum_sha256"]
for item in custody["source_files"]:
    path = Path(item["path"])
    assert digest(path) == item["sha256"] and path.stat().st_size == item["bytes"]
for item in [manifest["report"]] + [a["file"] for a in manifest["artifacts"]]:
    path = inputs / item["path"]
    assert digest(path) == item["sha256"] and path.stat().st_size == item["bytes"]
for name in ["evm-chronology-capture.json", "evm-chronology-receipt.json", "evm-rpc-transcript.jsonl", "capture-parameters.json"]:
    item = next(a for a in capture_qualification["artifacts"] if a["path"] == "retained-v3-evm-chronology/" + name)
    path = inputs / name
    assert digest(path) == item["sha256"] and path.stat().st_size == item["bytes"]
names = ["offline-source-custody.json", "production-qualification.json", "offline-d1b8b6e0.overlay.json",
         "retained_offline_chronology_replay_test.go", "retained_offline_chronology_verify_test.go",
         "offline-compile.log", "offline-compile.exit", "offline-compile.json",
         "offline-binary-build-info.txt", "offline-run-start.json", "offline-run.log",
         "offline-run.exit", "offline-run.json", "offline-stage.log", "offline-stage.exit",
         "offline-staging-argv.json", "seal_offline_chronology.py"]
names += ["retained-v3-offline-chronology/" + name for name in
          ["manifest.json", "offline-replay-parameters.json", "capture-qualification.json",
           "evm-chronology-capture.json", "evm-chronology-receipt.json", "evm-rpc-transcript.jsonl",
           "capture-parameters.json", "offline-chronology-receipt.json"]]
timeline = None
if receipt["status"] == "pass":
    assert receipt["verification_source_revision"] == revision
    assert digest(inputs / "replayed-timeline.json") == receipt["timeline_sha256"]
    timeline = read(inputs / "replayed-timeline.json")
    assert timeline["timelines"] == receipt["timelines"]
    assert len(timeline["upgraded_logs"]) == receipt["upgrade_event_count"]
    assert len(timeline["baselines"]) == receipt["baseline_count"]
    names.append("retained-v3-offline-chronology/replayed-timeline.json")
artifacts = [identity(root / name) for name in names]
qualification = {
    "schema": "urnetwork-retained-offline-chronology-qualification-v1",
    "sealed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "status": receipt["status"], "scope": "historical-coordinator-chronology-only",
    "verification_source_revision": revision, "verification_source_tree": custody["source_tree"],
    "captured_source_revision": receipt["captured_source_revision"], "source_worktree_clean": True,
    "binary_sha256": start["binary_sha256"], "binary_bytes": Path(start["argv"][0]).stat().st_size,
    "manifest_sha256": start["manifest_sha256"], "parameters_sha256": start["parameters_sha256"],
    "capture_qualification_sha256": parameters["capture_qualification_sha256"],
    "capture_sha256": capture_qualification["evm_capture_sha256"],
    "captured_rpc_transcript_sha256": capture_qualification["rpc_transcript_sha256"],
    "diagnostic_report_sha256": manifest["report"]["sha256"],
    "run_exit": exit_code, "started_at": receipt["started_at"], "completed_at": receipt["completed_at"],
    "plan_count": receipt.get("plan_count"), "journal_entry_count": receipt.get("journal_entry_count"),
    "relay_request_count": receipt.get("relay_request_count"),
    "rpc_reads": 0, "network_attempts": 0, "validator_recaptures": 0,
    "native_rpc_methods": 0, "chain_writes": 0, "read_only": True,
    "final_acceptance": False, "full_canonical_native_pass": False,
    "original_R46_verdict_unchanged": True, "sealed_v3_unchanged": True,
    "timeline_builder_passed": timeline is not None,
    "timeline_artifact_verifier_passed": timeline is not None,
    "artifacts": artifacts,
}
if timeline is not None:
    qualification.update(timeline_sha256=receipt["timeline_sha256"],
                         proxy_timeline_count=len(timeline["timelines"]),
                         initialization_count=len(timeline["baselines"]),
                         upgrade_count=sum(len(t["upgrades"]) for t in timeline["timelines"]),
                         upgrade_event_count=len(timeline["upgraded_logs"]))
if "error" in receipt:
    qualification["error"] = receipt["error"]
destination = root / "offline-chronology-qualification.json"
with destination.open("x") as output:
    output.write(json.dumps(qualification, indent=2) + "\n")
for name in names:
    os.chmod(root / name, 0o400)
os.chmod(destination, 0o400)
print(json.dumps({"qualification": str(destination), "sha256": digest(destination),
                  "status": qualification["status"], "network_attempts": 0}, indent=2))
