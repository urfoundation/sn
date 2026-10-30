#!/usr/bin/env python3
"""Exercise the candidate's real pre-build guards without Git or Rust work."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


HERE = Path(__file__).resolve().parent
BUILD = HERE / "build.sh"
COMMIT = "8cf229f8ef969389d054a4c161b53ffd164a041f"
TREE = "4cfd29d8d12499775e2b1cbabf64e6fa41f1f7cf"
OLD_COMMIT = "6a972693ae9580541225bd37696ac2fee26b8ae9"
OLD_TREE = "587abd7462d0cee0de4d0ec8bdf0df6a71cd947f"


class Runtime482BuildGuards(unittest.TestCase):
    def run_guard(self, *, old_tag=False, drift=False, wrong_spec=False):
        self.assertIsNotNone(shutil.which("bash"))
        self.assertIsNotNone(shutil.which("sha256sum"))
        with tempfile.TemporaryDirectory(prefix="runtime482-build-guards-") as tmp:
            base = Path(tmp)
            script = base / "sn/scripts/runtime-rehearsal/build.sh"
            script.parent.mkdir(parents=True)
            script.write_bytes(BUILD.read_bytes())
            source = base / "subtensor"
            runtime = source / "runtime/src/lib.rs"
            runtime.parent.mkdir(parents=True)
            runtime.write_text("    spec_version: " + ("481" if wrong_spec else "482") + ",\n")
            manifest = base / "sn/docs/spec/runtime-v482-source.sha256"
            manifest.parent.mkdir(parents=True)
            manifest.write_text(
                hashlib.sha256(runtime.read_bytes()).hexdigest()
                + "  runtime/src/lib.rs\n"
            )
            if drift:
                runtime.write_text(runtime.read_text() + "// changed source bytes\n")
            tools = base / "tools"
            tools.mkdir()
            git = tools / "git"
            git.write_text(
                "#!" + sys.executable + "\n"
                + "import os, sys\n"
                + "args = sys.argv[1:]\n"
                + "commit = " + repr(COMMIT) + "\n"
                + "tree = " + repr(TREE) + "\n"
                + "old_commit = " + repr(OLD_COMMIT) + "\n"
                + "old_tree = " + repr(OLD_TREE) + "\n"
                + "if args == ['ls-remote', '--tags', "
                + "'https://github.com/RaoFoundation/subtensor.git', "
                + "'refs/tags/v482', 'refs/tags/v482^{}']:\n"
                + "    print(os.environ['CONTROL_TAG_COMMIT'] + '\\trefs/tags/v482')\n"
                + "    sys.exit(0)\n"
                + "if args[:2] != ['-C', os.environ['CONTROL_SOURCE']]:\n"
                + "    sys.exit('unexpected Git invocation: ' + repr(args))\n"
                + "args = args[2:]\n"
                + "if args == ['cat-file', '-e', commit + '^{commit}'] "
                + "or args == ['cat-file', '-e', old_commit + '^{commit}']:\n"
                + "    pass\n"
                + "elif args == ['checkout', '--quiet', '--detach', commit]:\n"
                + "    pass\n"
                + "elif args == ['rev-parse', commit + '^{tree}']:\n"
                + "    print(tree)\n"
                + "elif args == ['rev-parse', old_commit + '^{tree}']:\n"
                + "    print(old_tree)\n"
                + "elif args == ['rev-parse', 'HEAD']:\n"
                + "    print(commit)\n"
                + "elif args == ['status', '--porcelain=v1', '--untracked-files=all']:\n"
                + "    pass\n"
                + "else:\n"
                + "    sys.exit('unexpected Git invocation: ' + repr(args))\n"
            )
            git.chmod(0o700)
            marker = base / "cargo-reached.json"
            cargo = tools / "cargo"
            cargo.write_text(
                "#!" + sys.executable + "\n"
                + "import json, os, pathlib, sys\n"
                + "pathlib.Path(os.environ['CONTROL_CARGO_MARKER']).write_text("
                + "json.dumps(sys.argv[1:]))\n"
                + "print('control reached cargo without building', file=sys.stderr)\n"
                + "sys.exit(73)\n"
            )
            cargo.chmod(0o700)
            protoc = tools / "protoc"
            protoc.write_text("#!/bin/sh\nexit 0\n")
            protoc.chmod(0o700)
            env = os.environ.copy()
            env.update({
                "PATH": str(tools) + os.pathsep + env.get("PATH", ""),
                "CONTROL_SOURCE": str(source),
                "CONTROL_TAG_COMMIT": OLD_COMMIT if old_tag else COMMIT,
                "CONTROL_CARGO_MARKER": str(marker),
                "CARGO_TARGET_DIR": str(base / "target"),
                "RUNTIME_REHEARSAL_JOBS": "1",
                "PROTOC": str(protoc),
            })
            completed = subprocess.run(
                ["bash", str(script), str(source)],
                env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                timeout=20, check=False,
            )
            reached = json.loads(marker.read_text()) if marker.exists() else None
            return completed, reached

    def test_reviewed_source_reaches_only_the_cargo_sentinel(self):
        completed, reached = self.run_guard()
        self.assertEqual(completed.returncode, 73, completed.stderr)
        self.assertEqual(reached, [
            "build", "--locked", "--profile", "release", "-p", "node-subtensor",
            "--features", "metadata-hash fast-runtime", "-j", "1",
        ])
        self.assertIn("control reached cargo without building", completed.stderr)

    def test_runtime481_tag_tree_is_refused_before_cargo(self):
        completed, reached = self.run_guard(old_tag=True)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIsNone(reached)
        self.assertIn(
            "tag v482 names " + OLD_COMMIT + ", whose tree is not the reviewed " + TREE,
            completed.stderr,
        )

    def test_previous_spec_is_refused_before_cargo(self):
        completed, reached = self.run_guard(wrong_spec=True)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIsNone(reached)
        self.assertIn("does not declare spec_version 482", completed.stderr)

    def test_changed_source_bytes_fail_the_actual_manifest_guard(self):
        completed, reached = self.run_guard(drift=True)
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIsNone(reached)
        self.assertIn("checkout differs from", completed.stderr)
        self.assertIn("runtime-v482-source.sha256", completed.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
