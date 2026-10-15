"""Actual Python/virtualenv invocation regressions; no application dependencies."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest import mock
import venv

from cargo_control import Refused, run_process
from child_context import ChildContext, bind_python_interpreter, replay_process_result


class PythonInterpreterTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.venv = self.root / "synthetic environment with spaces"
        venv.EnvBuilder(with_pip=False, symlinks=True).create(self.venv)
        self.python = self.venv / "bin/python3"
        self.assertTrue(self.python.is_symlink(), "fixture requires a real virtualenv interpreter symlink")
        self.real_python = self.python.resolve(strict=True)
        self.sha = hashlib.sha256(self.real_python.read_bytes()).hexdigest()
        self.cwd, self.output = self.root / "working", self.root / "evidence"
        self.cwd.mkdir()
        self.output.mkdir()
        self.environment = {"PATH": str(self.venv / "bin"), "LC_ALL": "C.UTF-8",
                            "PYTHONDONTWRITEBYTECODE": "1", "PYTHONNOUSERSITE": "1"}
        site = self.venv / "lib" / f"python{sys.version_info.major}.{sys.version_info.minor}" / "site-packages"
        site.mkdir(parents=True, exist_ok=True)
        # Two synthetic optional dependencies model the separate missing-import
        # outcomes without relying on installed yaml, ansible or network access.
        (site / "qualification_yaml_fixture.py").write_text("VALUE = 'synthetic-yaml'\n")
        (site / "qualification_ansible_fixture.py").write_text("VALUE = 'synthetic-ansible'\n")
        package = site / "qualification_venv_fixture"
        package.mkdir()
        (package / "__init__.py").write_text(
            "import qualification_yaml_fixture, qualification_ansible_fixture\n"
            "VALUE = qualification_yaml_fixture.VALUE + ':' + qualification_ansible_fixture.VALUE\n")
        (package / "test_imports.py").write_text(
            "import unittest\nfrom qualification_venv_fixture import VALUE\n"
            "class SyntheticImports(unittest.TestCase):\n"
            " def test_yaml(self): self.assertIn('synthetic-yaml', VALUE)\n"
            " def test_ansible(self): self.assertIn('synthetic-ansible', VALUE)\n")
        self.program = ("import json,sys; from qualification_venv_fixture import VALUE; "
                        "print(json.dumps({'value':VALUE,'prefix':sys.prefix,'executable':sys.executable}))")
        self.script = self.cwd / "synthetic-command.py"
        self.script.write_text(self.program + "\n")

    def context(self):
        context = ChildContext(self.cwd, self.environment)
        binding = context.bind_python(self.python, self.sha)
        return context, binding

    def test_literal_virtualenv_path_preserves_command_script_and_unittest_imports(self):
        context, binding = self.context()
        self.assertEqual(binding["path"], str(self.python))
        self.assertEqual(binding["target"]["path"], str(self.real_python))
        self.assertEqual(binding["target"]["sha256"], self.sha)
        commands = [("command", ["-c", self.program]), ("script", [str(self.script)]),
                    ("unittest", ["-m", "unittest", "-v", "qualification_venv_fixture.test_imports"])]
        for label, arguments in commands:
            result = context.run([binding["path"], "-B", *arguments], self.output, label, 15)
            self.assertEqual(result["exit"], 0, (self.output / (label + ".stderr")).read_text())
            self.assertTrue(result["tree_joined"])
            self.assertEqual(result["argv"][0], str(self.python))
            retained = json.loads(Path(result["process_result"]["path"]).read_text())
            self.assertEqual(retained["executable"], binding)
            if label != "unittest":
                observed = json.loads((self.output / (label + ".stdout")).read_text())
                self.assertEqual(observed["prefix"], str(self.venv))
                self.assertEqual(observed["executable"], str(self.python))
                self.assertEqual(observed["value"], "synthetic-yaml:synthetic-ansible")
            else:
                self.assertIn("Ran 2 tests", (self.output / (label + ".stderr")).read_text())

    def test_resolving_the_same_interpreter_reproduces_missing_virtualenv_imports(self):
        context, binding = self.context()
        bare = context.bind(self.real_python, self.sha)
        self.assertEqual(binding["sha256"], bare["sha256"])
        arguments = ["-B", "-m", "unittest", "-v", "qualification_venv_fixture.test_imports"]
        original = context.run([bare["path"], *arguments], self.output, "resolved-failure", 15)
        self.assertEqual(original["exit"], 1)
        self.assertIn("No module named 'qualification_venv_fixture'",
                      (self.output / "resolved-failure.stderr").read_text())
        corrected = context.run([binding["path"], *arguments], self.output, "literal-success", 15)
        self.assertEqual(corrected["exit"], 0)
        self.assertEqual(replay_process_result(original["process_result"])["exit"], 1)

    def test_actual_runner_receipt_keeps_venv_path_and_real_interpreter_separately(self):
        context, binding = self.context()
        helper_root = str(Path(__file__).resolve().parent)
        code = ("import json,os,sys; sys.path.insert(0," + repr(helper_root) + "); "
                "from child_context import ChildContext; "
                "print(json.dumps(ChildContext(os.getcwd(),dict(os.environ)).receipt()))")
        result = context.run([binding["path"], "-B", "-c", code], self.output, "runner", 15)
        self.assertEqual(result["exit"], 0, (self.output / "runner.stderr").read_text())
        receipt = json.loads((self.output / "runner.stdout").read_text())
        self.assertEqual(receipt["runner_invocation"]["path"], str(self.python))
        self.assertEqual(receipt["runner_executable"]["path"], str(self.real_python))
        self.assertEqual(receipt["runner_invocation"]["target"], receipt["runner_executable"])

    def test_replaced_symlink_with_the_same_target_refuses_before_launch(self):
        context, binding = self.context()
        self.python.rename(self.root / "original-python-link")
        self.python.symlink_to(self.real_python)
        with self.assertRaisesRegex(Refused, "executable identity changed"):
            context.run([binding["path"], "-c", "pass"], self.output, "changed-link", 15)
        self.assertFalse((self.output / "changed-link.stdout").exists())

    def test_retargeted_symlink_with_identical_elf_bytes_refuses_before_launch(self):
        context, binding = self.context()
        replacement = self.root / "different-python-origin"
        shutil.copyfile(self.real_python, replacement)
        replacement.chmod(0o500)
        self.python.rename(self.root / "original-python-link")
        self.python.symlink_to(replacement)
        with self.assertRaisesRegex(Refused, "executable identity changed"):
            context.run([binding["path"], "-c", "pass"], self.output, "changed-target", 15)
        self.assertFalse((self.output / "changed-target.process-result.json").exists())

    def test_changed_or_new_higher_priority_virtualenv_config_refuses_before_launch(self):
        for index, path in enumerate((self.venv / "pyvenv.cfg", self.python.parent / "pyvenv.cfg")):
            context, binding = self.context()
            original = path.read_bytes() if path.exists() else None
            path.write_text("home = /synthetic/changed-python\ninclude-system-site-packages = true\n")
            with self.assertRaisesRegex(Refused, "executable identity changed"):
                context.run([binding["path"], "-c", "pass"], self.output, f"changed-config-{index}", 15)
            self.assertFalse((self.output / f"changed-config-{index}.stdout").exists())
            if original is None:
                path.unlink()
            else:
                path.write_bytes(original)

    def test_postwait_config_change_retains_wait_without_context_success(self):
        context, binding = self.context()
        def change_after_join(*args, **kwargs):
            result = run_process(*args, **kwargs)
            with (self.venv / "pyvenv.cfg").open("a") as stream:
                stream.write("# synthetic post-wait change\n")
            return result
        with self.assertRaisesRegex(Refused, "executable identity changed"):
            context.run([binding["path"], "-c", self.program], self.output, "postwait", 15,
                        process_guard=change_after_join)
        path = self.output / "postwait.process-result.json"
        reference = {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
        result = replay_process_result(reference, require_context_verified=False)
        self.assertEqual(result["exit"], 0)
        self.assertTrue(result["tree_joined"])
        self.assertFalse(result["context_verified"])
        self.assertFalse((self.output / "postwait.process-context.json").exists())

    def test_strict_tool_binding_and_wrong_python_pin_still_refuse(self):
        context = ChildContext(self.cwd, self.environment)
        with self.assertRaisesRegex(Refused, "exact absolute origin"):
            context.bind(self.python, self.sha)
        with self.assertRaisesRegex(Refused, "SHA256 pin"):
            context.bind_python(self.python, "0" * 64)
        self.assertEqual(list(self.output.iterdir()), [])

    def test_python_path_cycles_and_parent_aliases_remain_refused(self):
        loop = self.root / "loop"
        loop.symlink_to(loop.name)
        with self.assertRaisesRegex(Refused, "symlink cycle"):
            bind_python_interpreter(loop)
        alias = self.root / "alias"
        alias.symlink_to(self.venv, target_is_directory=True)
        with self.assertRaisesRegex(Refused, "parent aliases"):
            bind_python_interpreter(alias / "bin/python3")

    def test_link_replacement_during_target_binding_is_refused(self):
        import child_context
        original = child_context.bind_executable
        def replace_after_binding(*args, **kwargs):
            result = original(*args, **kwargs)
            self.python.rename(self.root / "original-python-link")
            self.python.symlink_to(self.real_python)
            return result
        with mock.patch.object(child_context, "bind_executable", side_effect=replace_after_binding):
            with self.assertRaisesRegex(Refused, "link changed while binding"):
                bind_python_interpreter(self.python, self.sha)


if __name__ == "__main__":
    unittest.main()
