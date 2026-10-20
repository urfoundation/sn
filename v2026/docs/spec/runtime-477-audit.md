# Runtime477 mainnet compatibility review

This is a source/artifact and consumed-interface review of runtime 477, which
Bittensor mainnet installed on 2026-10-12 and replaced with 478 on 2026-10-13,
and the record of settlement v2's runtime rehearsal on it (`VERSION2-FINAL.md`
§14 gate 2). The SHA-256 of this file's exact committed bytes is the
`runtime_review_hash` that a validator, treasury or fleet approval may bind.
The record is evidence for that approver. It approves no genesis, finality
checkpoint, signing, deployment or activation, and grants no permission to
submit a transaction. No transaction was signed or submitted to mainnet while
preparing it. The [runtime475 review](runtime-475-audit.md) and its approvals
remain unchanged; they do not cover 477.

## Exact provenance

On 2026-10-13 the public RPC `https://lite.chain.opentensor.ai` (node
`4.0.0-dev-da06f033663`) reported genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`, chain
`Bittensor`, EVM chain ID 964 and `node-subtensor/477/1/1` at finalized block
9,276,785, `0x763f3c5a4e4441b65729feba2ee36711668a935697bc57ed7d0b77288ec9c315`
(2026-10-13T20:40:36Z). The public archive
`https://archive.chain.opentensor.ai` (node `4.0.0-dev-4b19dc31fc2`) returned
the same genesis, block hash, version, code hash and metadata. Both are third
parties' nodes: their finality and storage replies are those nodes'
assertions, not light-client proofs. They are observations. The owner's
`sn-mainnet runtime-snapshot` on the approved RPC remains the check of record
(`VERSION2-LAUNCH.md` step 2).

Between blocks 9,233,790 and 9,276,785 the archive's runtime version changed
once. Its `:code` held 475 through 9,267,631,
`0x1de25ace4f26947c0394cff9993c7fe6aa1783e53e779edca74de6d317fab5b6`
(2026-10-12T14:09:48Z), with the code and metadata hashes the 475 review
pins, and 477 from 9,267,632,
`0x3734a0beba56a4abed6f2ce2c2805156e7ccdd5136bbc1438a5a1f8df650e10b`
(2026-10-12T14:10:00Z). Block 9,267,633,
`0x1e7c6c363fa43170964314e240cc28ecfa7385e894472bec383089896bd44629`
(14:10:12Z), is the first block executed by 477. No 476 artifact reached
mainnet; spec 476 is the tag `v476` (`d6dd557a`, PR #3217), an ancestor of
v477. The version changed again 140 blocks after the capture above: the
archive holds 477 through 9,276,924,
`0x9470851129339e7b48605f91c0f0fcc665f08aae857d1add5b563e357ef580b8`, and 478
from 9,276,925,
`0xaf5cf4018b7d91b94a48f9904b5c1d453e94be0dcf686d4d42a7e508376cabb1`
(2026-10-13T21:08:36Z), with code hash
`0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1`. This
record therefore covers the runtime of blocks 9,267,632 through 9,276,924.

The official [v477 release][release] (not a prerelease, created
2026-10-08T21:13:47Z and published 2026-10-08T21:55:19Z by
`github-actions[bot]`) and the lightweight tag `v477` name exact source commit
`5c6e83e2f4936f257234f246957f75684c35227c` (tree
`5e22d768fb9a1645c16cd4fdb0f996111a6d91d6`), the merge of PR #3221 with parents
`d4d5288f` (the merge of PR #3218) and `fa1e174e`. `git ls-remote` reports the
same tag target for `opentensor/subtensor` and `RaoFoundation/subtensor`. SN
names this commit `crv4.NativeOwnerSource477`. The v475 commit `d1718c99` is an
ancestor: the [comparison][compare] is 9 commits ahead, none behind, 93 files.
The commits are PR #3217 (subnet owners may set the PoW difficulty), PR #3219
(the root-claim weight reservation on testnet), PR #3218 (the node's log
format, with a vendored informant) and PR #3221 (spec 477 and SDK 11.3.1).

The review's checkout of 5c6e83e2 was checked against the commit's tree: the
git object IDs recomputed from all 3,076 regular files and the one symlink on
disk match the tree's 3,077 entries, with no untracked or ignored path. Of the
93 changed files, 13 are runtime and pallet paths, 5 build only the node
(`node/Cargo.toml` and the four files of the new `vendor/sc-informant`), and 2
are the workspace `Cargo.toml` and `Cargo.lock`; the rest are documentation
(66), the SDK (4), the website (2) and one JavaScript test.
[The source manifest](runtime-v477-source.sha256) pins 191 files: all 185
files of the v475 manifest (14 changed) and the 6 changed or new build paths
outside it (`node/Cargo.toml`, the four files of `vendor/sc-informant` and the
pallet test `pallets/admin-utils/src/tests/pow_difficulty.rs`). Every changed
path that builds the runtime or the node is pinned. Not pinned, as for v475:
`vendor/frontier/Cargo.lock`, the BLS12-381 JSON test vectors, and the
documentation, SDK, website and tooling paths that build neither. Running
`sha256sum -c` on the manifest from the root of a clean checkout of the commit
checks every entry; on the d1718c99 tree it reports the 15 changed digests and
the 5 new files as absent.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Compressed runtime Wasm (`:code`, release `subtensor.wasm`) | 2,630,752 | `e226dd0326b4480a6da7f0534e9854bc399889c934b074c92448cbb7cd0fab63` | `18b52484d9c5d7523f704c86b17b2ab959fffbc6edb6766a77e9877f0ad19387` |
| Decompressed compact Wasm | 10,713,998 | `56d881f81462303e29f75b0ff6364a56094fae3d5387c6caaf46e9476e12947a` | `12c2334b9685380d595bf8013adedd43fea61ca2d9c7fcfcfcd5944dbf1d6b7d` |
| Metadata v14 (`state_getMetadata`) | 357,499 | `1d9a056217f440400df497ad70d4a6a6148abc19871d1b28e52464c507c11260` | `5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e` |

**Code hash.** `:code` is the unhashed well-known key `0x3a636f6465`. Its
bytes, read with `state_getStorage` at block 9,276,785, hash under BLAKE2b-256
to `state_getStorageHash(:code)` on both nodes, which the archive also returns
at blocks 9,267,632 and 9,267,633, and under SHA-256 to the release asset's
digest. They are byte-identical to the release asset and to the code embedded
in the upgrade call below. Removing the 8-byte Substrate zstd prefix
`52bc537646db8e05` and decompressing yields exactly the compact artifact
recorded in the release's srtool digest.

**Metadata hash.** SN computes BLAKE2b-256 over the exact `state_getMetadata`
bytes without re-encoding (`crv4.DecodeRuntimeMetadata`):
`0x5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e`. Both
nodes returned the same bytes at block 9,276,785, and the archive at 9,267,632
and 9,267,633; the test fixture
`crv4/testdata/runtime477-metadata.scale.gz.base64` carries them. The pinned
SN metadata probe was not run for this record. Instead the rehearsal node
below, built from the v477 source, was started on a dev genesis whose `:code`
is the exact compressed Wasm, so that its `state_getRuntimeVersion` and
`state_getMetadata` at genesis execute that Wasm's `Core_version` and
`Metadata_metadata`. It reproduced `node-subtensor/477/1/1`, metadata v14 and
the size and both digests in the third row. This is execution of the exact
Wasm, not a third RPC assertion. Metadata v15 was not captured.

**Release records.** The five release assets match the SHA-256 digests that
GitHub's API records for them:

| Release asset | Bytes | SHA-256 |
| --- | ---: | --- |
| `subtensor.wasm` | 2,630,752 | `e226dd0326b4480a6da7f0534e9854bc399889c934b074c92448cbb7cd0fab63` |
| `subtensor-digest.json` | 4,969 | `70ec3cf08593de8c059ef9a9d8e77da5c2ff21dadbc2b0d0e9589d9c6d06cf66` |
| `upgrade-manifest.json` | 1,979 | `719f866f5e95c453f8ba2071c775267ac59049c010d2a69ae22bb2161818eb8e` |
| `pending-release.json` | 578 | `1837b1cd2474db1481ab1e320beb9a7fa96fe93e6767ddffb7190bc906b9ec47` |
| `proxy_proxy_blob.hex` | 5,261,608 | `bb466e27c596162d9692d2b0068aad6b0604f64ff4ede7b89a39300608bc390c` |

GitHub records all five uploads at 2026-10-12T14:33:17Z, twenty-three minutes
after the code was set on chain, so the attached assets postdate the upgrade;
their bytes still match the chain and the upgrade call. The release is not
marked immutable; the identity above rests on the chain bytes, not on the
assets' mutability. The srtool digest names commit 5c6e83e2, srtool 0.18.3,
`rustc 1.89.0 (29483883e 2025-08-04)`, profile `production`, package
`node-subtensor-runtime`, image `paritytech/srtool:1.89.0`, build time
2026-10-08T21:45:50Z, the compressed and compact digests above, core version
477/1/1 and metadata v14. The release-train run and its job records were not
read.

The published upgrade call (2,630,803 bytes) decodes as `Proxy.proxy(real,
None, Sudo.sudo_unchecked_weight(System.set_code(code), ref_time
50,000,000,000, proof_size 0))` with `real`
`0x4471816662ea3cfadc9868e5f083e26a3be6706b8d8dad7fbef565983afb3556`, the
account of the 475 upgrade. Its embedded code is the exact artifact, and
BLAKE2b-256 of the call is the manifest's call hash
`0xfcc73559efd37fa8a52894ea37a49b924267fa1d4590e38f487f28980660f210`. The
archive's hash for proposal block 9,241,157 (2026-10-08T21:54:48Z) is the
manifest's `0xbb46e6e04f4467fdcca2e955fa8213de864e4dd778a41ca412330cae7d425f83`,
and that block's events hold the call hash. The code was set 26,475 blocks
later, in block 9,267,632: its extrinsic 20 carries the exact Wasm and its
events hold the call hash.

## Source-to-Wasm rebuild: not performed

**No reproducible source-to-Wasm rebuild was performed for this record.**
Nothing here establishes, independently of the upstream release, its srtool
digest and the upgrade call, that 5c6e83e2 compiles to these bytes. The 470
result predicts the likely outcome of one but is not evidence for 477. The
runtime dependency graph is unchanged from 475: the only `Cargo.lock` changes
are that `node-subtensor` gains `tracing-subscriber` and that `sc-informant`
becomes the vendored path crate, without `console`. Nothing in the tree sets
`CONST_RANDOM_SEED`, so an independent production build is again expected to
differ in compile-time hash-seed constants, as the 475 review explains. Before
binding this record, an approver must either accept this artifact-identity
basis without a rebuild, or commission one on a dedicated amd64 host.

What was built is the rehearsal node, which is not a production build.
`scripts/runtime-rehearsal/build.sh` checked that the tag `v477` names
5c6e83e2, that the checkout is clean, that `runtime/src/lib.rs` declares
`spec_version: 477` and every digest of the manifest, then built
`node-subtensor` with `--features "metadata-hash fast-runtime"` under the
checkout's toolchain, rustc 1.89 with target `wasm32v1-none`, the toolchain of
475. Its Wasm is 2,846,031 bytes, SHA-256
`3cfabd8d93c1e0060d8395a6295445cecea957caf7e6da3991d0dc72ab20fe48`, BLAKE2b-256
`71b7103db2a61d9393cab5298471e4ddc627a44d3c75b77a3aa55267dfb79f2f`, and the dev
chain's genesis `:code` is that Wasm byte for byte. `fast-runtime` changes
constants, so it differs from mainnet's Wasm, as for 475.

## Interface delta from 475 to 477

The exact 475 metadata (357,468 bytes, BLAKE2b-256
`0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff`, the
fixture the 475 review pins) was compared item by item with 477. The
comparison covers every storage entry (prefix, modifier, hashers, key and
value types, default bytes), every call, event and error (pallet index,
variant index, field names and types), every constant (type and value bytes),
the signed-extension list and the extrinsic type, including the full closure
of reachable named types; `RuntimeCall` and `RuntimeEvent` are compared
through each pallet's own enum. Each runtime has 1,605 items by this count: 28
pallet indices, 402 storage entries, 311 calls, 309 events, 405 errors, 136
constants, 13 signed extensions and the extrinsic type. 1,603 are identical;
none was added or removed, and 2 changed. The bytes agree with that: the 477
metadata is the 475 metadata with 31 bytes inserted, which are the two enum
variants below, and 2 bytes changed, the enum's variant count and the spec
version. The 25 runtime APIs that `state_getRuntimeVersion` declares, and
their versions, are identical.

| Item | 477 change | Meaning |
| --- | --- | --- |
| constant `System.Version` | spec 475 to 477 | Version only; one byte differs. |
| storage `SubtensorModule.LastRateLimitedBlock` | key's nested `Hyperparameter` enum appends `MinDifficulty` 37 and `Difficulty` 38 | `RateLimitKey` variants, `RecycleOrBurn` 24, `EpochConsensus` 35, `BurnRegistrationAllowed` 36 and the value are unchanged. |

SN reads `LastRateLimitedBlock` in the owner recycle profile, with the
`OwnerHyperparamUpdate(RecycleOrBurn 24)` key, and in the validator take
tool, with `LastTxBlockDelegateTake(5)`. Both keys encode as before.

Runtime-affecting source changes, beyond the version constant:

- **PoW difficulty** (PR #3217; [`pallets/admin-utils/src/lib.rs`][admin],
  `utils/rate_limiting.rs`). AdminUtils calls 4 `sudo_set_min_difficulty` and
  24 `sudo_set_difficulty` took root only. Now a subnet's owner may also send
  them for a non-root subnet, while its admin window is open and under the
  owner's per-hyperparameter rate limit, which the two new `Hyperparameter`
  variants key. Root is not rate-limited. Their declared weight is the larger
  of the earlier weight and `sudo_set_adjustment_alpha`'s; their encodings are
  unchanged. PoW registration stays disabled unless the owner or root enables
  it, as in 475.
- **Root claim weight** (PR #3219; `staking/claim_root.rs`,
  `pallets/subtensor/src/lib.rs`). `root_claim_hotkey_declared_work` returned
  1,025 units on the Finney testnet's genesis and 129 elsewhere. It now
  returns 129 everywhere, and the testnet constant is removed. Mainnet's value
  is unchanged.
- **Proxy filters** ([`runtime/src/proxy_filters`][proxy-filters]).
  `sudo_set_network_pow_registration_allowed`, `sudo_set_difficulty` and
  `sudo_set_min_difficulty` move from `RootConfigCalls` to
  `SubnetManagementCalls`. `ProxyType`, `pallets/proxy` and
  `common/src/proxy.rs` are unchanged.
- **Fee baseline.** The pinned fees of the two difficulty calls rise from
  106,802 and 106,832 rao to 152,662 rao, with their weight.
- **Node, not runtime.** `vendor/sc-informant` copies the informant of
  Polkadot SDK `cacb4310` without `console` styling, and the workspace
  `Cargo.toml` patches it in; `node/Cargo.toml` adds `tracing-subscriber`. No
  file under `node/src` changes.
- **Runtime crate and upgrade.** `runtime/src/lib.rs` changes in one line,
  `spec_version`. The migrations tuple and the pallet's migration hooks are
  unchanged (`pallets/subtensor/src/migrations` and `macros/hooks.rs` are
  byte-identical), so the upgrade ran no new migration. Polkadot SDK
  `cacb4310`, every runtime dependency, the pallet configuration, the fee
  schedule, the weight files and the EVM configuration are unchanged.

The remaining changes touch documentation, the SDK, the website and one test.

## What settlement v2 relies on

Paths are in upstream 5c6e83e2. "Identical" means byte-identical to d1718c99.
The R tests are those of the next section.

| Dependency | Source | Change from 475 | Verdict |
| --- | --- | --- | --- |
| Staking precompile `0x805`: `getStake` (`:354`), `getMinerCollateral` (`:1720`), `moveStake` (`:256`), `transferStake` (`:283`), their selectors, arguments and return data | `precompiles/src/staking.rs`, `precompiles/src/lib.rs` | Identical; all of `precompiles/` is | Unchanged. Smoke, R-2, R-3 |
| Their semantics: exact debit, a destination one rao short, the whole-position sentinel, `StakeUnavailable` on locked collateral, `AccountRejectsLockedAlpha`, `AmountTooLow` below 100,000 rao, the 128- and 256-entry staking-hotkey caps | `staking/move_stake.rs`, `staking/stake_utils.rs`, `staking/lock.rs`, `subnets/collateral.rs`, `primitives/share-pool`; the caps at `pallets/subtensor/src/lib.rs:93,100` | Identical, except that `lib.rs` loses the two lines of the testnet root-claim constant | Unchanged. Smoke, R-2, R-3 |
| Gas of a precompile dispatch: admission against the declared weight, charge of the actual weight, a refusal consuming the forwarded gas | `precompiles/src/extensions.rs`, `pallets/subtensor/src/weights.rs`, `macros/dispatches.rs` | Identical | Unchanged. Every measured figure equals 475's |
| The transfer toggle | AdminUtils call 61 `sudo_set_toggle_transfer`, now at `pallets/admin-utils/src/lib.rs:1685` (1649 in 475); `TransferDisallowed` at `staking/stake_utils.rs:1795-1807` | The call moved 36 lines down, unchanged; `stake_utils.rs` identical | Unchanged |
| Registration and burn through the neuron precompile `0x804`: `registerLimit` (`:236`), `burnedRegister` (`:204`), `getUid` (`:890`) | `precompiles/src/neuron.rs`, `subnets/registration.rs`, `subnets/uids.rs` | Identical | Unchanged. Smoke, R-1, R-4 |
| Registration collateral | `subnets/collateral.rs` | Identical | Unchanged. Smoke, R-2, R-4 |
| Immunity, pruning order, burn bounds, minimum stake | `subnets/registration.rs`, `subnets/uids.rs`; `SubtensorInitialImmunityPeriod` 4,096, `SubtensorInitialMinBurn` 500,000 rao, `SubtensorInitialMaxBurn` 100 TAO and `SubtensorInitialMinStake` 2,000,000 rao at `runtime/src/lib.rs:928-948` | Identical; `runtime/src/lib.rs` changes only its version line | Unchanged. SN25's stored values are chain state, which this record does not read |
| EVM configuration and gas schedule | `runtime/src/lib.rs` (`WeightPerGas`, `BlockGasLimit`, `GasLimitPovSizeRatio`, `DefaultBaseFeePerGas`, `SubtensorEvmBalanceConverter`), `vendor/frontier` | Identical apart from the version line | Unchanged. The transient-storage probe behaves as on 475 |
| Block and weight limits | `MAXIMUM_BLOCK_WEIGHT`, `NORMAL_DISPATCH_RATIO` and the block length at `runtime/src/lib.rs:248-272` | Identical | Unchanged. R-7: block gas limit 75,000,000, and the pool takes 64,998,500 gas and refuses 64,998,501 |
| Emission, epochs and tempo | `coinbase/`, `epoch/`, `subnets/` | Identical | Unchanged. R-3: the escrow hotkey earned 3,595,812,938 rao over two tempos without weights and nothing while a validator set them, to the rao as on 475 |
| Call value | `SubtensorEvmBalanceConverter` | Identical | Unchanged. Smoke |

Nothing that vault2, `STSettlement2`, `STClaimBatcher` or the launch tooling
calls or reads changed. The one change near them is the first of the list
above: SN25's owner can now set SN25's PoW difficulty. It matters only if the
owner or root enables PoW registration on SN25, which admits hotkeys without
a burn and so brings pruning forward; vault2 works with its escrow and pool
hotkeys pruned (R-4).

## Runtime rehearsal on 477

The R battery (`VERSION2-FINAL.md` §15.8) ran on 2026-10-13 on the node above,
from this repository at `188c1547` with this branch's changes. Every contract
is built by `forge` from that tree with the pinned toolchain, and R-1 deploys
the reviewed release of `sim-testnet/contracts_gen.go`. Each test starts its
own node; the tests reach only loopback.

| Test | Result on 477 |
| --- | --- |
| R-1 `TestR1SettlementV2Phase` | Pass. The approval names source 5c6e83e2 with the node's version 477 |
| R-2 `TestR2Capture` | Pass |
| R-3 `TestR3Claim` | Pass |
| R-4 `TestR4Registration` | Pass |
| R-5 and R-6 `TestR5Settlement` | Pass |
| R-7 `TestR7ClaimBatch`, `TestR7MaxGasDoesNotDependOnCalldata` | Pass |
| `TestRehearsalSmoke`, `TestRehearsalTransientStorage` | Pass |

Gas, beside the 475 figures of `scripts/runtime-rehearsal/README.md` and the
limits of `VERSION2-LAUNCH.md` section 2.6:

| Transaction | 475 | 477 | Limit |
| --- | ---: | ---: | ---: |
| `vault2-create` | 2,821,690 | 2,821,690 | 4,000,000 |
| `escrow2-register` | 1,463,833 | 1,463,833 | 2,500,000 |
| `settlement2-create` | 1,935,622 | 1,935,622 | 3,000,000 |
| `proxy2-create` | 310,486 | 310,462 and 310,486 | 1,000,000 |
| `vault2-link` | 31,329 | 31,317 and 31,329 | 150,000 |
| `batcher-create` | 285,209 | 285,209 | 400,000 |
| `registerOperatorPool` | 1,594,088 | 1,594,088 | |
| close, capturing: used; limit needed | 225,430; 2,688,632 | 225,430; 2,688,632 | signed at about 3.2M |
| close, nothing to capture | 126,610; 144,347 | 126,610; 144,347 | |
| close deferring 1 epoch | 131,633; 149,526 | 131,633; 149,526 | |
| close deferring 144 epochs | 2,409,622; 2,949,195 | 2,409,622; 2,949,195 | |
| commit | 205,288; 225,484 | 205,288; 225,484 | |
| answer | 149,074; 154,089 | 149,074; 154,089 | |
| finalize | 94,389; 100,877 | 94,389; 100,877 | |
| approve | 132,524; 135,736 | 132,524; 135,736 | |
| demand | 98,268; 104,424 | 98,256; 104,413 | |
| claim, vault2's first | 186,682; 2,572,701 | 186,682; 2,572,701 | |
| claim, an account's first | 186,707; 2,572,776 | 186,707; 2,572,776 | |
| claim, an account's first after its neighbour's | 169,595; 2,521,578 | 169,583; 2,521,542 | |
| claim, a repeat | 169,606; 2,521,610 | 169,606; 2,521,610 | |
| claim with vault2's staking-hotkey list at 128 | 1,256,716 | 1,256,740 | |
| `absorbStake` with that list at 128 | 1,220,874 | 1,220,874 | |
| batch of 32 claims, per claim | 147,740 | 147,735 | |
| the most claims one transaction holds | 418 | 418 | |
| the most gas one transaction may name | 64,998,500 | 64,998,500 | |
| `moveStake`, `transferStake` inside a contract | 81,753; 79,321 | 81,753; 79,321 | |
| `registerLimit` inside a contract | 1,409,550 | 1,409,550 | |

R-1 ran twice on 477, and the two rows with two figures are its two runs.
Every difference, between the runtimes and between those two runs, is a
multiple of 12 gas, the cost of one calldata byte being nonzero instead of
zero: each run draws new keys, so its addresses and proofs differ in zero
bytes. The largest is 2,796 gas in the 61.2 million of the batch of 418. The
475 record notes the same variation. No figure is over a limit, and the
deployer's, the Safe's, the operator's, the auditor's and the claimants' sizes
of section 2.6 stand.

The behaviour is the same in every case the battery checks: a close moved
exactly the pool's stake into escrow, and exactly `getStake − locked` with a
collateral lock, 38,742,541,778 of 40,223,672,009 rao; the floor refused
11,092 alpha rao and took 11,093 at price 9.014779842; a claim to a coldkey
at the staking-hotkey cap was refused and stayed claimable; a transfer of
conviction-locked alpha to vault2 was refused; 2^64 − 1 moved a whole
position; a refused precompile call consumed the gas forwarded to it; and a
transient write still survived a reverted call frame.

## SN consumers

No interface that SN consumes changed its encoding. The 475 review's consumer
table holds for 477 row by row, with two differences in the sources it names:
`staking/claim_root.rs` changed in the testnet weight above, and
`pallets/admin-utils/src/lib.rs` in the two difficulty calls, which leaves the
owner recycle call (19, 80) as it was. These sources are byte-identical to
d1718c99: `subnets/weights.rs`, `coinbase/reveal_commits.rs`, `epoch/`,
`staking/set_children.rs`, `staking/auto_parent.rs`,
`staking/decrease_take.rs`, `staking/increase_take.rs`,
`subnets/registration.rs`, `coinbase/root.rs`, `staking/add_stake.rs`,
`staking/move_stake.rs`, `staking/remove_stake.rs`, `staking/stake_utils.rs`,
`rpc_info/`, `pallets/commitments`, `pallets/drand`, `pallets/utility`,
`pallets/proxy`, `pallets/transaction-fee`, the migrations and every
precompile. The extrinsic format, the thirteen signed extensions and
transaction version 1 are unchanged; the `CheckMetadataHash` digest covers the
spec version, so a mode-1 signer must use 477's metadata.

SN's own checks pass on the exact 477 metadata, on this branch:

- `TestRuntime477ConsumedInterfacesMatchReviewedBaseline` compares the CRv4
  producer's consumed storage, calls, events and signed extensions, and all
  seven fleet purposes, with the reviewed 467 baseline.
  `TestRuntime477ValidatorProducerCapability` authenticates the exact artifact
  over a read-only transcript of the node's version reply and binds the
  validator producer purpose.
- `TestNativeOwnerRuntime477MetadataCapabilities` runs the native owner,
  passive root, treasury, census, owner-trim and root-registration profiles
  that the 475 metadata passes. `TestNativeOwnerRuntime477ChangedConsumedItems`
  pins the changed enum.
- `TestTakeCallsMatchReviewedRuntimeLayouts` and
  `TestTakeDelegateRateKeyMatchesReviewedRuntimes` keep the take calls (65, 66,
  75) and the delegate-take rate key.
- The successor tests for treasury, owner recycle, passive root, discovery and
  root capabilities, the treasury migration-completion test and the root
  registration test run for 477 beside 473 and 475.

## Limits and assessment

This record does not establish:

- a source-to-Wasm rebuild, exact or partial;
- independent finality: genesis, blocks and storage are two public nodes'
  replies, and the owner's own snapshot is the check of record;
- the economic semantics of the changed paths beyond the source read here;
- SN25's stored hyperparameters, or SN's behaviour if SN25's owner enabled PoW
  registration;
- metadata v15;
- GitHub's release records beyond the downloaded assets' bytes;
- anything about runtime 478. Upstream tagged `v478`
  (`850f2755bb05a3851f948489f0ab27b0645cebce`) on 2026-10-13, and its upgrade
  manifest names a mainnet proposal at block 9,276,607 with call hash
  `0x601d98fe0087a99cc12422e38a36f858bad06e7676dc3ec31e3022a05d209f5e`. At
  finalized block 9,276,829 (2026-10-13T20:49:24Z) the public RPC still
  reported 477 and held that proposal pending with one approval; mainnet
  installed 478 in block 9,276,925. 478 sends the staking precompile's
  `moveStake` and `transferStake` through a new dispatch path that changes the
  gas limit they need, so gate 2 repeats on it.

Raw captures were made locally and are not retained by this record, except
the 477 metadata that the test fixture carries. Every input is public, and the
listed sizes and digests identify each one.

**Assessment.** The runtime that the public nodes report on mainnet from block
9,267,632 through 9,276,924 is byte-identical to the official v477 release
asset, which the release's srtool digest attributes to exact source commit
5c6e83e2. The upgrade call that installed it carries the same bytes, and
executing that Wasm reproduces the pinned metadata hash. From 475 to 477 the
metadata changes in two items: the version, and two variants appended to the
`Hyperparameter` enum inside one storage key. The encodings of every call,
event, storage item, constant, runtime API version and signed extension that
the validator, the contracts, the operator server and the launch tooling
consume are unchanged. The staking and neuron precompiles, the stake-moving,
registration, collateral, emission and epoch sources, the EVM configuration,
the gas schedule and the block limits are byte-identical to 475's. The
behaviour changes are two difficulty setters that subnet owners may now call,
their proxy filter group, and a root-claim weight that only testnet used.
Settlement v2's exact bytecode passed R-1 to R-7 on a node built from this
source, with the gas of 475. Gate 2 therefore holds for runtime 477, and only
for it: a mainnet runtime of any other version repeats the gate, as 478 now
does. An approver who binds this record's SHA-256 accepts this
artifact-identity basis without a source-to-Wasm rebuild, or must first obtain
one. Independent finality, signing custody and activation remain separate
gates.

[release]: https://github.com/RaoFoundation/subtensor/releases/tag/v477
[compare]: https://github.com/RaoFoundation/subtensor/compare/d1718c99c34cf96abbf2bf09c0e9e48b945c76f5...5c6e83e2f4936f257234f246957f75684c35227c
[admin]: https://github.com/RaoFoundation/subtensor/blob/5c6e83e2f4936f257234f246957f75684c35227c/pallets/admin-utils/src/lib.rs
[proxy-filters]: https://github.com/RaoFoundation/subtensor/blob/5c6e83e2f4936f257234f246957f75684c35227c/runtime/src/proxy_filters/call_groups.rs
