#!/usr/bin/env python3
"""Execute a pinned Go test image from one retained private inode.

Some fixtures use os.Executable() as a protected protocol peer. Go's automatic
cache/build executable may be hardlinked or have unsuitable permissions. This
runner records that original observation, copies its exact pinned bytes into a
new owned image, and verifies the image before and after bounded execution.
It does not classify tests or replace source/module/compiler qualification.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import time

import cargo_control
import child_context
from cargo_control import (MAXIMUM_LOG_BYTES, MINIMUM_FREE_BYTES, Refused,
                           bounded_regular_bytes, digest, require, run_process)
from child_context import ChildContext


SCHEMA = "urnetwork-go-test-image-v1"
MAXIMUM_IMAGE_BYTES = 512 * 1024 * 1024
MAXIMUM_RECIPE_BYTES = 1024 * 1024


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino,
            "bytes": info.st_size, "mode": stat.S_IMODE(info.st_mode),
            "type": stat.S_IFMT(info.st_mode), "links": info.st_nlink,
            "uid": info.st_uid, "gid": info.st_gid,
            "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}


def ancestry(path):
    result = []
    for parent in path.parents:
        result.append({"path": str(parent), **identity(parent.lstat())})
    return result


def pin(path, expected):
    require(isinstance(expected, str) and re.fullmatch(r"[0-9a-f]{64}", expected),
            "exact lowercase executable SHA256 is required")
    require(Path(path).is_absolute(), "executable path must be absolute")


def stage_image(source, expected, destination, maximum=MAXIMUM_IMAGE_BYTES):
    """Bound before reading, stream once, and retain exact source metadata."""
    pin(source, expected)
    require(0 < maximum <= MAXIMUM_IMAGE_BYTES, "image maximum differs")
    descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        observation = {"path": str(source), **identity(before), "ancestors": ancestry(source)}
        require(stat.S_ISREG(before.st_mode) and 4 <= before.st_size <= maximum,
                "original Go executable is not bounded regular data")
        require(before.st_mode & 0o111, "original Go executable is not executable")
        # A hardlinked original is observed, not used as the test's os.Executable.
        used, checksum = 0, hashlib.sha256()
        output = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o500)
        try:
            while True:
                raw = os.read(descriptor, min(65536, maximum - used + 1))
                if not raw:
                    break
                require(used != 0 or raw.startswith(b"\x7fELF"), "original test image is not ELF")
                used += len(raw)
                require(used <= maximum and used <= before.st_size, "original test image grew")
                checksum.update(raw)
                view = memoryview(raw)
                while view:
                    count = os.write(output, view)
                    require(count > 0, "test image copy made no progress")
                    view = view[count:]
            require(identity(os.fstat(descriptor)) == identity(before)
                    and used == before.st_size and checksum.hexdigest() == expected,
                    "original test image changed or differs from exact pin")
            os.fchmod(output, 0o500)
            os.fsync(output)
        finally:
            os.close(output)
        retained = {"path": str(destination), "sha256": expected, **identity(destination.lstat())}
        verify_image(retained)
        return {"original": observation, "retained": retained}
    finally:
        os.close(descriptor)


def verify_image(retained):
    path = Path(retained["path"])
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        expected = {key: retained[key] for key in identity(before)}
        require(identity(before) == expected and stat.S_ISREG(before.st_mode)
                and before.st_nlink == 1 and before.st_uid == os.getuid()
                and stat.S_IMODE(before.st_mode) == 0o500
                and 4 <= before.st_size <= MAXIMUM_IMAGE_BYTES,
                "retained Go test image lost unique protected ownership")
        checksum = hashlib.sha256()
        used = 0
        while True:
            raw = os.read(descriptor, min(65536, before.st_size - used + 1))
            if not raw:
                break
            used += len(raw)
            require(used <= before.st_size, "retained Go image grew during verification")
            checksum.update(raw)
        require(identity(os.fstat(descriptor)) == expected
                and identity(path.lstat()) == expected and used == before.st_size
                and checksum.hexdigest() == retained["sha256"],
                "retained Go test image bytes or named identity changed")
    finally:
        os.close(descriptor)


def read_recipe(path):
    raw = bounded_regular_bytes(path, MAXIMUM_RECIPE_BYTES)
    value = json.loads(raw)
    require(value.get("schema") == SCHEMA, "Go test image recipe schema differs")
    return value, hashlib.sha256(raw).hexdigest()


def run(recipe_path, output):
    recipe, recipe_sha = read_recipe(recipe_path)
    require(recipe["expected_uid"] == os.getuid(), "test image execution UID differs")
    source = Path(recipe["source_image"]["path"])
    pin(source, recipe["source_image"]["sha256"])
    cwd = Path(recipe["cwd"])
    require(cwd.is_absolute() and cwd.resolve() == cwd and cwd.is_dir(), "test cwd aliases or is absent")
    require(output.is_absolute() and output.resolve() == output, "output path aliases")
    require(0 < recipe["timeout_seconds"] <= 3600, "test owner requires finite <=3600 second budget")
    arguments = recipe["arguments"]
    require(isinstance(arguments, list) and 0 < len(arguments) <= 32
            and all(isinstance(arg, str) and arg.startswith("-test.") and len(arg) <= 16384 for arg in arguments)
            and "-test.count=1" in arguments
            and sum(arg.startswith("-test.count=") for arg in arguments) == 1
            and sum(arg.startswith("-test.run=") and len(arg) > 10 for arg in arguments) == 1,
            "exact bounded Go test arguments with one selector and count1 required")
    floor = recipe.get("minimum_free_bytes", MINIMUM_FREE_BYTES)
    require(type(floor) is int and floor >= MINIMUM_FREE_BYTES, "shared test floor cannot be lowered")
    forecast = recipe["forecast"]
    require(set(forecast) == {"retained_image_bytes", "log_bytes"}
            and all(type(value) is int and value > 0 for value in forecast.values())
            and forecast["retained_image_bytes"] <= MAXIMUM_IMAGE_BYTES
            and forecast["log_bytes"] <= MAXIMUM_LOG_BYTES,
            "reviewed positive test image/log forecast required")
    require(shutil.disk_usage(output.parent).free >= floor + 2 * sum(forecast.values()),
            "test image lacks twice its reviewed retention/log increment")
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    receipt = {"schema": SCHEMA, "status": "HARNESS_REFUSAL", "recipe_sha256": recipe_sha,
               "runner_sha256": digest(Path(__file__)),
               "process_guard_path": str(Path(cargo_control.__file__).resolve()),
               "process_guard_sha256": digest(Path(cargo_control.__file__).resolve()),
               "context_guard_path": str(Path(child_context.__file__).resolve()),
               "context_guard_sha256": digest(Path(child_context.__file__).resolve()),
               "forecast": forecast, "started_unix": time.time(),
               "test_classification": "not-performed; caller must verify selected test events and source/module/build joins"}
    try:
        receipt["image"] = stage_image(source, recipe["source_image"]["sha256"],
                                       output / "go-test", forecast["retained_image_bytes"])
        environment = os.environ.copy()
        overrides = recipe.get("environment", {})
        require(set(overrides) <= {"PATH", "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPROXY",
                                  "GOSUMDB", "GOENV", "GOTOOLCHAIN", "GOPATH"}, "unreviewed Go fixture environment override")
        environment.update(overrides)
        # Parent compilation flags must not retarget a fixture's own go-list
        # subprocess to the parent module or overlay.
        environment.pop("GOFLAGS", None)
        environment["GOWORK"] = "off"
        receipt["child_goflags"] = "unset"
        receipt["child_gowork"] = "off"
        context = ChildContext(cwd, environment, recipe.get("runner_python"))
        context.bind(output / "go-test", recipe["source_image"]["sha256"])
        arguments = [str(output / "go-test"), *arguments]
        if "go_tool" in recipe:
            tool = recipe["go_tool"]
            require(isinstance(tool, dict) and set(tool) == {"path", "sha256"},
                    "Go JSON execution requires an exact Go executable pin")
            receipt["go_tools"] = context.prepare_go(tool["path"], tool["sha256"], output,
                                                      minimum_free=floor,
                                                      log_limit=forecast["log_bytes"])
            arguments = [receipt["go_tools"]["test2json"]["path"], "-t", *arguments]
        verify_image(receipt["image"]["retained"])
        require(shutil.disk_usage(output).free >= floor + 2 * forecast["log_bytes"],
                "test logs lack twice the remaining reviewed increment")
        probe_logs = sum(step["log_bytes"] for step in receipt.get("go_tools", {}).get("probes", []))
        require(probe_logs < forecast["log_bytes"], "tool preflight consumed the reviewed log forecast")
        receipt["execution"] = context.run(arguments, output, "test", recipe["timeout_seconds"],
                                             forecast["log_bytes"] - probe_logs, floor)
        verify_image(receipt["image"]["retained"])
        require(read_recipe(recipe_path)[1] == recipe_sha, "test image recipe changed")
        receipt["status"] = "SOURCE_PINNED_EXECUTION_ONLY"
    except BaseException as error:
        receipt["error"] = str(error)
        raise
    finally:
        receipt["finished_unix"] = time.time()
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")


def main():
    os.umask(0o077)
    def interrupted(signum, _frame):
        raise KeyboardInterrupt("owned Go test interrupted by signal " + str(signum))
    signal.signal(signal.SIGTERM, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("recipe", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        run(args.recipe.absolute(), args.output.absolute())
    except (Refused, OSError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
        print("Go test image harness refused:", error, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
