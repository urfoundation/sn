"""Separate each owner's source authority from a shared resource reservation.

The shared plan fixes command paths, USER units and limits. An owner's adopted
record fixes its own bytes. Reading another owner's source is deliberately not
part of resource observation: an unstarted owner can be prepared again without
invalidating a healthy peer. Existing generation, ancestry, OOM, floor and
joined-wait checks remain the resource observer's responsibility.
"""
import hashlib
import os
from pathlib import Path, PurePosixPath
import subprocess
import stat

from cargo_control import require
from joint_peer_admission import pin, pinned_json


class OwnerInputAdmission:
    """Only the caller's immutable adoption/job/runner bytes confer admission."""

    def __init__(self, job_path, runner_path, adoption_path, role):
        self.adoption = pin(adoption_path)
        adopted = pinned_json(self.adoption)
        self.plan_pin = adopted["focused_pair_admission"]
        self.plan = pinned_json(self.plan_pin)
        require(self.plan["schema"] == "urnetwork-owner-local-input-cohort-v1",
                "owner-local plan schema differs")
        require(role in self.plan["members"], "caller role is not selected")
        self.role = role
        for member in self.plan["members"].values():
            require(not ({"job", "runner", "job_sha256", "runner_sha256"} & set(member)),
                    "shared resource member must not carry source byte pins")
            require(member["argv"] == [member["python_argv"], member["runner_path"], member["job_path"]],
                    "shared member command differs")
            require(member["uid"] == 1000 and member["manager"] == "user" and
                    member["cgroup"] == "/user.slice/user-1000.slice/user@1000.service/app.slice/" + member["unit"],
                    "shared member USER namespace differs")
        member = self.plan["members"][role]
        require(str(job_path) == member["job_path"] and str(runner_path) == member["runner_path"] and
                str(adoption_path) == member["adoption_path"], "caller path selection differs")
        self.job_pin = {"path": str(job_path), "sha256": adopted["job_sha256"]}
        self.runner_pin = {"path": str(runner_path), "sha256": adopted["runner_sha256"]}
        job = pinned_json(self.job_pin)
        require(pin(runner_path) == self.runner_pin, "caller runner bytes differ")
        require(job["adoption_path"] == str(adoption_path) and job["systemd_unit"] == member["unit"],
                "caller job identity differs")
        require(job["execution_limits"] == member["execution_limits"] and
                job["execution_limits"]["MemoryMax"] == member["memory_max"] and
                job["execution_limits"]["MemorySwapMax"] == 0,
                "caller execution limits differ")
        self.source = job.get("source_admission")
        self.verify()

    def verify(self):
        require(pin(self.plan_pin["path"]) == self.plan_pin, "shared resource plan changed")
        require(pin(self.adoption["path"]) == self.adoption, "caller adoption changed")
        require(pin(self.job_pin["path"]) == self.job_pin, "caller job changed")
        require(pin(self.runner_pin["path"]) == self.runner_pin, "caller runner changed")
        if self.source is not None:
            require(pin(self.source["path"]) == self.source, "caller source authority changed")
        return {"plan": self.plan_pin, "role": self.role, "job": self.job_pin,
                "runner": self.runner_pin, "adoption": self.adoption, "source": self.source}


def _selected(path, spec):
    """Direct package files plus explicitly closed native/embed subtrees."""
    p = PurePosixPath(path)
    require(not p.is_absolute() and ".." not in p.parts, "invalid changed source path")
    return (path in spec["input_files"] or str(p.parent) in spec["package_directories"] or
            any(root == "." or path == root or path.startswith(root + "/")
                for root in spec["input_trees"]))


def _needed(path, spec):
    """Inspect selected entries and their ancestors, never unrelated subtrees."""
    return (_selected(path, spec) or path in spec["package_directories"] or
            any(value.startswith(path + "/") for value in
                spec["input_files"] + spec["package_directories"] + spec["input_trees"]))


def _descend(path, spec):
    return (path in spec["package_directories"] or
            any(root == "." or path == root or path.startswith(root + "/")
                for root in spec["input_trees"]) or
            any(value.startswith(path + "/") for value in
                spec["input_files"] + spec["package_directories"] + spec["input_trees"]))


def verify_git_input_scope(spec):
    """Verify a caller-authenticated complete package/input scope, not HEAD.

    This does not discover dependencies. The admitted caller must provide the
    complete selected package graph and all module, native, embedded, generated
    and configuration inputs. Changes outside that scope cannot qualify a new
    dependency graph. Tool/environment custody remains with ChildContext.
    """
    require(spec["schema"] == "urnetwork-selected-git-input-scope-v1" and
            spec["complete_input_scope"] is True, "complete source scope is not admitted")
    repository = Path(spec["repository"])
    resolved = repository.resolve(strict=True)
    require(str(resolved) == spec["resolved_repository"], "local dependency path changed")
    info = resolved.stat()
    require((info.st_dev, info.st_ino) == (spec["device"], spec["inode"]),
            "local dependency filesystem or inode changed")
    def git(*args):
        result = subprocess.check_output(["/usr/bin/git", "-C", str(repository), *args], timeout=30)
        require(len(result) <= 8 * 1024 * 1024, "source delta exceeds bound")
        return result.decode()
    # The Git index is not source custody: ignored files, assume-unchanged and
    # skip-worktree can all hide compiler inputs from an ordinary Git diff.
    expected = {}
    for row in git("ls-tree", "-rz", spec["base_commit"]).split("\0"):
        if not row:
            continue
        metadata, name = row.split("\t", 1)
        mode, kind, blob = metadata.split()
        if _selected(name, spec):
            require(kind == "blob" and mode in ("100644", "100755"),
                    "selected Git input is a symlink or non-regular object")
            expected[name] = (mode, blob)
    actual_files = set()
    entries = total = 0
    for directory, dirs, files in os.walk(resolved, followlinks=False):
        if Path(directory) == resolved and ".git" in dirs:
            dirs.remove(".git")
        for name in dirs + files:
            path = Path(directory) / name
            relative = path.relative_to(resolved).as_posix()
            if relative == ".git":
                continue
            if not _needed(relative, spec):
                if name in dirs:
                    dirs.remove(name)
                continue
            entries += 1
            require(entries <= 200000, "source namespace exceeds bound")
            before = path.lstat()
            require(not stat.S_ISLNK(before.st_mode), "selected source symlink needs separate authority")
            if stat.S_ISDIR(before.st_mode):
                if not _descend(relative, spec) and name in dirs:
                    dirs.remove(name)
                continue
            require(_selected(relative, spec), "selected source ancestor is not a directory")
            require(stat.S_ISREG(before.st_mode) and relative in expected,
                    "untracked or non-regular selected compiler input")
            mode, expected_blob = expected[relative]
            require(bool(before.st_mode & 0o111) == (mode == "100755"), "selected source mode changed")
            require(before.st_size <= 512 * 1024**2, "source file exceeds bound")
            total += before.st_size
            require(total <= 2 * 1024**3, "selected source bytes exceed bound")
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
            try:
                identity = lambda value: (value.st_dev, value.st_ino, value.st_size,
                    value.st_mode, value.st_uid, value.st_gid, value.st_nlink,
                    value.st_mtime_ns, value.st_ctime_ns)
                require(identity(os.fstat(descriptor)) == identity(before), "source changed before read")
                checksum = hashlib.sha1(b"blob " + str(before.st_size).encode() + b"\0")
                count = 0
                while True:
                    block = os.read(descriptor, min(65536, before.st_size - count + 1))
                    if not block:
                        break
                    count += len(block)
                    require(count <= before.st_size, "source grew during read")
                    checksum.update(block)
                require(count == before.st_size and checksum.hexdigest() == expected_blob,
                        "selected physical source bytes changed")
                require(identity(os.fstat(descriptor)) == identity(before) == identity(path.lstat()),
                        "source changed during read")
            finally:
                os.close(descriptor)
            actual_files.add(relative)
    require(actual_files == set(expected), "selected compiler input disappeared or moved")
    actual = git("rev-parse", "HEAD").strip()
    return {"repository": str(repository), "base_commit": spec["base_commit"],
            "observed_head": actual, "physical_input_files": len(actual_files),
            "physical_input_bytes": total, "outside_scope_inspected": False,
            "selected_input_changes": []}
