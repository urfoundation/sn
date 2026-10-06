"""Offline bridge to the independently pinned Subtensor native Ledger backend.

The Go owner command verifies its portable request, pins this source, and runs
it with Python isolated mode. No SDK client, RPC or submission API is imported.
The SDK's native extension provides RFC78 and the real USB HID implementation.
"""

import hashlib
import importlib.machinery
import importlib.util
import json
import os
import stat
import sys


SCHEMA = "urnetwork-mainnet-owner-ledger-adapter-v1"
SOURCE_COMMIT = "67dcf7f791dc495064c293f080a0702cb433e51e"
MAX_INPUT = 17 * 1024 * 1024
MAX_METADATA = 8 * 1024 * 1024
MAX_MESSAGE = 16 * 1024


def unique_object(pairs):
    """Refuse ambiguous duplicate keys, including in nested objects."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate adapter input field")
        result[key] = value
    return result


def decode_hex(value, maximum):
    """All public wire bytes use bounded canonical lower-case 0x hex."""
    if not isinstance(value, str) or not value.startswith("0x") or len(value) > 2 + 2 * maximum:
        raise ValueError("invalid or oversized adapter hex")
    raw = bytes.fromhex(value[2:])
    if "0x" + raw.hex() != value:
        raise ValueError("noncanonical adapter hex")
    return raw


def load_backend(path, expected_hash):
    """Hash the reviewed native extension before executing its module code.

    Build provenance for this artifact pin is provisioned independently. A
    package version string alone does not authenticate the SDK source commit.
    """
    if not isinstance(path, str) or not os.path.isabs(path) or os.path.normpath(path) != path:
        raise ValueError("native SDK backend path must be absolute and canonical")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o022 or not 0 < info.st_size <= 256 * 1024 * 1024:
            raise ValueError("native SDK backend is not a bounded immutable regular file")
        digest = hashlib.sha256()
        remaining = info.st_size
        while remaining:
            chunk = os.read(descriptor, min(1024 * 1024, remaining))
            if not chunk:
                raise ValueError("native SDK backend changed during read")
            remaining -= len(chunk)
            digest.update(chunk)
        if os.read(descriptor, 1) or "sha256:" + digest.hexdigest() != expected_hash:
            raise ValueError("native SDK backend does not match its independent build pin")
        # Linux can load the exact opened inode, removing the pathname reopen.
        # On macOS the private reviewed bundle is the filesystem trust boundary.
        opened_path = f"/proc/self/fd/{descriptor}" if os.path.isdir("/proc/self/fd") else path
        loader = importlib.machinery.ExtensionFileLoader("bittensor_core", opened_path)
        spec = importlib.util.spec_from_loader("bittensor_core", loader)
        module = importlib.util.module_from_spec(spec)
        loader.exec_module(module)
        after = os.stat(path, follow_symlinks=False)
        if (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) != (
            info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns
        ):
            raise ValueError("native SDK backend identity changed")
        for name in ("metadata_digest", "generate_extrinsic_proof", "LedgerDevice"):
            if not hasattr(module, name):
                raise ValueError("native SDK artifact lacks RFC78 or Ledger support")
        return module
    finally:
        os.close(descriptor)


def persist_response(path, result):
    """Publish one public device response before stdout; never replace a file.

    A partial file or failure after device issuance remains an unresolved
    liability. It cannot authorize a second call to the device.
    """
    if not os.path.isabs(path) or os.path.normpath(path) != path:
        raise ValueError("owner response path must be absolute and canonical")
    parent = os.path.dirname(path)
    info = os.stat(parent, follow_symlinks=False)
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        raise ValueError("owner response directory must be private")
    raw = (json.dumps(result, separators=(",", ":")) + "\n").encode()
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        while raw:
            written = os.write(descriptor, raw)
            if not written:
                raise ValueError("short owner response write")
            raw = raw[written:]
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    directory = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def main():
    """Prepare metadata without HID; sign only the exact prepared payload."""
    raw = sys.stdin.buffer.read(MAX_INPUT + 1)
    if len(raw) > MAX_INPUT:
        raise ValueError("adapter input exceeds bound")
    request = json.loads(raw, object_pairs_hook=unique_object)
    fields = {
        "schema", "mode", "request_hash", "backend_path", "backend_sha256", "metadata_scale",
        "metadata_digest", "spec_name", "spec_version", "owner_account_id", "account", "index",
        "call_scale", "included_in_extrinsic", "included_in_signed_data", "response_path",
        "proof_sha256", "expected_app_version",
    }
    if set(request) != fields or request["schema"] != SCHEMA or request["mode"] not in ("prepare", "sign"):
        raise ValueError("unsupported adapter request")
    for name in ("account", "index"):
        if type(request[name]) is not int or not 0 <= request[name] < 2 ** 31:
            raise ValueError("owner derivation index exceeds the hardened u31 range")
    version = request["expected_app_version"]
    if not isinstance(version, list) or len(version) != 3 or any(type(v) is not int or not 0 <= v <= 65535 for v in version):
        raise ValueError("invalid exact app version")
    if version[0] != 100 or tuple(version) < (100, 0, 5):
        raise ValueError("unsupported generic app version")
    owner = decode_hex(request["owner_account_id"], 32)
    metadata = decode_hex(request["metadata_scale"], MAX_METADATA)
    expected_digest = decode_hex(request["metadata_digest"], 32)
    if len(owner) != 32 or len(expected_digest) != 32 or metadata[:5] != b"meta\x0f":
        raise ValueError("Ledger signing requires AccountId32, digest32 and unwrapped metadata15")
    call = decode_hex(request["call_scale"], MAX_MESSAGE)
    extra = decode_hex(request["included_in_extrinsic"], MAX_MESSAGE)
    implicit = decode_hex(request["included_in_signed_data"], MAX_MESSAGE)
    payload = call + extra + implicit
    if not call or not extra or not implicit or len(payload) > MAX_MESSAGE:
        raise ValueError("invalid exact signature payload seams")
    backend = load_backend(request["backend_path"], request["backend_sha256"])
    chain = (metadata, request["spec_version"], request["spec_name"], 42, 9, "TAO")
    digest = bytes(backend.metadata_digest(*chain))
    if digest != expected_digest:
        raise ValueError("RFC78 digest differs from independently approved native action")
    proof = bytes(backend.generate_extrinsic_proof(call, extra, implicit, *chain))
    if not proof or len(payload) + len(proof) > MAX_MESSAGE:
        raise ValueError("Ledger payload plus proof exceeds 16 KiB")
    proof_hash = "sha256:" + hashlib.sha256(proof).hexdigest()
    result = {
        "schema": SCHEMA, "mode": request["mode"], "request_hash": request["request_hash"],
        "sdk_source_commit": SOURCE_COMMIT, "metadata_digest": "0x" + digest.hex(),
        "proof_sha256": proof_hash, "app_version": [0, 0, 0],
    }
    if request["mode"] == "prepare":
        if request["proof_sha256"]:
            raise ValueError("prepare request must not invent a previous proof")
    else:
        if proof_hash != request["proof_sha256"] or os.path.lexists(request["response_path"]):
            raise ValueError("proof changed or original device response already exists")
        # Device access begins only here, after Go synced its signing intent.
        device = backend.LedgerDevice()
        if tuple(device.app_version()) != tuple(version):
            raise ValueError("installed Ledger app version differs from independent pin")
        public_key, _ = device.address(request["account"], request["index"], 42, True)
        if bytes(public_key) != owner:
            raise ValueError("device-derived AccountId32 is not the existing subnet owner")
        signature = bytes(device.sign(payload, proof, request["account"], request["index"]))
        if len(signature) != 65 or signature[0] != 0:
            raise ValueError("Ledger returned a different signature scheme or width")
        result.update(public_key="0x" + owner.hex(), app_version=version, multisignature="0x" + signature.hex())
        persist_response(request["response_path"], result)
    sys.stdout.write(json.dumps(result, separators=(",", ":")) + "\n")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        sys.stderr.write(f"owner Ledger adapter refused: {error}\n")
        sys.exit(1)
