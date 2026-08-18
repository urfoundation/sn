"""Fast miner-side mirror of the server's structural unified-diff screen.

The evaluator repeats every check. Passing this module is never authority to
run a patch; it only gives a miner a useful local error before submission.
"""

from __future__ import annotations

import fnmatch
import hashlib
import pathlib
import re
import sys
from dataclasses import dataclass


HARD_FORBIDDEN = (
    ".git/**", ".github/**", "go.mod", "go.sum", "**/go.mod", "**/go.sum",
    "vendor/**", "db_migrations.go", "db_migrations_*.go",
    "api/**", "cli/**", "competition/**", "config/**", "vault/**", "site/**",
    "connect/sim-latency/**", "stats/**",
)
HUNK_HEADER = re.compile(r"^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?: .*)?$")
INDEX_LINE = re.compile(r"^index [0-9a-f]{7,64}\.\.[0-9a-f]{7,64}(?: 100644)?$")


@dataclass(frozen=True)
class ScreenedPatch:
    canonical_bytes: bytes
    sha256: str
    paths: tuple[str, ...]


def screen_patch(
    patch: str,
    *,
    max_patch_bytes: int,
    allowed_paths: tuple[str, ...] | list[str],
    forbidden_paths: tuple[str, ...] | list[str],
) -> ScreenedPatch:
    raw = patch.encode("utf-8", errors="strict")
    if not raw or len(raw) > max_patch_bytes:
        raise ValueError("patch is empty or exceeds max_patch_bytes")
    if "\x00" in patch or "\r" in patch or not patch.endswith("\n") or patch.endswith("\n\n"):
        raise ValueError("patch must be canonical LF-terminated UTF-8 text")
    lines = patch[:-1].split("\n")
    if not lines or not lines[0].startswith("diff --git "):
        raise ValueError("patch must start with a diff --git header")
    paths: list[str] = []
    seen: set[str] = set()
    current = ""
    old_header = new_header = hunk = False
    old_remaining = new_remaining = 0
    last_hunk_content = False

    def finish_section() -> None:
        if current and not (old_header and new_header and hunk):
            raise ValueError(f"path {current!r} is missing canonical file headers or a hunk")
        if hunk and (old_remaining or new_remaining):
            raise ValueError(f"path {current!r} has mismatched hunk line counts")

    for number, line in enumerate(lines, 1):
        if line.startswith("diff --git "):
            finish_section()
            fields = line.split()
            if len(fields) != 4 or not fields[2].startswith("a/") or not fields[3].startswith("b/"):
                raise ValueError(f"line {number}: malformed diff header")
            source, target = fields[2][2:], fields[3][2:]
            if source != target or not _safe_path(source):
                raise ValueError(f"line {number}: unsafe or rename-like path")
            if source in seen:
                raise ValueError(f"path {source!r} appears more than once")
            if _matches(HARD_FORBIDDEN + tuple(forbidden_paths), source) or not _matches(tuple(allowed_paths), source):
                raise ValueError(f"path {source!r} is outside the editable surface")
            seen.add(source)
            paths.append(source)
            current = source
            old_header = new_header = hunk = False
            old_remaining = new_remaining = 0
            last_hunk_content = False
        elif line.startswith("diff "):
            raise ValueError("only diff --git file headers are accepted")
        elif not hunk and line.startswith("--- "):
            if not current or old_header or line != f"--- a/{current}":
                raise ValueError(f"line {number}: old-file header does not match diff path")
            old_header = True
        elif not hunk and line.startswith("+++ "):
            if not current or not old_header or new_header or line != f"+++ b/{current}":
                raise ValueError(f"line {number}: new-file header does not match diff path")
            new_header = True
        elif line.startswith("@@ "):
            if not current or not old_header or not new_header:
                raise ValueError(f"line {number}: hunk appears before canonical file headers")
            if hunk and (old_remaining or new_remaining):
                raise ValueError(f"line {number}: previous hunk has mismatched line counts")
            match = HUNK_HEADER.fullmatch(line)
            if match is None:
                raise ValueError(f"line {number}: malformed hunk header")
            old_remaining = int(match.group(2) or "1")
            new_remaining = int(match.group(4) or "1")
            if old_remaining > sys.maxsize or new_remaining > sys.maxsize:
                raise ValueError(f"line {number}: hunk count exceeds platform bounds")
            hunk = True
            last_hunk_content = False
        elif line.startswith("@@@"):
            raise ValueError("combined diffs are not accepted")
        elif not hunk and line.startswith("index "):
            if not current or INDEX_LINE.fullmatch(line) is None:
                raise ValueError("only regular 100644 file indexes are accepted")
        elif not hunk and (line == "GIT binary patch" or line.startswith("Binary files ")):
            raise ValueError("binary patches are not accepted")
        elif not hunk and line.startswith((
            "new file mode ", "deleted file mode ", "old mode ", "new mode ",
            "rename from ", "rename to ", "copy from ", "copy to ",
            "similarity index ", "dissimilarity index ",
        )):
            raise ValueError("new/deleted files, renames, copies, and mode changes are not accepted")
        elif "subproject commit" in line.lower():
            raise ValueError("submodule changes are not accepted")
        elif line[:1] in {"+", "-"} and line[1:].strip().startswith(("//go:build", "// +build")):
            raise ValueError("build constraint changes are not accepted")
        elif hunk:
            if line.startswith(" "):
                old_remaining -= 1
                new_remaining -= 1
                last_hunk_content = True
            elif line.startswith("+"):
                new_remaining -= 1
                last_hunk_content = True
            elif line.startswith("-"):
                old_remaining -= 1
                last_hunk_content = True
            elif line == r"\ No newline at end of file" and last_hunk_content:
                last_hunk_content = False
            else:
                raise ValueError(f"line {number}: invalid unified-diff hunk line")
            if old_remaining < 0 or new_remaining < 0:
                raise ValueError(f"line {number}: hunk body exceeds declared line counts")
        else:
            raise ValueError(f"line {number}: unexpected file metadata")
    finish_section()
    if not paths:
        raise ValueError("patch contains no diff --git header")
    if paths != sorted(paths):
        raise ValueError("file sections must be sorted by path")
    return ScreenedPatch(raw, hashlib.sha256(raw).hexdigest(), tuple(paths))


def screen_file(path: str | pathlib.Path, **policy: object) -> ScreenedPatch:
    return screen_patch(pathlib.Path(path).read_text(encoding="utf-8"), **policy)


def _safe_path(value: str) -> bool:
    pure = pathlib.PurePosixPath(value)
    return (
        bool(value)
        and not value.startswith("/")
        and "\\" not in value
        and "//" not in value
        and str(pure) == value
        and ".." not in pure.parts
        and ".git" not in pure.parts
    )


def _matches(patterns: tuple[str, ...], value: str) -> bool:
    return any(fnmatch.fnmatchcase(value, pattern) for pattern in patterns)
