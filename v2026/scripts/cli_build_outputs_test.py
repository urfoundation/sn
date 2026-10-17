"""The release CLIs keep generated outputs out of source provenance.

Run with python3 -B scripts/cli_build_outputs_test.py. Uses disposable Git
repositories and the actual CLI ignore files; no compilation or network.
"""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
TOOLS = {"miner": "provider", "validator": "validator", "snclaim": "snclaim"}


class CliBuildOutputs(unittest.TestCase):
    def git(self, root, *args, check=True):
        return subprocess.run(["git", "-C", str(root), *args], check=check,
                              capture_output=True, text=True, timeout=15)

    def fixture(self, version):
        temporary = tempfile.TemporaryDirectory(prefix="sn-cli-outputs-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        shutil.copyfile(ROOT / ".gitignore", root / ".gitignore")
        for name in TOOLS:
            directory = root / "cli" / name
            directory.mkdir(parents=True)
            ignore = ROOT / "cli" / name / ".gitignore"
            if ignore.is_file():
                shutil.copyfile(ignore, directory / ".gitignore")
            (directory / "main.go").write_text("package main\n")
        if version:
            (root / version).mkdir()
            # go_mod_fork moves the cli tree intact, including nested dotfiles.
            (root / "cli").rename(root / version / "cli")
        self.git(root, "init", "-q")
        self.git(root, "config", "user.name", "Output Fixture")
        self.git(root, "config", "user.email", "fixture@example.invalid")
        self.git(root, "config", "core.excludesFile", "/dev/null")
        self.git(root, "add", ".")
        self.git(root, "commit", "-qm", "fixture sealed source")
        return root, root / version / "cli"

    def test_outputs_are_ignored_in_main_and_versioned_release_layouts(self):
        for version in ("", "v2026", "v2027"):
            with self.subTest(version=version):
                root, cli = self.fixture(version)
                for name, binary in TOOLS.items():
                    outputs = (f"linux/arm64/{binary}", f"windows/amd64/{binary}.exe", f"{binary}.tar.gz")
                    for output in outputs:
                        target = cli / name / "build" / output
                        target.parent.mkdir(parents=True, exist_ok=True)
                        target.write_bytes(b"generated release artifact")
                        result = self.git(root, "check-ignore", "--no-index", str(target), check=False)
                        self.assertEqual(result.returncode, 0, str(target))
                self.assertEqual(self.git(root, "status", "--porcelain=v1", "--untracked-files=all").stdout, "")

    def test_adjacent_untracked_and_tracked_sources_remain_visible(self):
        for version in ("", "v2026"):
            with self.subTest(version=version):
                root, cli = self.fixture(version)
                for name in TOOLS:
                    for relative in ("extra.go", "build_source.go", "nested/build/extra.go"):
                        target = cli / name / relative
                        target.parent.mkdir(parents=True, exist_ok=True)
                        target.write_text("package main\n")
                        result = self.git(root, "check-ignore", "--no-index", str(target), check=False)
                        self.assertEqual(result.returncode, 1, str(target))
                    tracked = cli / name / "main.go"
                    tracked.write_text("package main // changed source\n")
                    self.assertIn(str(tracked.relative_to(root)), self.git(root, "diff", "--name-only", "HEAD").stdout)
                    self.git(root, "add", str(tracked))
                    self.assertIn(str(tracked.relative_to(root)), self.git(root, "diff", "--cached", "--name-only").stdout)
                other = cli / "unrelated/build/source.go"
                other.parent.mkdir(parents=True)
                other.write_text("package unrelated\n")
                self.assertEqual(self.git(root, "check-ignore", "--no-index", str(other), check=False).returncode, 1)


if __name__ == "__main__":
    unittest.main()
