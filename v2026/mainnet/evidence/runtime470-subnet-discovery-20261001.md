# Runtime 470 SN25 discovery qualification, 2026-10-01

This increment adds a signer-free path to collect the owner/generation and UID
membership evidence needed before an independently approved policy can exist.
It does not produce that policy, classify a miner for removal, authorize trim,
prove independent finality or admit mainnet activation. MG-01 and MG-08 remain
open.

The isolated source started at SN `21c3a794b29e0d8c16ec776e42693dd9d83a0a3c`.
Raw live observations, metadata qualification, exact command logs, source hashes
and dependency records are retained under the restricted directory
`/mnt/data/sn-testnet/mainnet-runtime470-census-20261001/`. No production account,
route configuration, key or signed transaction is used as a committed fixture.

## Current-runtime compatibility and authority boundary

The exact official metadata identified by the [runtime audit](../../docs/spec/runtime-470-audit.md)
is 354,056 bytes with Blake2b-256
`8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
External tests run those complete bytes through the production subnet preview,
ranked owner-trim plan and discovery reader with synthetic state. They pass
normally in **0.751s** and under race in **6.604s** after the batch change.

The committed [protocol projection](../testdata/runtime470-subnet-codec.md)
retains the consumed current storage and call shapes while removing all
documentation and unrelated pallets/constants. Its tests use generated accounts,
a synthetic genesis, fake code bytes and local HTTP responses. The existing
`subtensor-subnet-census-67dcf7f-v1` wire profile accepts the reviewed v470 source
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`; its historical name is not a runtime
version check or source-build proof. Independent acceptance of the official
artifact and its documented non-reproducible seed constants remains required.

`subnet-discover --rpc URL --snapshot FILE` accepts either existing unapproved
snapshot format. It validates the envelope, artifact hashes and complete v470
tuple before RPC, then authenticates the retained native header and rechecks
network/runtime identity. The separate output includes raw storage, exact
SN25/root membership, observed owner/generation, capacities, activity, permits
and aggregate subnet alpha emissions. Every seat is `unclassified`;
`runtime_source_proven`, `reset_ready` and `apply_authority` remain false.
`membership_complete` does not cover custody, locks, claims, roles or history.
The public preview/trim parser rejects discovery output as a policy before RPC.

## Public archive throughput evidence

All live reads use the same retained combined snapshot at native block
**9,185,377**, whose input file SHA256 is
`72253d053d9293d7fe3d6f411a7b80d9357d1d7eda1682f58d66490279e5b3d1`.
No live signing, broadcasting or approval occurred.

The first per-key census exhausted its 15-minute context with HTTP 429 and
emitted zero JSON. The archive returned `retry_after_seconds:60`; the original
transport retried each worker at no more than five seconds. The retry correction
honors integer/date `Retry-After` and JSON seconds hints within the existing
deadline, with immediate cancellation and no additional request after refusal.

A second per-key attempt with that correction still exited **1** after
**900.781s** with HTTP 429/deadline causes and zero JSON. These are throughput
failures, not absent-storage observations. Both exact stderr files are retained.
Retry hints alone do not establish operational readiness.

A four-key, same-block `state_queryStorageAt` probe succeeded in **0.592s**:
SN25 membership/capacity was **256/256**, and root was **64/64**. The per-key
reader therefore needed 1,600 registration-field calls plus other reads, more
than 109 successful requests per minute to finish within 15 minutes.

Discovery now fetches only enumerated exact keys in sequential batches of at
most **128**, reducing those 1,600 calls to **13** for this observed membership.
Each batch requires exactly one change set at the retained hash and exactly one
value for each requested key; missing, duplicate, extra, foreign, stale or
malformed changes fail closed. Explicit null retains its metadata-default or
optional-absence meaning. Prepared values are consumed once through the normal
SCALE and repeated-value checks; subsequent overlapping reads cannot reuse stale
prepared values. No new concurrency or general RPC method admission is added.
Approved-policy preview/trim readers continue to use their original per-key path.

The final unmodified batch command exited **0** in **13.231s**, from
05:56:53.278 to 05:57:06.510 UTC. It retained **256 SN25 registrations**, **64
excluded root registrations** and **1,609 distinct storage values**, with every
seat unclassified and all four approval/custody blockers intact. The output is
an `unapproved_observation` under `rpc-assertion`; it has
`membership_complete=true`, `runtime_source_proven=false`, `reset_ready=false`
and `apply_authority=false`. This qualifies discovery at this retained block,
not future public-RPC throughput or a current execution census.

Raw output `archive-discovery-batch.json` has SHA256
`e1b8c5dddc91a32d8f740ba91589ad0055fa9c3c294c7922cf18b045afb3f6c3`
and independently recomputed domain-separated content hash
`sha256:6a95ceb0616842ebe9a7fc9a237957194753b2ccea234a27061c32fabe8795ac`.
The command/binary receipt and a redacted counts/authority summary are retained
beside it. Owner and hotkey identifiers remain only in restricted evidence.
The original and retry-only stderr SHA256 values are respectively
`e861ec0726c76e878be6f1df25fa85993e21f6ad585d41a3af401f57ee1b1888`
and `fe7bfa5c3835d99188fbdab26ebadbd50404e479857e6eaf01d37c8664ed83a2`.

## Deterministic qualification

The expanded pre-batch selection passed **117/117** roots normally (**110.715s**).
Disjoint complete race packages covered the same 117 roots: owner guard **13**
(**245.299s**), authority/window **18** (**221.735s**), and remaining
census/preview/runtime/RPC **86** (**482.796s**), with no failed or skipped roots.
An earlier monolithic race command timed out after **102/107** root passes at
**900.229s**; it remains retained as failed/incomplete, not a package pass.

After batching, the affected read/preview/root-storage selection passes **69**
roots normally (**40.256s**) and under race (**311.533s**), with no failed or
skipped roots. Tests cover the 128-key boundary, identical sorted evidence
versus per-key reads, wrong/missing/duplicate/foreign/partial responses,
null/default distinction, one-use staging, malformed SCALE, closing drift,
cancellation, policy-format separation and unchanged approved preview transport.

Six controlled regressions each fail at the intended test in both normal and
race modes: removed public route, disabled artifact-byte binding, ignored retry
hints, disabled batch block matching, disabled batching, and disabled late
cancellation refusal. All twelve are assertion failures, not compile failures.
The final `go vet ./mainnet`, formatting and whitespace checks pass and are
recorded with the source/evidence manifest. No full-repository test pass is
claimed.

The **103-file** external `SHA256SUMS` manifest verifies and has SHA256
`f34eaa8e1393e19757a5022bcf70a1d7719febe6c8c8ec57a8463e827d10e475`.
The final production/test source receipt `qualification-source.json` has SHA256
`a1eb4067028e9a86cd20acd49bbe29301dd4a3606e65297e9e26c5ed54e35800`.
It records the exact base, source/fixture files, Go version and sibling module
heads. The external modfile changes only relative sibling replacements to
absolute paths; the repository's immutable Connect/SDK pins remain in force.

Independent genesis/runtime/source approval, exact owner/generation and
protected/removal policy, custody/stake/lock/claim/history review, current
execution-time trim authority and actual activation remain separate gates.
