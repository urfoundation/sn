"""Deterministic event attribution controls; no test product or service runs."""

import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("qualification_events", Path(__file__).with_name("check-qualification-events.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)
PACKAGE = "github.com/urfoundation/sn/sim-testnet"
EXPECTED = {"TestFailure": "fail", "TestOther": "pass"}
MARKERS = {"TestFailure": "exact causal assertion"}


def event(action, root="", output=None):
    value = {"Action": action, "Package": PACKAGE}
    if root:
        value["Test"] = root
    if output is not None:
        value["Output"] = output
    return value


def transcript():
    return [event("start"), event("run", "TestFailure"), event("pause", "TestFailure"),
            event("run", "TestOther"), event("cont", "TestFailure"),
            event("output", "TestFailure", "file.go:10: exact causal "),
            event("output", "TestOther", "unrelated parallel output\n"),
            event("output", "TestFailure", "assertion\n"), event("fail", "TestFailure"),
            event("pass", "TestOther"), event("output", output="FAIL\n"), event("fail")]


def encode(events):
    return b"".join(json.dumps(value).encode() + b"\n" for value in events)


class QualificationEventTests(unittest.TestCase):
    def check(self, events, actual_exit=1):
        return checker.verify(io.BytesIO(encode(events)), EXPECTED, MARKERS, PACKAGE, actual_exit)

    def test_interleaved_root_output_and_split_literal_match(self):
        result = self.check(transcript())
        self.assertEqual((result["roots"], result["passed"], result["expected_failures"]), (2, 1, 1))

    def test_other_root_cannot_supply_failure_literal(self):
        events = transcript()
        events[5]["Output"] = "different failure"
        events[7]["Output"] = "\n"
        events[6]["Output"] = "exact causal assertion\n"
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.check(events)

    def test_package_output_cannot_supply_failure_literal(self):
        events = transcript()
        events[5]["Output"] = "different failure"
        events[7]["Output"] = "\n"
        events[10]["Output"] = "exact causal assertion\nFAIL\n"
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.check(events)

    def test_failure_literal_cannot_cross_roots(self):
        events = transcript()
        events[6]["Output"] = "assertion\n"
        events[7]["Output"] = "different tail\n"
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.check(events)

    def test_timeout_exit_is_not_expected_causal_failure(self):
        for status in (0, 2, 124, 137, 143):
            with self.assertRaisesRegex(ValueError, "actual binary exit"):
                self.check(transcript(), status)

    def test_exact_root_start_terminal_and_package_membership(self):
        controls = []
        values = transcript()
        controls.append(values[:2] + [event("run", "TestFailure")] + values[2:])
        values = transcript()
        controls.append(values[:9] + [event("fail", "TestFailure")] + values[9:])
        values = transcript()
        values[9] = event("skip", "TestOther")
        controls.append(values)
        values = transcript()
        values[3]["Test"] = "TestUnexpected"
        controls.append(values)
        values = transcript()
        values[3]["Test"] = "TestOther/subtest"
        controls.append(values)
        values = transcript()
        values[6]["Package"] = "another/package"
        controls.append(values)
        controls.append(transcript()[:-1])
        controls.append(transcript()[1:])
        controls.append(transcript() + [event("output", output="after terminal")])
        for index, values in enumerate(controls):
            with self.assertRaises(ValueError, msg=f"invalid census control {index}"):
                self.check(values)

    def test_malformed_or_duplicate_json_refuses(self):
        for value in (b'{"Action":"start","Action":"fail"}\n', b'{"Action":NaN}\n',
                      b'[]\n', b'null\n', b'{"Action":"start"}', b'\n'):
            with self.assertRaises(ValueError):
                checker.verify(io.BytesIO(value), EXPECTED, MARKERS, PACKAGE, 1)

    def test_line_total_and_event_bounds_are_enforced(self):
        controls = (("MAX_EVENT_BYTES", 3), ("MAX_STREAM_BYTES", 20), ("MAX_EVENTS", 2))
        for name, limit in controls:
            original = getattr(checker, name)
            try:
                setattr(checker, name, limit)
                with self.assertRaisesRegex(ValueError, "bound"):
                    self.check(transcript())
            finally:
                setattr(checker, name, original)

    def test_all_pass_does_not_require_failure_markers(self):
        values = [event("start"), event("run", "TestOther"), event("pass", "TestOther"), event("pass")]
        result = checker.verify(io.BytesIO(encode(values)), {"TestOther": "pass"}, {}, PACKAGE, 0)
        self.assertEqual(result["expected_failures"], 0)

    def test_expected_inputs_have_exact_canonical_membership(self):
        with tempfile.TemporaryDirectory(prefix="qualification-events-") as directory:
            outcomes, markers = Path(directory) / "outcomes.tsv", Path(directory) / "markers.tsv"
            outcomes.write_text("TestFailure\tFAIL\nTestOther\tPASS\n")
            markers.write_text("TestFailure\texact causal assertion\n")
            self.assertEqual(checker.expected_inputs(outcomes, markers), (EXPECTED, MARKERS))
            for invalid in ("TestOther\twrong root\n", "", "TestFailure\tx\nTestFailure\tx\n",
                            "TestFailure\tx\r\n", "TestFailure\tx"):
                markers.write_text(invalid)
                with self.assertRaises(ValueError):
                    checker.expected_inputs(outcomes, markers)
            markers.write_text("TestFailure\texact causal assertion\n")
            for invalid in ("TestOther\tPASS\nTestFailure\tFAIL\n", "TestOther\tSKIP\n",
                            "TestFailure\tFAIL\nTestFailure\tFAIL\n", "TestFailure\tFAIL\n\n"):
                outcomes.write_text(invalid)
                with self.assertRaises(ValueError):
                    checker.expected_inputs(outcomes, markers)


if __name__ == "__main__":
    unittest.main()
