# Runtime475 mainnet compatibility review

This is a source/artifact and consumed-interface review of runtime 475, which
Bittensor mainnet installed on 2026-10-07, shortly before the SN25 launch. The
SHA-256 of this file's exact committed bytes is the `runtime_review_hash` that a
validator, treasury or fleet approval may bind. The record is evidence for that
approver. It approves no genesis, finality checkpoint, signing, deployment or
activation, and grants no permission to submit a transaction. No transaction
was signed or submitted while preparing it. The
[runtime473 review](runtime-473-audit.md) and its approvals remain unchanged;
they do not cover 475.

## Exact provenance

On 2026-10-08 the snow Finney archive `http://172.28.208.185:9944` (node
`4.0.0-dev-e18ca67f1a0`) reported genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`, chain
`Bittensor`, EVM chain ID 964 and `node-subtensor/475/1/1` at finalized block
9,234,811, `0x5b4bb52cc2b35b226782dd0490e94d35ab06e888732ba9db72faef6ff89f1782`
(2026-10-08T00:44:48Z). The archive is our own node: its finality and storage
replies are that node's assertion, not a light-client proof.

From block 9,233,529, the finalized block of the 473 review, the archive's
`:code` changed once. It held 473 through 9,233,780,
`0x72c14c3edc8046f6523735c343a965d34d979f55b8aa8b2072450e173c39f84d`
(2026-10-07T21:18:36Z), and 475 from 9,233,781,
`0x69df2a41585dba58729cd694aabc0476f040db684dadea6908e7b2aaa241b48c`
(2026-10-07T21:18:48Z). Block 9,233,782,
`0x0e0f91d9762b74f5e1d68f41cbef0298e9e23be59fdf36a2ac98f3d6ca4a4d1c`
(21:19:00Z), is the first block executed by 475. No 474 artifact reached
mainnet; spec 474 appears only as an intermediate bump on the PR #3206 branch
(`24e6b4da`).

The official [v475 release][release] (not a prerelease, created
2026-10-07T16:08:15Z and published 2026-10-07T20:40:07Z by
`github-actions[bot]`) and the lightweight tag `v475` name exact source commit
`d1718c99c34cf96abbf2bf09c0e9e48b945c76f5` (tree
`289da5ddb3d20dcbdaaf25661b91070de5b7c94b`), the merge of PR #3214 with parents
`faaa14bb` (the merge of PR #3213) and `ba274b9f`. `git ls-remote` reports the
same tag target for `opentensor/subtensor` and `RaoFoundation/subtensor`;
GitHub redirects the former to the latter (repository 608683796). SN names this
commit `crv4.NativeOwnerSource475`. The v473 commit `f87cada6` is an ancestor:
the [comparison][compare] is 27 commits ahead, none behind, 263 files. The
release notes list PR #3206 (Null consensus, precise emissions and PoW
registration), PR #3213 (an SDK build fix for aarch64 Linux) and PR #3214 (a
sudo-adjustable basket minimum trade). GitHub reports the merge commit
signature as verified; that is GitHub's assertion, and no local key
verification is claimed.

The source archive of d1718c99 (60,762,291 bytes, SHA-256
`564bf27a928ab6db3b406ef5ee25c9b231d871dbd20341f115669e52a46fe970`) was
checked against GitHub's recursive tree: the git object IDs recomputed from all
3,071 extracted blobs and symlinks match, with no missing or extra path. The
f87cada6 archive used for the comparison again matches all 3,049 entries of
its tree. Of the 263 changed files, 52 are runtime, pallet, precompile or
primitive paths and one is `Cargo.lock`; the rest are documentation (153), the
SDK (44), the website (6) and repository tooling (7).
[The source manifest](runtime-v475-source.sha256) pins 185 files: all 174 files
of the v473 manifest (46 changed), the 9 changed or new runtime, pallet and
precompile paths outside it (`rpc_info/subnet_info.rs`, the subnet precompile
`precompiles/src/subnet.rs`, the four Solidity interface files of the neuron and
subnet precompiles, and three pallet test files, including
`tests/null_consensus.rs`), and the 2 unchanged take sources that SN's
validator take tool relies on (`staking/decrease_take.rs` and
`staking/increase_take.rs`). Every changed runtime-side path is pinned. Not
pinned, as for v473: `vendor/frontier/Cargo.lock` (the workspace lockfile
governs the build), the BLS12-381 JSON test vectors, and the documentation, SDK,
website and tooling paths that do not build the runtime. Running `sha256sum -c`
on the manifest from the root of a clean checkout of the commit checks every
entry; on the f87cada6 tree it reports the 54 changed digests and the one new
test file as absent.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Compressed runtime Wasm (`:code`, release `subtensor.wasm`) | 2,632,482 | `00ef6bf41d93e8f2763511fe9f7b77cf30df141e4c0dbf2f4a160bd902f0ef1b` | `557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0` |
| Decompressed compact Wasm | 10,713,692 | `4ec042e94065036334235c1216d50f9e46bed6f84fa7e130e9c41e8c82a1c55b` | `bb3cffa58de89eb1d75f831abe2077a624cc6a0b73265c867ba39dad0e4e7764` |
| Metadata v14 (`state_getMetadata`) | 357,468 | `e181ddacd13d1050e82a2afea8e58ba5d3fc84504fa92d2884c91df8c9dd8020` | `983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff` |
| Metadata v15 (`Metadata_metadata_at_version`, node-executed) | 400,657 | `7a4fc116d6d0c7ae83e1319cb22dfdb8b426c5eb8c34c95545d43eda91f5f287` | `c403651a3e0dc781e7394809007e67584bdcfe6a43ae1bf64c2925d33f7f29d9` |

**Code hash.** `:code` is the unhashed well-known key `0x3a636f6465`, so no
twox key prefix is involved. Its bytes, read with `state_getStorage` at blocks
9,233,781, 9,234,802 and 9,234,811, hash under BLAKE2b-256 to the node's
`state_getStorageHash(:code)` and to the pinned code hash
`0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0`, and under
SHA-256 to the release asset's digest. They are byte-identical to the release
asset and to the code embedded in the upgrade call below. Removing the 8-byte
Substrate zstd prefix `52bc537646db8e05` and decompressing yields exactly the
compact artifact recorded in the release's srtool digest.

**Metadata hash.** SN computes BLAKE2b-256 over the exact `state_getMetadata`
bytes without re-encoding (`crv4.DecodeRuntimeMetadata`). Running that function
on the captured bytes returns
`0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff`. The
archive returned the same bytes at blocks 9,233,781 and 9,234,811, and the test
fixture `crv4/testdata/runtime475-metadata.scale.gz.base64` carries them. The
pinned SN metadata probe (`tools/runtime-metadata-probe`, locked SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, built with rustc 1.89.0, executable
SHA-256 `36be2ba7937dd092388fc44dcf31d6e32d5051635276cb84736e7c1fcbfa552c`)
executed the exact compressed Wasm's `Core_version` and `Metadata_metadata`
without a stateful host call. It reproduced `node-subtensor/475/1/1`, metadata
v14 and every size and digest in the first and third rows. As a negative
control, the 473 metadata expectations were rejected with the observed 475
size. This is execution of the exact Wasm, not a second RPC assertion.

**Release and CI records.** The five release assets match the SHA-256 digests
that GitHub's API records for them:

| Release asset | Bytes | SHA-256 |
| --- | ---: | --- |
| `subtensor.wasm` | 2,632,482 | `00ef6bf41d93e8f2763511fe9f7b77cf30df141e4c0dbf2f4a160bd902f0ef1b` |
| `subtensor-digest.json` | 4,969 | `32cd79ef9955ed5daf3a16ff040d4cfc4c6b9e9ab1bd3b971cd81a03d6360334` |
| `upgrade-manifest.json` | 1,980 | `3a1af66a5f92eb79e58b16471c4c305c0824fead02aa34dd7a869760c3326ef3` |
| `pending-release.json` | 579 | `5640613a8071b2d2316bc5a66c96ba787cc6231cc42c701d357aa577557579f2` |
| `proxy_proxy_blob.hex` | 5,265,068 | `7c89616e5f5ad25c075ddf15dd7f40cae4da323937b8a13d6b081149b813bf1d` |

GitHub records all five uploads at 2026-10-07T21:30:46Z, twelve minutes after
the code was set on chain, so the attached assets postdate the upgrade; their
bytes still match the chain, the upgrade call and the CI digest. The release is
not marked immutable; the identity above rests on the chain bytes, not on the
assets' mutability. The srtool digest names commit d1718c99, srtool 0.18.3,
`rustc 1.89.0 (29483883e 2025-08-04)`, profile `production`, package
`node-subtensor-runtime`, image `paritytech/srtool:1.89.0`, the compressed and
compact digests above, core version 475/1/1 and metadata v14. In
[release-train run 37649682333][run], a push of d1718c99 to `main` with
conclusion `success`, job 112956943806, "Srtool build (once per train)",
succeeded from 18:46:34Z to 18:55:53Z on 2026-10-07. The digest's build
timestamps, 18:50:15Z, 18:54:26Z and 18:55:20Z, fall inside that job. The same
run deployed devnet and testnet and ran job 113004320770, "Propose mainnet
upgrade (multisig)", from 20:37:02Z to 20:40:11Z. The job's artifact
`runtime-475` (ID 11505146349, a 2,634,079-byte ZIP, API digest
`sha256:ea3e2381b317acd965d1b0055ffa7b0c57c2d23f5c575211978a2af576ed00cf`) was
not downloaded because the API requires authentication; it is cited as
GitHub's record only.

The published upgrade call decodes as `Proxy.proxy(real, None,
Sudo.sudo_unchecked_weight(System.set_code(code), ref_time 50,000,000,000,
proof_size 0))` with `real`
`0x4471816662ea3cfadc9868e5f083e26a3be6706b8d8dad7fbef565983afb3556`. Its
embedded code is the exact artifact, and BLAKE2b-256 of the call is the
manifest's call hash
`0x873147963c996988f0ef85fb930050c75f19e9b468578e10624ed7ce1439d26c`. The
archive's hash for proposal block 9,233,585 (2026-10-07T20:39:36Z) is the
manifest's `0xd2a90a47ebdaaca6afa54d698e047a7c0a968cee94b0ae27ea8bcde53fcf23a2`;
the code was set 196 blocks later, in block 9,233,781.

## Source-to-Wasm rebuild: not performed

**No reproducible source-to-Wasm rebuild was performed for this record.**
Nothing here establishes, independently of the upstream release, its srtool
digest, the CI job record and the upgrade call, that d1718c99 compiles to these
bytes. The only host available to this review was the same shared arm64
workstation as for 473. Its volume was 99% used with about 85 GB free, and
concurrent work held its load average between about 19 and 43 on 10 cores. The
upstream srtool image is `linux/amd64` only, so the build would have run under
emulation for hours and needed an estimated 10 to 20 GB. A build risked filling
the shared disk and disturbing timing-sensitive work, so it was not attempted.

The 470 result predicts the likely outcome but is not evidence for 475. The
runtime dependency graph is unchanged: the only `Cargo.lock` change adds
`libloading` to the SDK crate `bittensor-core`. d1718c99 still locks `ahash`
0.8.12 with `const-random` 0.1.18 and `const-random-macro` 0.1.16, and
`wasmi_collections` 0.32.3, and nothing in the tree, including the unchanged
`scripts/srtool` recipe and release workflow, sets `CONST_RANDOM_SEED`. An
independent build is therefore expected to differ again in the compile-time
hash-seed constants of `wasmi_collections::hash::RandomStateImpl::default`
([source][wasmi-hash], [seeds][ahash-seeds]); exact equality is not expected.
Before binding this record, an approver must either accept this
artifact-identity basis without a rebuild, or commission one on a dedicated
amd64 host and compare the result section by section and function by function.
The upstream recipe is `scripts/srtool/build-srtool-image.sh`, then
`run-srtool.sh` with `PACKAGE=node-subtensor-runtime`,
`BUILD_OPTS=--features=metadata-hash` and `PROFILE=production`.

## Interface delta from 473 to 475

The exact 473 metadata at block 9,233,780, the last 473 block (354,718 bytes,
BLAKE2b-256 `0xa97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172`,
as pinned by the 473 review), was compared item by item with 475 using the same
structural comparison as that review. It covers every storage entry (prefix,
modifier, hashers, key and value types, default bytes), every call, event and
error (pallet index, variant index, field names and types), every constant
(type and value bytes), the signed-extension list and the extrinsic type,
including the full closure of reachable named types; `RuntimeCall` and
`RuntimeEvent` are compared through each pallet's own enum. 473 has 1,578 items
and 475 has 1,593. 1,575 are identical; none was removed, 15 were added and 3
changed. All 28 pallets keep their indices. All 307 earlier calls, 403 earlier
errors and 306 earlier events are unchanged, as are 13 signed extensions under
extrinsic v4, 135 of 136 constants and 394 of 396 earlier storage items.
Metadata v15 adds the runtime APIs. All 25 APIs and their 124 method signatures
are identical, and the declared API versions are byte-identical
(`SubnetInfoRuntimeApi` `0x8375104b299b74c5` remains version 2).

| Item | 475 change | Meaning |
| --- | --- | --- |
| constant `System.Version` | spec 473 to 475 | Version only; the API list bytes are identical. |
| storage `SubtensorModule.NetworkPowRegistrationAllowed` | default `01` to `00` | No subnet has a row, so every subnet now reads PoW registration as disabled. |
| storage `SubtensorModule.LastRateLimitedBlock` | key's nested `Hyperparameter` enum appends `EpochConsensus` 35 and `BurnRegistrationAllowed` 36 | `RateLimitKey` variants, `RecycleOrBurn` 24 and the value are unchanged. |
| call `SubtensorModule.pow_register` | added as 152: `netuid`, `work_block: u64`, `nonce: u64`, `work: [u8; 32]`, `hotkey` | Owner-enabled, fee-free PoW registration. |
| call `AdminUtils.sudo_set_epoch_consensus` | added as 111: `netuid`, `mode: EpochConsensus` (`Yuma` 0, `Null` 1) | Subnet owner or root selects the epoch algorithm. |
| call `AdminUtils.sudo_trim_null_uids_batch` | added as 112: `netuid`, `target: u16` | Batched UID pruning on Null subnets. |
| call `AdminUtils.sudo_set_basket_min_trade_tao` | added as 113: `min_trade_rao: u64` | Root sets the basket minimum trade. |
| storage `SubtensorModule.SubnetEpochConsensus` | added: `NetUid Identity -> EpochConsensus`, default `Yuma` | No subnet has a row at block 9,234,811. |
| storage `LastYumaStepBlock`, `NullPruningTarget`, `SavedYumaMaxAllowedValidators` | added: `NetUid Identity ->` `u64`, `u16`, `u16`, optional | Consensus-mode bookkeeping; every Yuma epoch writes `LastYumaStepBlock`. |
| storage `LastPowRegistrationBlock` | added: `AccountId Blake2_128Concat -> u64`, optional | Per-hotkey PoW replay watermark. |
| storage `BasketMinTradeTao` | added: `u64`, default 500,000,000 rao | Root-set; 5,000,000 rao on chain at block 9,234,811. |
| events `SubtensorModule.EpochConsensusSet` 154 and `NullUidsPruningProgress` 155; `AdminUtils.BasketMinTradeTaoSet` 14 | appended | No earlier index moved. |
| errors `SubtensorModule.CommitPayloadTooLarge` 173 and `CommitQueueFull` 174 | appended | Mode-specific timelocked-commit admission. |

A scan of SN's production Go string literals finds 246 metadata names covering
268 items. Its hits among the changed items are `LastRateLimitedBlock` (the
owner recycle profile and the validator take tool), `NetworkPowRegistrationAllowed`
(the subnet census, preview and discovery) and the word "Version" in command
text. The recycle profile reads only the `OwnerHyperparamUpdate(1)` key with
`RecycleOrBurn(24)`, and the take tool only `LastTxBlockDelegateTake(5)`; both
keys encode as before. The census takes the registration-flag default from
authenticated metadata, so it now reports `pow_registration_allowed: false` for
SN25, which has no row. SN's native event walker and the GSRPC parser both
decode from execution metadata, so the appended events remain decodable.

Runtime-affecting source changes, beyond the version constant:

- **Null consensus** (PR #3206; [`subnets/mechanism.rs`][mechanism],
  `epoch/run_epoch.rs`, `coinbase/run_coinbase.rs`, `staking/stake_utils.rs`,
  `primitives/share-pool`). A subnet's owner or root may switch it to `Null`
  with AdminUtils call 111 while its admin window is open; owners are
  rate-limited per subnet, and the root subnet cannot be switched. A switch
  requires no pending timelocked commit on any of the 16 mechanism indices and a
  UID count within the mode's budget (the Yuma default UID limit, or 2,500 for
  Null). Under Null the largest-stake UID (ties to the lowest UID) is the sole
  validator and the only permit holder (`MaxAllowedValidators` is forced to 1).
  Only its weight row is loaded; miners are paid in proportion to that row
  without normalization or consensus clipping, dividends go to every staked UID
  in proportion to stake, consensus and trust are zero, and bonds freeze. Only
  the permit holder may set, commit or reveal weights. Each timelocked payload
  may hold 32 KiB divided by the mechanism count, and each epoch's queue at most
  64 KiB and 64 entries, evicting by stake priority. Switching back to Yuma
  restores the saved validator limit. No migration switches a subnet.
- **Yuma path.** For a subnet that stays on Yuma, the epoch's stake, permits,
  consensus clipping, bonds, incentive, dividends, emission split, owner cut and
  recycle or burn of owner-hotkey incentive compute as in 473; the Yuma epoch is
  the 473 code under a mode branch. The other differences on that path are
  bookkeeping. The bond cutoff now reads `LastYumaStepBlock + 1`, which each
  Yuma epoch writes with the same block as `LastMechansimStepBlock` (SN25 reads
  9,234,802 in both). The three emission pools are passed separately and
  re-summed. Owner and collateral credits go through new functions whose fast
  paths run only under Null, and unchanged values are no longer rewritten. One
  timing change can reach a Yuma subnet: when a non-root Null subnet is due in a
  block, only one epoch runs in that block and other due subnets are deferred
  one block (`EpochDeferred`), instead of up to the configured
  `MaxEpochsPerBlock`. SN's epoch schedule model does not model per-block
  deferral at all, and no Null subnet exists.
- **Timelocked commits** ([`subnets/weights.rs`][weights]).
  `MAX_CRV3_COMMIT_SIZE_BYTES`, the decode bound of the commit argument, grows
  from 5,000 bytes to 32 KiB, and a Yuma subnet now enforces the 5,000-byte
  `YUMA_COMMIT_SIZE_BYTES` at dispatch with `CommitPayloadTooLarge`. An
  oversized Yuma commit therefore decodes and fails in dispatch instead of
  failing to decode. The declared weights of the timelocked-commit, set and
  reveal calls add one read, and those calls remain fee-free.
  `reveal_commits.rs` only iterates keys instead of entries.
- **Registration** ([`subnets/registration.rs`][registration], AdminUtils).
  `register_limit`, `burned_register`, `register` and `root_register` keep
  their dispatch and source: `do_register_limit`, `register_neuron`,
  `do_root_register`, `get_root_neuron_to_prune`, `append_neuron` and
  `replace_neuron` are byte-identical, and the burn decay and bump are
  refactored with the same clamping. `pow_register` admits a hotkey without
  burn, collateral or fee when the owner has enabled PoW registration; its proof
  binds the subnet, a block hash at most five blocks old, the hotkey, the
  signing coldkey and a nonce, PoW difficulty decays every block on non-root
  subnets, and `LastPowRegistrationBlock` stops replay. AdminUtils call 19 now
  lets the subnet owner, not only root, toggle burned registration on a
  non-root subnet, and call 20, which always failed in 473, now toggles PoW
  registration; neither may leave both disabled. The owner-protected UIDs that
  pruning skips are now found by scanning the subnet's UIDs for hotkeys whose
  `Owner` is the owner coldkey, instead of walking that coldkey's
  `OwnedHotkeys`. The sets agree while those maps agree; SN25's set is {UID 1}
  under both rules at block 9,235,028. Pruning order is unchanged.
- **Children and auto parent delegation** ([`staking/set_children.rs`][children],
  commit `786d31bd`). Activating a pending child list now refuses
  (`TooManyParents`) to add a parent edge to a child that already has 100
  parents on that subnet, unless the edge exists. Shrinking or rewriting
  existing lists still applies, and nothing is truncated. Root validators'
  automatic parenting of owner hotkeys uses the same path and silently skips
  such an edge. `set_children`, its pending schedule and `set_childkey_take`
  are otherwise unchanged, and `auto_parent.rs` is byte-identical. SN25's owner
  hotkey (UID 1) has one parent.
- **Owner trim** ([`subnets/uids.rs`][uids], AdminUtils call 78).
  `sudo_trim_to_max_allowed_uids` is now transactional and refuses with
  `InvalidValue` when it cannot remove enough non-immune UIDs; 473 recorded the
  new UID count anyway. `sudo_set_max_allowed_uids` is bounded by the mode's
  UID budget, which for Yuma is the same default limit as before.
- **Basket trading** (PR #3214). The minimum basket trade comes from the
  root-set `BasketMinTradeTao`, still bounded below by `DefaultMinStake`.
- **Transaction validation.** `SubtensorTransactionExtension` now verifies a
  directly submitted `pow_register` proof before pool admission (longevity of at
  most five blocks, one `provides` tag per hotkey and work block) and adds its
  weight. The weight-call guard requires a validator permit only on Null
  subnets. Every other call, including the 473 nested basket walk, validates as
  before, and the signed-extension list is unchanged.
- **Proxy filters** ([`runtime/src/proxy_filters`][proxy-filters]). Legacy
  `register` and `register_limit` moved from the PoW-registration group to the
  burned-registration group, so `NonFungible` and `NonCritical` proxies no longer
  pass them while `Registration` still does; `pow_register` joins the PoW group.
  `ProxyType`, `pallets/proxy` and `common/src/proxy.rs` are unchanged.
- **EVM.** The neuron precompile (`0x804`, [`neuron.rs`][neuron]) adds
  `setMechanismWeightsV2` and `commitTimelockedMechanismWeightsV2` for
  Null-sized rows. Its 46 earlier selectors, including `getUid`, `registerLimit`
  and `burnedRegister`, are unchanged, and the earlier commit selectors keep
  their 5,000-byte bound. The subnet precompile (`0x803`), which SN does not
  use, adds Null pruning selectors. Every other precompile source and the
  precompile address list are unchanged.
- **Runtime crate and upgrade.** `spec_version` becomes 475. The migrations
  tuple and the pallet's migration hooks are unchanged, so the upgrade ran no
  new migration: only the total-issuance cleanup that runs on every upgrade, a
  retryable share-pool reconcile, and guarded one-shots, including the
  completed Alpha V2 migration. The block-step hook reserves the larger of the
  Null and Yuma weights, which are equal in the shipped weight file. Declared
  weights rise for `swap_basket` (one read), `sudo_set_mechanism_count` (17
  reads) and `sudo_trim_to_max_allowed_uids` (one read), and the runtime's fee
  baseline records those three changes. Polkadot SDK `cacb4310`, every runtime
  dependency, the pallet configuration, the fee schedule and the EVM
  configuration are unchanged.

The remaining changes touch documentation, the SDK, the website and repository
tooling.

| Consumer | Interfaces at 475 (pallet index, call/event index) | Result |
| --- | --- | --- |
| Validator weights and CRv4 timelocked commits | SubtensorModule (7) `commit_timelocked_weights` 113, `commit_timelocked_mechanism_weights` 118, `set_weights` 0, `set_mechanism_weights` 119, `commit_weights` 96, `reveal_weights` 97; events `TimelockedWeightsCommitted` 108, `WeightsSet` 5; Commitments (18) `set_commitment` 0; Utility (11) `batch_all` 2; AdminUtils (19) `sudo_set_commit_reveal_weights_enabled` 49; `CommitRevealWeightsEnabled`, `RevealPeriodEpochs`, `WeightsVersionKey`, `TimelockedWeightCommits`, `Weights`, `LastUpdate`, `MaxWeightsLimit`, `MinAllowedWeights`; `SubnetInfoRuntimeApi_get_selective_metagraph` | Encodings unchanged. SN's producer caps ciphertext at 5,000 bytes (`crv4.MaxCommitSizeBytes`), the Yuma dispatch bound. Epoch scheduling (`should_run_epoch`, `current_epoch_with_lookahead`), `epoch/math.rs`, `rpc_info/metagraph.rs`, Drand, Commitments and Utility are unchanged. `get_max_weight_limit` still returns `u16::MAX`. On a Null subnet only the sole permit holder could commit, set or reveal. |
| Children, parents and auto parent delegation | `set_children` 67, `set_childkey_take` 75, `set_auto_parent_delegation_enabled` 135; events `SetChildrenScheduled` 76, `SetChildren` 77, `AutoParentDelegationEnabledSet` 131; `ChildKeys`, `ParentKeys`, `PendingChildKeys`, `ChildkeyTake`, `AutoParentDelegationEnabled` | Encodings unchanged. Activation enforces the 100-parent fan-in cap; `auto_parent.rs`, `coinbase/root.rs`, `decrease_take.rs` and `increase_take.rs` are byte-identical. |
| Registration and burn | `register_limit` 134, `burned_register` 7, `register` 6, `root_register` 62; event `NeuronRegistered` 7; `Burn`, `MinBurn`, `MaxBurn`, `ImmunityPeriod`, `BlockAtRegistration`, `SubnetworkN`, `Keys`, `Uids`, `NetworkRegistrationAllowed`, `NetworkPowRegistrationAllowed` | Encodings unchanged except the PoW default. Burned and root registration are unchanged; new fee-free `pow_register`, owner registration toggles and owner protection keyed on `Owner`. |
| Staking | `add_stake` 2, `add_stake_limit` 88, `remove_stake` 3, `remove_stake_limit` 89, `move_stake` 85, `transfer_stake` 86, `swap_stake` 87, `transfer_stake_and_hotkey` 143; events `StakeAdded` 2, `StakeRemoved` 3, `StakeMoved` 4, `StakeTransferred` 92; `TotalHotkeyAlpha`, `AlphaV2`, `TotalHotkeySharesV2`, `StakingHotkeys`; `StakeInfoRuntimeApi` | Unchanged. `add_stake.rs`, `move_stake.rs`, `remove_stake.rs`, `staking/helpers.rs` and `rpc_info/stake_info.rs` are byte-identical; `stake_utils.rs` and the share pool add Null-only credit paths, and the stake-weight and inherited-stake functions are unchanged. |
| Owner hotkeys and owner recycle | `SubnetOwner`, `SubnetOwnerHotkey`, `OwnedHotkeys`, `RecycleOrBurn` (`Burn` 0, `Recycle` 1), AdminUtils `sudo_set_recycle_or_burn` 80, `LastRateLimitedBlock`, `MaxWeightsLimit`, `MechanismCountCurrent`, `SubnetEpochIndex`, `Keys`, `Uids`, `BlockAtRegistration` | Encodings unchanged; the rate key appends two hyperparameters after `RecycleOrBurn` 24. `get_owner_hotkeys` still walks `OwnedHotkeys`. Pruning protection now keys on `Owner`; for SN25 it is still {UID 1}. |
| Root | `root_register` 62, `claim_root` 121, `swap_basket` 150, `swap_basket_many` 151; storage read by the passive root service | Shapes unchanged. `coinbase/root.rs` and `claim_root.rs` are byte-identical; the basket minimum trade is now stored. The passive observer submits nothing. |
| EVM precompiles used by the contracts | `0x402` Ed25519 `verify`; `0x403` Sr25519 `verify`; `0x800` `transfer`; `0x802` `getColdkey`; `0x804` `getUid`, `registerLimit`, `burnedRegister`; `0x805` `getStake`, `moveStake`, `transferStake`; `0x808` `getAlphaPrice`; `0x080c` address mapping | Unchanged. `ed25519.rs`, `sr25519.rs`, `balance_transfer.rs`, `metagraph.rs`, `alpha.rs`, `staking.rs`, `address_mapping.rs` and `extensions.rs` are byte-identical; `neuron.rs` only adds two selectors. EVM chain ID 964. |
| Multisig | Multisig (13) `as_multi_threshold_1` 0, `as_multi` 1, `approve_as_multi` 2, `cancel_as_multi` 3; events 0 to 3; deposit constants | Unchanged: `pallet-multisig` 41.0.0 from Polkadot SDK `cacb4310` and its runtime configuration. Pending multisig operations keep their call hashes because the calls they wrap keep their encodings. |
| Proxy | Proxy (16) `proxy` 0, `add_proxy` 1, `remove_proxy` 2, `proxy_announced` 9; event `ProxyExecuted` 0; `ProxyType`, `Proxies` | Encodings unchanged. The proxy filter removes legacy `register` and `register_limit` from `NonFungible` and `NonCritical`; SN registers through Multisig and the neuron precompile, not a proxy. |
| Operator server | `System.Account` (wallet validation); `Commitments` and `System.Events` (receipt recovery); EVM JSON-RPC | Unchanged. The server pins no runtime version. |
| Transaction format and signing | Extrinsic v4 with `CheckNonZeroSender`, `CheckSpecVersion`, `CheckTxVersion`, `CheckGenesis`, `CheckMortality`, `CheckNonce`, `CheckWeight`, `ChargeTransactionPayment`, `SudoTransactionExtension`, `CheckShieldedTxValidity`, `SubtensorTransactionExtension`, `DrandPriority`, `CheckMetadataHash`; transaction version 1 | Unchanged. The `CheckMetadataHash` digest covers the spec version, so a mode-1 signer must use 475's metadata. |

SN's own checks pass on the exact 475 metadata. They ran on this repository at
`1c49ef46`, with sibling modules from the pinned launch source lock (content
hash `0x919af21979c0c6d5c0fc7c703a549725682e08f29755bb5766dda2f8a95f440f`):

- `crv4.ValidateProvisionalRuntimeMetadata`, which compares the CRv4 producer's
  consumed storage, calls, events and signed extensions with the reviewed 467
  baseline, reports them unchanged for 475, as do all seven fleet purposes.
  `TestRuntime475ValidatorProducerCapability` authenticates the exact artifact
  over a read-only transcript of the node's version reply, which declares
  `SubnetInfoRuntimeApi` version 2, and binds the validator producer purpose.
- `TestNativeOwnerRuntime475MetadataCapabilities` checks the Ed25519 and
  Sr25519 native signing profiles; the owner recycle call (19, 80), rate key and
  storage; passive root storage; treasury `register_limit`, `remove_stake_limit`
  and `transfer_keep_alive` under `as_multi`, `approve_as_multi` and
  `cancel_as_multi`; the economic emission and receipt events; `root_register`
  (7, 62) and the 500-rao existential deposit; the root-registration and
  subnet-census storage profiles; the owner-trim call, `Proxy.Proxies`, window
  and multisig profiles; the execution drain keys; and a planning-only root
  capability report for source 475. Apart from that report, the same checks
  pass through `TestNativeOwnerRetainedRuntimeMetadataCapabilities` with the
  exact 470, 473 and 475 metadata.
- `TestNativeOwnerRuntime475ChangedConsumedItems` pins the two changed items SN
  reads. `TestTakeCallsMatchReviewedRuntimeLayouts` and
  `TestTakeDelegateRateKeyMatchesReviewedRuntimes` keep the take calls (65, 66,
  75) and the delegate-take rate key.
- The successor tests for treasury, owner recycle, passive root, discovery and
  root capabilities, the treasury migration-completion tests and the root
  registration tests run for both 473 and 475.

## Limits and assessment

This record does not establish:

- a source-to-Wasm rebuild, exact or partial;
- independent finality: genesis, blocks and storage are snow archive replies;
- the economic semantics of the changed paths beyond the source read here,
  including Null consensus, PoW pricing and the emission arithmetic of large
  subnets;
- SN's behavior if SN25's owner or root switched SN25 to Null: SN does not read
  `SubnetEpochConsensus`;
- metadata v15 beyond the node's execution;
- GitHub's commit verification or CI artifact, which are cited as GitHub's
  records.

Raw captures were made locally and are not retained by this record, except the
475 metadata that the test fixture carries. Every input is public or readable
from an archive node at the listed blocks, and the listed sizes and digests
identify each one.

**Assessment.** The mainnet runtime installed at block 9,233,781 is
byte-identical to the official v475 release asset, which the release and
GitHub's CI record attribute to an srtool build of exact source commit
d1718c99. The upgrade call that installed it carries the same bytes, and
executing that Wasm reproduces the pinned metadata hash. From 473 to 475 the
encodings of every call, event, storage item, constant, runtime API and signed
extension that the validator, the contracts, the operator server and the
launch tooling consume are unchanged. The two changed items SN reads, the
`LastRateLimitedBlock` key enum and the `NetworkPowRegistrationAllowed`
default, keep the encodings SN uses. For a subnet that stays on Yuma, as SN25
does, weights, consensus, emissions, staking and stake weight compute as
before. The behavior changes that touch SN's paths are the dispatch-time
5,000-byte Yuma commit check, the 100-parent fan-in cap, owner protection keyed
on `Owner`, the stricter owner trim, owner registration toggles with PoW
registration off by default, a possible one-block epoch deferral if any subnet
adopts Null, and the proxy filter for legacy registration calls. Owner-selected
Null consensus would change SN25's validator semantics; only SN25's owner
multisig or root can select it. An approver who binds this record's SHA-256
accepts this artifact-identity basis without a source-to-Wasm rebuild, or must
first obtain one. Independent finality, signing custody and activation remain
separate gates.

[release]: https://github.com/RaoFoundation/subtensor/releases/tag/v475
[compare]: https://github.com/RaoFoundation/subtensor/compare/f87cada631f81d11683e715a9f059f693992e64a...d1718c99c34cf96abbf2bf09c0e9e48b945c76f5
[run]: https://github.com/RaoFoundation/subtensor/actions/runs/37649682333
[mechanism]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/pallets/subtensor/src/subnets/mechanism.rs
[weights]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/pallets/subtensor/src/subnets/weights.rs
[registration]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/pallets/subtensor/src/subnets/registration.rs
[children]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/pallets/subtensor/src/staking/set_children.rs
[uids]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/pallets/subtensor/src/subnets/uids.rs
[proxy-filters]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/runtime/src/proxy_filters/call_groups.rs
[neuron]: https://github.com/RaoFoundation/subtensor/blob/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5/precompiles/src/neuron.rs
[wasmi-hash]: https://github.com/wasmi-labs/wasmi/blob/3c42a099f031fb84f8c0acdb6ea756c8bbd24a0a/crates/collections/src/hash.rs
[ahash-seeds]: https://github.com/tkaitchuck/aHash/blob/9aa1ba20f05ed582eda04ea625d5658c92195a57/src/random_state.rs
