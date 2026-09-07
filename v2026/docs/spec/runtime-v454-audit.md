# Subtensor runtime 454 release audit

Release 1.0 uses the official Bittensor public testnet while the private archive
finishes syncing. The public testnet upgraded from runtime 453 to runtime 454 on
2026-09-04, after the preceding source-freeze candidate had been assembled.
This audit records the mandatory repin, source review and compatibility gates.

## Exact deployed identity

| Field | Reviewed value |
|---|---|
| Source repository | `https://github.com/RaoFoundation/subtensor` |
| Source tag | `v454` |
| Source commit | `14cde6410fe8ec81a940e290c56f94a632a0988d` |
| Runtime spec / transaction / state versions | `454` / `1` / `1` |
| Last runtime-453 block | `7,934,386` / `0xe98acd786b5bedba7dc0eeeeafe04943ec10d2ae624d958933f4a14699efab31` |
| First runtime-454 block | `7,934,387` / `0x5b3f3455125d78812299002a1926792a6876b03ac636ae53e93e4115f15a392b` |
| On-chain `:code` storage hash (BLAKE2-256) | `0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef` |
| Exact SCALE metadata hash (BLAKE2b-256) | `0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702` |
| Official compressed-Wasm size | `2,515,968` bytes |
| Official compressed-Wasm SHA-256 | `0xa55e76b4f4620bcdb4c787e499c87a35abb9913ba4cde001b08a00d1945ac4db` |
| Exact SCALE metadata size | `334,642` bytes |
| Exact SCALE metadata SHA-256 | `0xb592bafacd0f3cce1340a91f237f82a531968bd833cbd27339328c80ce92b1cf` |
| Upstream finney release-proposal call hash | `0x5a1c30f0387796da59522d4b84a71395533a4ee676e06c52eedb14262ae9c3c6` |
| Upstream finney release-proposal multisig timepoint | `8996567:7` |

The two Wasm digests intentionally use different algorithms. Subtensor RPC
returns the BLAKE2-256 storage hash through `state_getStorageHash`; the official
release asset manifest uses SHA-256. Executing `Core_version` and
`Metadata_metadata` from the exact compressed artifact reproduced
`node-subtensor/454/1/1`, metadata v14 and all recorded byte identities. At
22:18 UTC on 2026-09-04, `scripts/check-runtime-metadata-artifacts.sh` passed all
four exact deployed identities 451, 452, 453 and 454 against the public chain.

The finney call hash and timepoint authenticate the official publication; they
are not testnet receipts. Testnet identity is established by the exact version,
code and metadata responses at the first v454 block. Current reads, signing and
broadcasts accept only v454. Historical replay accepts the exact v451, v452 and
v453 tuples already present in the attempt journal. The per-provider immutable
metadata cache is hard-bounded to those four total identities, rejects a fifth,
and never lets historical metadata satisfy a strict current-runtime read.

Primary sources:

- [official runtime 454 release](https://github.com/RaoFoundation/subtensor/releases/tag/v454)
- [exact v453 to v454 comparison](https://github.com/RaoFoundation/subtensor/compare/v453...v454)
- [runtime contracts filter at the reviewed commit](https://github.com/RaoFoundation/subtensor/blob/14cde6410fe8ec81a940e290c56f94a632a0988d/runtime/src/lib.rs)
- [root-claim implementation at the reviewed commit](https://github.com/RaoFoundation/subtensor/blob/14cde6410fe8ec81a940e290c56f94a632a0988d/pallets/subtensor/src/staking/claim_root.rs)

## Source and artifact gates

The source manifest pins 24 changed or retained security-critical Rust files
and 12 metadata-generation files. The exact clean checkout passes:

```bash
test "$(git rev-parse HEAD)" = "14cde6410fe8ec81a940e290c56f94a632a0988d"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
SUBTENSOR_RUNTIME_SOURCE="$PWD" /absolute/sn/scripts/check-runtime-v454-source.sh
/absolute/sn/scripts/check-runtime-metadata-artifacts.sh
```

The second gate does not trust a local rebuild. It authenticates the official
compressed artifact against testnet `System.Code`, executes its runtime APIs in
the locked host-function sandbox, and requires exact SCALE decoding without
trailing bytes. A synthetic storage-host-call fixture proves the sandbox fails
closed rather than silently fabricating state.

The upstream FRAME qualification from the clean v454 checkout is:

```bash
export CARGO_TARGET_DIR=/absolute/persistent/subtensor-v454-target
cargo test --locked -p share-pool test_full_integer_withdrawal_clears_sub_rao_share_residue
cargo test --locked -p pallet-subtensor test_claim_root_declared_weight_covers_bounded_work
cargo test --locked -p pallet-subtensor test_claim_root_rejects_work_above_declared_budget
cargo test --locked -p pallet-subtensor test_claim_root_ignores_network_count_and_bounds_actual_basket_rows
cargo test --locked -p pallet-subtensor test_coldkey_wide_claim_selects_only_root_relevant_hotkeys
cargo test --locked -p pallet-subtensor test_root_basket_rounding_zero_take_sells_minimum_unit
cargo test --locked -p pallet-subtensor test_root_basket_rounding_zero_root_row_does_not_block_payable_rows
cargo test --locked -p pallet-subtensor test_storage_bloat_cleanup_is_bounded_and_preserves_nonzero_state
cargo test --locked -p pallet-subtensor test_staking_hotkeys_cleanup_is_bounded_and_preserves_live_relationships
WASM_BUILD_WORKSPACE_HINT="$PWD" cargo test --locked -p node-subtensor-runtime --test claim_root_weight
WASM_BUILD_WORKSPACE_HINT="$PWD" cargo test --locked -p node-subtensor-runtime --test metadata test_metadata
```

These source tests are a release gate, not evidence merely because their files
exist. Their actual output must be retained in the frozen qualification record.
The v454 source does not add a dedicated `ContractCallFilter` regression. That
gap is covered locally by an exact decision model and at deployment by the
disposable live contracts/precompile conformance battery; neither is described
as an upstream FRAME test.

The complete command list above passed from the clean `v454` checkout at commit
`14cde6410fe8ec81a940e290c56f94a632a0988d` on 2026-09-04 at 22:46 UTC, using
`CARGO_TARGET_DIR=/home/by/urnetwork/temp/subtensor-metadata-target`. The result
was 1/1 share-pool test, 8/8 focused pallet tests, 2/2 runtime claim-root weight
tests, and 1/1 runtime metadata test, with zero failed or ignored selected tests.
Cargo reported one upstream future-compatibility warning in `trie-db v0.30.0`:
its use of never-type fallback will require an explicit unit annotation in a
future compiler. This is a build-toolchain warning, not a failed runtime test or
deployed-Wasm discrepancy. The release keeps the upstream lockfile and reviewed
Rust toolchain unchanged; silently patching that dependency would invalidate the
exact-source qualification. Requalification is mandatory before adopting a Rust
compiler which promotes that warning to an error.

## Reviewed semantic deltas

The 23-file comparison contains five release-relevant behavior groups:

1. `ContractCallFilter` now admits the inherited inner
   `SubtensorModule::transfer_stake` call in addition to the exact
   `Proxy::proxy` envelope. This restores the contract-proxy path affected by
   v453 while retaining the intersection fix. No sibling move/add-stake,
   utility, arbitrary proxy variant or unknown call is admitted by this filter;
   the user's explicit proxy delegation remains independently required.
2. Coldkey-wide root claims classify at most 256 `StakingHotkeys` relationships
   and retain only hotkeys with root stake or a negative basket watermark.
   Admission separately counts each selected hotkey and every raw legacy Alpha
   and AlphaV2 row, including zero and duplicate rows. Declared dispatch weight
   now includes both full-claim and selection-scan envelopes; actual weight
   charges the larger observed dimension.
3. A live non-root holding with a positive marked entitlement whose
   proportional alpha slice floors to zero sells one atomic alpha. A nonfinal
   claimant receives no more than its marked entitlement; sale surplus remains
   root cash for other shareholders. Root and terminal rows retain their normal
   floor behavior, and the final shareholder can drain realized cash.
4. Withdrawing all displayed integer value now removes a remaining positive
   share worth less than one rao and subtracts it from the denominator. This
   prevents a supposedly drained position from reviving after later emissions
   while preserving the share/denominator conservation invariant. Failed
   valuation is never interpreted as zero.
5. Storage cleanup adds raw AlphaV2 zero rows, uses fresh v3/v2 completion
   markers, and releases dependent `StakingHotkeys` cleanup only after a
   positive storage-GC completion marker. Missing or removed progress state is
   no longer misread as successful completion.

The harness has deterministic Go models for every decision above. The custody
actor executes all seven local v454 boundary cases during both its control and
attack phases while the RPC actor independently samples exact runtime identity.
These models are bounded adversarial oracles, not substitutes for the pinned
FRAME tests or deployed-Wasm conformance checks.

## Release compatibility conclusion

The runtime keeps transaction and state versions at 1 and does not change the
commitment registration wire shape, CRv4 weight call, metagraph/neuron/staking
precompile addresses, or selectors used by the release. The contract filter
change directly affects the delegated transfer-stake path and therefore must
pass the live conformance battery before any campaign write is accepted. The
root-claim, share-dust and migration changes affect adjacent custody economics;
their exact upstream tests and continuous bounded actor are mandatory even
though the sim-testnet does not mutate third-party root positions.

No v453 observation, prior source freeze, generated manifest or signer can
authorize v454 implicitly. Release readiness requires the refreshed release
lock, source manifest, exact-Wasm gate, upstream qualification, local normal and
race gates, live doctor and deployment conformance evidence to agree on the
identity in this document.
