# Owner-recycle observation and integer reference

The selected target is 10% of the native miner allocation for providers and
90% owner-recycle. These signer-free commands implement two preconditions for
that policy. They do not construct weights, change validator caps, submit calls,
authorize activation, or establish a final Yuma outcome.

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
