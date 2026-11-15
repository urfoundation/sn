#!/usr/bin/env python3
"""Add a result interpretation to pinned, already joined Rust executions.

This never starts a process, rewrites an original receipt, or inherits a source
qualification. Executable/source admission remains in the referenced original
terminal. The output describes only the exact retained runtime stdout/stderr.
"""

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path


spec = importlib.util.spec_from_file_location("cargo_control", Path(__file__).with_name("cargo_control.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)

SCHEMA = "urnetwork-rust-result-reclassification-v1"
MAXIMUM_REQUEST_BYTES = 1024 * 1024
MAXIMUM_RECEIPT_BYTES = 4 * 1024 * 1024
MAXIMUM_ROOTS = 256


def decode(raw):
    """Reject ambiguous duplicate fields before inspecting pinned metadata."""
    def pairs(items):
        result = {}
        for key, value in items:
            guard.require(key not in result, "reclassification JSON repeats a field")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs)


def read_binding(binding, maximum):
    """Verify exact caller-pinned bytes through the shared bounded reader."""
    guard.require(isinstance(binding, dict) and set(binding) == {"path", "bytes", "sha256"},
                  "reclassification file binding differs")
    path = Path(binding["path"])
    guard.require(path.is_absolute() and path.resolve() == path, "reclassification path aliases")
    guard.require(type(binding["bytes"]) is int and 0 <= binding["bytes"] <= maximum,
                  "reclassification binding exceeds byte bound")
    raw = guard.bounded_regular_bytes(path, maximum)
    guard.require(len(raw) == binding["bytes"] and hashlib.sha256(raw).hexdigest() == binding["sha256"],
                  "reclassification input differs from its exact pin")
    return raw


def classify(request_path, request_sha256):
    """Require original terminal membership, joined argv, and both log pins."""
    raw = guard.bounded_regular_bytes(request_path, MAXIMUM_REQUEST_BYTES)
    guard.require(hashlib.sha256(raw).hexdigest() == request_sha256, "reclassification request pin differs")
    request = decode(raw)
    guard.require(isinstance(request, dict) and set(request) == {"schema", "original_terminal", "roots"}
                  and request["schema"] == SCHEMA, "reclassification request schema differs")
    roots = request["roots"]
    guard.require(isinstance(roots, list) and 0 < len(roots) <= MAXIMUM_ROOTS, "reclassification root count bound")
    terminal = decode(read_binding(request["original_terminal"], MAXIMUM_RECEIPT_BYTES))
    guard.require(isinstance(terminal, dict) and terminal.get("schema") == "native-incremental-terminal-v1"
                  and isinstance(terminal.get("phases"), list)
                  and isinstance(terminal.get("images"), dict), "original terminal grammar differs")
    phases = {}
    for phase in terminal["phases"]:
        guard.require(isinstance(phase, dict) and isinstance(phase.get("phase_id"), str)
                      and phase["phase_id"] not in phases, "original terminal repeats a phase")
        phases[phase["phase_id"]] = phase
    results, seen = [], set()
    for item in roots:
        guard.require(isinstance(item, dict)
                      and set(item) == {"phase_id", "receipt", "root", "expected_assertion"}
                      and isinstance(item["phase_id"], str) and item["phase_id"] not in seen,
                      "reclassification root request repeats or differs")
        seen.add(item["phase_id"])
        phase = phases.get(item["phase_id"])
        guard.require(phase is not None and phase.get("receipt") == item["receipt"],
                      "requested receipt is not in the pinned original terminal")
        receipt = decode(read_binding(item["receipt"], MAXIMUM_RECEIPT_BYTES))
        guard.require(receipt.get("phase_id") == item["phase_id"] and receipt.get("root") == item["root"]
                      and receipt.get("status") == phase.get("status"), "original phase identity differs")
        commands = receipt.get("commands")
        guard.require(isinstance(commands, list), "original commands absent")
        selected = [command for command in commands if isinstance(command, dict)
                    and str(command.get("label", "")).endswith("-rust-test")]
        guard.require(len(selected) == 1, "original phase must contain one selected Rust execution")
        command = selected[0]
        argv, outcome = command.get("argv"), command.get("outcome")
        guard.require(isinstance(argv, list) and len(argv) == 5
                      and argv[1:] == [item["root"], "--exact", "--nocapture", "--test-threads=1"]
                      and isinstance(outcome, dict) and outcome.get("argv") == argv
                      and outcome.get("tree_joined") is True and type(outcome.get("exit")) is int,
                      "original Rust execution is not the exact joined selected root")
        images = [image for image in terminal["images"].values()
                  if isinstance(image, dict) and image.get("path") == argv[0]]
        guard.require(len(images) == 1, "original Rust image absent or ambiguous in terminal custody")
        stdout = read_binding(command["stdout"], guard.MAXIMUM_LOG_BYTES)
        stderr = read_binding(command["stderr"], guard.MAXIMUM_LOG_BYTES)
        guard.require(outcome.get("stdout_sha256") == command["stdout"]["sha256"]
                      and outcome.get("stderr_sha256") == command["stderr"]["sha256"]
                      and outcome.get("log_bytes") == len(stdout) + len(stderr), "original outcome log binding differs")
        result = guard.classify_rust_output(outcome["exit"], stdout, stderr, item["root"], item["expected_assertion"])
        results.append({"phase_id": item["phase_id"], "original_receipt": item["receipt"],
                        "original_phase_status": receipt["status"], "original_error": receipt.get("error"),
                        "original_image": images[0], "stdout": command["stdout"], "stderr": command["stderr"],
                        "runtime_result": result})
    return {"schema": SCHEMA, "request": {"path": str(Path(request_path).absolute()), "bytes": len(raw),
                                          "sha256": request_sha256},
            "original_terminal": request["original_terminal"], "count": len(results), "results": results,
            "scope": "retained runtime result interpretation only; original phase and source qualification remain separate",
            "test_processes_started": 0, "originals_modified": False}


def publish(path, result):
    """Publish once, with file and directory sync, without replacing evidence."""
    path = Path(path)
    guard.require(path.is_absolute() and path.parent.resolve() == path.parent,
                  "reclassification output path aliases")
    raw = (json.dumps(result, indent=2, sort_keys=True) + "\n").encode()
    guard.require(len(raw) <= MAXIMUM_RECEIPT_BYTES, "reclassification output exceeds byte bound")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        remaining = memoryview(raw)
        while remaining:
            written = os.write(descriptor, remaining)
            guard.require(written > 0, "reclassification output made no progress")
            remaining = remaining[written:]
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--request", required=True, type=Path)
    parser.add_argument("--request-sha256", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    result = classify(args.request, args.request_sha256)
    publish(args.output, result)


if __name__ == "__main__":
    main()
