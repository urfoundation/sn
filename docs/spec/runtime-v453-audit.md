# Subtensor runtime 453 release audit

Release 1.0 uses the official Bittensor public testnet while the private archive
finishes syncing. On 2026-09-03 the finalized public chain upgraded from runtime
452 to runtime 453. This document records the source-to-chain identity and the
compatibility review which moved the release lock.

## Exact identity

| Field | Reviewed value |
|---|---|
| Source repository | `https://github.com/RaoFoundation/subtensor` |
| Source tag | `v453` |
| Source commit | `823bdcbc58a29f60b243be4737a7c72b34ac7d93` |
| Runtime spec / transaction / state versions | `453` / `1` / `1` |
| Finalized observation block | `7,925,883` |
| Finalized observation hash | `0x87c707403ffe5b36afb7796e1bd84126cdbcf181a61f97c3f1491c8354ae96f0` |
| On-chain `:code` storage hash (BLAKE2-256) | `0xabe169cc148e2a63068772788c191fa6566f02aa2ea9afb80cdeb28217bab4d4` |
| Exact SCALE metadata hash (BLAKE2b-256) | `0xb00e7e0188d537136a973df4d5c5f2c86ef903ffff49c1cf8d129dabc98b07ce` |
| Official compressed-Wasm size | `2,515,038` bytes |
| Official compressed-Wasm SHA-256 | `0x9e51859faf28a69365005e7dd7f152f239a305c468869b2f54303aba938d840e` |
| Upstream finney release-proposal call hash | `0x972c1c03fae47d58ad3dbfd701e58e56170936045b0a488170c05c8d0729fcd4` |
| Upstream finney release-proposal multisig timepoint | `8987926:11` |

The two Wasm digests intentionally use different algorithms. Subtensor JSON-RPC
returns the BLAKE2-256 storage hash through `state_getStorageHash`; the release
asset manifest publishes SHA-256. The downloaded official `subtensor.wasm` is
2,515,038 bytes; independently hashing those same bytes produced the recorded
SHA-256 and the exact on-chain BLAKE2-256 value above. This establishes byte
identity from the official release artifact to finalized testnet `System.Code`.

The upstream call hash and timepoint come from the official release's finney
multisig proposal. They authenticate the published source/artifact proposal but
are not a testnet receipt: finney block 8,987,926 and testnet observation block
7,925,883 are different chains. Testnet deployment is established only by the
complete runtime-version object, `System.Code` storage hash and exact SCALE
metadata bytes observed at the same finalized testnet block/hash in this table.

Carried immutable setup history is separately restricted to the two observed
predecessor artifacts: runtime 451 uses code/metadata hashes
`0xf3554a22dfcefa9b42b3a0a5e58c1e6c871795ecc9ea9da78bf0900e23e57c08` /
`0xeecd7e7c00377caec23c3dc754fd621963cc456fa5d02a4f66ff267b0494bd9d`;
runtime 452 uses
`0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc` /
`0x2e1d4f992a978fdd58652c8cf434c26bb8f89170e6a0fdbc9362b29e8fe8a835`.
Both require `node-subtensor`, transaction version 1 and state version 1. They
are admitted only to read or reconcile already-finalized persisted evidence,
never to sign or rebroadcast a write or extend the active release.

Primary sources:

- [official runtime 453 release](https://github.com/RaoFoundation/subtensor/releases/tag/v453)
- [exact v452 to v453 comparison](https://github.com/RaoFoundation/subtensor/compare/v452...v453)

The live read-only conformance probe is:

```bash
SP2_LIVE=1 \
SP2_SUBSTRATE=wss://test.finney.opentensor.ai:443 \
SP2_NETUID=521 \
go test ./crv4 \
  -run '^TestLive(MetadataConformance|CommitRevealVersion|EpochScheduleAndRound|DryRunExtrinsicEncoding)$' \
  -v -count=1 -timeout=10m
```

It verifies metadata call shapes, CRv4, epoch scheduling and signed extrinsic
encoding against the live finalized runtime without broadcasting a transaction.
The sim-testnet doctor gets one finalized hash per endpoint and passes that
explicit hash to `state_getRuntimeVersion`, `state_getStorageHash(:code)` and
`state_getMetadata`. Its raw runtime-version decoder requires `specName`,
`specVersion`, `transactionVersion` and `stateVersion` to be present exactly
once and encoded as canonical unsigned JSON integers. Missing, string, signed,
fractional, exponent, duplicate, overflowing and trailing encodings fail before
metadata can drive a storage key or call. It then checks the exact code and raw
SCALE metadata digests from the release lock and binds that authenticated
metadata plus spec/transaction versions to the client. Setup, planning,
execution, revision and read-only scenario paths use the same binding helper.
Signed deployment and production manifests disclose spec, transaction and
state versions plus the code and metadata hashes; readers compare all five to
the locally authenticated release lock. Secretless readers require the same
reviewed `453/1/1` and exact artifact hashes rather than allowing a manifest
signer to redefine release 1.0. Public semantic replay also reads the complete
version object, `System.Code` hash and raw SCALE metadata at
each metadata-driven native checkpoint at or after the signed campaign start,
compares them to that signed manifest, and records all three authoritative
responses in its immutable transcript. The archive-retention preflight likewise
authenticates its public endpoint's current finalized version, code and metadata
before it trusts historical-availability probes. Carried pre-campaign finalized
setup receipts remain explicit runtime-451/452 compatibility evidence: replay
requires exact `451/1/1` or `452/1/1` versions plus the known code and metadata
hashes at each block, without relabeling those historical bytes as runtime 453.
Historical native-funding reconciliation also requires its recovery and
inclusion checkpoints to share one complete artifact identity and reconstructs
the signed bytes with that authenticated historical metadata/version.
The continuous adversary samples both endpoints' complete version identity and
`:code` hash at each endpoint's native `chain_getFinalizedHead` hash; it never
substitutes an Ethereum block hash. Metadata hashing stays in the pre-launch
doctor and pinned public replay because the response is large.
Generated validator configurations carry all five fields, and the production
validator rejects any release-1.0 configuration which does not name this exact
reviewed identity before dialing or signing.

The release source gate authenticates the tag resolution and all 12 changed
Rust source/test files against `docs/spec/runtime-v453-source.sha256`:

```bash
scripts/check-runtime-v453-source.sh
# Or bind the same check to the clean checkout used for upstream tests:
SUBTENSOR_RUNTIME_SOURCE=/absolute/path/to/subtensor \
  scripts/check-runtime-v453-source.sh
```

This gate proves that the reviewed implementation and regression-test bytes are
the bytes at commit `823bdcbc58a29f60b243be4737a7c72b34ac7d93`; it does not
compile the runtime or execute FRAME. The authoritative Terra run used the
following commands from a clean detached checkout at that exact commit. Clang
is a required build dependency. The persistent external Cargo target keeps the
large initial runtime build reusable; the runtime tests must also point
`WASM_BUILD_WORKSPACE_HINT` at the clean checkout so the Wasm builder can find
the workspace:

```bash
test "$(git rev-parse HEAD)" = "823bdcbc58a29f60b243be4737a7c72b34ac7d93"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
command -v clang
export CARGO_TARGET_DIR=/absolute/persistent/cargo-target-v453
SUBTENSOR_RUNTIME_SOURCE="$PWD" /absolute/sn/scripts/check-runtime-v453-source.sh

cargo test -p pallet-drand --features std it_rejects_valid_signature_with_unbound_randomness
cargo test -p pallet-subtensor-proxy nested_proxy_intersects_outer_and_inner_authority
cargo test -p pallet-subtensor-proxy nested_announced_proxy_intersects_outer_and_inner_authority
cargo test -p pallet-subtensor test_transfer_stake_rejects_beta_escrow_destination
cargo test -p pallet-subtensor queued_network_registration_reserves_new_hotkey_for_payer
cargo test -p pallet-subtensor queued_network_registration_consumes_rate_limit
cargo test -p pallet-subtensor set_new_network_state_without_lock_id_updates_lock_pricing
cargo test -p pallet-subtensor register_network_queues_when_waiting_for_dissolve_cleanup
WASM_BUILD_WORKSPACE_HINT="$PWD" cargo test -p node-subtensor-runtime balance_transfer_precompile_respects_dispatch_guard_policy
WASM_BUILD_WORKSPACE_HINT="$PWD" cargo test -p node-subtensor-runtime proxy_precompile_preserves_outer_filter_across_nested_proxy
```

All ten named regressions passed: 10 passed, 0 failed, 0 ignored. The first
runtime test required about 9 minutes 49 seconds for the initial compile; the
two runtime tests completed in about 4.8 seconds with that persistent target
warm. These timings are recorded observations, not weakened test timeouts.

## Reviewed deltas

The upstream comparison contains one release commit and 12 changed files. The
five semantic changes are all security or economic hardening:

1. `pallets/drand/src/verifier.rs` derives SHA-256 from the authenticated
   signature and rejects a separately supplied randomness mismatch.
2. `pallets/proxy/src/lib.rs` carries the outer origin into both direct and
   announced nested proxy dispatch and intersects its filter with the inner
   proxy filter.
3. `precompiles/src/balance_transfer.rs` adds an explicit `CheckColdkeySwap`
   against the true mapped EVM caller to payable `transfer(bytes32)` before its
   runtime `transferAllowDeath` dispatch from the precompile account. The
   adjacent unchanged `transferKeepAlive(bytes32,uint256)` path already
   dispatches as the caller through the centralized runtime filter. The local
   decision model covers the effective policy on both paths without claiming
   that both received the v453 source patch.
4. `pallets/subtensor/src/staking/stake_utils.rs` rejects the protocol beta
   escrow as a user stake-transfer destination.
5. `pallets/subtensor/src/subnets/subnet.rs` reserves a queued registration's
   hotkey and updates last-lock/last-lock-block at queue admission; processing
   that queued entry no longer updates pricing a second time.

The queue regression deliberately models queue-state decisions, not FRAME's
balance storage transaction layer. Runtime 453 performs the identity,
system-account, rate, affordability and lock-id checks before locking the cost;
there is no external callback between the lock and the repeated system-account
check in hotkey reservation. The pinned upstream `networks.rs` tests exercised
the actual runtime path in Terra's passing Rust run above; their presence alone
would not be execution evidence. The local Go model separately checks the
reservation, exact rate boundary, partial-mutation and no-repricing decisions
used by the release's adversarial catalogue without claiming FRAME
transactional coverage.

Each delta has a deterministic Go decision-model supplement referenced by
`docs/spec/adversarial-matrix-v1.json`; those models do not execute FRAME or the
Wasm. Release acceptance requires the pinned source manifest, exact release
artifact, live code identity and Terra's recorded 10/10 execution of the
upstream Rust regressions; this source patch alone is not Rust execution
evidence. The concurrent actor fails on spec, transaction-version,
state-version, code-hash or finalized-head drift; the pre-launch doctor also
binds the exact finalized metadata bytes. No actor attacks a third-party account
or the shared public testnet.

## Compatibility conclusion

The comparison does not modify the commitments registration type, CRv4 weight
call, Neuron/Staking/signature-precompile selectors used by the release, or the
share-floor and registration-burn behavior already handled by the harness. Live
metadata and dry-run encoding probes pass at spec 453. The persisted v452 wire
schema identifier remains unchanged so already authenticated attempt history
continues to decode. Runtime-452 wording also remains in production Solidity
source comments whose source hashes are embedded in the already locked v453
artifacts; changing comments would produce new artifact identities. This audit
binds those comments to unchanged v453 compatibility semantics. Active Go types,
diagnostics, execution targets and new test names bind runtime 453 explicitly.
