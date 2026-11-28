Go tests that use `os.Executable()` as a protected child must run from an
explicitly retained test image. A normal `go test` invocation can select a
hardlinked cache executable or unsuitable mode even when the source and module
graph are correct. Preserve that original failure and its stat observation;
do not relax the production executable guard or call it a product failure.

Compile the exact test source once (`go test -c`, with the selected direct
`-modfile`/`-overlay` flags if applicable), bind the build log and ELF SHA256 to
the source/module receipt, then invoke `go_test_image.py` with a reviewed JSON
recipe and a new owned output directory. The recipe contains:

```
{
  "schema": "urnetwork-go-test-image-v1",
  "source_image": {"path": "/absolute/mainnet.test", "sha256": "64_lowercase_hex"},
  "expected_uid": 1000,
  "cwd": "/absolute/frozen/sn/mainnet",
  "arguments": ["-test.run=^ExactSelectedRoot$", "-test.count=1", "-test.v=test2json", "-test.timeout=10m"],
  "timeout_seconds": 660,
  "minimum_free_bytes": 118111600640,
  "forecast": {"retained_image_bytes": 536870912, "log_bytes": 8388608},
  "environment": {"TMPDIR": "/absolute/private/tmp"}
}
```

The runner captures the original link count, UID, mode and ancestor metadata,
streams exact pinned bytes into one fresh UID-owned mode0500/nlink1 ELF, and
checks its inode/content before and after execution. The shared qualified
process guard bounds output, joins descendants and cancels on owner timeout
or floor refusal. The reviewed image/log forecast needs twice its future
increment above the shared floor. An output directory is never reused.

Parent `GOFLAGS` are removed and `GOWORK=off` is explicit so a fixture's nested
Go command can inspect its own synthetic module instead of inheriting the
parent's overlay/modfile. Other required fixture environment overrides are
explicit in the recipe. This does not change the already-compiled parent
image's graph.

The receipt's `SOURCE_PINNED_EXECUTION_ONLY` status is not a test pass or a
source qualification. Independently verify selected Go test events, exit,
skip/failure distinctions, compilation/source/module joins and expected causal
assertions. Retain every original automatic-executable setup refusal rather
than relabeling a later corrected invocation as the original run. The helper's
own six tests exercise actual files and a small ELF; they do not qualify the
application or require rerunning already-passed application scopes.

For a compiler invocation owned by the qualification pipeline, a single fresh
retained ELF can also be the execution image. Reserve a unique private output
path before compiling, record the exact compiler/source/module inputs, and
never compile over that path again. After successful compilation, require a
regular UID-owned file with one link, protect it with mode0500, sync and hash
it, and retain its named device/inode/size/mode/link-count/timestamps together
with the SHA256. Verify that complete identity and content immediately before
and after execution. This avoids retaining a second identical compiler ELF;
it does not remove custody, source attribution or process supervision.

This direct-output method is limited to fresh compiler output controlled by
the same pipeline. Existing external images, cache executables, hardlinked
files, foreign-owned files and unreviewed ancestors still use the qualified
copy-and-verify path. The current `go_test_image.py` entrypoint always stages a
copy; it does not expose a direct-output option. A pipeline using the direct
method must independently retain the same finite process/log/floor guards,
explicit package cwd and child environment, cancellation/descendant joins,
source/module/compile evidence, exact selected-root census and expected causal
assertions. A chmod or a matching SHA256 alone is insufficient. Existing
receipts and original images remain immutable; removing a historical duplicate
requires a separate reference/process audit and an exact retained-byte binding.

For JSON conversion, a new recipe can include `go_tool` with the exact canonical
Go executable `path` and `sha256`. Declare `PATH`, `GOENV=off` and
`GOTOOLCHAIN=local` in `environment`. The shared child context resolves
`test2json` using that Go executable's `tool -n test2json` under the actual test
cwd and complete child environment. A modern Go release may return an
executable in GOCACHE. Do not construct a path beneath GOROOT. Both probes must
join successfully before any test body, and consume the same reviewed log
budget. The body directly invokes the pinned tool returned by Go.

Launch the Python runner with its reviewed absolute interpreter path. Include
`runner_python: {"path": "/absolute/actual/python", "sha256": "64_lowercase_hex"}`
to refuse a different actual interpreter before probing or running a body.
Receipts bind actual imported helper paths and hashes, interpreter/tool bytes,
cwd inode and a hash of the complete child environment. The frozen environment
is used for probes and the body; arbitrary ambient secret values are not
published. Tool and directory replacement refuses execution even if replacement
tool bytes match. These checks retain the existing bounded descendant-joining
process guard and do not qualify selected application tests by themselves.

Custom supervised runners can import `ChildContext` from `child_context.py`,
construct it once with the complete final child environment, bind reviewed
executables, and use `prepare_go` followed by `run`. Pass the actual bounded
`process_guard` callback when using a separately frozen guard. Avoid passing
the context through a wrapper that rebuilds the environment from its own
ambient variables. Retain the context/tool receipts alongside source and
resource admission. Preserve old frozen runners and receipts; use a new
runner/output root for a correction.

For a Python child, use `python = context.bind_python(selected_python_path,
expected_real_interpreter_sha256)` and pass `python["path"]` unchanged as
`argv[0]`. In particular, do not resolve a virtualenv's `bin/python3` before
launching: Python discovers `pyvenv.cfg` relative to the invoked path, and the
resolved base executable can have different installed modules. The explicit
Python binding retains that invocation path, its finite final-component
symlink chain, the separately hashed real ELF, and both virtualenv configuration
locations (including their absence). It checks them before and after the
child. Ordinary `bind` still requires a canonical executable path; it does not
silently permit arbitrary tool aliases. Script and `-m unittest` arguments use
the same preserved Python path and existing bounded process guard.

Runner receipts now retain `runner_invocation` alongside `runner_executable`.
Existing `runner_python` pins continue to authenticate the real executable;
the additional record preserves the actual virtualenv invocation/configuration.
This is interpreter context, not a complete Python dependency inventory: the
caller still binds required packages and source inputs. Preserve original
missing-module loader failures as failed outcomes, with separate labels for
corrected executions.

Each `ChildContext.run` now durably records the exact flat process-guard return
in `LABEL.process-result.json` immediately after Wait/join, before postchecks or
caller classification. A separate `LABEL.process-context.json` is written only
after logs are synced and the child context and tool custody are reverified.
The returned `process_result` path/hash can be passed to
`replay_process_result(reference)` after a checker-only failure. Replay verifies
the retained wait receipt and both output hashes and never starts a child. It
does not infer a test pass from output counts; source/image custody, exact root
events, actual exit and join still require their independent checks. A failed
postcheck preserves wait evidence but default replay refuses to claim verified
context. Explicit `require_context_verified=False` reads only that limited
wait observation. Existing result labels cannot be reused for another body.

Use `compiler_census()` for the admission census. It reads actual `/proc` tool
origins and NUL-separated argv, counts both bare and absolute `go test -c`
including Go's global `-C` option, and separately reports Go workers and Rust
compilers. A live unobservable compiler is a refusal, not zero compilers. The
census is sampled evidence; the shared resource lease and fresh capacity checks
remain required.
