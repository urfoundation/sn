"""Bind qualification tools and their actual child context before execution.

The caller still owns resource admission, source/image custody and result
classification. This module uses the existing bounded process-tree guard; it
never substitutes an ambient PATH or a guessed GOROOT tool location.
"""

import copy
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import sys

from cargo_control import MAXIMUM_LOG_BYTES, bounded_regular_bytes, require, run_process


MAXIMUM_EXECUTABLE_BYTES = 512 * 1024 * 1024
MAXIMUM_PROCESS_RECEIPT_BYTES = 1024 * 1024
MAXIMUM_PYTHON_LINKS = 16
MAXIMUM_PYTHON_CONFIG_BYTES = 64 * 1024


def durable_json(path, value):
    """Create one immutable result before downstream checks can discard it."""
    encoded = (json.dumps(value, sort_keys=True, indent=2) + "\n").encode()
    require(len(encoded) <= MAXIMUM_PROCESS_RECEIPT_BYTES, "process receipt exceeds bound")
    temporary = path.with_name(path.name + ".pending")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        view = memoryview(encoded)
        while view:
            count = os.write(descriptor, view)
            require(count > 0, "process receipt write made no progress")
            view = view[count:]
        os.fchmod(descriptor, 0o400)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    # Publish without replacing an earlier observation under the same label.
    os.link(temporary, path, follow_symlinks=False)
    temporary.unlink()
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {"path": str(path), "sha256": hashlib.sha256(encoded).hexdigest()}


def replay_process_result(reference, require_context_verified=True):
    """Read the actual retained wait result; this never launches or infers a pass.

    A caller still classifies test events and source/image custody independently.
    Incomplete postchecks can expose a wait-only result explicitly, never a
    successfully verified child context.
    """
    path = Path(reference["path"])
    require(path.is_absolute() and path.resolve(strict=True) == path,
            "process receipt path aliases or is absent")
    raw = bounded_regular_bytes(path, MAXIMUM_PROCESS_RECEIPT_BYTES)
    require(hashlib.sha256(raw).hexdigest() == reference["sha256"], "process receipt pin differs")
    record = json.loads(raw)
    require(record["schema"] == "urnetwork-qualification-process-wait-v1",
            "process receipt schema differs")
    result = record["result"]
    require(type(result["exit"]) is int and result["tree_joined"] is True,
            "process receipt lacks an actual joined wait result")
    label = record["label"]
    require(re.fullmatch(r"[A-Za-z0-9_.-]{1,96}", label) and
            path.name == label + ".process-result.json", "process receipt label differs")
    for stream in ("stdout", "stderr"):
        log = bounded_regular_bytes(path.parent / (label + "." + stream), MAXIMUM_LOG_BYTES)
        require(hashlib.sha256(log).hexdigest() == result[stream + "_sha256"],
                "retained process output differs")
    verified = path.with_name(label + ".process-context.json")
    context_verified = False
    if verified.exists():
        after = json.loads(bounded_regular_bytes(verified, MAXIMUM_PROCESS_RECEIPT_BYTES))
        require(after.get("schema") == "urnetwork-qualification-process-context-v1"
                and after.get("wait_result") == reference
                and after.get("child_context") == record["child_context"],
                "process context postcheck differs from actual wait result")
        context_verified = True
    require(not require_context_verified or context_verified,
            "actual wait is retained but child context postcheck is incomplete")
    return {**result, "process_result": reference, "context_verified": context_verified,
            "test_classification": "not-performed; actual joined execution only"}


def file_identity(info):
    """Retain named identity as well as bytes, including later replacement."""
    return {"device": info.st_dev, "inode": info.st_ino, "bytes": info.st_size,
            "mode": info.st_mode, "uid": info.st_uid, "gid": info.st_gid,
            "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}


def bind_executable(path, expected=None, interpreter=False):
    """Pin a real absolute executable; scripts also bind their direct interpreter."""
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path,
            "executable must name its exact absolute origin")
    if expected is not None:
        require(isinstance(expected, str) and re.fullmatch(r"[0-9a-f]{64}", expected),
                "executable SHA256 pin is invalid")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        require(stat.S_ISREG(before.st_mode) and before.st_mode & 0o111
                and 0 < before.st_size <= MAXIMUM_EXECUTABLE_BYTES,
                "executable is not a bounded executable regular file")
        checksum = hashlib.sha256()
        prefix, used = b"", 0
        while True:
            raw = os.read(descriptor, min(65536, before.st_size - used + 1))
            if not raw:
                break
            if not prefix:
                prefix = raw[:4096]
            used += len(raw)
            require(used <= before.st_size, "executable grew while binding")
            checksum.update(raw)
        require(used == before.st_size
                and file_identity(os.fstat(descriptor)) == file_identity(before)
                and file_identity(path.lstat()) == file_identity(before),
                "executable changed while binding")
        digest = checksum.hexdigest()
        require(expected is None or digest == expected, "executable differs from SHA256 pin")
        result = {"path": str(path), "sha256": digest, **file_identity(before)}
        if prefix.startswith(b"#!"):
            fields = prefix.split(b"\n", 1)[0][2:].decode().split()
            require(not interpreter and len(fields) == 1
                    and Path(fields[0]).name != "env", "script requires one direct pinned interpreter")
            result["interpreter"] = bind_executable(fields[0], interpreter=True)
        else:
            require(prefix.startswith(b"\x7fELF"), "executable format is not ELF or a direct script")
        return result
    finally:
        os.close(descriptor)


def bind_python_interpreter(path, expected=None):
    """Keep Python's invocation path while independently binding its real ELF.

    Python discovers a virtualenv beside the invoked path, before following
    executable symlinks. Resolving argv[0] changes imports even when the real
    interpreter bytes are identical. Only explicit final-component symlink
    chains are allowed here; normal tool binding remains canonical and strict.
    Dependency/source custody still belongs to the caller.
    """
    invocation = Path(path)
    require(invocation.is_absolute(), "Python invocation must be absolute")
    current, links, parents = invocation, [], {}
    seen = set()
    while True:
        require(current.parent.resolve(strict=True) == current.parent,
                "Python invocation parent aliases")
        parent = current.parent.lstat()
        parents[str(current.parent)] = {key: value for key, value in file_identity(parent).items()
                                       if key in ("device", "inode", "mode", "uid", "gid")}
        require(str(current) not in seen, "Python invocation symlink cycle")
        seen.add(str(current))
        before = current.lstat()
        if not stat.S_ISLNK(before.st_mode):
            break
        require(len(links) < MAXIMUM_PYTHON_LINKS, "Python invocation symlink bound")
        target = os.readlink(current)
        require(file_identity(current.lstat()) == file_identity(before),
                "Python invocation link changed while binding")
        links.append({"path": str(current), "target": target, "identity": file_identity(before)})
        selected = Path(target) if Path(target).is_absolute() else current.parent / target
        current = Path(os.path.normpath(selected))
    target = bind_executable(current, expected)
    require("interpreter" not in target, "Python invocation must target a real ELF interpreter")
    configuration = []
    for directory in (invocation.parent, invocation.parent.parent):
        config = directory / "pyvenv.cfg"
        try:
            before = config.lstat()
        except FileNotFoundError:
            configuration.append({"path": str(config), "identity": None, "sha256": None})
            continue
        require(stat.S_ISREG(before.st_mode), "Python configuration must be regular data")
        raw = bounded_regular_bytes(config, MAXIMUM_PYTHON_CONFIG_BYTES)
        require(file_identity(config.lstat()) == file_identity(before),
                "Python configuration changed while binding")
        configuration.append({"path": str(config), "identity": file_identity(before),
                              "sha256": hashlib.sha256(raw).hexdigest()})
    for link in links:
        require(file_identity(Path(link["path"]).lstat()) == link["identity"]
                and os.readlink(link["path"]) == link["target"],
                "Python invocation link changed while binding")
    require(str(invocation.resolve(strict=True)) == target["path"],
            "Python invocation target changed while binding")
    for parent, identity in parents.items():
        info = file_identity(Path(parent).lstat())
        require(all(info[key] == value for key, value in identity.items()),
                "Python invocation parent changed while binding")
    return {"kind": "python-invocation-v1", "path": str(invocation),
            "sha256": target["sha256"], "target": target, "links": links,
            "parents": parents, "configuration": configuration}


class ChildContext:
    """Freeze one cwd and complete environment for probes and the actual child."""

    def __init__(self, cwd, environment, runner_pin=None):
        self.cwd = Path(cwd)
        require(self.cwd.is_absolute() and self.cwd.resolve(strict=True) == self.cwd
                and self.cwd.is_dir(), "child cwd must be an exact existing directory")
        require(isinstance(environment, dict) and all(
            isinstance(key, str) and isinstance(value, str) and key and "=" not in key
            and "\0" not in key + value for key, value in environment.items()),
            "child environment must be a complete string mapping")
        self._environment = environment.copy()
        self._cwd_identity = self._directory_identity()
        self._environment_sha256 = self._environment_hash()
        self._executables = {}
        self.runner_invocation = bind_python_interpreter(sys.executable)
        # Existing runner pins authenticate the real executable. Keep that
        # contract while also retaining the invocation/configuration context.
        self.runner = self.runner_invocation["target"]
        if runner_pin is not None:
            require(isinstance(runner_pin, dict) and set(runner_pin) == {"path", "sha256"}
                    and runner_pin["path"] == self.runner["path"]
                    and runner_pin["sha256"] == self.runner["sha256"],
                    "actual Python interpreter differs from reviewed runner pin")

    def _directory_identity(self):
        info = self.cwd.lstat()
        return {"device": info.st_dev, "inode": info.st_ino, "mode": info.st_mode,
                "uid": info.st_uid, "gid": info.st_gid}

    def _environment_hash(self):
        return hashlib.sha256(json.dumps(self._environment, sort_keys=True,
            separators=(",", ":"), ensure_ascii=True).encode()).hexdigest()

    def bind(self, path, expected=None):
        """Add an explicit tool origin before it can be launched."""
        binding = bind_executable(path, expected)
        previous = self._executables.get(binding["path"])
        require(previous is None or previous == binding, "bound executable origin changed")
        self._executables[binding["path"]] = binding
        return copy.deepcopy(binding)

    def bind_python(self, path, expected=None):
        """Bind the selected Python path without resolving away its virtualenv."""
        binding = bind_python_interpreter(path, expected)
        previous = self._executables.get(binding["path"])
        require(previous is None or previous == binding, "bound Python invocation changed")
        self._executables[binding["path"]] = binding
        return copy.deepcopy(binding)

    def verify(self):
        """Refuse changed context and tools before launching or sealing a result."""
        require(self.cwd.resolve(strict=True) == self.cwd
                and self._directory_identity() == self._cwd_identity,
                "child cwd identity changed")
        require(self._environment_hash() == self._environment_sha256,
                "frozen child environment changed")
        require(bind_python_interpreter(self.runner_invocation["path"], self.runner["sha256"])
                == self.runner_invocation, "runner Python invocation changed")
        for binding in self._executables.values():
            binder = bind_python_interpreter if binding.get("kind") == "python-invocation-v1" else bind_executable
            require(binder(binding["path"], binding["sha256"]) == binding,
                    "bound executable identity changed")

    def receipt(self):
        """Bind all environment bytes without publishing arbitrary secret values."""
        return {"cwd": str(self.cwd), "cwd_identity": self._cwd_identity.copy(),
                "environment_sha256": self._environment_sha256,
                "environment_keys": sorted(self._environment),
                "tool_environment": {key: self._environment[key] for key in (
                    "PATH", "GOENV", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOCACHE",
                    "GOMODCACHE", "TMPDIR") if key in self._environment},
                "runner_executable": copy.deepcopy(self.runner),
                "runner_invocation": copy.deepcopy(self.runner_invocation)}

    def run(self, argv, output, label, timeout, log_limit=MAXIMUM_LOG_BYTES,
            minimum_free=0, process_guard=run_process):
        """Use the same frozen context and bounded tree-joining guard for every step."""
        require(isinstance(argv, list) and argv and all(isinstance(arg, str) for arg in argv)
                and argv[0] in self._executables, "child argv must begin with a bound absolute executable")
        output = Path(output)
        require(output.is_absolute() and output.resolve(strict=True) == output
                and re.fullmatch(r"[A-Za-z0-9_.-]{1,96}", label), "process output or label differs")
        result_path = output / (label + ".process-result.json")
        context_path = output / (label + ".process-context.json")
        require(all(not os.path.lexists(path) for path in (result_path, context_path,
                    result_path.with_name(result_path.name + ".pending"),
                    context_path.with_name(context_path.name + ".pending"))),
                "process label already retains an outcome")
        self.verify()
        original_context = self.receipt()
        try:
            result = process_guard(argv, self.cwd, self._environment.copy(), output, label,
                                   timeout, log_limit=log_limit, minimum_free=minimum_free)
        except BaseException as error:
            result = getattr(error, "qualification_process_result", None)
            if result is not None:
                require(result.get("argv") == argv and result.get("tree_joined") is True
                        and type(result.get("exit")) is int and result.get("guard_failure"),
                        "guard exception does not retain an actual joined wait")
                retained = durable_json(result_path, {"schema": "urnetwork-qualification-process-wait-v1",
                    "label": label, "result": result, "child_context": original_context,
                    "executable": copy.deepcopy(self._executables[argv[0]])})
                for stream in ("stdout", "stderr"):
                    descriptor = os.open(output / (label + "." + stream), os.O_RDONLY | os.O_NOFOLLOW)
                    try:
                        os.fsync(descriptor)
                    finally:
                        os.close(descriptor)
                error.qualification_process_reference = retained
            # No context-postcheck receipt is published on a guard failure.
            raise
        # Persist the exact guard return before postcheck, shape conversion or
        # caller classification. A later checker error cannot erase Wait/join.
        retained = durable_json(result_path, {"schema": "urnetwork-qualification-process-wait-v1",
            "label": label, "result": result, "child_context": original_context,
            "executable": copy.deepcopy(self._executables[argv[0]])})
        for stream in ("stdout", "stderr"):
            descriptor = os.open(output / (label + "." + stream), os.O_RDONLY | os.O_NOFOLLOW)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
        self.verify()
        durable_json(context_path, {"schema": "urnetwork-qualification-process-context-v1",
            "wait_result": retained, "child_context": self.receipt()})
        return {**result, "child_context": self.receipt(),
                "process_result": retained,
                "executable": copy.deepcopy(self._executables[argv[0]])}

    def prepare_go(self, go, expected, output, label="go-context", minimum_free=0,
                   process_guard=run_process, log_limit=131072):
        """Resolve Go's actual test2json under the body context before any test body."""
        require(self._environment.get("GOENV") == "off"
                and self._environment.get("GOTOOLCHAIN") == "local",
                "Go requires explicit GOENV=off and GOTOOLCHAIN=local")
        path = self._environment.get("PATH")
        require(path and all(item and Path(item).is_absolute() for item in path.split(os.pathsep)),
                "Go requires an explicit absolute child PATH")
        go_binding = self.bind(go, expected)
        found = shutil.which("go", path=path)
        require(found is not None and str(Path(found).resolve(strict=True)) == go_binding["path"],
                "child PATH resolves a different or absent Go executable")
        probes, remaining = [], log_limit
        for suffix, arguments in (("version", ["version"]), ("test2json", ["tool", "-n", "test2json"])):
            require(remaining > 0, "Go preflight exhausted its reviewed log forecast")
            result = self.run([go_binding["path"], *arguments], output, label + "-" + suffix,
                              60, min(65536, remaining), minimum_free, process_guard)
            require(result["exit"] == 0, "exact Go context probe failed: " + suffix)
            probes.append(result)
            remaining -= result["log_bytes"]
        raw = (output / (label + "-test2json.stdout")).read_text().strip()
        require(raw and "\n" not in raw and "\r" not in raw, "Go tool origin is absent or ambiguous")
        # Go may report a dynamically built cache executable outside GOROOT.
        # Accept an exact raw path (including spaces) or one shell-quoted path.
        if not Path(raw).is_absolute():
            parsed = shlex.split(raw)
            require(len(parsed) == 1, "Go tool origin is not one executable")
            raw = parsed[0]
        tool = self.bind(raw)
        return {"go": go_binding, "test2json": tool, "probes": probes,
                "version": (output / (label + "-version.stdout")).read_text().strip(),
                "child_context": self.receipt()}


def compiler_census(proc=Path("/proc")):
    """Observe a stable compiler generation across bounded exec/exit cuts.

    A failed /proc read is not proof of exit. Retry the same start time, skip
    only an observed dead process or absent directory, and refuse a persistently
    unobservable live compiler. This census never replaces the admission lease.
    """
    result = {"go_test_compilers": [], "go_workers": [], "rustc": []}
    kinds = ("go", "compile", "rustc")
    for entry in proc.iterdir():
        if not entry.name.isdigit():
            continue
        original_start = None
        reason = "live process lost compiler observation"
        last_error = None
        for unused in range(3):
            try:
                raw_stat = (entry / "stat").read_text()
                comm = raw_stat.split("(", 1)[1].rsplit(")", 1)[0]
                fields = raw_stat.rsplit(")", 1)[1].split()
                require(original_start is None or fields[19] == original_start,
                        "compiler PID reused during census")
                original_start = fields[19]
                if fields[0] in ("Z", "X"):
                    break
                reason = "live compiler executable is unobservable"
                try:
                    executable = os.readlink(entry / "exe")
                except PermissionError:
                    if comm not in kinds:
                        break
                    raise
                kind = Path(executable.removesuffix(" (deleted)")).name
                if kind not in kinds and comm not in kinds:
                    break
                reason = "live compiler argv is unobservable"
                raw_argv = (entry / "cmdline").read_bytes()
                current_stat = (entry / "stat").read_text()
                current_fields = current_stat.rsplit(")", 1)[1].split()
                require(current_fields[19] == original_start, "compiler PID reused during census")
                if current_fields[0] in ("Z", "X"):
                    break
                if not raw_argv or not raw_argv.endswith(b"\0"):
                    continue
                reason = "live compiler executable is unobservable"
                after_executable = os.readlink(entry / "exe")
                after_argv = (entry / "cmdline").read_bytes()
                after_stat = (entry / "stat").read_text()
                after_fields = after_stat.rsplit(")", 1)[1].split()
                require(after_fields[19] == original_start, "compiler PID reused during census")
                if after_fields[0] in ("Z", "X"):
                    break
                if (executable != after_executable or raw_argv != after_argv or
                        fields[1] != after_fields[1] or
                        comm != current_stat.split("(", 1)[1].rsplit(")", 1)[0] or
                        comm != after_stat.split("(", 1)[1].rsplit(")", 1)[0]):
                    reason = "live compiler snapshot remains unstable"
                    continue
                argv = [os.fsdecode(arg) for arg in raw_argv[:-1].split(b"\0")]
                row = {"pid": int(entry.name), "ppid": int(after_fields[1]), "starttime": original_start,
                       "comm": comm, "executable": executable, "argv": argv}
                if kind == "rustc" or comm == "rustc":
                    result["rustc"].append(row)
                elif kind == "compile" or comm == "compile":
                    result["go_workers"].append(row)
                else:
                    args = argv[1:]
                    if args[:1] == ["-C"]:
                        args = args[2:]
                    elif args and args[0].startswith("-C="):
                        args = args[1:]
                    if args[:1] == ["test"] and any(arg in ("-c", "-c=true") for arg in args[1:]):
                        result["go_test_compilers"].append(row)
                break
            except (FileNotFoundError, ProcessLookupError, PermissionError) as error:
                last_error = error
                if not entry.exists():
                    break
                try:
                    current = (entry / "stat").read_text().rsplit(")", 1)[1].split()
                except (FileNotFoundError, ProcessLookupError, PermissionError) as error:
                    last_error = error
                    if not entry.exists():
                        break
                    continue
                require(original_start is None or current[19] == original_start,
                        "compiler PID reused during census")
                original_start = current[19]
                if current[0] in ("Z", "X"):
                    break
        else:
            try:
                require(False, reason + ": " + entry.name)
            except Exception as error:
                raise error from last_error
    for rows in result.values():
        rows.sort(key=lambda row: row["pid"])
    return result
