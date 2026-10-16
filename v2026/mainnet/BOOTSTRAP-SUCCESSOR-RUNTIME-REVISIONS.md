# Additive successor runtime authority

This increment retains independently signed runtime revisions for one exact
successor execution. [Scoped qualification](evidence/bootstrap-successor-runtime-qualification-20260930.md)
passes ten new and thirty-six adjacent roots normal/race, twelve normal controls
and seven selected light race controls. Public `--submit`
remains unavailable: no production Safe deployment and complete storage-history
authenticator exists, and a runtime revision cannot supply that capability.

A routine runtime upgrade can be admitted after independent artifact review
without replacing the base canonical approval, original eight receipts, signed
Safe and relayer transactions, either nonce claim, counted attempts or maximum
liabilities. This is an incremental signed authority path. It does not implement
automatic compatibility admission; that broader RT-04/P0 remains separate.

## Independent revision

The original independent Ed25519 approver signs canonical JSON for
`bootstrapSuccessorRuntimeAuthorization` in the field order declared in
`bootstrap_successor_runtime_authority.go`, prefixed by
`urnetwork-mainnet-successor-runtime-revision-approval-v1` and one zero byte.
The envelope contains `authorization` and `signature_ed25519`; unknown fields
are rejected. No replacement approver key is accepted.

The authorization binds the exact execution plan hash, immutable base canonical
approval hash, revision sequence, predecessor envelope hash, complete runtime
version/code/metadata profile, separately pinned private review evidence and the
exact additive policy. Sequence one names the base envelope as its predecessor;
later revisions name the immediately preceding revision envelope. Hashes of
objects use SHA-256 over their canonical JSON encoding.

`runtime_source_commit` continues to identify reviewed codec semantics, not a
claim that deployed Wasm came from that commit. Review evidence must independently
cover artifact/build identity and applicability of the existing codec. A new
codec needs a separately reviewed implementation. A changed artifact with the
same complete version identity remains an explicit incompatible-profile gate;
the existing checkpoint reader is not weakened to guess between artifacts.

## Exact import and recovery

On `contract-successor-execution-resume --online`, add both
`--runtime-revision /private/revision.json` and
`--runtime-revision-sha256 sha256:...` to the existing execution and canonical
approval arguments. One envelope is imported before any network operation.
An already retained exact revision is idempotent. The base canonical approval
is still required and never replaced. Local result fields report the complete
revision count and tip, and whether a partial next revision is present.

Each revision is published privately under a fixed ordinal filename, with the
signed envelope hash also in its stage name. Empty and partial stages therefore
reserve exactly one independently signed envelope. Reopening checks all complete
signatures, evidence, canonical bytes and predecessors. Gaps, forks, changed
files and unexpected stages refuse admission. A missing revision referenced by a
counted or terminal event cannot be recreated by supplying a new input.

A partial next revision may be completed only from the same signed envelope.
It blocks new attempts and sends; historical reconciliation can still use
earlier complete authority. An interrupted counted attempt is recovered as
consumed before another runtime revision may be imported. An interrupted terminal
intent must finish under its existing authority before importing a new revision,
so recovery cannot change the pending event's exact bytes.

Old event fields, encoding and seals stay intact. New counted/outcome events
carry an optional revision seal and retain the original canonical seal. References
must resolve to complete retained history and cannot move backward. Import never
increases the attempt allowance, changes a fee, renews a native window or frees
a liability. An upgrade after reservation can consume that attempt without a
send; a later revision does not reclaim it.

## Current and historical reads

Current admission matches the full observed artifact against independently
retained profiles. Historical receipt authentication selects just the inclusion
and parent candidates from the complete history, then the existing CRv4 reader
authenticates both full artifacts and canonical first mapping insertion. Original
eight-action receipt authentication keeps its original profiles unchanged.

The journal is not capped by CRv4's ten-artifact per-call allowlist: each read
passes at most two profiles. The existing complete filesystem census remains
bounded at 4096 entries and each revision envelope at 16 KiB. Custody is neither
unlimited nor claimed to withstand rollback of the entire local directory.

All production route, finalized/pending admission, exact-hash lookup, Safe
provenance policy, signer cutover and one-write rules remain in force. Revision
files, ordinary getters and local fixture evidence cannot authorize public sends.
