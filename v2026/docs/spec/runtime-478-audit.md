# Runtime478 mainnet compatibility review

This is a source/artifact and consumed-interface review of runtime 478, which
Bittensor mainnet installed on 2026-10-13, and the record of settlement v2's
runtime rehearsal on it (`VERSION2-FINAL.md` §14 gate 2). Runtime 478 changes
how the staking precompile admits `moveStake` and `transferStake`, the two
calls vault2 depends on for good, so the record also reviews every gas
assumption of vault2 and of the tools that send its transactions. The SHA-256
of this file's exact committed bytes is the `runtime_review_hash` that a
validator, treasury or fleet approval may bind. The record is evidence for
that approver. It approves no genesis, finality checkpoint, signing,
deployment or activation, and grants no permission to submit a transaction.
No transaction was signed or submitted to mainnet while preparing it. The
[runtime477 review](runtime-477-audit.md) and its approvals remain unchanged;
they do not cover 478.

## Exact provenance

On 2026-10-13 the public RPC `https://lite.chain.opentensor.ai` (node
`4.0.0-dev-da06f033663`) and the public archive
`https://archive.chain.opentensor.ai` (node `4.0.0-dev-4b19dc31fc2`) reported
genesis `0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
chain `Bittensor`, EVM chain ID 964 and `node-subtensor/478/1/1` at finalized
block 9,277,117,
`0xe34591e9ffef093c8a6fb53d5a781cecd6e81bce94e17d7ddbdcd8b14b93522b`
(2026-10-13T21:47:00Z), with the same code hash and metadata. Both are third
parties' nodes: their finality and storage replies are those nodes'
assertions, not light-client proofs. They are observations. The owner's
`sn-mainnet runtime-snapshot` on the approved RPC remains the check of record
(`VERSION2-LAUNCH.md` step 2).

The archive's `:code` changed once after the 477 review's last block,
9,276,785. It held 477 through block 9,276,924,
`0x9470851129339e7b48605f91c0f0fcc665f08aae857d1add5b563e357ef580b8`
(2026-10-13T21:08:24Z), with the code hash
`0x18b52484d9c5d7523f704c86b17b2ab959fffbc6edb6766a77e9877f0ad19387` that the
477 review pins, and 478 from block 9,276,925,
`0xaf5cf4018b7d91b94a48f9904b5c1d453e94be0dcf686d4d42a7e508376cabb1`
(21:08:36Z). Block 9,276,926,
`0x3a416778561d13d8487d9be66bf8dc0ba521d694596cd514cfba262a818fc4cc`
(21:08:48Z), is the first block executed by 478.

The official [v478 release][release] (not a prerelease, created
2026-10-13T01:50:29Z and published 2026-10-13T20:05:31Z by
`github-actions[bot]`) and the lightweight tag `v478` name exact source commit
`850f2755bb05a3851f948489f0ab27b0645cebce` (tree
`5d9b6603fbc31470e68fba8dfa08738894f3a34d`), the merge of PR #3237 with parents
`5c6e83e2` (v477) and `4c969452`. `git ls-remote` of
`opentensor/subtensor` reports the same tag target. SN names this commit
`crv4.NativeOwnerSource478`. v477 is an ancestor: the [comparison][compare] is 9
commits ahead, none behind, 32 files. The commits are PR #3233 (the stake-exit
gas bound: `41afc661`, its tests `b5cbe84a`, the spec bump `8677391f` and the
`unstake_all` sizing `900bd9e8`), PR #3235 (restoring missing staking-hotkey
associations: `3010d109`), a docs regeneration `4c969452` and the release
merge, PR #3237.

The review's checkout of 850f2755 was checked against the commit's tree: the
git object IDs recomputed from all 3,078 regular files and the one symlink on
disk match the tree's 3,079 entries, with no untracked or ignored path. Of the
32 changed files, 9 build the runtime: `precompiles/src/extensions.rs`,
`precompiles/src/staking.rs`, `chain-extensions/src/lib.rs`,
`pallets/subtensor/src/macros/hooks.rs`, `migrations/mod.rs`,
`migrations/migrate_restore_staking_hotkeys.rs` (new),
`staking/remove_stake.rs`, `staking/stake_utils.rs` and `runtime/src/lib.rs`.
Three are its Rust tests, `chain-extensions/src/tests.rs`,
`pallets/subtensor/src/tests/migration.rs` and `runtime/tests/stake_exit_gas.rs`
(new). The rest are documentation (17), the website (2) and one TypeScript
test. `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`, `node/` and `vendor/`
are byte-identical to v477. [The source manifest](runtime-v478-source.sha256)
pins 193 files: all 191 files of the v477 manifest (12 changed) and the 2 new
build paths outside it, the migration and `runtime/tests/stake_exit_gas.rs`.
Every changed path that builds the runtime or the node is pinned. Not pinned,
as for v477: `vendor/frontier/Cargo.lock`, the BLS12-381 JSON test vectors,
and the documentation, website and tooling paths that build neither. Running
`sha256sum -c` on the manifest from the root of a clean checkout of the commit
checks every entry; on the 5c6e83e2 tree it reports the 12 changed digests and
the 2 new files as absent.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Compressed runtime Wasm (`:code`, release `subtensor.wasm`) | 2,634,898 | `1d64a44f3607eb431ef8bbd909dd35dfcbcf9cc9168d9bf74923c198d3619ac6` | `7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1` |
| Decompressed compact Wasm | 10,716,382 | `88366663c52937972d29eac31a353de391ec6b5976b4f3ca7b8e633682966e8e` | `d11c904ad4236a3ed71c86835e2565b0678c440f933a05d3ee053bc413a00373` |
| Metadata v14 (`state_getMetadata`) | 357,499 | `2513780705c9e9c1b9fb7e701670fee88cf8419b945100fc26ab87218b03e35a` | `4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747` |

**Code hash.** `:code` is the unhashed well-known key `0x3a636f6465`. Its
bytes, read with `state_getStorage` at block 9,276,926, hash under BLAKE2b-256
to `state_getStorageHash(:code)`, which both nodes return at block 9,277,117
and the archive at 9,276,925, and under SHA-256 to the release asset's digest.
They are byte-identical to the release asset and to the code embedded in the
upgrade call below. Removing the 8-byte Substrate zstd prefix
`52bc537646db8e05` and decompressing yields exactly the compact artifact
recorded in the release's srtool digest.

**Metadata hash.** SN computes BLAKE2b-256 over the exact `state_getMetadata`
bytes without re-encoding (`crv4.DecodeRuntimeMetadata`):
`0x4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747`. Both
nodes returned the same bytes at block 9,277,117, and the archive at 9,276,925
and 9,276,926; the test fixture `crv4/testdata/runtime478-metadata.scale.gz.base64`
carries them. The rehearsal node below, built from the v478 source, was
started on a dev genesis whose `:code` is the exact on-chain Wasm, so that its
`state_getRuntimeVersion` and `state_getMetadata` at genesis execute that
Wasm's `Core_version` and `Metadata_metadata`. It reproduced
`node-subtensor/478/1/1`, the 25 runtime APIs and their versions, metadata v14
and the size and both digests of the third row. This is execution of the
exact Wasm, not a third RPC assertion. Metadata v15 was not captured.

**Release records.** The five release assets match the SHA-256 digests that
GitHub's API records for them:

| Release asset | Bytes | SHA-256 |
| --- | ---: | --- |
| `subtensor.wasm` | 2,634,898 | `1d64a44f3607eb431ef8bbd909dd35dfcbcf9cc9168d9bf74923c198d3619ac6` |
| `subtensor-digest.json` | 4,969 | `6e25fdfa2f28f9f5fb12d659e71075f76011ea37a93d4c98aea8e7778f012b34` |
| `upgrade-manifest.json` | 1,979 | `4913cc9963e4f2e708d485e908f5881f7d685b7fb75b8c29ba4f95b62cb1995d` |
| `pending-release.json` | 578 | `7b61335bedbfd3dc1acdd735c9228851f0b1bcd18c98001c03aaebe2bc37748a` |
| `proxy_proxy_blob.hex` | 5,269,900 | `c95aadc872c2ceae2f45e4fd79ba8236462d6772f35ad70c9b9d845ce2c1752b` |

GitHub records all five uploads at 2026-10-13T21:19:24Z, eleven minutes after
the code was set on chain, so the attached assets postdate the upgrade; the
three downloaded (`subtensor.wasm`, the digest and the manifest) still match
the chain and the upgrade call. The release is not marked immutable; the
identity above rests on the chain bytes, not on the assets' mutability. The
srtool digest names commit 850f2755, srtool 0.18.3,
`rustc 1.89.0 (29483883e 2025-08-04)`, profile `production`, package
`node-subtensor-runtime`, image `paritytech/srtool`, build time
2026-10-13T01:59:41Z, the compressed and compact digests above, core version
478/1/1 and metadata v14. The release-train run and its job records were not
read.

The upgrade manifest names spec 478, commit 850f2755, the 477 runtime it
replaces, the Wasm's SHA-256 above, a maximum weight of ref time
50,000,000,000 and proof size 0, and a proposal in block 9,276,607,
`0xa136cb7c9aba7e8a1e19aefd8c4a6be6e8419de6780c2e9f99d31edff817f901`
(2026-10-13T20:05:00Z), extrinsic 7, with call hash
`0x601d98fe0087a99cc12422e38a36f858bad06e7676dc3ec31e3022a05d209f5e`. The
archive holds that block under that hash, and its extrinsic 7 (183 bytes)
carries the call hash. Extrinsic 10 of block 9,276,925 (2,635,202 bytes) is a
`Multisig.as_multi` at the proposal's timepoint, 9,276,607 index 7. Its call
decodes as `Proxy.proxy(real, None, Sudo.sudo_unchecked_weight(System.set_code(code),
ref_time 50,000,000,000, proof_size 0))` with `real`
`0x4471816662ea3cfadc9868e5f083e26a3be6706b8d8dad7fbef565983afb3556`, the
account of the 475 and 477 upgrades, its embedded code is the exact artifact,
and BLAKE2b-256 of the call is the manifest's call hash. The code was set 318
blocks after the proposal.

## Source-to-Wasm rebuild: not performed

**No reproducible source-to-Wasm rebuild was performed for this record.**
Nothing here establishes, independently of the upstream release, its srtool
digest and the upgrade call, that 850f2755 compiles to these bytes. The
runtime dependency graph is 477's: `Cargo.lock` is byte-identical to v477's,
and nothing in the tree sets `CONST_RANDOM_SEED`, so an independent production
build is again expected to differ in compile-time hash-seed constants, as the
475 review explains. Before binding this record, an approver must either
accept this artifact-identity basis without a rebuild, or commission one on a
dedicated amd64 host.

What was built is the rehearsal node, which is not a production build.
`scripts/runtime-rehearsal/build.sh` checked that the tag `v478` names
850f2755, that the checkout is clean, that `runtime/src/lib.rs` declares
`spec_version: 478` and every digest of the manifest, then built
`node-subtensor` with `--features "metadata-hash fast-runtime"` under the
checkout's toolchain, rustc 1.89 with target `wasm32v1-none`, 475's and 477's.
The build took 746 s at `-j 10`, from a copy of the 475 build's target
directory. Its Wasm is 2,848,341 bytes, SHA-256
`3d754c8b872a1728098fc980ba8bbd1bcaa7a1ec802565010f4ce173d628c308`,
BLAKE2b-256 `1978bf7751e93d8b2d6c3ecaf44c5e4db54f41dca9a7ea750c2ed4f59ad03ae6`,
and the dev chain's genesis `:code` is that Wasm byte for byte. `fast-runtime`
changes constants, so it differs from mainnet's Wasm, as for 477.

## Interface delta from 477 to 478

The exact 477 metadata (357,499 bytes, BLAKE2b-256
`0x5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e`, the
fixture the 477 review pins) and the 478 metadata have the same length and
differ in one byte, at offset 282,006: the low byte of `System.Version`'s spec
version, `0xdd` (477) to `0xde` (478). Every storage entry, call, event, error,
constant, signed extension and type is therefore unchanged, and so is the
encoding of everything SN consumes (`TestNativeOwnerRuntime478MetadataChangesOnlyTheSpecVersion`).
The `state_getRuntimeVersion` replies are identical apart from `specVersion`:
the same 25 runtime APIs at the same versions, `implVersion` 1,
`transactionVersion` 1 and `stateVersion` 1.

Runtime-affecting source changes, beyond the version constant:

- **Stake-exit admission** (PR #3233; [`precompiles/src/extensions.rs`][extensions]:90-120,
  [`precompiles/src/staking.rs`][staking]). Eight dispatches of the staking
  precompiles move from `try_dispatch_runtime_call` to the new
  `try_dispatch_stake_exit(call, coldkey)`: `removeStake`,
  `removeStakeFullLimit`, `moveStake` (`:256-281`), `transferStake`
  (`:283-308`), and those at `:795`, `:995`, `:1818` and `:2011`. Selectors,
  arguments and return data are unchanged. For a call whose declared weight
  includes a `StakingHotkeys` walk
  (`declared_staking_hotkeys_walks`, `pallets/subtensor/src/staking/stake_utils.rs:1466`),
  it charges one storage read (`record_db_reads(1)`, 625 gas) and admits the
  call against its declared weight less the part of each walk that the
  caller's current list cannot use (`staking_hotkeys_walk_unused`, `:1487`,
  from `staking_hotkeys_walk_immediate_bound`, `:1452`: a walk of the list's
  length plus one entry, at most 256). Both paths then dispatch through
  `dispatch_with_info` (`:122-175`), which is the 477 admission, reservation,
  dispatch and charge code unchanged. The declared transaction weights of the
  calls, the weight file and the post-dispatch charge are unchanged.
  `unstake_all` and `unstake_all_alpha` size each of their 16 slot walks the
  same way (`remove_stake.rs`, `unstake_all_declared_walks`), and the
  contracts chain extension charges stake exits the same sizing up front
  (`chain-extensions/src/lib.rs`). SN uses neither.
- **Staking-hotkey restoration** (PR #3235;
  [`migrations/migrate_restore_staking_hotkeys.rs`][restore]). The upgrade
  schedules `migrate_restore_staking_hotkeys_v1` (`runtime/src/lib.rs:1671`;
  `kickoff`, `:36`), and the pallet's idle hook continues it
  (`macros/hooks.rs:337-356`) once the Alpha V2 conversion, the storage-bloat
  cleanup and `migrate_cleanup_staking_hotkeys_v3` have finished. In bounded
  batches of at most 64 rows a block (`:21`, `:90`), using idle weight only,
  it scans every `AlphaV2` share row and then every `BasketClaimed`
  watermark, and appends the row's hotkey to its coldkey's `StakingHotkeys`
  where the pair must remain (any share row, zero and retired ones included,
  or a nonzero watermark) and the entry is missing. It appends without the
  admission caps, so that no existing position is left unwalked. On mainnet
  the cleanup migration had completed before 478; the archive showed the
  restoration at 64 rows scanned and none restored at block 9,276,926, 1,216
  and 12 at 9,276,944, and 26,248 and 512 at 9,277,340 (22:31:36Z), still in
  its first phase.
- **Runtime crate.** `spec_version` becomes 478 and the migrations tuple
  gains the restoration. Polkadot SDK `cacb4310`, every runtime dependency,
  the pallet configuration, the weight files, the fee schedule, the EVM
  configuration and the block limits are unchanged.

The remaining changes touch documentation, the website and tests.

## What settlement v2 relies on

Paths are in upstream 850f2755. "Identical" means byte-identical to 5c6e83e2.
The R tests are those of the next section.

| Dependency | Source | Change from 477 | Verdict |
| --- | --- | --- | --- |
| Staking precompile `0x805`: `getStake`, `getMinerCollateral`, `moveStake` (`:256`), `transferStake` (`:283`), their selectors, arguments and return data | `precompiles/src/staking.rs` | Only the dispatch of the stake exits changes; the views and every selector are identical | Unchanged interface. Smoke, R-2, R-3 |
| Their semantics: exact debit, a destination one rao short, the whole-position sentinel, `StakeUnavailable` on locked collateral, `AccountRejectsLockedAlpha`, `AmountTooLow` below 100,000 rao, the 128- and 256-entry staking-hotkey caps | `staking/move_stake.rs`, `staking/lock.rs`, `subnets/collateral.rs`, `primitives/share-pool`, `stake_utils.rs:1407-1418,1865-1874`; the caps at `pallets/subtensor/src/lib.rs:93,100` | `stake_utils.rs` only adds the three walk helpers; the rest identical | Unchanged. Smoke, R-2, R-3 |
| Gas admission of `moveStake` and `transferStake` | `precompiles/src/extensions.rs:90-175` | Admitted against the walk of the caller's own list plus one entry instead of a 256-entry walk | Changed; below. Every R test passes |
| Charge after dispatch, and a refused call consuming the gas forwarded to it | `extensions.rs:149-206` (`charge_and_refund_after_dispatch` from `:177`), the EVM executor | Identical code; one extra 625-gas read before the dispatch | Unchanged, +625 gas a stake exit. Smoke, admission test, R-7 |
| Registration and burn through the neuron precompile `0x804` | `precompiles/src/neuron.rs`, `subnets/registration.rs`, `subnets/uids.rs` | Identical | Unchanged. Smoke, R-1, R-4 |
| EVM configuration, gas schedule, block and weight limits | `runtime/src/lib.rs`, `vendor/frontier` | Identical apart from the version line and the migrations tuple | Unchanged. R-7: block gas limit 75,000,000; the pool takes 64,998,500 gas and refuses 64,998,501, with 0, 100,000 and 900,000 bytes of calldata |
| Emission, epochs, tempo, transient storage | `coinbase/`, `epoch/`, `subnets/`, Frontier | Identical | Unchanged. R-3, transient-storage probe |

### The admission rule, before and after

Weights are those of the runtime: 40,000 weight a gas (`weight_per_gas`), a
read 25,000,000 and a write 100,000,000 (`RocksDbWeight`), a walk of `e`
entries `(14e + 1)` reads (`stake_utils.rs:1421`), `move_stake` 260,676,000
with 29 reads and 12 writes and `transfer_stake` 313,389,000 with 27 reads and
11 writes (`weights.rs`), and every call's declared weight also holds the
coldkey-swap guard's 10,000,000 and 2 reads, which no dispatch is charged.

- **475 and 477.** A stake exit is admitted only if the gas forwarded to the
  precompile covers `(guard + call + walk(256)) / 40,000`: 2,296,767 gas for
  `moveStake` and 2,294,335 for `transferStake`, whatever the caller.
- **478.** It is admitted only if the gas forwarded covers
  `(guard + call + walk(min(n + 1, 256))) / 40,000`, `n` being the caller's
  `StakingHotkeys` length when the call runs: 65,517 + 8,750·n gas for
  `moveStake` and 63,085 + 8,750·n for `transferStake`, and 477's figures at
  n ≥ 255. The admission compares against the gas forwarded, not the gas left
  after the 625-gas read.
- **Both.** The call then uses its actual weight, the call and a walk of the
  list as it stands after the call, capped at what was admitted, and on 478
  also the 625-gas read. A refused call, by admission or by the dispatch,
  fails the precompile call and consumes all the gas forwarded to it.

`TestRehearsalStakeExitAdmission` (`rehearsal/admission_test.go`) measured the
least gas forwarded at which each call is admitted, by `eth_call` from a
contract's address, as third parties lengthened the contract's list, and
found the rule exact to the gas, including a move onto a hotkey new to the
list:

| Caller's list | `moveStake`, 477 | `moveStake`, 478 | `transferStake`, 477 | `transferStake`, 478 |
| ---: | ---: | ---: | ---: | ---: |
| 2 | 2,296,767 | 83,017 | 2,294,335 | 80,585 |
| 3 | 2,296,767 | 91,767 | 2,294,335 | 89,335 |
| 4 | 2,296,767 | 100,517 | 2,294,335 | 98,085 |
| 16 | 2,296,767 | 205,517 | 2,294,335 | 203,085 |
| 64 | 2,296,767 | 625,517 | 2,294,335 | 623,085 |
| 128 | 2,296,767 | 1,185,517 | 2,294,335 | 1,183,085 |

With 128 entries, the probe's `moveStake` in a transaction needed a limit of
1,228,031; sent with one gas less it failed and used 1,209,538 gas, all of the
gas forwarded to the precompile and the probe's 1/64 held back less its
revert.

### vault2's calls

vault2 (`evm/src/STSettlementVault2.sol`) names no gas on any call, so each
precompile call receives all but 1/64 of the gas left (EIP-150). It has no
`gasleft()` arithmetic and no try/catch: any refusal of a precompile call
reverts the vault2 call, and a claim stays claimable.

- **`moveStake`** in `_moveToEscrow` (`:611`), from `captureEmission` and
  `absorbStake`, and **`transferStake`** in `_payFromEscrow` (`:627-628`),
  from `claim`. On 478 each is admitted with less gas, never more, and
  charged 625 gas more. A transaction that succeeded on 477 with a given gas
  limit succeeds on 478 with it: R-1 to R-7 pass on both with the same
  bytecode. A transaction sized for 478 at a short list can run out of gas
  if third parties lengthen vault2's list before it is included, which no 477
  transaction could (below).
- **`getStake`, `getMinerCollateral`** (`:665`, `:683`), views: 4,748 and
  1,013 gas inside a contract, as on 475 and 477.
- **`registerLimit`, `getUid`** (`:554-556`), through the neuron precompile:
  unchanged; R-1's `escrow2-register` used 1,463,833 gas on 475, 477 and 478.
- **§7.3's refused-call fact.** A precompile call the runtime refuses still
  consumes all the gas forwarded to it, as measured above and by the smoke
  test (a refused transaction used 9,844,446 of its 10,000,000 gas on 477 and
  478). The claim batcher's design rests on it, and it holds.

`STSettlement2` (`evm/src/STSettlement2.sol`) also names no gas and has no gas
arithmetic; a close is two nested calls, `STSettlement2` to vault2 to the
precompile. `STClaimBatcher` (`evm/src/STClaimBatcher.sol`) forwards exactly
`gasPerClaim` to each claim inside try/catch, after checking `gasleft()`
against `gasPerClaim + gasPerClaim / 63 + 20,000`. On 478 a claim given the
default 2,600,000 needs less of it, so every item that paid on 477 pays; a
batch's least limit is still set by that check, and a refused item still
consumes its cap.

Each R test other than R-1 deploys the contracts built by `forge` from this
tree. vault2's creation bytecode is byte-identical to the release catalog's
(`sim-testnet/contracts_gen.go`; 13,396 bytes, SHA-256
`8a35e9818d4c813cb1add53f84323c79bc2383889d7b56dc616defe119110fd2`; 12,815 bytes
of runtime code, SHA-256
`789ca577fea787b5157349f55e6359eeadb1c962018d0f2c41f0ed97808c09dd`).
`STSettlement2`'s and the batcher's differ from the catalog's only in the
32-byte metadata hash of their CBOR trailer; R-1 deploys the catalog's bytes.

### vault2's own staking-hotkey list

On 478 the gas a capture, `absorbStake` or a claim needs grows with vault2's
own `StakingHotkeys` list, 8,750 gas an entry at the precompile. Third
parties can append an entry with any transfer of stake to vault2's coldkey
above the floor, until the list holds 128 (`stake_utils.rs:1865-1874`), as on
477; vault2's own moves face the 256-entry cap and go only to the escrow
hotkey; emission credits re-add the pool hotkey without a cap. A single pool's
vault2 therefore holds at most about 130 entries (128, the pool hotkey and the
escrow hotkey), and at most 131 once it holds stake on the v1 escrow hotkey in
phase C. A list whose position on a hotkey is moved away to nothing loses that
entry again.

The restoration migration cannot raise that bound. It appends a hotkey only
for a share row or watermark of that coldkey that lacks its entry: a position
vault2 actually holds, whose entry a correct index already has. vault2 is
created on 478, after the alpha-key retention fix of 473 that the migration
repairs, and every path that gives it stake keeps the index. A claimant's list
can grow by restored entries of the claimant's own positions; a claim to a
coldkey whose list then holds 128 without the escrow hotkey is refused
`TooManyStakingHotkeys` and stays claimable, the refusal R-3 already records.
The walk a claim is charged for is vault2's list, never the claimant's.

R-3 filled vault2's list to 128 through third parties. On 477 a claim then
used 1,256,728 gas against 154,206 with one entry and needed a limit of
2,411,058 either way; on 478 it used 1,257,365 against 154,831, and needed
1,273,292 against 159,817. `absorbStake` used 1,220,874 and 1,221,499 and
needed 2,374,168 and 1,245,279. A claim estimated at 173,484 gas before the
list filled, signed at that with 20% (208,180) and sent after, ran out of gas
and left the account claimable; signed at the claim tools' 2,600,000 it paid.
The §17 risk row is unchanged in cost (about 1.26M gas a claim with the list
full) and changes in kind: on 477 a claim needed 2.4M whatever the list, on
478 its need follows the list.

## Runtime rehearsal on 478

The R battery (`VERSION2-FINAL.md` §15.8) ran on 2026-10-13 on the node above,
from this branch: the whole package at `2b11bf9a`, whose harness sends each
measured transaction with the least gas limit at which an `eth_call` of it
succeeds and records that limit beside the gas used and `eth_estimateGas`'s;
the admission test at `15e8d070`; R-3 and R-7 again at `54b99122`, with the
claim tools' floor; and R-1 again at `a0a8814e`.
The same harness, pinned to spec 477, ran on the 477 rehearsal node of the 477
review (Wasm `3cfabd8d…`), which gives the 477 column below. Each test starts
its own node and reaches only loopback.

| Test | Result on 478 |
| --- | --- |
| R-1 `TestR1SettlementV2Phase` | Pass, twice: before this branch admitted 478 the approval named 475's source, and after, the reviewed source 850f2755 with the node's version 478 |
| R-2 `TestR2Capture` | Pass |
| R-3 `TestR3Claim` | Pass, with the pre-signed claims above |
| R-4 `TestR4Registration` | Pass |
| R-5 and R-6 `TestR5Settlement` | Pass |
| R-7 `TestR7ClaimBatch`, `TestR7MaxGasDoesNotDependOnCalldata` | Pass |
| `TestRehearsalSmoke`, `TestRehearsalTransientStorage`, `TestRehearsalStakeExitAdmission` | Pass |

The behaviour is the same in every case the battery checks: a close moved
exactly the pool's stake into escrow, and exactly `getStake − locked` with a
collateral lock, 38,742,541,778 of 40,223,672,009 rao; dust was refused and
captured later; the floor refused 11,092 alpha rao and took 11,093 at price
9.014779842; a claim to a coldkey at the staking-hotkey cap was refused and
stayed claimable; a transfer of conviction-locked alpha to vault2 was refused;
2^64 − 1 moved a whole position; the escrow hotkey earned 3,595,812,938 rao
over two tempos without weights and nothing while a validator set them; and
a transient write still survived a reverted call frame.

Gas at depth 29 (R-6, R-7), used and the least limit that succeeds, with
`eth_estimateGas`'s limit where the plan cites it. vault2's list held one or
two entries, as it will at launch. Figures that differ by a multiple of 12 gas
differ by calldata bytes, the runs drawing new keys.

| Transaction | 477 used | 477 least limit | 478 used | 478 least limit |
| --- | ---: | ---: | ---: | ---: |
| close, capturing (estimate 2,688,632; 246,899) | 225,430 | 2,513,577 | 226,055 | 236,399 |
| close, nothing to capture | 126,610 | 133,826 | 126,610 | 133,826 |
| close deferring 1 epoch | 131,633 | 141,343 | 131,633 | 141,343 |
| close deferring 144 epochs | 2,409,622 | 2,862,246 | 2,409,622 | 2,862,246 |
| commit | 205,288 | 214,491 | 205,288 | 214,491 |
| finalize | 94,389 | 100,822 | 94,389 | 100,822 |
| answer (`checkPath`) | 149,050 | 153,987 | 149,026 | 153,963 |
| approve | 132,524 | 134,165 | 132,524 | 134,165 |
| demand | 98,268 | 103,264 | 98,256 | 103,252 |
| claim, vault2's first | 186,706 | 2,443,533 | 187,331 | 192,317 |
| claim, an account's first (estimate 2,572,776; 206,967) | 186,707 | 2,443,534 | 187,332 | 192,318 |
| claim, an account's first after its neighbour's | 169,595 | 2,426,422 | 170,208 | 175,194 |
| claim, a repeat | 169,618 | 2,426,445 | 170,243 | 175,229 |
| R-7 single claims, five | 169,992 to 187,126 | up to 2,443,953 | 170,605 to 187,740 | up to 192,726 |
| batch of 1 | 180,914 | 2,704,269 | 181,539 | 2,704,269 |
| batch of 5 | 783,479 | 3,319,241 | 786,592 | 3,321,729 |
| batch of 10 | 1,509,004 | 4,058,765 | 1,515,302 | 4,064,438 |
| batch of 20 | 2,972,370 | 5,550,132 | 2,984,642 | 5,561,779 |
| batch of 32, the most the tools batch | 4,728,002 | 7,339,364 | 4,748,110 | 7,358,847 |
| the most one transaction holds | 61,205,925 (418) | 64,898,177 | 61,175,473 (416) | 64,861,477 |
| batch of 4 with two refusals | 2,974,627 | 5,502,107 | 2,975,960 | 5,502,815 |

Every transaction that moves stake uses 625 gas more on 478, a batch about 625
more per claim (148,378 per claim in a batch of 32, against 147,750), so one
transaction holds 416 claims instead of 418. Its least limit falls from about
2.4M to about 192,000 for a claim and from about 2.5M to about 236,000 for a
capturing close, while the list stays short. Nothing else changes: commit,
finalize, answer, demand, approve and the closes that move no stake are
identical, batches remain sized by the batcher's per-item cap, and the R-1
deployment used 2,821,690, 1,463,833, 1,935,622, 310,486, 31,329 and 285,209
gas of its six approved limits (4,000,000, 2,500,000, 3,000,000, 1,000,000,
150,000 and 400,000), the 475 figures. Outside depth 29: R-2's captures used
231,443 and 214,343 gas (230,818 and 213,718 on 477), R-4's
`registerOperatorPool` 1,594,088 and `registerPoolUid` 1,449,949 on both, and
R-4's `absorbStake` 127,749 (127,124). In the smoke test `moveStake` and
`transferStake` used 82,378 and 79,946 gas inside the probe (81,753 and
79,321), and `eth_estimateGas` gave their transactions 125,455 and 122,947
(2,427,799 and 2,420,524).

## Gas limits on 478

**What changed for senders.** On 477 the limit a stake-moving transaction
needed was fixed, about 2.4M for a claim and 2.5M for a capturing close,
whatever vault2's state, so a limit taken from `eth_estimateGas` stayed
sufficient. On 478 it follows vault2's list: about 8,750 gas an entry at the
precompile, 8,889 at a claim's transaction and 9,030 at a close's (two nested
calls) once the list is long enough for admission to bind, a few dozen
entries, and the 8,750 the walk is charged below that. Anyone can add entries between
an estimate and the transaction's inclusion. A sender that signs the estimate
plus a proportional margin covers about four entries for a claim (20% of
about 190,000) and five for a capturing close; a griefer who adds more makes
the transaction run out of gas.

**Sizing rule.** A stake-moving vault2 transaction is never signed below the
size it needed on 475 and 477: a claim, a batched claim and `absorbStake` at
the larger of its estimate with margin and 2,600,000
(`onchain.ClaimBatchGasPerClaim`), and a capturing close at the larger of its
estimate with 20% and 3,200,000. On a 477-style runtime the estimate already
exceeds the floor, so nothing changes; on 478 the floor covers every list
length up to the runtime's cap, where a claim at depth 29 needs 2,443,953 and
a capturing close 2,513,577 again. Only the gas used is paid.

| Sender | Sizing | Exposed on 478 | Status |
| --- | --- | --- | --- |
| `snclaim v2 claim` and the claim daemon's single claim | `miner/onchain/eth.go` `runTx`: estimate + 20% | Yes | Floor in this branch (`d49eabf1`); R-3 shows it on the runtime |
| The daemon's rebroadcast of signed bytes | `miner/claim_daemon.go` preflight at the signed limit | Yes: an outgrown claim would never be resent | Covered by the floor (`TestClaimV2SignedClaimOutlastsAGrownStakingHotkeyList`) |
| `snclaim --batch` and the daemon's batches | each item capped at max(2,600,000, its estimate); the batch at the least `eth_call` limit + 20% | No | Unchanged |
| Validator approve, challenge and demand; `snclaim v2 check` | `validator/settlement_v2_sender.go` via `runTx` | No: no stake moves | Unchanged |
| Server close | `server/controller/st_controller.go:1086`: estimate + ceil(20%); a replacement reuses the first limit (`:1066-1068`) | Yes, when it captures | Floor of 3,200,000 for a v2 stake move on server `main` (`54ae2aacf`) |
| Server commit, finalize, answer | same engine | No | Unchanged |
| SDK claim | `sdk/sn_ledger_claim.go:298-303`: estimate + 25% | Yes | Floor of 2,600,000 on SDK `main` (`3bc7ec95`) |
| `absorbStake` | no tool sends it (phase C) | Yes | Sign it at 2,600,000 or more |

**The bound of the close floor.** A close that defers `k` missed epochs before
capturing needs about 19,027 gas of limit for each (measured: the closes
deferring 1 and 144 epochs above). With vault2's list at `n` entries, in the
admission-bound range, it needs 143,318 + 19,027·k + (64/63)²·(65,517 + 8,750·n)
gas, the constant fitted to 477's capturing close, where admission bound
(2,513,577 at 2,296,767 admitted). The same model, with one nested call and
its constant fitted on 477, predicts R-3's claim and `absorbStake` at a long
list on 478 to within 12 gas. At n = 128 that is
1,366,770 + 19,027·k, and 3,200,000 covers k ≤ 96; at 130 entries k ≤ 95,
and at 131 k ≤ 94. So the floor is safe against any list growth for a
capturing close that defers at most 94 epochs. A close that defers more,
after at least 94 epochs without a close, is not covered when third parties
fill vault2's list between its estimate and its inclusion; above about 290
deferred epochs the estimate's own 20% covers the growth again. For such a
close the operator signs it at no less than 1,400,000 + 19,100·k gas, which
covers 131 entries. The root key can send that up to the re-signed policy's
`maximum_gas` of about 3.5M, k ≤ 109. Beyond that anyone can send it from any
funded key with that limit, as for every close the policy does not cover
(§8.1), up to the 64,998,500 gas a transaction may name, k ≤ 3,329.

**Hard-coded figures.** Before activation these matter:

- The contract phase's approval must name the 478 runtime: source 850f2755
  with version 478, `:code` hash `0x7e722b3a…`, metadata hash `0x4f96a9cb…`.
  The settlement v2 phase refuses a reviewed source named for another version
  (`TestEvmV2PhaseBindsReviewedSourceToItsSpecVersion`), so it refuses 477's
  source on the 478 runtime; this branch admits 850f2755 at 478.
- Before the first close: a server build with its close floor (server
  `main` at `54ae2aacf` or later), and the operator gas policy re-signed as
  `VERSION2-LAUNCH.md` O13 already plans
  (`maximum_gas` about 3.5M, intent liability at least that at the 30 gwei
  fee cap). The proposed `server/local/sn25golive/sn25-mainnet-gas-params.json`
  (`maximum_gas` 1,000,000, intent liability 5·10^16 wei) refuses a close
  signed at the floor. The server's native-fee denomination binds the runtime
  code (`server/model/st_native_fee_settlement_model.go:124`), so one signed
  for 477's code settles nothing executed by 478.
- Before the first claims: an SDK and app release with the SDK's claim floor
  (SDK `main` at `3bc7ec95` or later). snclaim's and the daemon's are in this
  branch.

Stale numbers that fail nothing: the "gas limit needed" figures of §7.3, §8.1,
§11 and §17 of `VERSION2-FINAL.md`, of `VERSION2-LAUNCH.md` §2.6 and of the
whitepaper (each true on 477; under the floor the sizes they lead to, about
3.2M for a close, 2.6M for a claim and 0.02 TAO on a gas key, are still the
sizes signed); the 475 measurements of `scripts/runtime-rehearsal/README.md`,
which its runtime 478 section supersedes; the in-repository chain fake of the
end-to-end tests (`mainnet/settlement_v2_e2e_chain_test.go:102`, a declared
2,366,000 and 2,359,000 gas for the two calls, and
`mainnet/settlement_v2_e2e_fixture_test.go:968-978`, which asserts an estimate
near 2,634,202, a refusal at 1,000,000 and use under a fifth of the estimate);
and the SDK's test fixtures (`sdk/sn_ledger_claim_test.go:211,854,912,1104`).
The batch constants hold: 2,600,000 a claim, 32 claims and 64,998,500 gas.

## SN consumers

No interface that SN consumes changed its encoding. The 477 review's consumer
table holds for 478 row by row: apart from the files listed above, every
source it names is byte-identical to 5c6e83e2, and `stake_utils.rs` and
`remove_stake.rs` change only by the added helpers. The extrinsic format, the
thirteen signed extensions and transaction version 1 are unchanged; the
`CheckMetadataHash` digest covers the spec version, so a mode-1 signer must use
478's metadata.

SN's own checks pass on the exact 478 metadata, on this branch:

- `TestRuntime478ConsumedInterfacesMatchReviewedBaseline` compares the CRv4
  producer's consumed storage, calls, events and signed extensions, and all
  seven fleet purposes, with the reviewed 467 baseline.
  `TestRuntime478ValidatorProducerCapability` authenticates the exact artifact
  over a read-only transcript of the node's version reply and binds the
  validator producer purpose.
- `TestNativeOwnerRuntime478MetadataCapabilities` runs the native owner,
  passive root, treasury, census, owner-trim and root-registration profiles
  that the 477 metadata passes, and
  `TestNativeOwnerRuntime478MetadataChangesOnlyTheSpecVersion` pins the
  one-byte delta.
- `TestTakeCallsMatchReviewedRuntimeLayouts` and
  `TestTakeDelegateRateKeyMatchesReviewedRuntimes` keep the take calls and the
  delegate-take rate key.
- The successor tests for treasury, owner recycle, passive root, discovery and
  root capabilities, the treasury migration-completion test and the root
  registration test run for 478 beside 473, 475 and 477.

## Limits and assessment

This record does not establish:

- a source-to-Wasm rebuild, exact or partial;
- independent finality: genesis, blocks and storage are two public nodes'
  replies, and the owner's own snapshot is the check of record;
- the end of the staking-hotkey restoration, or the entries it restores, which
  were still being written when this record was made;
- a measured capturing close at a long list or with many deferred epochs: the
  bound above is derived from measured parts;
- SN25's stored hyperparameters, metadata v15, or GitHub's release records
  beyond the downloaded assets' bytes;
- anything about a later runtime.

Raw captures were made locally and are not retained by this record, except
the 478 metadata that the test fixture carries. Every input is public, and the
listed sizes and digests identify each one.

**Assessment.** The runtime that the public nodes report on mainnet since
block 9,276,925 is byte-identical to the official v478 release asset, which
the release's srtool digest attributes to exact source commit 850f2755. The
upgrade call that installed it carries the same bytes, and executing that Wasm
reproduces the pinned metadata hash. From 477 to 478 the metadata changes in
one byte, the spec version; the encodings of every call, event, storage item,
constant, runtime API and signed extension that the validator, the contracts,
the operator server and the launch tooling consume are unchanged. The
behaviour change that reaches settlement v2 is the admission of `moveStake`
and `transferStake`: they need the gas of a walk of the caller's own
staking-hotkey list instead of a 256-entry one, and use 625 gas more. vault2
names no gas, does no gas arithmetic and catches no precompile failure, and
a refused precompile call still consumes the gas forwarded to it. Settlement
v2's exact bytecode passed R-1 to R-7 on nodes built from the 477 and the 478
source, so the same vault2 bytecode works on both, and no transaction that
succeeds on 477 fails on 478 with the same gas limit. What 478 changes is that
the limit a stake-moving transaction needs follows vault2's list, which third
parties can lengthen after an estimate; senders that never sign below the
475-era sizes are safe on both runtimes. Gate 2 therefore holds for runtime
478. An approver who binds this record's SHA-256 accepts this
artifact-identity basis without a source-to-Wasm rebuild, or must first obtain
one. Independent finality, signing custody and activation remain separate
gates.

[release]: https://github.com/RaoFoundation/subtensor/releases/tag/v478
[compare]: https://github.com/RaoFoundation/subtensor/compare/5c6e83e2f4936f257234f246957f75684c35227c...850f2755bb05a3851f948489f0ab27b0645cebce
[extensions]: https://github.com/RaoFoundation/subtensor/blob/850f2755bb05a3851f948489f0ab27b0645cebce/precompiles/src/extensions.rs
[staking]: https://github.com/RaoFoundation/subtensor/blob/850f2755bb05a3851f948489f0ab27b0645cebce/precompiles/src/staking.rs
[restore]: https://github.com/RaoFoundation/subtensor/blob/850f2755bb05a3851f948489f0ab27b0645cebce/pallets/subtensor/src/migrations/migrate_restore_staking_hotkeys.rs
