"""Reuse a complete content proof while its local physical inputs stay unchanged.

This is source custody, not a test-result cache. Capture reads every declared
input and closes every declared filename census. Later checks read metadata,
including kernel ctime, instead of the source bytes. The trust boundary is a
local Linux filesystem, a trusted kernel, and no privileged filesystem rollback.
Owner-writable files and hardlinks are permitted; neither implies immutability.

A saved proof is usable only through an independently pinned SHA256 and the
same complete request. Its existence, an executable hash, or a previous PASS
does not authorize reuse. Per-phase executable/output custody remains separate.
"""

import copy
from contextlib import contextmanager
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import threading
import time


SCHEMA = "urnetwork-source-verification-v1"
TRUST_PROFILE = "trusted-local-exclusive-source-owner-v1"
MAXIMUM_FILES = 100000
MAXIMUM_DIRECTORIES = 100000
MAXIMUM_DIRECTORY_DEPTH = 256
MAXIMUM_DIRECTORY_ENTRIES = 900000
MAXIMUM_CENSUS_BYTES = 64 * 1024**2
MAXIMUM_CENSUS_LEAVES = 800000
MAXIMUM_FILE_BYTES = 2 * 1024**3
MAXIMUM_TOTAL_BYTES = 32 * 1024**3
MAXIMUM_PROOF_BYTES = 96 * 1024**2
MAXIMUM_CONTEXT_BYTES = 4 * 1024**2
MAXIMUM_MOUNT_BYTES = 4 * 1024**2
LOCAL_FILESYSTEMS = {"ext4": 0xEF53, "xfs": 0x58465342,
                     "btrfs": 0x9123683E, "tmpfs": 0x01021994}
TRANSIENT_ERRNOS = {errno.EINTR, errno.EAGAIN, errno.EBUSY, errno.ESTALE,
                    errno.ETIMEDOUT, errno.EIO, errno.ECONNRESET}
IDENTITY_FIELDS = {"device", "inode", "type", "mode", "uid", "gid", "bytes",
                   "mtime_ns", "ctime_ns", "links"}
ANCESTOR_FIELDS = {"device", "inode", "type", "mode", "uid", "gid"}
CONTEXT_FIELDS = {"source_closure", "dependency_graph", "tools", "configuration", "mode"}


class SourceIntegrityError(Exception):
    """A proved input or its trusted physical boundary contradicts the request."""


class SourceUnavailableError(Exception):
    """No equality observation was obtained within the bounded read owner."""


def _require(condition, message):
    if not condition:
        raise SourceIntegrityError(message)


def _canonical(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(",", ":"),
                          allow_nan=False).encode()
    except (TypeError, ValueError) as error:
        raise SourceIntegrityError("input is not canonical JSON data") from error


def _path(value):
    _require(isinstance(value, str) and "\x00" not in value, "invalid input path")
    path = Path(value)
    _require(path.is_absolute() and str(path) == value and ".." not in path.parts,
             "input path must be absolute and normalized")
    return path


def _identity(info):
    return {"device": info.st_dev, "inode": info.st_ino,
            "type": stat.S_IFMT(info.st_mode), "mode": stat.S_IMODE(info.st_mode),
            "uid": info.st_uid, "gid": info.st_gid, "bytes": info.st_size,
            "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns,
            "links": info.st_nlink}


class ReadPolicy:
    """Create a fresh finite retry owner for each capture, load, or verification."""

    def __init__(self, attempts=3, delay_seconds=0.05, timeout_seconds=300,
                 cancel_event=None):
        _require(type(attempts) is int and 1 <= attempts <= 8, "invalid read attempts")
        _require(0 <= delay_seconds <= 1 and 0 < timeout_seconds <= 3600,
                 "invalid read budget")
        self.attempts = attempts
        self.delay_seconds = delay_seconds
        self.timeout_seconds = timeout_seconds
        self.cancel_event = cancel_event

    def owner(self):
        return _ReadOwner(self)


class _ReadOwner:
    def __init__(self, policy):
        self.policy = policy
        self.deadline = time.monotonic() + policy.timeout_seconds

    def check(self):
        if self.policy.cancel_event is not None and self.policy.cancel_event.is_set():
            raise SourceUnavailableError("source read canceled")
        if time.monotonic() >= self.deadline:
            raise SourceUnavailableError("source read budget exhausted")

    def call(self, operation, *args, **kwargs):
        failures = []
        for attempt in range(self.policy.attempts):
            try:
                self.check()
            except SourceUnavailableError as error:
                if failures:
                    raise error from ExceptionGroup("original source read failures", failures)
                raise
            try:
                return operation(*args, **kwargs)
            except OSError as error:
                if error.errno not in TRANSIENT_ERRNOS:
                    cause = (ExceptionGroup("original source read failures", failures + [error])
                             if failures else error)
                    raise SourceIntegrityError("source read refused: " + str(error)) from cause
                failures.append(error)
                if attempt + 1 < self.policy.attempts:
                    delay = min(self.policy.delay_seconds,
                                max(0, self.deadline - time.monotonic()))
                    if self.policy.cancel_event is None:
                        time.sleep(delay)
                    else:
                        self.policy.cancel_event.wait(delay)
        raise SourceUnavailableError("transient source reads did not recover") from ExceptionGroup(
            "original source read failures", failures)


def _request(files, directory_inventories, context):
    _require(isinstance(files, dict) and 0 < len(files) <= MAXIMUM_FILES,
             "bounded complete file map is required")
    _require(isinstance(directory_inventories, dict)
             and len(directory_inventories) <= MAXIMUM_DIRECTORIES,
             "bounded closed directory map is required")
    _require(isinstance(context, dict) and CONTEXT_FIELDS <= set(context)
             and all(context[field] for field in CONTEXT_FIELDS),
             "complete source/dependency/tool/configuration/mode context is required")
    _require(context.get("trust_profile") == TRUST_PROFILE,
             "explicit trusted exclusive source-owner profile is required")
    _require(len(_canonical(context)) <= MAXIMUM_CONTEXT_BYTES, "context exceeds bound")
    _require(isinstance(context["mode"], str), "mode must be explicit")
    for name, checksum in files.items():
        _path(name)
        _require(isinstance(checksum, str) and re.fullmatch("[0-9a-f]{64}", checksum),
                 "invalid source SHA256")
    count = 0
    for root, names in directory_inventories.items():
        _path(root)
        _require(isinstance(names, list) and all(isinstance(n, str) for n in names)
                 and names == sorted(set(names)), "closed names must be sorted and unique")
        count += len(names)
        _require(count <= MAXIMUM_CENSUS_LEAVES, "closed filename census exceeds bound")
        for name in names:
            p = Path(name)
            _require(name and p.parts and not p.is_absolute() and str(p) == name
                     and ".." not in p.parts and "\x00" not in name,
                     "closed filename escapes its root")
    return json.loads(_canonical({"files": files, "directory_inventories": directory_inventories,
                                  "context": context}))


def _mount_path(value):
    return re.sub(r"\\([0-7]{3})", lambda match: chr(int(match[1], 8)), value)


@contextmanager
def _descriptor(path, flags, owner):
    descriptor = owner.call(os.open, path, flags)
    try:
        yield descriptor
    except BaseException as original:
        try:
            os.close(descriptor)
        except OSError as error:
            raise SourceIntegrityError("source descriptor close refused") from BaseExceptionGroup(
                "original source and close failures", [original, error])
        raise
    else:
        try:
            os.close(descriptor)
        except OSError as error:
            # A failed close is never retried on a possibly reused descriptor.
            raise SourceIntegrityError("source descriptor close refused") from error


def _mountinfo(owner):
    chunks, used = [], 0
    with _descriptor("/proc/self/mountinfo", os.O_RDONLY | os.O_NOFOLLOW, owner) as descriptor:
        while used <= MAXIMUM_MOUNT_BYTES:
            chunk = owner.call(os.read, descriptor, min(65536, MAXIMUM_MOUNT_BYTES - used + 1))
            if not chunk:
                break
            chunks.append(chunk)
            used += len(chunk)
    raw = b"".join(chunks)
    _require(len(raw) <= MAXIMUM_MOUNT_BYTES, "mount table exceeds bound")
    return raw


class _Mounts:
    """Pin the actual mount mapping; unrelated new mounts do not poison reuse."""

    def __init__(self, owner):
        raw = _mountinfo(owner)
        self.rows = []
        try:
            for line in raw.decode().splitlines():
                left, right = line.split(" - ", 1)
                a, b = left.split(), right.split()
                self.rows.append({"id": int(a[0]), "parent": int(a[1]), "device": a[2],
                                  "root": _mount_path(a[3]), "path": _mount_path(a[4]),
                                  "options": a[5:], "filesystem": b[0],
                                  "source": _mount_path(b[1]), "super_options": b[2:]})
        except (ValueError, IndexError) as error:
            raise SourceIntegrityError("kernel mount table is malformed") from error
        self.by_path = {}
        for row in self.rows:
            self.by_path.setdefault(row["path"], []).append(row)
        self.selected = {}
        self.checked = set()

    def select(self, path):
        if path in self.selected:
            return self.selected[path]
        parent = path
        while True:
            rows = self.by_path.get(parent)
            if rows is not None:
                # Namespace stacks elsewhere do not alter this selected path.
                # Never infer a visible winner when this path's mapping is stacked.
                _require(len(rows) == 1, "ambiguous selected stacked mount mapping: " + parent)
                row = rows[0]
                _require(row["filesystem"] in LOCAL_FILESYSTEMS,
                         "source reuse requires a supported trusted local filesystem")
                self.selected[path] = row
                return row
            if parent == "/":
                break
            parent = parent.rpartition("/")[0] or "/"
        raise SourceIntegrityError("source path has no kernel mount identity")

    def check_filesystem(self, path, row, owner):
        if row["id"] in self.checked:
            return
        with _descriptor(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, owner) as descriptor:
            libc = ctypes.CDLL(None, use_errno=True)
            storage = ctypes.create_string_buffer(512)

            def observe():
                if libc.fstatfs(descriptor, ctypes.byref(storage)) != 0:
                    code = ctypes.get_errno()
                    raise OSError(code, os.strerror(code), path)
                return ctypes.c_long.from_buffer(storage).value & 0xFFFFFFFF

            actual = owner.call(observe)
            _require(actual == LOCAL_FILESYSTEMS[row["filesystem"]],
                     "statfs differs from selected local mount")
        self.checked.add(row["id"])


def _observe(path, owner):
    return _identity(owner.call(os.lstat, path))


def _hash_file(path, expected, owner, maximum=MAXIMUM_FILE_BYTES, retain_bytes=False):
    before_name = _observe(path, owner)
    _require(before_name["type"] == stat.S_IFREG and before_name["links"] > 0
             and before_name["bytes"] <= maximum, "source is not bounded regular data")
    with _descriptor(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, owner) as descriptor:
        before = _identity(owner.call(os.fstat, descriptor))
        _require(before == before_name, "source changed while opening")
        used, checksum, chunks = 0, hashlib.sha256(), []
        while True:
            chunk = owner.call(os.read, descriptor, min(1024 * 1024, before["bytes"] - used + 1))
            if not chunk:
                break
            used += len(chunk)
            _require(used <= before["bytes"], "source grew while reading")
            checksum.update(chunk)
            if retain_bytes:
                chunks.append(chunk)
        _require(used == before["bytes"] and checksum.hexdigest() == expected,
                 "source content differs from declared SHA256: " + path)
        _require(_identity(owner.call(os.fstat, descriptor)) == before
                 and _observe(path, owner) == before, "source changed while reading: " + path)
        return before, b"".join(chunks) if retain_bytes else None


def _directory(path, owner):
    before = _observe(path, owner)
    _require(before["type"] == stat.S_IFDIR, "closed directory aliases or changed type")
    with _descriptor(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, owner) as descriptor:
        _require(_identity(owner.call(os.fstat, descriptor)) == before,
                 "closed directory changed while opening")

        def names():
            # A transient partial readdir must retry the complete same namespace.
            os.lseek(descriptor, 0, os.SEEK_SET)
            result, used = [], 0
            with os.scandir(descriptor) as entries:
                for entry in entries:
                    owner.check()
                    _require(len(result) < MAXIMUM_DIRECTORY_ENTRIES,
                             "closed directory entry count exceeds bound")
                    used += len(os.fsencode(entry.name))
                    _require(used <= MAXIMUM_CENSUS_BYTES,
                             "closed directory name bytes exceed bound")
                    result.append(entry.name)
            return sorted(result)

        result = owner.call(names)
        _require(_identity(owner.call(os.fstat, descriptor)) == before
                 and _observe(path, owner) == before, "closed directory changed while reading")
        return before, result


class SourceVerification:
    """One sticky source proof, safe for serialized checks from multiple threads."""

    def __init__(self, data, policy, reference=None):
        self._data = data
        self._policy = policy
        self._reference = reference
        self._invalid = None
        self._lock = threading.Lock()

    @classmethod
    def capture(cls, files, directory_inventories, context, *, retry=None):
        policy = retry or ReadPolicy()
        owner = policy.owner()
        request = _request(files, directory_inventories, context)
        mounts = _Mounts(owner)
        data = {"schema": SCHEMA, "request": request,
                "key": hashlib.sha256(_canonical(request)).hexdigest(),
                "files": {}, "directories": {}, "links": {}, "ancestors": {}, "mounts": {}}
        visited = {}

        def walk(root, depth=0):
            _require(depth <= MAXIMUM_DIRECTORY_DEPTH, "closed directory depth exceeds bound")
            if root in visited:
                return visited[root]
            before, names = _directory(root, owner)
            _require(len(data["directories"]) < MAXIMUM_DIRECTORIES, "directory proof exceeds bound")
            data["directories"][root] = before
            leaves, used = [], 0

            def append(leaf):
                nonlocal used
                _require(len(leaves) < MAXIMUM_CENSUS_LEAVES,
                         "closed filename leaf count exceeds bound")
                used += len(os.fsencode(leaf))
                _require(used <= MAXIMUM_CENSUS_BYTES, "closed filename bytes exceed bound")
                leaves.append(leaf)

            for name in names:
                path = str(Path(root) / name)
                info = _observe(path, owner)
                if info["type"] == stat.S_IFDIR:
                    for leaf in walk(path, depth + 1):
                        owner.check()
                        append(name + "/" + leaf)
                else:
                    _require(info["type"] in (stat.S_IFREG, stat.S_IFLNK),
                             "closed census contains a special file")
                    append(name)
                    if info["type"] == stat.S_IFLNK:
                        data["links"][path] = info
            _require(_observe(root, owner) == before, "closed directory changed during census")
            visited[root] = sorted(leaves)
            return visited[root]

        for root, names in request["directory_inventories"].items():
            _require(walk(root) == names, "closed filename census differs: " + root)
        total = 0
        for path, expected in request["files"].items():
            info, _ = _hash_file(path, expected, owner)
            data["files"][path] = info
            total += info["bytes"]
            _require(total <= MAXIMUM_TOTAL_BYTES, "source byte census exceeds bound")
        selected = set(data["files"]) | set(data["directories"]) | set(data["links"])
        for path in selected:
            for parent in Path(path).parents:
                name = str(parent)
                if name not in data["ancestors"]:
                    info = _observe(name, owner)
                    _require(info["type"] == stat.S_IFDIR, "source ancestor aliases or changed type")
                    data["ancestors"][name] = {key: info[key] for key in ANCESTOR_FIELDS}
        for path in selected | set(data["ancestors"]):
            row = mounts.select(path)
            data["mounts"][path] = row
            # Symlinks in a name census are not followed or used as content inputs.
            if path not in data["links"]:
                mounts.check_filesystem(path, row, owner)
        result = cls(data, policy)
        result._verify(owner)
        return result

    def _verify(self, owner):
        data = self._data
        mounts = _Mounts(owner)
        for path, expected in data["mounts"].items():
            _require(mounts.select(path) == expected, "source mount mapping changed: " + path)
            if path not in data["links"]:
                mounts.check_filesystem(path, expected, owner)
        for path, expected in data["ancestors"].items():
            actual = _observe(path, owner)
            _require({key: actual[key] for key in ANCESTOR_FIELDS} == expected,
                     "source ancestor mapping or ownership changed: " + path)
        for kind in ("directories", "links", "files"):
            for path, expected in data[kind].items():
                _require(_observe(path, owner) == expected,
                         "source " + kind + " identity changed: " + path)
        # Closing the mapping fence never reopens global source bytes.
        after = _Mounts(owner)
        for path, expected in data["mounts"].items():
            _require(after.select(path) == expected, "source mount mapping changed during check")
        for path, expected in data["ancestors"].items():
            actual = _observe(path, owner)
            _require({key: actual[key] for key in ANCESTOR_FIELDS} == expected,
                     "source ancestor changed during check")
        owner.check()

    def verify(self, context):
        """Check retained metadata; a proven contradiction can never be rebaselined."""
        with self._lock:
            self._verify_with_lock(context)
            return self._summary_with_lock()

    def _verify_with_lock(self, context):
        if self._invalid is not None:
            raise SourceIntegrityError("source proof already invalid: " + str(self._invalid)) from self._invalid
        try:
            _require(_canonical(context) == _canonical(self._data["request"]["context"]),
                     "source verification context changed")
            self._verify(self._policy.owner())
        except SourceIntegrityError as error:
            self._invalid = error
            raise

    def summary(self):
        """Keep large identity inventories outside existing small terminal receipts."""
        with self._lock:
            return self._summary_with_lock()

    def _summary_with_lock(self):
        return {"schema": SCHEMA, "key": self._data["key"],
                "proof": copy.deepcopy(self._reference),
                "files": len(self._data["files"]),
                "closed_roots": len(self._data["request"]["directory_inventories"]),
                "directories": len(self._data["directories"]),
                "mode": self._data["request"]["context"]["mode"],
                "claim": "source custody only; no execution or result qualification"}

    def save(self, destination):
        """Retain a separate bounded, create-once proof for independent recipe pinning."""
        with self._lock:
            self._verify_with_lock(self._data["request"]["context"])
            path = _path(str(destination))
            _require(all(path != Path(root) and Path(root) not in path.parents
                         for root in self._data["request"]["directory_inventories"]),
                     "proof output may not mutate a closed source root")
            encoded = _canonical(self._data) + b"\n"
            _require(len(encoded) <= MAXIMUM_PROOF_BYTES, "source proof exceeds bound")
            temporary = path.with_name(path.name + ".pending")
            descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            try:
                view = memoryview(encoded)
                while view:
                    count = os.write(descriptor, view)
                    _require(count > 0, "source proof write made no progress")
                    view = view[count:]
                os.fchmod(descriptor, 0o400)
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
            os.link(temporary, path, follow_symlinks=False)
            temporary.unlink()
            parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
            self._reference = {"path": str(path), "sha256": hashlib.sha256(encoded).hexdigest()}
            return copy.deepcopy(self._reference)

    @classmethod
    def load(cls, reference, files, directory_inventories, context, *, retry=None):
        """Reuse only a caller-authenticated proof for the exact complete request."""
        request = _request(files, directory_inventories, context)
        _require(isinstance(reference, dict) and set(reference) == {"path", "sha256"},
                 "independently pinned source proof is required")
        path = str(_path(reference["path"]))
        policy = retry or ReadPolicy()
        owner = policy.owner()
        _, raw = _hash_file(path, reference["sha256"], owner, MAXIMUM_PROOF_BYTES, True)
        try:
            data = json.loads(raw)
        except (ValueError, UnicodeDecodeError) as error:
            raise SourceIntegrityError("retained source proof is malformed") from error
        _require(isinstance(data, dict) and data.get("schema") == SCHEMA
                 and data.get("request") == request
                 and data.get("key") == hashlib.sha256(_canonical(request)).hexdigest(),
                 "retained source proof request differs")
        _require(set(data) == {"schema", "request", "key", "files", "directories",
                               "links", "ancestors", "mounts"}, "source proof fields differ")
        for kind in ("files", "directories", "links", "ancestors"):
            _require(isinstance(data[kind], dict) and len(data[kind]) <= MAXIMUM_FILES * 8,
                     "retained identity census exceeds bound")
            fields = ANCESTOR_FIELDS if kind == "ancestors" else IDENTITY_FIELDS
            for name, identity in data[kind].items():
                _path(name)
                _require(isinstance(identity, dict) and set(identity) == fields
                         and all(type(value) is int and value >= 0 for value in identity.values()),
                         "invalid retained source identity")
        _require(set(data["files"]) == set(files)
                 and set(directory_inventories) <= set(data["directories"]),
                 "source proof omits declared inputs")
        _require(isinstance(data["mounts"], dict)
                 and all(isinstance(row, dict) for row in data["mounts"].values()),
                 "invalid retained mount identities")
        selected = set(data["files"]) | set(data["directories"]) | set(data["links"])
        ancestors = {str(parent) for name in selected for parent in Path(name).parents}
        _require(set(data["ancestors"]) == ancestors
                 and set(data["mounts"]) == selected | ancestors, "source proof omits path custody")
        for root, names in directory_inventories.items():
            for name in names:
                parent = (Path(root) / name).parent
                while str(parent) != root:
                    _require(str(parent) in data["directories"], "source proof omits closed directory")
                    parent = parent.parent
        result = cls(data, policy, copy.deepcopy(reference))
        result._verify(owner)
        return result
