#!/usr/bin/env python3
"""Run one source-pinned Rust omission control without Cargo freshness guesses.

All owners sharing a target must use its lease. An additional process census
refuses pre-existing unleased Cargo/rustc jobs. Only this package is cleaned;
dependency artifacts remain warm. Tests execute an immutable copied ELF, never
the mutable target path. The recipe and every source byte are checked again
before results can be classified. No test or compiler failure is a causal pass.
"""

import argparse
import ctypes
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import selectors
import stat
import subprocess
import sys
import time
import tomllib


SCHEMA = "urnetwork-cargo-control-v1"
MAXIMUM_RECIPE_BYTES = 1024 * 1024
MAXIMUM_FILES = 256
MAXIMUM_FILE_BYTES = 8 * 1024 * 1024
MINIMUM_FREE_BYTES = 110 * 1024 ** 3
MAXIMUM_LOG_BYTES = 64 * 1024 * 1024


class Refused(Exception):
    """An unqualified harness outcome; it can never be an intended red."""


def require(condition, reason):
    if not condition:
        raise Refused(reason)


def digest(path):
    with Path(path).open("rb") as source:
        result = hashlib.file_digest(source, "sha256").hexdigest()
    return result


def bounded_regular_bytes(path, maximum):
    """Refuse aliases/devices and bound allocation before reading control input."""
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        require(stat.S_ISREG(before.st_mode) and before.st_size <= maximum,
                "control input is not a bounded regular file")
        pieces, used = [], 0
        while True:
            raw = os.read(descriptor, min(65536, maximum - used + 1))
            if not raw:
                break
            used += len(raw)
            require(used <= maximum, "control input grew beyond byte bound")
            pieces.append(raw)
        after = os.fstat(descriptor)
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
                and used == before.st_size, "control input changed while reading")
        return b"".join(pieces)
    finally:
        os.close(descriptor)


def read_recipe(path):
    raw = bounded_regular_bytes(path, MAXIMUM_RECIPE_BYTES)
    require(0 < len(raw) <= MAXIMUM_RECIPE_BYTES, "recipe byte bound")
    recipe = json.loads(raw)
    require(recipe.get("schema") == SCHEMA, "recipe schema differs")
    return recipe, hashlib.sha256(raw).hexdigest()


def census(root):
    """Bind the entire small crate, including untracked code/config additions."""
    result = {}
    require(root.is_absolute() and root.resolve() == root, "crate path aliases")
    for directory, children, names in os.walk(root, followlinks=False):
        children[:] = sorted(name for name in children if name not in (".git", "target"))
        for name in children:
            require(not (Path(directory) / name).is_symlink(), "crate directory alias")
        for name in sorted(names):
            if directory == str(root) and name == ".git":
                continue
            path = Path(directory) / name
            info = path.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
                    "crate file is not a unique regular file")
            require(info.st_size <= MAXIMUM_FILE_BYTES, "crate file byte bound")
            result[path.relative_to(root).as_posix()] = {
                "sha256": digest(path), "bytes": info.st_size,
            }
            require(len(result) <= MAXIMUM_FILES, "crate file count bound")
    require("Cargo.toml" in result and "Cargo.lock" in result, "crate manifest absent")
    return result


def validate_sources(recipe):
    baseline = Path(recipe["baseline_crate"])
    source = Path(recipe["source_crate"])
    require(baseline != source, "control must have its own physical crate")
    pinned = recipe["baseline_files"]
    require(census(baseline) == pinned, "frozen baseline crate changed")
    expected = dict(pinned)
    seen = set()
    for mutation in recipe["mutations"]:
        name = mutation["path"]
        require(name in pinned and name.startswith("src/") and name.endswith(".rs")
                and name not in seen, "control must name exact existing Rust sources")
        require(mutation["before"] == pinned[name]
                and mutation["after"] != pinned[name], "control mutation basis differs")
        seen.add(name)
        expected[name] = mutation["after"]
    require(0 < len(seen) <= 8, "control mutation count bound")
    require(census(source) == expected, "control has missing or undeclared source changes")
    package = tomllib.loads((source / "Cargo.toml").read_text())["package"]
    require(package["name"] == recipe["package"], "package identity differs")
    return source, expected


def active_target_processes(target):
    """Refuse existing target writers/readers; never stop another owner's job."""
    conflicts = []
    target_text = str(target)
    for item in Path("/proc").iterdir():
        if not item.name.isdigit() or int(item.name) == os.getpid():
            continue
        try:
            if item.stat().st_uid != os.getuid():
                continue
            args = (item / "cmdline").read_bytes().split(b"\0")
            if not args or not args[0]:
                continue
            command = os.fsdecode(args[0])
            values = [os.fsdecode(value) for value in args if value]
            relevant = Path(command).name in ("cargo", "rustc", "rustdoc")
            uses_target = any(target_text in value for value in values)
            if relevant and not uses_target:
                environment = (item / "environ").read_bytes().split(b"\0")
                uses_target = ("CARGO_TARGET_DIR=" + target_text).encode() in environment
                if not uses_target and Path(command).name == "cargo":
                    uses_target = (item / "cwd").resolve() / "target" == target
            if uses_target and (relevant or command.startswith(target_text + "/")):
                conflicts.append(int(item.name))
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError as error:
            raise Refused("cannot inspect an owned target process") from error
    return conflicts


def set_subreaper(enabled):
    """This dedicated runner owns one child tree at a time, never a shared host."""
    libc = ctypes.CDLL(None, use_errno=True)
    original = ctypes.c_int()
    require(libc.prctl(37, ctypes.byref(original), 0, 0, 0) == 0, "subreaper read failed")
    require(libc.prctl(36, int(enabled), 0, 0, 0) == 0, "subreaper update failed")
    return bool(original.value)


def owned_children():
    return [int(value) for value in Path(f"/proc/self/task/{os.getpid()}/children").read_text().split()]


def join_process_tree(process, failed):
    """Reap adopted descendants even after a leader exits or ignores TERM."""
    unexpected = False
    if failed or process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    end = time.monotonic() + 5
    while True:
        children = owned_children()
        for pid in children:
            if pid != process.pid:
                unexpected = True
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        if process.poll() is None:
            process.wait(timeout=max(0.001, end - time.monotonic()))
        while True:
            try:
                pid, _ = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                break
            if pid == 0:
                break
            unexpected = True
        if not owned_children():
            return unexpected
        require(time.monotonic() < end, "owned descendant cleanup did not join")
        time.sleep(0.01)


def run_process(args, cwd, environment, output, label, timeout,
                log_limit=MAXIMUM_LOG_BYTES, minimum_free=0,
                resource_observer=None, before_tree_join=None):
    """Stream bounded logs; cancel and reap descendants on every completion path."""
    stdout = output / (label + ".stdout")
    stderr = output / (label + ".stderr")
    require(0 < log_limit <= MAXIMUM_LOG_BYTES, "process log bound differs")
    process = None
    failed = True
    used = 0
    joined = False

    def join_owned(failing):
        """A sampler failure cannot bypass the actual owned-tree wait."""
        nonlocal joined
        joined = False
        try:
            if before_tree_join is not None:
                before_tree_join()
        finally:
            unexpected = join_process_tree(process, failing)
            joined = True
        return unexpected

    try:
        with stdout.open("xb") as out, stderr.open("xb") as err:
            original_subreaper = set_subreaper(True)
            try:
                require(not owned_children(), "runner already owns an unrelated child")
                process = subprocess.Popen(args, cwd=cwd, env=environment, stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, start_new_session=True)
                with selectors.DefaultSelector() as select:
                    for pipe, destination in ((process.stdout, out), (process.stderr, err)):
                        os.set_blocking(pipe.fileno(), False)
                        select.register(pipe, selectors.EVENT_READ, destination)
                    deadline = time.monotonic() + timeout
                    while select.get_map() or process.poll() is None:
                        if time.monotonic() >= deadline:
                            raise subprocess.TimeoutExpired(args, timeout)
                        require(shutil.disk_usage(output).free >= minimum_free,
                                "active phase crossed the shared floor")
                        if resource_observer is not None:
                            resource_observer()
                        for key, _ in select.select(timeout=0.05):
                            raw = os.read(key.fileobj.fileno(), min(65536, log_limit - used + 1))
                            if raw:
                                used += len(raw)
                                require(used <= log_limit, "process output exceeds reviewed log forecast")
                                key.data.write(raw)
                            else:
                                select.unregister(key.fileobj)
                        if process.poll() is not None and owned_children():
                            require(not join_owned(False),
                                    "leader exited while descendants retained process custody")
                    code = process.wait()
                require(not join_owned(False), "surviving descendant after leader exit")
                failed = False
            finally:
                try:
                    if process is not None:
                        try:
                            join_owned(failed)
                        finally:
                            process.stdout.close()
                            process.stderr.close()
                finally:
                    set_subreaper(original_subreaper)
        return {"argv": args, "exit": code, "stdout_sha256": digest(stdout),
                "stderr_sha256": digest(stderr), "log_bytes": used, "tree_joined": True}
    except BaseException as error:
        # Preserve the original exception type/cause. Only an actual completed
        # owned-tree join supplies this wait; callers must keep it unqualified.
        # Reusing an exception instance must not reuse an earlier child's wait.
        error.qualification_process_result = None
        error.qualification_process_reference = None
        if process is not None and joined and type(process.returncode) is int:
            error.qualification_process_result = {
                "argv": args, "pid": process.pid, "exit": process.returncode,
                "stdout_sha256": digest(stdout), "stderr_sha256": digest(stderr),
                "log_bytes": used, "tree_joined": True,
                "guard_failure": {"type": type(error).__name__, "detail": str(error)}}
        raise


def fresh_artifact(path, package, crate):
    """Cargo's explicit root artifact is required, not elapsed time or mtimes."""
    selected = []
    for raw in bounded_regular_bytes(path, MAXIMUM_LOG_BYTES).splitlines():
        try:
            item = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            continue
        if item.get("reason") != "compiler-artifact":
            continue
        target = item.get("target", {})
        if target.get("name") == package.replace("-", "_") and item.get("profile", {}).get("test"):
            require(Path(target.get("src_path", "")).resolve() == crate / "src/lib.rs",
                    "compiler artifact points at another crate")
            require(item.get("fresh") is False, "Cargo reused a stale control artifact")
            require(item.get("executable"), "test artifact executable is absent")
            selected.append(item)
    require(len(selected) == 1, "exactly one newly compiled root test artifact is required")
    return selected[0]


def classify_rust_output(code, stdout, stderr, selector, expected_assertion=None):
    """Classify one libtest root; nocapture output may split its status line.

    This authenticates result structure, not executable/source provenance. The
    caller must retain that separate custody and process-join evidence. Libtest
    owns the unique stdout census and terminal summary; diagnostics on either
    stream may supply the intended assertion, but never a second test census.
    """
    require(isinstance(stdout, bytes) and isinstance(stderr, bytes)
            and len(stdout) <= MAXIMUM_LOG_BYTES and len(stderr) <= MAXIMUM_LOG_BYTES,
            "Rust output exceeds its bounded byte inputs")
    require(isinstance(selector, str) and 0 < len(selector) <= 4096
            and not any(value.isspace() for value in selector), "Rust selector is not exact")
    causal = expected_assertion is not None
    selected_reason = ("control did not execute exactly the selected failed root" if causal
                       else "Rust output did not execute exactly the selected root")
    output = stdout.decode(errors="replace")
    diagnostic = stderr.decode(errors="replace")
    run_pattern = r"(?m)^running ([0-9]+) tests?\r?$"
    root_pattern = r"(?m)^test (\S+) \.\.\.(?:[ \t]|$)"
    summary_prefix = r"(?m)^test result:"
    runs = list(re.finditer(run_pattern, output))
    roots = list(re.finditer(root_pattern, output))
    require(len(runs) == 1 and runs[0].group(1) == "1"
            and len(roots) == 1 and roots[0].group(1) == selector
            and runs[0].end() <= roots[0].start()
            and not re.search(run_pattern, diagnostic)
            and not re.search(root_pattern, diagnostic), selected_reason)
    require(len(re.findall(summary_prefix, output)) == 1
            and not re.search(summary_prefix, diagnostic),
            "Rust output requires exactly one official terminal summary")
    summaries = list(re.finditer(
        r"(?m)^test result: (ok|FAILED)\. ([0-9]+) passed; ([0-9]+) failed; ([0-9]+) ignored;"
        r"(?: ([0-9]+) measured; ([0-9]+) filtered out;(?: finished in [0-9]+(?:\.[0-9]+)?s)?)?"
        r"[ \t]*\r?$", output))
    require(len(summaries) == 1 and roots[0].end() <= summaries[0].start(),
            "Rust output requires exactly one complete ordered terminal summary")
    summary = summaries[0]
    expected = (101, "FAILED", "0", "1", "0") if causal else (0, "ok", "1", "0", "0")
    require(type(code) is int and (code, *summary.group(1, 2, 3, 4)) == expected
            and summary.group(5) in (None, "0"),
            "Rust exit/count differs from one intended behavioral failure" if causal
            else "Rust exit/count differs from one positive root without skips")
    if causal:
        require(isinstance(expected_assertion, str) and expected_assertion
                and (expected_assertion in output or expected_assertion in diagnostic),
                "control did not reach its exact intended assertion")
    return {"root": selector, "classification": "EXPECTED_BEHAVIORAL_FAILURE" if causal else "PASS",
            "exit": code, "passed": int(summary.group(2)), "failed": int(summary.group(3)),
            "ignored": int(summary.group(4)), "measured": int(summary.group(5) or "0"),
            "filtered_out": int(summary.group(6) or "0"), "summary": summary.group(0).strip()}


def classify_rust_root(code, stdout, stderr, selector, expected_assertion=None):
    """Read bounded immutable result files before classifying a single root."""
    return classify_rust_output(code, bounded_regular_bytes(stdout, MAXIMUM_LOG_BYTES),
                                bounded_regular_bytes(stderr, MAXIMUM_LOG_BYTES),
                                selector, expected_assertion)


def classify_test(code, stdout, stderr, selector, expected_assertion):
    """One selected assertion failure is distinct from setup, panic, or timeout."""
    require(isinstance(expected_assertion, str) and expected_assertion,
            "control did not reach its exact intended assertion")
    return classify_rust_root(code, stdout, stderr, selector, expected_assertion)


def run(recipe_path, output):
    recipe, recipe_sha = read_recipe(recipe_path)
    source, expected = validate_sources(recipe)
    target = Path(recipe["target_dir"])
    require(target.is_absolute() and target.resolve() == target and target.is_dir(),
            "target must be an existing exact owned directory")
    require(target.stat().st_uid == os.getuid(), "target has another owner")
    require(0 < recipe["compile_seconds"] <= 900 and 0 < recipe["test_seconds"] <= 900,
            "phase time budgets must be finite")
    require(1 <= recipe.get("jobs", 2) <= 2, "control compiler job bound")
    require(recipe.get("minimum_free_bytes", MINIMUM_FREE_BYTES) >= MINIMUM_FREE_BYTES,
            "control cannot lower the shared admission floor")
    forecast = recipe["forecast"]
    require(set(forecast) == {"compile_bytes", "retained_elf_bytes", "log_bytes"}
            and all(type(value) is int and value > 0 for value in forecast.values())
            and forecast["log_bytes"] <= MAXIMUM_LOG_BYTES,
            "positive reviewed compile/ELF/log forecast required")
    floor = recipe.get("minimum_free_bytes", MINIMUM_FREE_BYTES)
    require(shutil.disk_usage(target).free >= floor + 2 * sum(forecast.values()),
            "target lacks twice the reviewed future compile/retention/log increment")
    for tool in ("cargo", "rustc"):
        tool_path = Path(recipe[tool]["path"])
        require(tool_path.is_absolute() and tool_path.resolve() == tool_path,
                tool + " must pin the actual toolchain executable, not a shim")
        require(digest(recipe[tool]["path"]) == recipe[tool]["sha256"],
                tool + " executable differs")
    require(shutil.disk_usage(target).free >= recipe.get("minimum_free_bytes", MINIMUM_FREE_BYTES),
            "target is below the admission floor")
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    receipt = {"schema": SCHEMA, "status": "UNQUALIFIED", "recipe_sha256": recipe_sha,
               "runner_sha256": digest(Path(__file__)), "source_files": expected,
               "started_unix": time.time(), "steps": []}
    lease = target / ".urnetwork-cargo-control.lease"
    descriptor = os.open(lease, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not active_target_processes(target), "target has an active Cargo/rustc/test owner")
        environment = os.environ.copy()
        environment.update(recipe.get("environment", {}))
        require(set(recipe.get("environment", {})) <= {
            "CARGO_HOME", "RUSTUP_HOME", "PATH", "CARGO_PROFILE_DEV_DEBUG",
            "CARGO_PROFILE_TEST_DEBUG", "TMPDIR",
        }, "unreviewed compiler environment field")
        environment.update({"RUSTC": recipe["rustc"]["path"], "CARGO_TARGET_DIR": str(target),
                            "CARGO_INCREMENTAL": "0", "CARGO_BUILD_JOBS": str(recipe.get("jobs", 2))})
        for key in ("RUSTFLAGS", "CARGO_ENCODED_RUSTFLAGS", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER"):
            require(not environment.get(key), "ambient compiler override: " + key)
        cargo = recipe["cargo"]["path"]
        package_version = tomllib.loads((source / "Cargo.toml").read_text())["package"]["version"]
        clean = [cargo, "clean", "--locked", "--offline", "--package",
                 recipe["package"] + "@" + package_version,
                 "--target-dir", str(target)]
        receipt["forecast"] = forecast
        receipt["steps"].append(run_process(clean, source, environment, output, "clean", 60,
                                            forecast["log_bytes"], floor))
        require(receipt["steps"][-1]["exit"] == 0, "package-only clean failed")
        compile_args = [cargo, "test", "--locked", "--offline", "--lib", "--no-run",
                        "--message-format=json", "--target-dir", str(target)]
        receipt["steps"].append(run_process(compile_args, source, environment, output,
                                            "compile", recipe["compile_seconds"],
                                            forecast["log_bytes"] - sum(item["log_bytes"] for item in receipt["steps"]), floor))
        require(receipt["steps"][-1]["exit"] == 0, "control did not compile")
        require(validate_sources(recipe)[1] == expected, "source changed during compilation")
        artifact = fresh_artifact(output / "compile.stdout", recipe["package"], source)
        executable = Path(artifact["executable"])
        require(executable.resolve().is_relative_to(target), "artifact escaped owned target")
        require(executable.stat().st_size <= forecast["retained_elf_bytes"],
                "compiled ELF exceeds reviewed retention forecast")
        remaining_logs = forecast["log_bytes"] - sum(item["log_bytes"] for item in receipt["steps"])
        require(shutil.disk_usage(output).free >= floor + 2 * (executable.stat().st_size + remaining_logs),
                "artifact retention lacks twice its remaining reviewed increment")
        retained = output / "control-test"
        shutil.copyfile(executable, retained)
        retained.chmod(0o500)
        executable_sha = digest(executable)
        require(digest(retained) == executable_sha, "artifact changed during retention")
        receipt["artifact"] = {"cargo": artifact, "path": str(retained), "sha256": executable_sha}
        test_args = [str(retained), "--exact", recipe["test"], "--nocapture", "--test-threads=1"]
        receipt["steps"].append(run_process(test_args, source, environment, output,
                                            "test", recipe["test_seconds"], remaining_logs, floor))
        require(validate_sources(recipe)[1] == expected, "source changed during test")
        require(read_recipe(recipe_path)[1] == recipe_sha, "recipe changed during control")
        require(digest(retained) == executable_sha, "executed artifact changed")
        classify_test(receipt["steps"][-1]["exit"], output / "test.stdout", output / "test.stderr",
                      recipe["test"], recipe["expected_assertion"])
        receipt["status"] = "EXPECTED_BEHAVIORAL_FAILURE"
    except BaseException as error:
        receipt["error"] = str(error)
        raise
    finally:
        receipt["finished_unix"] = time.time()
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
        os.close(descriptor)


def main():
    os.umask(0o077)
    def interrupted(signum, _frame):
        raise KeyboardInterrupt("owned Cargo control interrupted by signal " + str(signum))
    signal.signal(signal.SIGTERM, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("recipe", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        run(args.recipe.resolve(), args.output.absolute())
    except (Refused, OSError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
        print("Cargo control UNQUALIFIED:", error, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
