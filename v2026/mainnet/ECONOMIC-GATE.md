# Owner-recycle observation and integer reference

The selected target is 10% of the native miner allocation for providers and
90% owner-recycle. These signer-free commands collect preconditions and evidence for
that policy. They do not construct weights, change validator caps, submit calls,
authorize activation, or establish a final Yuma outcome.

When the mode is Burn, the separate [owner transition](OWNER-RECYCLE-TRANSITION.md)
constructs an independently approved exact native request and retains public
signature/receipt custody. Current source also implements owner-local Ledger
signing, exact returned-reply import on the host, and separately approved bounded
submission/reconciliation; [qualification and actual authority remain open](MAINNET.md).
The observation/reference commands above remain signer-free. A successful
Recycle readback remains a precondition, not proof of the actual 10%/90% outcome.

## Finalized mode precondition

```sh
sn-mainnet check-recycle-mode --rpc "$OWNED_RPC_URL" --policy approved-policy.json
```

Supply network and runtime pins from an independent review, never by approving
whatever the queried endpoint just returned. There is no default mainnet genesis
or runtime version. A policy has these fields; placeholders and zero versions
below deliberately fail validation until replaced by approved values:

```json
{
  "schema": "urnetwork-mainnet-owner-recycle-policy-v1",
  "native_chain": "<approved chain name>",
  "genesis_hash": "<approved 32-byte 0x hash>",
  "evm_chain_id": 964,
  "netuid": 25,
  "storage_profile": "subtensor-recycle-storage-v1",
  "runtime_source_commit": "<reviewed 40-hex source commit>",
  "runtime_version": {
    "specName": "<approved runtime spec name>",
    "specVersion": 0,
    "transactionVersion": 0,
    "stateVersion": 0
  },
  "runtime_code_hash": "<approved 32-byte BLAKE2b-256 0x hash>",
  "runtime_metadata_hash": "<approved 32-byte BLAKE2b-256 0x hash>"
}
```

The command checks native chain, genesis and EVM chain identity, resolves a
finalized hash to its numbered header, and reads the complete runtime tuple,
`:code` storage hash, metadata, and mode at that same finalized hash. It hashes
the exact metadata bytes before decoding SCALE. It then checks the reviewed
storage profile: one `SubtensorModule.RecycleOrBurn` default-valued map, an
Identity-hashed SCALE u16 netuid, fieldless `Burn=0` / `Recycle=1` variants, and
the metadata-declared Burn fallback. Changed profiles or artifact pins fail.
The finalized height-to-hash mapping is checked again after the storage read.

A null storage value means the authenticated Burn fallback, and fails the
Recycle precondition. Missing JSON results, malformed or trailing enum bytes,
unknown variants and an absent/changed metadata entry also fail. The sealed
observation distinguishes raw absence from a stored Burn value and retains the
exact policy-file digest, finalized identity, runtime hashes, storage key and
effective bytes. Sealing is a content hash, not a signature or approval.

All reads share `--retry-window` (default 60 seconds). Identity/storage replies
remain limited to 1 MiB; only `state_getMetadata` has an 8 MiB allowance. No
latest-state fallback, provisional compatibility or implicit runtime upgrade is
admitted. Run the precondition again at activation and recovery boundaries; an
old snapshot does not establish the current mode.

Exit 0 means only that this finalized mode precondition passed. Exit 3 means a
valid Burn observation or inconsistent/mismatching RPC evidence; a valid Burn
observation is still written. Exit 2 means invalid input; exit 1 means an I/O or
RPC failure. Even a successful observation says `activation_ready: false`.

The profile describes storage encoding, not the complete runtime implementation.
The baseline source review is Subtensor commit
`67dcf7f791dc495064c293f080a0702cb433e51e`: enum/default/storage in
`pallets/subtensor/src/lib.rs`, recognized owner hotkeys and distribution in
`pallets/subtensor/src/coinbase/run_coinbase.rs`, and recycle/burn accounting in
`pallets/subtensor/src/coinbase/alpha.rs`. Its runtime number is not a mainnet
default or authority. A different operator-approved runtime may satisfy the
same storage profile, but its matching source-to-code provenance and actual
recycle implementation still require separate review. Metadata alone cannot
prove either.

## Integer reference

```sh
sn-mainnet economic-reference --input native-interval-reference.json
```

Example synthetic input:

```json
{
  "schema": "urnetwork-native-miner-reference-input-v1",
  "native_miner_allocations_alpha": ["9", "1"]
}
```

Each amount is one native interval's pre-withholding miner tranche, in alpha
atomic units. It excludes owner and validator/root allocations, TAO injection,
deposits, reserve principal and collateral principal. Only canonical decimal
u64 strings are accepted. Cumulative totals use arbitrary-precision integers:

```text
provider_total(k) = floor(sum(miner_interval[0..k]) / 10)
provider_interval(k) = provider_total(k) - provider_total(k - 1)
recycle_total(k) = miner_total(k) - provider_total(k)
```

The example allocates provider references `0, 1` and recycle references `9, 0`;
rounding is carried across intervals. Recycling creates no reserve credit and
no deferred provider liability. Reference rounding is distinct from native
runtime truncation dust. A capture followed by a claim is not two rewards.

The input is caller-supplied reference data. This command does not authenticate
interval provenance, detect omitted/replayed native intervals, calculate actual
Yuma incentives, derive quantization tolerance, or certify paid entitlements.
Its output keeps `actual_native_outcome_verified`,
`quantization_tolerance_established`, and `activation_ready` false.

## Bounded native incentive observation

The [incremental composition record](evidence/incremental-source-composition-20260929.md)
retains the observer's 18 focused and 170 adjacent normal/race root results,
eight causal controls and its separate 37-root SDK/MG06 composed qualification.
These source checks leave every economic and activation gate below unresolved.

```sh
sn-mainnet observe-native-miner-emission --rpc "$OWNED_ARCHIVE_RPC_URL" \
  --policy approved-native-observation.json
```

The policy describes an explicit, contiguous `(from_exclusive, through_inclusive]`
window of 1–128 native blocks. It requires a separately reviewed network,
runtime version/code/metadata tuple, subnet registration block and generation;
it grants no signing or economic authority. Deliberately invalid placeholders:

```json
{
  "schema": "urnetwork-native-miner-observation-policy-v1",
  "network": {
    "native_chain": "<approved name>",
    "genesis_hash": "<approved 32-byte 0x hash>",
    "evm_chain_id": 964
  },
  "runtime": {
    "runtime_source_commit": "67dcf7f791dc495064c293f080a0702cb433e51e",
    "runtime_version": {
      "specName": "<approved spec name>",
      "specVersion": 0,
      "transactionVersion": 0,
      "stateVersion": 1
    },
    "runtime_code_hash": "<approved 32-byte 0x hash>",
    "runtime_metadata_hash": "<approved 32-byte 0x hash>"
  },
  "netuid": 25,
  "subnet_registration_block": null,
  "subnet_generation": null,
  "from_exclusive": {"number": 0, "hash": "<exact hash>"},
  "through_inclusive": {"number": 0, "hash": "<exact hash>"},
  "maximum_uids": 4096
}
```

The collector authenticates SCALE header hashes and child-to-parent ancestry
from the owned route's finalized head, verifies every complete block body's
extrinsics root, and decodes all `System.Events` bytes against the approved
**parent execution runtime**. It checks the complete event envelope, variant
names/field shapes and metadata-selected indices, including compatible event
reindexing. A changed runtime or unsupported profile stops completeness; no
latest-state or old-metadata fallback is used. The final head must descend from
the opening head, and boundary canonical hashes and network identity are checked
again after collection. Headers do not supply independent GRANDPA justification
or storage proofs: `finality_authority` is `owned-rpc-assertion`, and
`independent_storage_proof` / `runtime_source_proven` remain false.

Reviewed source
[`epoch/run_epoch.rs`](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/run_epoch.rs)
emits `IncentiveAlphaEmittedToMiners` with UID-ordered u64 alpha terms before
owner recycling or collateral outcomes are reconciled. The collector retains
event indices, exact raw event bundles and hashes, per-UID amounts, and an
arbitrary-precision incentive sum. The event's netuid is a **mechanism storage
index**: reviewed
[`subnets/mechanism.rs`](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/mechanism.rs)
uses `netuid + mechanism * 4096`. Only the existing one-mechanism policy is
supported; an event for another mechanism of subnet 25 is rejected. Selected
events must be initialization events. Duplicate or contradictory target epoch
events, malformed other-subnet events and trailing SCALE bytes are rejected.

Before/after facts include exact subnet generation, UID count, mechanism count,
epoch counters, pending server/validator/root/owner budgets, stored block alpha
emission, owner cut and Recycle/Burn mode. `EpochSkipped` and `EpochDeferred` are
retained separately. An epoch advance without its terminal event fails rather
than becoming an empty emission interval. Explicit null Events storage means
the authenticated empty-vector default; an omitted JSON result is an error.
Burn remains an observed mode with a blocker, never an inferred Recycle pass.

The [October 2 context correction](evidence/native-economics-context-and-schedule-20261002.md)
at `dd21ed00` additionally authenticates `Tempo`, `TempoSet` and
`SubnetOwnerChanged`. `LastEpochBlock` is a scheduling anchor: a matching tempo
write can reset it without consuming an epoch. Such a reset requires unchanged
`SubnetEpochIndex`, a current-block anchor and matching final tempo; unexplained
resets, changed epoch counters and contradictory deferred epochs still fail.
An ordered initialization owner takeover may append one UID before Yuma without
emitting `NeuronRegistered`. Its incentive vector may therefore use the parent
census or parent count plus one. The event establishes neither recipient
generation nor ownership entitlement; those amounts remain unknown.

**The incentive sum is not the policy denominator M.** Reviewed
[`coinbase/run_coinbase.rs`](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs)
can accrue a miner tranche and drain it before post-block pending storage is
read. Stored alpha-out can be stale when a subnet was not selected. The epoch
code normalizes incentive/dividend terms using I32F32, multiplies using I96F32
and truncates each recipient amount to u64. Zero incentive can redirect a
positive pre-withholding miner tranche to dividends. Therefore neither zero
incentive, the vector sum, pending snapshots nor `Emission`/`MinerBurned` alone
establish M or the runtime-derived quantization tolerance Q. Every block retains
these denominator blockers, including zero-incentive fallback uncertainty.

Output schema `urnetwork-native-miner-observation-v1` leaves native allocation,
provider entitlement, owner recycling, Q and target outcome **null**;
`actual_native_outcome_verified` and `activation_ready` remain false. A complete
read has status `observed-economic-outcome-unresolved`. It does not map event UID
slots to authenticated hotkey generations, grant cross-window deduplication,
or treat collateral capture and a later claim as separate rewards. Do not feed
the observed incentive sum into `economic-reference` as an authenticated M.

One `--retry-window` bounds the whole operation (60s–15m, default 60s). Ancestry
walks are limited to 4096 blocks each, events to 1 MiB / 16,384 records per block,
and UIDs to the explicit 1–4096 policy bound. Retained header/block/state JSON has
an 8 MiB budget, plus one separately bounded metadata artifact. A final failed
attempt can add at most one independently bounded event/state response. The
collector retains exact metadata, raw storage, headers and partial block facts;
bodies are checked during collection but not archived in the artifact. These
are finite read bounds, not a durable archive service or a restart cursor.

Exit 0 means only that the requested range was read and rechecked. Exit 3 emits
a content-hashed partial `unresolved` artifact plus an error. Completed-prefix
amounts remain visible, and a failed attempted block never enters that total.
Exit 2 means invalid input; exit 1 means the output could not be written. The
content hash is not a signature, signed policy acceptance or activation approval.

## Remaining economic activation evidence

The separate full gate still needs an independently approved mainnet genesis
and exact runtime source/Wasm/metadata binding; the runtime-recognized owner
hotkey census and admitted signed validator policy; a proven activation drain
boundary without unexplained pre-activation pending miner emission; and exact
native intervals reconciled once to provider entitlement and recycled amounts.
Final incentives depend on the admitted weights, stake, permits, activity,
clipping, bonds, mechanism configuration and runtime rounding. A 90% proposed
weight destination cannot establish the final recycle fraction.

No runtime-derived rounding tolerance or final 10/90 outcome is currently
asserted. The reference output must not be substituted for those observations.
The selected assurance is an observed native target; these commands invent no
owner-enforced cap and do not relax existing validator caps.
