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
spec version and `System.Code` storage hash observed at the finalized testnet
block/hash in this table.

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
The sim-testnet doctor separately authenticates the exact finalized Wasm hash.

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
check in hotkey reservation. The upstream `networks.rs` tests execute the actual
runtime path and prove unaffordable registration is state-neutral, queued
registration reserves ownership immediately, and queue-time rate/lock state is
preserved through materialization. The local Go model separately proves the
reservation, exact rate boundary, partial-mutation and no-repricing decisions
used by the release's adversarial catalogue without overstating transactional
coverage.

Each delta has a deterministic Go decision-model supplement referenced by
`docs/spec/adversarial-matrix-v1.json`; those models do not execute FRAME or the
Wasm. Runtime behavior is grounded in the pinned upstream Rust regressions,
source diff, exact release artifact, and live code identity. The concurrent
actor for these chain-wide vectors is observation-only and fails on runtime,
state-version, or finalized-head drift; the pre-launch doctor separately binds
the finalized code hash. No actor attacks a third-party account or the shared
public testnet.

## Compatibility conclusion

The comparison does not modify the commitments registration type, CRv4 weight
call, Neuron/Staking/signature-precompile selectors used by the release, or the
share-floor and registration-burn behavior already handled by the harness. Live
metadata and dry-run encoding probes pass at spec 453. Existing v452 decoder and
historical evidence names therefore remain stable rather than rewriting already
authenticated attempt history; current manifests, doctor checks and new plans
bind runtime 453 explicitly.
