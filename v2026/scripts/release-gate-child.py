#!/usr/bin/env python3
"""Own one release phase, including orphaned children that create new sessions."""

import ctypes
import errno
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import time


def process_record(pid):
    try:
        fields = Path(f"/proc/{pid}/stat").read_bytes().rsplit(b") ", 1)[1].split()
        return int(fields[1]), int(fields[19])
    except (OSError, ValueError, IndexError):
        return None


def descendants(owner):
    records = {}
    for entry in Path("/proc").iterdir():
        if entry.name.isdecimal():
            record = process_record(int(entry.name))
            if record is not None:
                records[int(entry.name)] = record
    owned = {owner}
    while True:
        more = {pid for pid, (parent, _) in records.items() if parent in owned}
        expanded = owned | more
        if expanded == owned:
            return {pid: records[pid] for pid in owned if pid != owner}
        owned = expanded


def signal_descendants(owner, selected_signal):
    for pid, record in descendants(owner).items():
        try:
            descriptor = os.pidfd_open(pid)
        except ProcessLookupError:
            continue
        try:
            # A pidfd pins the signal target even if it exits after this read.
            # The second observation rejects recycling between enumeration
            # and pidfd_open; no process-group or session membership is assumed.
            if process_record(pid) == record:
                signal.pidfd_send_signal(descriptor, selected_signal)
        except ProcessLookupError:
            pass
        finally:
            os.close(descriptor)


def exit_status(wait_status):
    result = os.waitstatus_to_exitcode(wait_status)
    return result if result >= 0 else 128 - result


def wait_for_acknowledgement(job, parent, result, stop, *, clock=time.monotonic, wait=select.select):
    """Retain the reaped owner until ack, but bound only its canceled wait."""
    acknowledgement = os.open(job / "ack", os.O_RDWR | os.O_NONBLOCK)
    try:
        received = b""
        cancel_deadline = None
        while b"\n" not in received:
            if os.getppid() != parent:
                return result or 143
            timeout = 1
            if stop[0]:
                now = clock()
                if cancel_deadline is None:
                    cancel_deadline = now + 10
                remaining = cancel_deadline - now
                if remaining <= 0:
                    print("release gate: canceled owner acknowledgement deadline expired", file=sys.stderr, flush=True)
                    return 125
                timeout = min(timeout, remaining)
            readable, _, _ = wait([acknowledgement], [], [], timeout)
            if readable:
                received += os.read(acknowledgement, 64)
                if len(received) > 64:
                    return 125
        return (stop[0] or result) if received == b"joined\n" else 125
    finally:
        os.close(acknowledgement)


def own_phase(root, index, function):
    if not hasattr(os, "pidfd_open") or not hasattr(signal, "pidfd_send_signal"):
        raise RuntimeError("release phases require Linux pidfd support")
    probe = os.pidfd_open(os.getpid())
    try:
        signal.pidfd_send_signal(probe, 0)
    finally:
        os.close(probe)
    parent = os.getppid()
    stop = [0]

    def request_stop(number, _frame):
        if stop[0] == 0:
            stop[0] = 128 + number

    signal.signal(signal.SIGTERM, request_stop)
    signal.signal(signal.SIGINT, request_stop)
    libc = ctypes.CDLL(None, use_errno=True)
    # Adopt orphaned grandchildren, including setsid/Setpgid descendants. The
    # parent-death signal also requests cleanup if the foreground gate exits.
    for option, value in ((36, 1), (1, signal.SIGTERM)):
        if libc.prctl(option, value, 0, 0, 0) != 0:
            raise OSError(ctypes.get_errno(), "release child ownership prctl failed")
    if os.getppid() != parent:
        stop[0] = 143

    job = root / f"job-{index}"
    environment = os.environ.copy()
    environment["TMPDIR"] = str(job / "tmp")
    # Root directories provide privacy. Preserve ordinary tool output modes;
    # inheriting umask 077 breaks explicit portable-mode fixture contracts.
    os.umask(0o022)
    worker = subprocess.Popen(
        ["bash", "-c", 'set -euo pipefail; export RELEASE_GATE_WORKER_PID=$$; "$1"', "release-phase", function],
        env=environment,
    )
    (job / "ready").write_text("ready\n")
    result = None
    adopted_count = 0
    cleanup_started = None
    while True:
        no_children = False
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                no_children = True
                break
            if pid == 0:
                break
            if pid == worker.pid:
                result = exit_status(status)
                worker.returncode = os.waitstatus_to_exitcode(status)
            else:
                adopted_count += 1
                print(f"release gate: reaped adopted descendant pid={pid} exit={exit_status(status)}", file=sys.stderr, flush=True)
        if no_children:
            break
        if stop[0] or result is not None:
            if cleanup_started is None:
                cleanup_started = time.monotonic()
                print(f"release gate: joining descendants after worker={worker.pid} exit={result} stop={stop[0]}", file=sys.stderr, flush=True)
                # A successful phase that abandoned descendants did not join
                # its work. Clean it up, but never bless that phase as passing.
                if result == 0:
                    result = 125
            elapsed = time.monotonic() - cleanup_started
            signal_descendants(os.getpid(), signal.SIGTERM if elapsed < 2 else signal.SIGKILL)
            if elapsed >= 10:
                # No completion record means the foreground owner must retain
                # services and report failed/unproven cleanup, not success.
                raise RuntimeError("release descendants did not all reap")
        time.sleep(0.01)

    result = stop[0] or result
    if result == 0 and adopted_count:
        print(f"release gate: successful worker abandoned {adopted_count} reaped descendants", file=sys.stderr, flush=True)
        result = 125
    if result is None:
        raise RuntimeError("release worker exited without a wait status")
    # ECHILD above is the publication boundary: not one owned descendant can
    # still mutate a service, regardless of its process group or session.
    try:
        completion = os.open(root / "completions", os.O_WRONLY | os.O_NONBLOCK)
    except OSError as error:
        if error.errno == errno.ENXIO and os.getppid() != parent:
            return result
        raise
    try:
        os.write(completion, f"{index} {result}\n".encode())
    finally:
        os.close(completion)
    # Retain the leader identity until the foreground owner acknowledges it.
    # A malformed acknowledgement has a real failing exit, not a passing record.
    return wait_for_acknowledgement(job, parent, result, stop)


if __name__ == "__main__":
    if len(sys.argv) != 4 or not sys.argv[2].isdecimal():
        raise SystemExit("usage: release-gate-child.py ROOT INDEX EXPORTED_FUNCTION")
    raise SystemExit(own_phase(Path(sys.argv[1]), sys.argv[2], sys.argv[3]))
