# Successor execution: live custody checkpoints

This source change closes a live-owner durability gap in the evidence-anchor
continuation. It does not install a public Safe submission route or supply live
approval. The original eight-action custody, signed successor authority, inner
and outer transaction bytes, cumulative attempts and lifetime reservations keep
their existing formats and meaning.

## Failure and correction

The owner authenticated execution events on reopen, but its later checkpoints
only rechecked directory identity, nonce claims and authority journals. A lost
or changed execution file could therefore leave a healthy in-memory event while
the corresponding recovery evidence was missing. Deterministic regressions on
base `8436f94695f3481c16990151bccd98b92f41bae9` reproduced both observable failures:

- Deleting the counted attempt intent during the second observation still
  permitted the exact transport write, with cumulative attempts nine.
- Deleting the terminal record at `canonical-result-retained` still returned
  `installation_complete: true`.

The new read-only checkpoint verifies the exact claim and ready marker, walks
both copies of every event backward from the owner's retained seal through the
original adoption, and rejects unexpected execution files or stages. The walk
uses the existing 32-event and 4,096-directory-entry bounds and private-file
reader. A reopened interrupted outcome also retains its exact observed byte
hash until canonical recovery completes. Checkpoint refusal preserves remaining
files and nonce claims, closes the execution owner, and permits neither another
send nor an installation result. Only explicit reopen performs the existing
exact-intent recovery.

This is a check against changed custody during an open owner's lifetime. It
does not provide cross-host fencing, trusted filesystem restore, or protection
against an operator rolling back every copy of custody.

## Deterministic validation

Three new top-level roots exercise the production execution owner and durable
store with synthetic chain observations and test-only signatures:

- `TestBootstrapSuccessorExecutionStopsWhenCountedCustodyChangesBeforeSend`:
  twelve post-reservation faults cover lost attempt/adoption intents and records,
  lost claim/ready markers, altered attempt/adoption bytes, nonprivate or
  hardlinked attempts, and injected next events/stages. Each requires zero writes,
  nine retained attempts, closed ownership, and unchanged remaining evidence and
  nonce claims.
- `TestBootstrapSuccessorExecutionFreezesInterruptedOutcomeDuringOwnership`:
  a full staged terminal intent survives interruption and reopen; truncating it
  at recovery admission must not silently replace that retained witness.
- `TestBootstrapSuccessorExecutionStopsWhenTerminalCustodyChangesBeforeResult`:
  loss of the terminal record after canonical inclusion blocks a successful
  installation report without sending again.

The first two failure shapes above failed on the original source in 2.095s.
Their corrected focused run passed in 11.592s; the interrupted-outcome root passed
in 1.103s. A Go overlay replacing only `checkpointExecutionHistory` with a no-op
caused all three new roots to fail at their intended assertions in normal
(3.134s) and race (13.660s) modes. These are expected causal failures, with no
build failure or data-race substitute. Package vet passes.

The affected scope is the 55 roots selected by:

```sh
anchor_evidence=/home/by/urnetwork/temp/sn-mainnet-evidence-anchor-qualification-20261001
GOMAXPROCS=4 go test -modfile="$anchor_evidence/anchor.go.mod" ./mainnet \
  -run '^TestBootstrapSuccessor(Execution|Canonical|Runtime|SafeCurrent)' \
  -count=1 -timeout=20m -json
for family in Execution Canonical Runtime SafeCurrent; do
  GOMAXPROCS=4 go test -race -modfile="$anchor_evidence/anchor.go.mod" ./mainnet \
    -run "^TestBootstrapSuccessor${family}" -count=1 -timeout=20m -json
done
GOMAXPROCS=4 go vet -modfile="$anchor_evidence/anchor.go.mod" ./mainnet
```

All 55 selected roots pass both normal and race, backed by complete package PASS
streams. The four disjoint race scopes were launched concurrently; the loop above
reproduces the same selectors serially.

| Mode / scope | Passed roots | Package elapsed |
| --- | ---: | ---: |
| Normal / all four families | 55 | 438.573s |
| Race / Execution | 24 | 725.188s |
| Race / Canonical | 10 | 675.364s |
| Race / Runtime | 10 | 489.064s |
| Race / SafeCurrent | 11 | 660.777s |

The earlier monolithic race attempt is retained as incomplete: its first full
canonical fixture passed in 366.500s versus 59.360s normal, so it was stopped
before its 25-minute timer and the same unchanged source was repartitioned.
It ended by deliberate termination at 689.481s; that interrupted stream is not
package-PASS race evidence. The accepted streams contain no failed root or
data-race diagnostic. Formatting and `git diff --check` also pass.
This is author validation, not an independent production qualification or a
claim about unselected mainnet tests.

## Source and retained artifacts

The isolated branch is `fix/mainnet-evidence-anchor-continuation-20261001`, based
on `8436f946`. The Go source and test hashes are:

| File | SHA-256 |
| --- | --- |
| `bootstrap_successor_execution_checkpoint.go` | `d1e3857323f22809f3af0eb659f25da705487f771bfc7f698111b22cc2fa1a71` |
| `bootstrap_successor_execution_checkpoint_test.go` | `f0c0246d5c9ae9e82365c40acc8ad0a9cc96fb9deff019a9072f1fd2c8b9f041` |
| `bootstrap_successor_execution_store.go` | `2e6f5e1b4818af0a5af527276b10627520bb8209d9dd5e81c0a03953257f0355` |

The local artifact directory is
`/home/by/urnetwork/temp/sn-mainnet-evidence-anchor-qualification-20261001`.
It retains pre-fix and corrected logs, complete normal/race JSON streams,
the causal overlay and its failure streams, selected roots, source checksums,
the exact alternate module files and module-resolution capture. `MANIFEST.sha256`
seals these local artifacts. The harness uses
Go 1.26.6 on linux/amd64. Its alternate module file changes only sibling local
replacement paths for the isolated worktree; repository `go.mod`/`go.sum` and
the pinned SDK/Connect/SCTP versions are unchanged. Server is `898dc8f3`, Warp
`7498864c`, proxy `6204ae7d`, userwireguard `85fb1ca4`, glog `892ade4a` and
goidenticons `325750b3`.

## Remaining gates

Public successor `--submit` remains closed. Complete Safe history authentication
is still absent; the distinct current-only policy still needs explicit approval
of its assumptions and a separately qualified public-route installation. Live
identity/runtime/build review, signer cutover, owner/relayer approval and funding,
global custody enforcement, actual canonical anchoring, both UR validators and
the root role remain unresolved. Signing and RPC writes used synthetic local
fixtures only; no mainnet operation or activation was performed.
