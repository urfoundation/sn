#!/usr/bin/env python3
"""Check one captured test2json stream against a frozen exact root census.

The runner must capture the test binary's exit independently and invoke it with
-test.v=test2json before conversion. This checker neither runs tests nor proves
source identity: source, binary, converter and launcher fences remain required.
Expected FAIL is causal only when its literal occurs in that same root's Output;
package output or a concurrently running root cannot supply another root's proof.
"""

import argparse
import json
from pathlib import Path
import re
import sys


MAX_METADATA_BYTES = 1024 * 1024
MAX_EVENT_BYTES = 1024 * 1024
MAX_STREAM_BYTES = 64 * 1024 * 1024
MAX_EVENTS = 256 * 1024
ROOT = re.compile(r"Test[A-Za-z0-9_]+\Z", re.ASCII)


def rows(path, allow_empty=False):
    """Read canonical bounded TSV bytes; metadata order is itself an input."""
    with Path(path).open("rb") as source:
        data = source.read(MAX_METADATA_BYTES + 1)
    if len(data) > MAX_METADATA_BYTES:
        raise ValueError("metadata exceeds byte bound")
    if not data and allow_empty:
        return []
    if not data.endswith(b"\n") or b"\r" in data or b"\x00" in data:
        raise ValueError("metadata must have canonical newline-terminated rows")
    result = data.decode("utf-8").splitlines()
    if any(not row for row in result):
        raise ValueError("metadata contains a blank row")
    return result


def expected_inputs(outcome_path, marker_path):
    """Require one PASS/FAIL outcome per root and one literal per expected FAIL."""
    expected = {}
    for row in rows(outcome_path):
        fields = row.split("\t")
        if len(fields) != 2 or not ROOT.fullmatch(fields[0]) or fields[1] not in ("PASS", "FAIL"):
            raise ValueError("invalid expected-outcome row")
        if fields[0] in expected:
            raise ValueError("duplicate expected root")
        expected[fields[0]] = fields[1].lower()
    if list(expected) != sorted(expected):
        raise ValueError("expected roots are not bytewise sorted")
    markers = {}
    for row in rows(marker_path, allow_empty=True):
        fields = row.split("\t")
        if len(fields) != 2 or expected.get(fields[0]) != "fail" or not fields[1] or len(fields[1]) > 4096:
            raise ValueError("invalid root-bound failure literal")
        if fields[0] in markers or any(ord(character) < 32 for character in fields[1]):
            raise ValueError("duplicate or noncanonical failure literal")
        markers[fields[0]] = fields[1]
    if list(markers) != sorted(markers) or set(markers) != {root for root, outcome in expected.items() if outcome == "fail"}:
        raise ValueError("failure literal membership differs from expected failures")
    return expected, markers


def unique_object(pairs):
    """Duplicate JSON keys cannot silently substitute an event's root or action."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON event key")
        result[key] = value
    return result


def reject_constant(value):
    raise ValueError("non-JSON numeric constant: " + value)


def verify(source, expected, markers, package, actual_exit):
    """Consume bounded events, retaining only root state and short matcher tails."""
    wanted_exit = 1 if markers else 0
    if actual_exit != wanted_exit:
        raise ValueError(f"actual binary exit {actual_exit}, expected {wanted_exit}")
    states = {}
    outcomes = {}
    tails = {root: "" for root in markers}
    matched = set()
    started = False
    ended = False
    total = 0
    events = 0
    while True:
        line = source.readline(MAX_EVENT_BYTES + 1)
        if not line:
            break
        total += len(line)
        events += 1
        if len(line) > MAX_EVENT_BYTES or total > MAX_STREAM_BYTES or events > MAX_EVENTS or not line.endswith(b"\n"):
            raise ValueError("event stream exceeds a bound or ends in a partial event")
        event = json.loads(line, object_pairs_hook=unique_object, parse_constant=reject_constant)
        if not isinstance(event, dict) or event.get("Package") != package or event.get("FailedBuild"):
            raise ValueError("event package differs or build failed")
        action, root = event.get("Action"), event.get("Test", "")
        if not isinstance(action, str) or not isinstance(root, str) or ended:
            raise ValueError("invalid event identity or event after package termination")
        if not started:
            if action != "start" or root:
                raise ValueError("stream does not begin with its package start")
            started = True
            continue
        if action == "start":
            raise ValueError("duplicate package start")
        if root and root not in expected:
            raise ValueError("unexpected test root or undeclared subtest: " + root)
        if not root:
            if action == "output":
                if not isinstance(event.get("Output"), str):
                    raise ValueError("package output is not text")
                continue
            if action not in ("pass", "fail") or action != ("fail" if markers else "pass"):
                raise ValueError("unexpected package terminal action")
            if outcomes != expected or matched != set(markers):
                raise ValueError("root outcomes or root-bound failure literals are incomplete")
            ended = True
            continue
        state = states.get(root)
        if action == "run":
            if state is not None:
                raise ValueError("duplicate root start: " + root)
            states[root] = "running"
        elif action == "pause":
            if state != "running":
                raise ValueError("pause outside running root: " + root)
            states[root] = "paused"
        elif action == "cont":
            if state != "paused":
                raise ValueError("continue outside paused root: " + root)
            states[root] = "running"
        elif action == "output":
            if state not in ("running", "paused") or not isinstance(event.get("Output"), str):
                raise ValueError("output outside live root or non-text output: " + root)
            if root in markers and root not in matched:
                text = tails[root] + event["Output"]
                literal = markers[root]
                if literal in text:
                    matched.add(root)
                tails[root] = text[-(len(literal) - 1):] if len(literal) > 1 else ""
        elif action in ("pass", "fail"):
            if state != "running" or root in outcomes or expected[root] != action:
                raise ValueError("root terminal state differs: " + root)
            states[root] = "done"
            outcomes[root] = action
        else:
            raise ValueError("unexpected test action: " + action)
    if not started or not ended:
        raise ValueError("missing package start or terminal event")
    return {"status": "matched", "package": package, "roots": len(expected),
            "passed": sum(value == "pass" for value in outcomes.values()),
            "expected_failures": len(markers), "binary_exit": actual_exit,
            "events": events, "bytes": total}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--events", required=True)
    parser.add_argument("--outcomes", required=True)
    parser.add_argument("--failure-literals", required=True)
    parser.add_argument("--package", required=True)
    parser.add_argument("--binary-exit", required=True, type=int)
    args = parser.parse_args()
    try:
        expected, markers = expected_inputs(args.outcomes, args.failure_literals)
        with Path(args.events).open("rb") as source:
            result = verify(source, expected, markers, args.package, args.binary_exit)
    except (ValueError, OSError, UnicodeError) as error:
        print(json.dumps({"status": "refused", "error": str(error)}), file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
