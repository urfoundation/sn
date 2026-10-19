"""Actual file/commit changes exercise the owner and package boundaries."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

from cargo_control import Refused
from joint_peer_admission import pin
from owner_input_admission import OwnerInputAdmission, verify_git_input_scope


class OwnerInputs(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.members = {}
        for role in ("rust", "go"):
            d = self.root / role
            d.mkdir()
            (d / "runner.py").write_text("pass\n")
            unit = "test-" + role + ".service"
            limits = {"MemoryMax": 8 * 1024**3, "MemorySwapMax": 0}
            self.members[role] = {"unit": unit, "uid": 1000, "manager": "user",
                "cgroup": "/user.slice/user-1000.slice/user@1000.service/app.slice/" + unit,
                "python_argv": "/usr/bin/python3", "runner_path": str(d / "runner.py"),
                "job_path": str(d / "job.json"), "adoption_path": str(d / "adoption.json"),
                "memory_max": limits["MemoryMax"], "execution_limits": limits,
                "argv": ["/usr/bin/python3", str(d / "runner.py"), str(d / "job.json")]}
            (d / "job.json").write_text(json.dumps({"systemd_unit": unit,
                "adoption_path": str(d / "adoption.json"), "execution_limits": limits}))
        self.plan = self.root / "plan.json"
        self.plan.write_text(json.dumps({"schema": "urnetwork-owner-local-input-cohort-v1",
                                         "members": self.members}))
        for role in self.members:
            self.adopt(role)

    def adopt(self, role):
        d = self.root / role
        (d / "adoption.json").write_text(json.dumps({"focused_pair_admission": pin(self.plan),
            "job_sha256": pin(d / "job.json")["sha256"],
            "runner_sha256": pin(d / "runner.py")["sha256"]}))

    def owner(self, role):
        d = self.root / role
        return OwnerInputAdmission(d / "job.json", d / "runner.py", d / "adoption.json", role)

    def test_unstarted_peer_reprepare_preserves_active_owner(self):
        rust = self.owner("rust")
        p = self.root / "go/job.json"
        value = json.loads(p.read_text()); value["source_admission_note"] = "new exact source"
        p.write_text(json.dumps(value)); (self.root / "go/runner.py").write_text("value = 2\n")
        self.adopt("go")
        rust.verify(); self.owner("go").verify()

    def test_own_active_bytes_and_adoption_remain_immutable(self):
        owner = self.owner("rust")
        (self.root / "rust/runner.py").write_text("value = 3\n")
        with self.assertRaises(Refused): owner.verify()
        self.adopt("rust")
        with self.assertRaises(Refused): owner.verify()

    def test_shared_caps_and_member_paths_remain_immutable(self):
        owner = self.owner("rust")
        value = json.loads(self.plan.read_text()); value["members"]["go"]["memory_max"] *= 2
        self.plan.write_text(json.dumps(value))
        with self.assertRaises(Refused): owner.verify()
        self.adopt("go")
        with self.assertRaises(Refused): self.owner("go")

    def repository(self):
        d = self.root / "repo"; d.mkdir()
        def git(*args):
            return subprocess.check_output(["git", "-C", str(d), *args], stderr=subprocess.DEVNULL).decode().strip()
        git("init", "-q"); git("config", "user.name", "Fixture"); git("config", "user.email", "fixture@example.invalid")
        (d / "selected").mkdir(); (d / "unreached").mkdir()
        (d / "selected/input.go").write_text("package selected\n")
        (d / "go.mod").write_text("module fixture\n")
        git("add", "."); git("commit", "-qm", "fixture")
        info = d.stat()
        spec = {"schema": "urnetwork-selected-git-input-scope-v1", "complete_input_scope": True,
            "repository": str(d), "resolved_repository": str(d), "device": info.st_dev,
            "inode": info.st_ino, "base_commit": git("rev-parse", "HEAD"),
            "input_files": ["go.mod", "go.sum"], "package_directories": ["selected"],
            "input_trees": ["selected/assets"]}
        return d, spec

    def test_unrelated_source_and_docs_do_not_change_selected_inputs(self):
        d, spec = self.repository()
        (d / "unreached/change.go").write_text("package unreached\n")
        (d / "new-doc.md").write_text("documentation\n")
        self.assertEqual(verify_git_input_scope(spec)["selected_input_changes"], [])

    def test_source_embed_and_module_changes_are_rejected(self):
        d, spec = self.repository()
        for relative in ("selected/new.go", "selected/assets/new.bin", "go.sum"):
            p = d / relative; p.parent.mkdir(exist_ok=True); p.write_text("changed\n")
            with self.assertRaises(Refused): verify_git_input_scope(spec)
            p.unlink()
        spec["input_trees"] = ["."]
        (d / "unreached/new.go").write_text("package unreached\n")
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_dependency_symlink_or_filesystem_rebind_is_rejected(self):
        d, spec = self.repository()
        link = self.root / "dependency"; link.symlink_to(d)
        spec["repository"] = str(link); verify_git_input_scope(spec)
        other = self.root / "other"; other.mkdir(); link.unlink(); link.symlink_to(other)
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_ignored_compiler_input_is_rejected(self):
        d, spec = self.repository()
        (d / ".gitignore").write_text("selected/hidden.go\n")
        (d / "selected/hidden.go").write_text("package selected\n")
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_index_flags_cannot_hide_changed_physical_source(self):
        d, spec = self.repository()
        for flag in ("--assume-unchanged", "--skip-worktree"):
            subprocess.check_call(["git", "-C", str(d), "update-index", flag, "selected/input.go"])
            (d / "selected/input.go").write_text("package corrupted\n")
            with self.assertRaises(Refused): verify_git_input_scope(spec)
            (d / "selected/input.go").write_text("package selected\n")
            subprocess.check_call(["git", "-C", str(d), "update-index", "--no-assume-unchanged",
                                   "--no-skip-worktree", "selected/input.go"])

    def test_selected_file_symlink_is_rejected(self):
        d, spec = self.repository()
        external = self.root / "outside.go"; external.write_text("package selected\n")
        (d / "selected/input.go").unlink(); (d / "selected/input.go").symlink_to(external)
        subprocess.check_call(["git", "-C", str(d), "add", "."])
        subprocess.check_call(["git", "-C", str(d), "commit", "-qm", "selected symlink"])
        spec["base_commit"] = subprocess.check_output(["git", "-C", str(d), "rev-parse", "HEAD"]).decode().strip()
        external.write_text("package changed_without_git_link_change\n")
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_selected_source_rename_outside_scope_is_rejected(self):
        d, spec = self.repository()
        subprocess.check_call(["git", "-C", str(d), "mv", "selected/input.go", "unreached/input.go"])
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_unrelated_subtrees_are_not_walked_and_selected_ancestor_links_refuse(self):
        d, spec = self.repository()
        original_walk = os.walk
        visited = []
        def walk(*args, **kwargs):
            for row in original_walk(*args, **kwargs):
                visited.append(Path(row[0]))
                yield row
        (d / "unreached/large/peer/generated").mkdir(parents=True)
        with mock.patch("owner_input_admission.os.walk", side_effect=walk):
            verify_git_input_scope(spec)
        self.assertNotIn(d / "unreached", visited)
        (d / "selected").rename(d / "outside-selected")
        (d / "selected").symlink_to(d / "outside-selected", target_is_directory=True)
        spec["package_directories"] = []
        spec["input_files"] = ["selected/input.go"]
        spec["input_trees"] = []
        with self.assertRaises(Refused): verify_git_input_scope(spec)

    def test_fifo_replacement_at_open_is_nonblocking_and_refused(self):
        d, spec = self.repository()
        real_open = os.open
        target = d / "selected/input.go"
        observed_flags = []
        def replace_at_open(path, flags, *args, **kwargs):
            if Path(path) == target:
                observed_flags.append(flags)
                self.assertTrue(flags & os.O_NONBLOCK)
                self.assertTrue(flags & os.O_CLOEXEC)
                target.unlink()
                os.mkfifo(target)
            return real_open(path, flags, *args, **kwargs)
        with mock.patch("owner_input_admission.os.open", side_effect=replace_at_open):
            with self.assertRaises(Refused): verify_git_input_scope(spec)
        self.assertEqual(len(observed_flags), 1)


if __name__ == "__main__":
    unittest.main()
