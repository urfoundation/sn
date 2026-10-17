# Mainnet runtime observation admission — 2026-09-27

MG-04 / RT-04 remains **in progress**. This change makes independently approved
mainnet runtime identities observable through the outer validator gate. It does
not enable a production validator, signing, submissions, archive acceptance,
or automatic qualification of an unknown successor.

Source baseline: SN `2be0aa53e2ea03624d8e7f504eaa3ba5de969ce3`.
Toolchain: `go1.26.6 linux/amd64`. All fixtures use synthetic identities and
in-process read-only Rpc clients. No live chain, active configuration, release
lock, signer, journal or supervisor was changed.

## Root cause and correction

The inner stake/schedule and CRv4 source adapters already distinguish exact
artifact approval from purpose-specific compatibility. The outer validator
still required its compiled release tuple, so an independently reviewed mainnet
successor could not even pass configuration/native identity admission.

`LoadMainnetRuntimeObservationConfig` now admits a separate `schema_version: 2`
configuration. The existing `ReleaseConfig` fields and policy/evidence grammar
remain required; `mainnet_runtime_approvals` adds an ordered list of exact
`path`, `bytes` and `sha256` references. Existing schema-1 serialization is
unchanged because the new field is omitted when absent.

Each approval is one JSON document with these fields:

| Fields | Required meaning |
| --- | --- |
| `schema` | `urnetwork-validator-mainnet-runtime-approval-v1` |
| `revision`, `previous_sha256` | Revision 1 starts the history without a predecessor; each later revision increments by one and names the preceding document's exact SHA-256 |
| `native_chain`, `genesis_hash`, `evm_chain_id` | Independently supplied chain name and nonzero non-testnet genesis; EVM chain 964 |
| `deployment_id`, `validator_id`, `netuid`, `coordinator`, `policy_hash` | Exact configuration scope; mainnet policy must also validate and hash correctly |
| `valid_from_block`, `valid_through_block` | Closed, nonzero native block interval, bounded by the native `uint32` height; later intervals must be disjoint and increasing |
| `runtime_version` | Complete `specName`, `specVersion`, `transactionVersion`, `stateVersion` object; native spec name is `node-subtensor` |
| `runtime_code_hash`, `runtime_metadata_hash` | Exact nonzero code and metadata hashes |
| `runtime_source_commit`, `runtime_review_sha256` | Independently reviewed 40-hex source commit and 64-hex provenance/review digest |
| `runtime_review_scope` | `urnetwork-validator-runtime-observation-v1` |

History is bounded to 64 documents of at most 16 KiB each. The existing private
descriptor reader verifies each file's exact size, SHA, ownership and immutable
read. Parsing rejects duplicate keys, unknown fields and trailing JSON. The
configuration must pin the last approved runtime tuple, while a historical read
selects exactly one tuple by the requested block's interval. A lower spec
version is not inherently an error: an explicitly approved rollback is still
selected by its exact artifact and original interval.

References pin the operator-supplied independent approval bytes. A matching
digest does **not** prove an independent review occurred, and these documents
are not governance signatures. No production identity or approval is shipped.

## Read and authority boundaries

Call `ObserveMainnetRuntimeAtContext(ctx, native, cfg, blockHash)` with a config
returned by the new loader and an already opened native connection. A zero hash
selects the current finalized head; otherwise the exact requested historical
block is observed. The connection's URL must be listed in the config.

The observer checks fresh chain name, genesis and EVM964, canonical finalized
head/height, the selected block's canonical height and finality, then exact
runtime version/code/metadata. It repeats the selected block's canonical check
before returning. All network work uses the caller's context under a two-minute
ceiling. Rpc finality and ancestry remain provider assertions, not light-client
proofs or an independent-provider comparison.

The returned value contains the normalized serialized config SHA, exact
approval SHA/revision, chain identity, block/finality hashes and observed runtime
tuple. It exposes no authenticated signing proof and never rebinds the supplied
connection's mutable signing view. The private current/historical runtime gates
can bind a separately owned read view using the same checked approval interval.

Loaded approvals are immutable parsed objects, sealed to every serialized
configuration field. Replacing an approval path cannot reinterpret a running
observer; a fresh load against the old reference rejects the replacement.
Changing the config invalidates its seal. An older config with only the first
approval does not inherit a successor from another config or a later binary.

Producer, bootstrap, provisional and final-archive loaders reject schema 2.
Matching a runtime version cannot bypass explicit guards at hotkey loading,
native journal opening, producer startup stake eligibility, signing, prepared
replay and submission. Testnet provisional flags/connections and owner-recycle,
adoption or source-predecessor authority cannot be mixed into this path.

This is an identity observation purpose only. It neither admits arbitrary
storage layouts nor claims semantic or transaction compatibility. The existing
stake/schedule and source capability gates continue to reject incompatible
consumed interfaces. A complete successor policy, mainnet identity qualification,
producer history migration and operation-specific approval remain required
before any live signing/submission can be enabled.

## Deterministic validation

The new tests exercise the real config loader, outer runtime gate and read-only
observer. They cover exact successor/current/historical selection, explicit
rollback, original config isolation, altered approval bytes, malformed/ambiguous
documents, revision/predecessor/interval and every identity coordinate, sealed
config mutation, cached metadata with fresh genesis/code drift, noncanonical or
unfinalized blocks, cancellation, concurrent readers, unauthorized routes and
provisional connections. Concrete writer boundaries are tested before file or
Rpc access, including the formerly permissive matching signing version.

Affected regression command:

```sh
go test ./validator -run '^Test(MainnetRuntimeObservation|AuthenticatePinnedNativeRuntime|ReleaseRuntime|ReleaseConfig|LoadReleaseConfig|ReleaseProvisionalRuntime|ReleaseEvidenceV2HistoricalRuntime|ReleaseNativeValidator|NativeCommand|ReleaseEvidenceV2Config)' -count=1 -timeout=300s
go test -race ./validator -run '^Test(MainnetRuntimeObservation|AuthenticatePinnedNativeRuntime|ReleaseRuntime|ReleaseConfig|LoadReleaseConfig|ReleaseProvisionalRuntime|ReleaseEvidenceV2HistoricalRuntime|ReleaseNativeValidator|NativeCommand|ReleaseEvidenceV2Config)' -count=1 -timeout=300s
go vet ./validator
```

Causal controls replace one production file at a time using `go test -overlay`
with its exact baseline contents, leaving the new tests and all other source
unchanged. Restoring `validator/runtime_identity.go` makes the real successor
admission fail with the compiled-artifact refusal. Restoring
`validator/provisional_runtime_compatibility.go` makes the concrete signing
boundary accept the observation config solely because the runtime version
matches. Both controls must fail; the fixed implementation must pass.

Commands, overlays and logs are retained at
`/mnt/data/sn-testnet/evidence/mainnet-runtime-observation-20260927/`.

Recorded results:

- Affected normal regressions: **PASS**, 37.776s (`normal.log`).
- The same affected regressions with race detection: **PASS**, 230.153s
  (`race.log`).
- Final `^TestMainnetRuntimeObservation` tests, including the last omitted-history
  and unapproved-route controls: **PASS**, normal 0.871s and race 6.392s
  (`final-focused-normal.log`, `final-focused-race.log`).
- `go vet ./validator`: **PASS**, no warnings (`vet.log`).
- Old outer-gate control: **expected FAIL**, "native runtime is not the reviewed"
  artifact (`negative-runtime.log`).
- Old signing-gate control: **expected FAIL**, "matching version laundered
  observation into signing" (`negative-signing.log`).
- `git diff --check`: **PASS**.
