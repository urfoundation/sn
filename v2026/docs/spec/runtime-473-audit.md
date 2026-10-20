# Runtime473 mainnet compatibility review

This is a source/artifact and consumed-interface review of the runtime that
Bittensor mainnet runs at the SN25 launch. The SHA-256 of this file's exact
committed bytes is the `runtime_review_hash` that a validator, treasury or fleet
approval may bind. The record is evidence for that approver. It approves no
genesis, finality checkpoint, signing, deployment or activation, and grants no
permission to submit a transaction. No transaction was signed or submitted while
preparing it. The [runtime470 review](runtime-470-audit.md) and its approvals
remain unchanged.

## Exact provenance

On 2026-10-07 the snow Finney archive `http://172.28.208.185:9944` (node
`4.0.0-dev-e18ca67f1a0`) reported genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`, chain
`Bittensor`, EVM chain ID 964 and `node-subtensor/473/1/1` at finalized block
9,233,529, `0x396606cfbca47c969b45ded222d6726da65acda8805b753bf98134aa233f7724`
(2026-10-07T20:28:24Z), and at the launch snapshot block 9,232,515,
`0xa097dbd563c578335a94e0f57e7f1a7f92edf8fc424e7544e7732bac8abecd58`. The snow
snapshot `/mnt/data/mainnet/work/finalized-snapshot.json` (5,900,560 bytes,
SHA-256 `0b8126d969821b220322c2eea80c24bb72db3eeb70588433b7396963e6f65657`,
`unapproved_observation`) carries the same code and metadata bytes. The archive
is our own node: its finality and storage replies are that node's assertion, not
a light-client proof.

The archive's `:code` history from the 470 review block 9,184,596 is: 470
through 9,197,055; spec 472 (code BLAKE2b-256
`0x43bc67be9df30636d7e948e7bdb1ed065f2fb92029458cc939abf89d76d8ada3`) from
9,197,056 (2026-10-02T18:53:48Z) through 9,217,263; 473 from 9,217,264
(2026-10-05T14:15:24Z). Block 9,217,265 is the first block executed by 473. The
472 artifact has no tag or published release and is not reviewed here; the
comparisons below span it.

The official [v473 release][release] (not a prerelease, published
2026-10-05T13:52:31Z by `github-actions[bot]`) and the lightweight tag `v473`
name exact source commit `f87cada631f81d11683e715a9f059f693992e64a` (tree
`1133504e496fb78a30645b9db37ac3d193ed9298`), the merge of PR #3208 with parents
`c004cebf` (tag `v471`) and `fe455992`. `git ls-remote` reports the same tag
target for `opentensor/subtensor` and `RaoFoundation/subtensor`; GitHub redirects
the former to the latter (repository 608683796). SN names this commit
`crv4.NativeOwnerSource473`. The v470 commit `923fd1fa` is an ancestor: the
[comparison][compare] is 71 commits ahead, none behind, 226 files. GitHub
reports the merge commit signature as verified; that is GitHub's assertion, and
no local key verification is claimed.

The source archive of f87cada6 was checked against GitHub's recursive tree: the
git object IDs recomputed from all 3,049 extracted blobs and symlinks match, with
no missing or extra path (the v470 archive likewise matches all 3,021).
[The source manifest](runtime-v473-source.sha256) pins 174 files: all 139 files
of the v470 manifest (40 changed), every changed or new runtime, pallet,
precompile and vendored-precompile path (18 more), and 17 unchanged sources that
SN's readers, validator and contracts rely on: timelocked commit reveal, epoch,
auto parent delegation, add stake, UID registration, mechanism, rate limits,
pallet configuration constants, the coldkey-swap guard, `StakeInfoRuntimeApi`,
Drand, Utility, the Alpha, Ed25519, Sr25519 and Metagraph precompiles, and
`rust-toolchain.toml`. Not pinned: `vendor/frontier/Cargo.lock` (the workspace
lockfile governs the build) and the BLS12-381 JSON test vectors. Running
`sha256sum -c` on the manifest from the root of a clean checkout of the commit
checks every entry.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Compressed runtime Wasm (`:code`, release `subtensor.wasm`) | 2,593,406 | `cbd9f5b4c72edd86af5c956ae21c66448818e227f4085ecb021acd89e533adfc` | `7773f5c0a6d6e9ea9ff347edcc491246eec08a5cf441d964ee96f40d7fa65a08` |
| Decompressed compact Wasm | 10,571,212 | `a9627e8d01cff48e28909e5c07148d1e8110926f2c477e8e6609fa9deeb93e4f` | `f16f7a28db5b494915fc086c3c92950ffbc5cad1b33717eab4ac8da7d50ed39c` |
| Metadata v14 (`state_getMetadata`) | 354,718 | `054f253ec4e57a441ef79cddb7a9c0d9cd98efffbb8384add69b31315013632a` | `a97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172` |
| Metadata v15 (`Metadata_metadata_at_version`, node-executed) | 397,907 | `1e4fbc1932ac50de6f86f7bc6a73b6acb59005c3872c72a8e41ffcdc2adef392` | `6f986cb3658bb881c17a145e462679a9ced02c12247834f757c60c4fc97c6f6c` |

**Code hash.** `:code` is the unhashed well-known key `0x3a636f6465`, so no
twox key prefix is involved. Its bytes, read with `state_getStorage` at both
blocks, hash under BLAKE2b-256 to the node's `state_getStorageHash(:code)` and to
the pinned code hash
`0x7773f5c0a6d6e9ea9ff347edcc491246eec08a5cf441d964ee96f40d7fa65a08`, and under
SHA-256 to the release asset's digest. They are byte-identical to the release
asset, to the snapshot's `runtime_code_hex` and to the code embedded in the
upgrade call below. Removing the 8-byte Substrate zstd prefix
`52bc537646db8e05` and decompressing yields exactly the compact artifact
recorded in the release's srtool digest.

**Metadata hash.** SN computes BLAKE2b-256 over the exact `state_getMetadata`
bytes without re-encoding (`crv4.DecodeRuntimeMetadata`). Running that function
on the captured bytes returns
`0xa97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172`. The pinned
SN metadata probe (`tools/runtime-metadata-probe`, locked SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, rebuilt with rustc 1.89.0) executed
the exact compressed Wasm's `Core_version` and `Metadata_metadata` without a
stateful host call. It reproduced `node-subtensor/473/1/1`, metadata v14 and
every size and digest in the first and third rows. As a negative control, the
470 metadata digest was rejected with the observed 473 digest. This is execution
of the exact Wasm, not a second RPC assertion.

**Release and CI records.** The five release assets match the SHA-256 digests
that GitHub's API records for them:

| Release asset | Bytes | SHA-256 |
| --- | ---: | --- |
| `subtensor.wasm` | 2,593,406 | `cbd9f5b4c72edd86af5c956ae21c66448818e227f4085ecb021acd89e533adfc` |
| `subtensor-digest.json` | 4,969 | `f2680412b29b3279c3ad393c9b3129c1d1ca8bbbd3fb791b6390dba7b982c3fb` |
| `upgrade-manifest.json` | 1,980 | `de5921aa17ba4ba3594f7efca1c84a354b99bf14806a25c5a68489df7f6c8aec` |
| `pending-release.json` | 579 | `61cade8e0daac3891914e21d249da4efca1dc15012ead6228e83abc5a8e3e255` |
| `proxy_proxy_blob.hex` | 5,186,916 | `9caa35431062220c848a30f0b836744ccdf77b8178a55036a73a155c4c528115` |

The release is not marked immutable; the identity above rests on the chain
bytes, not on the assets' mutability. The srtool digest names commit f87cada6,
srtool 0.18.3, `rustc 1.89.0 (29483883e 2025-08-04)`, profile `production`,
package `node-subtensor-runtime`, image `paritytech/srtool:1.89.0`, the
compressed and compact digests above, core version 473/1/1 and metadata v14. In
[release-train run 37157697423][run], a push of f87cada6 to `main`, job
111304450166, "Srtool build (once per train)", succeeded from 22:13:36Z to
22:24:31Z on 2026-10-03. The digest's build timestamps, 22:17:49Z and 22:22:52Z,
fall inside that job. The run's overall conclusion is `cancelled` because its
last job, an SDK publication, was cancelled. The job's artifact `runtime-473`
(ID 11286910569, a 2,594,683-byte ZIP, API digest
`sha256:d1223dc4f7c064934aea4fb50c418184bc1ea7edb7abf19df8176e5076898f3b`) was
not downloaded because the API requires authentication; it is cited as GitHub's
record only.

The published upgrade call decodes as `Proxy.proxy(real, None,
Sudo.sudo_unchecked_weight(System.set_code(code), ref_time 50,000,000,000,
proof_size 0))` with `real`
`0x4471816662ea3cfadc9868e5f083e26a3be6706b8d8dad7fbef565983afb3556`. Its
embedded code is the exact artifact, and BLAKE2b-256 of the call is the
manifest's call hash
`0xef25400edfa5d02cb8f92ba7a8e576d722e804fd5b73170c6c615ab5b4042d86`. The archive's
hash for proposal block 9,217,147 is the manifest's
`0x4978abdbbd0ab1b287f12a7e477c1e21f3062aafc4527f708a1cc02f9d6c2594`.

## Source-to-Wasm rebuild: not performed

**No reproducible source-to-Wasm rebuild was performed for this record.**
Nothing here establishes, independently of the upstream release, its srtool
digest, the CI job record and the upgrade call, that f87cada6 compiles to these
bytes. The 470 review ran the pinned srtool recipe. The only host available to
this review was a shared arm64 workstation. Its volume was 100% used with about
28 GB free and falling. Concurrent work held its load average between 17 and 25
on 10 cores. The upstream srtool image is `linux/amd64` only, so the build would
have run under Rosetta emulation for hours and needed an estimated 10 to 20 GB.
The amd64 archive host was available to this review read-only. A build risked
filling the shared disk and disturbing timing-sensitive work, so it was not
attempted.

The 470 result predicts the likely outcome but is not evidence for 473.
f87cada6 still locks `ahash` 0.8.12 with `const-random` 0.1.18 and
`const-random-macro` 0.1.16, and `wasmi_collections` 0.32.3, and neither the
tree nor the release recipe sets `CONST_RANDOM_SEED`. An independent build is
therefore expected to differ again in the compile-time hash-seed constants of
`wasmi_collections::hash::RandomStateImpl::default` ([source][wasmi-hash],
[seeds][ahash-seeds]); exact equality is not expected. Before binding this
record, an approver must either accept this artifact-identity basis without a
rebuild, or commission one on a dedicated amd64 host and compare the result
section by section and function by function. The upstream recipe is
`scripts/srtool/build-srtool-image.sh`, then `run-srtool.sh` with
`PACKAGE=node-subtensor-runtime`, `BUILD_OPTS=--features=metadata-hash` and
`PROFILE=production`.

## Interface delta from 470 to 473

The exact metadata at the 470 review block (354,056 bytes, BLAKE2b-256
`0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`, as recorded
there) was compared item by item with 473. The comparison covers every storage
entry (prefix, modifier, hashers, key and value types, default bytes), every
call, event and error (pallet index, variant index, field names and types),
every constant (type and value bytes), the signed-extension list and the
extrinsic type. It includes the full closure of reachable named types;
`RuntimeCall` and `RuntimeEvent` are compared through each pallet's own enum.
Each side has 1,578 items. 1,574 are identical; three were removed, three added
and one changed. All 28 pallets keep their indices. All 307 calls, 403 errors
and 305 earlier events are unchanged, as are 13 signed extensions under
extrinsic v4 and 135 of 136 constants. Metadata v15 adds the
runtime APIs. All 25 APIs and their 124 method signatures are identical, and
the declared API versions are byte-identical (`SubnetInfoRuntimeApi`
`0x8375104b299b74c5` remains version 2).

| Item | 473 change | Meaning |
| --- | --- | --- |
| constant `System.Version` | spec 470 to 473 | Version only; the API list bytes are identical. |
| storage `SubtensorModule.Alpha` | removed from metadata | Retired V1 share map. |
| storage `SubtensorModule.TotalHotkeyShares` | removed | Retired V1 share denominators. |
| storage `SubtensorModule.AlphaMapLastKey` | removed | Retired V1 iteration cursor. |
| storage `SubtensorModule.StakeMoveCooldownUntil` | added: `(NetUid Identity, AccountId Blake2_128Concat) -> u64`, default 0 | Hotkey-swap destination cooldown. |
| storage `SubtensorModule.NetworkRegistrationEscrow` | added (in 472): `u32 Identity -> (AccountId, TaoBalance)`, optional | Subnet-registration escrow. |
| event `SubtensorModule.NetworkRegistrationCancelled` | added (in 472) as index 153: `coldkey`, `hotkey`, `lock_id: u32`, `error: DispatchError` | Appended variant; no index moved. |

Metadata-driven readers can no longer name `Alpha` or `TotalHotkeyShares`. A
scan of SN's production Go string literals finds 247 metadata names covering
269 items. Its only hits among the changed items are textual: the word "Version"
in command text and "Alpha" in an error message. No SN code reads a changed
item. SN's native event walker and the GSRPC parser both decode from execution
metadata, so the appended event remains decodable.

Runtime-affecting source changes, beyond the version constant:

- **Alpha V1 retirement** ([PR #3200][alpha-v2]). `migrate_alpha_v2`, scheduled
  in block 9,217,265 and run in idle weight, converted legacy share rows to V2
  and made one dust sweep. It finished at block 9,218,572,
  `0xa3db7caa6b0d93d8efbe6d689c72b6cef7152503a0baeb5b2059fad2cb06997b`
  (2026-10-05T18:37:00Z). There
  `HasMigrationRun("migrate_alpha_v2_and_unstake_dust_v1")` became true, and
  its progress record reached phase 3: 109,871 legacy rows converted, 585,619
  V2 rows swept, 247,750 rows deleted, 20,791,632,892 rao refunded, 976,186 rao
  burned, nothing deferred or pending. Positions whose
  fee-free executable sale value was below 1,000 rao were deleted and their
  proceeds burned. Legacy positions worth 1,000 to 3,000,000 rao were unstaked
  to their coldkeys. Positions protected by locks or collateral were converted
  and kept. At 9,233,529 the retired `Alpha`, `TotalHotkeyShares` and
  `AlphaMapLastKey` prefixes hold no keys, and the storage-bloat and
  `migrate_cleanup_staking_hotkeys_v3` idle migrations are also complete. SN's
  treasury migration witness reads this marker; any activation parent at or
  after 9,218,572 carries it.
- **Staking index** (PR #3209). `StakingHotkeys` now keeps a hotkey while any
  alpha key remains for the pair, in either format and even if zero-valued, or
  while `BasketClaimed` is nonzero. Under 470 it was removed once no nonzero
  share remained. The small-nomination fallback and storage cleanup prune the
  index. The [receive-setup](../../mainnet/TREASURY-RECEIVE-SETUP.md) coldkey
  swap requires an empty destination `StakingHotkeys`, so it must still read
  that list rather than infer it from balances.
- **Hotkey-swap cooldown** ([PR #3199][swap-hotkey]). A hotkey swap that moves
  nonzero alpha writes `StakeMoveCooldownUntil(netuid, new_hotkey)` =
  block + `HotkeySwapOnSubnetInterval` (7,200 blocks). Another hotkey swap out
  of that position on that subnet fails with
  `HotKeySwapOnSubnetIntervalNotPassed` until then. Stake moves, transfers,
  withdrawals and coldkey swaps do not read it.
- **Subnet registration** ([source][subnet]; its storage item and event already
  appear in the on-chain 472 metadata). `register_network` escrows the lock in a
  per-entry `rges` sub-account of the pallet account. Queue settlement handles
  the first entry per idle call. Terminal failures refund the escrow and emit
  `NetworkRegistrationCancelled`. The new pool is booked with the TAO actually
  paid, not a configured floor.
- **Basket trading** (PR #3198, [extension][extension]). The minimum basket
  trade is the larger of `DefaultMinStake` and 0.5 TAO. `MAX_BASKET_SWAP_LEGS`
  falls from 128 to 64; that bound is not in metadata.
  `SubtensorTransactionExtension` now validates `swap_basket` and
  `swap_basket_many` legs of a signed call, including legs nested in Utility
  `batch`, `batch_all`, `force_batch`, `as_derivative` and `if_else` and in
  Proxy `proxy` and `proxy_announced`, and adds their validation weight. Calls
  without basket legs gain no weight. A Proxy `real` that the walk reaches but
  cannot look up now fails validation with `NonAssociatedColdKey`. Multisig
  wrappers are not walked.
- **Root claims** (PR #3201). `claim_root_with_hotkey` admits 1,025 work units
  only when genesis is the Finney testnet's; mainnet keeps 129. The root claimant
  index is populated from V2 shares only.
- **EVM.** Final EIP-2537 BLS12-381 precompiles occupy `0x0b` to `0x11`
  ([PR #3205][precompiles]); every earlier address is unchanged. The staking
  precompile's `getTotalColdkeyStake` gas accounting reads the retired map
  through a storage alias with the same key.
- **Runtime crate.** `spec_version` becomes 473 and the migrations tuple gains
  `migrate_alpha_v2`. No pallet configuration, fee schedule, benchmarked weight
  file, proxy filter or EVM configuration changed; the `staking_fee.rs` change
  is test-only. `Cargo.lock` adds only the BLS12-381 precompile crate and
  `hex`/`serde_json` for the precompiles crate. Polkadot SDK `cacb4310` and
  every other dependency are unchanged.

The remaining changes touch CI, documentation, the SDK, the node and tests.

| Consumer | Interfaces at 473 (pallet index, call/event index) | Result |
| --- | --- | --- |
| Validator weights and CRv4 timelocked commits | SubtensorModule (7) `commit_timelocked_weights` 113, `commit_timelocked_mechanism_weights` 118, `set_weights` 0, `set_mechanism_weights` 119, `commit_weights` 96, `reveal_weights` 97; events `TimelockedWeightsCommitted` 108, `WeightsSet` 5; Commitments (18) `set_commitment` 0; Utility (11) `batch_all` 2; AdminUtils (19) `sudo_set_commit_reveal_weights_enabled` 49; `CommitRevealWeightsEnabled`, `RevealPeriodEpochs`, `WeightsVersionKey`, `TimelockedWeightCommits`, `Weights`, `LastUpdate`, `MaxWeightsLimit`, `MinAllowedWeights`; `SubnetInfoRuntimeApi_get_selective_metagraph` | Unchanged. `subnets/weights.rs`, `guards/check_weights.rs`, `coinbase/reveal_commits.rs`, `epoch/run_epoch.rs`, `epoch/math.rs`, `coinbase/run_coinbase.rs`, `rpc_info/metagraph.rs`, `utils/rate_limiting.rs`, Drand and Commitments are byte-identical. The commitment batch passes the new nested-basket walk unaffected. |
| Children, parents and auto parent delegation | `set_children` 67, `set_childkey_take` 75, `set_auto_parent_delegation_enabled` 135; events `SetChildrenScheduled` 76, `SetChildren` 77, `AutoParentDelegationEnabledSet` 131; `ChildKeys`, `ParentKeys`, `PendingChildKeys`, `ChildkeyTake`, `AutoParentDelegationEnabled` | Unchanged. `staking/set_children.rs`, `staking/auto_parent.rs`, `coinbase/root.rs` and `subnets/uids.rs` are byte-identical. Only `swap_hotkey`, which also moves child and parent links, gained the cooldown. |
| Registration and burn | `register_limit` 134, `burned_register` 7, `root_register` 62; event `NeuronRegistered` 7; `Burn`, `MinBurn`, `MaxBurn`, `ImmunityPeriod`, `BlockAtRegistration`, `SubnetworkN`, `Keys`, `Uids` | Unchanged; `subnets/registration.rs` and `subnets/uids.rs` are byte-identical. Only subnet creation (`register_network` 59) changed, which SN does not call. |
| Staking | `add_stake` 2, `add_stake_limit` 88, `remove_stake` 3, `remove_stake_limit` 89, `move_stake` 85, `transfer_stake` 86, `swap_stake` 87, `transfer_stake_and_hotkey` 143; events `StakeAdded` 2, `StakeRemoved` 3, `StakeMoved` 4, `StakeTransferred` 92; `TotalHotkeyAlpha`, `AlphaV2`, `TotalHotkeySharesV2`, `StakingHotkeys`; `StakeInfoRuntimeApi` | Shapes unchanged. Behavior changes are limited to the completed V1 retirement and dust sweep and the `StakingHotkeys` retention rule. `staking/add_stake.rs`, `staking/move_stake.rs` and `rpc_info/stake_info.rs` are byte-identical. |
| Owner hotkeys and owner recycle | `SubnetOwner`, `SubnetOwnerHotkey`, `OwnedHotkeys`, `RecycleOrBurn` (`Burn` 0, `Recycle` 1), `MaxWeightsLimit`, `MechanismCountCurrent`, `SubnetEpochIndex`, `Keys`, `Uids`, `BlockAtRegistration` | Unchanged. `utils/misc.rs`, including `get_owner_hotkeys` and `get_max_weight_limit` (still `u16::MAX`), is byte-identical. |
| Root | `root_register` 62, `claim_root` 121, `swap_basket` 150, `swap_basket_many` 151; storage read by the passive root service | Shapes unchanged. Behavior differs only as listed under basket trading and root claims; the passive observer submits nothing. |
| EVM precompiles used by the contracts | `0x402` Ed25519 `verify`; `0x403` Sr25519 `verify`; `0x800` `transfer`; `0x802` `getColdkey`; `0x804` `getUid`, `registerLimit`, `burnedRegister`; `0x805` `getStake`, `moveStake`, `transferStake`; `0x808` `getAlphaPrice` | Unchanged. `ed25519.rs`, `sr25519.rs`, `balance_transfer.rs`, `metagraph.rs`, `neuron.rs`, `alpha.rs`, `address_mapping.rs` and `extensions.rs` are byte-identical. `staking.rs` differs only in the retired-map alias and a comment, and the pallet code behind `getStake`, `moveStake` and `transferStake` is unchanged. The BLS12-381 addresses are unused. EVM chain ID 964. |
| Multisig | Multisig (13) `as_multi_threshold_1` 0, `as_multi` 1, `approve_as_multi` 2, `cancel_as_multi` 3; events 0 to 3; deposit constants | Unchanged: `pallet-multisig` 41.0.0 from Polkadot SDK `cacb4310` and its runtime configuration. |
| Proxy | Proxy (16) `proxy` 0, `add_proxy` 1, `remove_proxy` 2, `proxy_announced` 9; event `ProxyExecuted` 0; `ProxyType` | Unchanged: `pallets/proxy`, `common/src/proxy.rs` and `runtime/src/proxy_filters` are byte-identical. A nested `real` lookup failure now fails validation, as above. |
| Transaction format and signing | Extrinsic v4 with `CheckNonZeroSender`, `CheckSpecVersion`, `CheckTxVersion`, `CheckGenesis`, `CheckMortality`, `CheckNonce`, `CheckWeight`, `ChargeTransactionPayment`, `SudoTransactionExtension`, `CheckShieldedTxValidity`, `SubtensorTransactionExtension`, `DrandPriority`, `CheckMetadataHash`; transaction version 1 | Unchanged. The `CheckMetadataHash` digest covers the spec version, so a mode-1 signer must use 473's metadata. |

SN's own checks pass on the exact 473 metadata. They ran on this repository at
`86c3ff7d`, with sibling modules from the pinned launch source lock (content hash
`0x4b7443de96d39fe0b0f94a4da30ae8dbe756026a4a32c43acbe6567ca8abe169`):

- `crv4.ValidateProvisionalRuntimeMetadata` compares the CRv4 producer's
  consumed storage, calls, events and signed extensions with the reviewed 467
  baseline. It reports them unchanged for 470, 472 and 473.
- `TestNativeOwnerRetainedRuntimeMetadataCapabilities` ran with
  `SN_NATIVE_OWNER_METADATA_FILE` set to the 473 metadata and
  `SN_NATIVE_OWNER_METADATA_BLAKE2B256` set to its pinned digest. It passed,
  covering the Ed25519 native signing profile, the owner recycle call, passive
  root storage, and treasury `register_limit`, `remove_stake_limit` and
  `transfer_keep_alive` under `as_multi`, `approve_as_multi` and
  `cancel_as_multi`. `TestRootCapabilitiesRuntimeSuccessorRemainsPlanningOnly`
  also passed.

## Limits and assessment

This record does not establish:

- a source-to-Wasm rebuild, exact or partial;
- independent finality: genesis, blocks and storage are snow archive replies;
- the economic semantics of the changed paths beyond the source read here,
  including the dust sweep's valuations;
- the intermediate 472 artifact;
- metadata v15 beyond the node's execution;
- GitHub's commit verification or CI artifact, which are cited as GitHub's
  records.

Raw captures were made locally and are not retained by this record. Every input
is public or readable from an archive node at the listed blocks, and the listed
sizes and digests identify each one.

**Assessment.** The mainnet runtime at the SN25 launch is byte-identical to the
official v473 release asset, which the release and GitHub's CI record attribute
to an srtool build of exact source commit f87cada6. The upgrade call that
installed it carries the same bytes, and executing that Wasm reproduces the
pinned metadata hash. From 470 to 473 the interfaces consumed by the validator,
the contracts and the launch tooling are unchanged. The behavior changes that
touch SN's paths are the completed Alpha V1 retirement, the `StakingHotkeys`
retention rule, the hotkey-swap cooldown and the nested basket validation. None
changes the encoding of a call, event or storage item that SN reads or writes;
the `StakingHotkeys` rule changes when entries disappear, not their format. An
approver who binds this record's SHA-256 accepts this artifact-identity basis
without a source-to-Wasm rebuild, or must first obtain one. Independent
finality, signing custody and activation remain separate gates.

[release]: https://github.com/RaoFoundation/subtensor/releases/tag/v473
[compare]: https://github.com/RaoFoundation/subtensor/compare/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d...f87cada631f81d11683e715a9f059f693992e64a
[run]: https://github.com/RaoFoundation/subtensor/actions/runs/37157697423
[alpha-v2]: https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/migrations/migrate_alpha_v2.rs
[swap-hotkey]: https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/swap/swap_hotkey.rs
[subnet]: https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/subnets/subnet.rs
[extension]: https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/extensions/subtensor.rs
[precompiles]: https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/precompiles/src/lib.rs
[wasmi-hash]: https://github.com/wasmi-labs/wasmi/blob/3c42a099f031fb84f8c0acdb6ea756c8bbd24a0a/crates/collections/src/hash.rs
[ahash-seeds]: https://github.com/tkaitchuck/aHash/blob/9aa1ba20f05ed582eda04ea625d5658c92195a57/src/random_state.rs
