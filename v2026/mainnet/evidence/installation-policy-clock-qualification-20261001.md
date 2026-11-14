# Installation current policy clock qualification

The installation readback now requires the current coordinator's initial
`policyByIndex(0).effectiveBlock` to equal the EVM block from the original,
reauthenticated proxy CREATE receipt. Previously the current executable/domain
reader accepted any nonzero block at or before the selected EVM snapshot, even
though the complete initial proof had fixed that clock at the anchor. A rewritten
clock could therefore survive restoration of the reviewed implementation.
The reviewed [coordinator](../../evm/src/STCoordinator.sol) sets epoch zero to
its actual initialization block; `schedulePolicy` appends future policies and
does not rewrite the first entry.

The new installation-specific contract reader rejects missing, zero, wrong-address
or future proxy inclusion before its contract RPC reads. Its exact clock comparison
preserves the valid observation bytes, original receipt and journal seals,
Safe nonce, counted attempts and liabilities. The evidence-only contract reader
retains its narrower scope; it does not acquire installation authority. Ordinary
later accounting remains permitted by the existing current contract projection.

## Frozen source and graph

Code/test commit `5078046516951e116f953ae602fd2750f0919623`, tree
`5445c7654e02e6d8c631ee908f37b4ef2a37e292`, is based on SN `4164b197`.
It changes four Go files, including one new test file. Documentation is separate
from this frozen code. The immutable supplement uses a separate external modfile
and a dedicated read-only physical worktree for server
`94229abb02819cea97f972421972b4e79c1a32ab`, tree
`61a5f30830c44b99f14583a1c5e8ad88d6a70908`, with the existing versioned Connect/SCTP
`e1b5d77b5029` and SDK `5d37be3876e5` pins. Both consumed module graphs and all local
replacement identities are retained in the source receipt. The exact supplemental
test executables are retained. Tests use Go 1.26.6 on linux/amd64, read-only module resolution, bounded parallelism and
scratch/cache directories under `/mnt/data`.

## Evidence and limits

The new focused root executes the reviewed local EVM contracts and reads their
actual original journals. It checks exact positive projection equality,
refusal of invalid proxy custody before contract RPC, altered current getter
replies, and actual packed policy storage rewritten at a later block. The evidence-only
reader demonstrates the prior ambiguity, while installation rejects the same
state. The public-v2 installation regression also changes actual policy storage
after its exact anchor, keeping original historical native proofs and receipt
custody intact; fresh public readback must fail without another send.

| Execution | Normal | Race | Provenance scope |
| --- | --- | --- | --- |
| Original selected suite | 14/14 pass; 292.882 s | 14/14 pass; 1761.410 s | Execution verdicts retained; final shared-server checkout fence failed as detailed below. |
| Immutable focused supplement | 2/2 pass; 95.487 s | 2/2 pass; 609.535 s | Dedicated server `94229abb`; matching source/module fences and retained linked executables. |
| Exact-comparison omission | Intended assertion failure | Intended assertion failure | Frozen tests; only the exact policy-block comparison removed. |
| Installation-caller omission | Intended assertion failure | Intended assertion failure | Frozen tests; only the installation call restored to the weaker observer. |

Both vet executions exited zero. All positive executions finished with package
PASS and no skipped roots or race reports. The two supplemental roots repeat a
subset of the fourteen; their counts are not additive. Both omission controls
failed at the intended assertion in normal and race modes, with no skipped
roots or race reports. A pre-freeze diagnostic is retained but excluded from
these counts. This table describes selected-root author qualification, not
full-package coverage. Independent qualification is recorded below.

The raw evidence directory is
`/mnt/data/sn-testnet/mainnet-installation-policy-clock-astra-20261001`.
It retains exact commands, environment, stdout/stderr, terminal results,
selected-root lists, both module graphs, source fences, controls and binary
metadata. `receipt.json` SHA-256 is
`7566511d4e2845ced978d3adcf32bbe775222037ec650d9d49a796b73aabf7e3`;
the `SHA256SUMS` manifest covers 83 files, all verified, with SHA-256
`bdfbb1a1a179a1d367cd209b1755501eee80ee3cce9601bd378d81c3e9d7cecd`.
The supplemental race executable SHA-256 is
`1f5699fb816b68b85bf8fafd974fa83fc9085fb44280c9f61e896b44e373a6e1`;
it exactly matches the executable copied from the running process at 14:52:04
UTC. The immutable supplement ended at 14:58:25 UTC on October 1, 2026.

## Original checkout movement and immutable supplement

The original selected fourteen roots passed normally and under race detection,
and vet passed. The final source fence then detected that the shared physical
server checkout had advanced from `94229abb` to `24ac67d4` at 14:29:30 UTC.
The normal suite and all four omission-control executions had already finished;
the main race binary had started at 14:10:27 UTC. Those raw execution verdicts
remain preserved, but the original final checkout fence did not pass.

The original linked test executables had been cleaned up by `go test`. Three
exact cached race package archives were recovered, including the production
archive `37a6f4af9d005654ba61603a9137219c8d22b986bc9433c9b35018e4d5bf69e1`, written
at 14:10:21 UTC with the frozen SN source path. Their hashes, build IDs, original
cache modification times, server reflog and complete server diff are retained.
These support the compilation timeline; they do not replace a retained linked
executable or erase the failed checkout fence. The broad batch supplies no
qualification of server `24ac67d4`.

Only the two focused current-clock and public-readback roots were therefore
rerun normally and with race detection using a separate read-only physical
server `94229abb` worktree. That supplement retains both exact linked test
executables, their build IDs/module metadata, and matching before/after source
and module fences. It does not claim a fresh fourteen-root run.

## Independent source review

Sol-medium review used immutable exports of the exact SN `50780465` and server
`94229abb` trees. All 14 independently selected roots passed normally. Under
race detection, 14 distinct roots passed across retained primary and separate
production streams; the primary wrapper reached its 20-minute harness deadline
after ten passing roots, with no failed root, and is retained as a failed
wrapper rather than counted as a package pass. Focused clock and supplemental
production race packages exited zero. Vet passed. Removing installation caller
wiring or the exact clock comparison caused the intended regression assertions
to fail in both normal and race modes. The independent
[receipt](/mnt/data/sn-testnet/qualification/installation-policy-clock-sol-independent-20261001/summary.json)
is SHA-256
`c2c68da752710fa62f11bd2e026a085d3459cb82c553fe3f8937e2551c4abafc`;
the [review](/mnt/data/sn-testnet/qualification/installation-policy-clock-sol-independent-20261001/review.txt)
retains the scope and limits. No patch-specific blocking defect was found.

Current getter replies remain owned-RPC assertions at the authenticated mapping.
This increment does not add complete current contract storage or execution
history authentication. Complete Safe history and complete pending proof remain
unproven under the separately signed current-only policy. Actual independent
production v2 acceptance, all-signer/relayer custody, live installation,
service/operator/validator admission and activation remain external gates.
The selected release built from SN `233ea2be` does not include this successor
source; a rebuilt, independently qualified and approved release is required.
No live mainnet signature, transaction, image publication or service operation
was performed.
