"""Real subprocess/context regressions; no Go or Rust application is compiled."""

import hashlib
import functools
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import cargo_control
import owner_resource_guard
from cargo_control import Refused, run_process
from child_context import ChildContext, bind_executable, compiler_census, replay_process_result


class ChildContextTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.cwd, self.output = self.root / "working", self.root / "evidence"
        self.bin, self.cache = self.root / "bin", self.root / "cache with spaces"
        for directory in (self.cwd, self.output, self.bin, self.cache):
            directory.mkdir()
        self.tool = self.cache / "dynamic-test2json"
        shutil.copyfile(Path("/usr/bin/true").resolve(), self.tool)
        self.tool.chmod(0o500)
        self.go = self.bin / "go"
        self.go.write_text("#!" + str(Path(sys.executable).resolve()) + "\n"
            "import json,os,sys\n"
            "with open(os.environ['PROBE_RECORD'],'a') as stream:\n"
            " stream.write(json.dumps({'argv':sys.argv[1:],'cwd':os.getcwd(),'env':dict(os.environ)})+'\\n')\n"
            "if sys.argv[1:]==['version']: print('go version synthetic-context-fixture')\n"
            "elif sys.argv[1:]==['tool','-n','test2json']: print(os.environ['DYNAMIC_TOOL'])\n"
            "else: print('body-context-observed')\n")
        self.go.chmod(0o500)
        self.sha = hashlib.sha256(self.go.read_bytes()).hexdigest()
        self.environment = {"PATH": str(self.bin), "GOENV": "off", "GOTOOLCHAIN": "local",
            "LC_ALL": "C", "PYTHONCOERCECLOCALE": "0", "PROBE_RECORD": str(self.root / "probes.jsonl"),
            "DYNAMIC_TOOL": str(self.tool), "QUAL_CONTEXT_TOKEN": "synthetic-original"}

    def prepare(self):
        context = ChildContext(self.cwd, self.environment)
        tools = context.prepare_go(self.go, self.sha, self.output)
        return context, tools

    def test_missing_service_path_reproduces_old_launch_and_refuses_before_body(self):
        with self.assertRaises(FileNotFoundError):
            subprocess.run(["go", "version"], cwd=self.cwd, env={"PATH": str(self.root / "absent")}, check=True)
        self.environment.pop("PATH")
        context = ChildContext(self.cwd, self.environment)
        with self.assertRaisesRegex(Refused, "explicit absolute child PATH"):
            context.prepare_go(self.go, self.sha, self.output)
        self.assertEqual(list(self.output.iterdir()), [])

    def test_dynamic_tool_outside_goroot_and_exact_child_context_are_retained(self):
        self.assertFalse((self.bin.parent / "pkg/tool/linux_amd64/test2json").exists())
        context, tools = self.prepare()
        self.assertEqual(tools["test2json"]["path"], str(self.tool))
        self.assertEqual(tools["test2json"]["sha256"], hashlib.sha256(self.tool.read_bytes()).hexdigest())
        self.assertTrue(all(probe["tree_joined"] for probe in tools["probes"]))
        self.environment["QUAL_CONTEXT_TOKEN"] = "changed-parent-map"
        result = context.run([str(self.go), "body"], self.output, "body", 10)
        rows = [json.loads(line) for line in (self.root / "probes.jsonl").read_text().splitlines()]
        self.assertEqual([row["argv"] for row in rows], [["version"], ["tool", "-n", "test2json"], ["body"]])
        self.assertTrue(all(row["cwd"] == str(self.cwd) for row in rows))
        self.assertTrue(all(row["env"] == rows[0]["env"] for row in rows))
        self.assertEqual(rows[-1]["env"]["QUAL_CONTEXT_TOKEN"], "synthetic-original")
        self.assertEqual(result["child_context"]["environment_sha256"], tools["child_context"]["environment_sha256"])
        self.assertTrue(result["tree_joined"])

    def test_poisoned_child_path_is_refused_even_when_explicit_go_version_works(self):
        poison = self.root / "poison"
        poison.mkdir()
        shutil.copyfile(self.go, poison / "go")
        (poison / "go").chmod(0o500)
        self.environment["PATH"] = str(poison) + os.pathsep + str(self.bin)
        proof = subprocess.run([str(self.go), "version"], cwd=self.cwd, env=self.environment,
                               capture_output=True, timeout=10)
        self.assertEqual(proof.returncode, 0)
        with self.assertRaisesRegex(Refused, "different or absent Go"):
            self.prepare()
        self.assertEqual(list(self.output.iterdir()), [])

    def test_wrong_tool_pin_or_relative_command_cannot_start_a_child(self):
        context = ChildContext(self.cwd, self.environment)
        with self.assertRaisesRegex(Refused, "SHA256 pin"):
            context.bind(self.go, "0" * 64)
        context.bind(self.go, self.sha)
        with self.assertRaisesRegex(Refused, "bound absolute executable"):
            context.run(["go", "version"], self.output, "wrong", 10)
        self.assertFalse((self.root / "probes.jsonl").exists())

    def test_changed_child_directory_refuses_before_launch(self):
        context, _ = self.prepare()
        self.cwd.rename(self.root / "original-working")
        self.cwd.mkdir()
        with self.assertRaisesRegex(Refused, "cwd identity changed"):
            context.run([str(self.go), "body"], self.output, "body", 10)
        self.assertFalse((self.output / "body.stdout").exists())

    def test_replaced_executable_with_identical_bytes_refuses_before_launch(self):
        context, tools = self.prepare()
        original = self.root / "original-tool"
        self.tool.rename(original)
        shutil.copyfile(original, self.tool)
        self.tool.chmod(0o500)
        with self.assertRaisesRegex(Refused, "executable identity changed"):
            context.run([tools["test2json"]["path"]], self.output, "body", 10)
        self.assertFalse((self.output / "body.stdout").exists())

    def test_disappeared_dynamic_tool_is_not_accepted_from_successful_probe(self):
        self.tool.unlink()
        with self.assertRaises(FileNotFoundError):
            self.prepare()
        self.assertTrue((self.output / "go-context-test2json.stdout").exists())
        self.assertFalse((self.output / "body.stdout").exists())

    def test_interpreter_origin_is_bound_and_env_shebang_is_refused(self):
        binding = bind_executable(self.go, self.sha)
        self.assertEqual(binding["interpreter"]["path"], str(Path(sys.executable).resolve()))
        bad = self.bin / "indirect-python"
        bad.write_text("#!/usr/bin/env python3\nprint('unbound interpreter')\n")
        bad.chmod(0o500)
        with self.assertRaisesRegex(Refused, "direct pinned interpreter"):
            bind_executable(bad)

    def test_fixture_environment_secret_values_are_not_published(self):
        self.environment["SYNTHETIC_SECRET"] = "synthetic-do-not-publish"
        self.environment["GOPROXY"] = "https://synthetic-secret@modules.example/"
        context = ChildContext(self.cwd, self.environment)
        receipt = json.dumps(context.receipt())
        self.assertNotIn("synthetic-do-not-publish", receipt)
        self.assertNotIn("synthetic-secret@", receipt)

    def test_wrong_actual_python_pin_refuses_before_any_tool_probe(self):
        python = str(Path(sys.executable).resolve())
        with self.assertRaisesRegex(Refused, "Python interpreter differs"):
            ChildContext(self.cwd, self.environment, {"path": python, "sha256": "0" * 64})
        self.assertFalse((self.root / "probes.jsonl").exists())

    def test_checker_exception_after_wait_replays_exact_flat_result_without_rerunning(self):
        context = ChildContext(self.cwd, self.environment)
        context.bind(self.go, self.sha)
        result = context.run([str(self.go), "body"], self.output, "body", 10)
        # Reproduce the real integration defect: a checker expected nested
        # stderr metadata after the actual guard returned flat fields.
        with self.assertRaises(KeyError):
            _ = result["stderr"]["path"]
        path = self.output / "body.process-result.json"
        self.assertTrue(path.is_file(), "checker failure lost actual joined wait result")
        retained = json.loads(path.read_text())
        self.assertEqual(retained["result"]["exit"], 0)
        self.assertIs(retained["result"]["tree_joined"], True)
        rows = (self.root / "probes.jsonl").read_bytes()
        with mock.patch.object(subprocess, "Popen", side_effect=AssertionError("checker replay started a child")):
            replayed = replay_process_result(result["process_result"])
        self.assertEqual(replayed["exit"], result["exit"])
        self.assertEqual(replayed["stdout_sha256"], result["stdout_sha256"])
        self.assertEqual(replayed["stderr_sha256"], result["stderr_sha256"])
        self.assertTrue(replayed["tree_joined"] and replayed["context_verified"])
        self.assertEqual((self.root / "probes.jsonl").read_bytes(), rows)
        self.assertEqual(len(rows.splitlines()), 1)
        self.assertIn("not-performed", replayed["test_classification"])

    def test_postcheck_failure_keeps_actual_wait_but_cannot_claim_verified_context(self):
        context = ChildContext(self.cwd, self.environment)
        context.bind(self.go, self.sha)

        def replace_after_actual_wait(*args, **kwargs):
            result = run_process(*args, **kwargs)
            original = self.root / "original-go"
            self.go.rename(original)
            shutil.copyfile(original, self.go)
            self.go.chmod(0o500)
            return result

        with self.assertRaisesRegex(Refused, "executable identity changed"):
            context.run([str(self.go), "body"], self.output, "body", 10,
                        process_guard=replace_after_actual_wait)
        path = self.output / "body.process-result.json"
        reference = {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
        with self.assertRaisesRegex(Refused, "postcheck is incomplete"):
            replay_process_result(reference)
        observed = replay_process_result(reference, require_context_verified=False)
        self.assertEqual(observed["exit"], 0)
        self.assertTrue(observed["tree_joined"])
        self.assertFalse(observed["context_verified"])
        self.assertEqual(len((self.root / "probes.jsonl").read_text().splitlines()), 1)

    def test_changed_retained_output_and_existing_outcome_cannot_replay_or_restart(self):
        context = ChildContext(self.cwd, self.environment)
        context.bind(self.go, self.sha)
        result = context.run([str(self.go), "body"], self.output, "body", 10)
        with self.assertRaisesRegex(Refused, "already retains an outcome"):
            context.run([str(self.go), "body"], self.output, "body", 10)
        (self.output / "body.stdout").write_text("synthetic altered output\n")
        with self.assertRaisesRegex(Refused, "retained process output differs"):
            replay_process_result(result["process_result"])
        self.assertEqual(len((self.root / "probes.jsonl").read_text().splitlines()), 1)

    def test_go_image_entrypoint_executes_bound_dynamic_tool_and_retains_context(self):
        import go_test_image
        image = self.root / "source-image"
        shutil.copyfile(Path("/usr/bin/true").resolve(), image)
        image.chmod(0o500)
        # This is a real wrapper and real ELF, but deliberately no application
        # test census. The entrypoint may only report source-pinned execution.
        self.tool.chmod(0o700)
        self.tool.write_text("#!" + str(Path(sys.executable).resolve()) + "\n"
            "import os,subprocess,sys\n"
            "assert sys.argv[1]=='-t'\n"
            "assert os.environ['GOWORK']=='off' and 'GOFLAGS' not in os.environ\n"
            "sys.exit(subprocess.run(sys.argv[2:]).returncode)\n")
        self.tool.chmod(0o500)
        # The public entrypoint accepts only its reviewed environment fields.
        self.go.chmod(0o700)
        self.go.write_text("#!" + str(Path(sys.executable).resolve()) + "\n"
            "import sys\n"
            "if sys.argv[1:]==['version']: print('go version synthetic-context-fixture')\n"
            "elif sys.argv[1:]==['tool','-n','test2json']: print(" + repr(str(self.tool)) + ")\n"
            "else: sys.exit(2)\n")
        self.go.chmod(0o500)
        self.sha = hashlib.sha256(self.go.read_bytes()).hexdigest()
        python = bind_executable(Path(sys.executable).resolve())
        recipe = {"schema": go_test_image.SCHEMA, "expected_uid": os.getuid(),
            "source_image": {"path": str(image), "sha256": hashlib.sha256(image.read_bytes()).hexdigest()},
            "cwd": str(self.cwd), "arguments": ["-test.run=^SyntheticNoApplicationCensus$", "-test.count=1"],
            "timeout_seconds": 10, "minimum_free_bytes": 0,
            "forecast": {"retained_image_bytes": image.stat().st_size, "log_bytes": 65536},
            "environment": {"PATH": str(self.bin), "GOENV": "off", "GOTOOLCHAIN": "local"},
            "go_tool": {"path": str(self.go), "sha256": self.sha},
            "runner_python": {key: python[key] for key in ("path", "sha256")}}
        recipe_path = self.root / "recipe.json"
        recipe_path.write_text(json.dumps(recipe))
        output = self.root / "image-result"
        with mock.patch.object(go_test_image, "MINIMUM_FREE_BYTES", 0):
            go_test_image.run(recipe_path, output)
        receipt = json.loads((output / "receipt.json").read_text())
        self.assertEqual(receipt["status"], "SOURCE_PINNED_EXECUTION_ONLY")
        self.assertEqual(receipt["execution"]["argv"][0], str(self.tool))
        self.assertTrue(receipt["execution"]["tree_joined"])
        self.assertEqual(receipt["execution"]["exit"], 0)
        self.assertEqual(receipt["go_tools"]["child_context"], receipt["execution"]["child_context"])

    def process(self, proc, pid, comm, argv, executable, state="S"):
        entry = proc / str(pid)
        entry.mkdir()
        fields = [state, "1"] + ["0"] * 17 + [str(1000 + pid)]
        (entry / "stat").write_text(str(pid) + " (" + comm + ") " + " ".join(fields))
        (entry / "cmdline").write_bytes(b"\0".join(os.fsencode(arg) for arg in argv) + b"\0")
        (entry / "exe").symlink_to(executable)
        return entry

    def test_compiler_census_counts_bare_absolute_and_global_c_argv(self):
        proc = self.root / "proc"
        proc.mkdir()
        self.process(proc, 10, "go", ["go", "test", "-c"], "/synthetic/go/bin/go")
        self.process(proc, 11, "go", ["/synthetic/go/bin/go", "test", "-c", "-race"], "/synthetic/go/bin/go")
        self.process(proc, 12, "go", ["go", "-C", "/source", "test", "-c=true"], "/synthetic/go/bin/go")
        self.process(proc, 13, "go", ["go", "tool", "test2json", "-c"], "/synthetic/go/bin/go")
        self.process(proc, 14, "compile", ["compile", "-p", "example"], "/synthetic/go/compile")
        self.process(proc, 15, "rustc", ["rustc", "example.rs"], "/synthetic/rustc")
        self.process(proc, 16, "go", ["go", "test", "-c"], "/synthetic/go/bin/go", "Z")
        old_row = " 11 1 go /synthetic/go/bin/go test -c -race"
        self.assertIsNone(re.match(r"\s*\d+\s+\d+\s+go\s+go test -c(?: |$)", old_row))
        census = compiler_census(proc)
        self.assertEqual([row["pid"] for row in census["go_test_compilers"]], [10, 11, 12])
        self.assertEqual([row["pid"] for row in census["go_workers"]], [14])
        self.assertEqual([row["pid"] for row in census["rustc"]], [15])

    def test_unobservable_live_compiler_cannot_disappear_from_admission(self):
        proc = self.root / "proc"
        proc.mkdir()
        entry = self.process(proc, 10, "go", ["go", "test", "-c"], "/synthetic/go/bin/go")
        (entry / "cmdline").write_bytes(b"")
        with self.assertRaisesRegex(Refused, "argv is unobservable"):
            compiler_census(proc)
        (entry / "exe").unlink()
        with self.assertRaisesRegex(Refused, "executable is unobservable"):
            compiler_census(proc)

    def test_compiler_census_retries_executable_loss_until_same_generation_exits(self):
        proc = self.root / "proc"
        proc.mkdir()
        entry = self.process(proc, 10, "compile", ["compile", "-p", "synthetic"], "/synthetic/compile")
        original = (entry / "stat").read_text()
        calls = []
        def cut(path):
            self.assertEqual(path, entry / "exe")
            calls.append(path)
            if len(calls) == 2:
                (entry / "stat").write_text(original.replace(") S ", ") Z "))
            raise FileNotFoundError("synthetic exiting executable")
        with mock.patch("child_context.os.readlink", side_effect=cut):
            result = compiler_census(proc)
        self.assertEqual(len(calls), 2)
        self.assertEqual(result, {"go_test_compilers": [], "go_workers": [], "rustc": []})

    def test_compiler_census_retries_executable_loss_without_losing_live_worker(self):
        proc = self.root / "proc"
        proc.mkdir()
        entry = self.process(proc, 10, "compile", ["compile", "-p", "synthetic"], "/synthetic/compile")
        with mock.patch("child_context.os.readlink", side_effect=[
                FileNotFoundError("synthetic exec cut"), "/synthetic/compile", "/synthetic/compile"]):
            result = compiler_census(proc)
        self.assertEqual([(row["pid"], row["starttime"]) for row in result["go_workers"]], [(10, "1010")])

    def test_compiler_census_empty_argv_checks_exit_before_refusing(self):
        proc = self.root / "proc"
        proc.mkdir()
        entry = self.process(proc, 10, "compile", ["compile"], "/synthetic/compile")
        original = (entry / "stat").read_text()
        read_bytes = Path.read_bytes
        def cut(path):
            if path == entry / "cmdline":
                (entry / "stat").write_text(original.replace(") S ", ") Z "))
                return b""
            return read_bytes(path)
        with mock.patch.object(Path, "read_bytes", cut):
            self.assertEqual(compiler_census(proc)["go_workers"], [])

    def test_compiler_census_reuse_during_failed_read_is_never_exit(self):
        proc = self.root / "proc"
        proc.mkdir()
        entry = self.process(proc, 10, "compile", ["compile"], "/synthetic/compile")
        original = (entry / "stat").read_text()
        def cut(path):
            (entry / "stat").write_text(original.rsplit(" ", 1)[0].replace(") S ", ") Z ") + " 9999")
            raise FileNotFoundError("synthetic PID replacement")
        with mock.patch("child_context.os.readlink", side_effect=cut), \
                self.assertRaisesRegex(Refused, "PID reused"):
            compiler_census(proc)

    def test_compiler_census_persistent_permission_refusal_keeps_original_cause(self):
        proc = self.root / "proc"
        proc.mkdir()
        self.process(proc, 10, "compile", ["compile"], "/synthetic/compile")
        cause = PermissionError("synthetic inaccessible live executable")
        with mock.patch("child_context.os.readlink", side_effect=cause) as lookup, \
                self.assertRaisesRegex(Refused, "executable is unobservable") as caught:
            compiler_census(proc)
        self.assertEqual(lookup.call_count, 3)
        self.assertIs(caught.exception.__cause__, cause)

    def test_resource_guard_exception_retains_actual_wait_without_context_pass(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        cause = Refused("synthetic resource observer failure")
        def observe():
            raise cause
        guard = functools.partial(run_process, resource_observer=observe)
        with self.assertRaisesRegex(Refused, "synthetic resource observer failure") as caught:
            context.run([python, "-c", "import time; time.sleep(60)"],
                        self.output, "resource-failure", 15, process_guard=guard)
        self.assertIs(caught.exception, cause)
        reference = cause.qualification_process_reference
        result = replay_process_result(reference, require_context_verified=False)
        self.assertTrue(result["tree_joined"])
        self.assertIs(type(result["exit"]), int)
        self.assertEqual(result["guard_failure"]["detail"], str(cause))
        self.assertFalse(result["context_verified"])
        self.assertFalse((self.output / "resource-failure.process-context.json").exists())
        with self.assertRaisesRegex(Refused, "postcheck is incomplete"):
            replay_process_result(reference)
        with self.assertRaises(ProcessLookupError):
            os.kill(result["pid"], 0)

    def test_sampler_stop_exception_still_joins_and_retains_child(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        cause = Refused("synthetic sampler join failure")
        def stop():
            raise cause
        guard = functools.partial(run_process, before_tree_join=stop)
        with self.assertRaisesRegex(Refused, "synthetic sampler join failure"):
            context.run([python, "-c", "print('synthetic child done')"],
                        self.output, "sampler-failure", 15, process_guard=guard)
        result = replay_process_result(cause.qualification_process_reference, require_context_verified=False)
        self.assertEqual(result["exit"], 0)
        self.assertTrue(result["tree_joined"])
        self.assertFalse(result["context_verified"])
        self.assertEqual(cargo_control.owned_children(), [])

    def test_unverified_guard_cleanup_cannot_publish_a_joined_wait(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        join = cargo_control.join_process_tree
        def observe():
            raise Refused("synthetic observer failure")
        def unavailable(process, failed):
            join(process, failed)
            raise Refused("synthetic cleanup proof unavailable")
        with mock.patch.object(cargo_control, "join_process_tree", side_effect=unavailable), \
                self.assertRaisesRegex(Refused, "cleanup proof unavailable") as caught:
            context.run([python, "-c", "import time; time.sleep(60)"], self.output,
                        "unverified-cleanup", 15,
                        process_guard=functools.partial(run_process, resource_observer=observe))
        self.assertIsNone(getattr(caught.exception, "qualification_process_result", None))
        self.assertFalse((self.output / "unverified-cleanup.process-result.json").exists())
        self.assertEqual(cargo_control.owned_children(), [])

    def test_owner_resource_guard_keeps_failure_and_actual_wait(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        original_read = Path.read_text
        def memory(path, *args, **kwargs):
            if path == Path("/proc/meminfo"):
                return "MemAvailable: 200000000 kB\n"
            return original_read(path, *args, **kwargs)
        disk = SimpleNamespace(free=500000000000)
        stopped = []
        with mock.patch.object(Path, "read_text", memory), \
                mock.patch.object(owner_resource_guard.shutil, "disk_usage", return_value=disk), \
                mock.patch.object(owner_resource_guard, "resource_observation_failed", True), \
                mock.patch.object(owner_resource_guard, "before_tree_join", lambda: stopped.append(True)), \
                self.assertRaisesRegex(Refused, "owned resource observation failed") as caught:
            context.run([python, "-c", "import time; time.sleep(60)"], self.output,
                        "owner-failure", 15, process_guard=owner_resource_guard.run_process)
        result = replay_process_result(caught.exception.qualification_process_reference,
                                       require_context_verified=False)
        self.assertTrue(stopped)
        self.assertTrue(result["tree_joined"])
        self.assertEqual(result["guard_failure"]["detail"], "owned resource observation failed")
        self.assertFalse(result["context_verified"])
        self.assertEqual(cargo_control.owned_children(), [])

    def test_reused_exception_cannot_relabel_a_prior_actual_wait(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        cause = Refused("synthetic reused observer exception")
        def observe():
            raise cause
        argv = [python, "-c", "import time; time.sleep(60)"]
        with self.assertRaises(Refused):
            context.run(argv, self.output, "original-wait", 15,
                        process_guard=functools.partial(run_process, resource_observer=observe))
        original = cause.qualification_process_reference
        with mock.patch.object(cargo_control.subprocess, "Popen", side_effect=cause), \
                self.assertRaises(Refused):
            context.run(argv, self.output, "not-restarted", 15)
        self.assertIsNone(cause.qualification_process_result)
        self.assertIsNone(cause.qualification_process_reference)
        self.assertFalse((self.output / "not-restarted.process-result.json").exists())
        self.assertTrue(replay_process_result(original, require_context_verified=False)["tree_joined"])

    def test_process_creation_failure_cannot_fabricate_an_actual_wait(self):
        context = ChildContext(self.cwd, self.environment)
        python = str(Path(sys.executable).resolve())
        context.bind(python)
        cause = OSError("synthetic exec refusal")
        with mock.patch.object(cargo_control.subprocess, "Popen", side_effect=cause), \
                self.assertRaisesRegex(OSError, "synthetic exec refusal") as caught:
            context.run([python, "-c", "pass"], self.output, "not-started", 15)
        self.assertIs(caught.exception, cause)
        self.assertFalse((self.output / "not-started.process-result.json").exists())


if __name__ == "__main__":
    unittest.main()
