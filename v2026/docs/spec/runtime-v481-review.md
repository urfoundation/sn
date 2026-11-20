# Runtime 481 consumed-interface review

Review checkpoint: 2026-10-19. **The local runtime 481 rehearsal qualification
for gate 2 passed** for the pinned source and artifacts below: source review,
build guards, API/metadata regressions, captured-Wasm interface execution, all
eleven rehearsal-package roots and the separate R1 deployment rehearsal.
**The broader 96-phase release gate has not passed, and production owner
approvals remain pending.** This document grants no deployment, activation,
migration-completion or signing authority.

**Observed runtime and release.** The read-only snapshot was captured at
`2026-10-19T20:45:08.153250+00:00`. Its node reported finalized Finney block
`9320004`, hash
`0xad1eba66370988bd104b88f5d8e72053a9623fcd00ed7fa1fed7f1a248814ec3`,
and runtime `node-subtensor/481/1/1` (spec/transaction/state versions; the
`systemVersion` alias is also 1). The complete reply contains 25 runtime APIs.
This is a retained node observation, not a light-client finality proof or a
fresh observation at deployment time.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Captured chain Wasm | 2,652,697 | `48225f3199bf317966d0214588bc45fb49c28c3e7bd510293bb4da7d6aea224b` | `5fb504c1c793a3ddb5456f189ddef98b5146853aea6ff998501f1b6c6eb720bc` |
| Captured metadata v14 | 364,065 | `4dc0d25bc11d1ea15118b433ba34e78f40ae2bfbaa188757e6f67f0a3240266a` | `40db5b7feac3a8e9ab2ab000234257428826d9827ab326034a3329fdd0d50088` |

The [official v481 release](https://github.com/RaoFoundation/subtensor/releases/tag/v481)
publishes the same Wasm SHA-256 as the captured chain artifact. Its source tag
names commit
[`6a972693ae9580541225bd37696ac2fee26b8ae9`](https://github.com/RaoFoundation/subtensor/commit/6a972693ae9580541225bd37696ac2fee26b8ae9),
tree `587abd7462d0cee0de4d0ec8bdf0df6a71cd947f`. The release page's proposed or
prerelease label is not evidence of current chain state.

The [source manifest](runtime-v481-source.sha256) contains 211 independently
checked files and has SHA-256
`b01ff658726aeb6fcae2acfc78fa503f40cfeb778e4cd8538395dbc9087a2a1b`.
It retains all 206 predecessor entries and covers all 39 changed build paths,
including five newly added files. The source checkout was clean. The comparison
baseline is runtime 479 commit
`e66c7eed760efaa5a3b1c249b5a3369a9bc693cf`, tree
`b44c231f8dc95ba2f4e6e1bac6e377e387e47677`.

**Consumed API and metadata.** `SubnetInfoRuntimeApi`, ID
`0x8375104b299b74c5`, changes from version 2 to 3. Its only new method is
`get_subnet_emission_weights(NetUid) -> Option<SubnetEmissionWeightsInfo>`.
The SN-consumed `SubnetInfoRuntimeApi_get_selective_metagraph` method retains
its signature and delegate. Its complete 77-field SCALE layout is unchanged;
the metagraph source blob remains
`a627a4eff3313ca64691c21d94540e6622ab7c49`, and the existing layout fingerprint
remains `bb7420226d39c0eb`. Extraction of the global TaoWeight normalization
helper preserves the arithmetic, parent/child treatment and per-asset
truncation used by weighted stake.

The compatibility change admits API3 only with the exact decoded
`node-subtensor/481/1/1` tuple. API2 keeps its existing behavior. Other consumed
API versions, malformed entries and duplicate consumed API pairs are refused.
The producer wrapper also retains its existing duplicate top-level `apis`
rejection; this review does not claim that behavior for every shared-helper
caller. Each caller still checks its authenticated block artifact, consumed
metadata interfaces and transport or finality ownership. Production successor
admission still requires the separately signed opt-in, and the selective
metagraph decoder still enforces all 77 fields.

The [metadata fixture](../../crv4/testdata/runtime481-metadata.md) is byte-exact
to the captured metadata. The full structural oracle compares 1,599 predecessor
items with 1,616 current items: 17 additions, 25 changes and no removals. The
additions are one admin call, twelve errors, one event and three storage items.
The changed items are expanded RuntimeCall/RuntimeEvent wrappers,
`System.Version` and the appended `EmissionWeights` rate-limit variant. Every
other addition, removal or changed shape is refused. The extracted shared
renderer and the existing 479-versus-478 oracle retain their prior behavior.

**Economic scope.** Runtime 481 is not globally equivalent to 479. The source
review records these changes explicitly:

- A per-subnet five-slot vector, ordered burn/recycle/owner/validator/miner and
  normalized to `u64::MAX`, replaces the legacy owner flag/global percentage.
  The new setter enforces zero burn/recycle slots, owner/validator/miner bounds,
  a lease owner floor and the exact total.
- Root receives the minimum of the current validator share and the default
  validator share multiplied by the live globally weighted root proportion,
  before the remaining allocation. Off-default allocation therefore changes;
  metadata compatibility alone cannot establish unchanged payouts.
- Migration seeds the old owner setting and accrued-owner history. Persistent
  lease-origin knowledge covers currently mapped leases, without reconstructing
  all expired historical leases. Dissolution refunds now account for accrued
  owner-cut alpha, with a legacy fallback when that history is absent. Lowering
  today's owner slot does not erase past owner accrual.
- Admin calls 28 and 92 retain their indices and argument encoding but return
  `Deprecated`; new call 114 sets the emission vector. The SN-consumed admin
  call set contains none of these three calls.

The reviewed registration, weights, commitments, epoch-distribution and
recognized-owner recycling paths remain unchanged. This does not erase the
changed allocation before epoch distribution. SN's `ProviderShare1/10` is a
submitted weight-row fraction, not a fixed fraction of total alpha or miner
emissions. The legacy `OwnerCutEnabled` and `SubnetOwnerCut` observations are
seed/history fields, not authority for the current 481 vector; the economic
denominator remains explicitly unresolved.

Source catalog membership only binds a reviewed commit to a spec version.
Root capability reports remain planning-only and do not prove the deployed
source, current runtime, activation readiness or signing permission. Treasury
migration still needs its original completion marker, code and scope proofs.
Policies, approvals and prepared transactions for 479 do not become 481
authority through this catalog addition. The source review covers SN's
consumed interfaces and identified semantic changes, not the entire runtime's
security.

**Closed regression evidence.** The baseline run in session `94336` retained
two expected failures and five passing roots. Both failures reached the actual
API2-only refusal: the direct producer declaration check and the production
successor path with the captured 481 artifact. The final run in session `44301`
joined with exit 0 (Root result chunk `f7d8c6`) using `-race -count=1`,
`GOMAXPROCS=2`, `GOFLAGS=-p=2` and a 20-minute package timeout. Exactly 24 named
roots and 22 nested negative cases passed, with no failure or skip. All 19
pinned source files remained unchanged. Package results were CRv4 5.023s,
validator 61.256s and mainnet 122.272s.

That selection covers the exact API exception, complete metadata delta,
captured production successor, authority/interface refusals, catalog binding,
independent policy and migration proofs, and two retained API2/consumed-profile
controls. These focused results do not substitute for the rehearsal battery.

**Built node and captured-Wasm execution.** Session `43737` joined exit 0
(chunk `20b5a0`) after the pinned source node build with Rust 1.89,
`--locked --profile release` and features `metadata-hash fast-runtime`.
The three deterministic build-script controls also passed in session `83194`
(0.420s): reviewed pins reach only the Cargo sentinel, an old 479 tag tree is
refused before Cargo, and changed source bytes fail the actual manifest guard.
The node SHA-256 is
`cf5e271549c5e9ea5cb4c6eb8ea2ec2610aa6bcc08a423f9343ed5f5618b0961`.
Its built compact Wasm SHA-256 is
`dbd9fe4eac85aea63092f220af4f77b3405ca908100d5d4fa2721e1d0bac4461`.
The production workflow instead uses its srtool production profile with
`metadata-hash`. **Independent source-to-production-Wasm reproducibility has
not been proved.** The fast-runtime build is a distinct rehearsal artifact;
matching the published and captured production hashes does not close that gap.

Session `93353` joined exit 0 (chunk `1e9a34`) for local execution of the exact
captured chain Wasm. The harness first verified that the raw dev genesis
contained the built Wasm, then replaced only `:code` with the captured bytes.
The pinned node uses `WasmExecutor`, without a native-runtime fallback. A fresh
base path, loopback RPC/P2P listeners, safe RPC, zero peers, no authoring,
telemetry or offchain workers isolated the readback. At the same local genesis
block, the raw code hash, complete runtime-version/API object and all 364,065
metadata bytes matched the capture. Latest-before and latest-after remained
that genesis block.

Both owned commands joined exit 0; the node received SIGTERM, no SIGKILL was
needed, and neither owned process group had remaining members. Source and
input checks before and after matched. The raw node log retains
``Essential task `txpool-background` failed. Shutting down service.`` at 21:13:32,
the final shutdown second. Its exact order relative to SIGTERM is not recorded.
Completed interface readbacks and owned cleanup are established; an error-free
raw log or sustained node health is not claimed. The receipt deliberately
classifies only `LOCAL_CHAIN_WASM_INTERFACE_PASS`, records zero mainnet writes,
and leaves `gate2_complete` and `deployment_authority` false.

**Closed rehearsal results.** Session `16800` joined exit 0 (chunk `9947ce`).
The complete `./rehearsal/` package passed in 351.759s: all eleven source-declared
roots and 28 nested cases, with no failure, skip or panic. Its ten node-start
lines all name `node-subtensor/481` and source
`6a972693ae9580541225bd37696ac2fee26b8ae9`; the ledger-only root needs no node.
This includes smoke, transient storage, stake-exit admission, ledger/Merkle,
R2 capture, R3 claim, both R4 registration roots, R5 settlement with R6, and
both R7 batch/gas roots. Logged `AmountTooLow`, one-gas-below and stale-gas
transaction failures are asserted negative controls, not failing test outcomes.

The separate `TestR1SettlementV2Phase` in session `95789` joined exit 0
(chunk `06e2b5`), taking 26.93s (package 26.993s). Its node also identifies the
exact 481 source. All six actions completed with canonical local receipt and
contract-state readback; vault creation and escrow registration share block 32.

| Local R1 action | Block | Gas used | Approved test limit |
| --- | ---: | ---: | ---: |
| `vault2-create` | 32 | 2,821,678 | 4,000,000 |
| `escrow2-register` | 32 | 1,463,833 | 2,500,000 |
| `settlement2-create` | 33 | 1,935,622 | 3,000,000 |
| `proxy2-create` | 34 | 310,486 | 1,000,000 |
| `vault2-link` | 35 | 31,329 | 150,000 |
| `batcher-create` | 36 | 285,209 | 400,000 |

Both invocations used `-tags runtime_rehearsal -count=1 -v`, `GOMAXPROCS=4`,
`GOFLAGS=-p=2`, the pinned built node and independent temporary dev chains.
Their package timeouts were 90 minutes and 30 minutes respectively. R1 used
synthetic custody and approval fixtures; it explicitly retained
`ActivationReady=false`. These local transactions provide no production nonce,
custody, owner approval or activation authority. The successful package harness
waits for its test-owned node processes during cleanup; it does not emit the
separate process-group census used by the captured-Wasm harness.

The battery's retained `source_manifest_sha256` field names the frozen
19-file source-pin command record `api481-fixed-race-command.json`, whose
SHA-256 is `6def9a0b6da59ac183bd6ca1ddea4a1f0cc06a2650fa3d5be883472d1945a5ab`.
The separate 211-file Subtensor manifest has the `b01ff658…` hash above.
Qualification prose was finalized after these runs; runtime and test code were
not changed to record their results.

**Retained evidence.** The closed records below are preserved in the workspace
package `temp/handoff-complete/deployment-20261019/runtime481/`. Their hashes
identify the reviewed bytes; the package is outside this repository.

| Record | SHA-256 |
| --- | --- |
| `snapshot.json` | `e91697433fe9d872e54d1e7def1bc248b27289598c8aa8b8a8a03182010e6c14` |
| `source-consumed-review-v2.json` | `30d4d8f4057b118c6675a1ff7eafb8355be32b9c89f7b8df8783724b5f82bc4b` |
| `api481-red-and-metadata.log` | `ea1db28ef916c433a8344fd262c809376cffa62523645de8b014290027e051d9` |
| `api481-fix-source.json` | `9738af94ad5eebbb23e0f9e163b4f6ab622693462f08b903c931a64261ec4ef3` |
| `api481-fixed-race-command.json` | `6def9a0b6da59ac183bd6ca1ddea4a1f0cc06a2650fa3d5be883472d1945a5ab` |
| `api481-fixed-race-result.json` | `beca3c5a1aafedef251f2b987e5332d5be141acd157a6dd94419270e0fc4e166` |
| `api481-fixed-race.log` | `53adb2cb68e87640cde6603dc5bb86ccdba1f452da5d1413a28fe6a0b2fbaa97` |
| `build-guard-controls-result.json` | `e7f221b29351e033aa0564ea4307853f4c3229515e10fb78ce4c5dca07046634` |
| `node-build-result.json` | `26f3e404b0e12494f1a33452c126584d74b42bac63d2278c46bf113da398c2b1` |
| `node-build.log` | `285f27965a899b0d944be20c90dbd9ad97fe3d5ac4264efca388ad8e3edeb651` |
| `execute_chain_wasm.py` | `08c1520bcff7d7dc433bfb88900d4ea208047a6d0398e91a0a133fbf5bf84974` |
| `chain-wasm-command-result.json` | `a8e81fadae163a0b7519a479b2994e32b14ec35d6846e45939427ec72f41a063` |
| `onchain-wasm-execution/receipt.json` | `af0c666a36942b8887149673a774cf83be9266f26dbcfe51dd57e03118cf71e9` |
| `rehearsal-battery-command.json` | `c6380d8fdb47d7b4e5e7961752d24ffd16a740eaee8b203668f210953f60a90c` |
| `rehearsal-battery-result.json` | `e6b75a3b29a85db82b79ffa469af756f2d42b6fa11b5807c17126a28c649c3f0` |
| `rehearsal-battery.log` | `70a8ca2e73df816ca74a3d5c9eea9090dc982ebdbefbdbd2556b6645d4063cc4` |
| `rehearsal-r1-command.json` | `6ae9d9228ce031370a1b1c86e97d6169116ca445046dd26e710a1f0514d72462` |
| `rehearsal-r1-result.json` | `3656a043c52ca7b23339bc7becf9211fc1e72e6af5cb896ef915738bf7c1f3b4` |
| `rehearsal-r1.log` | `caa9421b309b7b99a83811093d41d3164dc8878d05b0688a0121cc2a11ee22b0` |

These results close this runtime 481 gate 2 qualification only. They neither
credit the incomplete full release gate nor satisfy pending production owner
approvals. Fresh finalized runtime/code/metadata observations and the
deployment's separate approval requirements remain necessary before production
action. A changed runtime requires its own review and qualification; historical
479 or earlier results are not credited to 481.
