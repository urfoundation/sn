# Bounded owner-trim qualification — 2026-09-27

The separate signer-free [`owner-trim-qualify`](../OWNER-TRIM-BOUNDED.md)
checks a proposed mortal window against authenticated census, runtime metadata
and exact-block storage. It does not require an atomic hotkey predicate when
the conditional safe-set invariant holds. It retains every requested original
generation and explicitly uncertain residual identities, with no execution or
full-reset authority.

Qualified in `/home/by/urnetwork/sn-mg08-owner-trim-guard-20260927`, based on
`cfa796ab9cecf99de2d84dd8b148018fa56906e1`, with Go 1.26.6 linux/amd64.
Retained evidence:
`/mnt/data/sn-testnet/evidence/mainnet-owner-trim-bounded-20260927/RESULT.md`.
All bundle checksums verified. `SHA256SUMS` SHA256:
`44070ff1b3549b30dbeaf78574faf06e9f0327aad7d9e0f3d725719e96a64a65`.

Final source, bound by `qualified-source.sha256` and `source.patch`:

| Check | Result |
| --- | --- |
| `go test ./mainnet -json -count=1 -timeout=15m` | 222 roots and 41 subtests passed; 155.639s |
| `go test -race ./mainnet -json -run '^Test(OwnerTrimBounded\|ObservationStorage)' -count=1 -timeout=10m` | 14 roots passed; 105.417s |
| `go vet ./mainnet` | Passed |
| Source checksum verification and `git diff --check` | Passed |

An earlier run, retained against `prior-source.sha256`, passed 222 normal
roots plus 41 subtests (145.508s) and 27 affected race roots plus 41 subtests
(387.949s), using
`go test -race ./mainnet -json -run '^Test(OwnerTrimBounded|OwnerTrimGuard|ObservationStorage)' -count=1 -timeout=15m`.
The final follow-up only added the explicit public subnet-pruning/reuse
assumption and its assertion. The 13 unchanged existing guard tests reuse
that broader race qualification; all changed bounded tests and the full normal
package were rerun. This is not one final-source 27-root race invocation.
No qualification run timed out or reported a data race.

Deterministic tests cover immunity at exclusive expiry, approved-miner immunity
expiry, emission reorder, protected roles and custody, registration/newcomers,
first and subsequent hotkey swaps, coldkey announcements, epoch scheduling
including the drift fallback, admin windows, leases, strict capacity ratios,
stale/forked heads, contradictory same-hash timing, metadata drift,
interruption, every original generation retained and forbidden execution inputs.

Adjacent profile review found that unknown hasher names bypassed the former
two-hasher condition. `hasher-causal-red.log` records the deterministic failure
of `TestObservationStorageProfileRejectsUnknownHasher`; the explicit
identity/blake128concat/twox64concat allowlist passes in the green log and
final normal/race suites. The new fixed-width coldkey announcement and u32
decoders reject truncated or trailing data.

Reviewed Rust files are retained and hashed from source pin
`67dcf7f791dc495064c293f080a0702cb433e51e`. The no-epoch predicate excludes
conviction owner takeover and permit updates; a lease is unsupported. Public
subnet registration can separately prune/dissolve a subnet at capacity and
eventually reuse its netuid, so protection against that path is explicitly
unproved rather than mislabeled as privileged-only churn.

Remaining gates are approved live policy/runtime/census evidence,
owner/proxy/pending-action fences, governance/runtime stability, public subnet
pruning/reuse protection, source-to-Wasm provenance, full custody/effect audit,
actual signed mortality and nonce/fee recovery, exact receipt attribution and
actual-subset reconciliation. No live chain call, signing, submission, merge
or push occurred. Exit 0 remains conditional evidence, never an execution token.
