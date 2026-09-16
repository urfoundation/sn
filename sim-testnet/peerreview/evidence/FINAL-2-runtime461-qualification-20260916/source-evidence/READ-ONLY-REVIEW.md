# Read-only runtime 460 to 461 adjacent and consumed-interface review

Scope: local source review against SN `1860261f524054b3d8132a48f75307ffdb792322`. No source/state mutation, RPC, tests, builds, artifact execution or chain qualification was performed. This document records source compatibility findings; exact Wasm/metadata identity and focused qualification remain separate requirements.

The supplied GitHub comparison is from `8d5f20ec1a5e5d90295d43046dacdefc54aaed06` to `7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b`: 68 commits, 273 changed files. All production patches under runtime/pallets/common/precompiles have complete added/deleted-line counts matching the comparison record. The extracted new source was independently hashed against all 71 paths in the prior reviewed source manifest: 53 identical, 18 changed, none missing.

## Exact inputs

- GitHub comparison: `/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/upstream-records/460-to-461.json`; SHA256 `135984190e3bf917a96e3ae3b659f9fcfe2c2975f433ace440e431e6516c283d`.
- Exact source archive: `/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/upstream-records/upstream461-source.tar.gz`; SHA256 `864ce118ad144b688183346066ca63f174f2ae27e2e40a8a695208e0071cc875`.
- Prior 71-path manifest: `/home/by/urnetwork/sn/docs/spec/runtime-v460-source.sha256`; SHA256 `98c2aac04434d2ec67931479cf646918571970718078c50f711486c8db889644`.

## Consumed interfaces

- `pallets/subtensor/src/rpc_info/metagraph.rs` is byte-identical, including selective layout `bb7420226d39c0eb` at line 119 and `get_selective_metagraph` at line 877. The selective five-field request and complete 77-field decoder in SN `crv4/validator_stake.go` do not acquire a source-layout change.
- In `runtime/src/lib.rs`, the complete diff changes the spec constant, adds a root-weights migration, and changes only BetaBasketRuntimeApi from version 3 to 4. `TxExtension` at line 1655, unchecked extrinsic shape, and pallet indices Subtensor=7, Utility=11, Commitments=18 remain unchanged. CRv4 calls 113 and 118 in `pallets/subtensor/src/macros/dispatches.rs:1806,1899` are unchanged; the complete dispatch diff removes root-only call 146 and adds basket call 150. Commitments, Utility, drand and epoch source paths have no comparison changes.
- The complete `pallets/subtensor/src/subnets/weights.rs` diff removes only `do_set_root_weights`. Existing non-root commit/reveal, stake/permit checks (`check_validator_permit` at 1078), and canonical schedule (`get_epoch_index` at 1184) remain unchanged.
- Changed storage declarations in `pallets/subtensor/src/lib.rs` are root-basket storage and new `SubnetFastMovingPrice`. Consumed registration/owner/alpha/permit/schedule/reward declarations remain untouched: Keys, Uids, Owner, SubnetworkN, TotalHotkeyAlpha, StakeThreshold, ValidatorPermit, SubnetOwnerHotkey, SubnetEpochIndex, Emission, Incentive and Dividends.
- `pallets/subtensor/src/staking/stake_utils.rs` adds a fast-price EMA after the existing slow EMA (`update_fast_moving_price`, line 83). It factors alpha/TAO author-fee settlement into helpers at lines 848 and 877 while retaining the original recipient, amounts, no-author burn and error branches. The inherited-stake formula at line 342 and stake lookup at line 559 are unchanged. Parent/child set_children, alpha move/lock/remove, registration/leasing, share pool, root coinbase, delegate API and EVM address/transfer precompile files are byte-identical to the prior manifest.
- `common/src/proxy.rs` appends BasketTrading=18 without shifting existing proxy variants. Proxy filters add the corresponding narrow basket grant and remove the deleted root call; existing CRv4/commitment authority is unchanged.
- Production Go search across SN sim-testnet/crv4/validator/miner found no consumers of `set_root_weights`, `stake_into_basket`, `claim_root`, `swap_basket`, RootWeights, BetaBasket, BasketSummary, `get_validator_weights` or SubnetFastMovingPrice. Historical final native reward observation still consumes the UID-indexed emission/incentive/dividends/alpha surfaces (`sim-testnet/final_semantic_native_reward_read_v2.go:83`).

No additional consumed-interface incompatibility was identified in this bounded source review. This does not claim that all upstream economic behavior is unchanged.

## Substantial upstream changes outside those consumed interfaces

Root basket target vectors are removed. Basket deposits mirror holdings; dividends accrue in place; new `swap_basket` trades are bounded by concentration, liquidity, turnover and price controls. Basket flush/claim work accounting changes. BetaBasketRuntimeApi removes `get_validator_weights`, adds trading status, and changes BasketSummary layout. These changes require truthful source provenance rather than a spec-only description.

Relevant additional production witnesses include `pallets/subtensor/src/staking/{basket_trade,basket_flush,basket_views}.rs`, `pallets/subtensor/src/migrations/migrate_remove_root_weights.rs`, `pallets/subtensor/src/rpc_info/basket_info.rs`, `pallets/subtensor/src/macros/events.rs`, `pallets/admin-utils/src/{lib,weights}.rs`, and `common/src/proxy.rs`. Existing changed manifest paths include the shared claim/fee/proxy implementations below.

`migrate_remove_root_weights` at line 39 clears `Weights[NetUidStorageIndex::ROOT]` at line 52, removes retired root gates, and preserves the root concentration cap. Its post-upgrade checks at line 126 require non-root row count preservation. `basket_trade.rs:66`, `basket_flush.rs:346` and `claim_root.rs:205` are the new trade, dividend deposit and mirrored allocation entry points. Root-only history must retain its old exact runtime identity; new current admission cannot reinterpret an old owner as 461.

## Adjacent SN gates and operational operands

After the main reviewed-artifact catalog/provenance update, three explicit conditions still need reviewed 461 membership: `crv4/validator_stake.go:60` (also used by schedule), `crv4/source_commitment.go:59` (strict offline and live source payload), and `sim-testnet/runtime_config_identity.go:18`. Preserve original config identity 455 while permitting its explicitly reviewed transition to current 461; do not rewrite signed history.

`deploy/testnet/public.yml` and `deploy/testnet/release.lock.yml` need the exact current identity. `scripts/check-runtime-metadata-artifacts.sh` pins artifact count/version/schema/source cases; `scripts/check-runtime-v454-source.sh` pins exact source versions/manifests. Their additions must preserve old manifests and exact identities. The metadata checker's live RPC is already LAN-only despite historical public provenance text.

Historical capture/replay and activation policies are catalog-derived (`sim-testnet/runtime_identity.go:42,195`; `validator/runtime_identity.go:42,54`). Append exact 461 and retain exact 460: a 461 owner can inherit reviewed predecessors, while a 460 owner cannot inherit future 461. Miner current-runtime constants alias the same catalog. No executable mainnet consumer exists in mainnet/mainnnet; those contain planning documents, and this review does not establish mainnet qualification.

The private r4 `observe-native-schedule.py:61` explicitly requires `(460,1,1)` and must receive the new explicit admitted tuple after qualification, while preserving LAN/context/chain checks. It was not edited or executed here. The old CLI's read-only release-lock renderer validates compiled static runtime identity (`release_lock_render.go:317,400`), so prior same-runtime renderer reuse is not automatically valid for a 461 lock.

## Exact changed intersection with prior 71-path manifest

- `pallets/subtensor/runtime-api/src/lib.rs`
- `pallets/subtensor/src/benchmarks/benchmarks.rs`
- `pallets/subtensor/src/benchmarks/helpers.rs`
- `pallets/subtensor/src/lib.rs`
- `pallets/subtensor/src/macros/dispatches.rs`
- `pallets/subtensor/src/macros/errors.rs`
- `pallets/subtensor/src/macros/hooks.rs`
- `pallets/subtensor/src/migrations/mod.rs`
- `pallets/subtensor/src/staking/claim_root.rs`
- `pallets/subtensor/src/staking/stake_utils.rs`
- `pallets/subtensor/src/subnets/weights.rs`
- `pallets/subtensor/src/tests/claim_root.rs`
- `pallets/subtensor/src/tests/migration.rs`
- `pallets/subtensor/src/tests/stake_into_basket.rs`
- `runtime/src/lib.rs`
- `runtime/src/proxy_filters/call_groups.rs`
- `runtime/src/proxy_filters/mod.rs`
- `runtime/tests/claim_root_weight.rs`

All other 53 manifest paths matched their existing SHA256 exactly. No tests or builds were run by this reviewer.
