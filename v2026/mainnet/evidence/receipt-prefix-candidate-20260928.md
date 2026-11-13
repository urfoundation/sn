# Bounded native receipt-prefix candidate — 2026-09-28

[Component qualification is complete](receipt-prefix-qualification-20260928.md):
56 roots have scoped normal/race passes after retained fixture corrections,
nine causal families reproduce their intended failures, and vet passes. The
receipt records source-capture limits and final release work still open. Astra
performed compile-only checks; Terra executed bodies. No live reads, signing
or deployment.

Base: `a58878eaf15fee744cfc20fd06a17b623a27547f`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-receipt-prefix-20260928`.

The previous validator scan restarted at preparation after any late failure.
Miner recovery similarly persisted only after the whole up-to-4,096-block range.
A 60-second read attempt could therefore lose thousands of completed reads and
repeat indefinitely. The shared scanner now returns at most 128 authenticated
complete bodies per chunk, with original transaction/range inputs and actual
coverage separate from the observed finalized head. Its existing unbounded
compatibility API keeps its behavior. A later unavailable read returns the
already authenticated prefix alongside its original error, including a prefix
shorter than 128 blocks. The incomplete body contributes no absence. Actual
commitment/identity contradictions, including mixed integrity/timeout errors,
do not export a partial witness.

Miner recovery commits each completed chunk through the existing authenticated
record owner and semantic `ScanProof` v1. Legacy cursors still require their one
qualified rescan; no executable identity is added. The complete body supplies
number/parent commitment, removing redundant per-block header/hash calls.

Production validator recovery authenticates original runtime authority before
loading one 4 KiB-bounded signed cache file. Its scope contains the original
complete config hash, genesis, hotkey, vector, envelope/sidecar signature,
prepared transaction/hash/height and original creation time. Its signing domain
does not authorize native operations. Only a complete shared-reader witness for
that transaction and contiguous range can advance it. No intent/history write
occurs per chunk. Each chunk owns the existing 60-second attempt and 300-second
read budget; one poll has a 4,096-block ceiling. Completed local work is not
subjected to another whole-operation I/O timeout. A read-attempt deadline saves
its admitted prefix before retry; two controlled deadline barriers in the real
V2 owner test verify that slow throughput does not repeatedly lose that work.

Descriptor-owned private reads and atomic writes retain no-follow, owner, mode,
single-link, finite-size and close checks. Missing/empty/other-intent/old-schema
cache entries cause a rescan. Eviction while a process retains authenticated
coverage can be repaired by the next complete chunk. Forged coverage or unsafe
file ownership is rejected by the cache reader and never used as evidence.
Because this namespace is optional acceleration, read/save failure disables its
disk use for the runtime and emits one closed cache-degraded observation. The
runtime can keep its actually verified scalar prefix in memory; a cold restart
rescans if no valid file remains. A skipped write is not a durability claim.
Original intent, signature, authority and chain contradictions remain hard.
A canonical parent recheck joins each resumed chunk; a behind endpoint preserves
pending work. Cache loss does not erase the durable intent or authorize a
replacement signature. The bounded public-service diagnostic exporter is a
separate composition dependency: this slice exposes and tests a nonblocking
closed callback, and does not claim its nil default is an observable alert.

The historical-only pending branch now reads nonce after complete absence and
schedule at the same authenticated boundary, allowing genuine foreign nonce
consumption to resolve old liability. It still cannot rebroadcast through old
authority, and an epoch/approval deadline is not mortality for signed immortal
bytes. Exact receipt/event/application verification remains unchanged.

Deterministic scope includes:

- Shared bounded coverage, interrupted/null bodies, exact canonical predecessor,
  input/continuity binding, bounded requests and behind-head unknown outcome.
- Real miner command/store restart through a null response after 128 complete
  bodies and within a partial chunk, runtime-update digest 8, restored exact
  inclusion and one original send.
- Real nonempty V2 begin/update/restart with a late receipt timeout, completed
  signed prefix, cache loss during continuation and cold eviction, unchanged
  intent bytes/age, later original inclusion and one original broadcast.
- Separately signed config renewal preserving original cache scope and actual
  durable foreign-nonce resolution without granting replay or inventing receipt.
- Actual cache signatures and private physical files; changed signature/hash/
  height, symlink, hardlink, mode and size controls reject reuse. Actual corrupt
  cache reads and failed optional-path writes leave the original V2 owner usable,
  preserve memory progress and report degradation once.
- The actual 60/300-second read owner is interrupted by deterministic logical
  deadlines after genuine admitted bodies; retry begins after each saved prefix,
  and nonce is read only after coverage reaches its captured finality boundary.

The long synthetic fixture selects future headers before observation. Its optional
finite hash-to-height lookup only removes repeated fixture header construction;
all returned raw headers/bodies still pass the real canonical/trie reader.
The separately signed zero-price source corpus tests continuation, not paid
capture, ten-percent economics or mainnet acceptance.

Recommended affected qualification (enumerate nonzero roots before bodies):

```sh
go test ./crv4 -run '^Test(ReceiptScan|ReceiptHeader|LocateFinalizedExtrinsic)' -count=1 -timeout=600s
go test ./miner -run '^TestFleet(Mainnet(Publish|Register)|Recovery(Native|Publish|Register))' -count=1 -timeout=1200s
go test ./validator -run '^Test(ProductionReceipt|ProductionContinuation|ProductionAuthorityHistoryPending)' -count=1 -timeout=1800s
go vet ./crv4 ./miner ./validator
```

Run normal and race. Preserve previously qualified full public-startup scopes;
this does not modify startup/loop/monitor code. Their correction source will be
composed separately. Reuse unchanged strict receipt/source-finality suites and
their causal evidence; repeat only affected shared-range/caller regressions.

Static source enumeration selects 15 CRv4, 23 miner and 18 validator roots.
Terra must still enumerate the built package before running bodies. The author
compiled all three packages with `-run '^$'`; no test-body pass is claimed.
The nine prepared source controls and exact intended assertion substrings are
in `/mnt/data/sn-testnet/qualification/receipt-prefix-20260928/causal/manifest.json`.
`prepare.go` beside that manifest can remap the overlays to a physical frozen
qualification checkout without changing its source.

Causal controls must compile and reach named invariants: restore the original
miner all-or-nothing range to lose the saved prefix, disable validator prefix
loading to repeat completed bodies, restore historical-only wait before nonce
to strand old liability, and remove signature verification to accept forged
coverage. Additional controls remove interrupted-prefix return or restore a
fatal cache-read/save return. A fixture setup failure is not a successful causal control. Keep all
failed captures and correct only their affected root/source when needed.
