"""Deterministic guard controls; these do not launch or clean real Cargo jobs."""

import importlib.util
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("cargo_control", Path(__file__).with_name("cargo_control.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class CargoControlTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.baseline, self.source = self.root / "baseline", self.root / "control"
        for directory in (self.baseline, self.source):
            (directory / "src").mkdir(parents=True)
            (directory / "Cargo.toml").write_text('[package]\nname="example-probe"\nversion="0.1.0"\n')
            (directory / "Cargo.lock").write_text('version=4\n')
            (directory / "src/lib.rs").write_text("fn admitted() -> bool { true }\n")
        before = guard.census(self.baseline)
        (self.source / "src/lib.rs").write_text("fn admitted() -> bool { false }\n")
        after = guard.census(self.source)
        self.recipe = {"baseline_crate": str(self.baseline), "source_crate": str(self.source),
                       "baseline_files": before, "package": "example-probe", "mutations": [
                           {"path": "src/lib.rs", "before": before["src/lib.rs"],
                            "after": after["src/lib.rs"]}]}

    def artifact(self, fresh):
        path = self.root / "compiler.jsonl"
        path.write_text(json.dumps({"reason": "compiler-artifact", "fresh": fresh,
            "profile": {"test": True}, "target": {"name": "example_probe",
            "src_path": str(self.source / "src/lib.rs")}, "executable": "/retained/example"}) + "\n")
        return path

    def test_same_package_old_mtime_stale_artifact_is_not_a_control(self):
        guard.validate_sources(self.recipe)
        with self.assertRaisesRegex(guard.Refused, "stale control artifact"):
            guard.fresh_artifact(self.artifact(True), "example-probe", self.source)
        accepted = guard.fresh_artifact(self.artifact(False), "example-probe", self.source)
        self.assertIs(accepted["fresh"], False)

    def test_compiler_artifact_must_name_this_physical_crate(self):
        with self.assertRaisesRegex(guard.Refused, "another crate"):
            guard.fresh_artifact(self.artifact(False), "example-probe", self.baseline)

    def test_undeclared_source_and_dependency_changes_are_refused(self):
        guard.validate_sources(self.recipe)
        (self.source / "src/extra.rs").write_text("// unbound source\n")
        with self.assertRaisesRegex(guard.Refused, "undeclared"):
            guard.validate_sources(self.recipe)
        (self.source / "src/extra.rs").unlink()
        (self.source / "Cargo.lock").write_text('version=3\n')
        with self.assertRaisesRegex(guard.Refused, "undeclared"):
            guard.validate_sources(self.recipe)

    def test_mutating_frozen_baseline_does_not_relabel_the_control(self):
        (self.baseline / "src/lib.rs").write_text("// changed original\n")
        with self.assertRaisesRegex(guard.Refused, "baseline crate changed"):
            guard.validate_sources(self.recipe)

    def test_exact_assertion_and_count_required_even_with_exit101(self):
        stdout, stderr = self.root / "stdout", self.root / "stderr"
        selector = "historical::tests::exact_control"
        stdout.write_text(f"running 1 test\ntest {selector} ... FAILED\n"
                          "test result: FAILED. 0 passed; 1 failed; 0 ignored;\n")
        stderr.write_text("assertion failed: unproved write was accepted\n")
        guard.classify_test(101, stdout, stderr, selector, "unproved write was accepted")
        with self.assertRaisesRegex(guard.Refused, "exact intended assertion"):
            guard.classify_test(101, stdout, stderr, selector, "a different failure")
        with self.assertRaisesRegex(guard.Refused, "exit/count"):
            guard.classify_test(1, stdout, stderr, selector, "unproved write was accepted")

    def test_cargo_build_failure_and_filtered_zero_tests_are_not_causality(self):
        stdout, stderr = self.root / "stdout", self.root / "stderr"
        for output in ("error[E0425]: missing source item", "running 0 tests\n0 passed; 0 failed"):
            stdout.write_text(output)
            stderr.write_text("expected message from compiler context")
            with self.assertRaisesRegex(guard.Refused, "exactly the selected failed root"):
                guard.classify_test(101, stdout, stderr, "historical::tests::exact_control", "expected message")

    def process_program(self, leader_exits):
        child = "import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); print('READY',flush=True); time.sleep(60)"
        return ("import os,signal,subprocess,sys,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); "
                f"child=subprocess.Popen([sys.executable,'-c',{child!r}],stdout=subprocess.PIPE); "
                "assert child.stdout.readline()==b'READY\\n'; print(child.pid,flush=True); "
                + ("os._exit(0)" if leader_exits else "time.sleep(60)"))

    def assert_process_gone(self, pid):
        with self.assertRaises(ProcessLookupError):
            os.kill(pid, 0)

    def test_normal_leader_exit_refuses_and_reaps_remaining_descendant(self):
        output = self.root / "normal-process"
        output.mkdir()
        with self.assertRaisesRegex(guard.Refused, "descendants retained process custody") as caught:
            guard.run_process([sys.executable, "-c", self.process_program(True)],
                              self.root, os.environ.copy(), output, "child", 15)
        pid = int((output / "child.stdout").read_text().strip())
        self.assert_process_gone(pid)
        self.assertTrue(caught.exception.qualification_process_result["tree_joined"])
        self.assertEqual(caught.exception.qualification_process_result["exit"], 0)
        self.assertEqual(caught.exception.qualification_process_result["guard_failure"]["type"], "Refused")

    def test_failed_phase_joins_term_ignoring_leader_and_descendant(self):
        previous = guard.set_subreaper(True)
        process = None
        try:
            process = subprocess.Popen([sys.executable, "-c", self.process_program(False)],
                                       stdout=subprocess.PIPE, start_new_session=True)
            with selectors.DefaultSelector() as ready:
                ready.register(process.stdout, selectors.EVENT_READ)
                self.assertTrue(ready.select(timeout=15), "actual child did not reach its ready barrier")
                pid = int(process.stdout.readline())
            self.assertTrue(guard.join_process_tree(process, True))
            self.assert_process_gone(pid)
            self.assert_process_gone(process.pid)
        finally:
            if process is not None:
                guard.join_process_tree(process, True)
                process.stdout.close()
            guard.set_subreaper(previous)

    def test_recipe_read_refuses_fifo_and_preexisting_oversize(self):
        fifo = self.root / "fifo"
        os.mkfifo(fifo)
        with self.assertRaisesRegex(guard.Refused, "bounded regular file"):
            guard.bounded_regular_bytes(fifo, 16)
        large = self.root / "large"
        large.write_bytes(b"abc")
        with self.assertRaisesRegex(guard.Refused, "bounded regular file"):
            guard.bounded_regular_bytes(large, 2)

    def test_reviewed_growth_must_fit_before_any_compiler_starts(self):
        target, output = self.root / "target", self.root / "output"
        target.mkdir()
        recipe = dict(self.recipe, schema=guard.SCHEMA, target_dir=str(target),
                      compile_seconds=60, test_seconds=60, jobs=1,
                      forecast={"compile_bytes": 10 ** 30, "retained_elf_bytes": 1, "log_bytes": 1024})
        recipe_path = self.root / "recipe.json"
        recipe_path.write_text(json.dumps(recipe))
        with self.assertRaisesRegex(guard.Refused, "twice the reviewed future"):
            guard.run(recipe_path, output)
        self.assertFalse(output.exists())

    def test_actual_pipe_overflow_cancels_owned_process(self):
        output = self.root / "overflow-process"
        output.mkdir()
        script = "import os,time; print(os.getpid(),flush=True); os.write(1,b'x'*8192); time.sleep(60)"
        with self.assertRaisesRegex(guard.Refused, "output exceeds reviewed log forecast") as caught:
            guard.run_process([sys.executable, "-c", script], self.root, os.environ.copy(),
                              output, "child", 15, log_limit=4096)
        # The process group and all adopted descendants must be joined even if
        # the overflowing chunk was refused before its bytes were logged.
        self.assertEqual(guard.owned_children(), [])
        self.assertTrue(caught.exception.qualification_process_result["tree_joined"])
        self.assertIs(type(caught.exception.qualification_process_result["exit"]), int)
        self.assertIn("output exceeds", caught.exception.qualification_process_result["guard_failure"]["detail"])


if __name__ == "__main__":
    unittest.main()
