# Owned root submission qualification — 2026-09-27

The [owned root submitter](../ROOT-SUBMISSION.md) implements actual native HTTP
submission under a separate signed route/action approval. It pins the original
signed extrinsic, syncs each numbered attempt before transport, never retries
that number, and durably reconciles uncertain sends through the canonical reader.
The full service and real offline public-signature adapter compose with this
transport in local deterministic fixtures through finalized native receipt.

Worktree: `/mnt/data/sn-testnet/worktrees/mg08-root-submission-20260927/sn`.
Branch: `codex/mg08-root-submission-20260927`.
Base: `32b6a19c91e5275bc44a6407ecedf1a8b58e1e16`.
Go 1.26.6 linux/amd64; sibling connect clean at
`358cefaef9b058cdd06ba5e9c4feeadef64ae1fb`.

Commands ran from that worktree, with package execution in its `mainnet/`
directory. Capture wrappers copy the actual generated test binary and build
metadata, then exec it unchanged. They do not modify selection or test behavior.

| Exact command | Result |
| --- | --- |
| `go test -json -exec /mnt/data/sn-testnet/evidence/mainnet-root-submission-20260927/capture-normal.sh ./mainnet -count=1 -timeout=15m` | 265 roots + 41 subtests passed; 217.278s package time |
| `go test -race -json -exec /mnt/data/sn-testnet/evidence/mainnet-root-submission-20260927/capture-race.sh ./mainnet -run '^TestRootSubmission' -count=1 -timeout=15m` | All 12 affected roots passed; 292.734s package time |
| `go vet ./mainnet` | Passed |
| Source hashes and staged/unstaged `git diff --check` | Passed |

The 12 affected roots use no subtests. Earlier focused runs passed 9 roots in
24.178s, 11 in 35.103s and all 12 in 37.597s. Final normal/race runs use the
same frozen Go source; later edits are documentation only.
No test failure, skipped event, timeout or race report occurred.

Retained [raw evidence and exact command outcomes](/mnt/data/sn-testnet/evidence/mainnet-root-submission-20260927/RESULT.md)
include JSON events, complete selected and passed membership, actual test
binaries/build identities, source patch, hashes and verification. No server,
NetEscrow, monitor or alerts implementation changed. Adjacent action and service
files have documentation-comment updates only; their behavior, native crypto and
canonical receipt implementation are reused unchanged.

| Artifact | SHA-256 |
| --- | --- |
| Frozen Go patch | `b5ef306a9e673289a24c6f934d57f0d14d1dae359efefc50a6698c058fc22737` |
| All mainnet Go files plus module manifests | `06a0dadd04131558aab915bc1afdce253ce816043c9df29eb850535f8faf24e2` |
| Actual normal binary | `54433683591aee9d1a292fe3fb3a7672b59b4882321fed5ba64f637f17a02b46` |
| Actual race binary | `bafc3771b3cd8ed52834a2b366488d2ccdf8ff3e79c5f32c52d14ecac22d0e69` |

Coverage forces exact approval and request substitution, initial durable intent,
lost response, overload/redirect/pool errors, malformed/duplicate/case-folded
acknowledgements, original signature replacement, skipped and exhausted attempt
numbers, absent/revoked authority, wrong chain/runtime/generation/nonce, final
network recheck, canceled authority/request/waiters, concurrent calls, both sides
of ambiguous journal commits, restart and immutable old receipt recovery,
canonical rollback/fork, unsafe/missing/foreign files and full service composition.
All network requests terminate at local test fixtures with synthetic keys and
chain identities. No live native signer, key, chain send, activation, deployment,
merge or push was used.

The implementation reduces the missing production transport dependency. It does
not supply live approval or prove owned-node configuration, current effective
eligibility, global hotkey/nonce fencing, native device custody, revocation,
hostile-host rollback resistance, inclusion-time generation/runtime protection
or enforceable payment exposure. Plain HTTP depends on the approved owned
network. Canonical readback still trusts the approved node for consensus and
storage. No CLI or activation path is wired, and the ordinary read-only RPC
whitelist remains unchanged. These limits are explicit in
[ROOT-SUBMISSION.md](../ROOT-SUBMISSION.md).
