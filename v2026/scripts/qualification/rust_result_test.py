"""Deterministic result-format and retained-evidence controls; no child processes."""

import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("rust_result_reclassify", Path(__file__).with_name("rust_result_reclassify.py"))
reclassify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reclassify)
guard = reclassify.guard
ROOT = "tests::synthetic_exact_proof"
ASSERTION = "assertion failed: missing proof became accepted"


class RustResultTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name).resolve()
        self.stdout, self.stderr = self.directory / "stdout", self.directory / "stderr"

    def output(self, failed=False, interleaved=True):
        status = "FAILED" if failed else "ok"
        passed, failures = (0, 1) if failed else (1, 0)
        middle = '{"layout":1,"complete":true}\n{"layout":0}\n' if interleaved else ""
        return (f"\nrunning 1 test\ntest {ROOT} ... {middle}{status}\n\n"
                f"test result: {status}. {passed} passed; {failures} failed; 0 ignored; "
                "0 measured; 14 filtered out; finished in 0.01s\n").encode()

    def classify(self, stdout=None, stderr=b"", code=0, assertion=None):
        self.stdout.write_bytes(self.output() if stdout is None else stdout)
        self.stderr.write_bytes(stderr)
        return guard.classify_rust_root(code, self.stdout, self.stderr, ROOT, assertion)

    def test_interleaved_positive_preserves_exact_root_and_summary(self):
        result = self.classify()
        self.assertEqual((result["root"], result["classification"], result["passed"], result["ignored"]),
                         (ROOT, "PASS", 1, 0))
        self.assertEqual(result["filtered_out"], 14)

    def test_interleaved_causal_uses_actual_assertion_on_either_stream(self):
        for stdout, stderr in ((self.output(True), ASSERTION.encode()),
                               (self.output(True).replace(b'{"layout":0}', ASSERTION.encode()), b"")):
            self.stdout.write_bytes(stdout)
            self.stderr.write_bytes(stderr)
            result = guard.classify_test(101, self.stdout, self.stderr, ROOT, ASSERTION)
            self.assertEqual(result["classification"], "EXPECTED_BEHAVIORAL_FAILURE")

    def test_contiguous_and_minimal_existing_summary_still_classify(self):
        self.classify(self.output(interleaved=False))
        for ending in (b"", b"\r"):
            output = (b"running 1 test\n" + f"test {ROOT} ... FAILED\n".encode()
                      + b"test result: FAILED. 0 passed; 1 failed; 0 ignored;" + ending + b"\n")
            self.assertEqual(self.classify(output, ASSERTION.encode(), 101, ASSERTION)["failed"], 1)

    def test_missing_duplicate_and_unexpected_roots_refuse(self):
        original = self.output()
        announcement = f"test {ROOT} ... ".encode()
        for output in (original.replace(announcement, b"diagnostic ... "),
                       original.replace(announcement, announcement + b"\n" + announcement),
                       original.replace(ROOT.encode(), b"tests::another_proof"),
                       original + b"test tests::another_proof ... ok\n",
                       original + b"test tests::another_proof ... ignored\n"):
            with self.assertRaisesRegex(guard.Refused, "exactly the selected root"):
                self.classify(output)

    def test_missing_duplicate_and_zero_run_census_refuse(self):
        original = self.output()
        for output in (original.replace(b"running 1 test\n", b""),
                       original + b"running 1 test\n", original.replace(b"running 1 test", b"running 0 tests"),
                       original.replace(b"running 1 test", b"running 11 tests")):
            with self.assertRaisesRegex(guard.Refused, "exactly the selected root"):
                self.classify(output)

    def test_missing_duplicate_malformed_and_reordered_summaries_refuse(self):
        original = self.output()
        summary = original[original.index(b"test result:"):]
        for output in (original.replace(summary, b""), original + summary,
                       original.replace(b"test result: ok.", b"test result: incomplete."),
                       summary + original.replace(summary, b""),
                       original.replace(b"finished in 0.01s", b"unfinished summary")):
            with self.assertRaisesRegex(guard.Refused, "terminal summary"):
                self.classify(output)

    def test_wrong_exit_ignored_and_wrong_counts_never_pass(self):
        for output, code in ((self.output(), 101), (self.output(), False),
                             (self.output().replace(b"0 ignored", b"1 ignored"), 0),
                             (self.output().replace(b"1 passed", b"0 passed"), 0),
                             (self.output().replace(b"0 failed", b"1 failed"), 0),
                             (self.output().replace(b"0 measured", b"1 measured"), 0)):
            with self.assertRaisesRegex(guard.Refused, "exit/count"):
                self.classify(output, code=code)

    def test_causal_requires_failed_summary_exit_and_exact_assertion(self):
        for output, code in ((self.output(True), 0), (self.output(), 101),
                             (self.output(True).replace(b"0 ignored", b"1 ignored"), 101)):
            with self.assertRaisesRegex(guard.Refused, "exit/count"):
                self.classify(output, ASSERTION.encode(), code, ASSERTION)
        with self.assertRaisesRegex(guard.Refused, "exact intended assertion"):
            self.classify(self.output(True), b"an unrelated assertion", 101, ASSERTION)

    def test_stderr_cannot_supply_or_duplicate_official_structure(self):
        for diagnostic in (b"running 1 test\n", f"test {ROOT} ... ok\n".encode(),
                           b"test result: ok. 1 passed; 0 failed; 0 ignored;\n"):
            with self.assertRaises(guard.Refused):
                self.classify(stderr=diagnostic)
        with self.assertRaises(guard.Refused):
            self.classify(b"", self.output())

    def bind(self, name, raw):
        path = self.directory / name
        path.write_bytes(raw)
        return {"path": str(path), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}

    def json_binding(self, name, value):
        return self.bind(name, (json.dumps(value, sort_keys=True) + "\n").encode())

    def retained(self, mutate=None):
        stdout = self.bind("runtime.stdout", self.output())
        stderr = self.bind("runtime.stderr", b"")
        image = {"path": str(self.directory / "not-executed.test"), "sha256": "1" * 64}
        argv = [image["path"], ROOT, "--exact", "--nocapture", "--test-threads=1"]
        command = {"label": "synthetic-rust-test", "argv": argv, "stdout": stdout, "stderr": stderr,
                   "outcome": {"argv": argv, "exit": 0, "tree_joined": True,
                               "stdout_sha256": stdout["sha256"], "stderr_sha256": stderr["sha256"],
                               "log_bytes": stdout["bytes"] + stderr["bytes"]}}
        receipt = {"phase_id": "synthetic", "root": ROOT, "status": "FAILED",
                   "error": "original contiguous parser refused", "commands": [command]}
        if mutate is not None:
            mutate(receipt)
        receipt_binding = self.json_binding("receipt.json", receipt)
        terminal = {"schema": "native-incremental-terminal-v1", "images": {"synthetic": image},
                    "phases": [{"phase_id": "synthetic", "status": "FAILED", "receipt": receipt_binding}]}
        terminal_binding = self.json_binding("terminal.json", terminal)
        request = {"schema": reclassify.SCHEMA, "original_terminal": terminal_binding,
                   "roots": [{"phase_id": "synthetic", "receipt": receipt_binding, "root": ROOT,
                              "expected_assertion": None}]}
        return self.json_binding("request.json", request)

    def test_reclassification_preserves_original_refusal_and_starts_no_process(self):
        request = self.retained()
        originals = {path: path.read_bytes() for path in self.directory.iterdir()}
        result = reclassify.classify(Path(request["path"]), request["sha256"])
        self.assertEqual(result["results"][0]["original_phase_status"], "FAILED")
        self.assertEqual(result["results"][0]["runtime_result"]["classification"], "PASS")
        self.assertEqual(result["test_processes_started"], 0)
        self.assertEqual({path: path.read_bytes() for path in originals}, originals)
        output = self.directory / "additional.json"
        reclassify.publish(output, result)
        self.assertEqual(json.loads(output.read_bytes()), result)
        with self.assertRaises(FileExistsError):
            reclassify.publish(output, result)
        self.assertEqual({path: path.read_bytes() for path in originals}, originals)

    def test_reclassification_refuses_changed_logs_and_unjoined_or_different_argv(self):
        request = self.retained()
        (self.directory / "runtime.stdout").write_bytes(self.output() + b"changed")
        with self.assertRaisesRegex(guard.Refused, "exact pin"):
            reclassify.classify(Path(request["path"]), request["sha256"])
        for mutate in (lambda r: r["commands"][0]["outcome"].update(tree_joined=False),
                       lambda r: r["commands"][0].update(argv=["different"]),
                       lambda r: r["commands"][0]["outcome"].update(stdout_sha256="0" * 64)):
            request = self.retained(mutate)
            with self.assertRaises(guard.Refused):
                reclassify.classify(Path(request["path"]), request["sha256"])

    def test_reclassification_refuses_duplicate_fields_roots_and_foreign_receipts(self):
        with self.assertRaisesRegex(guard.Refused, "repeats a field"):
            reclassify.decode(b'{"root":1,"root":2}')
        request = self.retained()
        value = json.loads(Path(request["path"]).read_bytes())
        value["roots"].append(value["roots"][0])
        request = self.json_binding("request.json", value)
        with self.assertRaisesRegex(guard.Refused, "repeats or differs"):
            reclassify.classify(Path(request["path"]), request["sha256"])
        value["roots"] = value["roots"][:1]
        value["roots"][0]["receipt"]["sha256"] = "0" * 64
        request = self.json_binding("request.json", value)
        with self.assertRaisesRegex(guard.Refused, "not in the pinned original terminal"):
            reclassify.classify(Path(request["path"]), request["sha256"])


if __name__ == "__main__":
    unittest.main()
