"""Prospective two-owner admission; never launch, stop or signal either owner.

Both callers retain the qualified child guard. A Root-pinned plan selects two
exact USER generations and one physical state directory. Its first resource
observation and generation claims are immutable. Missing terminal publication
blocks the next phase, without canceling an already running healthy phase.
An exactly stopped failed generation releases only its resource slot. Its
original missing waits and failed qualification remain unchanged.
"""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import threading
import time

import child_context as child
from cargo_control import Refused, bounded_regular_bytes, require


GIB = 1024 * 1024 * 1024
GROWTH = 10 * GIB
MINIMUM_DATA = 6307319808
INITIAL_MEMORY = 106 * GIB
HEALTHY_MEMORY = 96 * GIB
INITIAL_DOCKER = 300128382976
HEALTHY_DOCKER = 295833415680
LIMITS = {"compiler": 8 * GIB, "short": 2 * GIB}


def pin(path):
    """Bind exact regular bytes; callers select paths before reading them."""
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path, "joint path aliases")
    raw = bounded_regular_bytes(path, 1024 * 1024)
    return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}


def pinned_json(reference):
    """Keep authority external to this adapter and retain original bytes."""
    require(set(reference) == {"path", "sha256"} and
            re.fullmatch(r"[0-9a-f]{64}", reference["sha256"]), "joint pin fields differ")
    path = Path(reference["path"])
    require(path.is_absolute() and path.resolve(strict=True) == path, "joint path aliases")
    raw = bounded_regular_bytes(path, 1024 * 1024)
    require(hashlib.sha256(raw).hexdigest() == reference["sha256"], "joint pinned bytes differ")
    return json.loads(raw)


def identity(info):
    """Physical identity prevents replacing retained state with equal bytes."""
    return {"device": info.st_dev, "inode": info.st_ino}


def retained(path, uid):
    """Read an immutable owner-only record, including its physical identity."""
    path = Path(path)
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and stat.S_IMODE(before.st_mode) == 0o400
            and before.st_uid == uid and before.st_nlink == 1, "joint record custody differs")
    reference = pin(path)
    value = pinned_json(reference)
    after = path.lstat()
    require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
            "joint record changed during read")
    return value, {**reference, **identity(before)}


def resources():
    """Observe real host resources without changing the common baseline."""
    def volume(path):
        info = os.statvfs(path)
        return {"bytes": info.f_bavail * info.f_frsize, "inodes": info.f_favail}
    memory = next(int(line.split()[1]) * 1024 for line in Path("/proc/meminfo").read_text().splitlines()
                  if line.startswith("MemAvailable:"))
    return {"data": volume("/mnt/data"), "docker": volume("/var/lib/docker"),
            "memory_available": memory, "time_ns": time.time_ns()}


def resource_floor(initial):
    """The first observation owns the entire pair's cumulative allowance."""
    require(initial["data"]["bytes"] >= GROWTH and initial["memory_available"] >= INITIAL_MEMORY
            and initial["docker"]["bytes"] >= INITIAL_DOCKER, "joint initial resource forecast refused")
    require(initial["data"]["inodes"] >= 1000000 and initial["docker"]["inodes"] >= 1000000,
            "joint initial inode floor refused")
    return max(MINIMUM_DATA, initial["data"]["bytes"] - GROWTH)


def healthy(current, floor):
    """Final and intermediate observations enforce the same original floor."""
    require(current["data"]["bytes"] >= floor and current["memory_available"] >= HEALTHY_MEMORY
            and current["docker"]["bytes"] >= HEALTHY_DOCKER, "joint healthy resource floor crossed")
    require(current["data"]["inodes"] >= 1000000 and current["docker"]["inodes"] >= 1000000,
            "joint healthy inode floor crossed")


def memory_oom(raw):
    """Read cumulative counters without treating malformed evidence as zero."""
    require(isinstance(raw, str), "joint memory event evidence differs")
    counts = {}
    for line in raw.splitlines():
        fields = line.split()
        require(len(fields) == 2 and fields[0] not in counts
                and re.fullmatch(r"[0-9]+", fields[1]), "joint memory event evidence differs")
        counts[fields[0]] = int(fields[1])
    require("oom" in counts, "joint memory event evidence differs")
    return any(counts.get(name, 0) > 0 for name in ("oom", "oom_kill", "oom_group_kill"))


def process(pid):
    """Retry bounded exec/reparent cuts; never retry across a reused PID."""
    directory = Path("/proc") / str(pid)
    original_start = None
    for unused in range(3):
        fields = (directory / "stat").read_text().rsplit(")", 1)[1].split()
        require(original_start is None or original_start == fields[19], "joint process PID reused during read")
        original_start = fields[19]
        if fields[0] == "Z":
            raise ProcessLookupError("joint observed process has exited")
        status = (directory / "status").read_text()
        uids = next([int(value) for value in line.split()[1:]] for line in status.splitlines() if line.startswith("Uid:"))
        group_raw = (directory / "cgroup").read_text()
        groups = [line[3:] for line in group_raw.splitlines() if line.startswith("0::")]
        argv = (directory / "cmdline").read_bytes()
        if len(groups) != 1 or not argv.endswith(b"\0"):
            after = (directory / "stat").read_text().rsplit(")", 1)[1].split()
            require(fields[19] == after[19], "joint process PID reused during read")
            if after[0] == "Z":
                raise ProcessLookupError("joint observed process has exited")
            continue
        executable = os.readlink(directory / "exe")
        after_executable = os.readlink(directory / "exe")
        after_argv = (directory / "cmdline").read_bytes()
        after_group = (directory / "cgroup").read_text()
        after = (directory / "stat").read_text().rsplit(")", 1)[1].split()
        require(fields[19] == after[19], "joint process PID reused during read")
        if after[0] == "Z":
            raise ProcessLookupError("joint observed process has exited")
        if fields[1] == after[1] and argv == after_argv and executable == after_executable and group_raw == after_group:
            return {"pid": pid, "ppid": int(fields[1]), "start": fields[19], "state": after[0],
                    "uid": uids[0], "euid": uids[1], "cgroup": groups[0], "executable": executable,
                    "argv": [os.fsdecode(value) for value in argv[:-1].split(b"\0")]}
    raise Refused("joint live process remains unstable after bounded snapshots")


def departed_observation(member, observation, prior, departed=False):
    """A collected manager record never creates or replaces a generation."""
    invocation = observation["invocation"]
    require(invocation in ("", prior["invocation"]), "joint invocation changed")
    require(observation["unit"] == member["unit"] and
            observation["main_pid"] == 0 and observation["process"] is None and
            observation["empty"] and observation["active"] in ("inactive", "failed") and
            observation["cgroup"] in ("", member["cgroup"]),
            "joint departure is not an empty stopped generation")
    if invocation != prior["invocation"]:
        require(invocation == "" and observation["cgroup"] == "" and
                (observation.get("manager_absent") is True and
                 observation.get("load_state") == "not-found" and observation["active"] == "inactive" or
                 departed and observation.get("load_state") in ("loaded", "not-found")),
                "joint invocation changed")
    return {"unit": member["unit"], "observed_invocation": invocation,
            "original_invocation": prior["invocation"],
            "manager_absent": observation.get("manager_absent") is True,
            "load_state": observation.get("load_state"),
            "active": observation["active"], "main_pid": 0,
            "cgroup": observation["cgroup"], "empty": True,
            "memory_events": observation.get("memory_events")}


def admit_member(member, observation, prior=None, departed=False):
    """Only the selected full USER namespace and original generation qualify."""
    require(observation["unit"] == member["unit"], "joint unit differs")
    require(observation["cgroup"] in ("", member["cgroup"]), "joint USER namespace differs")
    invocation = observation["invocation"]
    if prior is not None and invocation != prior["invocation"]:
        # Default transient-unit collection removes InvocationID. This only
        # permits reconciliation of an old claim; the original terminal and
        # wait evidence still bind physical release without qualifying tests.
        departed_observation(member, observation, prior, departed)
    current = observation.get("process")
    if current is None:
        return None
    require(re.fullmatch(r"[0-9a-f]{32}", invocation) and invocation != "0" * 32,
            "joint invocation absent")
    require(observation["cgroup"] == current["cgroup"] == member["cgroup"] and
            current["uid"] == current["euid"] == member["uid"], "joint USER namespace differs")
    require(current["state"] != "Z" and current["argv"] == member["argv"] and
            current["executable"] == member["python"], "joint runner process differs")
    result = {"invocation": invocation, "pid": current["pid"], "start": current["start"],
              "cgroup": current["cgroup"], "uid": current["uid"]}
    require(prior is None or result == prior, "joint MainPID generation changed")
    return result


def observe_member(member):
    """Query the USER manager explicitly; same-named SYSTEM units confer nothing."""
    result = subprocess.run(
        ["/usr/bin/systemctl", "--user", "show", member["unit"], "-p", "Id", "-p", "ActiveState",
         "-p", "LoadState", "-p", "MainPID", "-p", "InvocationID", "-p", "ControlGroup"],
        text=True, capture_output=True, timeout=15)
    values = dict(line.split("=", 1) for line in result.stdout.splitlines())
    absent = values.get("LoadState") == "not-found" and values.get("ActiveState") == "inactive"
    require(result.returncode == 0 or result.returncode == 1 and absent, "joint USER manager observation failed")
    require(values.get("Id") == member["unit"], "joint USER manager unit differs")
    pid = int(values.get("MainPID", "0"))
    require(not absent or pid == 0 and not values.get("InvocationID") and not values.get("ControlGroup"),
            "joint missing-unit observation contradicts a generation")
    current = None
    if pid:
        try:
            current = process(pid)
        except (FileNotFoundError, ProcessLookupError):
            # No generation is released here. A subsequent exact terminal and
            # MainPID0 observation must still reconcile the retained claim.
            pass
    group = Path("/sys/fs/cgroup") / member["cgroup"].lstrip("/")
    empty = not group.exists()
    memory_events = None
    if not empty:
        try:
            events = dict(line.split() for line in (group / "cgroup.events").read_text().splitlines())
            empty = events.get("populated") == "0"
            require((group / "memory.max").read_text().strip() == str(member["memory_max"])
                    and (group / "memory.swap.max").read_text().strip() == "0", "joint peer hard limits differ")
            memory_events = (group / "memory.events").read_text()
            stopped = (pid == 0 and current is None and empty
                       and values["ActiveState"] in ("inactive", "failed")
                       and values.get("ControlGroup") == member["cgroup"])
            # A cumulative peer OOM can survive after its entire generation
            # stops. Only failed-terminal reconciliation may release that slot.
            require(not memory_oom(memory_events) or stopped, "joint peer OOM observed")
        except FileNotFoundError:
            require(not group.exists(), "joint live cgroup became unobservable")
            empty = True
    return {"unit": values["Id"], "invocation": values.get("InvocationID", ""), "cgroup": values.get("ControlGroup", ""),
            "main_pid": pid, "active": values["ActiveState"], "empty": empty, "process": current,
            "load_state": values.get("LoadState"), "manager_absent": absent,
            "memory_events": memory_events}


def admit_census(members, claims, rows):
    """Every observed compiler/body belongs to a selected original descendant."""
    by_pid = {row["pid"]: row for row in rows}
    admitted = []
    for row in rows:
        if not row.get("workload"):
            continue
        owners = [role for role, member in members.items() if row["cgroup"] == member["cgroup"]
                  or row["cgroup"].startswith(member["cgroup"] + "/")]
        require(len(owners) == 1, "unrelated compiler or body is live")
        role = owners[0]
        require(role in claims and row["uid"] == row["euid"] == members[role]["uid"],
                "joint workload owner is unbound")
        require((role == "compiler") == (row["workload"] == "compiler"), "joint workload role differs")
        if role == "short":
            require(row.get("protected_executable") is True and
                    row.get("executable_sha256") in members[role]["body_sha256"], "joint body executable differs")
        root = claims[role]
        cursor = row
        seen = set()
        while cursor["pid"] != root["pid"]:
            require(cursor["uid"] == cursor["euid"] == members[role]["uid"] and
                    (cursor["cgroup"] == members[role]["cgroup"] or
                     cursor["cgroup"].startswith(members[role]["cgroup"] + "/")),
                    "joint ancestor namespace differs")
            require(cursor["pid"] not in seen and cursor["ppid"] in by_pid,
                    "joint workload is not an observed peer descendant")
            seen.add(cursor["pid"])
            cursor = by_pid[cursor["ppid"]]
        require(cursor["start"] == root["start"], "joint ancestor PID was reused")
        admitted.append({"role": role, "pid": row["pid"], "start": row["start"]})
    return admitted


def _workload_snapshot(members, image_cache):
    """Retain the existing compiler census and add Go/libtest/Python bodies."""
    compiler_ids = {row["pid"] for values in child.compiler_census().values() for row in values}
    body_paths = set(members["short"]["body_executables"])
    rows = []
    for directory in Path("/proc").iterdir():
        if not directory.name.isdigit():
            continue
        raw = []
        try:
            raw = (directory / "cmdline").read_bytes().split(b"\0")
            stat_fields = (directory / "stat").read_text().rsplit(")", 1)[1].split()
            if stat_fields[0] == "Z" or not raw[0]:
                continue
            row = process(int(directory.name))
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError:
            require(int(directory.name) not in compiler_ids and
                    not any(value.startswith(b"-test.") for value in raw), "live workload is unobservable")
            continue
        name = Path(row["executable"].removesuffix(" (deleted)")).name
        args = row["argv"]
        if (row["pid"] in compiler_ids or name in ("cargo", "rustc", "rustdoc", "compile", "cgo", "asm", "link", "gcc", "g++", "cc", "clang", "clang++")
                or name == "go" and len(args) > 1 and args[1] in ("test", "build", "run", "install")):
            row["workload"] = "compiler"
        elif (row["executable"] in body_paths or name.startswith("memfd:")
              or name == "test2json" or name.endswith(".test")
              or any(value.startswith("-test.") for value in args)
              or any(value.startswith("--test-threads") for value in args)
              or "--exact" in args or "--nocapture" in args or "unittest" in args or "pytest" in args):
            row["workload"] = "body"
            if row["cgroup"] == members["short"]["cgroup"] or row["cgroup"].startswith(members["short"]["cgroup"] + "/"):
                try:
                    descriptor = os.open(directory / "exe", os.O_RDONLY | os.O_CLOEXEC)
                    try:
                        info = os.fstat(descriptor)
                        require(stat.S_ISREG(info.st_mode) and 4 <= info.st_size <= 512 * 1024 * 1024,
                                "joint body executable is not bounded regular data")
                        key = (row["pid"], row["start"], info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
                        if name.startswith("memfd:"):
                            seals = fcntl.fcntl(descriptor, fcntl.F_GET_SEALS)
                            required = fcntl.F_SEAL_SEAL | fcntl.F_SEAL_WRITE | fcntl.F_SEAL_GROW | fcntl.F_SEAL_SHRINK
                            require(seals & required == required, "joint body memfd is not sealed")
                        else:
                            require(info.st_uid == members["short"]["uid"] and not info.st_mode & 0o022
                                    and info.st_nlink == 1, "joint body executable protection differs")
                        if key not in image_cache:
                            checksum = hashlib.sha256()
                            used = 0
                            while True:
                                block = os.read(descriptor, min(65536, info.st_size - used + 1))
                                if not block:
                                    break
                                used += len(block)
                                require(used <= info.st_size, "joint body executable grew")
                                checksum.update(block)
                            after = os.fstat(descriptor)
                            require(used == info.st_size and
                                    (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
                                    (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                                    "joint body executable changed")
                            image_cache[key] = checksum.hexdigest()
                        row["executable_sha256"] = image_cache[key]
                        row["protected_executable"] = True
                    finally:
                        os.close(descriptor)
                except (FileNotFoundError, ProcessLookupError):
                    continue
        rows.append(row)
    return rows


def workload_rows(members, image_cache):
    """Retry a bounded parent-exit cut before judging descendant ownership."""
    for unused in range(3):
        rows = _workload_snapshot(members, image_cache)
        by_pid = {row["pid"]: row for row in rows}
        incomplete = False
        for row in rows:
            if not row.get("workload"):
                continue
            owners = [member for member in members.values() if row["cgroup"] == member["cgroup"]
                      or row["cgroup"].startswith(member["cgroup"] + "/")]
            if len(owners) != 1:
                continue
            member = owners[0]
            cursor, seen = row, set()
            while cursor["argv"] != member["argv"] or cursor["cgroup"] != member["cgroup"]:
                if cursor["pid"] in seen or cursor["ppid"] not in by_pid:
                    incomplete = True
                    break
                seen.add(cursor["pid"])
                cursor = by_pid[cursor["ppid"]]
        if not incomplete:
            return rows
    raise Refused("joint live descendant ancestry remains unobservable after bounded snapshots")


class JointAdmission:
    """One caller's view of immutable shared observations; checks are serialized."""
    def __init__(self, job_path, runner_path, adoption_path, role, expected_adoption=None):
        self.adoption_pin = pin(adoption_path)
        require(expected_adoption is None or self.adoption_pin == expected_adoption, "joint caller adoption changed")
        adoption = pinned_json(self.adoption_pin)
        self.plan_pin = adoption["joint_admission"]
        self.plan = pinned_json(self.plan_pin)
        require(self.plan["schema"] == "urnetwork-qualification-joint-peer-v1"
                and self.plan["growth_bytes"] == GROWTH and self.plan["uid"] == 1000
                and set(self.plan["members"]) == set(LIMITS), "joint selected class differs")
        self.role = role
        self.uid = self.plan["uid"]
        self.members = self.plan["members"]
        require(os.getuid() == os.geteuid() == self.uid, "joint executor UID differs")
        for name, member in self.members.items():
            require(member["uid"] == self.uid and member["memory_max"] == LIMITS[name]
                    and member["manager"] == "user" and
                    member["cgroup"] == f'/user.slice/user-{self.uid}.slice/user@{self.uid}.service/app.slice/' + member["unit"],
                    "joint selected USER namespace or hard limit differs")
            require(member["argv"] == [member["python_argv"], member["runner"]["path"], member["job"]["path"]],
                    "joint selected command differs")
            pinned_json(member["job"])
            require(pin(member["runner"]["path"]) == member["runner"], "joint runner pin differs")
        require(self.members[role]["job"] == pin(job_path) and
                self.members[role]["runner"] == pin(runner_path), "joint caller selection differs")
        require(pinned_json(self.members[role]["job"])["adoption_path"] == str(adoption_path),
                "joint caller adoption path differs")
        self.directory = Path(self.plan["state_directory"]["path"])
        self.state_lock = threading.Lock()
        self.observed = {}
        self.image_cache = {}
        self.lock_identity = None
        self.started = []
        self.floor = None
        self.initial = None
        self.claims = {}
        self.peer_adoptions = {}
        self.boot = Path("/proc/sys/kernel/random/boot_id").read_text().strip()
        self.check(phase=True, initializing=True)
        require(self.claims[role]["pid"] == os.getpid(), "joint caller is not selected MainPID")

    def check(self, phase=False, initializing=False):
        """No missing peer releases a claim; phase admission needs reconciliation."""
        with self.state_lock:
            require(pinned_json(self.plan_pin) == self.plan, "joint plan changed")
            require(pin(self.adoption_pin["path"]) == self.adoption_pin, "joint caller adoption changed")
            current = resources()
            observations = {role: observe_member(member) for role, member in self.members.items()}
            rows = workload_rows(self.members, self.image_cache)
            running = subprocess.check_output(["docker", "ps", "--no-trunc", "--quiet"], text=True, timeout=15).splitlines()
            require(running == [], "joint model or Docker workload is live")
            selected = self.plan["state_directory"]
            require(self.directory.resolve(strict=True) == self.directory, "joint directory aliases")
            info = self.directory.lstat()
            require(identity(info) == {key: selected[key] for key in ("device", "inode")}
                    and stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
                    and info.st_uid == self.uid, "joint directory custody changed")
            lock_path = self.directory / "admission.lock"
            descriptor = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            try:
                lock_info = os.fstat(descriptor)
                require(stat.S_ISREG(lock_info.st_mode) and lock_info.st_uid == self.uid
                        and stat.S_IMODE(lock_info.st_mode) == 0o600 and lock_info.st_nlink == 1,
                        "joint lock custody differs")
                require(self.lock_identity is None or self.lock_identity == identity(lock_info), "joint lock inode changed")
                self.lock_identity = identity(lock_info)
                deadline = time.monotonic() + 2
                while True:
                    try:
                        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        require(time.monotonic() < deadline, "joint publication lock unavailable")
                        time.sleep(.01)
                require(identity(lock_info) == identity(lock_path.lstat()), "joint lock replaced")
                require(not list(self.directory.glob("*.pending")), "joint interrupted publication retained")
                return self._check_with_lock(phase, initializing, current, observations, rows)
            finally:
                os.close(descriptor)

    def _record_with_lock(self, name, value=None):
        """Publish once or read the original; equal-byte replacements still fail."""
        path = self.directory / name
        if not path.exists():
            require(name not in self.observed and value is not None, "joint retained record disappeared")
            child.durable_json(path, value)
        record, reference = retained(path, self.uid)
        require(name not in self.observed or self.observed[name] == reference, "joint original record replaced")
        self.observed[name] = reference
        require(value is None or record == value, "joint original record differs")
        return record, reference

    def _check_with_lock(self, phase, initializing, current, observations, rows):
        """The shared baseline and both first-seen generations are never renewed."""
        initial_path = self.directory / "initial.json"
        first = None
        if not initial_path.exists():
            require(not list(self.directory.glob("*.generation.json")) and self.initial is None,
                    "joint original baseline disappeared")
            first = {"plan": self.plan_pin, "boot": self.boot, "lock": self.lock_identity, "resources": current,
                     "floor": resource_floor(current)}
        initial, initial_pin = self._record_with_lock("initial.json", first)
        require(initial["plan"] == self.plan_pin and initial["boot"] == self.boot and initial["lock"] == self.lock_identity
                and initial["floor"] == resource_floor(initial["resources"]), "joint baseline differs")
        self.floor, self.initial = initial["floor"], initial_pin
        healthy(current, self.floor)
        pending = []
        states = {}
        for role, member in self.members.items():
            require(pin(member["runner"]["path"]) == member["runner"] and pin(member["job"]["path"]) == member["job"],
                    "joint peer source pin changed")
            peer_adoption = pin(member["adoption_path"])
            name = role + ".generation.json"
            prior = None
            if (self.directory / name).exists() or name in self.observed:
                claim, unused = self._record_with_lock(name)
                require(claim["plan"] == self.plan_pin and claim["boot"] == self.boot, "joint claim origin differs")
                require(claim["adoption"] == peer_adoption, "joint original peer adoption changed")
                prior = claim["executor"]
            observation = observations[role]
            departure_name = role + ".departure.json"
            departed = (self.directory / departure_name).exists() or departure_name in self.observed
            require(not departed or prior is not None, "joint departed generation claim disappeared")
            actual = admit_member(member, observation, prior, departed)
            if actual is not None:
                require(not departed, "joint departed generation became live")
                claim, unused = self._record_with_lock(name, {"plan": self.plan_pin, "boot": self.boot,
                    "adoption": peer_adoption, "executor": actual})
                self.claims[role] = actual
                states[role] = "live"
            elif prior is None:
                require(observation["main_pid"] == 0 and observation["empty"], "unbound joint peer is transitioning")
                states[role] = "not-started"
            else:
                self.claims[role] = prior
                states[role] = "terminal-pending"
                if observation["main_pid"] == 0 and observation["empty"] and observation["active"] in ("inactive", "failed"):
                    departure = self._departure_with_lock(role, prior, observation, departed)
                    if departure:
                        states[role] = departure
                if states[role] == "terminal-pending":
                    pending.append(role)
            self.peer_adoptions[role] = peer_adoption
        require(states[self.role] == "live", "joint caller generation is not live")
        census = admit_census(self.members, self.claims, rows)
        require(not phase or not pending, "joint peer terminal is not yet joined and retained")
        return {"initial": self.initial, "floor": self.floor, "states": states, "census": census,
                "resources": current, "phase_admitted": bool(phase)}

    def run_phase(self, context, argv, output, label, timeout, **kwargs):
        """Register every attempted child before launch and retain its real wait."""
        self.check(phase=True)
        require(kwargs.get("minimum_free") == self.floor, "joint child floor was reset")
        row = {"label": label, "wait": None}
        self.started.append(row)
        failure = None
        try:
            return context.run(argv, output, label, timeout, **kwargs)
        except BaseException as exc:
            failure = exc
            raise
        finally:
            path = Path(output) / (label + ".process-result.json")
            if os.path.lexists(path):
                try:
                    row["wait"] = pin(path)
                    child.replay_process_result(row["wait"], require_context_verified=False)
                except Exception as exc:
                    row["replay_error"] = {"type": type(exc).__name__, "detail": str(exc)}
                    if failure is None:
                        raise

    def terminal(self, value):
        """Retain actual joins separately from context and test qualification."""
        joined = bool(self.started) and all(row["wait"] is not None for row in self.started)
        verified = joined
        replay_error = None
        try:
            for row in self.started:
                if row["wait"] is not None:
                    waited = child.replay_process_result(row["wait"], require_context_verified=False)
                    verified = verified and waited["context_verified"]
        except Exception as exc:
            joined = verified = False
            replay_error = {"type": type(exc).__name__, "detail": str(exc)}
        value["joint_admission"] = {"plan": self.plan_pin, "initial": self.initial,
            "executor": self.claims[self.role], "adoption": self.adoption_pin,
            "started": self.started, "all_started_joined": joined,
            "all_started_context_verified": verified, "replay_error": replay_error}
        return value

    def _departure_with_lock(self, role, executor, observation, departed):
        """Prove a stopped slot without promoting its failed or missing execution."""
        member = self.members[role]
        observed = departed_observation(member, observation, executor, departed)
        path = Path(member["terminal_path"])
        if not os.path.lexists(path):
            require(not departed, "joint original terminal disappeared")
            return False
        terminal, reference = retained(path, self.uid)
        require(terminal["job"] == member["job"], "joint peer terminal job differs")
        evidence = terminal["joint_admission"]
        require(evidence["plan"] == self.plan_pin and evidence["initial"] == self.initial
                and evidence["executor"] == executor,
                "joint terminal generation or original baseline differs")
        adoption = pinned_json(evidence["adoption"])
        require(evidence["adoption"]["path"] == member["adoption_path"]
                and evidence["adoption"] == self.peer_adoptions.get(role, pin(member["adoption_path"]))
                and terminal["adoption"] == evidence["adoption"]
                and adoption["joint_admission"] == self.plan_pin
                and adoption["job_sha256"] == member["job"]["sha256"]
                and adoption["runner_sha256"] == member["runner"]["sha256"], "joint peer adoption differs")
        require(len(evidence["started"]) <= member["maximum_phases"]
                and len({row["label"] for row in evidence["started"]}) == len(evidence["started"]),
                "joint started-phase census differs")
        # Short runners retain every execution before projecting qualified body
        # phases. A guard failure can interrupt that projection after the wait.
        executions = role == "short" and "executions" in terminal
        phases = terminal["executions"] if executions else terminal["phases"]
        require(len(phases) == len(evidence["started"]),
                "joint terminal omitted or changed an original phase wait")

        def failed(record):
            failure, errors = record.get("failure"), record.get("resource_errors")
            require(failure is None or isinstance(failure, dict), "joint failure evidence differs")
            require(errors is None or isinstance(errors, list), "joint resource failure evidence differs")
            return bool(failure or errors)

        terminal_failed = failed(terminal)
        if observation.get("memory_events") is not None:
            require(not memory_oom(observation["memory_events"]) or terminal_failed,
                    "joint stopped peer OOM lacks original failure")
        missing = []
        for phase, row in zip(phases, evidence["started"]):
            label = row["label"]
            require(isinstance(label, str) and re.fullmatch(r"[A-Za-z0-9_.-]{1,96}", label),
                    "joint wait label differs")
            original = pinned_json(phase["receipt"]) if role == "compiler" or executions else phase
            if role == "compiler" or executions:
                require(original["job"] == member["job"], "joint original phase job differs")
                suffix = ".receipt.json" if role == "compiler" else ".execution.json"
                require(Path(phase["receipt"]["path"]).name == label + suffix, "joint original phase label differs")
            original_failed = failed(original)
            result = original.get("result")
            original_wait = result["process_result"] if result is not None else original.get("process_result")
            if "process_result" in original and result is not None:
                require(original["process_result"] == original_wait, "joint original phase wait contradicts result")
            if executions:
                require(phase["process_result"] == original_wait, "joint execution wait differs")
            if original_wait != row["wait"]:
                # Older compiler runners do not copy a guard exception's wait
                # into result=None. Bind that retained original by exact label.
                receipt_path = Path(phase["receipt"]["path"]) if role == "compiler" else None
                require(role == "compiler" and result is None and original_wait is None
                        and original_failed and row["wait"] is not None
                        and receipt_path.name == label + ".receipt.json"
                        and Path(row["wait"]["path"]) == receipt_path.with_name(label + ".process-result.json"),
                        "joint terminal omitted or changed an original phase wait")
            if row["wait"] is None:
                require(original_failed, "joint missing wait lacks original phase failure")
                missing.append(label)
                continue
            require(Path(row["wait"]["path"]).name == row["label"] + ".process-result.json", "joint wait label differs")
            waited = child.replay_process_result(row["wait"], require_context_verified=False)
            require(waited["tree_joined"] is True and type(waited["exit"]) is int,
                    "joint peer wait is unjoined or invalid")
        joined = bool(evidence["started"]) and not missing
        if executions:
            require(terminal["actual_waits"] == [row["wait"] for row in evidence["started"]],
                    "joint terminal execution wait census differs")
            # A projection may stop at the first failed execution, but cannot
            # replace or contradict any original execution already projected.
            require(len(terminal["phases"]) <= len(phases), "joint projected phase census differs")
            for phase, projected in zip(phases, terminal["phases"]):
                original = pinned_json(phase["receipt"])
                require(projected == dict(original, execution_receipt=phase["receipt"]),
                        "joint projected phase differs from original execution")
        require(evidence["all_started_joined"] is joined, "joint terminal joined-wait claim differs")
        require(joined or (terminal_failed and terminal["status"] == "FAIL_OR_INCOMPLETE"),
                "joint incomplete terminal lacks original failure")
        require(evidence.get("replay_error") is None and not any(row.get("replay_error") for row in evidence["started"]),
                "joint original wait replay failed")
        final = terminal["final"]
        if not terminal_failed:
            healthy(final, self.floor)
        if final is not None:
            if "error" in final:
                require(set(final) == {"error"} and isinstance(final["error"], dict) and bool(final["error"]),
                        "joint failed final resource evidence differs")
            else:
                require(final["cgroup"].lstrip("/") == member["cgroup"].lstrip("/"), "joint final resource namespace differs")
                require(not memory_oom(final["memory_events"]) or terminal_failed,
                        "joint terminal OOM lacks original failure")
        release = {"basis": "replayed-joined-waits" if joined else "stopped-empty-failed-generation",
                   "all_started_joined": joined, "missing_wait_labels": missing,
                   "test_qualification": "not-performed"}
        name = role + ".departure.json"
        if departed:
            original, unused = self._record_with_lock(name)
            require(original["executor"] == executor and original["terminal"] == reference,
                    "joint original record differs")
            require(original.get("release", release) == release, "joint original release differs")
        else:
            self._record_with_lock(name, {"executor": executor, "terminal": reference,
                                         "first_manager_observation": observed, "release": release})
        return "joined-terminal" if joined else "stopped-failed-terminal"
