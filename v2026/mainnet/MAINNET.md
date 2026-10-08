# Mainnet launch and operations plan

## One operator and one validator — October 7

The owner decided that SN25 launches with **one network operator (UR) and one validator (`ur-mainnet`)**, and that the
coordinator's governance Safe starts with one owner. The code still supports the two-operator, two-validator and 2-of-3
forms with unchanged behavior and bytes; the launch uses the new one-operator, one-validator and 1-of-1 forms:
- **Policy.** The approved [`deploy/mainnet/policy-v1.yml`](../deploy/mainnet/policy-v1.yml), hash
  `0x6b188830b47e3b7dbfafc2839d7e1f460125115c9fbded053fa46293c79130a2`, sets `minimum_healthy_no_count` and
  `minimum_live_validator_count` to 1 and binds fleets for 12 epochs. Its first epoch lasts one day and the
  owner Safe schedules the weekly production cadence from epoch 1 ([LAUNCH.md](LAUNCH.md#produce-the-protocol-policy-hash)). On mainnet the weight-cap feasibility check counts
  the treasury's reserve recipients, so the 32768 cap holds with one operator. Testnet keeps the pool-only rule.
- **No independent cross-check.** The validator runs with `controlled_no_ids: []` and scores UR's own pool. A config may
  not list every configured operator there, because that would leave no weight to submit. Nothing independent checks
  UR's scoring at launch.
- **Stake.** A validator steers SN25 only while it holds more than kappa (50%) of validator stake. `ur-owner` signs one
  launch call: a `set_children` on SN25 that names `ur-mainnet` as the SN25 owner hotkey's child at 100%, so the owner
  hotkey's stake weight counts for `ur-mainnet`. No ownership or alpha moves, and there is still no coldkey swap.
  `bootstrap-chain readiness` keeps `EFFECTIVE_STAKE_MAJORITY_UNVERIFIED` on the sole validator.
- **Evidence.** With one configured operator, that operator's own server holds the only release V2 evidence replica.
- **Reserve-only row.** On the mainnet schema-3 treasury path, an epoch whose provider allocation is genuinely empty
  submits a row that gives each approved reserve recipient 1/n (1/2 each with two) and providers nothing, marked
  `reserve_only_empty_provider_allocation`. Every verifier rederives it, and mainnet conformance accounts for those
  tranches. The owner-recycle preview planner still refuses an empty allocation.
- **Bootstrap.** A fresh preparation uses `urnetwork-mainnet-bootstrap-chain-config-v5` with one UR role, `sole`
  ([BOOTSTRAP-CHAIN.md](BOOTSTRAP-CHAIN.md)). The passive root seat may share the sole hotkey only under the same
  coldkey. The two-unit `activate-validators` tooling refuses v5 and is not the launch path: the validator runs as
  `validator run` through xops on snow ([LAUNCH.md](LAUNCH.md#run-it-on-snow)).
- **Governance Safe.** SafeL2 1.4.1 `0x56F4Dad575576CC0B679FEf52899630f9F605418`, owner
  `0x16C372dbBb24cd8473345ab13971E40814C8658F`, threshold 1, created by the relayer in block 9,229,341. Only the
  execution request schema `urnetwork-mainnet-successor-execution-single-owner-request-v1` selects this profile, with one
  65-byte EIP-712 signature ([successor execution](BOOTSTRAP-SUCCESSOR-EXECUTION.md)). Owners can be added and the
  threshold raised later by a Safe transaction.
- **EVM roles.** Brien's Ledger (Ethereum app) holds the deployer `0xA9D4A6a331F59942BD7389a5402120D69047C090`, the Safe
  owner, the guardian `0x450C14EA62F76F11630780e194E01F9f524BAbFd` and the Safe relayer
  `0x81E925DAEC15cb334d4b9FD90e9c4f10025eB666`. The commitment oracle `0x56Ddfb8f3E267E98EfDa690110645f31365BCF03` is a
  service key in `vault/main/sn.yml`; no mainnet oracle service exists yet.

Older passages below that require two UR validators, two healthy operators or a 2-of-3 Safe keep their dated scope; for
the launch, this decision supersedes them.

## Owner trim decision — October 7

The owner decided that the SN25 launch performs **no owner trim**.

Old miner registrations remain, and ordinary registration pruning removes them:
- **No payouts today.** At finalized block 9,228,211, read from Snow's Finney archive, 1 of SN25's 256 UIDs received
  miner incentive, and all 14 permitted validators weighted only that UID.
- **Payouts stay at zero.** Zero weight is enough to keep old miners at zero payout while UR's validator holds the
  consensus stake.
- **Pruning takes them out first.** A new registration on the full subnet replaces the lowest-ranked registration past
  its immunity, which is a zero-emission old miner.

A trim would have added immediate slot reclamation and a hedge against other validators re-weighting old miners. It
would have cost four independent approvals, two multisig Ledger signatures, a best-effort risk policy, a
protected-identity timing window, and permanently lower capacity.

The bootstrap preparation still carries its census and protection set. Its trim-execution phase stays unexecuted and is
recorded in the readiness receipt. Our zero-emission keys are registered close to launch, inside their 21,600-block
immunity, so UR's weights reach them before ordinary pruning could:
- the two `ur-reserve` recipients;
- the `ur-mainnet` validator hotkey.

The multisig owner-trim tooling (`OWNER-SIGNING.md`) remains available if a trim is ever selected.

## Root validator decision and take checkpoint — October 6

The owner set the root validator's terms ([operator discovery design](../docs/OPERATOR-DISCOVERY.md), sections 1.6 and 7). For our own hotkey this supersedes the passive, observation-only root strategy in [Root validator on netuid 0](#root-validator-on-netuid-0); the reviewed `root-register` workflow and its custody rules keep their scope.

- **18% takes.** The validator hotkey sits on root (netuid 0) and validates SN25. Its delegate take and its SN25 childkey take are both 18%, 11,796/65,535: `MaxDelegateTake` and `MaxChildkeyTake` default to 11,796 in runtimes 455 through 473, and the take commands refuse anything above the live maximum.
- **Child hotkeys from anyone.** Anyone, for example through tao.com, may name our hotkey as their child on SN25. The parent's coldkey alone signs `set_children`; we keep no allowlist and approve nothing.
- **Deployment.** The validator runs on snow as a systemd service installed by `xops/main/ansible/run-validator.sh`, which builds the binary locally the way `run-edges.sh` builds `warpctl`. The unit runs `validator run --config=<path> --progress-file=<path> --durable-volumes=<path> --durable-volumes-sha256=<hash> --operators-refresh=1h`.
- **Weights.** The validator weights only the operators pinned in its signed release config. `--operators-refresh` reports drift between `ur.xyz/operators.yml` and the pinned operators; it never changes weights, evidence or protocol state.

- **Dedicated hotkey** (owner decision, October 6). The validator hotkey is the dedicated `ur-mainnet` hotkey, not the SN25 owner hotkey, and the SN25 owner Ledger signs none of its operations.
- **Multisig coldkey.** Its coldkey is the `ur-mainnet` 2-of-3 native multisig `5C9z2rXL1WFLVF78EVg7LZJ8zSi4FheXmbj8omrVhRZCxnQ3`. The signatories are `brien-ur-mainnet` (Brien's Ledger, `m/44'/354'/10'/0'/0'`), `jack-ur` and `keith-ur`, the same pattern as the other `ur-*` multisigs. Registration, stake and take changes are multisig calls; `validator take status` reads them back.

One runtime consequence needs its own step. Unless auto parent delegation is disabled first, `root_register` makes the hotkey the full-weight parent of every subnet owner hotkey, SN25's included. Our SN25 stake weight would then go to the SN25 owner hotkey, which is ours but doesn't run this validator. The validator's coldkey disables it with `set_auto_parent_delegation_enabled(hotkey, false)` before root registration. [LAUNCH.md](LAUNCH.md#root-validator-on-netuid-0) has the sequence.

**Code checkpoint.** `validator take status`, `take set` and `take childkey` (`validator/take.go`) sign `decrease_take`, `increase_take` and `set_childkey_take` with the coldkey seed on the `validator stake add` path. That path covers:
- authentication of the pinned runtime, with the live take, bounds and last change read at the finalized block;
- a check of each call's arguments against the authenticated metadata;
- `payment_queryInfo` fee approval under `--fee_limit_rao`;
- a dry run unless `--apply`, then journaling, finality and a readback of the stored take.

`take status` also reports root and subnet registration, auto parent delegation, children and parents. `validator register` cannot register on root: it signs `register_limit`, which root refuses, and native `root_register` has no burn ceiling. Root registration therefore uses btcli or the reviewed `sn-mainnet root-register` workflow.

On the uncommitted `feat/operator-discovery-take` worktree, 16/16 focused take tests pass: percent conversion and bounds, call encoding against the runtime 461 metadata and the runtime 470 projection, dry runs from hand-derived storage, and usage parsing. `go build` of `validator` and `cli/validator`, `go vet ./validator/` and `GOOS=linux go vet ./validator/` also pass. No live chain was used. No root registration, take change or deployment has happened, and `activation: blocked` is unchanged.

## Code and focused-test checkpoint — October 6

The requested code changes, website guides and focused tests are complete. The [delivery checkpoint](evidence/code-delivery-checkpoint-20261006.json) binds the final paired builds, Server operator checks and website validation; its linked earlier indexes preserve exact tested source tuples and original failures. This checkpoint supersedes older current-state and unexecuted-test statements below. These scoped results do not establish a full-suite pass or mainnet readiness.

| Qualified scope | Retained result |
| --- | --- |
| Root-seat registration, separate coldkey signing and exposure policy | 54/54 focused tests pass. |
| Native proof RPC retry and refusal evidence | Original 16-root run: 11 PASS / 5 FAIL. The seven-root successor passes all five failures plus two additional checks; the four SN feed tests also pass. |
| Selected payout schedule preflight before migration writes | 6/6 focused tests pass. |
| Provider and client-key reads | Original validator12: 10 PASS / 2 FAIL. Provider4: 3 PASS / 1 FAIL, resolving both original failures. Final validator3: 3/3 PASS, including the repaired control and both shared GET/wallet deadline cases. |
| Chain, bootstrap-prefix and steering read deadlines | 22/22 selected tests pass: nine chain, seven bootstrap and six steering roots on SN `bc3c20d0`. |
| Server retry taxonomy and owned fixture permissions | Taxonomy14 passes under `UMask0077`; the fixture repair then passes four selected tests under the ordinary mask. The original setup refusal remains retained. |
| Published Connect owner-ledger API | Core8 and Server interop2 pass on their retained source tuples. The original test-image staging failure remains separate. |
| Durable-volume write health | 15/15 selected normal tests and 2/2 selected race tests pass on Connect `501c172d`. |
| Treasury validator readiness | Original treasury14: 8 PASS / 6 fixture failures. Corrected treasury7 passes all six new cases plus one passive-bootstrap control. |
| Miner selected state and refusal guidance | Original miner4: 3 PASS / 1 FAIL; the corrected four-test run passes all four. |
| Retained Solidity carry chronology | Three normal tests pass; the prior-carry omission control fails at its required assertion. All 1,658 current Solidity inputs match. |
| Cancellation, accounting and economic progress | Four Server pure tests and two isolated DB tests pass; two SN economic-progress tests pass with race detection on their recorded source tuple. |
| Server operator CLI and service integration | 51 selected top-level normal tests pass, plus two taskworker subtests; all 27 CLI/all tests also pass with race detection. |
| Website miner, validator and operator guides | Astro builds 95 pages; 2,511 internal links have zero dead links. SEO and committed dates for 48 URLs pass. |
| Final SN and Server all-package builds | Both `go build ./...` commands pass on SN `ff1ba7d2`, Server `875fbca2` and Connect `501c172d`. |

The final builds consumed SN `ff1ba7d2`, Server `875fbca2` and Connect `501c172d`. The operator receipt separately retains consumed `ff1ba7d2` and closure readback `3a3fc001`; their three changed files are documentation only, with no Go difference. The website build uses `85c91dc5`; the later `1a119685` commit changes generated dates only and passes the postcommit date check.

The earlier paired package builds, fixture4 and storage tests use SN `bc3c20d0`, Server `a4da14a5` and Connect `501c172d`. The later local cancellation, DB and economic-progress tests use SN `9a21e156` with the same Server and Connect revisions. Treasury7 and miner4 use SN `ff1ba7d2`, again with Server `a4da14a5` and Connect `501c172d`; the later paired builds and Server operator checks retain their separate receipts. Deadline22 retains Server `948f12a7` / Connect `a5dfb3c7`; taxonomy14 retains its separate earlier tuple and umask. Earlier builds, interop2 and direct tests remain pinned in the initial index. Each result retains its source and selection, with no blanket test qualification for later upstream changes. Earlier retention20 and raw-version scope3 passes remain retained alongside the original Rust11 10 PASS / 1 FAIL record.

The selected economics remain **10% for providers and 90% received by `ur-reserve`**, which sends no funds. Earlier recycle routing and mandatory reserve-signing statements retain historical scope only. Live mainnet capture, production database migration, deployment and activation were outside this code/test task. The original capture-v2 failure and its unretained raw RPC reply remain in the [operational evidence](/home/by/sn-testnet-root-fallback-20261005/original-epoch9218962-v2/run/evidence/closed-owner-readback-v1.json); they are not reclassified by these code results. `activation: blocked` is unchanged.

The selected deployment fields `deployment_id`, `coordinator`, `settlement_vault`, `policy_hash` and `readiness_sha256` remain empty. Owner/root/operator custody and roles, registered recipient setup, runtime authority and activation remain operational work outside this code/test scope. Historical pending statements keep their dated scope; the current qualification above records implementation and test status.

## Current reserve and economic decision — October 5

The user selected **10% of the native miner allocation for providers and 90% received by `ur-reserve` for future network improvements**. `ur-reserve` only receives funds and does not send funds, including registration charges or setup fees. Use the supplied native address and public recipient hotkeys; reserve receiving does not require multisig reconstruction, signatory identities, Ledger device configuration or general spending/signing qualification. Fund recipient setup separately and use the runtime-supported receiving-only ownership-transfer path described in [recipient setup](TREASURY-RECEIVE-SETUP.md); its observed delay remains a launch constraint. This supersedes the earlier mandatory reserve-signing workflow and the owner-recycle economic choice. The treasury remains separate from provider claim collateral and the immutable, one-way `STReserveSink`.

The receive-only destination is `5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR` (native AccountId32 `0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410`). Its prefix-42 SS58 checksum and round trip are valid. The [public destination descriptor](TREASURY-EMISSIONS.md#public-configuration-contract), schema `urnetwork-native-treasury-destination-v1`, contains `profile`, `netuid`, `genesis_hash`, `account_id` and public `recipient_hotkeys`. Its explicit file must contain only that descriptor. If `vault/main/sn.yml` also contains root keys or other secrets, prepare a separate public-only snapshot and pin that snapshot; never supply the combined vault file to the treasury CLI. The strict earnings schedule in `config/main/sn.yml` remains separate. Use `treasury describe --destination FILE --destination-sha256 HASH`; the same destination flags select receive-only `observe` and `policy-plan` with their explicit `--input`.

Describe accepts the known address with an empty recipient list. Routing still requires at least two actual registered SN25 recipient hotkeys under the existing cap, authenticated `Owner`/UID/registration generations, owner-set exclusion, and an independently signed runtime and economic policy. No recipient hotkeys or registered generations have been supplied here. If they are missing on chain, one-time external registration and fresh readback are required. The receive-only path does not register keys or authorize reserve spending. The earlier confirmed **2-of-3 multisig** information applies only to the optional, unselected sending-custody workflow; its strict derivation and Ledger requirements remain intact for that workflow.

The [receive-only treasury successor](TREASURY-EMISSIONS.md) still needs focused execution qualification and deployment. Existing recycle and multisig qualification retain their original scopes. Preserve the 38 original requirements, their counts and all retained evidence; earlier economic or mandatory reserve-custody statements below are historical. Owner, root and contract actions retain their own signing and authority requirements. The October 6 inclusive new-earnings boundary, pre-cutoff USDC obligations and `activation: blocked` remain unchanged. No keys, transactions or deployment are supplied by this decision.

## Runtime migration and focused qualification — October 5, 19:45 UTC

**Original-profile assembly update:** the current-source runtime-profile CLI built successfully and assembled all 17 reviewed rules into an exact 20,629-byte profile, SHA256 `6c9c387bcffe8c492d77764a9956927be5de2a0dede8663d2bf3a94c9840aae4`. Current capture and proof binaries also built successfully. The [assembly checkpoint](evidence/original-profile17-assembly-20261005.json) retains the joined results and the real SN25 epoch request for block 9,218,962. Assembly is now complete; original block capture/replay and production approval remain pending. Earlier pending statements below retain their historical scope.

**Latest focused checkpoint, source `49ccc424`:** the fresh mainnet binary and all 21 selected Go tests passed; all 18 selected Rust regressions passed, including the previously failing observer-proof capture test. The 12 database tests also passed, completing the remaining cutoff tests and three original ARIN population tests. Every worker joined and closed, and database resources were removed. The [checkpoint](evidence/focused-qualification-source49-20261005.json) pins the retained closure evidence. The unsigned 17-rule original-runtime profile has passed static wire and callsite review; actual CLI assembly and original block capture/replay remain pending. These results do not establish production authority, recipient setup, deployment or loaded activation configuration.

**20:06 UTC update:** the [five-root Rust treasury run](/mnt/data/sn-testnet/sol-focused-harness-20261005/rust5/evidence/rust5.terminal.json) and [17-root financial run](/mnt/data/sn-testnet/mainnet-parallel-20261005/focused-server-qualification-20261005/financial17-v2/body17-v2/result.json) passed. Both retain completed waits and successful cleanup; actual Rust exports remain available for the two pending Go consumers. Receive-only routing and the current launch-plan correction are merged through `28f9936b`, with focused tests pending. The profile inspection/assembly tool is merged through `f2b68135`; its tests and actual production layouts remain pending. The older pending statements below preserve their checkpoint scope.

The [reserve chain observation](/mnt/data/sn-testnet/reserve-sn25-recipients-20261005T200449Z/OBSERVATION.json) at finalized block 9,219,009 returned no `OwnedHotkeys` for the supplied reserve account. Runtime-473 metadata interprets the absent value as an empty list. No recipient UID or generation can be inferred from that address. This is an endpoint-finalized observation with retained read proof, not an independently completed trie/GRANDPA verification. Supply and register actual receiving hotkeys before admitting the 90% native routing policy; reserve spending signers remain unnecessary for receiving.

The runtime-473 admission correction is merged through `a70dae9c`. Admission verifies the reviewed source capability and an authenticated migration-completion witness at the exact signed activation parent; a runtime number alone grants no authority. A later completed-migration snapshot cannot authorize an earlier activation. The [31-root runtime result](/mnt/data/sn-testnet/sol-focused-recovery-61-20261005/sn473-normal-v1/evidence/sn473-normal.owner.receipt.json) passes all 26 mainnet and five validator roots with actual retained assets, no skips and no resource errors. Five Rust treasury roots are pending. Retained recovery results are 55/55 focused roots and 23/23 SDK race roots. These results do not qualify the production execution profile.

The actual runtime-473 capture/replay profile remains an engineering prerequisite. A named-function census or synthetic fixture profile does not establish production callsite semantics. Qualify the actual profile against the official runtime bytes, then obtain scoped authority approvals. Registered recipient generations, contract deployment and loaded activation configuration remain separate launch inputs. Reserve receiving requires no signing-custody qualification. Postactivation settlement intervals are an acceptance stage after activation, rather than evidence that can be supplied before signing the launch.

Runtime profile adapters must preserve where each value came from. The explicit single-mechanism storage join derives the epoch total from three original tranche returns, maps UIDs through original metadata-typed `Keys` gets, and uses the original epoch-counter write. Two bounded observer reads retain registration and generation from the same proof-backed execution overlay at the selected drain; they are separate from Wasm memory and runtime get records. Missing stack fields cannot be filled from later RPC snapshots. The source and focused tests are authored; actual profile assembly, execution and independent approval remain separate obligations.

The storage adapter now matches a bounded exact leaf-to-caller path before reading memory. Recipient joins use original get/set pool deltas, explicit custody returns and actual zero-entitlement reads; fully captured rewards do not require a fabricated liquid credit. The recycle branch uses one bounded `Owner(hotkey)` observer read from its current execution overlay. Capture runs the same observer to retain phase and dynamic proof paths, then discards its provisional trace before independent strict replay. The unchanged failed capture root remains required after the source correction. Original metadata pins no longer implicitly select fee accounting; whole-fee and full-Yuma authority remain explicit optional scopes. These new paths and deterministic regressions are source changes, not actual runtime-profile qualification or approval.

## Local dependency validation and retained results — October 5

**Current execution priority — after the October 5, 16:26 UTC checkpoint:** continue deployable builds and useful parallel focused validation, then remaining suites. The [closed four-attempt checkpoint](evidence/focused-window-terminal-and-treasury-status-20261005.json) preserves completed artifacts and unfinished work; it does not establish overall readiness. Full-catalog completion is not a prerequisite for focused work. Canonical config `9c4a3435` still blocks activation with empty deployment, contract, policy and readiness fields; software passes and the implemented treasury path do not supply those authorities.

The current [38-requirement evidence ledger](evidence/original38-current-evidence-ledger-20261005.json) separates implementation, normal/race/control evidence and external authority or rehearsal requirements. All original outcomes remain open; this does not mean there are 38 code defects. The [latest native-fee result](evidence/nativefee-and-bounded-copy-progress-20261005.json) passes all **13 roots normally and all 13 with race detection**, with zero failures/skips, joined waits and no resource errors. These are actual default-module results on the corrected `47003d6e` component. Its 2,017 pinned input files remain unchanged at `4991966f`; preserve those results without repeating the positive bodies solely for unrelated later changes. Native-fee causal controls and downstream consumers remain separate. The [retired-wallet payment correction](evidence/original-native-and-payment-progress-20261005.json) is merged in Server `d64c9beb`: retry only the durable original request and key, preserving fresh-wallet and earning-cutoff admission. Its ten new and two changed tests, plus eight controls, still require execution.

The defect was a promoted `bytes.Buffer.ReadFrom` method: `os/exec` copied output through that method and bypassed the guarded `Write`, including its byte cap and cancellation hook. Existing owner-cancellation commit checks were already present. The merged fix uses a private named buffer so output passes through the guard. A separate fixture correction owns a protected copy of the temporary test executable without relaxing production admission. Preserve the original [ten-pass/one-fail run](evidence/nativefee11-normal-result-20261005.json) and the later five-pass/eight-fail results in each mode; the current 13/13 results follow those distinct corrections. The merged mainnet fixture likewise preserves an already protected engine's exact path and hash, copying only an inadmissible temporary test image. The selected seven-root mainnet fixture group, including all three new custody roots, now passes normally and with race detection in the [joined adjacent-batch results](evidence/current-main-qualification-progress-20261005.json). This does not qualify the full mainnet package.

The [process recovery suite](evidence/process-recovery-tooling38-result-20261005.json) passes 38 tests, and the [expanded peer suite](evidence/nativefee-and-bounded-copy-progress-20261005.json) passes 50 in one joined invocation; the earlier 44-test result remains retained. These regressions cover recovery after the [compiler observation failure](evidence/default-server-compiler-interruption-20261005.json) and [independent peer refusal](evidence/independent-peer-interruption-20261005.json). Preserve original PID/start identities, durable waits on exceptional exits, and release a stopped peer's resource reservation independently of its test outcome. The optional immutable-source helper’s bounded-mount correction is merged. Its [34-test follow-up](evidence/original-native-and-payment-progress-20261005.json) retains **33 PASS / 1 FAIL**. The corrected assertion now passes in the [current tooling38 run](evidence/current-compilers-and-ph15-producer-normal-20261005.json), alongside ten interpreter and 27 ChildContext tests. A later duplicate launch stopped before any test body; the original 38 passes and joined wait remain intact. This is not a new full34 rerun. Preserve the earlier 31 failed outcomes. The helper and its 18 controls remain separate from active jobs, which continue full hashing; this does not delay product qualification.

The merged output-bound fixes add four release-builder and four inventory-command roots. The [adjacent batch has joined](evidence/current-main-qualification-progress-20261005.json): release-builder normal and race each retain **93 PASS / 2 FAIL / 0 SKIP** among 95 qualifying roots, while `arinshadowctl` and the selected replay group pass all **13 and seven roots respectively in each mode**. The [two release-builder failures](evidence/unix-socket-harness-diagnosis-20261005.json) arose in `net.Listen` before the intended assertions because the harness temporary path exceeded the Unix socket pathname limit. Only those two roots per mode were rerun with a short, fresh, owned temporary directory; **all four targeted invocations passed**, with unchanged source and assertions. The original 93 passes plus the two passing retries cover all 95 qualifying roots per mode. Preserve the failed original batch and the separate retry receipts; this is a union of results, not a new full-package rerun.

Size data-volume capacity from the non-root account’s available bytes after filesystem reservations. The [recorded `/mnt/data` reserve adjustment](evidence/data-volume-reserve-headroom-20261005.json) reduced this dedicated ext4 volume’s root reserve from 5% (about 100 GB) to 1% (about 20 GB), releasing 80,015,912,960 bytes to ordinary users. Record an explicit, reversible reserve policy for non-root data workloads; temporary-file cleanup cannot release reserved blocks. The original capacity baseline/cap, per-owner growth limits of 8 GiB and 2× forecast margins remain unchanged. Test and deployment status are unchanged.

The [merged start-window correction](evidence/repair-dispatch-window-source-and-scope-20261005.json) in `8776b038` rechecks signed authority after successful storage inspection and immediately before the fixed process-start command. A slow inspection or original census must not carry an expired start permission into dispatch. Preserve the original inspection failure, the appropriate wall/monotonic and observation-age bounds, and the durably consumed one-shot start. Its ten new tests and six controls remain unexecuted; fourteen existing adjacent tests outside the master catalog remain explicit pending completion obligations. The finite contract audit found no confirmed source gap in its reviewed inputs; actual deployment authority and pending qualification remain separate. Subsequent merged corrections and their qualification limits are recorded below.

The [merged runtime-identity correction](evidence/runtime-identity-source-and-scope-20261005.json) in `1d4251f4` compares each completed name, genesis and EVM identity before the next RPC. A completed contradiction remains a hard refusal even if a later read times out; null/empty replies remain unavailable and malformed replies remain hard. All five public readers retain their original read budgets. Its seven new validator tests and eleven controls are unexecuted; 28 existing adjacent tests outside the master remain separately required.

The [production-boundary clarification](evidence/production-boundary-source-and-scope-20261005.json) in `03b94755` confirms that the current schema-3 V2 measured, signed and durable submission path is implemented. Legacy observation and unsigned bootstrap review plans retain their intentional refusal to authorize execution. Only diagnostics and tests changed; newly emitted unsigned review-plan hashes can change. Two new validator roots, one changed existing mainnet assertion and three controls remain unexecuted.

The [merged PH-12 and ARIN checkpoint](evidence/ph12-auth-arin-source-and-scope-20261005.json) binds SN `5e71e63c`, Server `67467b26` and SDK `e3f4f3fb`. Claims use the provider credential and original eligible contribution; earning-wallet authority requires explicit prospective consent, not a generic login proof. Retain and sync the exact signed consent before POST, then replay that original after a lost acknowledgement without replacing its endpoint or identity. The SDK now decodes Server decimal-string `no_id` exactly into its existing `int64` field, while preserving legacy integer input and refusing malformed or partial results. The 20 new miner and 16 new Server roots remain unexecuted. SDK has a separate 15-root obligation: 12 new HTTP/unit roots, two existing API neighbors and one generator-currentness root under the nested `sdk/cgo` module. The current SDK normal result below covers its fourteen root-module obligations; the nested generator and SN/Server consumer qualification remain separate.

The [merged native-upload finality correction](evidence/upload-finality-source-and-scope-20261005.json) in `02e507ee` retains the selected block and first/highest finalized witnesses through replacement and nested reads. It treats authentic lower-only coverage as temporary unavailability, checks fresh closing finality after timestamp storage, and keeps completed canonical or identity conflicts hard. The original 300-second owner and shorter caller deadlines are preserved. Eleven new validator roots, one changed existing canonical-switch fixture and seven controls remain unexecuted.

The [merged EVM decision-finality correction](evidence/evm-decision-route-source-and-scope-20261005.json) in `516b397a` gives opening, financial and closing reads one original 300-second owner, with a 60-second ceiling for each physical header or the shorter caller deadline. Authentic lower or missing finalized coverage retries header evidence without replaying financial reads. Completed canonical contradictions remain hard, and errors cannot publish a partial decision. Its sixteen new validator roots, changed existing deadline fixture and thirteen controls remain unexecuted. The adjacent [route-identity correction](evidence/route-identity-and-skip-inputs-20261005.json) is now merged in `ee81380c`: opening and closing checks compare each completed chain ID and genesis before another RPC can obscure it. Null replies remain unavailable; present malformed or contradictory identities remain hard. The original 300-second owner and 60-second physical-read bounds are unchanged. Its nine new validator roots (56 cases), six existing neighbors and seven controls remain unexecuted.

The [retained Core224 normal run](evidence/focused-core-server-progress-20261005.json) has joined with **222 PASS / 2 FAIL / 0 SKIP** and no resource errors. Its earlier metadata/output namespace collision refused before any LIST or test body. Preserve both records. The H1 probe timer could mask a physical timeout with a closed-pipe cause, and the HTTP cause walker could admit callbacks before charging the complete child frame. Both corrections are merged in Connect `11f99c8f`; all twelve new roots and both original failed roots now pass in separate fourteen-root normal and race runs. Their eleven controls remain unexecuted. The legitimate upstream Core changes receive no pass credit from the older `026b8502` image. Published SDK `18fca2ed` includes the Hint callback fixture correction and legitimate upstream performance-profile validation; no additional stricter setter patch is needed. Its focused 23-root normal run now passes; SDK race, generator-currentness and twelve controls remain pending. SN `494c65be` adds two wallet-child diagnostic tests, retained as separate focused obligations without increasing the product master or crediting the old unobservable control.

The [merged Server fixture corrections](evidence/focused-core-server-progress-20261005.json) preserve the original populations and assertions: the opt-in subscriber benchmark keeps 20,000 providers and 240,000 connections, Stats keeps 160,000 connections and its sixteen query-plan calls, and setup commits bounded 512-row groups. They add one regression root and three controls; the benchmark reuses existing roots and ARIN controls. The paid/free failure came from comparing terminal originals before the real legacy worker completed, not a production weighting change. Its corrected fixture and new public Redis sibling add one root and two controls, all unexecuted. The original frozen model failures and skips remain recorded in the closed, incomplete run described below.

The merged retention correction hardens only the **existing optional MinIO/S3-compatible store**: startup paths apply the complete owned rule set for a shared endpoint and bucket. It adds no AWS service, dependency or required mainnet storage infrastructure. Missing or disabled storage remains a no-op, and startup application stays best-effort in the background. Its six new feature tests, nineteen existing neighbors and seven controls are separate from launch requirements and remain unqualified.

**Retained focused results:** [the same fourteen Core roots pass normally and with race detection](evidence/focused-sdk23-core-race-and-build-lessons-20261005.json) on `11f99c8f`, with zero failures/skips and joined, closed owners. SDK `18fca2ed` / Core `11f99c8f` passes all **23 focused normal tests**; SDK race, generator-currentness and twelve controls remain pending. Server `62104c48` retains its successful model-test compiler result. These results remain separate from the following incomplete attempts.

| Final-window attempt | Actual result and remaining scope |
| --- | --- |
| Deployable build | Server `go build ./...` was cut off after **221 seconds**, exit −15/joined. All four SN binary builds remained unstarted. No deployable-build pass is established. |
| SN focused normal3 | Mainnet compilation and full **2,413-root LIST passed**. Its 34-root body received only **three seconds**: **2 PASS / 1 SKIP / 1 timeout-failure / 30 unrun**. Validator compilation was cut off after 55 seconds, exit −15/joined; miner never started. Retain the mainnet image and LIST; the package scope remains incomplete. |
| Forge carry3/control1 | The 53-file build passed in **65.83 seconds**. LIST triggered ten more files of compilation and hit its **20-second timeout**. Neither the three positive bodies nor the control ran. |
| Server cancellation6 | The peer observer refused database startup before LIST or bodies. The child joined and exact owned PostgreSQL/network resources were removed. All six tests remain unrun. |

A readiness ETA is a reporting estimate, **not a timer that should kill a healthy phase**. Give compilation, implicit compilation during listing, and test bodies adequate physical timeouts with cleanup and resource allowance. Preserve completed artifacts and original failures, and continue unfinished work after the checkpoint; do not shorten a test to the few seconds left in an estimate.

Four subsequent deterministic DB-observer regression cases pass in the [separate harness receipt](evidence/focused-window-terminal-and-treasury-status-20261005.json). They execute no database integration or cancellation6 body and do not clear that unfinished scope.

The original mainnet34 compiler failed on the unused `encoding/hex` import; merged `802f9862` removes it, and the corrected mainnet test compiler now passes as recorded above. Deployable binaries still require successful builds. Compile them early to expose shared-package failures before expensive fixtures and catalog work. Preserve the earlier SDK pre-stage refusal caused by an extra `adopted_by` annotation: validate substantive approved fields exactly while permitting declared provenance annotations that cannot alter approval.

The original **1,865-root model run has closed with incomplete coverage**. Its 64-MiB output guard stopped the child before natural package completion; the Go test timeout did not expire. The unchanged raw stream records **1,813 RUN / 1,789 PASS / 7 FAIL / 10 SKIP**, plus **seven started roots without terminal outcomes and 52 never started**. Nested events retain 323 RUN / 322 PASS. The owner, process tree and owned database resources are closed. The original guarded body receipt keeps its empty count field; a separate executor audit supplies these partial observations. None of this qualifies the full suite or later source. Preserve all seven failures and ten uncovered skips, and do not restart the old full suite for bookkeeping.

The seven failures comprise three ARIN population fixtures, Stats population setup, the FP2 expectation, the paid/free worker fixture and the AsyncDebit page-cancellation boundary. The first six have the previously described source corrections. The [reviewed AsyncDebit correction](evidence/focused-compiler-and-model-stop-progress-20261005.json), source `24f75908` merged as `3ef7bd03`, now recognizes complete permitted joined-cancellation causes only after owned completed progress; adjacent legacy settlement preserves hard siblings and the prior cursor. Its financial and replay assertions remain intact. Two new roots, four retained debit roots, adjacent tests and nine controls are explicit pending obligations; no corrected body or control pass is claimed.

Keep full-suite log forecasts proportional to measured output, and process event streams without losing the distinction between observations and retained bytes. This run observed 67,108,865 bytes but retained 67,107,457 because the guard rejected the crossing chunk before writing it. The [separate harness qualification](evidence/focused-core14-and-current-server-compile-20261005.json) now passes ten log-budget, forty peer-observer and seven closed-owner tests under bounded memory and time. This preserves the old failure and supplies no full-suite or product-control pass; future jobs retain their own disk safeguards. The old eight-skip input snapshot remains historical; the final audit also records the subscriber benchmark and `auto_explain`-dependent query-plan skips. All ten remain uncovered. The active 401-byte `pro.yml` exists with inert values; apply the approved nonzero product/referral and onboarding inputs only to a fresh owned fixture.

The earlier [mainnet/miner compiler and PH-15 producer32 normal passes](evidence/current-compilers-and-ph15-producer-normal-20261005.json), [eight Server compiler passes](evidence/repair-dispatch-window-source-and-scope-20261005.json), and **113/113 Rust tests with 61 retained exports** retain their original input tuples and limits. Race tests, causal controls, current Go native86 consumers and remaining bodies are separate. These results do not transfer to changed source simply because it is on main. All 38 original outcomes and preserved scope counts remain intact.

The [finite launch-path review](evidence/focused-compiler-and-model-stop-progress-20261005.json) found no missing adapter on the selected existing-root-seat, reviewed-470, owner-local Ledger and approved two-UR-start route. The October 7 decision replaced that two-UR start with one sole UR validator ([One operator and one validator — October 7](#one-operator-and-one-validator--october-7)). Actual owner/device approvals, identities, funding, signed configuration and installed deployment authority remain required inputs. Three complete emission intervals and a full 50,400-block settlement/claim cycle are postactivation acceptance outcomes. The review does not grant a new root-lifecycle signer or authority for an unreviewed runtime version. Later historical sections retain their dated results and do not override this current checkpoint.

The [PH-13 source audit](evidence/server8-and-native113-progress-20261005.json) confirms the intended historical cancellation limit: current production has no authorizing cancellation operation or original cancellation clock. Retain canceled rows with unknown attribution; report ingestion, zero-byte cleanup and context cancellation cannot invent that authority. This audit adds no new source fix or qualification gate. The [PH-15 observer and dashboard extension](evidence/ph15-source-implementation-and-scope-20261005.json) is merged, and the same **12 targeted dashboard tests pass** with no failures or skips in the [latest 8.732-second run](evidence/focused-core14-and-current-server-compile-20261005.json). The earlier 6.981-second pass remains separate; this repeat adds no unique test coverage. It preserves the original 36 financial gauges and adds 36 coverage/progress scalars plus acknowledged operational status. Preparation throughput, local finalized-observation cadence and catch-up ETA to an admitted boundary stay distinct; future/repair estimates and unsupported facts remain unknown. The result used actual Promtool and SN source checks, and the process closed cleanly. All **32 Go producer normal tests now pass** with zero failures, skips or resource errors. The 32 race tests and four controls remain unrun; the prior conservation33 pass remains separate. Earlier YAML/Ansible loader failures are retained; the successful run used an existing qualified virtual environment without source changes or dependency installation. Other owners’ transaction/incident records and live ingestion, on-call delivery and outage drills still require their own evidence.

The [large-scale coverage review](evidence/large-scale-coverage-gap-20261005.json) identifies five required roots omitted from the 2,503-root selection: Claim129, Restore129, the 2,048-UID physical-head/archive/restore case, and both large-profile witness restoration cases. Preserve the existing selection and controls; add these roots explicitly or prove that their retained results apply to identical inputs. Historical Claim129 passes took about 1,694 seconds normally and 8,152 seconds with race detection. Keep separate adequate mode budgets and durable per-root results. The selected 1,024-UID case contains two reads each admitted for 900 seconds, so its previous 900-second whole-root timer can cancel healthy work. Its reviewed normal harness bound is now 3,600 seconds, with a 3,720-second child limit. This changes execution bounds, not assertions or census scale; execution and race qualification remain pending. The [explicit scale supplement](evidence/current-mainnet-scale-supplement-20261005.json) preserves the 2,503 existing roots and appends all five in both modes: a 2,508-root baseline, including 613 SN/Connect/VLESS roots. The [current selection](evidence/original-native-and-payment-progress-20261005.json) preserves that baseline and restores all **86 original native-readiness tests** omitted from the recent incremental selection. None overlaps the earlier 2,637 roots; together with the ten payment tests, these form the preserved 2,733-root baseline. The [PH-15 scope addition](evidence/ph15-source-implementation-and-scope-20261005.json) adds 17 new tests and restores five existing neighbors absent from that baseline. Its other ten existing neighbors are already counted. That 2,755-root selection is preserved. Restoring [two previously requested payment neighbors](evidence/current-compilers-and-ph15-producer-normal-20261005.json) forms the preserved 2,757-root baseline. The [start-window addition](evidence/repair-dispatch-window-source-and-scope-20261005.json) contributes exactly ten new mainnet roots. The [runtime-identity addition](evidence/runtime-identity-source-and-scope-20261005.json) adds seven validator roots; the production-boundary clarification adds two more, forming the preserved 2,776-root baseline. The [native-upload addition](evidence/upload-finality-source-and-scope-20261005.json) preserves the 2,787-root baseline. The [merged PH-12 and ARIN checkpoint](evidence/ph12-auth-arin-source-and-scope-20261005.json) adds 37 owned roots, forming the preserved 2,824-root baseline. The decision-finality and route-identity additions preserve the 2,849-root baseline. The twelve Core and two Server fixture additions bring the selection to **2,863 Go roots per mode**: 573 SN (302 mainnet and 104 validator), 236 Connect, eight VLESS and 2,046 Server (101 controller, 1,868 selected model and ten new `api/handlers` roots). This maintained model selection does not yet include any additional declarations from the separately reviewed upstream Server delta. The 116 existing adjacent obligations outside the master include the six route neighbors. Those obligations and the separate SDK 15 remain required by exact package/name recipes. The upstream review separately retains 51 relevant roots: 18 already selected and 33 additional obligations; three diagnostics are excluded from new mainnet scope. All prior 290 control groups remain; 33 SN, 27 Server, three ARIN fixture and four FP2 groups form the preserved 357-group baseline; thirteen decision-finality and seven route-identity controls preserve the 377-group baseline; sixteen Core/Server correction controls bring the operative catalog to **393 groups**. Reused ARIN controls are counted once. One additional SN redundant-defense diagnostic and the SDK’s twelve controls remain separately recorded; the diagnostic is excluded from expected causal kills. The two original native Rust controls were already counted. The four runtime-renewal consumers retain their actual five-original fixture prerequisite. The [native fixture map](evidence/server8-and-native113-progress-20261005.json) now binds all nine profiles with 38 original files. The full Rust suite retained the two distinct generic producer/conservation profiles with five original jobs each; these are separate from the renewal jobs. The Go native86 consumers still require their own execution. These counts describe selection, not execution passes or complete requirement coverage.

The [finite contract input comparison](evidence/ph12-auth-arin-source-and-scope-20261005.json) retains the actual **226 Forge passes across 20 suites** and the two five-contract builds under their original source/tool/library receipts. Current Solidity and literal build inputs match; unrelated Go changes do not require repeating those results. The three later carry roots already passed, and the original prior-carry omission control produced its required assertion failure. The [retained carry checkpoint](evidence/carry-treasury-qualification-20261006.json) pins those closed results and verifies all 1,658 current Solidity inputs with zero mismatches; no Forge rerun was needed. The historical recovery causal pair and four catalogue Go controls keep their exact scopes. None supplies deployed artifacts, native 10/90 proof, signing authority or live acceptance.

SN main `9c7a2842` now selects all twelve replacement modules from local paths, including Connect, SDK and Connect's SCTP fork. Server main also uses sibling substitutions. Keep external dependency versions pinned. Validate ordinary module resolution with `GOWORK=off`, using the actual sibling checkouts and their module sums; a workspace can mask obsolete published dependencies and therefore cannot establish that the declared module configuration builds. Record each sibling revision with the release so local substitution remains reproducible. Current sibling changes require affected validation; unchanged components retain their original evidence.

The [nine-package normal compiler result](evidence/main-nine-normal-compiler-result-20261005.json) passes all nine phases on the earlier SN `babb0af5` / Connect `7ca8e222` / Server `3e1fe2d7` workspace tuple. Those retained images support test execution on those exact inputs. They do not validate the newer sibling-default graph, test bodies, race behavior or deployment. The [corrected Rust libtest compilation](evidence/corrected-rust-renewal-compiler-result-20261005.json) now passes and joins cleanly, with no resource errors. The [targeted renewal test](evidence/corrected-rust-renewal-body-result-20261005.json) now passes with no skips and retains all five previously missing exports, independently hash-verified. The earlier 37 passing roots and original failed result remain retained. The [read-omission control](evidence/renewal-read-omission-control-result-20261005.json) now rejects the intended missing observation with the exact causal assertion: its child exits 101 with one expected failure, while the control runner completes successfully. The subsequent full 113-root Rust normal suite passes as recorded above; its original causal controls and the current Go native86 consumers remain pending. The monitoring conservation scope has passed as recorded below.

The [ordinary local-module checks](evidence/default-local-module-result-20261005.json) completed all ten source stages and four dependency graphs with joined waits and no resource errors. All four graphs failed: SN and Server lack checksums for the newly imported TLS dependency, while the offline cache lacks Connect and VLESS dependencies. The declaration and checksum correction is pushed in SN `fcfd5acd` and Server `c1982631`; local replacements and existing external selections are preserved. The [corrected read-only default graphs](evidence/readonly-default-module-result-20261005.json) now pass in all four contexts: SN 1,105 packages, Connect 468, Server 1,076 and VLESS 576. All five staging/list waits exit zero and join, with no resource errors. This resolves dependency preparation; compilation and product behavior remain separately unverified. A complete local sibling layout does not establish complete module sums or cache availability. Keep dependency preparation distinct from compiler and runtime results, and collect independent graph failures before fixing them.

Keep terminal receipts small and bind bulk evidence by hash. The dependency job completed all eight download/proposal commands with joined zero exits, then its final receipt exceeded the one-MiB serialization bound because it embedded 1,735 module records. The [compact recovery checker](evidence/authenticated-dependency-recovery-result-20261005.json) now passes and verifies all eight original phases and 1,735 download records, without repeating commands. Original outputs and waits remain authoritative; the failed original wrapper and absent final sample are retained. Completed commands must not be repeated solely because terminal serialization failed. A recovered summary must distinguish actual command success from its failed original wrapper and from later read-only build qualification. The [fifteen deterministic tooling regressions](evidence/hydration-tooling15-result-20261005.json) now pass in one actual invocation, exit zero with the child tree joined. They exercise compact receipt limits, corrupted or incomplete originals, bounded graph errors and retained-output refusal; no dependency or product commands were repeated.

The concurrency adapter's actual thirty-test run completes **29 PASS / 1 FAIL / 0 SKIP**, with no resource errors. A generation-refusal test expected a different refusal diagnostic. The source correction is merged in `5f377dd5`: reject a changed nonempty invocation before the stopped-process reconciliation branch, preserve original generation records, and add deterministic blank-invocation and revival coverage. The [corrected canonical-main suite](evidence/joint-adapter33-main-result-20261005.json) now passes all 33 tests, with joined waits and no resource errors. Preserve the original failure. Parallel product jobs still require their own exact inputs and resource bounds. Process census reads must also tolerate a process exiting between reads, while checking the same PID and start time so an observation retry cannot adopt a replacement process. An earlier wrapper refused before launching tests because unrelated stopped Docker containers existed. Admission now checks running workloads and the exact owned database resources. Historical foreign stopped containers are not active resource consumers and must not be deleted to make qualification pass. Resource and process checks must protect owned work while allowing unrelated, independently bounded validation to continue.

## Code frozen on main — October 5

**Retained publication checkpoint:** the [code freeze manifest](evidence/main-code-freeze-20261005.json) records SN `babb0af5`, Connect `7ca8e222`, Server `3e1fe2d7` and xops `482050c3`, with independently checked remote heads. This is a code freeze, not a verified release or deployment.

Verification at that freeze selected these combined main versions; subsequent source and dependency corrections are recorded above. Collect ordinary failures across the complete normal, race, model and causal-control scopes; fix them in targeted commits on main. Retain successful results when their complete source, dependency, tool and configuration inputs are unchanged. Do not require separate branch qualification before merging reviewed fixes. Deployment and signing retain their own approval and readiness requirements.

All seven compilers on the retained SN475/Core07/Server87d inputs passed. The [combined-main dependency result](evidence/published-main-metadata-result-20261005.json) now verifies the exact frozen main archives, checksum-pinned published gvisor dependency and three offline import graphs: seven phases exited zero and joined, with no resource errors. This establishes dependency resolution, not test behavior. Verify the main package images and execute the complete normal/race selections, including all 1,824 selected Server model roots on `c1982631`, without repeating the smaller model subset separately. The [current source coverage](evidence/current-mainnet-go-coverage-20261005.json) retains all prior selections and adds the affected neighbors: 2,503 Go roots per normal/race mode, with all 227 existing controls retained. Reuse older images only where complete compiler inputs match. The historical results and all 38 outcome requirements below remain intact.

The [retained 38-root Rust run](evidence/main-rust38-result-20261005.json) completed 37 passes and one failure, with no skips; all processes joined with no resource errors. It retained 24 of 29 planned exports. The runtime-renewal fixture counted its read/write host-observation pair as one principal operation. The corrected targeted rebuild, renewal body and read-omission control now pass; all five exports are verified, as recorded above. Preserve the original failed receipt and the separate targeted results. The subsequent full 113-root normal suite pass is recorded above; original causal controls and Go consumers remain separate.

The [canonical-main monitoring result](evidence/main-xops-conservation33-result-20261005.json) passes the corrected control, then all 33 conservation roots with no failures/skips: 34 invocations, 33 unique tests. The process joined and resource checks passed. Preserve the original 32-pass/one-fail result; production alert ingestion and delivery remain separate launch checks. The Rust fixture correction is pushed in main `b13a4225`; its targeted normal rebuild, renewal body and read-omission control now pass as recorded above. Remaining Rust and Go consumer qualification stays open.

## Retained qualification checkpoint — October 5, 04:47 UTC

**The October 5 01:00 UTC deadline was missed. All 38 original outcomes remain open.** This retained [source/receipt checkpoint](evidence/carry-compiler-checkpoint-20261005.json) records status at 04:47 UTC. Its pending and unqualified entries describe that checkpoint; the opening section records subsequent results. No deployment, live signing authority, October 6 transition or final acceptance is established.

| Result at the 04:47 UTC checkpoint | Action and scope limit recorded then |
| --- | --- |
| Server `4b917455` on SN `8e010496` / Core `25a4ce7f` passes **37 selected normal roots, zero failures/skips**, including all 19 previously failed model roots. Compiler/body and outer database waits exited zero/joined; resource qualification and cleanup completed. | Preserve original `c60` **1,703 RUN / 1,673 PASS / 19 FAIL / 11 allowed SKIP**, exit one with cleanup complete. Eleven failures exposed the signed-domain NUL/`jsonb` production defect; eight were fixture defects. `4b` preserves exact signed bytes, uses a canonical-prefix projection for indexing, corrects fresh 773/775 and appends 779. Qualify remaining migration, normal/race/vet and operative-control scopes; 37 passes do not establish a newer-source full-suite pass. |
| Owner/CLI/custody component on exact SN `8e010496` / Core `25a4ce7f` / Server `2f8a6ee4`: **75/75 normal PASS, zero skips**, formed from disjoint nine plus 66 roots. | The five original `178` failures remain recorded; their corrected roots now pass. Race, independent controls, actual owner-local Ledger replies and separate root/UR authority remain open. No later source inherits this result. |
| Ordinary SN8e/Core25a/Server2f8 funding remains **38 RUN / 37 PASS / 1 FAIL / 0 SKIP**; all 13 new joint-flow roots pass. Later [selected338 native carry](evidence/selected338-native-carry-result-20261005.json) finishes **3 RUN / 2 PASS / 1 FAIL / 0 SKIP**, exit one/joined: both funding consumers pass; chronology/archive still fails. | Source-only `47566b25` corrects the fixture: a 6% claim does not force capital use. The 99% leaf uses a 49-unit claim and 61-unit payment to require at least 13 capital units; a new small-credit neighbor retains unknown capital use through archive/restart. Production is unchanged; execute corrected/adjacent roots and controls, retaining both original failures. |
| Monitor `9430262c` on SN8e/Core25a compiles, then completes **17 RUN / 15 PASS / 2 FAIL / 0 SKIP**, exit one/joined, resource-qualified with cleanup complete. Fault fixtures name a nonexistent constraint (`42704`) and try to disable a protected system trigger (`42501`). | Four-file source child `ec3feae1` corrects both fault fixtures and adds canonical same-endpoint/hash close-expiry reads plus four roots. Execution remains pending; preserve the 943 result of 15/17, prior 7885 compile failure and 780 monitor/catalog, race and full-model qualification gaps. |
| On SN `88bd7956` / Core `07fe87bd` / Server `ec3feae1`, metadata passes 45 blobs/three graphs. The [four public compiler phases](evidence/latest88-public-compiler-result-20261005.json) finish: SN `nativefee`, Server root and `cli/stnativefees` **compile PASS**; `controller` **compile FAIL** on two stale fixture fields. All phases joined with no recorded resource errors. | New SN `47566b25` / Core `07fe87bd` / Server `87d278f5` is test-only corrected source: six stale references now use `ClosedWork.UsageBytes` (two Server/four SN), preserving all asserted totals. Source ACKs do not establish compilation; successor metadata/compiler and normal/race/control bodies remain pending. Older successful images stay bound to their original tuples. |
| Exact SN `3388840a` / Core `87e1bf9f` / Server `1ea80fa2` now passes **30 normal fee-settlement model roots, zero failures/skips**: 26 SQL/ledger roots and four public proof consumers using the 24 selected exports. Body and outer database waits exited zero/joined; resource qualification and cleanup completed. | Server root-package/controller/CLI, standalone nativefee tests, monitor/catalog, race, independent controls, full model and remaining Rust/Go carry bodies remain open. Unknown or unmappable contradictions retain liability; admitted settlements require all seven originals. No default conversion or production authority is inferred. |
| Published xops `a1fdda0d` retains 311/311 PASS. Clock-alert `2f1043a3` focused 33 now finishes **32 PASS / 1 FAIL / 0 SKIP**, exit one/joined, with no recorded resource errors. Its control expected `got: []`; actual Promtool printed `got:[]`. | Test-only `482050c3` accepts whitespace only within the anchored empty-result field and rejects nonempty/embedded/trailing diagnostics. Production renderer and 33/318 identities are unchanged; corrected execution remains pending. Preserve the actual failed control and all live ingestion/delivery gates. |

Exact SN338/Core87/Server1ea metadata, normal mainnet/model compilers and [one three-case proof exporter](evidence/native-fee-exporter-result-20261005.json) pass; all 24 exported files are hash-verified and protected. The exporter uses its own executable as a synthetic nested peer with real signed-receipt/GRANDPA checks. The [30 selected model roots](evidence/server1ea-native-model30-result-20261005.json) now pass within the scope above. [Exact selected 338 Rust receipts](evidence/selected338-rust-assets-result-20261005.json) establish successful `runtime-historical-capture`, `runtime-historical-proof` and library-test image compilation, exit zero/joined with no resource errors. One principal-export root passes with four protected fixture files, **zero failed/ignored and 112 tests filtered out**. This is one exporter result; the full Rust suite, corrected carry/archive root and independent controls remain unqualified. Neither these fixture results nor compilation establish mainnet deployment or production authority.

The source-only recovery regressions retain one captured cause graph through producers, preparation, steering and cancellation; inspect actual DNS children before availability flags; preserve hard Path/Link errors; and bound concrete geth revert inspection. Catalog projections require execution and do not prove a privileged production rehearsal. Compile fixtures against the actual public API: source ACKs are not compiler passes. Capital in an original source does not prove that every partial payment used capital. Retain observed pass/fail counts on Unix exit one, join the failed scope, and continue unrelated independently admitted work.

The complete PH-01–PH-28/MG-01–MG-10 ledger still governs recovery/custody, nonempty ingress-to-payout, native conservation, both validator roles and production rehearsal. October 6 governs new earnings; pre-cutoff USDC obligations remain payable. Published config `9c4a3435` remains blocked with five empty mainnet fields: `deployment_id`, `coordinator`, `settlement_vault`, `policy_hash` and `readiness_sha256`. Current source permits no post-cutoff USDC fallback; actual database preparation, running adoption and original/authority readiness remain unproved. Preserve 10% provider/90% recycle, owner-local Ledger/no Snow, separate root custody and the closed-testnet exceptions. Earlier dated evidence remains unchanged.

## Retained execution addendum — October 5, 00:55 UTC

**Frozen October 5 00:55 UTC; 4 minutes remained before the October 5 01:00 UTC deadline (October 4, 8 PM CDT).** This [additive readback](evidence/mainnet-release-addendum-20261005.json) supersedes current-state statements in the retained October 4 checkpoint below. **All 38 original outcomes remain open.** No deployment, owner/device approval, real root/UR activation or economic acceptance is established.

The executed compile tuple is SN `17863973` / Core `25a4ce7f` / Server `ba526028`: normal `-c -vet=off` passed, exit zero, joined, with no recorded resource errors. Its actual image lists all 66 selected roots successfully; the body finishes 61 PASS/5 FAIL/0 SKIP. Source successors Server `2f8a6ee4` (child of `902a924a`) and funding SN `127db99c` are separate uncompiled/unexecuted inputs. The protected full Server model remains the original `c60` / Core `7de` / SN `0fbb` tuple. A changed source graph does not inherit an older binary's passes.

| Actual observation and lesson | Exact correction and adjacent regression scope | Remaining qualification |
| --- | --- | --- |
| The earlier `5a` image compiled, then panicked in package initialization at `repair_operator_authority.go:30`; Go regexp refused `{1,2048}`. Selected test listing exited two, joined, before any of the 65 bodies ran. Null result counts are not zero-failure success. | [SN `17863973`](/mnt/data/urnetwork/operator-recovery-handoff-20261004/regex-init-17863973/handoff.txt) preserves the same ASCII class and 1–2,048-byte bound using `+` and an explicit byte guard. The new signed-host root covers 1/1,000/1,001/2,047/2,048 bytes, 2,049-byte rejection and invalid/empty characters; the omission control removes the byte guard. Exact peer source scan found no other counted-repeat literal above 1,000. | Actual normal body completed **66 RUN / 61 PASS / 5 FAIL / 0 SKIP**, exit one/joined, with no recorded resource errors. The new regex boundary root passes. The five owner-fixture failures remain open below; affected reruns, race/vet and independent controls are separate. |
| Selected178 normal execution has five owner-fixture failures: unfinished-reservation recovery plus four owner signing import/reply/v1-v2 custody roots. Their exact names and raw failures are retained in [the readback](evidence/mainnet-release-addendum-20261005.json). | Source followup `de32a1b4` replaces fabricated completed-checkpoint loss with actual precompletion store/recycle/trim interruption seams and adds two negative identity roots. Separate child `c43aa2fd` supplies the explicit durable-volume context in the four failed import/reply fixtures and adds one hard-refusal root. Both exact children of178 are bound in the readback; production custody/readiness guards remain intact. | Compose both source-only children onto funding127, compile and run the corrected/adjacent roots and readiness/custody controls. Neither child has executed. Keep the five original failures and all 61 passed roots; no hardware signing or live owner authority is inferred. |
| xops `55f1` retry2 completed 309 PASS/2 FAIL/0 SKIP; its original census/depth failures passed, but two alert timing fixtures failed. Corrected `a1` retry3 now completes **311 PASS / 0 FAIL / 0 SKIP**, 74.892 seconds, exit zero/joined, with no recorded resource errors. | [xops `a1fdda0d`](/mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-a1fdda0d-intake.json) fixes unspecified recording/alert group evaluation order in two fixtures. All 311 identities, production rules, SN09 pin and 120/180-second thresholds remain unchanged. The actual run includes reversed-order controls and eventual 150/210-second alerts, real pinned promtool and loopback Prometheus. | Retain both failed runs and the exact successful311 scope. Extra independent final-fence controls and actual Grafana/Mimir ingestion, alert delivery and on-call/repair drills remain separate open gates. |
| The protected 1,703-root `c60` model prefix is **1,571 RUN / 1,545 PASS / 8 FAIL / 11 SKIP** and still has no package terminal. Its 8 observed failed roots remain failures. Every observed skip is in the frozen allowlist; those are coverage gaps, not passes. | Fixture child `902a924a` follows URL `ba526028`, cutover `01f6e707` and earlier barrier `ed2a77ab`; these address the first six failures. [Latest child `2f8a6ee4`](/mnt/data/urnetwork/legacy-settlement-fixtures-handoff-20261005/intake.json) addresses the next two legacy failures. Production guards/migrations are unchanged by all these fixture corrections. | Preserve the protected run to terminal exit and joined/resource disposition. Compile the corrected model graph and execute failed roots, adjacent regressions and controls. Legacy queue acknowledgement is not terminal settlement; the distinct intent worker must run. The mainnet-package compile above does not execute Server model tests. |
| Three URL fixtures expected expiry instead of the existing six-minute renewal headroom. | [`ba526028`](/mnt/data/sn-testnet/mainnet-parallel-20261005/astra-url-renewal-fixture/HANDOFF.json) asserts the original four-hour window minus six minutes using explicit clocks and joined workers: three corrected roots, 27 adjacent roots and two omission controls. | Corrected model bodies and controls remain pending; no timing threshold or quota/security rule is relaxed. |
| The missing-original census fixture inserted a newly settled NULL-usage row after prospective guard745, which correctly rejected it before the intended census assertion. | [`01f6e707`](/mnt/data/sn-testnet/mainnet-parallel-20261005/server-closed-work-cutover-fixture/HANDOFF-01f6e707.json) seeds actual historical NULL usage under prefix744, applies current migrations, then contrasts it with a genuine current close. One corrected root, four adjacent model roots, six guard roots and three controls retain nil-result refusal of incomplete originals. | Compile and execute corrected/adjacent/control scopes; production guard745 is preserved. Missing original usage never becomes zero credit. |
| The healthy force-close fixture expected the payer's SQL debit before the asynchronous debit worker ran. Raw failure was an aggregate accounting assertion; phase diagnosis comes from the exact production branches. | [`902a924a`](/mnt/data/sn-testnet/mainnet-parallel-20261005/astra-force-close-healthy-fixture/HANDOFF.json) proves retained 1,024-byte debt/journal before the public worker, exact debit/release after it, and no work on replay, while retaining the dispute-update rejection guard. One corrected root, 29 adjacent roots and three controls. | All corrected bodies/controls remain pending. Calling the public worker in a test does not establish deployed scheduler cadence/restart recovery. Contract-close `ed2` separately corrects its cached activity barrier with nine roots/one causal variant; original failures stay recorded. |
| Two explicit legacy fixtures treated foreground durable intent as synchronous financial completion. | [`2f8a6ee4`](/mnt/data/urnetwork/legacy-settlement-fixtures-handoff-20261005/intake.json) invokes actual `FlushLegacySettlements`, preserves original stream state, checks exact pending/terminal accounting and brackets the 15-minute refusal backoff with DB clocks around the worker. Two corrected roots, six neighbors and two omitted-worker controls are frozen/source-reviewed. | Corrected model compiler, bodies and controls remain pending. This separate intent worker does not use the Redis consumption-journal expectation, and fixture execution will not establish deployed scheduler cadence. |
| Independent subset bounds could hide shared funding: two 50-unit claims exhaust one 100-unit obligation funded by 50 income + 50 capital, yet prior aggregation allowed income `[0,100]`. | [SN `127db99c` / component `cf04a36f`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-funding-correlation-final/evidence/intake-v2.json) correlates selected claims/payments through original obligation and carry capacity, including late payments, retired topology and cold replay; unknown sources remain unknown. Fifteen new roots/nine new controls; affected funding union 54 roots plus operator 23 and combined 33 controls. | Source review/application checks only. Funding is excluded from the `178` image. Required public native capture/replay originals, normal/race/vet and controls remain pending; no proxy proof or skipped public path qualifies it. |

The source graph through Server778 now includes the actual timed-open producer and publisher, with exact original boundary/DB-lock authority; that resolves the earlier missing-source description for its named graph. Complete authenticated nonempty ingress → payout → provider-window/Claim execution remains unqualified, and canceled historical work remains unknown. Source composition and migration availability establish no deployment.

The settled requirements are unchanged: 10% native miner allocation to providers and 90% owner recycle (superseded on October 5 by 90% received by `ur-reserve`); October 6 at 00:00 UTC governs new earnings, while all pre-cutoff USDC obligations and retries remain payable without conversion or double payment. Every old planner must be replaced before relying on its new shared lock. Owners keep their Ledger locally with no Snow access; root hotkey/coldkey custody is separate. Both root and UR validator roles remain required (since October 7 the UR role is one sole validator, whose hotkey may also hold the root seat), and current v470 participation has no periodic SetRootWeights obligation. The closed final testnet and its accepted exceptions are retained.

## Retained checkpoint — October 4, 23:56 UTC

**Finalization deadline: October 4 at 8 PM US Central (CDT), October 5 at 01:00 UTC.** This [read-only release checkpoint](evidence/mainnet-release-checkpoint-20261004.json) is frozen at **October 4 23:56 UTC: 63 minutes remained**. It establishes no production acceptance or deployment. At the deadline, report every unfinished implementation, qualification and external input explicitly. The [38-requirement ledger](PRELAUNCH-FIXES.md#reconciled-status-of-all-38-requirements--october-4) preserves all original outcomes; every row remains open.

The documentation base is SN `3b75fd9a`. The sealed source checkpoints used by this matrix are [SN `1fa43d01`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-original-custody-final/evidence/intake.json), Core `25a4ce7f`, [Server `6c25ca53`](/mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-777-union/source-handoff-6c25ca53.json) and [xops `86f5eedf`](/mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-86f5eedf-intake.json). They are source checkpoints, not one qualified release. The running Server model image uses the earlier `c60b2bc1` / Core `7de1d3e8` / SN `0fbb0ccf` tuple. A separate normal `-c -vet=off` compile on SN `5aaf88a6` / Core `25a4ce7f` / Server `80bcc285` passed: exit zero, joined, no recorded resource errors, retained image 127,324,112 bytes. That result establishes compilation only; bodies, vet and causal controls remain pending. Exact generated bytes, schema, runtime engines, policy, host configuration and affected normal/race/control evidence still need one accepted manifest. Earlier dated summaries retain their original scopes.

Astra owns implementation and source review; Sol owns admitted compiler/test execution and independent result readback; Root owns final integration and publication. Preserve healthy work, exact successful receipts and original failures. All Git commits inherit the global Bitprecipice author configuration; never add repository/worktree identity overrides.

The settled economics remain **10% of native miner allocation for providers and 90% owner recycle** (superseded on October 5 by 90% received by `ur-reserve`; see [Current reserve and economic decision](#current-reserve-and-economic-decision--october-5)), with equal weight for paid/free completed traffic. Recycling does not fund the reserve. **2026-10-06 00:00:00 UTC is the inclusive new-earnings boundary**; obligations earned before it may finish paying in USDC later, including backlog and processor retries. Do not convert their unpaid value to alpha or pay the same usage twice. The [published policy readback](evidence/current-cutoff-readiness-config-20261003.json) records `activation: blocked`; the [fresh October 4 config/planner handoff](/mnt/data/sn-testnet/mainnet-parallel-20261004/oct6-planner-ownership/HANDOFF.json) retains the same `main/sn.yml` blob `05b56036` at config main `9c4a3435`. Loaded worker policy, deployment and actual boundary enforcement remain unverified. The new planner lock is effective only after every old planner binary has been replaced; old writers do not honor it.

The owners retain their Ledger and sign on their own device; they have **no Snow access**. Snow may verify, retain and submit only the exact returned signed action. Root hardware custody is separate and still needs its actual device/API and key roles identified; current root lifecycle actions may require the owning or staker coldkey rather than the hotkey. Both the owned netuid-0 root role and UR-subnet validator role remain required, including the two initial UR instances in the bootstrap plan (one since the [October 7 decision](#one-operator-and-one-validator--october-7)). The reviewed v470 accumulation strategy needs no periodic hotkey signature or retired root-weight call, but a passive monitor alone does not establish that the actual root participant is admitted, active and earning.

A later [whole-work intake](/mnt/data/sn-testnet/mainnet-whole-work-inventory-20261004/evidence/sn-v11/intake.json), SN `864f3680`, adds immutable signed Open observations and public raw clocks: 164 roots per mode, 70 controls and three vets are authored and unexecuted. Reported Current `b037` / Server `d014` composition remains separate source work outside the sealed `1fa` / `6c25` graph below; canceled historical work remains unknown.

### Release actions remaining at this checkpoint

Declared test scopes below overlap; do not add them into a final unique-root total. A static patch-application check is not a causal body result. Sol selects and executes admitted phases under the current resource lease; source owners preserve the protected running image and prepare successors separately.

| Release lane | Actual evidence at the checkpoint | Next required result | Owner / open requirements |
| --- | --- | --- | --- |
| Claim and restore | [Corrected Claim129](evidence/mainnet-release-checkpoint-20261004.json) passes normally (1,694.27 seconds) and under race (8,152.19 seconds), exact `1eb679a4` / Core `e77caf30` / Server `f67c7ff7`, exit zero, joined and resource qualified. The original 129-review/2,048-work assertion is unchanged. Restore129 normal/race, twelve finite Claim roots per mode and six intended causal failures remain separate retained results. | Bind unchanged closure or run affected successors; finish complete owner/namespace restore and production recovery evidence. Keep original 2,184-work failure as historical evidence. | Sol / PH-01, 08, 09, 24; MG-03, 09 |
| Running Server model | [Actual 1,703-root model body](evidence/mainnet-release-checkpoint-20261004.json) is running on `c60`; frozen partial census is **1121 RUN / 1110 PASS / 1 FAIL / 9 SKIP**, with no package terminal. `TestContractCloseOriginalCanceledHistoryReadRollsBackThenRecovers` failed. The nine observed skips were preapproved, with zero unknown skips in the bound audit; missing configuration/dependencies and the optional benchmark remain coverage gaps. | Preserve this run to actual exit/cleanup/resource disposition. Qualify test-only `ed2a77ab` correction with nine roots and its causal variant; this running suite is not a suite PASS. | Sol, Bootstrap / PH-16; MG-02, 10 |
| Final source and schema | [Server `6c25ca53`](/mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-777-union/source-handoff-6c25ca53.json) joins gas `a1c9ca00`, fixture `ed2a77ab` and prior-creation `cd453`/`a17`. Prefix through 775 and exact 776 are preserved; 777 is appended. Static declarations: 1,738 model, 1,013 controller, 531 root (530 Linux). All 88 control patches apply; bodies and compiler remain unexecuted. | Finish the separately tracked wallet/roster/gas-cleanup fixture children, join and qualify the authored still-open producer `f0f44f75`/778, then seal the required Core/SN/Server graph and qualify its changed closure. | Current, Composer, source owners / PH-06, 13, 16, 27; MG-02, 05 |
| Original custody and capacity | [SN1fa/Core25a](evidence/mainnet-release-checkpoint-20261004.json) adds complete original creation/request/publication preparation and restore. The custody handoff adds six Core and thirteen SN roots including capacity. Its sealed affected union has 157 roots per mode and 43 control recipes, all unexecuted. | Qualify real public fresh/restore paths, complete original files, empty/partial crash files and cold SDK lifecycles. Keep the original birth profile; capacity increase cannot silently adopt an old root. | Recovery, Integration, Sol / PH-01, 09, 23, 26; MG-03, 09 |
| Provider work and payout | [SN `bb75`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-provider-finality-union/evidence/intake.json) retains 78 provider-consumer roots/21 controls, 148 whole-work roots/56 controls and 22 operator roots/15 controls per mode as source inventories. Server 776 and prior-creation acquisition are source-composed. | Prove the nonempty authenticated ingress → original creation/session/work → payout artifact → provider-window/Claim path, original earning/wallet selection, and independently authenticated still-open/canceled work. Producer `f0f44f75`/778 has six authored roots but is a separate unexecuted child; complete final public-path qualification and canceled historical work remain open. | Integration, Bootstrap, Current, Sol / PH-11–13, 17, 27; MG-03, 06 |
| Gas, native fees and conservation | [Gas `a1c9ca00`](/mnt/data/sn-testnet/mainnet-parallel-20261004/operator-gas-policy/INTAKE.json) implements independently pinned approval and conservative nonce/lifetime ceilings: 30 declared roots per mode, 19 controls, unexecuted. [Native readiness](/mnt/data/sn-testnet/mainnet-parallel-20261004/native-witness-review/sn/mainnet/evidence/native-qualification-readiness-20261004/readiness.json) names 22 Rust roots, 86 Go roots per mode and 23 controls on its own pinned graph. | Execute matching current capture/replay engines and native fee/finality/conservation consumers. Retain full gas ceilings; actual native-fee proof, denomination admission and idempotent liability release remain an explicit gate. Prove original 10/90 allocation; receipt status, inferred refunds and policy renewal release no reserved ceiling. | Native, WholeFee, DepositWallet, Sol / PH-04, 11, 12, 19; MG-04, 06 |
| Runtime, HTTP and recovery | Finality `74bcb574` preserves first/highest witnesses and implicit selection through the outer owner; its joined affected partition has 33 roots per mode. HTTP `3d7734ab` adds ten roots, 46 affected roots per mode and 19 controls. All remain source-only. | Execute affected normal/race/vet and operative controls on the joined graph; preserve deadlines, hard causes, original signed history, bounded parallel repair and actual child joins. | Budget, HTTP, Recovery, Sol / PH-03, 07, 18, 19, 21, 22, 25 |
| Monitoring and dashboard | [xops `86f5eedf`](evidence/mainnet-release-checkpoint-20261004.json) executed 311 roots with real pinned Prometheus/promtool: **309 PASS / 2 FAIL / 0 SKIP**, 47.962 seconds, exit one, joined. Failures affect the producer-census parser and deep-JSON refusal expectation. The exact SN `09f8f989` pin, 104 panels/177 queries and 14 chain/36 conservation/six controller gauges remain bound. | Qualify source correction `55f1acf0`, its adjacent cases/controls and unchanged 311-root combined scope. Reliability/provider-window gauges remain unknown. Heartbeat cannot refresh outcome freshness; actual Grafana/Mimir ingestion, alert delivery and on-call/repair drills remain open. | Sol, Monitoring, Ops / PH-15, 28; MG-07 |
| Custody and both validator roles | Owner-local Ledger workflow `fc55c559` adds three roots; 59 selected roots/seven controls remain unexecuted. Current two-UR start code exists. Current v470 root participation uses registration/stake/basket state; no periodic SetRootWeights is required. | Supply actual owner/device approvals and signed originals, identify separate root hotkey/coldkey custody, verify admitted active/earning root and two UR roles (one since October 7), installed contracts and approved host inputs. Software fixtures do not supply those facts. | Owner, Bootstrap, Root / PH-14; MG-01, 08 |
| Rollout and acceptance | October 6 new-earnings policy remains published with activation blocked. No loaded-worker adoption, deployment, real economic outcomes or final mainnet acceptance is established. | Replace every old payout planner before relying on the shared lock; preserve all pre-cutoff USDC debt/retries. Complete rollout/rollback/outage/restore rehearsal and, after authorized activation, observe three native emission intervals plus a complete 50,400-block UR settlement/claim cycle. | Root, operators / all 38 outcomes; MG-10 |

The final testnet remains closed. Its accepted operating exceptions and later repairs do not rewrite [R48's terminal result](../sim-testnet/FINAL-4.md): zero complete acceptance epochs and `final_acceptance=false`. Preserve missed native history, low-usage/provisional readiness and uncredited historical debt as explicit exceptions; mainnet qualification must address their production causes without restarting the closed testnet campaign.

Use the current reviewed finite resource admission and a fresh physical/memory check for every new qualification phase. Preserve the original 113,681,502,208-byte baseline, existing live reservations and 2× forecast margin. A larger scratch allowance, reclaimed checkout space or successful compiler is neither a product pass nor production-capacity evidence. This documentation reconciliation executes no compiler, database, test body, signing or deployment.

## Retained component progress — October 4

**October 4 continuous monitor deployment published:** The [latest independent publication readback](evidence/xops-fee-profile-current-publication-root-readback-20261004.json) verifies xops main `ba2f5323`, exact changed source/tool hashes and all 85 independent passes, including real Prometheus/promtool execution. It retains original durable roots and immutable input lineage, six independent role rosters, the 300-second read budget and explicit populated fee-profile bounds. The separately configured continuous conservation owner is still being added; production host activation and exercised alerts/on-call remain separate.

Future qualification uses the [100GiB cumulative scratch admission](evidence/future-parallel-qualification-resource-admission-20261004.json) under the standing automatic budget authorization. Preserve the original 113,681,502,208-byte baseline and 2× margin. At most two warm Go compilers and two bounded short bodies may overlap after fresh disk/memory admission; Rust, database fixtures and long history bodies require separate forecasts. Preserve running jobs and retained images; apply updated guards only to subsequent phases. Database qualification also retains the original Docker-root baseline and requires an exact container-owner census before launch.

**October 4 current signed-close source:** The [current generation readback](evidence/original-close-current-generation-root-readback-20261004.json) verifies global-author Core `7a805bd0` with the same generated tree and wire bytes as the earlier receipt. Actual protocol compilation exposed a test fixture passing a private-key slice as the public-key type; no behavior test ran. Correct the fixture, retain production signing and descriptor bytes, and qualify the real Core/Server/SN join. Generated source and compiler success are separate prerequisites.

**October 4 original Rust controls complete:** The [raw seven-control readback](evidence/original-capture-seven-rust-controls-root-readback-20261004.json) verifies21 clean/compile/body phases and seven exact intended assertion failures, with no ignored root and joined children. Retain the original fourteen capture/Yuma assets and successful exporters. Remaining work is affected Go integration, full funding/finality/fee consumers and current release qualification.


**October 4 actual vault fixture correction:** The [3f source review](evidence/capture-precompile-3f-root-source-review-20261004.json) confirms a test-only correction: synthetic precompile accounts now have code presence required by Solidity typed void calls, while actual registration, UID lookup and stake movement remain executed by the explicit adapters. Check those actual selector counts and unchanged success/rollback accounting. Qualify the affected vault/capture roots and a fixture-owned omitted-code control; no production contract behavior is weakened.


**October 4 corrected historical worker progress:** The [raw four-root readback](evidence/funding-corrected-proof-four-root-readback-20261004.json) confirms three passes and one RootMissed source-page failure in each mode. The correct `runtime-historical-proof` target resolves the wrong worker invocation; its original-income/later-payment, lost-ack composition and complete Yuma10/90 tests now pass. Keep `runtime-transition-replay` as a distinct protocol worker rather than substituting it by filename similarity. Preserve these six successes; repair the missed-root continuation and the separate real-vault precompile fixture before rerunning affected tests. Full economic acceptance remains incomplete.


**October 4 original-close wire generation:** The [pinned protobuf readback](evidence/original-close-pinned-protobuf-root-readback-20261004.json) verifies the exact generated Core child, unchanged proto input, maintained generator versions and sole generated-file delta. Original signed-close bytes use new field6 while stable report identity remains field5. The generation prerequisite is resolved; stage exact declared Core peers and qualify real producer, Server retention and SN consumers before publication. A generated descriptor is not a passing transport or accounting result.

**October 4 coherent capture/funding correction:** The [cd2 source and controls review](evidence/funding-dual-profile-cd2-root-source-review-20261004.json) verifies a five-path successor: remove the unsupported Go capture surcharge while retaining actual Rust/worker limits, select the maintained vault artifact and advance the original Claim expectation through matched epoch retirement. Four new deterministic roots cover profile combinations, exact boundaries, required-query rejection and real vault success/rollback. Qualify its19 affected/new/adjacent roots in both modes, vet and four labelled controls per mode using the matching8ad engines; retain all other successful partitions. Source review supplies no passing execution or mainnet acceptance.

**October 4 optional provider configuration liveness:** The [domain-reader source finding](evidence/provider-optional-domain-nonregular-source-finding-20261004.json) identifies blocking path open before descriptor admission: a FIFO with no writer can stall provider startup although close evidence is optional. Admit a regular file through nonblocking descriptor handling, bound its bytes and reject ambiguous documents. Missing or invalid optional input records a diagnostic and leaves the provider unsigned and operating. Add deterministic nonregular/no-writer tests and bounded worker cleanup; a byte cap alone does not prevent startup hangs.

**October 4 matching engines compiled:** The [independent build readback](evidence/funding-8ad5-matched-engines-root-readback-20261004.json) verifies all33 exact crate files, actual Cargo success for capture and replay, and two distinct protected executable images. Use these matching engines with the retained original capture/Yuma assets; the older engine/profile mismatch stays recorded. Only affected dual-engine partitions are rerun. Compilation alone does not establish execution behavior, funding conservation or mainnet acceptance.

**October 4 retained funding terminal results:** The [independent raw terminal census](evidence/funding-remaining-and-f51-terminal-root-readback-20261004.json) confirms20 ordinary passes and one fixture failure in each mode, plus nine dual-engine failures in each mode under the original mismatched composition. The f51 successor gets beyond the chain-identity seam but fails at a reused Claim epoch precondition in both modes. Review the complete fixture continuation before another successor, including expected epochs, signed admission, archive/reset and exact retries. Preserve the forty ordinary successful executions; rebuild matching dual engines and rerun affected bodies only. Funding qualification remains incomplete.

**October 4 combined capture and engine findings:** The [original dual-engine failures and exact source review](evidence/funding-dual-engine-original-failures-source-review-20261004.json) retain nine normal failures. The native capture caller allows272MiB plus two4MiB principal companions, while its shared worker rejects output above272MiB before execution. The actual Rust producer already includes both principal companions in its total272MiB native output limit; remove the unsupported Go-only8MiB surcharge and keep producer, caller and worker limits consistent. Test exact boundaries and adjacent modes. Five capture fixtures also request `STSettlementVault` although the maintained catalog names it `SettlementVault`. The Yuma continuation used an older engine without its required observation purposes; qualify fresh capture/replay engines from the actual matching production closure. Preserve successful ordinary tests and original assets, fix these related joins as a batch and rerun only affected partitions. No corrected execution or economic acceptance is established yet.

**October 4 funding fixture correction:** The [f51 source review](evidence/funding-candidate-f51-root-source-review-20261004.json) verifies a single test-only successor to the original funding candidate-isolation failure. The earlier fixture combined synthetic-mainnet authority with fixture-mainnet live readers and therefore refused before reaching its intended custody check. Enroll the fee and Claim authority before the live fixture first publishes; preserve its actual network and every original isolation/retry assertion. Qualify only the corrected root normally and under race detection, plus vet, retaining successful union partitions and the original failure. Production network identity checks remain unchanged.

**October 4 Server identity control results:** The [independent raw control readback](evidence/server5b-controls-root-readback-20261004.json) verifies all fourteen intended assertion failures across three control groups in normal and race modes, with no panic, timeout or skipped root. Together with the retained64 positive results and model vet, these bodies exercise immutable participant identity, deleted-client continuation and provider-local conflict isolation. The original resource wrapper remains unqualified: one PostgreSQL lifecycle observation failed after the final test body, followed by both containers observed absent and successful cleanup. Preserve that telemetry gap and qualify its teardown checker separately; do not repeat successful bodies or treat this selected scope as full Server acceptance. Optional original-evidence capacity must likewise leave ordinary valid close accounting available while recording unknown provenance; caller cancellation and actual signature contradictions retain their separate refusal semantics.


The [corrected original-capture exporter readback](evidence/funding-8ad5-original-capture-exporters-pass-root-readback-20261004.json) independently verifies both actual roots passing, joined descendants and seven exact private0600 outputs. Combined with seven retained Yuma/populated files, all14 original fixture prerequisites are available. Continue remaining funding41 partitions on existing protected Go images and the finality17 scope; preserve nine prior ABI positives and two framing successes. Fixture/control qualification and full conservation remain separate from these completed prerequisites.

The [original-finality implementation review](evidence/economic-finality-f7-production-tests-controls-root-review-20261004.json) covers frozen production integration, all13 new deterministic roots and seven exact operative controls. Retain original independently signed checkpoint/certificate bytes, require every quiet and financial EVM boundary, preserve economic cursors and incomplete evidence, and cache under held custody with transactional2x capacity charges. Qualify17 roots per mode, vet and14 causal executions on the actual dependency graph using corrected8ad capture assets. Complete provider provenance, whole original fees, runtime approval and activation remain independent requirements.

The [committed-capture correction review](evidence/funding-capture-committed-effects-source-review-20261004.json) distinguishes two genuine host observations (get/set) from one independently censused committed write. The exporter now verifies the pair’s actual before/after values, key, Apply identity and mutation ordinal/hash while retaining original amount, transaction, count and rollback assertions. The adjacent Go consumer already filters committed writes and joins original mutations; no production decoder/count relaxation is indicated. Qualify the two affected exporters and seven controls, preserving seven Yuma assets and two framing successes.

The [capture-framing execution](evidence/funding-e00-framing-pass-capture-census-failure-root-readback-20261004.json) passes both new deterministic framing roots. Both exporters now reach actual original capture and fail named census assertions (two versus one, four versus two). Review actual hook/mutation/transaction/Apply identity to distinguish observations from economic effects before adjusting fixtures or consumers; no capture output is accepted from these failures. Preserve both framing passes and seven prior Yuma assets while independent Server qualification continues.

The [qualified validator fix is published on main](evidence/validator-current-qualified-publication-root-readback-20261004.json) at `61972f83`. Cached terminal replay now remains bound to its original held archive custody and checks cancellation/admission before returning. The two published files exactly match the tested current-release candidate; all other Git paths/modules remain unchanged, and remote main is independently verified. This is an integrated component fix, not full mainnet acceptance.

The [current-release validator integration qualification](evidence/validator-current-e9-three-roots-vet-qualified-root-readback-20261004.json) independently counts all three affected original roots passing in normal and race modes, with zero failures/skips and successful vet. Full20,849-source and seven-peer closure is rechecked after execution. Together with retained14-root positives and12 causal controls, this qualifies publication of the exact two reviewed validator files on the currentCore2ea graph. Full historical-lineage/contention and all-role launch requirements remain separate.

The [capture-framing correction](evidence/funding-capture-extrinsic-framing-source-review-20261004.json) explicitly types the actual synthetic transaction bodies as bytes and adds independent canonical SCALE/header checks plus real capture/replay refusal tests for inferred integer words, truncated/trailing payload and nonminimal length. Keep strict production decoding and all seven passing Yuma assets. Execute the two failed exporters and two new regressions; existing five controls plus an inference-restoration control remain. The [scratch-cap addendum](evidence/qualification-scratch72-root-admission-addendum-20261004.json) raises only future test scratch allowance to72GiB under the user’s automatic budget approval, preserving the original baseline and2x resource margins; it grants no mainnet spend or signing authority.

The [original runtime exporter result](evidence/funding-fd922-original-exports-two-pass-two-fail-root-readback-20261004.json) independently counts two passing Yuma/populated roots and two failing capture roots after the corrected libtest compiles. Preserve the seven successful original output files and run their dependent Go scopes. Both capture failures reject nonexact SCALE bytes; Astra traced untyped integer vectors in the fixture to incorrect byte framing. Correct the test producer with explicit byte types, keep exact production decoding and add framing regressions. No successful capture asset or full funding qualification is inferred.

The [current-release validator image readback](evidence/validator-current-e9-two-image-root-readback-20261004.json) independently verifies successful normal and race compiler exits, joined descendants and exact protected image hashes/inodes. Execute the three affected roots per mode and vet before publication; compilation alone does not close the current-release integration gate.

The [corrected original-export runner](evidence/funding-fd922-original-export-runner-root-adoption-20261004.json) is reviewed and adopted subject to fresh compiler-slot/resource admission. Its four exporter definitions and fourteen output sets exactly retain the original recipe; the only source delta is the reviewed test import. Wait for the current validator compiler to join, then start under the existing2x reserve without another approval round or rebuilding unaffected Go images. Actual compile, exporter bodies and five causal controls remain unproven.

The [five corrected Rust causal controls](evidence/funding-fd922-rebased-controls-root-source-review-20261004.json) reproduce each exact original omission against the corrected source while retaining the codec import. This checks that fixing compilation did not erase the transaction-body, apply-index, Yuma-input, pending-parent or populated-matrix regression tests. Each control still needs a fresh compiling image and its named intended failure; source comparison is not qualification.

The [corrected original-export crate review](evidence/funding-original-export-codec-successor-source-review-20261004.json) verifies exactly one shared test-module SCALE codec import in successor `fd9229`. Its child Yuma modules inherit the alias, addressing all five observed compiler errors without changing economic assertions, production Rust, Cargo dependencies or Go source. Compile the corrected crate and execute the original four exporters/five controls; existing exact Go images remain reusable. The [current-release validator compiler review](evidence/validator-current-e9-image-runner-root-adoption-20261004.json) independently verifies all20,849 source entries and seven clean peers, then admits two protected images under the original cumulative resource budget. Three current dependency-graph roots per mode and vet remain required before publishing the qualified validator fix.

The [actual original-export libtest compile](evidence/funding-ab48-original-export-compile-root-finding-20261004.json) fails with five unresolved `codec` namespace references across three fixture source files. No exporter body ran. Repair the imports in a separately frozen successor, retain the original compiler failure, and rerun the unchanged four-export/five-control scope; this is a test-source compile defect, not an RPC or production-runtime failure. Independent component qualification continues.

The [original funding exporter runner review](evidence/funding-ab48-original-export-operational-root-adoption-20261004.json) admits the exact four positive exporter roots after fresh resource and full crate/toolchain/helper checks. Reuse warm dependencies while freshly compiling the package libtest; retain each original output and independent failure, protected image custody and joined descendants. Five causal controls remain separate. This admission supplies no passing execution or measured populated-workload sizing.

The [independently counted Server identity scope](evidence/server5b-selected-model-two-mode-root-readback-20261004.json) passes all 32 selected roots in both normal and race modes with zero failures or skips. Protected images retain exact hashes and inode custody; the fixture exits and joins cleanly, with complete sampled PostgreSQL/Redis coverage. Model vet and three causal-control groups remain before component qualification. Preserve the original workspace refusal beside this successful recovery.

The [duplicate-close-report source review](evidence/close-report-identity-tests-controls-root-review-20261004.json) covers all 17 new deterministic tests and eight production controls. Retain original authenticated client/ReportId and exact content atomically with byte accumulation; retry committed settlement after a lost reply, and keep report identity after contract/client cleanup. Public controller routing and equal paid/free/companion completed-byte weighting are covered at source level. Qualify the 34-root normal/race scope, three vets and eight controls per mode before deployment. Durable transport-authenticated reports alone do not prove independently signed physical work or a complete earning window.

The [original funding exporter scope](evidence/funding-ab48-original-export-scope-root-review-20261004.json) verifies all 33 crate source entries against the original reviewed composition. Execute four actual exporters, retain 14 exact outputs and qualify five causal controls, then feed those results to the existing corrected Go images for the full funding scope. Source recipes and matching hashes do not replace execution or measured populated resource sizing.

The [Server fixture workspace recovery](evidence/server5b-logical-workspace-recovery-review-20261004.json) fixes logical-versus-physical module path admission without rebuilding valid test images. Preflight the exact child helper command, working directory and GOWORK; a model-only graph cannot prove the startup helper resolves. The corrected helper and model graphs pass, and the unchanged 32-root normal/race scope is adopted. Preserve the original zero-body refusal and successful cleanup; actual model execution remains pending.

The [corrected ABI two-mode readback](evidence/coordinator-ab48-nine-two-mode-independent-readback-20261004.json) verifies nine normal and nine race passes, no skips or failures, joined exit zero and exact protected image bytes. The four original fixture failures now pass both modes. Fixture-control/vet and complete funding41 remain separate pending scopes.

The [current-main validator integration](evidence/validator-terminal-current-integration-source-review-20261004.json) contains only the exact two qualified terminal-custody files over current main. Other source and module bytes remain unchanged. Because the current Core/artifact package graph differs from the original qualification, execute the small affected-root normal/race and vet scope before publication; retain the already qualified original bodies and controls.

The [corrected ABI nine-root normal readback](evidence/coordinator-ab48-nine-normal-independent-readback-20261004.json) independently reproduces all nine passes, including the four original fixture failures, with no skips or failures and joined exit zero. Race and fixture-control results remain pending. Keep this result separate from full funding41 qualification and original failures.

The [original e51 partition readback](evidence/recoverye51-original-partitions-independent-readback-20261004.json) independently verifies all 47 normal roots: 43 pass, four fail, zero skip. It retains sealed race partitions separately and preserves the four original coordinator-fixture failures. Corrected ABI replay must qualify the affected roots; retain unaffected original results with explicit source/dependency joins. The separate 129-history recovery remains incomplete.

The [contextual provider correction](evidence/provider-context-cancellation-source-review-20261004.json) is source-reviewed across 12 changed files and nine deterministic regressions. Actual operation context reaches row copying, payout allocation, Merkle construction and publication/readback without changing legacy grammar. Independent canonical-value neighbors and a public HTTP cancellation-classification regression remain part of qualification.

Corrected `ab48` normal/race mainnet test images compile, but the original Yuma, populated 1,024/2,048-UID and vault-sequence exporters remain source-only. Qualify those actual original-runtime producers before the dependent funding tests; run independent ABI/recovery roots meanwhile and retain explicit unexecuted status for missing assets.

The [qualified validator selected component](evidence/validatorb6-selected-component-qualified-20261004.json) combines fourteen passing roots per mode, vet and all twelve intended causal-control failures. Root independently verified the final nonempty-head lineage and terminal-custody groups. Integrate the exact reviewed validator delta with current source/dependency evidence; broader-history and full release requirements remain open.

The [funding control review](evidence/fundinge0-operative-controls-source-review-20261004.json) verifies eight operative production mutations for original income/carry, retained totals, capital contradictions, tolerance, stale acceptance and owner custody. They remain unexecuted; join the corrected ABI fixture before full funding qualification.

The [closed-work behavioral source review](evidence/provider-closed-work-tests-controls-source-review-20261004.json) covers all 23 new deterministic roots and nine operative control groups. It includes actual paid/free SQL closure, retention migration, operator publication/retry, validator HTTP reconstruction and monitor active/cold evidence. The 50-root selected normal/race scope, six vets and 13 intended causal failures per mode still need execution; source review does not close provider authentication.

The [validator causal-control readback](evidence/validatorb6-four-controls-independent-readback-20261004.json) reproduces four operative groups in both normal and race modes: missing predecessor, unnecessary history reread, omitted cached-lookup custody and partial cache publication. Each modified build compiles and fails at the intended assertion while the original selected positive root passes. Two groups remain; retain the verified eight outcomes without repeating them.

The [Server attribution selected-test runner](evidence/server5b-selected-model-runner-root-review-20261004.json) is reviewed and adopted for 32 exact roots in normal and race modes using protected compiled images and private owned database/cache services. Fresh aggregate resource admission remains mandatory; the original cumulative baseline stays intact. Independent event counts, process joins and service cleanup are required before qualification. This does not qualify the later provider producer or whole Server suite.

The [close-report identity finding](evidence/close-report-dropped-identity-source-finding-20261004.json) confirms that Core retains a stable report ID on retries while the Server controller drops it before the incremental checkpoint update. Duplicate delivery can count bytes again. Retain report identity and semantic content atomically with accounting, make identical retries idempotent, and refuse conflicting reuse locally. Qualify concurrent retries, independent equal-byte reports, terminal retries and rollback/restart continuity; preserve legacy empty-ID behavior and review every hosted/native route before client rollout.

The [provider closed-work production review](evidence/provider-closed-work-production-source-review-20261004.json) verifies frozen SN `768e188a` and Server `5460b1d7` against exact source blobs. Usage and original rows share one SQL snapshot; an over-capacity optional census is discarded completely. Validator and monitor reconstruct identities, byte partitions and active/cold counters. This establishes a source component, not complete physical-work, reliability, eligibility or independent-window authentication. Qualify the declared tests and controls, then finish those authority producers before claiming the economic gate complete.

The [coordinator ABI fixture correction](evidence/coordinator-tuple-ab48-source-root-review-20261004.json) is source-reviewed at `ab48c41d`: both uint256 policy caps are concrete zero values, and a new actual HTTP/generated-ABI regression covers the policy, operator, commitment, vault and epoch getters. It changes no production timeout or retry behavior. Qualify the corrected fixture against all four affected original roots; retain original failures and other passing bodies.

The [partial e51 entitlement failure review](evidence/entitlemente51-partial-abi-tuple-fixture-finding-20261004.json) confirms three actual failures share a coordinator-fixture panic: policyAt leaves the two big.Int deposit caps nil, and ABI Outputs.Pack panics before responding. The 300-second GET retry runs correctly on EOF; the later artifact liveness guard never reaches HTTP. Initialize every ABI tuple field, inspect adjacent builders and add deterministic pack/public-route regressions. Preserve the original failures and keep remaining partitions running; this partial observation is not a complete 20-root verdict or production-node diagnosis.

The [validator14 two-mode terminal and vet readback](evidence/validatorb6-normal-race14-vet-independent-readback-20261004.json) independently reproduces fourteen normal and fourteen race passes, zero skips/failures, joined successful bodies and vet. Both protected images match their original receipts. Run the six staged causal control groups before claiming the full selected scope qualified; these results do not qualify other role or funding changes.

The [funding custody child review](evidence/fundinge0-custody-source-root-review-20261004.json) verifies exact e0f1011d over72 with four changed paths and six declared regressions. Public funding projection checks original owner/context before and after arithmetic and returns nil on refusal; private cold admission retains its one complete final fence. Tests count exactly two complete hot checks without payload rereads, refuse late equal-byte inode replacement/cancellation without JSON, and preserve a separately held original across candidate refusal and identical retry. The suspected shared-candidate mutation is not established as a product defect; the actual public isolation test remains to execute. This source correction is not qualification.

The [old normal 129-review timeout readback](evidence/restore-ade5-normal129-original-timeout-readback-20261004.json) independently counts one executed failure at 7200.04 seconds, joined exit 1 and the original archive-admission/custody stack. It is consistent with the reviewed repeated-prefix source defect, but proves neither corrected throughput nor a corrected full-history pass. Keep the original failure and automatically advanced race case; corrected ordinary work proceeds independently. Do not classify this local test deadline as an ephemeral RPC outage or restart preparation.

The [corrected e51 mainnet build readback](evidence/conservatione51-two-mainnet-builds-independent-readback-20261004.json) verifies successful normal/race compiler exits, joined processes and original owned test-image hashes. Sol runs the 47 ordinary roots in 20/20/7 partitions per mode and preserves the separate 129-review singleton, prior compiler failures and validator results. Compilation is not behavior or final conformance acceptance.

The [selected funding72 mathematical review](evidence/funding72-selected-mathematics-source-review-20261004.json) retains exact source and eighteen declared ordinary/public roots. Fungible stake uses conservative income/non-income bounds; missed roots and later claims transport original value without new earnings. Full Yuma tolerance is counted once, and reassessment drops stale target, fee and source projections. Qualify actual public capture/carry/credit/payment and cold/lost-ACK continuations after the owner-fence successor; candidate map isolation remains under review. The complete original measurement, finality and provider-fee producer joins remain required. This selected source review proves no execution or readiness.

The [funding-cache publication review](evidence/funding-public-cache-custody-source-finding-20261004.json) finds a related WIP owner-fence gap: earlier component admission precedes direct cold-funding map/counter use, and the public summary has no final archive-owner check. Bound summary computation/publication with original owner and cancellation checks; late identity loss must return no accepted projection. Keep private cold admission and its one complete final fence separate so this correction does not restore repeated historical prefix work. Add closed/canceled/replaced-owner and healthy no-reread regressions. This is a source finding assigned to Astra, not a reproduced test failure or completed correction.

The [actual b6 normal validator body](evidence/validatorb6-normal14-independent-body-readback-20261004.json) passes all fourteen selected roots with no failures/skips, exit 0 and joined processes. Root independently counted original test2json events and rechecked the protected image. Race, vet and six causal controls remain separate; preserve the successful normal body while correcting the unrelated mainnet precompile fixture.

The [e51 fixture correction and original four-build readback](evidence/conservation-capturee51-source-and-original-build-readback-20261004.json) verify the actual imported geth1.17 interface, exact six-line insertion, two retained mainnet compile failures and both successful protected validator images. Continue fourteen validator roots per mode using those images; compile only the corrected mainnet package and then resume its 48-root union. Source admission is not corrected execution.

The [immutable participant successor review](evidence/server-participant5b22-source-root-review-20261004.json) verifies exact Server 5b22 over 4a5 with only two changed paths. First admitted provider identity survives ordinary and companion retries after membership changes or removal. New streams still require current identity; conflicting retained roles hold only affected work. Qualify ten new and 22 neighboring model roots per mode, model vet and three causal controls/seven intended red roots per mode. Missing/canceled/aborted first admission must publish neither stream link nor partial participants. This is source-only and leaves the original full model result intact.

The [original b6 compiler failure](evidence/conservationb6-mainnet-original-compile-failure-readback-20261004.json) retains three exact missing-Name assignment diagnostics, exit 1 and joined compiler processes. The effective VM interface must be checked for every actual or fake precompile before qualification; graph resolution alone does not establish source compilation. Fix the test fixture against the pinned interface, collect remaining independent compile outcomes and preserve valid validator/history work. No mainnet body ran in this failed compile.

The [clean terminal-custody join review](evidence/conservation-follow-terminalb6-source-root-review-20261004.json) binds b6 to exact ffe plus two frozen464 validator paths. Terminal cached closure reads now recheck the original owner before returning an owned copy; cancellation or directory replacement refuses without a source-history replay. Sol is staging this exact candidate; 62 roots per mode and 14 declared controls are not yet qualified.

The [joined follow/history source review](evidence/conservation-follow-historyffe-scoped-source-root-review-20261004.json) verifies exact `ffe74b36`: native-worker isolation, bounded private cold admission and the actual-monitor restore fixture correction are joined without manual resolution. Cold admission reads every original page and runs one complete final custody fence before publishing its index; hot use and fresh reopen retain their own checks. Restore tests obtain actual runtime companions and compare the checked original durable state. Qualify48 mainnet and11 unchanged validator roots per mode plus13 controls; keep the full129-review singleton isolated and preserve prior successes. This join does not close funding conformance or the separate newly discovered terminal-custody seam.


The [dependent dual-engine normal19 readback](evidence/admissionab0-dual77-normal19-independent-failure-readback-20261004.json) records nine passes and ten failures with every root executed and no skips. Nine failures share a missing completed-block runtime read context; trace the restore fixture finite-CLI path against the real complete monitor observation rather than fabricating runtime fields or weakening admission. The separate full restore-state comparison failure remains under investigation. Preserve the nine successes, continue race and long-history work, and batch root causes with adjacent deterministic tests before qualifying affected successors.


The [continuous native-worker source review](evidence/conservation-native-followce78-scoped-root-source-review-20261004.json) verifies `ce78b81c`: one bounded native capture/replay worker hands its actual result to the parent under the exact original native predecessor and policy. Vault, Claim and artifact publication can continue while native work is pending; missing evidence remains pending, transient reads retry, and a genuine native contradiction holds only that domain. Cancellation joins the worker before outer custody closes. Qualify seven new and five neighboring roots per mode plus three exact controls. Full native-income-to-entitlement funding conformance remains separate and must exclude opening stock, deposits and refunds from earnings.


Qualification scheduling must follow actual workload and resource ownership. A restriction against a second long128/129-history body must not serialize unrelated ordinary dependent tests behind the first. Once exact source/image/export prerequisites and fresh combined resource admission pass, run the ordinary work alongside the retained long body with its original reservation included. Keep per-phase concurrency bounded and retain original outcomes; an unfinished long history is not a prerequisite for independently scoped ordinary roots.


The [three drained-exporter causal controls](evidence/proof-drained-continuation77-three-controls-independent-readback-20261004.json) independently reproduce the exact old first-block guard failure, repeated-accrual second-block failure and invalid nonzero opening-parent rejection. Every isolated mutation compiles successfully and reaches its named assertion; setup errors do not count as proof. Together with the six retained positive roots this qualifies the targeted fixture correction, while dependent Go execution and full composed-release acceptance remain separate.


The [populated composed-source review](evidence/conservation-populated3a13-composed-source-scope-review-20261004.json) verifies exact `3a13c69` inputs,174 Go declarations per mode,17 Rust roots and65 exact rebased controls. The join retains the large physical profile, complete restore owners, entitlement census, nonzero populated1,024/2,048-UID workload and corrected drained dispatch. Logical2x reserves must be followed by actual complete serialized-size and host measurements; sparse or zero-stake padding does not establish production capacity. Qualify the composed source separately from retained component results. Native-follow isolation, archive-prefix performance and full native-income-to-entitlement funding conformance remain subsequent implementation joins.


The [corrected exporter independent readback](evidence/proof-drained-continuation77-six-root-independent-readback-20261004.json) verifies a fresh Rust test image, six exact passing roots and all22 private hashed exports. This supplies the previously missing five-job proof-drained corpus without weakening the original post-state verifier. Advance the nineteen dependent Go roots using retained exact images; three causal controls and complete joined-source qualification remain separate. The original failed exporter and running long recovery remain retained.


The [corrected proof-drained exporter source review](evidence/proof-drained-continuation77-scoped-root-source-review-20261004.json) verifies the sole test-file change at `77a4ab38`. First-block dispatch uses the independently authenticated absent epoch marker, so a drained parent can execute accrual; later jobs retain that marker and do not repeat emissions. All five expected child roots remain independently constructed, and actual capture plus reduced replay are required before export. Qualify six Rust roots and three causal controls using a fresh libtest image while retaining unchanged qualified engines and Go images. Keep the original failure, successful ordinary roots and live long recovery; dependent Go results remain pending until the corrected corpus exists.

The [corrected single-root qualification](evidence/policy-refusalb1-one-root-and-retained-ordinary-qualified-20261004.json) verifies the policy-refusal assertion normally and with race detection at `b1`. Join that narrow result with the twenty unchanged original `ab0` successes per mode; preserve the original20PASS/1FAIL batches and vet result. This establishes the selected ordinary21 scope through retained evidence and one corrected test, rather than redoing every successful body. Dual-engine, long-history, causal-control and final composed-source work remain separate.

The [independent terminal and drained-program finding](evidence/admissionab0-ordinary-terminal-and-drained-root-finding-20261004.json) verifies20 passes/one diagnostic assertion failure in both Go modes and vet success. The new Rust test image compiles, but the proof-drained exporter correctly fails post-state reproduction: its initial accrual sits inside a nonzero-pending guard that a zero-pending parent cannot enter. Correct that specific original-program first-block gate and retain independent expected roots plus all later empty jobs; changing the verifier or copying its returned root would hide the defect. Keep the nineteen dependent roots visibly unexecuted until the corrected job corpus exists, while other implementation work continues.

The [independent ordinary21 readback](evidence/admissionab0-normal21-failure-and-b1-source-root-review-20261004.json) verifies20 normal passes and one retained diagnostic-order assertion failure. Both joined fee/Claim/native adoption regressions, actual initial Claim observation and the originally failing fee-revision root pass normally; their race/control and full joined scope remain separate. A narrow test-only `b1` successor accepts either actual original-policy refusal while preserving exact no-output, namespace and retained-identity checks. Do not label a refusal as owner reset merely because the diagnostic reached a different valid boundary.

The [joined admission source review](evidence/joined-admissionab0-scoped-source-root-review-20261004.json) verifies the batched `ab0` correction against eight exact changed Git inputs and41 declared roots per mode. After fee compaction forks an archive view, every later Claim/native adoption must consume the same fully admitted original view; a failed signed successor must leave the original checkpoint and obligations unchanged. Exercise lost acknowledgement after actual directory sync, repeated identical application and reopening from retained archives. Keep command-specific review files in separate namespaces so a later request cannot replace the storage preparation request. These source changes await behavioral qualification and do not relax custody or signature checks.

The [independent counter correction review](evidence/conservation-counterab0-independent-source-review-20261004.json) verifies the test-only `ab0` successor before compilation: compare the actual unsigned observation counter with an unsigned count and retain the original source finding. The corrected joined archive batch still requires its41-root normal/race qualification. The [independent normal full128 recovery readback](evidence/claim-full128-normal-recovery-independent-readback-20261004.json) verifies one actual public Claim-window root, exact RUN/PASS census and joined process exit; race and remaining recovery bodies are separate. Preserve successful original work while qualifying the successor.

Mainnet readiness remains unproven. The economic decisions are settled: providers receive 10% of the native miner allocation, 90% is recycled through the owner path (superseded on October 5: the 90% goes to `ur-reserve`), and paid/free completed traffic has equal weight. The October 6 `00:00 UTC` cutoff changes new-earnings attribution; pre-cutoff USDC obligations may finish paying later. Published configuration remains blocked from mainnet activation until the exact deployment package is ready.

The [requirement mapping correction](evidence/readiness-requirement-outcome-mapping-correction-20261004.json) restores every original outcome in the current checkpoint. PH-19 concerns historical runtime authority; economic funding and settlement belong to PH-11/PH-12 and MG-06. MG-03 retains complete durable recovery, and MG-10 retains release rehearsals plus the original post-activation observation intervals. Regenerate the readable checkpoint table from its JSON rows so dated component progress cannot silently replace the complete requirement. All38 requirements remain open; this tracking correction grants no implementation or acceptance credit.

The [fee/Claim archive handoff finding](evidence/fee-claim-archive-handoff-root-finding-20261004.json) confirms a product composition defect reached after correcting scratch ancestry: fee retirement forks a private archive view, then the planner updates Claim admission on the authoritative view while the candidate retains the stale fork. Reattach the exact admitted view after successful admission and before Claim/native adoption; preserve fee calculations and original authority rather than loosen the guard. The public regression must combine fee retirement with Claim/native continuation and verify refused admission publishes nothing. Collect the other checkpoint, drained-activation, restore-request/mount and declared-forecast failures in the same correction batch.

The [repeated capture source review](evidence/native-capture263b-sequence-source-root-review-20261004.json) preserves all production and module bytes while adding actual same-block transaction ordinals, next-block capture after archival and delayed first-receipt recovery through retired native inputs. An adjacent older outage fixture targeted a transaction removed by capture setup; explicitly target the actual original receipt and verify the fault is reached before interpreting recovery. Same-block deposited5 is not repeated earnings, and the archived opening stock14 is not counted again. The final pending union is86Go roots per mode and15Rust roots with its exact controls; execute the composed admission/profile/entitlement successor rather than rebuilding every superseded intermediate. FullUID zero-stake vectors do not settle nonzero-validator work budgets; qualify realistic weight/bond density and actual host sizing separately with the selected2x margin.

The [physical storage profile review](evidence/economic-storage5f5-scoped-source-root-review-20261004.json) verifies the fixed32MiB owner, separately signed monotonic logical head budget, original archive forecasts and matching preparation/restore registry. The full1024/2048 synthetic jobs retain every UID and row, with explicit zero-stake additional identities; they do not establish dense-population or production host sizing. Public tests cover larger heads, exact cold snapshots, signed growth and actual physical restore. Qualification remains pending on the composed source, including complete native-owner restore and entitlement reference/clone limits; preserve both original economic authority and existing successful scopes.

The [entitlement and funding source review](evidence/conservation-entitlement9a-scoped-source-root-review-20261004.json) pins fourteen changed Git sources and the original seven control groups. Commitment-transaction authority is distinct from the artifact signer; accepted live/cold claim leaves must match the complete original allocation. Carry edges retain their earlier epoch and pool, and fully paid Finalized roots remain active until actual expiry. Missing artifacts and unchanged capacity holds preserve progress and avoid repeating admitted work. This is source review only: the32-root normal/race scope, vets and controls remain, and the joined producer must separately establish native-income versus opening-capital composition before claiming the10/90 result. Include the actual drained-activation and archive-admission fixture baselines in the composed correction.

The [original composed Go terminal readback](evidence/native-adoptionade5-original-terminal-root-readback-20261004.json) independently verifies 75 file bindings and every selected raw root census: each mode completes fifty roots with seven passes, forty-three failures and zero skips; vet passes. All eighty-six failed roots report the same protected-ancestor refusal. These retained results do not establish product defects or final readiness; the separate protected-environment replay is collecting the previously unreachable assertions.

The [composed Go runner review](evidence/native-adoptionade5-go-body-operational-root-review-20261004.json) retains the exact fifty-root scope in both modes, including both original long-history cases. The [environment ancestry finding](evidence/native-adoptionade5-environment-ancestry-root-finding-20261004.json) establishes that a private scratch leaf below a group-writable ancestor cannot satisfy physical custody. Protect the complete owned ancestry before qualification; do not weaken production checks or change active directory metadata. Preserve the original terminal, successful roots, source and images, then replay only affected roots under a separately protected sibling scratch directory. A successful environment counterpart does not prove the remaining recovery behavior: the corrected replay has exposed additional checkpoint-census, restore-request and mount-admission refusals. Collect all outcomes and distinguish fixture reachability errors from production defects before batching deterministic corrections.

The [complete corrected restore terminal](evidence/native-restore729c-terminal-root-review-20261004.json) reproduces all 25 roots in each normal/race mode: eleven pass, fourteen fail, no skips; vet passes and all bodies join. The [three-file fixture successor](evidence/native-restore425-fixture-source-root-review-20261004.json) addresses both distinct setup causes without production changes. Qualify fourteen affected roots plus two new deterministic preparation roots per mode; keep the eleven successful roots. Fresh ancestors require explicit private protection, and future preparation targets must not acquire an owner generation before public preparation. The complete 129-review path remains isolated with its own long timeout.

The [Rust fixture resource admission](evidence/principal-activationec242-rust-resource-root-review-20261004.json) independently verifies all 29 staged crate files, exactly one changed test source and the original cumulative baseline with a two-times forecast. Run the three new/exporter roots and two fixture controls after the current lane joins; do not rebuild the unchanged capture/replay engines or repeat their 24 successful roots. A control-runner continuation must verify real sealed phase names and receipt layout: `a499-729-normal` cannot be looked up as the older `a499-normal`. Preserve the preflight refusal and use a distinct corrected runner; no product failure follows from that orchestration error.

The [signed native-renewal review](evidence/native-renewal1fe-source-root-review-20261004.json) verifies source `1fe78d5a`: original-key append-only adoption retains combined policy identity, independently signed inner reviews, pending job authority and authenticated archive history. Its nineteen-root normal/race scope and six causal groups remain unqualified. Compose the test-type and real activation corrections first; truthful archive entry capacity and a separate long-history timeout are required. Renewal must preserve completed work without treating a new runtime or build as a fresh owner.

The [allocation operational-cause finding](evidence/yuma-operational-cause-source-finding-20261004.json) identifies an adjacent recovery regression at source level. Original Yuma cancellation or witness-capacity exhaustion is wrapped as `errRpcIntegrity`; its consumer prioritizes that marker and retains a native hold. Cancellation during append recalculation also reaches a generic noncapacity hard-hold branch. Fix the actual replay/sample/save/follow path and test healthy continuation, capacity retention and genuine conflict precedence. Do not infer evidence disagreement from a timeout, cancellation or resource limit. The [exact principal successor stage](evidence/principal-activationec242-stage-root-review-20261004.json) verifies all nine paths against its physical base, including four new Go files; staging is separate from compilation and behavioral qualification.

The [corrected restore source](evidence/native-restore729c-test-types-source-root-review-20261004.json) fixes six test compilation errors without changing production behavior. Its [retained normal compile and runner review](evidence/native-restore729c-positive-operational-root-adoption-20261004.json) are independently verified. The [actual normal body](evidence/native-restore729c-normal-root-review-20261004.json) executes all 25 roots: eleven pass, fourteen fail, no skips. Failures reach storage preparation; the combined fixture has an unprotected fresh ancestor, while the native fixture encounters previous owner custody metadata. Trace and correct these distinct setup paths without weakening production custody. At that normal readback race/vet remained live; the later terminal above supersedes that state. Successful bodies remain retained, and the [adapted controls](evidence/native-restore729c-controls-operational-root-review-20261004.json) are admitted after their exact passing counterparts.

The [principal activation and subprocess successor](evidence/principal-activationec242-source-root-review-20261004.json) is source-reviewed at `ec242e3b`. Original parent proofs now start drained and the actual next-block program accrues before its epoch; the shared capture/replay worker retains bounded child diagnostics alongside the original wrapped process/cancellation/I/O cause. Fresh principal exports, sixteen Go roots per mode, three Rust roots, vet and five control groups remain to qualify. Diagnostic words must never determine retry or integrity classification. Preserve unchanged production engines and all earlier outcomes.

The [fixture normal readback](evidence/principal-fixture4af-normal-root-review-20261004.json) independently reproduces twelve executions: three pass, nine fail, no skips. Seven now reach the original drained-activation guard; two reach a rejected replay whose diagnostic is reduced to exit status. Preserve the actual parent proof with pending emissions rather than inventing a drained RPC state. Correct the original fixture to execute accrual from a genuinely drained parent, and retain bounded real child diagnostics with cancellation and source rejection kept distinct. The race body remains separate and continues; all successful bodies stay retained.

The complete native restore [positive runner](evidence/native-restore-a499-positive-operational-root-adoption-20261004.json) and [six-control continuation](evidence/native-restore-a499-controls-operational-root-adoption-20261004.json) are reviewed for automatic execution after the current lane joins and fresh admission passes. Exact normal/race images, real capture/replay engines, per-phase persisted resource samples and positive-counterpart bindings remain required. Control output paths include both group and mode; otherwise exclusive creation would fail on the second mode. These admissions do not establish behavioral success.

The [targeted fixture runner](evidence/principal-fixture4af-operational-root-adoption-20261004.json) is admitted after its full mechanism/preflight review; [actual launch readback](evidence/principal-fixture4af-launch-root-readback-20261004.json) confirms the runner and normal compiler are live. Keep twelve selected roots per mode, vet and two causal controls per mode, retaining earlier successes and the distinct Rust engines. Its imported resource guard uses the stricter original twelve-GiB reserve, not the smaller proposal threshold. No test outcome is inferred from launch.

The [complete native restore resource review](evidence/native-restore-a499-finite-resource-root-review-20261004.json) independently verifies all 20,773 staged Git blobs and modes, measured 226,103,559 bytes, exact Core `c618` workspace inputs and 24 unique selected roots per mode. Preserve the original cumulative resource baseline with a two-times sixteen-GiB reserve. Split ordinary roots, actual capture/replay roots and the 129-review continuation into separately retained phases; keep six causal controls per mode and vet. Actual runner preflight and fresh physical admission remain required before execution.

The [effective fixture stage review](evidence/principal-fixture4af-effective-stage-root-review-20261004.json) verifies all four inherited/current test paths against exact `4af` Git blobs over the unchanged `dc1` base. Both virtual new files and existing helpers are present; the selected graph discovers them. The corrected twelve-root/two-control recipe is bound. Actual behavioral qualification remains; metadata discovery and source staging are separate from compilation and test results.

The [Core publication review](evidence/core-c618-publication-root-review-20261004.json) verifies the actual remote main and unchanged qualified production/test/module bytes; use `v0.0.0-20261004075743-c6186b79caaa` for dependency publication. Complete native restore still requires its separate behavioral gate. Overlay staging must include every inherited delta relative to its physical base, not only the newest parent commit: the `4af` fixture over `dc1` needs four exact test paths, including both retained `8d3` helpers. Metadata graph discovery alone does not prove those helper symbols compile.

The [principal fixture custody source review](evidence/principal-fixture4af-source-root-review-20261004.json) verifies the test-only `4af` successor and nineteen bindings. It provisions the actual checkpoint lock before declaring the synthetic volume, protects fresh archive metadata, and adds public tests that retain a lost lock or empty committed head without repair or extra source reads. Qualify only the ten previously failing roots plus these two new roots per mode, vet and two causal controls. Correct the inherited recipe wording before runner admission; preserve unchanged production/Rust bytes and prior successes.

The [read-only Core terminal review](evidence/core-c618-terminal-root-review-20261004.json) independently verifies 61 input/output bindings, ten exact passing roots in each normal and race mode, vet, and four named causal failures when the old production behavior is restored. Exact private read-only modes survive copy, lost acknowledgements and publication; genuine permission changes remain conflicts. Core `c618` is qualified for this component scope. Publish its module and join the complete native restore `a499` gate; component success does not establish full producer restore or mainnet readiness.

The fixture continuation is terminal: normal and race each retain ten passes and ten identical missing-checkpoint-lock failures, with no skips; vet passes. The next correction must provision the complete actual fixture custody before declaration and distinguish an absent initial head from an empty committed head. Retain successful bodies and qualified Rust assets instead of repeating unaffected work.

The [complete native restore source](evidence/native-complete-restorea499-source-root-review-20261004.json) is reviewed at `a4990a2f`, with 32 exact bindings, thirteen new public/adjacent roots and a 24-root normal/race scope. It restores the native proof/job namespace and independently signed approval files alongside the exact combined checkpoint, preserving partial writes, certified ancestry, renewal acknowledgements and a completed job whose acknowledgement was lost. The required Core `c618` dependency must be explicitly joined before qualification. The additive causal manifest keeps an original unused-import control-construction error and corrects it without altering product/test source. Actual bodies and six production controls remain to run.

The [fixture continuation normal body](evidence/principal-fixture8d3-normal-root-review-20261004.json) finishes ten passes and ten failures, with no skips. The fresh-root protection/tampering root passes; the remaining principal fixtures now expose missing checkpoint-lock provisioning. Fix the complete fixture ownership/provisioning chain and adjacent constructors together, keeping real runtime admission strict. Preserve the ten successes and original results; this batch does not establish complete principal accounting.

The [original principal Go batch](evidence/principal-dc1-go-original-terminal-root-review-20261004.json) is terminal and independently reproduced: normal and race each run all 38 roots with19 passes,19 identical fixture-custody failures and no skips; mainnet vet passes. Both protected images match their original receipts, and the wrapper/body processes have exited. The [reviewed incremental runner](evidence/principal-fixture8d3-operational-adoption-root-review-20261004.json) has started actual overlay compilation after its helper preflight and selected graph passed. It selects nineteen masked bodies plus the new protection/tamper root per mode, preserving the earlier nineteen successes and qualified Rust assets. Compilation, positive bodies, the fixture omission and six original production causal groups remain distinct outcomes to collect.

The [read-only Core successor](evidence/native-readonly-corec618-source-root-review-20261004.json) is source-reviewed at `c6186b79`. It admits exact private `0400`/`0600` files and restores the original mode after verified copy, before sync and publication. Four new public tests cover immutable bytes/mode/inode, lost acknowledgements and genuine changed-permission refusals; six neighboring roots and two exact old-body controls remain to execute in each mode. Publish and join the qualified Core version before qualifying the complete native restore adapter.

The [read-only native evidence restore finding](evidence/native-readonly-restore-mode-source-finding-20261004.json) confirms a cross-repository contract mismatch: finality publishes private read-only files (`0400`), while Core physical inventory accepts only `0600` and restore staging does not reseal the declared mode. Preserve exact original private modes through inventory, copy, interruption reconciliation and publication; refuse group/world permissions. Qualify a narrow Core successor and then the actual combined native job/checkpoint restore. Changing evidence to writable files would leave the underlying mismatch unresolved.

The [Yuma lifecycle review](evidence/yuma-owner-cancellation-source-finding-20261004.json) finds that the unfrozen allocation validator recalculates with a background context during active/history admission. Carry the real owner cancellation through derivation, reopen and summary, and stop expensive work when its finite arithmetic budget is exhausted. If validated work is reused, bind it to exact immutable evidence and authority. Add deterministic public canceled-owner and budget controls before this candidate is frozen; no behavioral success is claimed for the reviewed work in progress.

The [incremental fixture stage](evidence/principal-fixture8d3-incremental-stage-root-review-20261004.json) verifies the two read-only Go overlay files against the reviewed Git blobs, leaving the original complete source untouched. The finite twenty-root normal/race scope reuses qualified Rust assets and preserves unaffected bodies. Actual runner preflight, original-process completion, fresh physical resource admission and behavioral execution remain required; staged source alone is not qualification.

The [original dc1 Go normal body and fixture correction review](evidence/principal-fixture8d3-source-root-review-20261004.json) retain 38 exact executions: 19 pass, 19 fail and no skips. Every failed root reaches the same physical-custody refusal because the freshly created test directory inherited group-write permission. The reviewed `8d33892c` successor changes only two test files: provision the fresh selected directory explicitly, then prove the actual public consumer refuses later permission tampering without repair or another Claim read. Qualify the nineteen previously masked roots plus the new deterministic root in each mode and one fixture omission per mode; reuse exact qualified Rust assets after custody checks. Preserve original results rather than weakening runtime custody or repeating unaffected Rust work.

The [corrected Go operational admission](evidence/principal-dc1-go-fd4-operational-adoption-v2-20261004.json) retains both precompile wiring failures and pins the explicit ELF custody helper and its sibling import path. The complete runner must exercise its real imports, helper APIs and asset custody before an expensive phase; syntax checks and test declarations alone do not prove that launch path. No production deployment authority follows from offline qualification.

The [dependent Go capture/replay admission](evidence/principal-dc1-go-fd4-operational-root-adoption-20261004.json) binds the qualified original principal Rust assets to 38 public Go roots per mode and one vet. Current physical readback verifies 2,698 Go/module files against the frozen source. Use the original resource baseline, cumulative 56 GiB cap and a two-times forecast; run one compiler/body at a time, retaining exact images and sampled resources. Six Go causal groups remain separate. Admission does not imply any Go test success, live runtime authority or deployment.

The [two principal Rust causal controls](evidence/principal-dc1-rust-controls-root-review-20261004.json) are independently qualified: each changed private crate differs from the 29-file positive source census at exactly its declared production file, compiles freshly, and produces one named assertion failure with no ignored root. This completes the Rust component scope alongside the retained 24 positives and ten exported jobs. Actual Go fd4 capture/replay integration, principal effects and the complete economic witness remain separate.

The [original principal Rust positives](evidence/principal-dc1-rust-positives-root-review-20261004.json) pass all 24 exact selected roots without failures or skips, with ten actual exported jobs independently rehashed. The [fresh dual-engine build](evidence/principal-dc1-dual-engine-build-root-review-20261004.json) produces distinct protected capture/replay executables; the lib-test image is separately retained. The [operational admission](evidence/principal-dc1-rust-operational-root-adoption-20261004.json) preserves the original cumulative resource baseline and samples actual process-forest memory. Two Rust causal controls and 38 dependent public Go roots per mode remain separate; actual fd4 capture/replay execution and whole economic readiness are not inferred from these Rust positives.

The [original fee race body](evidence/fee-c04-original-race-terminal-root-review-20261004.json) completed all 49 selected roots: 48 passes, one fixture-status failure and no skipped, unfinished or timed-out bodies. The greater-than128-review history root passed in 2,845 seconds. The failed root correctly refused an archived receipt contradiction before another source read; its expected command status is corrected separately in `6820` and the reviewed successors. Retain the successful bodies and the original failed result. Qualification of changed economic behavior, causal controls and complete semantic restore remains separate.

The [corrected native receipt/finality source](evidence/server-receipt4a5-typed-causes-source-root-review-20261004.json) is reviewed at `4a5bbdae`. It uses the shared bounded cause inspector before invoking unwrap/network methods, retains unknown observations without inventing a verifier conflict, and preserves actual hard-refusal precedence. Eight new deterministic roots include real transport/body/close failure, charged-journal retention and healthy same-authority continuation. Qualify 24 roots per mode, two vets and four causal groups; preserve the completed model suite.

The [whole-read short partition](evidence/runtime-observation6fa-short-partition-root-review-20261004.json) independently reproduces 80 new passing root executions and retains the previously verified ten validator normal passes: 45 roots in each mode across five non-mainnet packages. All six vets pass. Raw events and nine protected images match; five mainnet roots per mode and twelve causal control groups remain separate. Preserve these successful bodies.

The [original principal-effects source](evidence/economic-principal-effects986-source-root-review-20261004.json) is reviewed at `986078c3`, with 58 exact bindings across 26 changed paths. Original before/after runtime queries and independent committed mutation census separate stock from income, retain unclassified net-zero writes, discard rolled-back effects and recompute archived totals from original checkpoints. Qualify the source independently; an earning label or matching balance cannot prove full 10/90 behavior. Actual native-to-vault capture join, full Yuma/quantization and entitlement/funding remain required.

The [native receipt retry source review](evidence/server1709-receipt-retry-typed-nil-source-finding-20261004.json) found an adjacent typed-nil dispatch gap outside the completed bounded-cause scope. Reject nil concrete receivers before custom unwrap/network methods, test actual no-retry/no-publication and healthy recovery, and qualify a distinct successor. Keep the original unexecuted scope and all successful component results; no live run failure is inferred.

The [bounded-cause causal controls](evidence/server1709-bounded-causes-controls-root-review-20261004.json) independently reproduce all six production omissions in normal and race modes: seven named assertion failures per mode, with successful positive counterparts and no setup, timeout or skipped-body substitution. This completes the selected retry/error-inspection component gate. Review exact changed-path coverage before Server publication; final release composition remains separate.

The [combined Claim-window source](evidence/conservation-claim-window7dc-source-root-review-20261004.json) is reviewed at `7dc19440`. Original-key offline proposals feed the existing durable archive plan/apply path. Unknown obligations, matched original receipts, cumulative reviews and bounded predecessor state remain retained through adoption and recovery. Qualify seventeen roots per mode and seven causal control groups; complete-owner cross-volume restore and full entitlement/root/funding census remain required. Forecast long-history duration separately with a two-times margin; preserve successful bodies rather than rerunning them after a package deadline.

The [Server bounded-cause positive scope](evidence/server1709-bounded-causes-positive-root-review-20261004.json) independently reproduces all 31 selected normal roots, 31 race roots and six vets on exact Server1709. Raw event censuses, protected images, selectors and source/dependency bindings match. The six causal omission groups remain separate; these successes do not close the whole release.

The [original opening-principal source](evidence/economic-principal-dc1-source-root-review-20261004.json) is reviewed at `dc1aeb15`. Original signed authority fixes the API/layout, exact parent and query census. Capture/replay use the same original proof, and the consumer independently verifies canonical results and retains the first stock identity through archive/reopen. Absent balances remain distinct from zero; incomplete observations preserve healthy sibling progress. Qualify 38 Go roots per mode, 24 Rust roots and the eight causal control groups. This stock-only increment does not prove deposits/withdrawals, the complete miner denominator/quantization, entitlement/funding, or activation readiness.

The [validator normal retry scope](evidence/runtime-observation6fa-validator-normal-root-review-20261004.json) passes all ten selected roots, including actual WebSocket reconnect through the production whole-read owner. It keeps original authority, retry budget, cancellation and genuine identity-conflict checks. Qualify remaining packages and race/control scopes separately; retain these exact successful bodies.

The [complete Server model body](evidence/server1709-complete-model-body-root-review-20261004.json) finished all 1,540 top-level roots: 1,529 passes, eleven skips and no failures. Test execution and owned cleanup exited zero. One PostgreSQL lifecycle observation error occurred after body exit; retain the original incomplete resource result separately and reuse the successful bodies. Configured-input, actual auto_explain and current source-host contract follow-ups remain; final changed non-model package and release composition are not inferred.

The [whole-read successor six-package compilation](evidence/runtime-observation6fa-six-compiles-root-review-20261004.json) passes on exact `6fa` source. Its fifty-root normal/race scope, six vets and twelve causal control groups remain. Both original reconnect failures are retained; compilation alone does not prove retry recovery.

These dated records preserve source identities, failures and scoped successes. Their older pending descriptions are historical; use the current table and readiness checkpoint for the next action.

The [manager recovery composition](evidence/miner-manager-composition549-source-root-review-20261004.json) is source-reviewed at `549f6926`: four real managers and96 synthetic instance lifetimes exercise durable-prefix reuse, fresh-owner reconciliation, manager-local replacement, write-before-HTTP dispatch and lost-reply recovery. Qualify its fifteen-root normal/race scope and five causal omissions with the required test build tag. This establishes a concrete test path, not SDK registration, resource sizing or full repair closure.

The [corrected signed fee-revision union](evidence/fee-revision3e7-source-root-review-20261004.json) is source-reviewed at `3e7feec7`. It joins the exact reviewed nil/index admission and command-status correction before revision/evidence mutation. Qualify its fifty-four-root normal/race scope and nine controls; keep the unexecuted bb716 precursor and original failures separate. Combined Claim-window adoption and complete-owner cross-volume restore are confirmed missing mechanisms and remain assigned implementation work.

The [archive-index successor](evidence/fee-retention6580-source-root-review-20261004.json) is source-reviewed at `6580a2b4`. It preserves nil-archive receipt-only indexing and refuses nil operands, empty or malformed segment catalogs, invalid retirement references and closed custody before retained-index mutation. Three deterministic roots cover those adjacent cases, with known-receipt idempotence and the exact command-status fixture correction retained. Qualify its forty-five-root normal/race union, seven causal control groups and vet under a cumulative budget informed by the measured large-history test; keep all earlier failed and successful results.

The [original fee race result](evidence/fee663-original-race-timeout-root-review-20261004.json) retains seventeen passes, fifteen failures and fourteen unrun roots. Its cumulative one-hour package deadline expired after a passing greater-than128-review test consumed about48 minutes; the named final root had run only two seconds. Retain that successful history test, collect unfinished/affected scopes separately, and investigate actual history scaling and repeated fixture-image hashing/sealing costs without reducing the scenario. The [scoped warm-Go admission](evidence/scoped-warm-go-resource-admission-root-review-20261004.json) permits one compile and one body beside the active model using a conservative two-times forecast, preserved capacity baseline and independently checked lease; it does not modify live runner guards or erase earlier admission refusals.

The [retirement normal batch](evidence/fee-retention121-partial-terminal-root-review-20261004.json) retains thirty-three passes, two failures and six unrun roots after a nil-archive panic. The new admission branch assumed an archive and last segment even for receipt-only indexing. Define safe legacy/no-retirement behavior, reject malformed retirement before retained-index mutations, and cover nil and empty segment cases deterministically. Batch this correction with the already reviewed command-status fixture fix; preserve successful bodies and qualify the unfinished/affected scope independently.

The [signed fee-revision test graph](evidence/original-key-fee-revision-test-graph-root-review-20261004.json) resolves all849 selected packages without incomplete/error entries and binds all six Core consumers to published `ad6fc3a1`. Its metadata-only admission keeps the original resource baseline and prior preflight refusal. Compilation and fifty-one-root normal/race qualification remain; a graph pass cannot substitute for economic behavior.

The [whole-read retry successor](evidence/runtime-observation-owner6fa-source-root-review-20261004.json) is source-reviewed at `6fa7c4ad`: it preserves the original query and shared retry deadline, and rejects typed nil causes before retry authority or custom unwrap dispatch. Qualify its fifty-root normal/race union, six package compiles/vets and twelve operative control groups. The original reconnect failures remain retained; source review does not establish all-role recovery.

The [corrected fee normal batch](evidence/fee-c04-normal-terminal-root-review-20261004.json) completed forty-eight passes and one fixture failure, with no skips. Both public commands reached the intended archived-receipt contradiction: observer reopen returns status3 and archive-plan admission returns status2. The distinct `6820f42a` test-only successor makes those expectations explicit while preserving no-read, no-output and custody assertions. Qualify its five-root scope and causal controls; retain the forty-eight successful bodies and the original failed result. Mainnet economic readiness still requires opening principal, execution effects, the complete miner allocation witness and entitlement/funding census.

The [original-key fee revision](evidence/original-key-fee-revision-source-root-review-20261004.json) is source-reviewed atbb716 with51 roots per mode and seven operative controls queued. It preserves signed predecessor/review lineage and historical policies for backlog, and releases only the corresponding typed policy hold after signed adoption. Ordinary proof/signature quarantines remain held. Opening principal, execution effects, the full miner denominator/quantization and entitlement/root/funding witnesses remain required implementation; selected fee sums cannot establish whole10/90 conformance.

The [observation-cause review](evidence/runtime-observation-typed-nil-source-finding-20261004.json) found typed nil causes could acquire generation-retry authority or reach a custom unwrap. Reject them before dispatch and add deterministic controls while preserving bounded traversal and hard/cancellation precedence. Do not qualify the uncorrected source as all-role recovery. A quiet or buffered raw log also does not establish that a body has not started: use the live protected test process and owning lineage; never restart for observation uncertainty.

The [retirement logical-guard correction](evidence/fee-retirement121-source-root-review-20261004.json) is source-reviewed at121. Both tests now use owned publication and require the actual logical cause. Qualify its41-root normal/race scope, five operative controls and vet; preserve the unexecuted precursor sources. The [configured-input follow-up](evidence/current-model-skipped-input-followup-root-review-20261004.json) stages exact public config for eight currently skipped model roots on a separate fixture after active-suite cleanup. Keep the current complete run unchanged. The remaining source-host comparison expects a removed prober table; review the current consumer contract rather than fabricate legacy input.

The [transport race batch](evidence/runtime-transport-original-race-root-review-20261004.json) reproduces the same sole reconnect failure:32 passes, one failure, no skips. Both original modes remain failed and retained. Qualify the distinct whole-read recovery successor; do not repeat f3 or infer recovery from compilation.

The [fee-retirement review](evidence/fee-retirement-logical-guard-source-finding-20261004.json) found two adjacent tests whose direct checkpoint overwrite could satisfy a generic refusal before reaching the intended logical guard. Correct the unknown-obligation and archive-summary fixtures through the actual physical-head publisher, require the named logical cause and qualify omitted-guard controls. Keep physical-tamper coverage separate. Retain the unexecuted eca source/graph history; qualify its corrected successor instead of collecting misleading positives. Production retirement preserves exact archived proofs, hot unresolved identities and transaction deduplication, but its41-root normal/race scope remains unqualified.

The [fee-fixture successor](evidence/fee-fixture-c04-source-root-review-20261004.json) is source-reviewed atc04 with49 roots per mode queued. It preserves all production bodies and corrects the signer, physical publication and stable Claim birth fixtures. Three new deterministic signature tests require original-key admission before malformed-wire and canonical foreign-key refusal. Preserve ineffective original controls as unqualified and prove the corrected omission reaches its intended guard.

The [transport normal batch](evidence/runtime-transport-original-normal-root-review-20261004.json) completes32 passes and one reconnect failure with no skipped roots. An implicit WebSocket reconnect changes transport generation during authentication; the attempted proof correctly expires. Implement whole-read reobservation through the actual public owner within the original budget and pinned block. Keep generation binding and genuine returned mismatch refusals. Collect the original race result independently and qualify the correction as a distinct successor.

The [transport-generation successor](evidence/runtime-transport-f3-compile-root-review-20261004.json) compiles all three selected packages. Its normal33-root scope has completed with32 passes and one reconnect failure; collect race and qualify the whole-read recovery successor independently. Reconnect expiry must trigger bounded reobservation of the original pinned block and preserve healthy sibling progress; returned semantic contradictions remain hard. Compilation alone does not close the all-role runtime requirement.

The [fee-consumer normal batch](evidence/fee-consumer-original-failures-root-review-20261004.json) completed all46 roots:28 pass,18 fail,none skipped. Preserve the failed source and complete diagnostics. Astra is correcting three fixture seams together: native-fee signatures must roundtrip the existing bare canonical wire format; logical receipt-contradiction fixtures must use the owned checkpoint publisher instead of bypassing physical custody; and a fixed Claim instance must retain its original start time across refreshes. Add causal public-path tests and qualify a distinct successor. A signature-check omission that still passes is an unqualified control, not proof of protection. The independent Server fee-context scope passes nine normal and ten race roots, with both selected vets passing. These scoped results do not qualify the failed SN consumer or the whole economic witness. The healthy complete model suite and original SN race batch continue independently.

The [container-lifecycle observer correction](evidence/container-lifecycle-scoped-root-review-20261004.json) passes nine deterministic controls. It records actual same-ID teardown states, retries within a finite observation budget and accepts absence only after workload exit and confirmed removal. Unknown/live/foreign observations remain unavailable. This is a scoped helper result; the active model sampler remains unchanged and original telemetry failures remain.

The [corrected complete Server model suite](evidence/server1709-complete-model-live-root-review-20261004.json) has started behavioral tests on Server1709/SN663/Coread6. Root independently verified the live test binary and exact launch bindings. Collect the full suite without stopping at ordinary assertions; report any telemetry gap separately. The fee-consumer and Server exporter test binaries compile, and their scoped normal/race/controls can qualify concurrently within measured resource admission. Full release qualification remains open.

The [corrected Server1709 compile](evidence/server1709-actual-compile-root-review-20261004.json) passes all six affected packages under the selected SN663/Coread6 graph. This fixes the a2 missing-import build failure; no behavioral root was selected. The corrected complete model fixture is authorized to start automatically. An observer teardown gap remains separately reportable and must not hold or erase an admitted test body.

The resilience review now includes runtime-continuity and checkpoint observations: unavailable RPC reads must not be labeled as changed runtime, noncanonical history or regressed finality. Qualify bounded reconnect/reobservation of the same pinned block alongside genuine contradiction controls before current-role admission.

The [current combined Server model attempt](evidence/current-combined-model-build-failure-root-review-20261004.json) is terminal before any test root: `task/metrics.go` calls `errors.New` without importing `errors`. Fixture cleanup succeeds; the original failed compile and separate teardown sampling gap remain. Astra is fixing the import and reviewing adjacent changed-package build seams. Sol must compile the actual complete selected model package before starting its corrected full fixture. A passing dependency enumeration does not establish successful compilation. Preserve the [earlier live observation](evidence/current-combined-model-live-root-readback-20261004.json) as history, not current status.

The [corrected405 payout body scope](evidence/model405-body-teardown-gap-root-review-20261004.json) passes eleven normal and eleven race roots, vet and the four intended fixture-control failures. Its original terminal remains `FAIL_OR_INCOMPLETE`: a Redis process disappeared between container inspection and cgroup read during owned teardown. All494 host-resource samples remain available, and cleanup completes. Retain the single telemetry gap; do not rerun successful tests solely to make the measurement flag green. The next run needs a documented scoped disposition, verified owned cleanup and fresh cumulative resource admission.

The new fee-consumer review exposed the same architectural risk as sim-testnet startup: an optional verifier was placed before the conservation follow loop and every failure returned a global error. Correct that path before qualification. Keep the prior checkpoint on failed admission, isolate fee issues from native/vault/Claim progress, and merge only completed verifier results into the current owner state. A slow verifier must not block sibling observations or replace their newer cursors with its older snapshot. Add deterministic outage, recovery and cancellation controls.

The [corrected independent fee worker](evidence/independent-fee-worker-source-root-review-20261004.json) is frozen at `663a0ea2` and source-reviewed. It uses a separate bounded verifier and merges results into the latest checkpoint; six new causal roots cover held reads, missing-request recovery, cancellation, request quarantine, the actual finite deadline and handoff conflicts. Its46-root normal batch has completed with28 passes and18 retained failures; collect the original race batch and qualify the distinct corrected successor with operative controls. The selected Server fee-context tests and vets pass within their reported scope. Complete fee retirement, signed authority revisions and whole economic witnesses remain open.

Continuous fee operation also requires two real mechanisms beyond the first source-only consumer: retire complete proof payloads into authenticated archives while retaining transaction deduplication and unresolved fees, and admit signed semantic/engine/checkpoint revisions under the original trust root. Keeping every proof hot until the checkpoint byte limit or pinning one runtime profile forever would reproduce the capacity and version-change failures from testnet. A selected transaction census remains separate from whole-provider fee coverage.

The [native profile continuation](evidence/native-profile-final-root-review-20261004.json) is terminal with all26 phases passing and no unfinished phase. Both actual engines compile; corrected exporter and unsupported-profile cases pass, the public seventeen-root normal/race scopes pass, and all remaining recovery/prefix controls qualify. Completed Server, capacity and prefix positives retain their exact inherited sources. The original failed batches remain. Corrected405 payout body qualification has finished successfully within its scoped telemetry exception; final composed source, complete economic witnesses and unattended operator admission remain.

The remaining production work has explicit owners. Astra Integration implements admitted fee consumption, original opening-principal effects, the complete native miner denominator/quantization witness and entitlement/funding census. Astra Current implements the shared bounded error inspector, pooled route replacement, multi-component repair checkpoints, Claim-window adoption and cross-volume restore, then unattended native capture/replay/monitor and approved runtime-profile renewal. These are code requirements, not tasks assigned to an absent operator agent. Root reviews and integrates each qualified increment; Sol runs independent tests.

Continue the one active complete model suite on corrected Server `1709ffe0`, SN `663a0ea2` and published Core `ad6fc3a1`. The earlier a2 attempt failed compilation before any behavioral root. Do not restart the healthy successor to adopt fee-fixture corrections or a new telemetry helper. Collect all ordinary assertions, retain exact source identity and report observation gaps separately. Qualify the corrected SN fee union independently; the model suite imports only its selected protocol/Merkle/payout packages and cannot establish final SN mainnet behavior.

The [qualified Core read-cause fix](evidence/core-ac65-main-publication-20261004.json) is now merged and pushed on Connect main at `ad6fc3a1`. The complete durablevolume subtree matches the tested ac65 source; upstream ledger/transfer changes are preserved. The final SN/Server candidate must adopt the published module explicitly and qualify that composition. Existing Core631/2ea results retain their original source identity.

The four critical workstreams are proceeding in parallel: economic execution witnesses; durable recovery, receipt archives and resources; complete source composition; and independent qualification. Astra Max owns implementation and root-cause fixes, with Sol Medium running tests. The [latest independently checked results](evidence/parallel-critical-path-readback-20261004.json) establish Core ac65 at sixteen passing normal and sixteen passing race roots, vet and four operative regression controls. Server c2b finishes both modes at forty-three passes and two companion-fixture failures, with worker tests, vets and six controls complete and owned fixtures removed. The exact zero-byte-anchor correction405 and receipt-lineage correctionc16 are source-reviewed; their behavioral qualification remains pending. Continue only the affected scopes, retain successful results, and join each qualified increment without restarting preparation.

The native lifetime successor now compiles both actual engines and passes eight original Rust roots plus the borrowed-backend regression root. Two exporters exposed a separate test-fixture defect: synthetic bulk-memory instructions are unsupported by the pinned node execution profile. Correction40e keeps the production VM unchanged, and the completed continuation above passes the affected exporters and unfinished checks using retained engines. Original failures remain.

The [original native qualification batch is terminal](evidence/native-finite66-terminal-independent-review-20261004.json): 20 phases passed, four failed and 42 were not run because their dependencies failed. Both 32-root Server modes, both vets, four Server controls and all six capacity phases passed. The original failed result is retained beside the now-passing native/profile continuation. Its initial preflight exposed an expired resource-lock holder before any test ran. Keep cumulative capacity accounting tied to an immutable filesystem baseline and authenticate the current lease holder separately; process renewal must not reset consumed capacity or discard completed work. The corrected runner uses that separation. The [lease-continuity evidence](evidence/native-qualification-lease-continuity-20261004.json) retains the original launch conditions; resource consumption is not reset for continuation.

The [reviewed source integration](evidence/qualified-base-main-integration-20261004.json) is now on SN main at `4a2c2681`. Its code, tests and module files exactly match the qualified `2477` composition; only current documentation and evidence differ. Original failed results remain retained. The [Connect merge](evidence/connect-upstream-main-integration-20261004.json) is also pushed at `c5b10cc7`, preserving upstream changes and exact qualified restore bytes. SN keeps the qualified Core `2ea` dependency; no new combined-source test result is inferred. Native producer, renewal, conservation and Server model correction qualification are still pending.

**October 4 composed Go compilation:** The [normal compiler and contiguous exporter review](evidence/native-adoptionade5-go-normal-compile-root-review-20261004.json) verifies the protected normal test image, joined compiler tree and one additional passing original producer exporter with five exact outputs. No Rust rebuild or repeated successful body was needed. Race compilation and the fifty selected roots per normal/race mode, two distinct long-history partitions, vet and applicable causal controls remain; preserve the original source/module and imported-package census alongside the images. Full entitlement/root agreement authenticates obligations but does not classify captured opening stock or deposits as new native income; capital provenance and the policy result stay unknown until separately established.

**October 4 capture review and completed Rust controls:** The [terminal Rust control review](evidence/native-adoptionade5-rust-controls-terminal-root-review-20261004.json) verifies two distinct omission libtests compile and each reaches its exact intended failure, with zero skips and joined children. The earlier runner import refusal remains retained beside the corrected same-command preflight; it is not a product-test failure. Preserve the four passing positive roots and twelve exports. The Go fifty-root union additionally needs five contiguous original producer jobs; run that exporter once from the same already-built, owned libtest rather than rebuild or repeat successful work.

The [selected native-to-vault source review](evidence/native-vault-captured039-partial-source-root-review-20261004.json) distinguishes opening stock, deposits, withdrawals, refunds and liquid earnings, and checks exact native mutation/transaction/receipt identity. Authored public tests cover rollback, foreign identities, delayed receipts and unexplained net-zero movements. Actual execution and broader capture composition remain pending; add repeated-capture ordinal and archive-continuation coverage before final qualification. This partial review does not close production accounting or capacity requirements.

**October 4 activation and regression-control follow-up:** The [Yuma activation correction review](evidence/native-yuma-activation79c-source-root-review-20261004.json) verifies the two test-only changes and fifteen exact bindings: legacy, Yuma3 and liquid branches now check all three original parent-trie pending values before actual accrual and replay. Production bytes remain unchanged relative to the capture-join candidate; this is source review, not a passing run. Preserve all pending capture, typed-cause and capacity scopes when composing qualification. The [Rust control mechanism review](evidence/native-adoptionade5-rust-controls-operational-root-review-20261004.json) admits two independent one-expression omission crates under the retained resource budget. Fresh compile, exact named intended failure and owned process/image closure are required before either control qualifies.

**October 4 composed recovery qualification:** The [exact composed source review](evidence/native-adoptionade5-composed-source-root-review-20261004.json) retains native signed renewal, restore, drained activation and bounded diagnostics together. The [fresh Rust terminal review](evidence/native-adoptionade5-rust-terminal-root-review-20261004.json) independently verifies every staged Git blob (20,780 entries), all four actual selected roots and twelve fixture outputs; fresh capture/replay engines were built for this production closure. Rust causal controls and the fifty-root Go union per normal/race mode, including both distinct long-history roots, remain required. The qualified classifier itself rejects nonzero exits, ignored roots and mismatched censuses before returning; tracing that helper disproved a suspected missing outcome guard, avoiding an unnecessary patch. These four results are also independently confirmed from raw output.

The [Yuma parent finding](evidence/yuma-activation-parent-source-finding-20261004.json) identifies a separate test seam: Yuma-only exporters left the original parent proof funded while their RPC fixture reported zero. Correct the actual parent trie and retain observed original accrual for every Yuma branch; do not weaken production validation or infer qualification from source alone. Full physical UID capacity and entitlement/root/funding conservation remain in progress.

The [38-requirement checkpoint](evidence/current-readiness-checkpoint-20261003.md) remains authoritative for scope. Historical receipts below keep their exact source and test conditions. Component qualification does not establish production activation.

| Workstream | Verified state | Required next result |
| --- | --- | --- |
| October 6 earnings and Server recovery | Server5f financial scope passes22 roots per normal/race mode, vet and five controls per mode. Its full model run retains1,507 passes/17 failures/11 skips; test-only correctionc2b completes both modes at43 passes/two companion-fixture failures; exact anchor correction405 is source-reviewed and queued. Config preserves the inclusive UTC cutoff and pre-cutoff USDC obligations. | Qualify affected model corrections and skipped-input follow-ups, then deployed schema/config/worker adoption and duplicate-free settlement. Historical full-suite failures remain retained. |
| Miner, provider and claim recovery | Retained request recovery, GET retry, rolling expectations and claim consumer scopes are qualified by component. | Current all-role continuation, actual operator identities/custody and Claim archive adapter preserving unresolved obligations. |
| Native and EVM accounting | Native/EVM integration and component runtime/catalog/archive scopes are qualified. Continuous producer SNb32/Server8eb is frozen and source-reviewed, with separate actual capture/replay engines; both corrected actual engines compile; original Rust roots and populated capacity pass. The40e continuation passes all26 phases, including corrected exporters, node-profile refusals and public/control scopes under its original Core631 graph. | Qualify actual native producer and populated provider capacity; complete signed authority/runtime/resource renewal preserving accounting history, then prove native/vault/Claim conservation and the 10/90 policy. |
| Original-runtime execution witness | Bounded host and current collector/request integration qualified and merged. Collector retains twelve Rust roots/four fault controls/three Go-Rust cases; request retains ten normal/race roots/six controls. | Offline archive adapter and fee-context verifier qualification; actual parent witness, runtime/source/profile and independently admitted finality. |
| Restore and capacity | Same-root and cross-root exact512/two-head restores pass normally and with race detection. Cross-root core passes sixteen roots per mode; custody successor passes eight core/five public roots per mode, vets and fourteen intended old-body failures. The original defect remains recorded. | Integrate qualified copied-source custody successor and qualify nested-path/EVM/Claim restore composition; signed capacity adoption and actual host backup/restore rehearsal. |
| Validator efficiency and isolation | Correction mainnet37 and durablehead6 roots pass in normal/race modes; three vets pass. Original validator scope retains the same three failures in both modes. Foreground successor2477 is qualified:20 roots per normal/race mode, vet and exact production-gate/fixture-capacity controls per mode. | Qualified successor and retained monitor fixes are integrated on main; preserve their original test scopes. Full historical lineage/invalidation, production sizing and composed role continuation remain. |
| Final release and launch inputs | Qualified native/EVM and read-cause increments published; immutable Cargo and Go test-image guards integrated. Collector composition, Claim and restore candidates retain separate qualification scopes. | Compose qualified source/module/config inputs; verify repeatable build, approved runtime/genesis/RPC, two-operator four-role hosts, signer custody and rollout. |
| Production outcome | October6 config is published; no deployment, owner signing or observed mainnet settlement claimed. | Owner-approved activation, deployed contract/policy admission, monitoring/on-call exercise and observed conservation/settlement interval. |

The [full-lineage probe source review](evidence/release-lineage-heade65-source-review-20261003.json) verifies SN `e65bd674`, a single test-file change over the integrated collector source. Seven new roots and four neighbors now exercise original signed nonempty HeadEMA, real source-read counts, owned-copy isolation, cancellation, late refusal and a genuinely signed double-fold artifact. Twenty-three bindings and five original-control inputs match. This is uncompiled test source: qualify its eleven-root union once rather than running the superseded empty-head slice. Production-sized history, upload contention, durable cross-process reuse and Claim CPU/capacity continuation remain separate work.

The [copied-source custody successor qualification](evidence/native-cohort63-scoped-qualification-20261003.json) now verifies SN `63a74b7b` with Connect `77235bb4`. Eight core and five public roots pass in each normal/race mode, including the exact 512-segment/two-head restore; both vets pass. Restoring five original core bodies makes five core and two public roots fail at the intended assertions in each mode, fourteen regression-control failures total, with all control images compiling successfully. Root matched 155 artifact bindings and independently counted every result; the original broad graph setup failure remains retained beside its successful selected-import retry.

The fix authenticates the copied original once under retained custody before target effects, checks source identity through application and retains completed healthy-peer progress. It avoids rehashing full payload history at every target write. This qualifies the exact component sources, not a current combined release: preserve the newer collector, accounting, guards and module inputs when integrating the full restore ancestry. Namespace, EVM/Claim continuation and production host restore still require their own results.

The [finite mixed qualification batch](evidence/mixed-qualification-terminal-20261003.json) has finished all 35 distinct charged phases. Root rehashed 283 artifact bindings, checked the immutable final ledger, and verified that its host lease was released. Sampled cumulative filesystem consumption was 3,692,138,496 bytes against the 24 GiB cap. Preserve the original dependency setup refusal and the four intended regression-control phase failures; completed work is not restarted or discarded. This sampled consumption informs the next workload forecast and does not establish production capacity.

EVM restore SN `42f76551` now passes the selected 848-package dependency graph and compilation. No EVM behavior/race/vet result follows from compilation. The next finite batch must use the completed current-source restore/Claim composition and frozen repair/lineage inputs, with separate forecasts for any database-backed full Server model suite. Historical full-model receipts remain distinct from current Server `5f2edd47`.

## Qualification continuity and node-profile fidelity — October 4

The [qualified Rust result classifier](evidence/rust-output-classifier-integration-20261004.json) is integrated on main. Fourteen unit tests and two original-predicate controls passed. Twelve retained prefix outputs were wrongly rejected because diagnostic JSON interrupted the printed test status; exact root identity, exit code and final one-root census reproduce their success without running them again. All fifteen prefix positives have now passed; four prefix controls remain unfinished. Original failure receipts remain unchanged.

Reading a protected executable may change its access time. Authenticate its inode, content, permissions, ownership, size and modification/change times; do not reject unchanged bytes because access time advanced. The corrected custody checker passed six tests and an original-body control, allowing retained Go images to be reused.

Synthetic runtime fixtures must obey the actual node feature profile. Unsupported instructions should fail explicitly, with parent state and pending work preserved. Qualification should fix an invalid fixture rather than enable unsupported production VM features. Compilation, execution, economic conservation and live runtime authority remain separate results.

The [Server model correction batch is running](evidence/parallel-qualification-launch-20261004.json). Its qualification has been admitted for 45 model roots and one worker root in both normal and race modes, two vets and three regression controls per mode. Use the original cumulative resource baseline and collect all ordinary failures. Run one separately forecast full successor model suite afterward. Astra continues conservation, renewal, retry and source integration work in parallel; none of this establishes deployed mainnet behavior. The corrected 26-phase native fixture continuation is adopted and queued after model cleanup, with engines and prior positive results retained. Current admission uses the original cumulative 48 GiB grant and workload forecasts with 2x margins; historical 110 GiB free-space requirements below describe earlier batches, not an additional current launch floor.

## Bounded error causes and mixed companion accounting — October 4

Error classification itself must terminate. Recursive wrapper walks can loop on a cycle or exhaust the stack on a deep chain; unbounded joined-error traversal can also defeat an otherwise bounded RPC retry. Bound depth and total visited causes, preserve hard-error precedence, and treat exhausted or empty classification as unknown. Receipt absence, funding-only backoff, intentional host exclusions, payment retries and schema authority have different policies: share traversal safeguards without merging their meanings. A nonempty joined error containing only nil children must not establish a completed receipt-absence census. Require deterministic cyclic, deep, wide, nil-child and mixed hard/transient tests at the actual consumers.

The active Server correction batch exposed two companion payout fixture failures. Both retain one pending consumed debit and two escrow rows, including a deliberately unpersisted zero-byte forward anchor. A conservation oracle must distinguish that anchor from the settled paying contract, then prove the anchor remains unchanged, the exact consumed debt is retained, available payer credit cannot increase and provider allocation/account balances remain stable through drain and replay. Do not remove the invalid-row assertion without proving which rows can legitimately remain unresolved. Preserve the current failed outputs, collect the remaining batch, and qualify any corrected fixture in a separate successor. These are current findings, not completed fixes. The [independently counted normal scope](evidence/model-c2b-normal-independent-census-20261004.json) completed all 45 model roots with 43 passes and two failures; the worker root passed. The [terminal readback](evidence/parallel-critical-path-readback-20261004.json) now verifies race at43 passes/two failures, both vets and six intended regression controls, with cleanup complete. Correction405 is queued; the original failed result remains.

## Combined release scope — October 4

The [frozen source input readback](evidence/composed-source-input-readback-20261004.json) verifies the exact combined SN and Server source, module files and physically retained tests. This is provenance evidence, not a combined release test result. Qualification must include the nonempty lineage scope, complete Claim/EVM/native restore union and its namespace boundaries, plus the new durable-command conflict regression. Four historical Claim controls are now narrowly rebased and source-reviewed; all fourteen original Claim/EVM control bodies match the combined candidate, preserving later qualified error handling. Their execution remains pending.

Keep broader requirements explicit: existing capability tests do not establish pooled endpoint replacement across every role; individual repair tests do not establish completed-prefix retention across simultaneous component repairs; small transport fixtures do not establish production fleet evidence sizing. Implement and test those actual paths. The standalone Core read-cause correction remains pending and cannot be inferred from a release that still selects its predecessor. Its [small preflight and helper-interface check](evidence/core-preflight-and-control-rebase-20261004.json) passed: 132 dependency packages, the new test file discovered, and an actual protected ELF record verified through the qualified helper. Native and Core tests may overlap after database cleanup when their combined 12 GiB doubled data forecast and 48 GiB memory admission fit the original resource grant. Reuse qualified process/image guards; do not introduce a fresh custody-record grammar in each scope.

## Evidence sampling must preserve the run — October 3

The [preexecution sampler review](evidence/model-sampler-source-finding-20261003.json) caught an undefined cache reference in the proposed model-test resource sampler. An uncaught thread error could leave incomplete measurements while a thread-not-alive check appeared successful. No fixture or test operation had started. Keep the original source and fix the sampler separately; this is a test-evidence defect, not an established production worker failure.

Sampler completion must be explicit. Record unavailable resource and container observations with their original causes, and distinguish a completed sample stream from a terminated thread. An observation failure must not stop an already admitted model run or erase its raw outcomes. Qualify the sampler failure paths, then report any remaining measurement gap separately from test success. Apply the same distinction to production monitoring: failed observation is neither proof of changed authority nor evidence of a healthy outcome.

## Validator repair observation recovery — October 3

An unavailable manager, cgroup, policy or journal read does not prove that the validator generation changed. The [repair source review](evidence/validator-repair0fef-source-review-20261003.json) verifies SN `0fef99be`: nine changed files, unchanged module inputs and fourteen evidence bindings. The implementation preserves original read/cancellation causes before comparing returned state. Acknowledged generations and consumed stop/start reservations remain durable; later observation of the same generation must not issue another action. Confirmed inode or retained-marker loss remains an integrity refusal.

Fourteen deterministic new test roots and six neighbors are queued for independent normal/race qualification with old-body controls. Tests inspect persisted approval/journal state and actual stop/start counts, including failed readback after acknowledgment and recovery on the same generation. This source is uncompiled; current role composition, native accounting and production host adoption remain open. Observation failures must not manufacture a generation conflict or replenish consumed action budgets.

## Restore namespace fidelity — October 3

Restore must accept the paths already admitted by the retained inventory. Reusing the fresh-provisioning depth limit rejected valid existing data: inventory accepted 32 components, while preparation and monitor-tree restore capped paths at 16. The [source-reviewed successor](evidence/namespace-restoreaa46-source-review-20261003.json) permits 32 components for restore and keeps fresh provisioning at 16. Original absolute-path 1,024-byte and filename 155-byte limits remain.

SN `aa46d5c1` and Connect `e7f24717` include deterministic deepest-path, escaped longest-path and overflow controls through public restore and pending continuation. Root verified thirteen evidence bindings and six changed Git blobs. These candidates are uncompiled and do not inherit qualification from shallower fixtures. Require independent normal/race checks, current-source composition and an actual host restore before accepting this fix. Paths beyond the inventory ceiling must fail before target effects; preserve original references and accounting throughout restoration.

The [historical unchanged full-model run](evidence/customer-current-full-model-terminal-20261003.json) is terminal: Server `3f32d730` completed all 1,457 top-level roots with 1,437 passes, nine failures and eleven explicit skips; fixture cleanup exited zero. Root rehashed eighteen artifact bindings and independently reproduced the complete counts, failed/skipped names and zero unfinished roots. This source remains failed. The nine failures map to the recovery/query-guard, allocator and reservation-oracle batches below. Run one full successor suite after its affected normal/race gates qualify; unrelated optional probes must not delay broad collection.

The [thirteenth inactive-cache reclaim](evidence/cache-reclaim-13-20261003.json) verified removal of 222 unreferenced old compiler archives and recovered 3.02 GiB. It preserved active caches, modules, sources and evidence; a transient compiler-directory scan failure remains recorded. Qualification admission must still recheck the 110 GiB floor before each heavy phase. This local headroom result does not establish production capacity or backup readiness.

Do not stop broad qualification on its first ordinary assertion failure. Preserve the running source and collect all failures; repair in separate candidates, then qualify the actual combined release. Runtime/source approval and production activation remain distinct from offline implementation readiness.


The [original local restore race failure](evidence/local-pending-original-race-failure-20261003.json) retains four coverage refusals and three passing controls. The [corrected external signed-input fixture](evidence/local-signed-input-corrected-normal-20261003.json) now passes public resume, lost-ack recovery and two adjacent controls; its two pending-outcome roots still fail because their mock hides earlier historical receipts. Qualify the narrowly corrected pending mock separately. These results establish progress without claiming complete local recovery or changing production coverage requirements.

Astra’s source review indicates the dispute rollback fixture reads the old reservation counter while the public create path uses the newer counter. This is an unqualified fixture diagnosis: require a nonzero pre-sweep witness, both counters and the exact request token, followed by failed and successful public settlement checks. Apply the same review to drift and TTL failures; preserve every original result.

The [frozen allocator successor](evidence/redis-allocation-source-intake-20261003.json) preserves the public payer-client distribution policy and atomic whole-grant attempts. Ordinary discovery uses bounded keyset pages in original financial order, with explicit capacity holds and configurable reviewed limits. Root verified all 5,386 physical Git blobs and 44 file bindings; its [eight new roots and seventeen neighbors now pass independent normal execution](evidence/redis-allocation-normal-qualification-20261003.json), with both fixture cleanups successful. Root rehashed twenty-seven bindings and reproduced all twenty-five distinct passing names, with no failures or skips. The [same twenty-five roots also pass race detection and package vet](evidence/redis-allocation-product-qualification-20261003.json), with clean fixture shutdown; root rehashed seventeen result bindings and matched the exact passed names. Five causal groups and the reservation-oracle successor remain separate pending scopes. These selected gates do not close the final combined release. Unknown counters must remain retained while independently valid funding peers can continue; paid/free SN weighting does not authorize changing the Server funding policy.

The [admission policy review](evidence/admission-policy-boundary-review-20261003.json) distinguishes intentional approximate Redis admission from authoritative settlement and payout obligations. Upstream explicitly accepts cache-loss and still-open-after-24-hour over-admission; do not impose absolute admission consistency through a test correction. Verify public request-token TTL, legacy mirror drift and mixed public/legacy terminal settlement separately. Scheduled reconciliation has a five-minute recurrence and thirty-minute task budget. The targeted CLI applies by default and currently lacks a finite whole-operation deadline; improve cancellation and bounded execution without losing retained debt or changing the declared economics.

The [reservation test successor](evidence/reservation-oracle-source-intake-20261003.json) is frozen at Server `1d3c275b`. Root verified thirty-four file bindings, the clean source tree and an exact four-test-file delta over allocator `4aaf4402`; production and module bytes are unchanged. Its [five affected/new roots and fourteen settlement neighbors pass independent normal execution](evidence/reservation-oracle-normal-qualification-20261003.json), with both private fixture cleanups complete. Root rehashed twenty-seven bindings, reproduced the exact nineteen passed names and matched the verified allocator parent receipt. The [same nineteen roots pass race detection and package vet](evidence/reservation-oracle-product-qualification-20261003.json), with fixture cleanup complete. Root reproduced the exact nineteen race positives and matched the prior normal scope. One full successor model run is now active on this exact source; the original full-suite failure and separate old-oracle discriminator remain retained. Public dispute tests keep the real public create path; legacy-only batching and TTL tests select their legacy owner explicitly. A test correction must not erase a production accounting failure or substitute legacy coverage for the public path.

The [adjacent Safe read review](evidence/safe-read-error-adjacent-review-20261003.json) found combined read-error/mismatch branches in owner storage, nonce, guard slots, retained digest and pending code/authority checks. They can label missing observations as changed authority. Fix these in a separate successor: return read unavailability or cancellation before comparing values, then retain strict rejection of actual returned contradictions. Qualify the real public read paths; pure local signature/hash/encoding failures remain permanent. The frozen historical-receipt candidate does not close these adjacent paths.

Full-model skip attribution is now exact: `TestNetEscrowRevisionTriggerUsesContractKeyAfterUpgrade` requires authorized `auto_explain`; `TestSubscriberGuardQueryPlan` instead requires the explicit `ARIN_SUBSCRIBER_BENCHMARK=1` disposable workload. Source/config/GeoLite and optional benchmark skips remain separate input-dependent results. Follow-ups must retain their own sources and conditions, and must not rewrite the original eleven skips.

The [native runtime-renewal successor](evidence/native-runtime-renewal-source-intake-20261003.json) is frozen at SN `881b967a`. Root verified forty bindings, the clean commit/tree and twelve changed Git blobs; module files are unchanged from `00d`. It preserves original checkpoint identity while allowing reviewed append-only read-purpose runtime history, nonshrinking capacities and acknowledged retry-budget renewal. Ten new roots, sixteen neighbors, eight causal groups and a separate actual-old-grammar positive remain pending. Literal compatibility fixtures are synthetic old-layout bytes; no old-binary fixture-generation claim is made. Continuous EVM economics and rolling claims remain separate implementation work.

## Historical RPC recovery lesson — October 3

A missing expected historical receipt is an unavailable read, not proof that
previously authenticated chain evidence changed. Retry expected historical
receipt and transaction-position reads within the original signed route budget;
retain typed unavailability on exhaustion or cancellation. New approvals should
use the preferred 300-second budget, while existing signed 60–900-second budgets
must remain unchanged. An intentionally nullable pending-transaction lookup has
a different contract and must not wait merely because its outcome is not known.

Do not join a transport timeout or unavailable read with a canonical-boundary
conflict. Returned malformed data or contradictory hash, status or nonce still
requires an integrity refusal. Recovery must preserve the original signed
transaction and durable outcome, continue healthy independent owners, and never
resubmit blindly. Qualify delayed success beyond sixty logical seconds, null-read
exhaustion, cancellation, positive contradictions and healthy-peer continuation
through the actual production path. The [production successor is now frozen](evidence/retained-evm-read-source-intake-20261003.json) at SN `5a0a8f66`: root verified seventeen bindings, all seven changed Git blobs and the clean commit/tree. Its nine-root normal/race scope, package vet and four historical causal controls remain pending. The paired-head and Redis scoped receipts do not qualify it; its historical dependency graph also does not establish the final composed release.


## October 6 payout transition target

The requested mainnet launch target is **2026-10-06 00:00:00 UTC**. The shared
provider payout policy belongs in `config/main/sn.yml`. Implementation and
qualification are in progress; this target is not yet a verified deployed
schedule or a mainnet activation receipt.

The owner-approved policy allows obligations earned before the boundary to
finish paying in USDC afterward, including the unplanned legacy backlog and
processor retries. New mainnet earnings start at the inclusive boundary.
Use one explicit accounting boundary across legacy planning, queued sends,
completed-contract usage and SN epoch settlement. Do not convert unpaid dollar
obligations into alpha or pay the same usage through both systems. Reconcile
already-submitted transfers throughout the transition. Customer billing is
outside this provider payout change.

Before announcing the schedule as operational, qualify the exact Server and
config sources, deploy them, verify the loaded UTC policy and mainnet identity,
and demonstrate the running payout workers enforce the boundary. Mainnet
contract, signing, custody and runtime authority remain separate launch inputs.

The transition rollout must retain evidence for each step. The [planner
ownership correction](/mnt/data/sn-testnet/mainnet-parallel-20261004/oct6-planner-ownership/HANDOFF.json)
adds a shared transaction lock and original-allocation comparison, but old
planner binaries ignore that lock. Replace and join every old provider payout
planner before resuming the new planner set; verify the actual running images.
Keep accepted processor attempts and their original idempotency keys available
for reconciliation. The four new concurrency/cancellation roots, eighteen
neighbors and three omission controls remain unexecuted at the source handoff.

1. Merge the qualified Server and config revisions, then publish the exact
   binaries/images and an immutable `main/sn.yml` revision. Preserve the reviewed
   UTC boundary and legacy-obligation policy; a later configuration edit must
   not turn post-cutoff earnings into new USDC obligations.
2. Apply and verify the payout schema features and initialize the durable
   earning-policy anchor from the exact reviewed config before admitting new
   allocations or sends. The anchor is implemented in frozen Server candidate
   `cdcb61fa`, with [independent scoped qualification complete](evidence/payout-boundary-qualification-20261003.json). Final combined release and deployment remain pending. Initialization
   must be idempotent for the same boundary and refuse a conflicting earning
   policy; subsequent readiness activation must not redefine earning time.
   Join any replaced worker and preserve its pending processor attempts and
   original idempotency keys; do not clear them during rollout. A missing or
   unavailable anchor suspends affected new work while accepted transfers
   continue reconciliation.
3. Start the qualified payout planners, payment workers and SN usage readers
   with the same declared policy and selected mainnet identity. The policy file
   is read per operation, while other process configuration may be cached;
   changing a file alone does not prove coherent adoption across workers.
4. Run `bringyourctl sn-transition-status` in the deployed environment and retain
   its config digest, selected identity, schema status and readiness reason.
   The command explicitly does not prove deployment: independently record each
   running executable/image and configuration revision, then verify actual
   worker behavior before, at and after the boundary.
5. If mainnet readiness is blocked at the boundary, retain post-cutoff usage and
   keep pre-cutoff USDC reconciliation running. Report the blocked new-earnings
   path and its claim limits. Recovery must preserve the boundary and retained
   attempts; rolling back to an old unrestricted USDC writer is not an approved
   recovery path.

These are rollout requirements, not completed production steps.

The [durable-boundary source checkpoint](evidence/payout-boundary-source-20261003.md)
records the frozen implementation and 13 new deterministic controls, followed
by its separately retained normal/race, causal and vet/build results. It does not prove a merged release or deployed earning schedule.
The [first boundary execution attempt](evidence/payout-boundary-fixture-setup-failure-20261003.json)
failed during container initialization before any test; corrected execution
uses a distinct readable fixture copy without changing product source.
Owned cleanup completed. The corrected exact-source boundary qualification is now complete; the original setup failure remains recorded.

The [frozen transition plan](evidence/payout-transition-plan-20261002.md)
records candidate Server `b19f1eba` / config `93dc65fd`, including the exact
close-time partition and explicitly blocked mainnet activation. The
[first normal batch](evidence/payout-transition-first-normal-20261002.json)
passed 15 of 16 selected tests, four package vets and the CLI build. The
remaining test failed during fixture creation before its paid/free assertion;
its correction and the manual-bonus/processor-snapshot safeguards are being
implemented in a separate successor. No completed qualification, merged payout
code or deployed schedule is claimed by this checkpoint.

The separate Server successor `ffcc77b8` freezes the fixture correction,
adjustment provenance, amount/wallet reservation and processor-send safeguards;
its [independent normal batch](evidence/payout-transition-successor-normal-20261002.json)
completed 24 passes and one fixture failure across 25 selected tests, with no
skips. The [independent race batch](evidence/payout-transition-successor-race-20261002.json)
has the same 24 passes/one fixture failure, no skips and no race reports.
[Five affected package vets and the CLI build](evidence/payout-transition-successor-vet-build-20261002.json)
also pass. The later financial successor below preserves canceled payments' original
subsidy and reliability obligations through replanning, including after the
cutoff. Finish its full current model qualification before treating this
transition as qualified.
Neither candidate has been merged or deployed as the operational schedule.

Financial successor Server `97d22989` now freezes original-component recovery,
bounded historical ambiguity handling, exact-attempt response admission and
same-transaction request/outcome history. Its
[independent 36-test normal batch](evidence/payout-retention-first-normal-20261002.json)
completed 35 passes and one fixture failure, with no skips. The fixture used the
renamed `network_point` table. Test-only successor `63027130` corrects it to
`account_point`, also checks unchanged point value and verifies held-payment
continuation; production and module bytes remain identical to `97d22989`.
Qualification runs the three affected tests separately while retaining unchanged
successful scopes. The [three affected normal tests](evidence/payout-retention-fixture-normal-20261002.json)
now pass [normally and with race detection](evidence/payout-retention-fixture-race-20261003.json),
with no skips or race reports. [Five affected package vets and the CLI build](evidence/payout-retention-vet-build-20261003.json)
also pass on that exact source. The full `./model` suite is running with an
isolated PostgreSQL/Redis fixture; its terminal result remains pending.
Its optional missing-operator-proxy check has a [separate passing one-root
run](evidence/model-geolocation-companion-20261003.json) with an explicitly
pinned companion checkout. That pass does not rewrite the original suite's
skip or close other optional-feature skips.
The [six normal recovery control groups](evidence/payout-retention-causal-normal-20261003.json)
produce all 12 intended assertion failures: stale-attempt admission, retained
obligations, bounded recovery, historical excess and atomic journal/reset
updates are discriminated. These include explicitly labeled omission controls
and an old-body control with the current request-journal precondition; they are
not all unmodified historical-source runs. [Fourteen additional financial recovery roots pass with race detection](evidence/payout-retention-attempt-race-20261003.json),
with no skips or race reports and completed owned-fixture cleanup. They cover
actual amount/wallet admission, stale processor outcomes, retained obligations,
bounded recovery and atomic attempt history. The
[race recovery controls](evidence/payout-retention-causal-race-20261003.json)
also produce all 12 intended assertion failures across six groups, with no race
reports and completed owned-fixture cleanup. The
[normal final-Circle, submission-basis and CLI controls](evidence/payout-operative-causal-normal-20261003.json)
produce five intended failures, including actual post-limiter refusal and stale
amount/wallet protection. The [same operative controls under race detection](evidence/payout-operative-causal-race-20261003.json)
also produce all five intended failures, without race reports and with owned
fixture cleanup complete. The [full financial model suite](evidence/payout-retention-full-model-20261003.json) at exact Server `63027130` now passes: 1,400 top-level roots, all 249 nested subtests, zero failures and nine explicit skips. The skips remain recorded; the optional proxy and [nested SQL-plan checks](evidence/payout-retention-auto-explain-20261003.json) now have separate exact-source passes. Later policy binding and GET retry candidates require their own qualification.
These checkpoints do not supply completed release, deployment or activation
evidence.

The separate [payment GET retry candidate](evidence/payment-get-retry-independent-20261003.json)
at Server `5c93b812` passes 12 normal/race roots and controller vet. Actual Circle
transaction and Coinbase rate reads have one caller-owned budget (300 seconds
by default, minimum 60 seconds), bounded responses and typed retry causes.
Cancellation and identity/schema refusals remain distinct; transfer submission
is outside the read loop. Timing controls use an injected clock. Its
[retry-omission controls](evidence/payment-get-retry-causal-20261003.json)
produce four intended transient/no-retry failures and retain two permanent/hard
positive controls in each mode, without race reports. This is an explicit
omission experiment, not an unmodified historical-source run. The broader
wallet-read successor is now frozen at `25f617dc`, with its [26-root test
intake](evidence/wallet-get-intake-20261003.json) verified and queued. Current
release composition remains pending; neither source is deployed.

The [read-only signed-ledger correction](evidence/restore-ledger-readonly-independent-20261003.json)
at `d3c84fe3` separately passes two normal/race roots and validator vet in its
pinned workspace. It permits nonempty signed-history inspection without a
writer and preserves signature/byte-bound refusal. The [pre-fix valid-prefix
control](evidence/restore-ledger-readonly-causal-20261003.json) reproduces the
original nil-hook panic normally and under race detection, without modifying
original production source. The [public ledger-restore adapter](evidence/restore-ledger-adapter-independent-20261003.json)
at `912315cf` separately passes six roots normally and under race detection,
plus mainnet/validator vet, after correcting scratch ancestry without source
changes. The [old public-dispatch control](evidence/restore-ledger-public-causal-20261003.json)
reproduces the missing ledger semantic adapter in both modes without changing
original production code. Native raw/member/multi-owner restoration and current
published composition remain pending; no production restore operation has
occurred.

The sole normal failure is in the model slice: the paid/free test attempts to
change immutable terminal attribution before reaching its weighting assertion.
This historical failure precedes the separately qualified correction below. Root, controller, taskworker
and CLI selected tests all passed. This failure does not establish a weighting
defect or a successful weighting check. The separate test-only correction
`6a63892b` replaces the invalid non-NULL `"open"` fixture outcome with production's
NULL active state and checks that terminal cancellation becomes immutable;
its [targeted independent test passes normally and with race detection](evidence/payout-transition-fixture-qualification-20261002.json).
That one-test scope reaches the actual SN usage reader and proves equal
paid/free bytes, active/canceled exclusion and no new USDC plan for those
post-cutoff rows. It does not relabel either failed 25-test batch as a pass or
qualify the pending financial successor. Preserve the initial private
PostgreSQL setup failure separately from these actual test results.

## Retained preparation checkpoint — October 3

This is the retained October 3 checkpoint. The [October 4 reconciliation](PRELAUNCH-FIXES.md#reconciled-status-of-all-38-requirements--october-4) supplies current status for all 28 hardening outcomes and ten production gates. The component results below retain their exact original sources and do not assert current final-release or live deployment acceptance.

| Component | Current evidence | Remaining work |
| --- | --- | --- |
| Miner GET recovery, guarded spool and retained-member recovery | Integrated on SN main with the scoped independent receipts cited below. | Include their exact bytes and dependencies in the final release and recovery rehearsal. |
| Fleet runtime capability selection | Integrated at `9671f456`; [independent 23-test normal/race scope](evidence/fleet-runtime-catalog-progress-20261002.md). | Current runtime authority, full native-role coverage and composed release qualification. |
| Server blob readers | Server origin/main `29ce22d6` preserves the payout source and newer local-authority/probe and Redis repair work; [26-test independent reader scope](evidence/current-graph-reader-progress-20261002.md) remains historical. | Full current-server dependency/release composition and remaining model-suite qualification. |
| Provider payout transition | Published config `93dc65fd`; financial630 full-model pass and [frozen Server2a combined qualification](evidence/payout-get-composition-qualification-20261003.json). | Candidate `19952183` now passes targeted upgrade/Redis tests normal/race, causal controls, five vets and CLI build. Current SN `8cb08c95` / Server `0e04f038` compatibility passes 25 selected roots in normal and race modes on the published `5def2fa4` graph, with ten package vets; [the complete scoped receipt](evidence/current-module-compatibility-qualification-20261003.json) binds 122 verified files. The joined Server is now [published at `29ce22d6`](evidence/server-publication-20261003.json), retaining current upstream with dependency `08d48400`; complete the final current model scope, customer join and deployed worker-adoption verification. |
| Offline preparation | Tracked SN `0384cbfc` / Connect `7600ea5c` / Server `c2563f9a` pass [75 author tests per mode by an exact checksum-only source join, seven vets and three binary builds](evidence/preparation-tracked-module-20261002.md). Earlier `1f66a2bd` retains its separate 101-test independent scope. | Integrate published Connect `08d48400` and independently qualify the current composition; active SN `8cb08c95` / Server `0e04f038` compatibility remains pinned to `5def2fa4`. Candidate `9d7d57fc` preserves all `e08e1b11` claim/provider source; its join is source-only. |
| Directory owners and private-root creation | Directory owners pass [103 author tests per mode](evidence/directory-owner-preparation-author-20261002.json); explicit fresh leaf-root creation at SN `c8998b31` / Connect `7600ea5c` passes [109 author tests per mode and four vets](evidence/private-root-preparation-author-20261002.json). | Independent current-main tracked-module qualification, retained/restore semantics and capacity revisions. |
| Provider and claim monitoring | Provider monitoring is integrated at `83d92f75`; [20 independent tests per mode and three vets](evidence/provider-monitor-integration-20261002.md) preserve exact source scope. The durable claim producer is integrated at `bd5e7e72` with [37 author and 29 independent tests per mode](evidence/claim-projection-independent-20261002.json), retaining separate graph scopes. | Finite-window claim consumer is now integrated with [23 normal/race roots and causal qualification](evidence/claim-consumer-qualification-20261003.json). Complete continuous expectation renewal, current module/source composition, economic-domain monitoring and actual alert/recovery rehearsal. |

The offline preparation command now has scoped author coverage for fresh ledger,
native, fixed snapshot and directory owners, including explicitly reviewed
private leaf-root creation. Its tracked dependency was authenticated from local
Git through a file proxy. The shared recovery source is now published as Connect `08d48400`, retaining the qualified `9fb897f8` durable-volume subtree and current discovery/write-admission code. The earlier `9e7ec0af` publication remains historical; consumer adoption and qualification remain separate. Retained/restore
semantic rebinding and joined capacity/retention revisions remain implementation
work. The separate physical-export candidate is unqualified and grants no restart.

The explicit physical-restore core at Connect `53bc92fa` has now passed
[32 independent normal tests](evidence/restore-core-independent-normal-20261002.json),
with no skips or failures. This scope checks original member identity, exact
source/target authority, namespace capacity and interrupted publication. It does
not qualify public SN/native adapters, retained revisions or an operational
restore. The separate [same-root race/vet addendum](evidence/restore-core-race-vet-20261003.json)
now passes; the remaining consumers and production restoration stay open.

The separate SN `1982267c` snapshot/fleet/claim restore increment passes
[seven independent normal tests](evidence/restore-snapshot-independent-normal-20261002.json).
It preserves original payloads and pending intents while rebinding reviewed
physical storage identity, including owner-device state at its original logical
path. Race/vet/causal scopes, ledger/member and multi-owner restore, retained
revisions and current published-module composition remain open.

The qualification volume has another
[15.63 GiB of inactive compiler archives reclaimed](evidence/cache-reclaim-8-20261002.json),
with live-process and repeated physical-file checks. Its metadata reference
scan explicitly excludes generated container runtime storage. This creates
headroom for the model run; it does not close production sizing or restore gates.

Launch still needs an exact current composed release, runtime/genesis/checkpoint
and production policy authority, provisioned custody and host configuration,
restore/upgrade qualification and approved bootstrap actions. Runtime 472 is an
unapproved observation; the v470 planning exception does not extend to it.
Owner device and production signing inputs remain live gates. Keep the 10% native
miner allocation / 90% owner recycle policy and both validator roles unchanged.
Both were later superseded: the reserve receives the 90% since October 5, and one
sole UR validator replaces the two since October 7.

## Historical qualification checkpoints

**October 2 retained-member integration:** main merge `b0fc5e99` now includes the qualified retained-member recovery and exact public custody fixtures. The original current-composition batch completed nine passes/one census assertion failure in each mode; the separate corrected execution root passed independently in normal/race plus vet. [Exact scopes and preserved failure](evidence/successor-member-qualification-20261002.md). Main code/module bytes match the reviewed correction while newer docs remain retained. New release, module intake and storage preparation gates stay separate.

**October 2 adjacent reader successors:** Server main `1d72f577` now includes exact qualified `2c4e5dca` blob and module changes. Independent26 affected tests pass normally and with race detection, plus vet; old-body controls retain seven failures/six positives per mode. [Integration and exact scopes](evidence/current-graph-reader-progress-20261002.md) preserve newer upstream work and keep final current-main release composition open. Preparation `f5c0707b` remains a separate author-qualified reader candidate (11 validator/seven public CLI tests per mode, three vets) awaiting independent review. Native/snapshot preparation and fleet catalog work retain separate scope receipts.

**October 2 runtime intake:** the public archive now exposes exact finalized-block runtime **472** at block 9,197,096. The [retained read-only capture](evidence/runtime-472-route-observation-20261002.json) records its code/metadata hashes and raw snapshot bindings with `unapproved_observation` admission. Snow VPN RPC returned HTTP 502 through a 60-second read budget. Continue public RPC preparation; independently audit the new artifact before any signing admission. The v470 planning exception does not authorize v472. Offline hardening continues.

**October 2 finite-claim GET recovery — integrated:** main merge `d5c5df7bf368221253a514cae046e4cd56d55d33` now includes `40d07ae6`; all miner and module bytes match the independently qualified source. Nine affected tests pass normally and with race detection; vet passes. Both old public read-call controls fail for the expected typed transient status in each mode. The [independent receipt](evidence/finite-claim-independent-20261002.json), SHA-256 `de05ae8dcc5418e03ef448c3618a3223c04f30935e12531758fab5d0a21a3f9c`, and all 23 manifest bindings were rehashed. The sustained-outage test exercises real SDK/HTTP reads with a deterministic retry clock representing 65 seconds per read; it is not a 65-second wall-clock outage rehearsal. The original 300-second deadline test also uses an injected clock. Only epoch/pool GETs retry; signing/submission remain outside the loop. No release deployment is implied.

**October 2 preparation successor:** frozen SN `f4ad15db` / Connect `ea827777` now runs the full bounded fresh-ledger preparation batch after correcting control-record sizing before effects, post-header cancellation uncertainty, EOF handling and plan transport. The full bounded author batch passes 73 core, seven ledger and seven public CLI roots in each of normal/race modes, plus four package vet scopes. Root verified all 95 sealed [author manifest](evidence/fresh-ledger-preparation-author-20261002.json) bindings; independent review remains pending. This slice enrolls a fresh, already provisioned daemon ledger only. Owner-local signing enrollment, native/snapshot adapters, private-root creation and retained/restore remain separate work. The exact reader post-cancellation byte-admission issue is assigned to a narrow successor; current results will remain immutable.

**October 2 current-graph and reader progress:** Server `22e3c1ba` independently passes 12 normal/12 race selected tests plus vet on published Connect `e0d75562`. Upstream changes are retained in review `32196d57`; its reader successor `2c4e5dca` awaits actual current-graph independent qualification. Guarded spool reader `68a7be85` is now merged into main after 15 author and 15 independent tests in each mode plus vet. [Exact scopes and retained failures](evidence/current-graph-reader-progress-20261002.md) remain separate from full release qualification.

**October 2 offline preparation candidate:** the actual `storage-prepare plan/apply` dispatcher and fresh-ledger adapter now exist at candidate SN `a045a9c7` / Connect `03aafa41`. Both public tests pass with a retained reader-contract correction overlay: exact apply produces a ledger the real validator can reopen, and a wrong digest leaves no effects. The original compiled candidate failed with wrapped EOF and remains retained. Commit/freeze, crash-recovery, race/vet and independent qualification are pending; native/snapshot, retained/restore and private-root creation remain subsequent implementation work. This candidate is not on main or qualified for production.

**October 2 provider monitoring:** actual provider readiness publication and independent expected-roster monitoring at frozen `98fcff06` pass 42 author normal/42 race tests plus vet. [Evidence and dependency intake](evidence/provider-monitor-progress-20261002.md) keep proof/settlement unknown, independent qualification pending and server current-graph testing separate. This does not close MG-07 or authorize deployment.

**October 2 integration update:** qualified miner startup/callback source `36648203` and test-only private-root fixtures `28c970db` are now merged into main. Callback qualification passes 40 author roots and 12 independent selected roots in each of normal/race modes, plus miner vet; fixture qualification passes seven roots in each mode under both umask 002 and 077. [Exact evidence and limits](evidence/provider-callback-qualification-20261002.md). Current server dependency intake, provider readiness monitoring and offline storage preparation remain separate work. These merges do not qualify a new release or authorize activation.

**October 2 continuation checkpoint:** observer `6d398662` is now independently qualified at 17 normal/17 race roots plus vet. Composition `69f4bbdd` / server `10a8f4d8` now passes 78 selected normal/78 race roots and vet, with a separate independent 12-root normal/race scope and vet. The [exact sealed composition](evidence/durable-owner-composition-qualification-20261002.md) is an incremental source qualification. Miner startup isolation `c3707376` passes 37 normal/37 race roots, while callback isolation, member recovery ordering and production preparation remain separate work. The [checkpoint and retained receipts](evidence/mainnet-continuation-checkpoint-20261002.md) record exact scopes, a newly exposed pending-stage ordering defect, private fixture ancestry and immutable physical evidence paths. No launch or full-backlog closure is inferred.

**October 2 MG-06 release composition (MG-02):** the [qualified scoped successor](evidence/release-258e25b4-server0aa1-20261002.md) at frozen SN `258e25b4` / server `0aa1e244` now packages economic-observer `dd21ed00` and signed-schedule `258e25b4` with the prior owner, contract-admission, recycle and schema-752 composition. Sequential source and eight-image repeats match and pass independent readback. Fresh current-pair qualification passes 83 normal/83 race roots and three-package vet; an independent nine-root normal/race scope also passes. Thirty-six component receipts and unchanged-server tests preserve their original scopes. This resolves the earlier source exclusion for the new artifact only. All ten fresh unsigned-plan actions stay blocked; independent compiler/dependency provenance, production policy/configuration, rollout/restore, published/running image identity and live authority remain open. Earlier releases stay immutable. Later server `025802a5` retention-debt/cleanup changes remain outside this frozen release. Its [static review and focused qualification checkpoint](evidence/server-and-storage-progress-20261002.md) is now recorded: 33 server roots pass normal/race and four package vets; seven SN composition roots pass normal/race, with its original `6cfc4773` mutex-copy vet failure preserved separately. Three historical server roots pass normal/race. The [full model run and separate test-only repair](evidence/server-stats-fixture-qualification-20261002.md) are now recorded: original `025802a5` executed all 1,345 roots with 1,334 passes, one statistics-fixture failure and ten optional skips; the isolated `a3fc4270` repair passes all eight affected roots normal/race and package vet. The original full run remains failed, and no patched-source full-suite pass is claimed. No successor production release or deployment qualification is inferred.

**October 2 retained relay fixture correction:** the separate [test-only fix](evidence/relay-retained-fixture-qualification-20261002.md) at SN `734a82dc` passes 35 author normal and 10 race tests, plus 2 independent normal and 10 race tests; `./sim-testnet` vet passes in both scopes. It retains the original signed plan and journal while rebuilding fixture synchronization, canonical header coordinates and the actual producer activation domain. The original `6cfc4773` vet/header failures remain preserved. This does not qualify the broader model run or change the frozen `258e25b4` / `0aa1e244` production artifact.

**October 2 durable storage (MG-09 / PH-09): in progress.** The merged Connect `6cd720cf` [generation/read-admission primitive](evidence/durable-generation-progress-20261002.md) passes 48 author and independent roots per mode plus vet. The new [isolated owner-custody checkpoint](evidence/durable-owner-custody-qualification-20261002.md) separately qualifies native journal `695f6683` (42 author/independent roots per mode), snapshot `a5c765c4` (17 roots plus 17 subtests per mode), and miner fleet/claim `b3c3d66` (201 roots plus three inherited subtests per mode). Monitor `f1b445f9` passes 114 author and independent normal/race roots and vet; these are still separate consumer scopes. These sources require explicit public-entry policy and preprovisioned retained-member authority, preserve reads under write pressure, reconcile only exact pending bytes after the old owner joins, and stop only the affected role. Connect inventory-v3 `0a5cda0e` separately passes 57 author and independent normal/race roots and vet with bounded owner-attribute retention. A separate [CLI candidate `7fb6b6a1`](evidence/durable-inventory-cli-qualification-20261002.md) passes nine author and independent normal/race roots and three-package vet, including explicit owner-local commands and report-only rebound comparison. The [independent receipts and new composition failures](evidence/durable-composition-progress-20261002.md) are now retained. Connect constructor `71df099c` independently passes 59 roots per mode and vet (including the prior 57) and is merged; the current consumer `0a5cda0e` module does not inherit it. The former `cb2e3ffe`/v2 and frozen release receipts are unchanged. The separate [validator/root/bootstrap/local-blob adopter](evidence/durable-adopter-qualification-20261002.md) at SN `eb0abe22` / server `1b7cc78b` passes 111 selected normal/race roots and four-package vet in both author and independent scopes; five causal controls discriminate per mode. The real service-credential test passes separately in the author scope and its evidence was independently rehashed. Module-only server `005a9066` pins the same tested Connect source, with a separate standalone dependency join. The independent adopter receipt is source-pinned to that exact pair; immutable registry/member census, broader fixture migration and peer composition are not closed. Production offline root/lease/nonce/owner-anchor preparation is still missing. Validator/bootstrap/root/server composition, deployment declaration assets, capacity/rotation policy, actual restore and a new exact release remain open. No source receipt authorizes deployment or closes all of PH-09.

**October 2 actual public composition failures:** exact test-only `ea2bb37f` reproduces three failures in normal and race: persistent root observation omits durable context, a missing completed passive-monitor checkpoint is recreated, and passive preparation admits a missing retained snapshot head. Separate fixture `22d4ee3e` over unchanged adopter production exposes 48 successor write-admission refusals plus two outdated typed-identity assertions (75 roots: 25 pass, 50 fail). Fixes and complete source integration are in progress; neither failed composition is a qualified launch candidate. [Exact evidence and scope boundaries](evidence/durable-composition-progress-20261002.md).

**October 2 passive observer continuation:** the rendered passive service uses `Restart=no`; source `653061a1` exits after one exhausted preparation-observation budget and abandons unused signed samples. Its separate 11-root author normal/race and vet receipt remains scoped to the earlier custody/retry fixes. Implemented successor `97c7ae85` retains the same owners, emits `storage-unavailable` without a fresh observation and continues at the signed interval; exact test-only `6d398662` now passes 17 normal/17 race roots plus vet independently, preserving the earlier RPC-count fixture failure. Separately, the exclusive successor writer fix `f3c8a618` passes eight new plus two typed-identity roots independently in both modes and vet. Complete composition and module adoption remain open. [Evidence and continuation lesson](evidence/durable-observer-continuation-progress-20261002.md).

The [implementation backlog](evidence/mainnet-implementation-backlog-20261002.md)
maps all 28 PH requirements and ten MG gates to actual production callsites,
separating missing code, scoped implementation, audit leads and live inputs.
Storage composition is one slice; provider/domain monitoring, runtime
continuation, retry coverage, economic proof and full acceptance remain open.

Updated 2026-10-02. **Mainnet activation is blocked.** At the user's direction,
preparation now uses the [Rao Foundation public archive RPC](evidence/public-archive-switch-20261001.md)
until Snow finishes synchronizing. The archive returned mainnet genesis and
EVM ID 964 and supports the tested historical runtime read. Its public method
profile lacks `debug_getRawHeader`; the qualified exact-header fallback now
passes a [live combined native/EVM finalized snapshot](evidence/public-finalized-snapshot-20261001.md).
Independent identity and
runtime-source approval remain open. No transaction has
been sent through either route. The earlier read-only
[Snow/LAN RPC comparison](evidence/snow-rpc-route-20260927.json) showed that
`http://172.28.208.185:9944` served the same **testnet** chain as
`http://192.168.1.162:9944`: EVM chain ID **945** (`0x3b1`), rather than the
expected mainnet ID 964. During the operator's node-data move, the
[13:42–13:47 UTC read-only retry on September 28](evidence/snow-route-observation-20260928-0115.md#follow-up-recheck)
returned HTTP 502 for chain-ID reads across 21 attempts. That is an unavailable
route, not a mainnet identity. Verify the restarted route and independently
approve the mainnet chain identity before admitting any signer.

The [contract installation admission correction](evidence/contract-installation-admission-qualification-20261002.md)
at source `ebf69b9` makes the first reserve CREATE check pending balance against
its own and all later same-sender value/gas reservations. Proxy and descendant
review now require pairwise distinct deployer, owner, guardian and oracle
addresses, including unsigned preview. Author qualification passes 35 selected
roots normally and under race plus vet; independent qualification passes fourteen
roots in both modes plus vet. Unchanged-source controls reproduce both defects,
including one underfunded synthetic send and 48 admitted role collisions.
Original signed bytes, journal custody, attempt ceilings and the separate Safe
approval remain intact. The [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) now packages it with owner `0f7c8698` and recycle `25aa1515`; the older `3d1e2ecf` receipts remain unchanged.
Production policy, live authority, installation and activation remain open.

**September 30 operator report:** Snow mainnet is still synchronizing. This is
the current operator report, not a new verified RPC observation or a mainnet
identity/readiness attestation. Live activation remains closed while sync,
independent chain identity and the other production gates are unresolved.

The [original contract custody correction](evidence/original-evm-custody-qualification-20261001.md)
closes a local ownership gap in the first eight contract actions and their
receipt/installation readers. A file lock protects its opened inode, so matching
bytes at a replaced pathname do not preserve ownership. The owner now retains
the physical directory, original marker and exact journal through reconciliation,
counted publication, send and completion; shared readers retain the same marker
identity through their final observations. Missing completed custody stops the
operation and must not be reconstructed from an in-memory record. Ordinary
interrupted unsigned claims and exact-byte receipt recovery keep their original
scope. The [exact custody successor](evidence/release-1d580d5e-serverac86-20261001.md) packages writer `cb9f3aa2`,
reader `1922981d` and diagnostic `4e6b4a7e` at SN `1d580d5e` / server `ac86855d`,
with matching local source/image repeats. It supersedes the SN `28ebfced` baseline
while independent release and live authority gates remain open. The separate root/trim stores,
cross-host custody and live acceptance remain unqualified by this correction.

The [bootstrap readiness custody correction](evidence/bootstrap-readiness-custody-qualification-20261001.md)
at source `3d1e2ecf` separately covers the five original preparation markers and
the passive profile's three-marker subset. Their original physical custody and
exact journals now remain checked through receipt/installation observations,
successor send admission, trim publication and validator/passive-host decisions.
An observed integrity failure preserves original signed bytes and consumed start
allowances; an intact interrupted preparation remains recoverable. All 44 author
normal roots, fourteen race roots and vet pass; independent qualification passes
all eleven new roots in both modes plus vet, with seven unchanged-source causal
failures. A separate omission proves the final validator host check. The frozen
`1d580d5e` release excludes this correction; the
[qualified readiness baseline](evidence/release-3d1e2ecf-serverac86-20261002.md) packages that source, but is
superseded for launch by the separately qualified owner-trim source `0f7c8698`,
contract-admission source `ebf69b9` and recycle-custody source `25aa1515`, now included in the [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md). Full release and activation gates remain open.
The [owner-trim successor](evidence/owner-trim-retained-intent-20261002.md) now
qualifies the additional trim marker/journal and bounded original-byte submission
under a separate signed residual policy. Legacy root native mutation ports remain
unavailable and outside this qualification. Actual owner/device authority, risk
acceptance, external custody and live activation remain open.

The [two-UR current-admission route](VALIDATOR-CURRENT-ADMISSION.md) has
[scoped qualification](evidence/validator-current-admission-qualification-20261001.md)
for separately signed initial starts at frozen SN `b8dc332a` with server
`0b8e758d`. It freshly binds original CREATE/anchor custody and EVM scan
floors to native eligibility, strict majority stake capacity, current contract
views and both operator/key/proof domains. Exact independent current-policy and
lifetime signer/host-custody acceptance are required. Empty authenticated local
tails are the initial scope; unfinished intents require separate recovery.
Permanent per-unit claims, post-sync rechecks and responsive generation evidence
preserve one-start semantics. Actual approvals, live admission/systemd rehearsal,
root-service authority and applied 10/90 evidence remain unresolved. No unit or
signer was activated by implementation or tests. Separate bounded SN compatibility
with server `720e7c61` passed; schema 750/subscriber-v2 rollout remains outside
that evidence.

Sim-testnet is closed with known exceptions at the user's direction. The
[original R48 report](../sim-testnet/FINAL-4.md) remains a failed provisional
attempt with **zero completed acceptance epochs**. Its later retained resume
recovered services with no setup actions dispatched and explicitly retained
`final_acceptance=false`; it did not produce final acceptance. The process-log
scanner overrun, missed native epoch, policy-scoped client-key rollover,
historical R46 handoff provenance bug and usage debt are mapped to concrete
production work in [the gate tracker](PRELAUNCH-FIXES.md#production-gates-in-execution-order).
Do not restart testnet or make a passing testnet result a fictional input to
the mainnet plan.

The original bootstrap design was based on SN
`a59294e98ea02d05125015ae02cf32f2c0059c8a`. Subsequent implementation and
qualification must use an explicit composed release, including compatible
SN/server/SDK/Connect/config revisions. Some shared and simulator fixes exist;
the complete mutating bootstrap, root-validator service and operational repair
system remain production work.

**Server `720e7c61` is a separate integration candidate.** The
[static review and qualification gate](evidence/server-720e-integration-review-20261001.md)
requires migration **750** before those binaries take traffic, plus an exact
SN/server compatibility receipt and successor release/source-image inventory.
The [independent disposable-schema check](evidence/server-schema750-qualification-20261001.md)
passes the selected 749-to-750 readiness, default-off, seed-picker, normal/race
and vet scopes. It also proves a mixed-writer v2 activation blocker: an old
UPSERT can retain a prior positive quality attestation after changing a row.
Keep the launch `provider.yml` subscriber policy absent/`0` and the v2 candidate
classifier/MMDB out of active inputs unless every operator's actual miner cohort
proves fresh trails through the SN Quality/force-minimum seed picker. Under v2,
unknown or legacy connection facts are excluded even from fallback and named
selection; service health alone cannot prove trail progress. The existing
contract/operator/activation qualifications remain scoped to server `0b8e758d`
until the separately recorded compatibility gate passes. This review does not
change live policy, rebuild artifacts or approve deployment.

The [schema-751 mixed-writer successor](evidence/server-schema751-write-guard-20261001.md),
server `a464bb3e`, adds a per-write token/trigger and revokes existing unbound
positives. Its author qualification passes 27 selected roots normal/race and
vet, including the full live guard and warm-cache/rollup behavior; removing the
trigger fails both causal model controls. Independent normal/race/vet and
trigger-omission controls also passed. The correction is merged into server
`main` at `94229abb` but absent from the pinned server-720 release. Keep v2 off while successor
source/image qualification, schema-751 lock-duration checks, complete writer/API
rollout, lookup coverage and every operator's actual miner-trail/load canaries
remain open. Readiness permits an older binary on the newer schema and does not
establish that policy readiness.

[mainnet/main.go](main.go) implements signer-free
`inspect`, `runtime-snapshot`, `finalized-mapping`, `finalized-snapshot`,
`monitor`, `subnet-discover`, `subnet-preview`, `owner-trim-plan`, `owner-trim-recheck`,
`owner-trim-reconcile`, `owner-trim-qualify`, `root-preview`, `root-monitor`,
`check-recycle-mode`, `economic-reference`, offline `source-lock`,
[local `release-inventory`](RELEASE-INVENTORY.md), and the
[signer-free blocked `plan`](PLAN.md). Two narrower executable phases now exist:
[`bootstrap plan/apply/resume`](BOOTSTRAP-ROOT.md) retains local root custody
and imports externally signed payloads, while
[`bootstrap-contracts preview/plan/apply/resume`](BOOTSTRAP-CONTRACTS.md)
prepares the first eight installation actions through unanchored validator
evidence CREATE under the approved transaction journal. The
[`bootstrap-chain plan/apply/resume`](BOOTSTRAP-CHAIN.md) command composes their
offline custody preparation with a retained trim review and two protected UR
role inputs under one restart-safe local journal. Its v2 preparation verifies
both initial schema-3 signed configs against independent role/runtime inputs;
its [offline qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
keeps live producer eligibility and service activation as separate gates. V3
independently approves the separate root-service configuration. The read-only
`bootstrap-chain readiness` increment binds that accepted v3 preparation and
its original child journals to one current finalized census, with distinct
UR/root generation, permit, window and checkpoint blockers. Its
[exact-source qualification](evidence/bootstrap-readiness-qualification-20260929.md)
passes all ten new normal/race roots, the 148-root adjacent normal/race scope,
vet and four causal controls. The redundant broad race process was intentionally
terminated after its passing prefix; the exact 148-root disjoint race union is
the qualified coverage. The later [composed source check](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md)
passes focused normal/race, vet and fences on SN `e35771ec` and server `b7c8c743`.
It retains false activation/current-authority flags and all pending chain phases.
The [offline contract prerequisite increment](BOOTSTRAP-CHAIN.md#offline-contract-installation-prerequisites)
exposes the original eight-attempt/nine-action mismatch before signing and
inspects original action custody without replay. Its [focused qualification](evidence/bootstrap-contract-prerequisites-focused-qualification-20260929.md)
passes twelve new roots and six causal controls normal/race. The separate
[partial adjacent battery](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
passes 150/270 roots in both modes; 120 remain unrun. Eight retained successful actions
leave only the evidence anchor unfinished. The separate
[execution custody and owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) now implements an
independent approval domain, exact eight-action adoption, separate local nonce
registry claims and durable cumulative one-send/reconciliation machinery.
Its [scoped qualification receipt](evidence/bootstrap-successor-execution-qualification-20260929.md)
records twenty-one focused and six adjacent roots passing normal/race on
corrected `75ea2158`, with ten causal control pairs and a verified forty-five-file
evidence manifest.
The first private-input fixture failures and the later ten-minute race
harness timeout remain preserved. The exact race retry passes with an explicit
twenty-minute package budget, without changing production deadlines.
The concrete [canonical execution adapter](BOOTSTRAP-SUCCESSOR-EXECUTION.md#separate-canonical-authority-and-online-resume)
has [scoped independent qualification](evidence/bootstrap-successor-canonical-qualification-20260930.md)
on corrected frozen source `a7186754`: ten focused and twenty-two adjacent roots
pass normal/race, fourteen normal causal controls and six selected race controls
reach their intended assertions, and the sealed source/module fences match.
Preliminary `82da3d40`
passed all eight roots normal/race before the Safe provenance gap was identified;
those results do not qualify the corrected source. Online resume requires a
separate signature binding Safe build review, a reviewed current runtime,
signer-cutover evidence and a separately signed exact Safe deployment/storage
history statement.
It reauthenticates all eight original receipts through the existing native/EVM
adapter, checks scoped finalized/pending Safe and contract state, reconciles the
exact retained transaction and retains one-counted-write machinery. The current
runtime may differ from the historical original runtime under that new approval.
The ten roots include actual pinned Safe proxy/singleton execution and
lost-reply recovery in two full local graph fixtures with an explicitly synthetic
history capability. RPC finality and pending
state remain owned-node assertions; external build review and complete signer
cutover remain independently attested assumptions, not facts proved by a local
registry or a global transaction-pool census. No live authority is established.

**P0 gate: canonical Safe deployment and storage provenance.** The original
complete-history submission route remains unavailable. Without the separate v2
current-only opt-in below, public `--submit` exits before custody loading or attempt
reservation. A signed
history report is necessary review input, but cannot provide the missing
`bootstrapSuccessorSafeProvenanceAuthenticator`. Current Safe owner/module
getters cannot exclude nonzero mapping entries unreachable from their sentinel
lists; tests inject real orphan owner and module entries into the published code
and demonstrate this gap. Implement and independently qualify deployment,
initialization and complete authority-relevant storage/delegatecall history
through finalized and scoped pending state before enabling complete-history sends. The
adapter must bind the exact Safe/profile, approved route and signed evidence;
ordinary getters or a loosely labeled file cannot substitute. Read-only
reconciliation remains available. The separate
[qualified readmission increment](evidence/bootstrap-successor-readmission-qualification-20260930.md)
`cd4261a8` moves expensive history authentication before final pending
Safe/relayer nonce and Safe-state admission, and invalidates earlier admission
when a refresh fails. Both heavy roots and three selected adjacent roots pass
normal/race; both causal controls reproduce their intended failure in both
modes, with exact source/dependency evidence sealed. This does not supply the
missing history authenticator. MG-08 remains open.

The separate [bounded Safe archive census](SAFE-HISTORY-CAPTURE.md) has
[sealed offline qualification](evidence/safe-history-census-qualification-20260930.md)
on paired SN `36fea176` and server `d21492c3`: 68 positive root executions pass
normal/race; ten normal controls and five selected race controls are causal,
including a distinct normal published-Safe fixture-oracle control. Its actual
read-only command retains private durable witnesses, reuses the native
ordered-trie and server receipt decoder, accepts unrelated traffic, and retains
exact direct calls and committed logs over a pinned interval. Complete internal/reverted
execution, native hooks, clean deployment and pending authority remain unproven;
the report keeps every history/send verdict false. This evidence layer does not
implement the missing provenance authenticator or change the public-submit gate.

The [qualified native trace increment](evidence/safe-history-native-trace-qualification-20260930.md)
adds bounded SDK block traces and authenticated parent runtime code proofs to
that retained census, with canonical closing checks. It marks SDK-filtered
ClearPrefix/root events and missing rollback or inner/reverted EVM execution as
unproven. The complete-history authenticator and public submit gate remain
open; no live node trace was qualified.

**Qualified read-only Safe current-authority proposal.** The separate
[current-storage proof](SAFE-CURRENT-AUTHORITY-PROPOSAL.md) at `aa9f715b`
authenticates every storage word under the exact Safe's native account prefix,
the reviewed runtime and published proxy/singleton code plus native metadata at
one canonical finalized snapshot. Missing intersecting branches, orphan owner or
module mappings and any extra storage refuse admission. The
[qualification note](evidence/safe-current-storage-qualification-20260930.md)
records eleven new and ten adjacent roots passing normal/race, all ten normal
and exactly five selected race controls causal, with source/dependency evidence
sealed. This is read-only evidence and a distinct
signed policy proposal, with no history claim, custody import or public-send
capability. Existing native RPC cannot prove a complete pending overlay; the
final scoped recheck reports that limitation explicitly.

**Qualified current-policy custody increment.** Frozen source
`3f88a948` adds the [separate signed acceptance journal](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md),
exact counted/outcome references and partial-publication recovery under the
original exclusive owner. Later policy or runtime approvals cannot rewrite an
earlier counted outcome, reset attempts, release nonce claims or reduce maximum
liabilities. The original history statement remains retained and no historical
truth is inferred from the current-only proof. The
[qualification note](evidence/safe-current-custody-qualification-20260930.md)
records eight new and thirty-one adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and sealed exact
source/dependency evidence. That custody increment installed no public import
flag or production capability; the separately qualified native capability below
adds read-only import and the internal proof path. The later public v2 route below
requires an explicit new independent risk-policy acceptance.

**Qualified native current-policy capability.** The separate
`95a905d4` [capability](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
connects independently signed current-policy custody to the concrete native
complete-prefix verifier. Public online resume can import pinned acceptance
files for local custody and historical reconciliation. That September 30 source
kept public submission closed; its v1 acceptance remains public read-only.
An internal route requires the complete retained runtime tip and both policy
signatures, keeps proof work before final scoped pending checks, and preserves
one exact counted send. Its proof block/hash/root remains separate from any
later admission-window head. The
[qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and the sealed exact
source/dependency evidence. Two original control-oracle mismatches remain
unresolved in the receipt, with fresh corrected reproductions retained separately.
No policy approval or public-send activation is claimed; the original signed
history statement remains retained and unproven. Complete pending storage and
independent finality are not established by the owned RPC observations.

**Public current-only Safe route, October 1.** The [v2 acceptance interface](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md#explicit-public-v2-acceptance)
adds a separate signature domain and exact `--accept-safe-current-policy` opt-in.
The latest independent acceptance must expressly allow bounded public submission
and accept owned-RPC finality, non-atomic scoped pending checks, signer cutover,
absent complete history/pending proof and changes between reads. V1 cannot be
upgraded by a flag. Both signatures, original custody and the complete runtime tip
remain required; wrong or absent acceptance refuses before reservation or send.
The route uses the existing complete finalized prefix proof and repeated final
readmission, preserves the original exact signed bytes/nonce/attempt/liability,
and leaves historical read-only reconciliation available after interruption.
The [qualification receipt](evidence/safe-current-public-qualification-20261001.md)
records 68 author roots passing normal/race, three causal control pairs and vet;
independent review passes 14 focused roots normal/race and vet on its separately
pinned server graph. The exact source and 79-file evidence manifest are sealed.
Actual independent production
v2 acceptance, signer cutover, live authority and installation remain open MG-08
gates. No risk policy was accepted and no live transaction was sent in this work.

**Original-authority contract installation readback.** The
[anchor/readback producer](BOOTSTRAP-CHAIN.md#original-authority-evidence-anchor-and-installation-readback)
joins all eight original receipts, the exact counted Safe anchor and its binding
event to complete five-contract and Safe storage proofs at anchor inclusion.
Its stable installation identity preserves the original approvals, terminal
journal and receipt across later observations. A separate current snapshot proves
complete Safe storage and the existing executable/domain views without treating
normal accounting or a later observed Safe nonce as new transaction authority.
The [installation clock correction](evidence/installation-policy-clock-qualification-20261001.md)
also requires the current epoch-zero policy block to equal the original proxy
CREATE receipt's EVM inclusion block; a later observed policy clock cannot
renew that original authority. Author and independent source tests pass within
the receipt's stated graph and harness limits.
The typed producer lets service admission share its selected finalized boundary;
a JSON report cannot authorize a service. Readback makes no network write but
can finish the original local terminal journal after interruption. Current-only
v2 policy selection/signature, signer custody, live installation and service
activation remain external gates. Complete history is not inferred from current
storage. See the [scoped evidence](evidence/contract-installation-anchor-20261001.md).

**Contract-to-validator declaration admission — scoped qualification complete.** The
separate signer-free [contract-role plan](BOOTSTRAP-CONTRACT-ROLES.md) binds both
signed UR configs to the approved coordinator proxy, vault and initial policy
identifier, with the evidence journal's exact immutable domain. Pairwise config
agreement cannot substitute for this deployment binding. The original preparation
and recovery scope stay unchanged. This earlier declaration check alone does not
verify canonical installation, the evidence anchor, deployment scan floors,
current state or service activation. It does not advance a durable chain phase
or install a public send route. The corrected `f4470d0d`
[qualification receipt](evidence/bootstrap-contract-role-qualification-20260930.md)
records 40 positive executions (eight focused and twelve adjacent roots, each
normal/race) and three causal controls in both modes. The earlier `43dcd01f`
fixture failure remains separate evidence.

**Original contract receipt admission — scoped qualification complete.** The
[read-only receipt command](BOOTSTRAP-CONTRACT-RECEIPTS.md) reauthenticates all eight
exact original native/EVM inclusions and historical postconditions, with separate
per-receipt budgets and final canonical continuity. Its signed EVM scan-floor
check excludes omission of the original deployment prefix; normal finalized-head
advancement is allowed. The `c6b31fdb`
[qualification receipt](evidence/bootstrap-contract-receipts-qualification-20260930.md)
records five focused and sixteen adjacent roots passing normal/race (42 positive
executions), five normal causal controls and exactly three selected light race
controls. Current state, the evidence anchor, complete indexing, installation
and service activation remain unverified; all original pending phases remain.
No live RPC, transaction or public Safe route is part of this increment.

**Current bootstrap contract fields — scoped qualification complete.** The
[read-only current-state command](BOOTSTRAP-CONTRACT-CURRENT.md) composes exact
original receipt admission with the original five-account bootstrap profile at
one finalized native/EVM mapping. Exact runtime/getter/slot replies remain
owned-RPC assertions. The snapshot stays distinct from later canonical continuity;
normal head advancement is accepted. The `2b87b133`
[qualification receipt](evidence/bootstrap-contract-current-qualification-20260930.md)
records three focused and sixteen adjacent roots passing normal/race (38 positive
executions), all six normal causal controls and exactly two selected light race
controls. Original zero-activity/policy requirements remain strict. An unset or
expected evidence pointer grants no anchor-history claim; complete storage, Safe
authority, installation and activation remain unverified. No public send route or
live mainnet action is introduced.

**Qualified runtime authority increment.** The
[additive revision path](BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md) at
`3d526830` retains separately signed runtime revisions under the original
independent key and immutable base authorization. Each revision binds reviewed
artifact/codec evidence and its exact predecessor. Original receipts, signed
transaction bytes, both nonce claims, counted attempts and full liabilities stay
intact. Current admission matches approved complete artifacts; historical reads
select the inclusion/parent pair from the full history, including more than ten
retained profiles. [Independent qualification](evidence/bootstrap-successor-runtime-qualification-20260930.md)
passes ten new and thirty-six adjacent roots normal/race, all twelve normal
controls and exactly seven selected light race controls. Exact source/dependency
evidence is sealed; the earlier canonical/readmission receipts remain unchanged.

**Current native-header authority correction.** The
[first implementation](evidence/current-native-header-authority-20261001.md)
authenticates producer and fleet headers. Its
[adjacent successor](evidence/current-native-header-adjacent-authority-20261001.md)
at SN `30354d78` also closes upload and independently signed observation-window
substitution, startup/activation evidence, applied-row journals and shared
native receipt/state readers, including the separate historical finality witness.
Complete header coordinates and closing canonical
checks preserve original signed windows and signing/execution/post-state roles.
Local qualification supplies no automatic runtime approval or live acceptance.
The [claim EVM finality correction](evidence/miner-claim-evm-finality-20261001.md)
at `6dcb94a1` replaces the native/EVM clock comparison with exact EVM state and
closing finality checks, including fresh receipt publication and original-byte
recovery. The [shared onchain correction](evidence/shared-evm-finality-closure-20261001.md)
at `222e45a8` also closes finalized and canonical receipt witnesses for the
shared send path; transient errors, missing blocks and regressed finality stay
pending within the original deadline. MG-04/PH-04 stay open. The
[bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
packages these corrections at exact SN `28ebfced` / server `ac86855d`; independent
release qualification and live acceptance remain separate gates.

The later [bootstrap finality correction](evidence/bootstrap-finality-qualification-20261001.md)
at `98df8b5f` retains the original finalized witness through historical identity
selection and closes dependent runtime, census, root, readiness and validator
admission reads. Ordinary finality advancement preserves the selected snapshot;
a regressed frontier or replaced witness refuses new evidence. The separate
`41f053ab` successor closes the refreshed EVM admission pass after account and
contract reads, before it can publish send readiness. Refusals retain the original
signatures and nonces without consuming an attempt; recovery sends the same
transaction once. The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
packages these exact source corrections at SN `28ebfced` / server `ac86855d`.
The linked evidence distinguishes exact source qualifications,
fixture corrections and disjoint passing race continuations from the preserved
package timeouts. Owned-RPC assertions do not close independent chain/runtime
authority or live activation gates.

**Runtime continuity policy proposal — inspection boundary qualified.** The
[signed compatibility envelope and inspector](RUNTIME-CONTINUITY-POLICY.md)
bind original production-validator authority to a separate semantic verifier,
exact future artifact, complete consumed-interface/economic scope and output
provenance. Inspection checks the owned finalized snapshot and later continuity
without changing the original signing view or custody. A genuine semantic
verifier, proof replay and durable production selection remain absent; a matching
metadata profile or signed assertion alone cannot open fresh signing. MG-04 and
RT-04 remain open. The miner's existing original-runtime recovery is unchanged.
The [scoped qualification record](evidence/runtime-continuity-policy-qualification-20260930.md)
records fifty positive root executions, seven normal and three selected race causal
controls, exact source/dependency seals and the separate original failed fixture
attempt. This qualification supplies no semantic verifier or production selection.

**Finite runtime replay — execution boundary qualified.** The separate
[offline transition executor](RUNTIME-SEMANTIC-REPLAY.md) now checks exact old/new
Wasm against explicit finite state cases under a signed executable/rules/evidence
boundary and a bounded subprocess owner. It compares return bytes and full declared
storage effects in on-chain context. This is finite fixture coverage, not complete
economic equivalence or authenticated mainnet state. Automatic selection and fresh
signing stay closed; no production route is installed. The
[sealed qualification](evidence/runtime-semantic-replay-qualification-20260930.md)
records 48 Go positive executions normal/race, 19 Rust tests normally, eight normal
and four selected Go race causal controls, with exact source/build/dependency seals.

**P0 follow-up: automatic compatible runtime admission (RT-04).** The additive
path still requires a new independent signed artifact review for each upgrade.
It does not supply automatic compatibility or eliminate that live approval gate.
Design and qualify a separately approved compatibility authority and preserve
every historical profile, signature, nonce claim, attempt and liability. Same-version
changed artifacts and unsupported codecs remain explicit closed gates. The
missing genuine Safe history authenticator and public-submit gate are unchanged.
The qualified [signed local successor preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md)
adds a separate approval domain and fixed resumable local claim while preserving
original receipts and additive proposed floors. Its [scoped receipt](evidence/bootstrap-successor-preparation-qualification-20260929.md)
passes thirteen focused and three adjacent roots normal/race, plus thirty-two
intended causal executions. It provides no executable allowance, Safe authority
or signing; a different physical custody root requires approved migration.
Its [separate six-root composed smoke](evidence/bootstrap-successor-preparation-qualification-20260929.md#separate-composed-smoke)
passes normal/race on exact merge `93a0a060`, including both full-v3 commands,
same-root recovery, pure Safe calculations and MG-07 incident recovery.
The [qualified unsigned successor proposal](evidence/bootstrap-contract-successor-qualification-20260929.md)
retains the eight completed seals and any ninth reservation while computing
additive attempt/lifetime ceilings for the unfinished anchor and retry margin.
Six new and twelve inherited roots pass normal/race with six causal controls in
both modes. The [separate full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
now passes normal/race and all six intended causal executions on `c294fefd`.
It uses genuine local eight-action execution and original v3 preparation. Its
finite 60-second local send budget is approved before custody; the earlier d7
one-second race timeout remains preserved as a noncausal attempt.
The earlier fifteen-root MG-07/prerequisite composed smoke passes both modes on
`79ff2c6e`, and the later twelve-root successor composed smoke passes both modes
on `1e2b2abb`. These scoped results supply no Safe authority, signed adoption or
live installation; the separate SDK count remains 543/618 with 75 pending.
The [qualified offline Safe release verifier](evidence/safe-release-profile-qualification-20260929.md)
checks explicit 1.4.1/1.5.0 Safe/SafeL2 published proxy/singleton code, ABI,
source/compiler inputs and storage layout. Its six new and four adjacent roots
pass normal/race with sixteen intended causal executions. Independent rebuild,
current account and initializer-owner binding, authority, signing and execution
remain false. Existing versus new Safe is unresolved; a different new address
requires separately authorized ownership migration from the retained owner.
The [qualified pure Safe evidence layer](evidence/safe-execution-evidence-qualification-20260929.md)
binds the exact EIP-712 digest, supplied signature structure/recovery and declared
inner outcome/nonce semantics to those published profiles. Thirteen focused and
four adjacent roots pass normal/race; all twenty-six causal executions reach
their intended assertion. The corrected oracle filters the selected variant
before decoding; the earlier e074 setup failures remain preserved as unqualified.
This layer has no command, signer, current owner/threshold proof, canonical
receipt authentication, custody or execution path. The artifact verifier's
separate nine-root composed smoke also passes normal/race on `18a88db4`.
The pure layer's [separate four-root composed smoke](evidence/safe-execution-evidence-qualification-20260929.md#separate-composed-smoke)
passes both modes on `c648495f`; it predates the signed local preparation merge.
The [offline successor Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) has
[scoped independent qualification](evidence/bootstrap-successor-safe-review-qualification-20260929.md)
on `d0207448`. It joins the completed signed preparation, all eight original
receipt seals and the full selected published release to the exact evidence
anchor digest. It borrows custody read-only, refuses partial claims and retains
both completed and unexecuted original liabilities in its proposed outer cost.
Test compilation and vet pass. Independent runs pass nine focused and four
adjacent roots in both normal and race modes; all fourteen causal executions
reach their intended assertion. The sealed logs, patches, exact source and
dependency graph were independently checked.
The focused roots include a full-v3 public-command fixture and the published
Safe proxy bytecode oracle.
This is unsigned review: owner signatures and complete outer calldata are absent,
and no nonce, budget, execution approval or global custody is allocated. The
native review window cannot expire a Safe signature because it is outside the
Safe digest. Canonical original receipt adoption, current Safe/evidence authority,
signature lifetime and window enforcement and globally fenced relayer signer
custody remain launch gates. The separate execution owner enforces its conditional
state transitions in code; its concrete canonical adapter has scoped independent
qualification, while the production Safe history authenticator and explicitly
approved live authority remain missing.
No mainnet transaction,
contract installation or validator activation is established by this increment.
The [qualified owner-trim action](evidence/owner-trim-null-storage-repair-20260929.md),
integrated at `ce567305`, adds durable exact-action recovery and actual-subset
reconciliation under original v3 custody. Sol's 25 focused roots and exact
229-root expanded union pass normal/race, with vet and eight causal controls.
The failed first candidate `4033609` and its null-storage failure remain
preserved; the successor corrects the production proxy reader. Its separate
approval cannot replace current authority in strict v1/v2: their named
enforcement and custody capabilities remain required. The October 2
[explicit best-effort owner workflow](OWNER-TRIM-BEST-EFFORT.md), frozen at
`0f7c8698`, adds a fresh Ledger action domain and a separately signed submission
policy over the original signed bytes, exact runtime/census/protected generations,
mortality, fees and consumed allowance. Its [scoped qualification and current
prerequisite check](evidence/owner-trim-retained-intent-20261002.md) retain permanent
physical-custody failures, canonical uncertainty reconciliation and independent
pruning/re-entry risk choices. The observed SN25 immunity had expired, and the
inspected v470 owner cannot close all registration routes; usable submission
therefore needs explicit risk review beyond the conservative defaults. No live
risk acceptance or owner signature is supplied. The [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) includes this source; the older `3d1e2ecf` artifact excludes it. Full production qualification and live approval remain open.
The historical owned Snow route returned HTTP 502 at 06:59 UTC on September 29,
providing no current mainnet identity or authority. The complete bootstrap,
Safe evidence anchor, native signing device and live role
activation remain unfinished. The
[retained Snow inspection](evidence/snow-route-inspect-20260927-1051.json)
observed chain ID 945, which fails the required mainnet ID 964 gate.
This plan and its read-only evidence perform no
mainnet transaction, deployment, UID removal or validator activation.

## Bootstrap prerequisites and current blockers

| Prerequisite | Current disposition and next result required |
| --- | --- |
| Interim public mainnet RPC and independent identity authority | The [October 1 public archive capture](evidence/public-archive-switch-20261001.md) observed mainnet genesis, EVM ID 964 and runtime spec 470. The [qualified exact-header fallback](evidence/public-header-fallback-20261001.md) passed a [live combined native/EVM finalized snapshot](evidence/public-finalized-snapshot-20261001.md) without `debug_getRawHeader`. Independent genesis/runtime/source approval and a current complete SN25 census remain outstanding. Snow remains an unsynced future failover. Pin the public archive for read-only discovery and unsigned plans; recheck finalized identity before signing. |
| Immutable qualified release | Compose the actual SN/server/SDK/Connect/config and contract artifacts, including selected branch fixes and migration order; qualify their real interfaces and publish an approved manifest. Historical R48 builds do not qualify later per-user deposit or zero-price changes. |
| Exact mainnet census and authority | Read SN25 membership, roles, custody, immutable contracts and locks at one finalized snapshot; resolve reset feasibility and all protected identities before making an executable plan. |
| Economic and custody decisions | The user selected **owner-recycle for the remaining 90%**. Implement and qualify that path and the 10% native-miner target on the actual runtime; finalize mainnet policy, tolerance, keys/Safe, root-registration protection and spend/count/expiry ceilings. Recycled value is not reserve custody. No testnet allowance carries over. |
| Production safety and liveness | Close the linked recovery, runtime, policy/identity, settlement and monitoring gates; retain independent history and complete signature/nonce ownership. Testnet provisional exceptions grant no mainnet authority. |
| Operations and staged acceptance | Install monitor/alerts in the existing telemetry stack, name primary/backup on-call, rehearse bounded repair and rollout/rollback, then collect actual native and settlement evidence after an approved activation. |

Read-only inspection and offline implementation can proceed while required
inputs remain unresolved. The planner must expose those blockers and refuse
mutating phases until their exact dependencies and authorization are complete.
The current Snow xops `vars.yml` still selects `testfinney` with EVM ID 945 and
the testnet genesis. Its [prepared cutover guard](https://github.com/urnetwork/xops/commit/ec443da)
is not deployed: it requires the data mount in both full-host and isolated
lightnode rollouts, renders a network-specific bootnode including finney's
`/ws` transport, and rejects mixed testnet/mainnet identity inputs. Before
starting the mainnet node, select distinct reviewed node generations, the
approved finney genesis and runtime pins, EVM ID 964, bootnode host/port/peer,
and reference route as one configuration. Observe the started node's finalized
identity; configuration checks alone do not establish it.

## Requested outcome and decisions

The bootstrap must deliver all four requested outcomes:

1. Reset the existing miner registrations on our UR subnet (Bittensor SN25, netuid 25), with an exact census and an explicit meaning of reset.
2. Install and initialize the production contract set with the approved custody and governance identities.
3. Begin provider rewards at **10% of the native miner allocation**. This is not 10% of all subnet emission, not a validator take, and not the head/tail steering parameter.
4. Operate both an owned **root validator on netuid 0** and an owned **validator on the UR subnet**.

The target UR mainnet netuid is SN25 (netuid 25). The user selected `owner-recycle` for the other 90% on 2026-09-27; this recycles native allocation and does not fund our reserve. The verified mainnet RPC route, keys, spend ceilings, reset mechanism and exact runtime implementation remain inputs to the executable plan. None has a default mainnet address or financial allowance. Existing testnet spend approvals do not authorize mainnet spend.

Two constraints determine the implementation. There is no demonstrated subnet-owner call that arbitrarily clears every miner registration while retaining an arbitrary list of validators. Also, the current UR contracts and validator policy do not provide a standalone switch that changes the native miner allocation to 10%. The planner must expose these as capability decisions, not claim that lowering UID capacity or setting `theta: 0.1` fulfills them. The requested 10% target allows the exact runtime's explicitly established quantization tolerance; a stronger enforceable hard cap is a separate assurance choice, not an additional user requirement.

The standard UR validator now implements the separately approved
[schema-3 production path](OWNER-RECYCLE-PRODUCTION.md) for the measured 10/90
owner-recycle row. Live native economics and migration of already signed work
remain unqualified. The read-only root observer alone cannot activate a root
signing service; qualified current authority, actual native custody and service
wiring remain explicit gates. Zero-price/equal-demand support changes operator
demand/deposit semantics; it does not by itself cap native miner allocation or
choose where the remaining 90% goes.

The miner fleet now has a [mainnet runtime authority gate](../miner/FLEET-MAINNET-RUNTIME.md)
for register, publish, bind, status and revoke. It requires separately approved
genesis, source/build review and exact code/metadata/version bytes before
signing, submission and receipt readback. That source change does not supply
those approvals. Its separately qualified durable recovery retains original
signed transactions and reconciles uncertain sends; partial-scan checkpointing,
automatic runtime admission and live deployment remain separate work.

The draft policy is `reset.mode: unresolved` and **`emissions.remainder: owner-recycle`**. A preview remains non-executable while reset capability, the runtime-specific 10%/90% mechanism or other required inputs are unresolved. Qualify the selected economic mechanism before installing an immutable vault or removing existing registrations. The remainder choice is settled; live economic qualification and the exact signed production policy remain work.

## Source and runtime boundary

The September 14 design inspected Subtensor [commit `67dcf7f791dc495064c293f080a0702cb433e51e`][subtensor-commit], dated 2026-09-07, following the release-455 merge, in `RaoFoundation/subtensor`. The source-specific capability observations below describe that baseline, **not an attestation of the current mainnet Wasm**. Recheck them against the selected live runtime and approved artifacts before planning any action.

The production inspection gate must authenticate one finalized native block and its corresponding canonical EVM block using the explicitly approved mainnet RPC route. The Rao Foundation public route is the approved interim transport until Snow synchronizes; an owned node is not a prerequisite for that fallback. Endpoint ownership and independent finality authority must be recorded separately, and agreement between routes from one provider does not establish independent verification. Require an independently approved genesis hash, EVM chain ID 964, native chain identity, complete runtime version, `:code` hash, metadata hash, node build identity, and the reviewed runtime source/artifact mapping. Verify the finalized native header's complete SCALE bytes against its hash before using that hash for state reads; a header number and same-height lookup are insufficient. Decode one complete runtime identity, rejecting contradictory `stateVersion`/`systemVersion` aliases. The `runtime-snapshot` command captures exact finalized code and metadata bytes, verifies code against its storage hash, and repeats canonical/network checks after reading them. Its output remains an unapproved observation. Admission still needs signed-extension, call, storage and precompile review, an independently reviewed source-to-Wasm mapping, and native/EVM finalized mapping. A matching `specVersion` alone is insufficient; [the existing runtime authenticator](../crv4/runtime_identity.go) already binds more than that number.

For native/EVM mapping, the current Snow testnet header carries a Frontier
`fron` consensus digest whose payload names an EVM block hash. The signer-free
[`finalized-mapping` command](FINALIZED-MAPPING.md) authenticates the native
header, decodes the reviewed digest variant, fetches raw EVM RLP by that hash
with canonicality required, reproduces its Keccak hash, and checks canonical
lookup using the EVM header's decoded number. Equal block numbers alone are
not a mapping. Its [Snow evidence](evidence/finalized-mapping-snow-20260927.json)
remains unapproved until the selected mainnet identity, runtime and source
artifact are independently reviewed. The signer-free
[`finalized-snapshot` command](FINALIZED-SNAPSHOT.md) now captures runtime
bytes and this mapping under one authenticated finalized native hash, with
final canonical rechecks after both reads. Its [Snow evidence](evidence/finalized-snapshot-snow-20260927.json)
reproduces code, metadata, native header and EVM header hashes at that one
block. Two separate latest-head observations still cannot be joined into one
launch-plan authority merely because their chain IDs match.

The read-only observation at **2026-09-27 04:16:25 UTC** compared Snow VPN
`http://172.28.208.185:9944` with LAN testnet `http://192.168.1.162:9944`.
Both returned `system_chain=Bittensor`, `eth_chainId=0x3b1` (945), node version
`4.0.0-dev-e18ca67f1a0`, genesis
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`,
and finalized native head
`0x3e9119c77dcb7b12557035023f9ad3dbadc60f24443d01c0f32a6c14d81f35d7`.
The [raw identity record](evidence/snow-rpc-route-20260927.json) has
`same_identity_and_head=true`. This is an observed **testnet genesis and route**,
not an approved mainnet genesis. A node's `Bittensor` display name is insufficient
network authority. Both routes must be rejected for mainnet in this state.
The later [07:41 UTC Snow readback](evidence/snow-route-inspect-20260927-0741.json)
still returned EVM ID 945 and the same testnet genesis; no mainnet route cutover
has been observed.

A later mainnet deployment at either address requires the correct owned route,
fresh readback and independent operator approval of the genesis/runtime domain.
Do not guess a different port or inherit a library/public fallback. Bind RPC URLs,
resolved upstreams, TLS identities where applicable and local proxy routes in the
plan. The Foundry configuration no longer defines public `mainnet` or `testnet`
RPC aliases; deployment and probe commands require an explicit owned URL. A
loopback proxy must have the approved owned mainnet node as its sole
upstream. Preserve zero artificial request pacing on that route; bound
concurrency, retries and cancellation. A separately approved read-only comparison
node is an independent observer, never a silent signing/submission fallback.
Protocol block windows and on-chain rate limits still apply.

The standard UR validator's native submission path uses
`author_submitAndWatchExtrinsic` and requires an explicitly approved WS/WSS
route. Its HTTP native-read adapter does not provide subscriptions. Configure
HTTP EVM/read access and WS native access on the same owned node as distinct
explicit routes; never infer a URL conversion or silent fallback. A bootstrap
or observer that submits by a different qualified mechanism retains its own
transport contract.

At admission, record native and EVM clocks separately. Verify their mapping; do not assume equal height or treat an EVM receipt as native finality. Historical reads must remain at the receipt's authenticated block. Until RT-01 through RT-08 are qualified for production, an unknown runtime stops dependent new signing pending explicit adapter admission. The target operating model automatically admits a compatible consumed profile under the approved compatibility policy, retains exact historical identities and suspends only unsupported operations. A testnet provisional profile alone cannot authorize that production behavior.

Retain the authenticated runtime proof with each historical or signing view;
metadata-cache eviction must not revoke that view or force its immutable audit
to run again. The [RT-06 correction](evidence/runtime-proof-eviction-20260927.md)
implements this ownership boundary for provisional consumers while retaining
fresh block/chain identity checks, exact signing domains and strict mainnet
rejection. A new connection must establish its own authority; the correction
does not qualify automatic production runtime admission or durable proof reuse.

Validator stake and schedule reads now have [separate block-bound read
capabilities](evidence/validator-read-capability-20260927.md). An independently
approved future artifact can satisfy its consumed storage and selective API
profile without a compiled spec-version entry. A schedule also requires its
epoch-storage profile; a stake-only read does not. These checks reject changed
interfaces before storage decoding and retain exact historical pins. They grant
no new signing or production configuration authority; wider automatic runtime
admission remains part of RT-01 through RT-08.

The [atomic source capability](evidence/source-runtime-capability-20260927.md)
also removes the source encoder's redundant spec list for independently
approved successors. A private bound view retains its exact artifact/block
witness, then checks the selected atomic calls and ordered signing extensions.
Source preparation refuses a different block before reading storage or taking
a nonce. Offline signed-byte reconstruction establishes encoding and signature
integrity; it cannot grant runtime approval. Original bytes remain reusable
after cold authentication at the same artifact, while a changed signing domain
still requires separate reconciliation.

The standard validator's [qualified source-receipt correction](evidence/validator-source-runtime-qualification-20260929.md)
keeps original preparation, parent execution and post-state runtime views
separate across an independently approved upgrade. Original signed bytes stay
fixed; events use execution metadata and commitment readback uses post-state
metadata. All 103 selected receipt and adjacent roots pass normal and race
qualification. Changed execution signing domains, unsupported consumed
interfaces and absent historical approvals still fail. Automatic runtime
approval, wider current-head consumers, both validator roles and live release
qualification remain open.

The validator now has a separate [mainnet runtime observation admission
path](evidence/mainnet-runtime-observation-20260927.md). A schema-2 config pins
an ordered history of independently reviewed exact artifacts, provenance and
finite native block intervals. Every observation checks the configured route,
fresh native name/genesis/EVM964 identity, finality and canonical block before
selecting that interval's artifact; a later approval cannot reinterpret an
earlier interval or expand an already loaded config. This removes the outer
compiled-version gate for runtime identity reads. It grants no storage profile,
producer, signing or submission authority: those remain blocked until the
complete production successor policy and independently verified mainnet
identity are qualified. Existing schema-1 config and historical authority stay
unchanged.

Current source changes matter to this design:

| Subject | Source-backed observation | Bootstrap consequence |
| --- | --- | --- |
| Subnet emission allocation | The inspected `get_shares` uses price EMA, a `1 - MinerBurned` adjustment, then an emission gate. A flow-based helper also exists but is not the selected `get_shares` path. [Source][subtensor-shares] | Do not assume an older Taoflow formula or a root-validator vote controls our subnet's allocation. Attest the actual runtime path. |
| Current root strategy | Runtime470 has removed `set_root_weights`. Dividends accumulate where earned; optional coldkey/proxy basket trades change holdings. [Current guide][root-reborn-470], [removal migration][root-removal-470] | Select the separately approved [passive root service](ROOT-PASSIVE-SERVICE.md) in a fresh v4 plan alongside both UR validators, or a v5 plan alongside the sole one (the SN25 launch). Retain legacy signed actions and their recovery history. |
| Miner collateral | Registration collateral can survive deregistration; later earnings can affect release and capture. [Official collateral guide, pinned source][collateral-guide] | A UID reset is not a balance, lock, or stake reset. Pool capture must distinguish emission, locked collateral, and principal. |
| Native versus signed limits | The local whitepaper records runtime-dependent weight-limit behavior and requires a signed policy cap. [Local specification](../WHITEPAPER.md#15-concrete-parameters) | Observe runtime getters and enforce the signed cap independently. Do not assume a successful setter changed native enforcement. |

When documentation and the exact runtime source disagree, record the discrepancy and resolve it against the authenticated runtime. For example, the inspected emission-enable implementation affects pool-side injection while retaining participant-side emission; it cannot serve as an owner-controlled miner payout pause. [Storage contract][subtensor-storage]

## Authority and capability matrix

The word “root” identifies three different things here: the Substrate `Root` origin, a netuid-0 validator, and a UR settlement Merkle root. None grants either of the other authorities.

| Action | Required authority | Real limit and admission check |
| --- | --- | --- |
| Inspect finalized chain state | Read access to the owned node | No signer; authenticate chain and block before interpreting storage. |
| Change supported subnet parameters or request trimming | Subnet-owner coldkey, or a genuinely authorized chain `Root` origin | Owner calls have individual permissions, rate limits and administrative windows. A call name beginning with `sudo_` does not itself mean the owner has chain Sudo. |
| Change maximum validator permits or the global subnet-owner cut | Chain `Root` in the inspected implementation | Root-validator registration and the UR owner wallet do not satisfy `ensure_root`. No automatic governance proposal or Sudo attempt. |
| Remove every selected miner | Depends on an actually supported mechanism | No general owner-authorized arbitrary bulk removal has been established. See the reset alternatives below. |
| Register a UR hotkey and acquire stake | Its coldkey or a proven permitted proxy/contract origin | Registration, burn, collateral, pool price, capacity and eligibility are independent checks. |
| Register the root-validator hotkey | Its coldkey through the root registration path | Does not confer administrative authority. The native call's burn-price limitation needs special handling below. |
| Submit UR consensus weights | The registered UR validator hotkey | Correct permit/eligibility, stake, activity, mechanism and CRv4 timing are required. |
| Observe the root dividend basket | Read-only access plus independently approved existing identity, runtime and finite observation policy | The selected passive service has no transaction authority; current seat, ownership, stake, delegation and runtime must match. |
| Trade a root dividend basket | Its coldkey or explicitly authorized current `BasketTrading` proxy | Current price/budget/liquidity/concentration checks apply; no basket-trading adapter is admitted by this passive strategy. [Current source][root-basket-470] |
| Deploy EVM contracts | Dedicated EVM deployment signer | Exact nonce, creation bytecode, constructor data, gas and value envelopes. |
| Govern the coordinator | Approved EVM Safe: 1-of-1 at the SN25 launch, or the 2-of-3 profile | Safe authorization does not authorize native subnet-owner calls. |
| Pause permitted coordinator actions | Configured guardian under contract rules | Cannot claw back reserve principal, rewrite earned claims or pause valid vault claims. |
| Withdraw immutable reserve or upgrade the settlement vault | No such release-1.0 authority | Reject any proposed action requiring this capability. |

The inspected admin implementation permits owner-limited trimming and selected parameters; maximum validators and owner-cut setters require chain `Root`. The emission-enable setter also requires `Root` and is not a percentage setter. [Pinned admin implementation][subtensor-admin]

Every planned transaction records its actual origin: native account or proxy real account, EVM sender, Safe address and threshold, and the exact role it exercises. Prove ownership and proxy filters from chain state. Do not manufacture an authority assumption from possession of a similarly named key file.

## Exact UID census and reset

### What is being reset

Scope is the approved UR mainnet netuid, SN25 (netuid 25), only. Netuid 0 and other subnets are excluded. A UID is a mutable slot, not a permanent miner identity, and a neuron can perform more than one role. “All miners” must become a signed list of **hotkey identities and registration generations**, not a range such as `1..255` or “all UIDs without a validator permit.”

At finalized block `B`, write `census.json` containing every UID and both directions of its UID/hotkey mapping; coldkey ownership; registration block; owner identity; role classification; permits and activity; native and mechanism-specific emission/weights; immune status and expiry; collateral and other locks; stake positions relevant to custody; commitments and associated EVM identity. Include the block hash and runtime identity for every decoded field. Reconcile the complete cardinality against `SubnetworkN`; missing entries or ambiguous ownership block planning.

The signer-free [SN25 census and reset preview](SUBNET-CENSUS.md) now authenticates
one finalized runtime and complete forward/reverse SN25 and root identity maps,
then compares declared protected/removal generations with the inspected owner
trim selection. It does not yet collect collateral, stake, claims, commitments,
EVM associations or every mechanism-specific weight. Even an exact candidate
set keeps `reset_ready=false`: the trim call cannot bind hotkey generations at
execution, and the source-selected owner cooldown is not a metadata constant.
The separate runtime 470 `subnet-discover` command reads a retained unapproved
runtime/finalized snapshot without inventing an approved owner or generation
policy. Its [current-runtime qualification](evidence/runtime470-subnet-discovery-20261001.md)
checks exact official metadata and synthetic state; the output records complete
SN25/root UID membership, observed owner/generation and raw storage while every
seat stays unclassified. `membership_complete` does not mean a complete custody
or reset census. Runtime/source, role and removal approval remain independent,
and its distinct artifact cannot be consumed as a trim policy or execution plan.
The retained public snapshot completed with 256 SN25 and 64 root registrations
in 13.231 seconds using exact-key batches, after per-key reads exhausted their
15-minute windows under HTTP 429. The owner/hotkey details remain restricted;
this membership observation does not close the current custody/role census or
independent approval gates.
The separate signer-free `owner-trim-plan` command ranks bounded owner
capacities against that authenticated census, predicts removed generations and
survivor UID mapping, and names each old miner that would remain. Its
[algorithm and limits](SUBNET-CENSUS.md#best-effort-owner-trim) retain
`reset_ready=false`, `apply_authority=false` and `full_reset_completed=false`.
The [recheck and reconciliation commands](OWNER-TRIM-GUARD.md) now rebuild a
retained plan from its historical authenticated census, refuse drift before a
prospective call, and compare later exact generations, survivors and root
membership. A matching read is not a transaction receipt or execution token;
the reviewed owner call still accepts only netuid and capacity. Any execution
path must explicitly resolve the protected-identity risk between recheck and
inclusion, record the actual receipt and reconcile effects.

Construct disjoint `remove`, `preserve`, and `unresolved` sets. Preserve explicit owner and validator hotkeys, including a validator currently lacking a permit, and any reserve, pool or escrow identity whose existing custody or earned claims require continuity. Membership in both a requested removal scope and a protected custody/validator role is an explicit conflict requiring a reviewed resolution; it is not silently omitted from “all.” Third-party validator identities receive the same explicit classification. Snapshot netuid-0 membership independently to prove it was untouched.

The owner-key launch target is to remove as many approved old miner registration generations as the runtime safely permits, while preserving every identity in `preserve`. The report must show each requested miner as `removed`, `retained_by_runtime`, or `unresolved`, with a verified old-to-new UID mapping for every survivor. A partial trim is a partial native reset, never a claim that all old UIDs were removed. `unresolved` evidence blocks activation; a known retained native registration needs an explicit launch disposition. Invalidate pre-cutover UR bindings and scoring eligibility prospectively while preserving historical claims. After cutover, the standard validator applies the same current admission and proof rules to retained and newly registered miners; the reset does not introduce a permanent hotkey blacklist. If a removed miner later re-registers, record a new generation; never let reuse of the numeric UID satisfy the old identity's postcondition. Registration-open policy and the cutover window must specify whether re-entry is allowed.

UR scoring exclusion cannot erase a retained hotkey's native registration or prevent an independent validator from weighting it. The 10% provider outcome must therefore be checked against the actual post-trim native incentive rows, including every retained old miner. A residual native payout is disclosed as an observed exception; it is not recast as UR provider earnings or a successful full reset.

The majority SN25 validator will run the standard `sn/validator` binary and
its ordinary evidence-based scoring policy. That can help the owner-key reset
only indirectly: old miner generations that are absent from eligible head and
pool evidence receive no positive weight from our validator, which may lower
their observed emissions over future native intervals and make them more likely
to be chosen by the [emission-ranked owner trim][subtensor-uids]. Do not assume
that ownership of the majority seat implies arbitrary zero weights or that an
old miner with valid current evidence will be excluded. The validator cannot deregister
anyone, bypass immunity or minimum capacity, force the other validators' votes,
or grant chain-Root authority. Reobserve finalized emissions and rerun the
complete protected-identity plan before each proposed trim; never assume a
submitted weight row has already changed the chain's trim ordering. The
netuid-0 role observes its distinct dividend basket; runtime470 has no root
weight setter. [Current root strategy](ROOT-PASSIVE-SERVICE.md),
[subnet weight setter][subtensor-weights]

Deletion of a registration does not delete historical events, refund registration cost, erase coldkey assets, or extinguish collateral and claims. Historical UR bindings continue to use their original block-specific mapping. Invalidate or renew only future bindings that reference displaced UID generations; preserve proof and claim history.

### Supported paths and their limits

| Mode | What it accomplishes | Condition for selection |
| --- | --- | --- |
| `owner-trim` | Lowers capacity, removes runtime-selected low emitters and compresses surviving UIDs. | This is the preferred owner-key path. A pinned simulation and execution-time guard must prove no protected identity can be removed. Record the exact removals and surviving old miners; an incomplete removal set remains an explicit launch exception, not a full reset. |
| `bounded-replacement` | New registrations replace runtime-selected existing neurons as capacity fills. | A finite, budgeted sequence proves every intended replacement and no protected loss, including competing registrations and changed pruning inputs. If the runtime cannot enforce the approved selection at execution, do not automate the destructive sequence. |
| `new-subnet` | Starts a separate metagraph and contract deployment on a new netuid. | Explicitly selected alternative with its own subnet-registration allowance and migration plan. It leaves the old subnet and its registrations in existence; it is not a reset of that subnet. |
| `chain-root-migration` | Can implement the literal removal policy if the chain's authorized governance adopts a suitable migration. | Separately reviewed runtime/call and authentic governance execution. The bootstrap verifies its finalized result; it never pretends the UR owner can grant itself this authority. |
| `ur-generation-only` | Resets UR application admission/scoring/bindings prospectively. | Explicitly accepted narrower outcome. It makes no claim to remove native UIDs. |

The inspected trim implementation enforces minimum and maximum capacity, protects owner-immune and temporarily immune entries, and requires the immune percentage to remain strictly below its runtime threshold. It removes according to emission rank and migrates the survivors' slot-indexed state. The ordinary `set_max_allowed_uids` path cannot set capacity below the occupied count. [Trim implementation][subtensor-uids], [capacity documentation][max-uids]

At the reviewed runtime, `sudo_set_network_registration_allowed` and the
per-block registration limit require chain `Root`; the SN25 owner cannot assume
it can close registration for a trim window. Even an already closed
registration flag does not prevent a coldkey-authorized hotkey swap from
changing the registered generation. A bounded owner-key execution path must
therefore establish its protection invariant under actual registration and
swap/custody behavior through inclusion, or leave the trim as a read-only
proposal. [Registration setter](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L728), [hotkey swap](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).

Ordinary registration pruning is a different algorithm. In the inspected source it excludes owner-protected identities but can fall back to temporally immune candidates in some capacity conditions. Do not use the trim immunity model to justify a replacement sequence. `clear_neuron` is an internal implementation routine, not evidence of an owner-callable reset endpoint. [Registration implementation][subtensor-registration]

The reset planner must derive the live minimum, timing restrictions, immunity threshold and pruning order, including ties, rather than use the documentation's nominal 64-slot minimum or 30-day trim interval as constants. Select the most effective admissible owner trim by *safe removal of approved old miners*, not merely by the smallest UID count; keep all protected identities and native custody rights intact. An external actor can change the candidate set between preview and execution. A fresh preflight reduces that race but does not eliminate it; destructive automatic apply requires an execution-time guard or a demonstrated invariant under all allowed intervening changes. Otherwise export the unsupported action and report `RESET_CAPABILITY_BLOCKED`. Owner keys are the only available native administrative authority for this launch; do not plan chain-Root calls or imply that a netuid-0 validator seat supplies them.

A narrower owner-key path may be admissible without an atomic hotkey predicate:
prove that every generation the runtime could remove before the submitted call
expires is an explicitly approved old miner, while every protected identity
remains immune throughout that window. Then changes to emission ordering can
change *which approved old miners* are removed, but cannot remove a protected
identity. The proof must cover the entire mortal transaction window, including
earlier same-block actions: registration, hotkey swaps, temporary-immunity
expiry, owner/immune status, native epoch updates and capacity limits. An
already closed registration flag is insufficient if a subnet-owner takeover
can register a neuron directly during an epoch; rule that path out or choose a
window before the next epoch. First hotkey swaps have no cooldown, so a
cooldown proof needs an authenticated nonzero last-swap value for each relevant
coldkey or a concrete custody fence. Majority-validator weights alone do not
establish any of these conditions. Reconcile actual removals from the
finalized receipt rather than treating the preview's predicted list as the
result. [Epoch owner takeover](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs#L389), [hotkey swap](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).

The signer-free [`owner-trim-qualify`](OWNER-TRIM-BOUNDED.md) now tests a
conservative version of this condition from exact-block RPC evidence: protected
immunity through a proposed 4–256-block mortal era, closed registration,
authenticated hotkey cooldowns and coldkey-swap delays/announcements, no native
epoch or admin-window closure, no lease, and the runtime capacity/immune-ratio
limits. Approved miners may lose immunity without expanding the approved set.
Every requested generation remains explicit, including unresolved originals
and conditional residuals. Passing predicates remain conditional on unproved
owner/proxy/pending-action, governance/runtime and public subnet-pruning/reuse
fences; source-to-Wasm,
custody, actual signed mortality, receipt and subset reconciliation still block
execution. Exit 0 is evidence only; apply/reset/full-reset remain false.

A separate [best-effort policy domain](OWNER-TRIM-BEST-EFFORT.md) can submit only
an original offline owner-signed action after independent acceptance of the exact
unenforceable governance, protected-generation selection, inclusion-fee and
custody residuals. Public subnet pruning/reuse and competing registration/re-entry
each require their own explicit signed option. Current observed drift still
blocks submission; these choices do not establish the strict invariant above.
Canonical dispatch and before/after correspondence retain every unresolved
old-miner disposition and never authorize validator activation or claim a full reset.

After a finalized reset, repeat the entire census and compare identities, not just counts. Reconcile commitments, balances and locks separately. Re-establish approved capacity and permitted registration settings before new pool/head registrations; no “temporary” parameter change may remain unreported. Existing settlement/claim service must stay available throughout any migration.

## Ten percent of native miner allocation

### Denominator and accounting

Use alpha's atomic units for reward accounting, distinct from TAO rao and EVM wei. The reference denominator is the UR subnet's **native miner tranche before the chosen 90% withholding mechanism**, over explicitly identified native emission intervals after activation. It excludes the owner cut, validator/root dividends, TAO pool injection, deposits, reserve principal, and collateral principal. A miner's newly earned reward captured into collateral still counts as that miner's economic reward; it is not a way to hide payments from the 10% calculation.

Let `M_i` be the authenticated miner allocation for native interval `i`, using the exact runtime's accumulation, mechanism split, drain and rounding rules. Let `M_total(k) = sum(M_i, i <= k)`. Define the integer reference for the cumulative provider target:

```text
P_reference(k) = floor(M_total(k) / 10)
P_interval(k) = P_reference(k) - P_reference(k - 1)
remainder_reference(k) = M_total(k) - P_reference(k)
```

This conserves reference rounding across intervals; independent floating-point `0.1 * amount` calculations are prohibited. Use checked integer/rational arithmetic. Record `Q(k)`, the absolute alpha-unit tolerance derived from the selected runtime's actual u16, fixed-point and per-recipient rounding over the observation window. The requested target is met when actual provider entitlement differs from `P_reference(k)` by no more than that explicitly reviewed tolerance. Do not invent a large percentage allowance for consensus disagreement or require zero dust when the chain cannot represent the fraction exactly. Track native truncation dust separately from the policy remainder. Allocation at a later UR settlement epoch must reference its underlying native intervals exactly once. Align activation to a proven drain boundary with no unexplained pre-activation `PendingServerEmission`; never relabel old rewards as a new 10% budget.

Under the nominal 18% owner / 41% miner / 41% validator split, this request corresponds to approximately **4.1% of participant-side subnet emission**. The actual owner-cut fraction and fixed-point rounding must be read and reproduced, not hardcoded as exactly 41%. The inspected implementation accumulates a miner half after the owner cut, and zero incentive can redirect the miner allocation to validators. [Participant distribution implementation][subtensor-coinbase]

The existing UR `theta` divides provider distribution between direct head miners and tail pools. Once the 10% mechanism is selected, construct reference allocations `H = floor(theta * P_reference)` and `T = P_reference - H`, then reconcile their actual native outcomes against the approved quantization tolerance. A separately selected hard-cap mode additionally requires actual cumulative entitlement not to exceed its signed ceiling. Changing theta alone leaves the native miner pot intact; row normalization also makes multiplying every validator weight by 0.1 ineffective. [UR steering specification](../WHITEPAPER.md), [existing steering tests](../validator/steer_test.go)

Count entitlement once: native rewards to provider-owned head coldkeys, and provider entitlement funded through tail pools, are the two payment channels. A tail capture followed by a claim is one reward, not two. Target measurement, and any separately selected cap, covers earned entitlement and locked miner reward, not just liquid claims completed so far. Existing valid claims retain their original terms.

### Selected owner-recycle policy for the other 90%

| Policy | Feasibility and consequences | Design disposition |
| --- | --- | --- |
| `owner-recycle` | If final Yuma incentive is directed to the runtime-recognized owner-hotkey set under recycle mode, that portion is recycled. It is not placed in our reserve or retained as a future provider claim. | **Selected by the user, 2026-09-27.** Implement an explicit mainnet policy successor and prove the actual 10% provider / 90% recycle outcome within the approved runtime tolerance. |
| `owner-burn` | The same owner-directed withholding path can burn instead. Burn/recycle supply effects differ. | Not selected; verify the actual on-chain mode is recycle. |
| `reserve-custody` | Routes the remainder into an explicitly defined reserve position, with separately enforced accounting and no provider claim on it. | Not selected. The current vault has no “send 90% to reserve” operation; owner-recycle provides no reserve credit. |
| `deferred-provider-liability` | Pays 10% now while preserving 90% as future provider claims. | Not selected; recycling does not create a deferred provider liability. |
| `native-runtime-cap` | A runtime-enforced miner sub-allocation could provide the strongest whole-subnet boundary. | Not the selected mechanism. No suitable owner-callable primitive was established in the inspected source. |

The existing implementation withholds owner-directed incentive in both burn and recycle modes and records the withheld ratio. Choosing recycle does not avoid that accounting. [Owner-directed distribution][subtensor-coinbase] In the pinned source, recycling decrements `SubnetAlphaOut` and calls the alpha-asset recycle operation; burning calls its distinct burn operation without that decrement. Neither operation credits the owner wallet or our vault. [Alpha accounting][subtensor-alpha-accounting] The withheld ratio reduces the subnet's demand share before the emission gate; a 90% withholding policy is economically consequential and does not imply an exact 90% reduction in final TAO allocation after renormalization and gating. [Allocation formula][subtensor-shares]

The inspected runtime defaults `RecycleOrBurn` to **Burn**. Its `AdminUtils.sudo_set_recycle_or_burn(netuid, Recycle)` call accepts the subnet owner or chain root, subject to the runtime's owner rate limit and admin window; the EVM alpha precompile exposes `setRecycleOrBurn(uint16,uint8)` with mode `1` for recycle. These are alternatives for one reviewed operation, not two changes to submit. The bootstrap must read the exact runtime metadata and current finalized storage, select the authorized route, set recycle if needed, and verify the finalized `RecycleOrBurn[25] == Recycle` value before activating any owner-directed 90% weight proposal. A pending call, a failed call, or the default Burn value is not recycle. Recheck the mode at every activation/recovery boundary and alert on drift. [Admin setter][subtensor-admin], [default storage][subtensor-storage], [EVM precompile][subtensor-alpha-precompile]

The [runtime 470 owner transition](OWNER-RECYCLE-TRANSITION.md) now supplies a
separate offline approval/action domain, exact direct-owner call 80, immutable
native/Ledger request and public signature custody, and bounded canonical
receipt/readback recovery. Its planner checks the complete original era against
the per-subnet hyperparameter 24 rate limit and state-based admin window. The
public path neither signs nor broadcasts. Absent/default Burn was still observed
at finalized block 9,187,604; actual owner/device approval, transition and Recycle
readback remain open. A successful mode transition alone never establishes the
10% provider allocation or economic activation, and this source requires a
successor to the retained SN `6c801a25` / server `720e7c61` release baseline.
The implementation is merged at `233ea2be`; [sealed qualification](OWNER-RECYCLE-TRANSITION.md#qualification-scope)
records 29 author and 24 independent Sol roots passing normal/race, zero skips,
and mainnet vet passing on the exact `b3880266` source. This is offline source
qualification; the live and economic gates above remain open.

The [October 2 recycle custody correction](evidence/owner-recycle-custody-20261002.md)
at source `25aa1515` additionally binds signing handoffs and retained receipts to
the original physical marker, private directory and preceding journal. A
detached marker, deleted completed state or active-owner rollback cannot return
a request or finalized mode success. Original nonce/signature recovery and
unused interrupted claims remain supported. Qualification of this later source
records separate author and independent 23-root normal/race passes, package vet
and seven causal baseline failures in each run, preserving the earlier receipts
and frozen release. Actual owner/device authority, bounded transmission and
native 10/90 evidence remain open.

The [October 2 native-context and scheduling corrections](evidence/native-economics-context-and-schedule-20261002.md)
retain separate source pins `dd21ed00` and `258e25b4`. The observer distinguishes
tempo anchor resets from consumed epochs and accepts a source-authenticated
owner takeover that appends one UID before Yuma, while preserving unknown
recipient generations and economic amounts. Fresh production CRv4 preparation
requires an independently signed tempo-drift profile, exact runtime/source
authority and a private producer capability; it follows v470's strict
`BlocksSinceLastStep > tempo` predicate and post-initialization commit phase.
Old approvals and pending bytes keep their original timing interpretation.
Monitor policy explicitly selects the corresponding forecast; historical
misses still require an observed epoch crossing. These later sources need their
own qualified release. They supply no live approval, denominator `M`, tolerance
`Q`, finalized 10/90 outcome or service activation; MG-06 remains open.

Release 1.0 explicitly rejected owner-directed burning as its head/tail steering strategy. The selected owner-recycle launch policy must therefore be encoded as an explicit economic-policy successor, with its activation and accounting independently verified. Preserve the independent-validator objective and signed weight caps: do not raise a cap, create arbitrary owner recipients, or displace validators merely to force a 90% weight destination. [Whitepaper, head/tail decision](../WHITEPAPER.md#138-headtail-split-θ-in-one-mechanism-chosen-not-two-mechanisms-not-owner-burn)

A weight proposal is not an enforceable payout fraction. Independent validator weights, Yuma clipping, bonds, activity, permits, normalization and u16 rounding affect final incentive. For either owner-withholding path, qualify the complete runtime outcome against the admitted validator set and review adjacent/adversarial weight states. The draft assurance mode is `observed-native-target`: demonstrate the actual 10% allocation within `Q(k)`, disclose sensitivity to other validators, and monitor subsequent deviation. It does not promise that other validators can never change the outcome. If a stronger `enforced-cap` mode is selected, prove that ceiling under all admitted conditions or report `EMISSION_CAP_UNENFORCEABLE`; an after-the-fact monitor is not enforcement. Halting our validator does not revoke other validators' weights or stop already queued native emission. [Consensus implementation][subtensor-epoch]

Claim “started at 10%” only after the observed native outcomes meet the target and its runtime-derived tolerance. Report deviations and dust explicitly; a low payout below a hard ceiling alone does not establish that the target was reached. If only a proposed weight target can be shown and the actual outcome cannot yet be established, report `ACTIVATED_AWAITING_EMISSION_OBSERVATION`. A material miss is not relabeled as quantization. Future changes to the target or assurance level require a new reviewed policy.

### Immutable vault implications

[STSettlementVault](../evm/src/STSettlementVault.sol) captures the eligible pool emission through the configured staking precompile and keeps immutable claim accounting. It has no upgrade or treasury sweep. Its conservation checks distinguish total captured, total paid, escrow accounting, pending funding and outstanding liabilities. Publishing payout roots at 10% while leaving 90% in this vault does not make the remainder an owner reserve or erase its accounting obligations.

The [claim-recovery correction](../evm/CLAIM-RECOVERY.md) keeps an accepted
Merkle leaf and provider credit if an exact runtime transfer or balance-delta
check fails. The attempted payment is isolated in a vault self-call so its
state can roll back without rolling back claim acceptance. This changes the
non-upgradeable vault's bytecode and requires a new deployment artifact and
an authenticated runtime rehearsal of nested EVM/native rollback. It does not
repair an already deployed vault or loosen exact payout accounting.

A reserve design must specify a different enforceable custody path before deployment: who owns each head and pool registration, which precompile call transfers each tranche, which principal and collateral are excluded, who can authorize the transfer, and how cumulative provider liabilities are bounded. Moving all head miners behind a new contract would change release 1.0's provider-owned direct-payment model. Such a design requires its own contract/policy qualification and migration plan; a coordinator upgrade cannot retrofit withdrawal authority into an old vault.

Implement the selected owner-recycle path against the exact runtime: authenticate the recognized owner-hotkey set and recycle mode, preserve the signed cap and validator independence, and prove the finalized 10% provider / 90% recycle split with exact interval accounting. Keep the recycle fraction separate from native rounding and supply-side effects. No reserve transfer or new reserve-custody contract is implied. Do not deploy an immutable contract set before the selected path and custody layout are qualified.

The [signer-free successor preview](../validator/OWNER-RECYCLE-PLANNER.md)
(`9a287d92`) is an implementation step, not activation. It binds a proposed
successor to the unchanged parent policy, builds the provider tenth using the
existing head/tail theta, gives the remainder equally to usable recognized
owner UIDs, and refuses rows that violate the signed cap before or after u16
quantization. With the current `32768/65535` per-recipient cap, a single
owner destination cannot carry 90%; at least two usable registered owner
hotkeys are necessary. The pinned source permits multiple hotkeys of the
subnet-owner coldkey and recognizes their incentive for recycling, but their
mainnet registration, masks and protection are unknown. The preview always
refuses submission and reports its row as a proposed weight fraction; an
authenticated owner census, signed policy transition and measured native
Yuma/emission outcome remain separate gates.

The [signed successor admission](../validator/OWNER-RECYCLE-ADMISSION.md)
now verifies an independently pinned approval, immutable local custody and an
exact finalized runtime/owner census under explicit Recycle mode. It does not
start steering: both submission paths remain fenced until measurement,
signatures, pending intents, archive replay, coordinator/client-key policy and
the native drain boundary use one successor authority. The distinct
[schema-3 production path](OWNER-RECYCLE-PRODUCTION.md) now joins those inputs
through the standard V2 producer, exact prepared source/row, a separate hotkey
sidecar and durable intent/archive replay. Old observation approvals stay
fenced. Original pending receipts can still be reconciled. An admitted 10/90 weight row remains a
proposal until independent validators and finalized native allocation prove
the economic result.

Policy rollover must retain the previous epoch's signed payout and its actual
coordinator policy/window. The [operator epoch-policy correction](evidence/operator-policy-custody-qualification-20261001.md)
allows successor deposit sizing after restart without the original policy file:
it authenticates the current deployment, reads immutable historical `policyAt`
at that canonical hash and verifies both finalized boundaries. Fresh payout
issuance refuses a configuration that does not match the requested epoch.
Local two-operator migration, processed registration, fresh proof and deposit
tests do not establish live readiness. Preserve exact old signature bytes and
complete every operator's future-boundary cutover before activation.

Settlement admission also requires the compatible server custody reader and
migrations through 728. The [retained timestamp correction](PRELAUNCH-FIXES.md)
prevents historical terminal NULL close times from disappearing out of every
epoch: it blocks new payout construction before chain reads and retains the
same debt after archive cleanup. Record the exact historical NULL census before
coordinated reader/writer/reaper cutover. Unknown timestamps are unresolved
debt, never guessed epoch assignments or automatic zero-credit exclusions.
Previously retained artifacts stay immutable and are not certified complete by
this prospective fix. NetEscrow writer drain, revision/fence restore policy and
archive capacity remain separate production gates.

The [atomic payer admission candidate](PRELAUNCH-FIXES.md), composed server
`b6f49bdb`, removes Redis from
credit authorization: both origin and companion creation lock payer balances
and read durable reservations after the lock, while settlement commits the
consumed-byte debit with the terminal outcome. Retain its causal evidence and
deploy it only after draining all old creators and asynchronous debit posts.
The same release must use checked settlement arithmetic: an overflow-safe
bilateral mean and negative-grant refusal prevent malformed reports/history
from claiming an outcome without the corresponding consumed-byte debit.
An old terminal outcome or settled escrow marker cannot establish whether a
historical debit post ran; independently reconcile exact historical financial
state before activation. This does not automatically repair old balances or
make participant sweep/account payout posts atomic or replay-safe. Their
durability, full conservation and lock-contention capacity remain open gates;
the migration catalog through 728 is unchanged by this code slice.
In particular, the terminal settlement can commit before its in-memory
participant-sweep post creates `transfer_escrow_sweep` rows consumed by the
payment planner. A lost post can omit a provider payment; terminal-outcome
replay alone cannot recreate it. Qualify a durable correction with causal
lost-post, rollback and replay controls before activation.

## Contract deployment, custody and initialization

Reuse the release-1.0 contracts and reviewed ABI/artifact generation, with mainnet-specific inputs. The existing [Deploy script](../evm/script/Deploy.s.sol) is the ordering reference, not a command the bootstrap blindly shells out to. The Go planner must build exact transaction payloads and independently read back their results.

Prepare against authenticated pinned observations and tolerate ordinary
finalized-head advancement during the operation. Refresh affected current
authority, nonce and custody inputs without discarding the original signed
transaction or completed phase. A failed mapping read remains unavailable
evidence; report a mapping contradiction only after receiving a conflicting
value. Keep retries within the approved operation budget and retain unresolved
signed liabilities for reconciliation.

The [executable contract phase](BOOTSTRAP-CONTRACTS.md) wires
`bootstrap-contracts preview/plan/apply/resume` to exact reserve CREATE preparation,
public signed-byte custody, a finite owned-HTTP submission allowance and
canonical transaction/runtime/getter recovery. The next selected actions now
implement predecessor-bound settlement vault CREATE, coordinator
implementation CREATE, escrow registration and atomic initialized proxy
CREATE under the same approved graph and finite attempts.
Offline preparation requires no deployment outputs.
The [reserve qualification](evidence/bootstrap-contract-qualification-20260928.md)
and [vault qualification](evidence/bootstrap-contract-vault-qualification-20260928.md)
retain their distinct source graphs and test scopes; the vault candidate passed
51 focused roots normal/race and 426 full normal mainnet roots. The
[coordinator qualification](evidence/bootstrap-contract-coordinator-qualification-20260928.md)
passed 71 focused normal roots, both exact race shards and 446 full normal
mainnet roots. [Escrow qualification](evidence/bootstrap-contract-escrow-qualification-20260928.md)
passed 95 selected roots normal/race and 470 full normal roots; live native
burn/refund behavior remains unmeasured. [Proxy qualification](evidence/bootstrap-contract-proxy-qualification-20260928.md)
passed 120 selected roots normal/race and 495 full normal roots. Genuine Safe
evidence anchoring and authenticated live authority remain required. The
[scoped graph qualification](evidence/bootstrap-contract-plan-graph-qualification-20260929.md)
passes five graph, 28 evidence and 177 adjacent roots normally and under race,
with six causal controls. The broader `./mainnet` normal package stopped at its
60-minute window after 299 passing roots and zero assertions; it is incomplete.
The separate [nine-action candidate status](evidence/contract-graph-review-block-20260928.md)
records successful complete-command diagnostics and an unresolved failure test.
Automatic review blocked that test correction; the candidate is not admitted
as the executable production release.
Unsigned `preview` exports the exact independently signable approval bytes after
local structural/artifact review without opening custody or a network route.
Signed commands retain the approval check. Offline reopen preserves completed
receipts as explicitly retained observations, not fresh chain audits; these
[follow-up command regressions passed in both modes](evidence/bootstrap-contract-qualification-20260928.md).

| Identity | Custody/authority |
| --- | --- |
| Subnet owner coldkey | Owners report that the existing account is Ledger-derived. They keep the key on their own Ledger with the Polkadot generic app, have no Snow access, and run the [qualified offline owner-side signing handoff](OWNER-SIGNING.md) on their own device. Snow receives only an exact signed reply for verification, retention and submission. A [pinned Linux SDK artifact](evidence/owner-ledger-native-sdk-qualification-20260930.md) has synthetic-device qualification; the owners' actual platform, physical device, on-chain account, metadata digest and live call still need verification. |
| EVM deployer | Limited bootstrap gas/value; no ongoing governance custody. `0xA9D4A6a331F59942BD7389a5402120D69047C090` on Brien's Ledger (Ethereum app). |
| Coordinator owner | Actual Safe with its approved owners and threshold. The launch Safe is SafeL2 1.4.1 `0x56F4Dad575576CC0B679FEf52899630f9F605418`, 1-of-1, owner `0x16C372dbBb24cd8473345ab13971E40814C8658F` on Brien's Ledger (owner decision, October 7); owners can be added and the threshold raised later by a Safe transaction. The tooling also keeps the 2-of-3, three-owner profile. |
| Guardian | Separate limited operational authority. `0x450C14EA62F76F11630780e194E01F9f524BAbFd` on Brien's Ledger (Ethereum app). |
| Commitment oracle | Separate reviewed signer/service with original and any scheduled route authenticated. Its key `0x56Ddfb8f3E267E98EfDa690110645f31365BCF03` is a service key in `vault/main/sn.yml`; no mainnet oracle service exists yet. |
| Root validator coldkey/hotkey | Existing root hotkey hardware custody and the actual root-owning/staker coldkeys or allowed proxies must be identified independently. The passive observer uses public identity and independent config/host approvals, without a native signer. Reviewed v470 accumulation needs no periodic hotkey signature; registration, claims, stake and basket actions use their specific coldkey/proxy authority. Neither root key role inherits the subnet owners' Ledger or approval. Actual device/API and live participation remain unverified. |
| UR validator hotkey and stake coldkey | UR scoring; may be the reviewed reserve target when explicitly selected. |
| Operator demand deposit signer | Each operator keeps its own EVM signing key in its own secrets vault. The coordinator binds that address to its `noId` and deposit hotkey for the active epoch. Owner Ledger and the SN bootstrap never load operator deposit keys; this secrets vault is distinct from the on-chain settlement vault. The [qualified worker custody check](evidence/operator-deposit-custody-qualification-20260930.md) still needs real wallet and coordinator verification. |
| Vault mapped coldkey | Immutable tail-pool and escrow custody. No human holds its private key. |
| Reserve mapped coldkey | Permanent reserve stake under the immutable sink. |

Role equivalence must be deliberate. In particular, naming a UR validator “owner validator” does not prove that it is the native `SubnetOwnerHotkey`. The planner checks the actual mapping and does not assume UID 0. Require the existing deployer's distinct owner/guardian/oracle constraints. Inspect Safe singleton bytecode, owners, threshold, enabled modules, guards, fallback handler and pending transactions; an address merely implementing `getOwners` and `getThreshold` is insufficient.

Freeze compiler and dependency versions, creation and deployed bytecode,
link/immutable locations, constructor encodings, source identities and storage
layout. The [current size check](evidence/contract-size-candidate-20260927.md)
puts `STCoordinator` at 24,564 runtime bytes, just **12 bytes** below Foundry's
24,576-byte limit; any source or build-input change requires a fresh size and
exact-artifact deployment rehearsal. The selected live runtime's code-size rule
still needs authentication. For a dedicated deployer starting at nonce `n`, the
existing core sequence is:

| Nonce | Action | Required postcondition before its dependants |
| --- | --- | --- |
| `n` | CREATE `STReserveSink` | Exact predicted address, bytecode, netuid, reserve hotkey, bootstrap, mapped coldkey. |
| `n+1` | CREATE `STSettlementVault` | Exact custody identities, claim horizon, minimum transfer and bootstrap. |
| `n+2` | CREATE `STCoordinator` implementation | Exact implementation bytecode; implementation initializer disabled. |
| `n+3` | `vault.registerEscrow(maxBurnRao)` | Escrow registration under the vault's mapped coldkey, correct UID/ownership and bounded debit/refund. |
| `n+4` | CREATE ERC1967 proxy with initialization calldata | Initialization occurs in the constructor; approved Safe, guardian, oracle, custody links and initial policy are set atomically. |
| `n+5` | `reserve.setRecorderOnce(proxy)` | Exact one-shot recorder. |
| `n+6` | `vault.setCoordinatorOnce(proxy)` | Exact one-shot coordinator. |
| `n+7` | CREATE `STValidatorEvidence` | Exact approved deployment domain, coordinator/vault identities, predicted address and immutable getter readback; creation leaves the evidence unanchored. |

The escrow registration deliberately consumes a nonce before proxy creation. Predict all addresses before construction because the mapped coldkeys are immutable. Native rao-to-EVM-value conversion uses `1 rao = 10^9 wei` here; bounds and conversions must reject overflow. Authenticate `blake2_256("evm:" || H160)` against the runtime mapping before custody is funded.

The first eight actions through `n+7` have an offline executable path with complete scoped normal and race qualification; no live authority or installation is established. Anchor [STValidatorEvidence](../evm/src/STValidatorEvidence.sol) as a separately planned subsequent action with its genesis/deployment domain and coordinator/vault identities. The current release uses the coordinator's one-shot `fixValidatorEvidence`; an existing foreign anchor is a hard conflict. The Safe's inner nonce and a relayer's outer EVM nonce are distinct from the deployer CREATE graph and each need independently retained custody and canonical postconditions. Use the fresh mainnet nonce graph, not the sim-testnet graph's extra upgrade, fleet-helper or adversarial contracts. Mainnet artifacts must not include those test fixtures by default. [Evidence deployment reference](../sim-testnet/evidence_deployment.go), [readback reference](../sim-testnet/evidence_deployment_runtime.go)

Only then register the approved operator pool hotkeys under vault custody, establish reserve-target eligibility, activate evidence identities and future bindings, and fund reviewed stake/deposit positions. Provider-owned head miners register through their own authorized identities; bootstrap cannot sign for unrelated miners. Every registration is present in the spend/count plan. Reconcile pool/escrow collateral and minimum-transfer semantics before the first production capture; immutable custody must not become stranded by an unqualified runtime change.

Use the actual [mainnet policy validation](../protocol/policy.go): a UR settlement epoch is **50,400 native blocks**, with the reviewed production root-commit/finalization/close windows and claim retention. The deploy script's mainnet reference windows are 1,200 / 14,400 / 120 blocks and 8 claim epochs plus 1 grace epoch. Encode all fields explicitly in the signed mainnet policy; do not inherit accelerated 300- or 360-block testnet settings. Mainnet economic caps, deposit tiers, theta, minimum operator/validator counts and binding horizons need independent review.

The [steady cadence candidate](evidence/mainnet-steady-cadence-candidate-20260928.md)
represents mainnet from epoch zero with `after_accelerated_epochs: 0` and identical
initial/production epoch, root-commit, finalization and close windows. Both epochs
must contain exactly 50,400 blocks. The installer separately requires
`effective_epoch: 0` for its initial complete approved policy; a later approved
steady policy does not invent an accelerated period. Existing positive-count
accelerated policies, including historical mainnet policies, retain their exact
bytes and meaning. Testnet still requires its existing positive-count transition.
The two validator snapshot consumers need no relaxation: they compare the actual
pinned snapshot against that representable canonical policy. The signed public
config loader and both readers are covered by 24 selected protocol/validator
tests passing normally and with race detection, plus vet. Three causal controls
each reproduce their intended failure in normal and race execution. Composed
installer qualification remains pending; the component receipt records its
dependency scope and one missing causal-wrapper exit. Generic Solidity window
checks alone do not establish composed launch acceptance.

Preserve the current guarantees: the coordinator owns neither custody position, the sink has no outbound path, and valid earned vault claims survive coordinator pause or upgrade. Pausing new application activity is not a native emission kill switch. Initial contracts establish their epoch clock at deployment, so the plan must include sufficient time to finish setup and a future activation boundary; it cannot assume a dormant deployment has no running clock.

## Running the validators

**October 7 decision:** SN25 launches with one UR validator, `ur-mainnet`, whose hotkey also holds the root seat
([decision](#one-operator-and-one-validator--october-7)). Its preparation is a one-role bootstrap v5, and it runs as
`validator run` through xops on snow ([LAUNCH.md](LAUNCH.md#run-it-on-snow)). The two-unit `activate-validators`
component below refuses v5 and is not the launch path; it remains for two-validator deployments.

The [initial two-UR installation component](VALIDATOR-ACTIVATION.md) now provides
a concrete `activate-validators` command for exact static-unit installation,
role-group-readable runtime copies of the original signed configs, current
bootstrap admission and durable per-unit start/recovery. Its [scoped independent
qualification](evidence/validator-activation-qualification-20260930.md) is sealed:
88 positive root executions pass normal/race, and twelve normal plus five
selected race controls are causal. **No deployment was performed.** It keeps
bootstrap v3 role/generation
and producer approvals, both current permits and original custody separate from
process authority. The current source includes an actual current-authority
adapter in [validator_activation_command.go](validator_activation_command.go):
`admit-current` and `start` can construct it only with a separate exact
`--current-approval`, accepted hash and distinct independent approval key.
This is implemented command wiring, not an absent adapter or an unconditional
public-start refusal. The selected release still needs complete qualification
and authentic current checkpoint, operator, contract, custody, stake and signed
start acceptance. No live starts are established. A systemd acknowledgement or
progress file does not prove weights or the 10/90 outcome. Actual root
participation and any necessary current native lifecycle actions remain separate
from UR producer starts.

The [qualified native prerequisite reader](evidence/validator-native-admission-qualification-20260930.md)
now authenticates the original signed runtime at both current and activation
checkpoint hashes, exact native epoch and drain facts, current generation,
owner, activity and explicit Recycle. Its canonical anchors and sample age are
rechecked before admission. It does not supply the remaining operator,
contract, signer-custody or effective-majority authority. Those facts must be
provided and checked by the separate current-admission/start path.

The [qualified `admit-evidence` increment](evidence/validator-current-evidence-qualification-20260930.md)
now reads the original deployed contract graph and both operators' signed
activation/client-key evidence under bounded read-only RPC/API transport. It
records a partial projection without granting start authority. Full producer
proof history and worker health, deployment provenance, global signer custody
and effective majority remain separate launch gates.

The [qualified `admit-stake` command](evidence/validator-stake-capacity-qualification-20260930.md)
observes the original roles' complete native stake census and computes a
conservative capacity lower bound under the pinned runtime's threshold,
normalization, permit and activity rules. It reports capacity and current
activity separately. It cannot prove applied weight influence or grant public
start authority; actual majority behavior must be observed after launch.

The [qualified `admit-health` increment](evidence/validator-proof-health-qualification-20260930.md)
replays each role's original pinned operator activation prefix and records
protected standard-validator progress as a separate health signal. Completed
proof checkpoints remain durable across a later read failure. It leaves the
current mutable proof namespace, live per-operator worker attestation and
global signer custody as explicit gates; a missing heartbeat is not treated
as corrupted proof history.

The [qualified `admit-committed` continuation](evidence/validator-committed-prefix-qualification-20260930.md)
also replays the service UID's current committed control history from both
original operator origins and preserves each completed checkpoint. This is a
read-only subgate. Unsealed ledger/intent state, live worker attestation,
global signer custody, applied weights influence and signed launch authority
remain mandatory before public start.

The [qualified bounded unsealed inventory](evidence/validator-unsealed-inventory-qualification-20261001.md)
adds service-owned signed ledger tails, unfinished trails and the empty intent
boundary to durable read-only custody. The [qualified canonical tail-boundary
extension](evidence/validator-tail-boundary-qualification-20261001.md) also
authenticates every signed tail's bounded EVM epoch/policy/eligibility boundary.
Nonempty intent graphs, historical provider bindings and the other launch gates
remain open.

### Root validator on netuid 0

**October 6 decision:** our validator hotkey runs on root and validates SN25 at an 18% delegate take and an 18% SN25 childkey take, and accepts child hotkeys from anyone; see [the decision record](#root-validator-decision-and-take-checkpoint--october-6). The passive observation path below remains the record of the earlier strategy.

The [runtime470 source/artifact review](../docs/spec/runtime-470-audit.md) and
[passive root service](ROOT-PASSIVE-SERVICE.md) define an implemented observation
path: fresh bootstrap schema v4 with two independently approved UR production
configs, or v5 with one (the SN25 launch), and a separately approved existing
netuid-0 role using `passive_accumulate_in_place`. This observer does not
complete the requested actual root participation/earnings or authority for necessary coldkey lifecycle actions. At reviewed source
`b4662ed8`, [root_service_command.go](root_service_command.go) returns
`activation-blocked` from `activate`; [root_service_runtime.go](root_service_runtime.go)
constructs observation/reconciliation and offline custody ports, leaving native
`Authority` and `Submitter` absent. This is a limitation of the legacy mutation
service, not a requirement to restore periodic signing or removed root-weight
calls. Assess actual seat, root stake, delegate/child behavior and earnings under
an independently approved current runtime. Any needed registration, stake,
claim or basket action requires its actual owning/staker coldkey or allowed
proxy, separately approved custody, transport and qualification.

The passive policy binds genesis/full runtime,
source/code/metadata, hotkey/coldkey, seat generation, minimum stake, delegate
take, existing delegation, route, private checkpoint and finite observation
window/cadence. The real bounded `root-passive-service` command reuses the root
monitor after verifying the independent config signature and completed original
preparation. It has no native signing/submission path or heartbeat transaction.
Its `ready` result is observation readiness and `activation_ready` stays false.
The separately signed [`activate-root-passive` host owner](ROOT-PASSIVE-SERVICE.md#independently-approved-static-host-owner)
now provides exact sandboxed static installation and one durable process start,
with invocation recovery and a dedicated writable checkpoint directory. It keeps
the original v4 or v5 private approvals unchanged and consumes no UR start allowance.
Historical status and manager liveness do not prove continuing observer health.
Its [qualification](evidence/passive-root-host-20261001.md) supplies no live host
approval or deployment; actual current seat/stake, independent runtime authority,
the exact host signature/acceptance and live monitor evidence remain launch gates.
The earlier SN `6c801a25` release excludes this source successor. The reviewed v470 runtime has no `set_root_weights`; that rules out the
retired root-weight action for this passive strategy. It does not provide the
actual current root participation or authority for necessary lifecycle actions. [Removal][root-removal-470]

Retain the historical v3 `explicit_root_weights` action/custody capabilities and
their signed bytes without conversion. The [existing-seat action owner](ROOT-ACTION.md) provides an offline-qualified
mortal root basket encoder, durable one-request signing/nonce ownership and
receipt/expiry recovery. Its read-only chain adapter reconstructs canonical
native inclusion and receipt evidence from the approved owned RPC, with exact
historical execution-runtime checks; it does not independently prove GRANDPA
finality or storage. The [offline custody handoff](ROOT-OFFLINE-CUSTODY.md) now
verifies independent exact-action approval and retains one matching public
signature before handing it to this owner; missing receipts remain unresolved
across restart. The [root service decision owner](ROOT-SERVICE.md) now couples
one approved basket decision and its original native intent in one private
durable journal, with a finite joined supervisor and independent signer/submission
ports. Its read-only canonical weight view and pinned-runtime normalization
select an intent, never signing authority. The [owned-RPC submission adapter](ROOT-SUBMISSION.md)
now sends exact signed bytes under separate action/route approval, retains
uncertain numbered attempts and reconciles canonical outcomes before another
approved send. Its local composition with offline custody and the service owner
is qualified. The [bounded root-service command](evidence/root-service-runtime-qualification-20260930.md)
now composes original input admission, observation and issued-signature recovery,
with a closed public activation gate. Production live authority, globally fenced
native custody and a qualified separate hardware signer remain absent; there is
no live root signing command supplied by that legacy path. A signed root
call does not bind registration generation, so pending-action seat changes need
custody exclusion or separately authenticated incident reconciliation. The
legacy action must be reconciled under its original runtime authority. Changing
strategy, signing fees and distributed custody fencing requires separate approval
and qualification; a local reserve is not a native maximum-fee argument.
The current [UR validator config](../validator/config.go) rejects netuid 0 and is
not a root-validator implementation.

For an existing root seat, verify hotkey/coldkey ownership, current membership and registration generation, stake, immunity, delegate take, children/parents, basket configuration and accrued rights before adoption. For a new seat, burn-priced root registration has no displacement stake comparison while capacity is free. At full capacity, the applicant's root stake must be at least the lowest-staked nonimmune member's; registration fails if every seat is immune. Registration alone does not establish sufficient stake to retain a seat. The [current participant source review](/mnt/data/sn-testnet/mainnet-parallel-20261004/root-current-participant-source-review-20261004.json) also verifies that adding stake requires an existing hotkey account. Do not assume a new hotkey can always register first and stake later when the root network is full, or invent an account-creation workaround. The offline `root-capabilities` source checks metadata shape only; its eleven authored tests are unexecuted, and all current membership/capacity/stake/earning values remain unknown. [Current root registration implementation][subtensor-root-470]

One specific budget gap must not be hidden: native `root_register(hotkey)` has no maximum-burn argument, while `register_limit` rejects netuid 0. A fresh quote is not an atomic price ceiling. The inspected Neuron precompile also exposes `rootRegister(bytes32)` without a limit. [Native call definitions][subtensor-dispatches], [registration limits][subtensor-registration], [Neuron interface][neuron-interface]

The source now implements the separate runtime473 `root-register` workflow for an absent seat, also available as `bootstrap-chain root-registration`. Its `observe` and `plan` steps retain the finalized capacity/stake/ownership census, nonce, runtime artifacts and concrete burn/balance quote. The independently approved policy names `operator_coldkey_account_id`, `root_hotkey_account_id`, `receive_only_reserve_account_id` and `subnet_owner_account_id`; the operator must differ from the reserve and current SN25 owner. An existing seat stays on the existing passive-service path.

Registration accepts only the explicit `operator_balance_exposure: "whole-reducible-operator-balance-at-inclusion"` domain with every source-defined exposure acknowledgement. `quoted_burn_preflight_limit_rao` and `fee_reserve_rao` are preflight thresholds; they do not cap inclusion-time burn or fees. Any supplied `strict_burn_cap_rao` is refused. Separately signed operator consent, actual operator hardware signing or an independently returned original public signature, and a separate production submission approval remain required. No reserve or subnet-owner device configuration is selected automatically.

The sequence is `observe`, `plan`, independent consent, `reserve`, `export`, owner-local `inspect-request`/`ledger-plan`/`sign`, `import-reply` (or pinned `import`), `submit-plan`, independent submission consent, then `submit`/`reconcile`. The daemon state uses the new `mainnet-root-register` physical owner kind and `root-register-action.json`; operator-device custody is independently provisioned. One original nonce, era, signature and finite cumulative post allowance survive restart. Every post first reconciles exact canonical bodies; uncertain signing is not repeated, an existing seat is not re-registered, and expiry never silently changes the approved action. A new unsigned plan requires fresh observation and approval.

`bootstrap-handoff` requires the original finalized `NeuronRegistered(0, uid, hotkey)` event and agreeing inclusion-block ownership/registration generation. It emits a proposed passive root role and original receipt pins; the passive configuration and service approval must still be completed independently before existing bootstrap-chain v4 or v5 orchestration. Its eight contract actions are unchanged. A successful receipt retains the exact transaction fee but leaves actual burn unknown; parent-block Burn or end-block balance differences do not identify that debit. These new source paths and deterministic regressions require focused execution qualification; source review grants no spending or activation authority.

The root-seat admission choice must be explicit:

- Adopt an already registered, approved root identity and prove its finalized receipt and current ownership.
- Use the new separately approved runtime473 native registration workflow with its explicit inclusion-time operator balance exposure and retained original receipt. Actual burn remains unknown unless separately proved; it is never described as “max-burn protected.”
- If a strict numeric cap is required, qualify a separate on-chain enforcement mechanism first. A possible EVM wrapper would change coldkey custody and require proof of rollback, fee separation, ownership and subsequent staking/withdrawal authority; no such helper is supplied by this implementation or the existing UR contracts.

Without a retained seat or the new workflow's actual operator approvals and finalized registration, root bootstrap remains pending. A strict-cap request is refused while no enforcing mechanism exists; the requested root role is not omitted.

Root registration can automatically delegate child weight to every existing subnet owner unless the identity opts out first. The current passive policy observes existing automatic, current and pending delegation without changing it. A new registration or opt-out needs its own explicit plan, coldkey association and current dispatch review. Historical root weight vectors were removed by the runtime migration; preserve old signed actions and reconcile any outstanding liability instead of inventing a reset transaction. [Current dispatch definitions][subtensor-dispatches-470], [removal migration][root-removal-470]

Select **accumulate in place with no custom weight vector** for the new unsigned launch plan. Dividends are held where earned and no periodic write is required. Optional basket trades are a different coldkey/proxy capability and are not implemented by the passive service. The new observer explicitly rejects metadata that restores the retired root weight call/gates. [Current basket behavior][root-reborn-470]

Stake the root seat from its own explicit TAO allowance, retain fees/ED, and observe the actual retention margin. Stake or basket top-ups, re-registration, claims, take changes and basket trades require separate bounded plans. The passive service monitors finality, seat ownership, stake rank, delegation, basket state and runtime identity. Claiming root yield and unstaking principal are distinct operations with runtime-dependent windows; neither is enabled automatically by “run a root validator.”

### UR subnet validator

Run the production [validator entry point](../cli/validator/main.go), using complete deployment, policy, runtime, genesis, operator API, evidence and signing inputs. It must acquire current UR eligibility and validate finalized usage/evidence before emitting native CRv4 weights. Preserve the signed weight cap, head/tail rules, deposit/quality calculation and full prefix/history admission. Reject testnet provisional-input deferrals on mainnet.

Observe effective alpha/root-stake contribution, child attribution, `TaoWeight`, stake threshold, permits, activity, CRv4 version, reveal schedule and mechanism state. Do not transplant the testnet stake target or an old 0.18/0.018 TAO multiplier. The inspected production epoch path gives the owner UID special eligibility treatment; another owned hotkey still needs its own proper eligibility. A configured process being alive does not prove it has a permit or that its weight row was revealed and applied. [Permit calculation][subtensor-epoch]

The root seat does **not** count as a UR validator: a seat on netuid 0 alone does not validate the UR subnet, even when the same hotkey holds both, as at the SN25 launch. The approved mainnet policy requires at least one live validator and one healthy operator. By owner decision (October 7) the launch has exactly one of each, and its validator scores UR's own pool with no independent cross-check ([decision](#one-operator-and-one-validator--october-7)). Do not generate synthetic peers to satisfy the count.

Capacity is computed from the union of actual UR hotkeys: head miners, one pool per operator, distinct UR validator identities, escrow and owner/other protected identities. Root-only membership consumes no UR slot. A validator-permit limit is not a reserved partition of UID space. Keep the release's one-mechanism requirement and approximately 200-head target only if the live capacity and all additional identities fit. Do not assume “200 miners + 56 validators” leaves space for pools and escrow.

Manage both services with independent state directories, signer permissions, logs, executable/config hashes, and one writer per hotkey. Coldkeys and Safe owners stay out of online validator processes. Supervision has bounded restart policy; ownership transfers require the old process and every child to be joined. Read-only health includes finalized lag, evidence/index gaps, permit activity, revealed weight rows, pool/escrow capture, signed cap violations and root seat/basket drift.

## Go CLI and action model

[mainnet/main.go](main.go) currently contains signer-free `inspect`, `monitor`,
`subnet-preview`, `root-preview`, `root-monitor`, `check-recycle-mode` and `economic-reference`
commands, plus the offline `source-lock`, `release-inventory` and blocked-review `plan` commands. `inspect --rpc URL`
emits a content-hashed identity snapshot. Supplying
any expectation requires all of `--expected-chain`, `--expected-genesis` and
`--expected-evm-chain-id`; `monitor` always requires all three. The monitor emits
JSON lines for `ok`, `rpc-error`, `rpc-integrity`, `identity-mismatch`,
`finality-conflict`, `finality-stalled` or `checkpoint-error`, with a default 30-second interval after
each completed sample and five-minute stall threshold. It detects identity/finality conflicts,
rechecks the prior finalized block when the head advances, validates JSON-RPC
response version/ID, and rejects response bodies exceeding 1 MiB. Hash comparisons
accept equivalent hexadecimal casing. Malformed, inconsistent or oversized RPC
evidence emits terminal `rpc-integrity`; that status, `identity-mismatch` and
`finality-conflict` exit with code 3. Availability failures remain `rpc-error`
observations and do not establish healthy state. Repeated read failures retain
their first observed time; `severity` becomes `warning` after two minutes and
`critical` after five, while the monitor keeps retrying. A host-clock rollback
escalates immediately rather than postponing the page threshold. The command
contains no signer or submitter. Focused normal/race tests and vet pass; the retained Snow rejection
demonstrates actual wrong-network refusal.

`monitor --checkpoint /absolute/path/monitor.json` adds a single-owner local
continuity checkpoint. Before reporting a newly finalized position as healthy,
it atomically persists the approved chain/genesis/EVM identity, last finalized height and hash, and
progress time with a content checksum. The v3 checkpoint also retains the last
successful identity/continuity read and the start of an unresolved read outage,
including an initial outage before any finalized position is known. It clears
that outage only after a complete identity and continuity read. The reader
accepts valid v1/v2 records without inventing their missing success times and
writes v3 on its next state change; a rollback to the old
binary requires an explicit compatible checkpoint migration, not silent file
replacement. Restart loads the retained position and checks
the prior finalized hash against the route; a regression or changed historical
hash is still visible after process restart. A corrupt, foreign or symlinked
checkpoint stops admission; an unavailable write emits `checkpoint-error` and
exits rather than reporting health. The file is local continuity evidence, not
independent node confirmation or an approval. The operator must place it on a
durable, backed-up volume and supervise the monitor.

`monitor --metrics-file /absolute/path/monitor.prom` now exports bounded atomic
textfile gauges to the existing Fluent Bit collector. It exposes completed
sample freshness, last successful read, finalized progress, unresolved outage
and explicit status/severity. The [telemetry guide](MONITOR-TELEMETRY.md) and
tested [alert examples](monitor-alerts.example.yml) include an independently
supplied expected-host roster so one healthy host cannot conceal a missing
second host. Actual deployment, ingestion and alert delivery, independent
supervision and cross-domain health remain open work. A failed export reports
critical status and exits; stale retained metrics never establish fresh health.
Restart retains the prior metrics until a completed sample, including a prior
critical event. Physical path checks prevent checkpoint/metrics lock collisions,
and success timestamps include the final required continuity read.

An unchanged retained head with a checkpoint progress time ahead of the host
clock reports stalled until genuine finalized advancement resets the clock.
`root-preview` and finite `root-monitor` perform the separate [read-only root
census](ROOT-VALIDATOR.md). Their `ready` result means observation policy
readiness only; `activation_ready` is always false. They load no signer and do
not register, stake, submit root weights or authorize basket claims.
`subnet-preview` performs the [read-only SN25 reset feasibility
census](SUBNET-CENSUS.md), with explicit protected and removal generations;
`reset_ready` is always false and no UID is changed.
`owner-trim-plan` adds a bounded ranked partial-trim prediction and explicit
residuals, without changing that admission result.
`owner-trim-recheck` and `owner-trim-reconcile` extend the read-only evidence
through drift detection and exact post-state comparison, with the same blocked
execution status and no signer.
`owner-trim-qualify` separately evaluates a bounded approved-subset invariant;
its conditional result retains unproved window assumptions and no apply authority.

The [existing-seat root action owner](ROOT-ACTION.md) is an offline-qualified
one-action signing and recovery core, not a CLI command or live root validator.
It retains the original signed bytes, nonce and fee reservation across ambiguous
submissions. The [service owner](ROOT-SERVICE.md) now owns the approved decision,
composite intent and finite supervisor; read-only canonical observation and
receipt adapters exist. The separate [submission adapter](ROOT-SUBMISSION.md)
implements owned HTTP writes and durable attempt reconciliation. Production
current-authority and custody adapters, actual route/seat approval and command
activation must still be supplied and qualified before publishing a root basket;
the current accumulate-in-place strategy needs no periodic root transaction.

The [executable local root-custody bootstrap phase](BOOTSTRAP-ROOT.md) now wires
`bootstrap plan/apply/resume` to these existing custody and service owners. It
completes local journal preparation, exports the approved public packet and
imports its verified signature without a native key or network operation.
Resume reconciles actual child state after interrupted progress or output;
completed journals cannot be recreated as fresh allowances. Results separate
local custody completion, signature awaiting import and pending chain phases.
This is one implemented phase, not full bootstrap or root service activation.

The [offline chain composition](BOOTSTRAP-CHAIN.md) now joins that root owner,
reserve CREATE custody, a retained trim review and two distinct protected UR
role inputs (one `sole` role under the later v5 schema) under one durable local
preparation. Its [component qualification](evidence/bootstrap-chain-qualification-20260928.md)
passed 369 full normal roots, all 87 selected race roots in six disjoint shards,
and four causal families. The original aggregate race timeout remains retained.
Tests used physical Connect `358cefae`, server `0633780c` and SDK `42241118`;
they do not qualify the current composed release graph. Executed trim, complete
contract installation, actual UR producer admission, healthy operators, current
root authority, all live services and native economic acceptance remain open.

The v2 offline admission addition now reuses the standard validator's strict
schema-3 config and production approval verification for the intended majority
and secondary UR roles; v5 applies the same verification to its one sole role.
It binds exact public identities, independent signer pins and
runtime/source/deployment to the retained protected generations.
Current stake/permit, key possession, operator evidence and healthy services,
deployed contracts and runtime authenticity remain unproven. V1 journals retain
their original limited status and cannot be silently upgraded. This addition
and its shared config decoder extraction are now [qualified](evidence/ur-bootstrap-admission-qualification-20260928.md)
on the frozen composed source graph: 588 root and 183 descendant executions
passed, with six causal controls in normal/race modes. The result covers bounded
offline admission; actual chain effects and a complete release remain open.

The v3 offline addition independently pins the separate netuid-0 root role,
its original action approver and a distinct full-service-config approver. It
verifies a domain-separated signed approval of the exact child root plan and
service configuration, retaining only a public inspection and live-authority-
pending status. V1/v2 journals keep their prior domains and recovery scope.
The [qualification](evidence/root-role-admission-qualification-20260928.md)
passed 403 full normal roots, all 38 selected bootstrap-chain race roots in
three disjoint shards, seven metrics fixture race roots and vet. Live root
eligibility, key custody, service activation and a composed production release
remain separate gates.

`check-recycle-mode --rpc URL --policy FILE` binds the finalized mode read to
independently supplied mainnet genesis, runtime code/metadata and complete
version pins. It validates the runtime-declared map, enum and Burn fallback and
reports whether the finalized value is Recycle. `economic-reference --input FILE`
computes cumulative integer 10% provider / 90% recycle references from
caller-supplied native miner tranches, carrying rounding between intervals.
Both retain `activation_ready=false`: the first proves only mode storage under
the approved artifact, and the second does not authenticate interval inputs or
actual Yuma payouts. [Command contract and remaining gates](ECONOMIC-GATE.md)
cover the source-to-code, owner-hotkey, allocation and observation work. Focused
normal/race tests and vet pass after the 2026-09-27 data-volume recovery.

`inspect`/`monitor` remain single-route identity/finality observers. The
separate `runtime-snapshot` captures raw finalized code/metadata without
granting authority. `inspect` and `monitor` validate the complete runtime tuple
internally, but their v1 JSON reports only spec/transaction numbers; emit a
versioned full-tuple observer artifact before using them as independent runtime
identity evidence. Unknown digest variants require reviewed decoding support.
These commands do not map EVM receipt finality, compare independent nodes, inspect SN25
custody/validator/settlement state, deliver alerts,
or execute repairs. Those are MG-07 and related production gates. An identity
snapshot hash proves the captured bytes, not operator approval or node truth.

The [shared metadata decoder](evidence/runtime-metadata-bounds-20261001.md) now
admits at most 8 MiB of raw metadata before hex allocation, refuses collection
counts larger than their input, and shares finite storage/work/depth budgets
across nested SCALE decoders and the v14 derived lookup map. Self-sealed discovery
input and unapproved RPC bytes receive this guard; they remain unapproved.
Independent owner/root metadata pins still precede decoding. Valid runtime470
metadata and the separately pinned SDK-v15 owner path retain their existing
semantics. The [shared native HTTP transport](evidence/http-rpc-response-bounds-20261001.md)
now bounds direct/unmarked responses and batches before JSON decoding. Configured
CRV4 metadata and mainnet discovery already had their own physical/per-call
bounds; the uncovered submission, unknown-method and direct-client paths now
receive the shared cap too. It permits 32 MiB + 64 KiB + 2 decompressed JSON body
bytes, including a full 16 MiB events field after hex expansion. Batches share
that aggregate ceiling and must be split by the read owner if larger. Error
statuses are closed without reading their bodies; complete framing, physical
release, cancellation and exact response IDs precede any result publication.
The change grants no retry authority to submissions.

The [direct EVM HTTP successor](evidence/evm-http-response-admission-20261001.md)
adds the same finite aggregate allowance to all seven direct miner and `stctl`
dial sites. Its shared `evmrpc` owner bounds both encoded and expanded gzip bytes,
validates complete response framing/IDs and closes the physical body before
geth can publish results or drain a successful prefix. Error-status bodies are
discarded without reading; their text cannot acknowledge a signed transaction.
Read status retries remain with the existing finality owner, while redirects
cannot replay signed POSTs. Original signed recovery history and exact replay
authority remain unchanged. Validator, mainnet custom RPC and server receipt
collection already use separate bounds; WebSocket/IPC, aggregate process memory
and host capacity are not newly qualified by this change.

The [current source checkpoint](evidence/source-lessons-checkpoint-20261005.json) adds finite inspection of complete physical error graphs, including typed nil, cycles and hard siblings. Legitimate optional DNS causes remain valid. Hard body/Close failures and cancellation survive upgrade fallback, while WebSocket batching stays intact. SDK shutdown retains the same signed close frame until acknowledgement or a joined out-of-band handoff. Separately, economic follow reads can continue after an acknowledged financial checkpoint during a transient metrics-only outage, without publishing success or replaying retained effects. All of these new source paths still require their named normal/race/control qualification.

The [public cross-endpoint readback](evidence/public-cross-endpoint-20261001.md)
compares Rao archive and the public entrypoint at one pinned finalized block.
Their genesis, header, runtime version, metadata and `:code` digests match, but
this remains RPC corroboration; independent finality, source and operator
approval are required before signing or activation.

The [pure plan foundation](PLAN.md) consumes one `finalized-snapshot`, a source
lock and release inputs by exact hashes. `plan --outline` exposes the unbound
dependency graph while approved mainnet identity is unavailable;
`plan --config FILE` accepts the separate strict JSON review schema and refuses
testnet EVM945 or an unexpected genesis. Both modes keep every action blocked,
with no apply authority. Supplied review manifests remain unvalidated until
their actual semantic/capability/custody checks are implemented.

Frozen source SN `d9d01a6a` lets eight pure review modes reach input validation without unrelated daemon durable-volume declarations: `bootstrap plan`; `bootstrap-chain plan`, `contract-plan`, `contract-role-plan`; `bootstrap-contracts plan`/`preview`; and `root-service plan`/`root-passive-service plan`. Explicit inputs and their authority checks still apply. Retained-state and effect commands keep custody admission. Four new public regressions and two controls remain unexecuted.

Keep the executable plan builder pure after authenticated snapshot inputs are supplied.
Separate chain adapters, signer interfaces, state storage and supervisors so
preview cannot reach a transaction submission path.

The table distinguishes existing commands and bounded phase implementations
from the remaining combined launch interfaces. The generic `plan` graph remains
blocked; its target `apply`, `resume`, `services` and `report` commands are designs.
Separate bootstrap commands already implement local preparation and explicit
contract submission at their own scope. Their existence does not qualify the
combined release, provision production authority or prove a deployment.

| Command | Behavior |
| --- | --- |
| `inspect` | Extend the existing read-only identity capture with authority, census, capabilities, balances, custody and validators; emit a hashed snapshot. |
| `runtime-snapshot` | Existing signer-free capture of one finalized runtime's exact `:code` and metadata bytes, complete version and node identity; independent approval and source-to-Wasm review remain separate. |
| `finalized-mapping` | Existing signer-free capture of linked finalized native and raw EVM header commitments with an owned-RPC canonicality assertion; no signing or mainnet approval. |
| `finalized-snapshot` | Existing signer-free same-block capture of runtime code/metadata and native/EVM mapping; its output remains unapproved observation. |
| `monitor` | Extend the existing read-only identity/finality loop with durable checkpoints, independent comparisons, complete domain health and existing-stack alert delivery. |
| `subnet-preview` | Existing signer-free finalized SN25/root UID census and owner-trim candidate comparison; full custody and execution-time reset authority remain open. |
| `subnet-discover` | Signer-free runtime 470 membership/owner/generation discovery from a retained unapproved runtime or combined finalized snapshot; leaves every seat unclassified and supplies no trim policy or apply authority. |
| `root-preview` / `root-monitor` | Existing signer-free finalized root seat and strategy census; an offline [existing-seat action core](ROOT-ACTION.md) exists, but production signing and activation remain separate work. |
| `check-recycle-mode` | Existing signer-free finalized storage-mode precondition; extend with an approved mainnet artifact and operational readback at activation/recovery. |
| `economic-reference` | Existing signer-free cumulative integer 10%/90% reference from caller-supplied native intervals; a treasury reference may mark reserve-only intervals, whose whole tranche goes to the treasury. Actual chain reconciliation remains a separate gate. |
| `source-lock` | Existing offline lock of clean SN and every local Go replacement Git commit, module checksums, Go version and tool hash. It binds source inputs only; artifacts, rollout approval and qualification remain separate. |
| `release-inventory` | Existing local candidate inventory of exact executable/contract/config/policy/migration/image/dependency/toolchain files, bound to the rechecked source lock; missing categories remain explicit, and release completeness/provenance/deployment approval stay false. |
| `plan` | Existing pure blocked-review graph via `--outline` or strict JSON `--config FILE`; hashes exact finalized-snapshot/source-lock/release inputs. Every action remains non-executable. Full semantic admission, payloads and executable authorization remain future work. |
| `bootstrap plan/apply/resume` | Existing [local root-custody phase](BOOTSTRAP-ROOT.md), with exact plan/run-directory acceptance and separately pinned public signature import. It does not submit or activate services. |
| `bootstrap-chain plan/apply/resume/readiness` | Existing [bounded chain preparation](BOOTSTRAP-CHAIN.md); preparation preserves local custody, while explicit readiness observes the approved route with a 60-second minimum retry budget. Contract and trim review subcommands retain their separate scope. |
| `bootstrap-contracts preview/plan/apply/resume` | Existing [eight-action contract phase](BOOTSTRAP-CONTRACTS.md). Only explicit `resume --online --submit` admits a bounded original signed submission; local apply and unsigned preview do not install contracts. Production inputs and complete installation verification remain required. |
| `owner-signing` | Existing [owner-local device interface](OWNER-SIGNING.md). Signing remains on the owners' devices; Snow receives separately verified public results, not owner private keys. Actual hardware and production provisioning remain gates. |
| `activate-validators`, `activate-root-passive`, `root-passive-service` | Existing [UR activation](VALIDATOR-ACTIVATION.md) and [passive root service](ROOT-PASSIVE-SERVICE.md) interfaces with exact custody/configuration admission. Production approval, installation and operational qualification remain separate. |
| `repair-validator`, `repair-active-validator` | Existing [stopped-validator](VALIDATOR-REPAIR.md) and [active-hang](ACTIVE-VALIDATOR-REPAIR.md) repair interfaces. These bounded phase commands do not implement the generic repair graph below or prove an operational repair rehearsal. |
| `apply --accept-plan HASH` | Execute only the exactly reviewed plan with matching signed authorization, prerequisites and ceilings. |
| `status` / `verify` | Read-only journal reconciliation and current/finalized postcondition verification. |
| `resume --accept-plan HASH` | Recover in-flight actions, verify retained receipts and continue the same approved graph without duplicate spend. |
| `services start` / `services stop` | Run or join the plan's admitted services; starting write-capable validators is an explicit authorized phase. |
| `report` | Produce a complete acceptance or incomplete/blocked report with evidence references and realized spend. |
| `repair plan` / `repair apply --accept-plan HASH` | Produce and execute only the exact approved, bounded repair graph; share the existing durable transaction owner and lifetime ledger. Not implemented. |

`source-lock --sn-dir /absolute/sn/path` emits a content-hashed JSON record of
the clean SN Git HEAD and every local `go.mod` replacement's clean Git HEAD,
the exact `go.mod`/`go.sum` hashes, current Go version and command-binary hash.
It refuses modified or untracked repository files, ignored replacement module
files and active `go.work` overrides; it rechecks each HEAD after hashing. It
reads no RPC and holds no signer. This is one input to the release
manifest, not an approval or a claim that compiled binaries, Foundry bytecode,
generated files, ignored files, configuration, migrations or the running images
match those commits. The release owner must bind those artifacts separately,
qualify the composed source and approve the resulting immutable manifest.

`release-inventory --config FILE` provides the [actual-file candidate
inventory](RELEASE-INVENTORY.md). It rechecks source closure and selected file
bytes, refusing stale per-artifact source bindings. It enumerates missing
categories but does not infer complete coverage from one file per category,
prove a build or image, inspect a deployed migration state, or approve a release.
Ignored Solidity libraries and compiler identities are explicit dependency and
toolchain inputs; they are not included by the Go source lock alone.

The [current release builder](RELEASE-BUILD.md) composes all seventeen selected
SN/server commands, all five production contracts and eight image contexts.
Server `898dc8f3` aligns SDK, Connect and the SCTP fork with SN's exact reviewed
module versions/sums; independent qualification builds all thirteen server
commands after the original nine failures. The builder pins clean source trees,
effective module graphs, module zip bytes, tool identities, binary build info,
retained/selected/compiled contract hashes, recipes and migrations. The default
retained catalogue keeps Coordinator and ValidatorEvidence's historical metadata
bytes, so its exact source-to-bytecode equality remains false. Explicit
`contract_catalog: "fresh"` selects exact creation/runtime compiler output for
all five contracts, preserves the historical catalogue separately and refuses
ABI, constructor, storage-layout or immutable-reference changes. The selected
schema-1 catalogue is consumed through the bootstrap plan's exact file path and
SHA256; the checked-in binding does not need replacement.

The [fresh-catalogue qualification](evidence/fresh-contract-catalogue-build-20260930.md)
now records a complete local candidate: all seventeen binaries built, all ten
contract creation/runtime pairs exact, and all five deployment interfaces
unchanged. Independent Sol qualification passes the 31 builder roots normal/race,
vet, causal controls and complete artifact readback. The receipt pins the selected
catalogue separately from the unchanged historical files.

The [bounded scratch-image qualification](evidence/scratch-image-qualification-20261001.md)
builds and independently reads back the exact `server-competitionworker`
Linux/amd64 image from this candidate. A separate Sol build reproduced its OCI
archive bytes. The [seven-service offline image qualification](evidence/seven-service-image-qualification-20261001.md)
now supplies pinned local Ubuntu OCI and package inputs for the remaining seven
unchanged production recipes. Its independent rebuild produced byte-identical
archives and passed complete OCI readback. This qualifies a local image build,
not a published or deployed release; aggregate release and deployment flags
remain false.

The [offline aggregation command](RELEASE-BUILD.md) can now verify those separate
receipts against one exact original source manifest and emit a sealed eight-image
attestation. Its [frozen-candidate qualification](evidence/release-image-aggregate-qualification-20261001.md)
binds SN `2d53e6f2` and server `ecbf3aad`, rehashes all 337 input artifacts and
replays the OCI/rootfs checks against their exact parent binaries and recipes.
Only the aggregate's local `source_to_image_verified` advances; original evidence
is unchanged and reproducibility, release completion and deployment approval
remain false. This operation needs no Docker service and performs no build,
publication or application execution.

The later [unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
locks SN `de0823ce` and server `0b8e758d`, including the newer validator and
operator custody changes. Its source lock and partial inventory retain the
actual reviewed Connect/SDK module pins separately from observed newer main
heads. It does not inherit the older aggregate's image provenance. That bundle
does not supply its own complete source/image composition, and keeps release
completion and deployment approval false.

The [pre-Safe baseline and permission repair](evidence/release-pre-safe-baseline-and-modes-20261001.md)
subsequently build SN `1806b3b3` / server `0b8e758d` twice with empty compiler
caches: all seventeen executable hashes and ten contract creation/runtime
outputs match. The API image attempt correctly rejects a `0700` executable
created under the build host's private umask. The qualified builder correction
preserves exact requested permissions without weakening OCI verification or
changing retained outputs. All eight corrected OCI exports and their separate
337-artifact aggregate now pass, establishing local source-to-image coverage
for this exact baseline. This baseline predates the later public
Safe-submission implementation; a release of that source needs a fresh exact
source/image build. Same-host repeatability does not establish independent
reproducibility or authorize a deployment.

The [frozen Safe-source release](evidence/release-safe-source-20261001.md)
then binds exact SN `095a2208` / server `0b8e758d`, including the Safe and
permission corrections. It builds all seventeen commands and five fresh
contracts twice and supplies eight new OCI images with a complete local
source-to-image aggregate. All eight platform manifests and OCI archives also
match on repeat. Its 55-file inventory repeats exactly, while
production policy and published/deployed image identity remain absent. The
frozen source commit is distinct from later reporting commits; independent
reproducibility, actual service/configuration/policy qualification and release
approval remain open.

The [current-admission packaging baseline](evidence/release-6c801a25-server720-20261001.md)
now binds exact SN `6c801a25` / server `720e7c61`, retaining the same
Connect/SDK dependency pins. Its two fresh-cache builds match all seventeen
executables and ten contract bytecode outputs; eight OCI images have a complete
local source-to-image aggregate. All 89 builder roots pass normal/race and
vet. Its source migration inventory records 750, without applying it. Any
launch selecting later passive-root host, owner recycle-mode transition or
mixed-writer/migration-751 changes requires a successor source/image build;
this baseline does not attest them.
Independent reproduction, actual configuration/policy, rollout and release
approval remain open.

The [successor preparation](evidence/release-successor-preparation-20261001.md)
retains the pre-owner graph without relabeling its original scope. The
[historical successor release](evidence/release-233ea2be-server942-20261001.md)
binds SN `233ea2be` / server `94229abb`, including passive-root host,
owner recycle-mode transition and schema-751 mixed-writer source. Two fresh
source builds match all seventeen executables and ten contract bytecode
outputs; both eight-image aggregates pass, and every OCI platform manifest,
configuration and archive matches. All 89 builder roots pass normal/race and
vet. The unsigned 56-file inventory repeats exactly and retains 751 migration
source entries without applying them. Connect/SDK pins remain unchanged.
Later reporting commit `7c5d964f` is distinct from the actual build source.
Independent reproduction, production policy/configuration, migration/restore,
rollout, publication identity and release approval remain open.

The [historical finality/migration successor](evidence/release-689938d6-server6c39-20261001.md)
binds exact SN `689938d6` / server `6c39d307`, with the same effective Connect/SDK
pins. It includes complete-header authority, claim/shared EVM finality,
installation clock continuity, historical native capture and monitor changes.
Two sequential fresh-cache builds match all seventeen executables and ten
contract bytecode outputs. Each retains 175 artifacts, including all twelve
current migration source inputs. Both eight-image aggregates verify
342 artifacts each, with identical platform/configuration/archive bytes. All 95
builder/source-graph roots pass normal/race and vet; a causal old-selector control proves the earlier
eight-file omission. The 61-file unsigned inventory repeats exactly and records
751 catalogue entries without applying them. The superseded e4 attempt and all
predecessor releases retain their original bytes and separate scope. Independent
compiler/build reproduction, production policy/configuration, migration/restore,
rollout, publication identity and release approval remain open.

The [earlier metadata/HTTP successor](evidence/release-bab49e1c-servera3e2-20261001.md)
now packages metadata-decoder `b9ee4c91`, shared HTTP/subscription `58852c47`
and direct EVM HTTP `0dea3f26` at frozen SN `bab49e1c`, paired with server
`a3e2e668` and unchanged Connect/SDK pins. Two sequential source builds and
separate initially empty image stores match all seventeen executables, ten
bytecode outputs and eight OCI platform/configuration/archive identities.
Both complete local source-to-image aggregates verify 342 input artifacts.
All 95 builder/source-graph and thirteen server sampler roots pass normal/race,
and their packages pass vet. The optional server sampler stays off without
explicit bounded configuration. Its 61-file inventory repeats exactly, with
twelve migration inputs and 751 catalogue entries retained but not applied.
Full module-body qualification is incomplete for 363 SN and 372 server graph
nodes. Independent provenance/build reproduction, actual role behavior,
production configuration/policy, rollout, published identity and release
approval remain open. Earlier receipts retain their original scope, and later
reporting/authority documentation does not change this frozen binary source.

The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
binds exact SN `28ebfced` / server `ac86855d`, including native observation
closure, refreshed EVM send admission and the selected-grant locking change.
Connect/SDK pins remain unchanged. Two sequential fresh-cache builds match all
seventeen executables and ten bytecode outputs; both complete eight-image
aggregates verify 342 artifacts with identical platform/configuration/archive
bytes. All 95 builder/source-graph and thirteen sampler roots pass normal/race
and their packages pass vet. The repeated 61-file inventory retains twelve
migration inputs and 751 source catalogue entries without applying migrations.
Sixteen original component records retain their exact source and failure scopes.
Independent readback passes both source candidates and both eight-image sets;
its primary receipt and separate B-image addendum preserve their distinct scope.
Local builds and artifact readback do not close independent compiler/build
reproduction, composed service/database behavior, production policy, migration,
rollout, published identity or release/deployment approval. Later source changes
require a separate exact release; reporting commits do not change these pins.
This baseline remains **superseded for launch** by the
[exact custody successor](evidence/release-1d580d5e-serverac86-20261001.md); its original receipts are preserved.

The [qualified readiness release baseline](evidence/release-3d1e2ecf-serverac86-20261002.md) binds frozen
SN `3d1e2ecf` / server `ac86855d`, including the five-marker/passive-root correction.
It is **superseded as local artifact selection** by the [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) at SN `1320845d` / server `0aa1e244`, which includes the later owner, contract-admission and recycle corrections. Its own receipts remain historical and unchanged; neither selection authorizes launch.
Two sequential fresh-cache builds match seventeen binaries and ten bytecodes;
two separate eight-image builds match OCI platform/config/archive bytes.
Independent A/B source and all sixteen image readbacks pass. The unchanged author
seal verifies 1,060 retained files and must be read with the separate corrected
finalization guard. The failed schema adapter and checkpointed seal-only
continuation remain recorded; no source/image build or completed qualification
phase was rerun. Original readiness and historical component scopes stay separate.
The exact binary captures a new public combined snapshot at block 9,191,688 and
emits identical ten-action plans, all blocked/non-executable with twenty-eight
missing and two supplied-unvalidated requirements. The `1d580d5e` predecessor
receipts remain preserved. Independent compiler provenance, complete production
qualification, legacy writer custody, live authority and acceptance remain open.

The [original EVM custody successor](evidence/release-1d580d5e-serverac86-20261001.md) previously bound exact
SN `1d580d5e` / server `ac86855d`, including writer `cb9f3aa2`, borrowed-reader
`1922981d` and diagnostic `4e6b4a7e`. Ten clean source checkouts were freshly
fetched from remotes to the data volume. Two sequential fresh-cache builds
match seventeen executables and ten bytecode outputs; both eight-image
aggregates verify 342 source/image artifacts and match all platform,
configuration and archive bytes. Builder/source-graph and sampler normal/race
gates and vet pass. Twenty component records retain their original source,
failure and continuation scopes; exact Go/module inclusion of the qualified
custody source and all 107 custody author evidence checksums pass. The repeated
61-file inventory retains twelve migration inputs and 751 catalogue entries
without applying migrations. Independent compiler/build reproduction, complete
module-body provenance, arm64, restore, production policy/configuration,
service behavior, migration/rollout, attestation and actual deployed image
identity remain open. The separately [qualified five-marker/passive-root custody
correction](evidence/bootstrap-readiness-custody-qualification-20261001.md) at `3d1e2ecf`
is outside that historical source and is now included in the
[exact readiness successor](evidence/release-3d1e2ecf-serverac86-20261002.md). Neither local artifact selection
approves release or launch.

Independent A/B source and all sixteen OCI image readbacks pass, with the
primary receipt and B-image addendum retaining separate scopes. The exact
release binary also captures a fresh public combined snapshot at block
9,191,346 and emits two identical unsigned plans. All ten actions remain
blocked/non-executable, with twenty-eight missing and two supplied-unvalidated
requirements; both authority/readiness flags remain false. The planning
supplement is sealed separately from the build artifacts.

No signed mainnet deployment plan has been evidenced. The existing catalogue
is release/testnet history, not established mainnet signing authority. Fresh
catalogue review and independent qualification are the preferred path for the
first unsigned mainnet plan. Checking for externally held signed commitments
remains a launch gate; any such plan, artifact or transaction must be preserved
and reconciled before selection changes. A metadata-equivalence exception is
only a conditional fallback, not selected by this path. Earlier independent
image reproduction remains scoped to SN `2d53e6f2` / server `ecbf3aad`; the
later source releases keep their separate independent-builder gate.
Full compiler/config/policy qualification and release approval remain open, as
do published-image identity and running-image readback.

The [earlier composed local candidate](evidence/release-candidate-v11-20260927.md)
locks SN `265231f9`, server `77cb401e` and Connect `c68689c4` with all local
Go replacements. Its [partial actual-file inventory](evidence/release-inventory-candidate-v11-20260927.json)
hashes 85 selected files, including seven locally built executables, all eight
server Dockerfiles, all seven image-build Makefiles, the exact package lock,
40 Ubuntu payloads, six signed-index inputs and four copied contract artifacts.
It has no approved policy or
published/deployed OCI image identity and remains unapproved.
The September 27 server source branch `codex/mainnet-composed-hardening-20260927` at
`b6f49bdb` composes the append-only migration 728 and retained-usage reader,
the operator receipt-census recovery correction, atomic payer admission and
checked settlement arithmetic on the v11 base. The earlier `7bf88d79`
combined controller selector passed normal, race and vet on disposable
PostgreSQL/Redis ([composed evidence](evidence/server-composed-hardening-20260927.md)).
This changes the source identity: the v11 source lock,
inventory and rebuilt artifacts do **not** attest the newer branch. Refresh
the complete manifest and production-path qualification before approving a
deployment. [Timestamp custody](/mnt/data/sn-testnet/evidence/mainnet-usage-time-custody-20260927/RESULT.md),
[operator recovery](evidence/operator-recovery-census-20260927.md).
The subsequent [MG03/R48 source composition](evidence/operator-mg03-r48-composition-20260929.md)
is integrated at server root `05fee56f`. Its full-merge parent preserves both
original histories and the complete qualified census/receipt/controller
lineage; the successor adds only two composition tests. Sol's normal/race
receipts cover 134 affected roots on the merge and, separately, two new plus
52 adjacent model roots on the successor. Their exact pinned physical graph
does not qualify the current dependency roots as a combined release. Refresh
the source/artifact lock and composed build before deployment.
Mainnet inventory implementation passed 177 normal tests and all 177 race
test bodies in a bounded run plus exact continuation; the latter is not one
whole-package race pass. A subsequent validator test-fixture correction passed
87 affected tests normally and under race without changing production seed
custody. The [unbound blocked outline](evidence/blocked-plan-outline-20260927.json)
names ten non-executable actions and 24 missing requirements. Its negative
control rejects retained Snow testnet EVM ID 945; the earlier
[same-block Snow observation](evidence/source-lock-finalized-snapshot-20260927.md)
authenticates code, metadata and linked native/EVM headers but remains
unapproved. The earlier [composition](evidence/source-lock-composed-20260927.md)
separately qualified unchanged validator and receipt selectors. This is
offline qualification of those source paths, not a complete release or an
approved mainnet configuration. The prior
[v7 source and contract build](evidence/source-lock-blocked-plan-20260927.md)
remain linked evidence; subsequent evidence-only commits do not alter the
frozen earlier candidates' Git identities.

The earlier v8 binaries use a local exploratory Go profile. An offline
[production-style probe](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v8-20260927/production-profile/RESULT.md)
also builds static, trimmed, version-stamped Linux/amd64 SN executables, but
they are not in the v8 inventory or an approved image. The pinned server API
and taskworker likewise build and repeat exactly from clean source; their
[build record](/mnt/data/sn-testnet/evidence/mainnet-server-binaries-20260927/RESULT.md)
is retained. Server commit `969d6c74` first pinned the six service Dockerfile
bases that still used a mutable Ubuntu tag. The subsequent [v9 image
probe](/mnt/data/sn-testnet/evidence/mainnet-images-v9-20260927/RESULT.md)
found that `apt-get` still read moving Ubuntu repositories. Server commit
`a211d56c` pins complete package payloads for seven service Dockerfiles,
including proxy's `curl` closure, and installs them offline. The
[package qualification](/mnt/data/sn-testnet/evidence/mainnet-server-package-pins-20260927/RESULT.md)
authenticates signed indexes and 40 payloads; three normal/race contract tests
and vet passed. Independent [amd64 API/proxy image
probes](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md)
built those recipes and extracted exact selected binaries. The
[v10 build and inventory record](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v10-20260927/RESULT.md)
binds the clean composition and replays its 81-file inventory byte-for-byte.
Choose approved production versions and architectures; archive exact package
and base inputs; qualify arm64 image execution, full service behavior,
source-to-image provenance, selected published OCI manifests and running-image
readback before MG-02 can close. Local binaries and image IDs are not an
approved rollout.
An [uncached API rebuild](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md#forced-rebuild-result)
produced a different OCI digest from identical pinned inputs; package logs,
cache and timestamps varied. The release build must either normalize those
outputs and prove exact repeatability, or identify an independently reviewed
immutable image without claiming reproducible bytes.
Server `77cb401e` removes only the two volatile generated files and gives all
seven image recipes a fixed source epoch and timestamp-rewriting exporter.
The [no-cache OCI qualification](/mnt/data/sn-testnet/evidence/mainnet-server-image-repro-20260927/RESULT.md)
repeated API and proxy runnable amd64 platform manifests, configs and layers
exactly. Its top-level indexes remained distinct because provenance described
different invocations. The source-level contract tests passed normal/race,
with offline image smoke. A separate [v11 taskworker image check](/mnt/data/sn-testnet/evidence/mainnet-server-taskworker-image-20260927/RESULT.md)
repeated its exact runnable amd64 platform image twice and verified the embedded
candidate binary in an offline container. Keep arm64, remaining service images, independent builder,
full attestation/SBOM/scanner policy, owned archive, registry publication and
deployed readback open.

The [incremental SDK/MG06 composition](evidence/incremental-source-composition-20260929.md)
preserves the original source histories and the reviewed versioned SDK, Connect
and SCTP pins. Its 37 focused roots pass normal/race and four mainnet smoke roots
pass normally; the original SDK candidate has complete 618-root normal coverage.
Its race root-body union is 618/618. The [corrected SDK snapshot](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
now records 543/618 backed by package-PASS streams, with 75 pending and no root
failures. This separate source graph still lacks broad race package closure.
The bounded
[`observe-native-miner-emission` reader](ECONOMIC-GATE.md#bounded-native-incentive-observation)
retains canonical event/state evidence with complete and partial outcomes. Its
18 focused and 170 adjacent roots pass normal/race, with eight causal controls.
Native denominator, quantization, recipient generation, entitlement, actual
owner recycling and the 10/90 outcome remain unresolved. This incremental
source integration does not approve a release or mainnet activation.

MG-10 testing lesson (2026-09-29): the SDK-pinned 618-root unsharded race
process reached its one-hour package timeout while
`TestEvmEscrowRegisterClaimRecoveryKeepsFourLocks` had been active for about
25 seconds; no top-level root failure was observed. The subsequent
[audit](evidence/incremental-source-composition-20260929.md#completed-and-pending-qualification)
records 618/618 top-level root-body passes, but only 410/618 have a pass inside
a stream with package PASS; 208 still require terminal package coverage.
Preserve the original unsharded and six initial shard timeouts; wave2/08 had
no package terminal at that snapshot. Use disjoint exact root shards, retain
each shard's source fence, package exit and original timeout, and preserve
completed results without resetting their evidence. Broad race package
qualification remains open.

The [later scheduling review](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
records 543/618 package-backed SDK roots and 75 pending. The evidence predecessor
checkpoint root runs fourteen serial fixtures and passed in about 28 minutes in
both its shard and exact-root retry; no product failure was established. Give
that root its own package/time budget and group other roots by measured duration
without reducing causal cases. Preserve the earlier timeout evidence.

The [owner-recycle measured decision](../validator/OWNER-RECYCLE-MEASUREMENT.md)
now joins signed successor approval, exact native owner census and fully
replayed original V2 provider proofs in a distinct capsule. It reconstructs
the proposed 10% provider / 90% owner weight row, but emits only a blocked
unsigned intent. It cannot enter the existing native signing/submission path;
validator eligibility, operator health, complete history, drained activation,
custody, archive and observed final incentives remain MG-06 gates. Its affected
139-test race selector and final-source 58-test focused normal selector passed;
the [qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-decision-20260927/RESULT.md)
states the exact limits. The next operator-observed variant invokes the actual
canonical coordinator reader after full proof replay: it checks active registry,
pool and provider mappings, exact deposit/conviction amounts, policy and source
root/window against the measurement, then rechecks chain/genesis and the exact
EVM decision hash. Its distinct v2 capsule binds those retained facts to the
original provider bytes; v1 capsules remain byte-compatible. This resolves
decision-time coordinator claims without treating active registration as API
health or independently proving the native/EVM mapping. Native validator
eligibility, API/key/payout history and all activation/signing gates stay open.
Its [qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-readiness-20260927/RESULT.md)
retains 138 normal and 138 race passes, vet, cross-compile and the final
naming-only follow-up without claiming launch approval.

The [production transition](OWNER-RECYCLE-PRODUCTION.md) separately enables the
actual standard validator under an independently signed schema-3 config. It
authenticates the exact compatible producer interface, validator stake/permit
and bounded activity, owner census, canonical operator facts and the pinned
zero-pending-emission activation block, then signs the measured row through the
real CRv4 source batch. Its sidecar remains bound through durable intent recovery
and independent archive observation. The same approved config can continue its
finite epoch window and replay historical decisions after head/cache changes.
Bounded content-addressed original config/approval bundles now preserve prior
sidecars across compatible independently signed renewals, source-file loss and
restart. The original drained activation and proof progress remain intact;
current signing never inherits an old grant. Policy, signer and custody migrations
remain separate transitions. Actual mainnet inputs and economic outcome remain
unprovided. Final economic outcome is a monitored
postcondition, not a prerequisite to the first submission.

PH-03 remains open for production continuation across read outages and epoch
changes. The [source-finality read candidate](evidence/source-finality-read-candidate-20260928.md)
preserves transport causes and unknown receipt evidence while reusing the exact
admitted body index/events. The paired
[adjacent-read candidate](evidence/production-read-cause-adjacency-candidate-20260928.md)
covers miner recovery, owner census/eligibility and activation setup. Their
[combined qualification is complete](evidence/source-read-cause-qualification-20260928.md),
with original fixture failures and corrections retained. They do not substitute
for retained-intent reconciliation before fresh snapshots, durable nonempty
intent-owner tests, bounded authenticated receipt prefixes or same-boundary
nonce/expiry decisions. Those changes must preserve original signed bytes,
missed/unknown outcomes and independently approved continuation authority.

The public startup lifecycle now reaches authenticated retained recovery before
requiring fresh signing eligibility. Its former native-runtime, EVM snapshot,
UID and stake ordering could prevent the historical activation/disk/intent
owners from opening. The separate startup scope below exercises the complete
public root through an outage, original receipt and applied row while keeping
current authority checks for new signing.

The [qualified production continuation](evidence/production-continuation-candidate-20260928.md)
moves the actual production `Run` branch and `submitOnceV2` toward the retained
intent before unrelated fresh scheduler/runtime/EVM observations. Its first
qualification scope is real nonempty V2 begin/replay/update/restart, exact
original receipt and application, and one canonical absence/nonce boundary.
Production read waits use 300 seconds overall with 60-second attempts; exhausted
reads remain visible waits, and mixed integrity/custody failures remain hard.
These native signatures use an immortal era: a later native epoch or local
approval deadline cannot revoke already signed bytes. Unknown work retains its
original signature and age until receipt or authenticated foreign nonce use at
the scanned boundary resolves it. Missed opportunities remain missed.
All 10 CRv4 and 22 validator roots now have normal/race coverage, with the original
race timeout and exact six-root completion retained separately. Six regression
control families reached their intended assertions in both modes. Full
`RunRelease` activation, normalized config and dual-upload composition have
their separate component results below. Bounded scan-prefix reuse and
historical-only foreign-nonce resolution now have the separate qualification
below; remaining startup, release and live-authority work keeps MG-04 and PH-03
incomplete. The separately
[qualified native HTTP integration](evidence/production-native-http-integration-20260928.md)
connects physical native causes to phase-owned waits and corrects response/close
boundaries. Composed native/EVM startup failures require their later scope.

The integrated [public startup continuation](evidence/production-startup-continuation-candidate-20260928.md)
reopens original native observation at its signed activation block before
unrelated current preparation. The public `RunRelease` root still authenticates
activation, both operator histories, real disk ownership and original intent
custody; current UID/stake and initial settlement publication gate fresh intents
and trail workers. Local semantic replay uses the caller's lifecycle rather than
a five-minute I/O deadline. Independent transient native/EVM branches compose
without losing their physical causes or hiding mixed integrity errors.
Its affected roots now have normal/race coverage, five causal controls reproduce
the intended failures in both modes, and validator vet passes. The receipt
preserves original fixture failures, reused passing scopes and the missing
pre-execution dependency-content seal; final release composition remains open.
The empty-store case requires independently
provisioned client identities, keys and existing JWTs; first-client registration
is not part of that claim. Both operator server-key/public-object/session routes
remain startup dependencies, and initial missing-JWT registration failure can
still return from the root after disk replay. That potentially mutating identity
operation needs its own durable reconciliation, not generic read retries.
Native production submission uses an explicitly approved WS/WSS route for
`author_submitAndWatchExtrinsic`; the same node may provide HTTP EVM/read RPC,
but HTTP read support alone grants no native subscription or writer capability.

The [operator registration candidate](OPERATOR-REGISTRATION.md) addresses the
separate first/missing-client barrier with durable request/identity custody, a
dedicated idempotent server route and an independent authentication worker in
the actual public root. Retained native observation is independent of live JWT
readiness; new publication, trails, signing and rebroadcast remain gated. Fresh
creation requires signed per-operator `allow_client_registration`, default false
with historical encoding preserved. Existing operations reconcile regardless
of that flag. Its [withdrawal recovery correction](evidence/operator-withdrawal-ownership-qualification-20260928.md)
is integrated: 20 affected roots and both causal controls pass normally and
under race detection. Original failed parent captures remain retained. Final
dependency composition and the additive server-first migration/rollout remain
pending; no live identities or deployment are supplied by this source change.

The qualified and integrated [provider client-registration change](evidence/provider-client-registration-candidate-20260929.md)
migrates `provide` and `auth-provide` to the existing versioned API protocol.
The provider seed and original request are durable before allocation; direct
and proxy slots retain their actual endpoint/key identity without a fabricated
validator or chain identity. Explicit new-registration permission and explicit
legacy-key adoption are separate decisions. Registration retries and required
refresh/logout writes stay bound to the original physical custody directory.
All 114 selected roots pass per normal/race mode (228 executions), and all 17
causal variants are valid in both modes (34 executions). The selection covers
114/2324 package roots; it is not full miner/validator coverage. Original e32
fixture failures remain sealed separately. Daemon fixtures reach authenticated
handoff before serving-device construction, with separate callback fixtures.
The no-config measurement validator's durable primary identity has its separate
qualification below. API deployment, real custody/adoption decisions,
processed-key and proof readiness, chain/contract authority and all live launch gates remain
open; this component does not close MG-04 or PH-13.

The qualified and integrated [measurement primary-client change](evidence/validator-measurement-client-registration-candidate-20260929.md)
addresses that no-config validator path. It retains the original key and a
distinct direct `validator-measurement-v1` scope, always uses `allowCreate=false`,
and adopts an existing JWT or replays its exact retained operation.
Unknown lost identity requires recovery. Registration and refresh borrow the
original physical key directory; completed authentication releases the separate
bootstrap lock while retaining the key owner through joined shutdown. The
runner bypasses key regeneration and returns through cleanup instead of an
inner process exit. The `c3fe0cf2` parent remains unqualified: its CLI fixture
used docopt's process-exiting parser and aborted both focused validator packages.
Test-only child `e33f64d4` preserves production bytes and uses non-exiting fixture
parsers. Independent normal/race qualification passes all 24 focused plus 13
adjacent roots in each mode across twelve package PASS/exit-zero streams, and all
38 causal executions reach their intended assertions. Separate Astra and
integration audits verify the sealed results and exact source/module graph;
integration adds only documentation above that source. This is selected coverage.
Existing ephemeral tunnel-client allocation, legacy release-config callers and separate
proof/stats history ownership remain outside this primary-identity repair.
Its local lifecycle fixture reaches seed discovery and shutdown, not live trail
completion. API deployment, actual custody, approved chain/Safe authority,
economic acceptance and independent monitoring remain required; MG-04 and PH-13
stay open.

The [qualified receipt-prefix recovery](evidence/receipt-prefix-qualification-20260928.md)
now supplies the shared 128-block scan contract to both native recovery owners.
Miner records retain their existing signed semantic proof. The validator keeps
one bounded, original-intent-bound signed acceleration file; cache eviction or
an incompatible cache schema causes a rescan while custody and exact signed
bytes remain intact. Completed chunks and admitted partial prefixes persist
before subsequent reads, including retry after a later timeout. Optional cache
read/write faults disable disk acceleration and report degradation while original
intent reconciliation continues; memory-only progress is not a durability claim.
The separately qualified public diagnostic exporter now exposes degradation. No
per-block full journal rewrite or executable-hash invalidation is introduced.
Pending old approvals can resolve authenticated foreign nonce use at the exact
covered boundary while fresh signing/rebroadcast stays independently gated.
All 56 affected roots have scoped normal/race passes after retained fixture
corrections; nine causal families and package vet pass. The receipt preserves
failed captures and the incomplete pre-execution seal for one fixture scope.
Final release composition, first-client recovery and live authority remain open;
this component does not close MG-04 or PH-03.

The [server upload admission](VALIDATOR-UPLOAD-RUNTIME.md) now consumes an
independently pinned schema-3 configuration and retains only read-only runtime
intervals, routes and deployment scope. Original activation reads use their
signed historical window; current eligibility requires the unexpired current
window. Its detached projection includes validated original authority bundles
without carrying economic configs or signing authority. The ordinary upload signer and server-used admission constructor are
joined in local deterministic tests. This closes the downstream tuple-only
history gap without granting writer capability or proving remote delivery,
original economic authority, mainnet deployment or live approval custody.

No implicit apply, automatic subnet creation, private-key CLI flags, “force” bypass, mutable `latest` artifact, or inherited network defaults. Every mutating command takes an explicit run directory and accepted plan hash. Read-only discovery and pure review plans may run while other gates remain unresolved; their explicit input checks still apply. Mutating phases require accepted plans, actual production authority and durable custody.

The canonical plan binds schema and action-format versions; exact config/policy bytes; resolved configuration roots and runtime routes; source/dependency/artifact/binary identities; owned-node and runtime identities; native/EVM snapshot hashes; all public roles; census and reset classifications; actual transaction payloads/origins; expected CREATE addresses and nonces; phase dependencies; validity windows; spend/count caps; and the chosen emission-denominator/remainder policy. Hash canonical bytes with domain separation. The signed authorization names that hash, network, expiry, allowed phases and ceilings. Reject duplicate fields, unknown schema versions, overflow, unexpanded substitutions and ambiguous addresses.

Implemented review commands, with no signing or submission:

```sh
sn-mainnet plan --outline > /secure/ur-mainnet/review/outline.json
sn-mainnet plan --config /secure/ur-mainnet/plan-config.json > /secure/ur-mainnet/review/blocked-plan.json
```

The current [JSON schemas](PLAN.md) are `urnetwork-mainnet-plan-config-v2` and
`urnetwork-mainnet-release-input-v2`. Bound review carries inline
`treasury_destination` with the public destination schema, native account,
genesis and any supplied recipient hotkeys; it carries no reserve signing
fields. The default outline and current bound review use
`urnetwork-mainnet-blocked-plan-v3`, with the `1/10` provider and `9/10`
ordinary-native-treasury target and a `treasury-policy` requirement. Reserve
signing custody and an owner Recycle-mode transition are not gates for this
receive-only plan. Actual registered generations and independent signed
economic/runtime authority remain required. Explicit historical v1 config and
release schemas retain the original owner-recycle v2 review output.

The resulting blocked-plan hash cannot be passed as executable apply authority.
Current review preserves the separation of action preconditions from produced
postconditions: deployed getter
proofs follow installation, revealed/applied validator rows follow activation,
and realized native economics follow the first approved submission. None is a
circular prerequisite to its own producer.

The separate implemented local phase uses its own strict JSON schema and exact
independently approved root custody packet:

```sh
sn-mainnet bootstrap plan --config /secure/ur-mainnet/bootstrap-root.json > /secure/ur-mainnet/review/root-custody-plan.json
sn-mainnet bootstrap apply --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
sn-mainnet bootstrap resume --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
```

[BOOTSTRAP-ROOT.md](BOOTSTRAP-ROOT.md) specifies its public-signature import and
exact recovery contract. It does not perform owner trim, contract installation,
UR activation or a root broadcast. Complete contract installation must include
the `STValidatorEvidence` journal and its anchor/coordinator binding; the
existing `Deploy.s.sol` alone is insufficient. The testnet EVM945 deployment
manager is not a mainnet adapter.

The remaining operator examples below are future interfaces requiring an executable
schema and complete semantic admission; the implemented `plan` does not accept
the draft YAML config, `--snapshot`, `--phase` or `--out` flags.

```sh
sn-mainnet inspect --config /secure/ur-mainnet/bootstrap.yml --out /secure/ur-mainnet/inspection
sn-mainnet apply --plan /secure/ur-mainnet/review/plan.json --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json --run-dir /secure/ur-mainnet/run
sn-mainnet status --run-dir /secure/ur-mainnet/run
sn-mainnet resume --run-dir /secure/ur-mainnet/run --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json
sn-mainnet verify --run-dir /secure/ur-mainnet/run --out /secure/ur-mainnet/verification
```

This future executable YAML config sketch is neither current JSON input schema.
It intentionally contains `null` for unapproved identities and monetary values.
A real executable plan must reject them. Values represent required fields, not
suggested budgets or fake addresses; secret material is supplied through local
signer references rather than embedded here.

```yaml
schema: urnetwork-mainnet-bootstrap-v1
network: mainnet
deployment_id: null
netuid: 25
owned_node:
  substrate_url: null
  evm_url: null
  ownership_attestation: null
  expected_genesis_hash: null
  expected_evm_chain_id: 964
  runtime_artifact_manifest: null
  rpc_pacing: none
  fallback_urls: []
release:
  source_lock: null
  contract_manifest: null
  binary_manifest: null
  policy_file: null
  closed_testnet_report: sim-testnet/FINAL-4.md
  known_exceptions_manifest: null
  production_qualification_manifest: null
roles:
  subnet_owner: null
  evm_deployer: null
  coordinator_safe: null
  guardian: null
  commitment_oracle: null
  root_validator: null
  ur_validator: null
  independent_ur_validators: []
  reserve_hotkey: null
  escrow_hotkey: null
reset:
  mode: unresolved
  census_file: null
  remove_generations_file: null
  preserve_identities_file: null
  registration_during_cutover: null
emissions:
  denominator: native_miner_allocation_before_withholding
  provider_fraction: {numerator: 1, denominator: 10}
  assurance: observed-native-target
  quantization_tolerance_manifest: null
  remainder: owner-recycle
  mechanism_manifest: null
  activation_boundary: null
root_validator:
  registration_mode: null
  auto_parent_delegation: false
  basket_strategy: accumulate_in_place
  custom_weights: []
  stake_rao: null
  delegate_take: null
ur_validator:
  release_config: null
  stake_plan: null
  independent_validator_evidence: null
limits:
  total_tao_debit_rao: null
  total_alpha_commitment_units: null
  evm_fee_wei: null
  native_fee_rao: null
  per_registration_burn_rao: null
  root_registration_debit_rao: null
  maximum_registrations: null
  maximum_subnet_creations: 0
  maximum_transactions: null
  stake_price_limits: null
  expiry_finalized_block: null
operations:
  run_dir: /secure/ur-mainnet/run
  signer_manifest: null
  service_manifest: null
  worker_limit: null
  monitor_manifest: null
  independent_reader_manifest: null
  telemetry_and_alert_routes: null
  slo_manifest: null
  repair_authorization: null
  primary_on_call: null
  backup_on_call: null
  incident_evidence_store: null
  rollback_compatibility_manifest: null
```

The complete schema also requires action-level value/gas/fee bounds, collateral exposure, swap price/minimum-output limits, claim/deposit policy caps and fee reserves. Totals aggregate economic debits once across EVM and native representations. Refunds are recorded separately; they do not replenish lifetime authorization unless the plan explicitly defines that rule. Reverted transactions consume fee budget. New attempts, repairs and replacements retain the same lifetime ledger.

## Phases, finality and recovery

| Phase | Admission and work | Completion evidence |
| --- | --- | --- |
| 0. Evidence and production qualification | Preserve failed R48 and known exceptions; compose the launch release and close applicable MG-02 through MG-07 pre-activation checks on a controlled production-path rehearsal. Read-only identity discovery may proceed independently. | Exact release/qualification/exception manifests; no fabricated testnet pass or provisional authority carried into mainnet. |
| 1. Mainnet inspect and review | Verify node/runtime, complete census, owner authority, capabilities, keys, artifacts, the no-trim decision (October 7), the receive-only treasury mechanism that superseded owner recycle, and budgets. | Canonical feasible plan and exact operator authorization; any expected retained old miners have an explicit launch disposition. |
| 2. Cutover | No owner trim for the SN25 launch (owner decision, October 7): old registrations stay for ordinary pruning. Apply configuration changes within their native windows and register UR's zero-emission keys within their immunity. | Census and protection set recording the protected identities and the no-trim decision; retained old miners are reported, never labelled a reset. |
| 3. Contracts and registration | Deploy exact custody graph, anchor evidence, register approved pool/escrow/head/validator identities. | Canonical finalized receipts, code/getter proofs and registration ownership. |
| 4. Stake and service readiness | Apply bounded stake/deposit plans; admit both services, the policy's UR validator/operator safety set (one of each at the SN25 launch), independent monitor and on-call. | Current root membership, UR eligibility, authenticated runtime configs, single service ownership, delivered test alerts and qualified repair/rollout policy. |
| 5. Emission activation | At the approved native boundary, activate the qualified 10% mechanism and corresponding signed UR policy. | Native incentive outcome, remainder destination, weights, stake/collateral deltas and policy epoch agree. |
| 6. Acceptance and operations | Observe the specified production interval, settle/claim genuine accrued emission and reconcile all funds. | Self-contained final report; all four requested outcomes satisfied with no open cap/custody exceptions. |

Phases form a dependency graph, not a best-effort list. The final activation boundary may need a new snapshot and plan revision after lengthy setup; revisions authenticate the prior plan and completed receipts, preserve original immutable identities and lifetime caps, and explicitly authorize changed future actions. They do not rewrite the prior plan, retroactively approve execution, or reset spend.

Persist a write-ahead, hash-linked action journal with states such as `planned`, `intent-recorded`, `signed`, `submitted`, `included`, `finalized`, `postcondition-verified`, `failed` and `canceled`. Append and fsync the exact intent and signed payload before broadcast, with mode-0600 protection for recoverable signed bytes. Public evidence contains no secrets. Journal records include parent plan/action hashes, full origin/domain, payload hash, nonce, value/fees, transaction hash, actual block/receipt/event positions and the authenticated postcondition snapshot.

Use atomic manifest/config writes and one exclusive run owner. Native account and EVM/Safe nonce ownership must be explicit, including whether two representations share an underlying account. Serialize one-shot setup dependencies. Pipeline independent operations only after adapter-specific evidence shows nonce, finality and spend recovery remain correct. A timeout, canceled watch or lost RPC response is not proof that a transaction failed; search for the exact signed transaction before deciding to rebroadcast or replace it.

Native success requires finalized inclusion and successful dispatch at the correct extrinsic index. EVM success requires a canonical receipt and the corresponding native finality mapping, plus exact event/getter/code readback. Decode events with that block's authenticated metadata. Where batched calls can partially succeed, record every child result; prefer an actually atomic batch when the desired action requires all-or-nothing behavior. Never infer child success from the outer batch alone.

Recovery rules:

- For CREATE, find the original nonce transaction and compare exact address, code and immutable parameters; do not redeploy because a local marker is missing.
- For registration, compare hotkey ownership and generation, not only UID presence. A foreign occupant is a conflict, not an idempotent success.
- For one-shot links and initialization, an exact existing value is reusable only with authentic receipt/precondition history. A conflicting initialized value stops recovery.
- For stake, deposits and capture, reconcile actual deltas and retained-source balances before any retry. Never repeat an economic action because its terminal response was lost.
- For local rendered configuration, bind format, resolved route, paths and authority/capacity inputs in the action intent. An approved new local render can converge stopped services; it cannot pretend an old receipt already proves new bytes.
- For superseded actions, authenticate the historical postcondition at its source and the specific finalized successor that authorizes current state. Do not demand obsolete live equality after an approved successor, and do not globally waive current checks.
- On cancellation, stop new signing, join submitted transaction owners and service children, persist the actual terminal state, and report `CANCELED`, not `PASS`. An incomplete deployment or native inclusion remains recoverable work.

There is no automatic rollback of a finalized UID removal, registration burn or immutable deployment. Recovery uses a newly reviewed bounded forward action where supported. Old custody contracts and claim artifacts remain served until their obligations have actually ended. An emergency service stop does not imply that native emission stopped or that the vault may stop honoring claims.

## Continuous monitoring and repair

The [stopped-validator repair increment](VALIDATOR-REPAIR.md) adds a concrete
`repair-validator claim|resume|status` path under an independently signed,
expiring fixed-unit/release/host/generation envelope. It can consume one durable
start only after the approved prior generation is stopped and its descendant
cgroup is empty, then retains the acknowledged invocation and exact-source
progress postcondition. An unacknowledged consumed start is explicitly uncertain
and cannot retry automatically. [Offline qualification](evidence/validator-repair-qualification-20260930.md)
is sealed for exact source `af570cdc`: 52 positive normal/race executions passed;
twelve normal and seven selected race controls were causal. Admission includes
the genuine cgroup-v2 filesystem and post-sync authority/sample-age rechecks.
This increment installed no unit and issued no live start. Production active-hang
rehearsal, root/operator services, initial activation, independent RPC, delivered alerts
and monetary repair remain open.
The host deployment owner must exclude concurrent privileged service or file
changes; the local journal lock does not provide that exclusion by itself.

Mainnet operation needs three separate owners: an independent read-only monitor,
service supervisors, and a bounded repair controller. The monitor observes and
reports; supervisors recover an approved process generation; the controller
executes only already authorized actions. Implement and rehearse this separation
under MG-07/PH-28 before production activation. The current `monitor` command
supplies identity/finality observations and an optional read-only
[`--services` consumer](SERVICE-MONITOR.md) for independently configured
validator roles.

The [steering responsiveness increment](SERVICE-MONITOR.md#steering-responsiveness)
adds explicit per-role loop-outcome budgets and persistent critical incidents
for an observed steering loop that stops returning while its publisher remains
fresh. A returned read/reveal/epoch wait remains responsive; startup without a
baseline stays unknown. Checkpoint v4 retains the exact episode through restart,
source loss and policy edits, and requires a later actual outcome for recovery.
Its [source qualification](evidence/steering-liveness-qualification-20261001.md)
includes command/producer boundaries, compatible v3 repair custody and independent
expected-host alert fixtures. No live service was changed. Approved production
budgets and delivered alerts remain open; the stopped-repair controller still
refuses active generations.

The separate [active steering-hang repair](ACTIVE-VALIDATOR-REPAIR.md) adds an
independently signed exact incident/role/release/host/generation capability.
It retains one stop and one start, a finite descendant-cgroup join, permanent
generation custody across alternate envelopes, and the complete original
checkpoint. Current recovered/unknown steering or policy/identity drift refuses
stop, including after durable reservation. Unacknowledged starts remain manual;
completion requires an actual new steering outcome. Its
[source receipt](evidence/active-validator-repair-qualification-20261001.md)
does not grant a production envelope or activation. Real-systemd stop behavior,
exclusive host/signer custody, anti-rollback policy and on-call rehearsal remain
P0 gates. No live service was changed.

The [operator journal monitor increment](OPERATOR-MONITOR.md) has an
[offline-qualified production reader/consumer](evidence/operator-monitor-qualification-20260930.md):
62 Go root executions passed normal/race, four alert fixtures passed, and nine
normal plus five selected race controls were causal.
It observes actual read-only PostgreSQL transaction/attempt and settlement-mirror
projections through the existing monitor owners, retaining domain incidents
across outage/restart. Fresh DB access and empty pending counts do not establish
chain success. Independent RPC, provider/client-key readiness, full liabilities,
root-validator progress and protocol deadlines remain unknown. The separate
stopped-validator capability requires its own independently approved envelope;
monitor observations alone grant no repair authority.
Deployment, dedicated read-only credentials, query-load qualification and alert
delivery remain open MG-07/PH-28 gates.

The [read incident continuity increment](READ-INCIDENTS.md) preserves stable
per-role outage IDs, first/latest failures, successful-read recovery evidence and
recurrence through checkpoint restart. Read recovery does not establish service
health or grant repair/spend authority. The bounded summaries need independently
retained events for complete incident timelines. Normal/race qualification,
covering all 82 affected roots and six causal controls in both modes, is recorded
in the [qualified read incident receipt](evidence/read-incident-continuity-qualification-20260929.md)
for integrated source `1bb311fc`. Compatible checkpoint rollout, deployment and
alert delivery remain pending.

The standard validator now has a qualified optional
[`--progress-file` producer](SERVICE-PROGRESS.md). It reports bounded intent and
settlement observations without acquiring another protocol reader or signer;
publication failures do not cancel validation. Its separate heartbeat,
successful-observation, durable-progress and publication-acknowledgment times
must remain distinct in dashboards. The consumer now retains these facts across
outages and restart, with separate per-role checkpoints and atomic metrics.
[Qualification](evidence/service-monitor-qualification-20260928.md) passed 48
selected normal/race roots, five controls in both modes and service/chain alert
rules. Delivered alerts, other production domains, supervision and the repair
controller remain deployment or implementation work. A fresh file alone is not
proof of healthy validation. The optional [native deadline observer](NATIVE-DEADLINES.md)
adds explicit completion margins, submission-window forecasts and retained
reported epoch misses through the actual per-role worker. It grants no receipt,
signature-expiry or success authority; late applied reports cannot clear a
historical incident. Its [component qualification](evidence/native-deadline-qualification-20260928.md)
passes 35 selected roots and five causal controls in both normal/race modes,
plus native-deadline and inherited service alert fixtures. Production margins,
deployed alerts and authoritative incident resolution remain pending. The integrated
[bounded output correction](evidence/bounded-diagnostic-output-qualification-20260928.md)
now isolates validator startup, steering and runtime diagnostics, plus these
chain/service workers, from blocked log destinations. Its 76 affected roots have passing scoped normal
and race coverage, with six regression controls in both modes, vet and three
offline alert-rule suites. Each role retains finite output capacity and actual
delivery/loss counters. Deploy the progress consumer before the producer's
optional diagnostic extension, and retain independent missing/stale-file alerts.
The [root-service/root-monitor output follow-up](evidence/root-output-qualification-20260928.md)
is now integrated and component qualified: 41 roots have passing normal/race
coverage, six causal families reproduce their intended failures, and vet plus
seven offline alert rules pass. Root-monitor now emits compact event v2 and
optional independent metrics; deploy compatible log consumers first. Finite
preview v1 still supplies the full census. The actual trail worker and its
proof-warning path now have their separate
[bounded-output qualification](evidence/trail-diagnostic-output-qualification-20260928.md):
all 17 affected roots have passing normal/race coverage and five causal controls
reproduce the intended failures. Physical full-pipe checks cover real proof
progress and joined cancellation. Required ledger/proof failures remain hard.
The [miner output and shutdown correction](evidence/miner-diagnostic-output-qualification-20260928.md)
is also integrated: 18 affected miner roots, the actual Warp status reader and
four causal controls passed their normal/race checks. Its optional diagnostic
status extension reports local delivery separately from process liveness;
consumer compatibility must precede rollout. The separate
[diagnostic cause isolation correction](evidence/diagnostic-cause-isolation-qualification-20260928.md)
is integrated and passes 20 affected roots and five causal controls in both
modes. It never invokes arbitrary error methods for optional labels and leaves
opaque causes unknown. SDK/internal logging
and final registration composition remain separate scopes. Local output acknowledgment
does not establish remote ingestion or alert delivery.

### Independent observations and existing telemetry

Run the monitor separately from bootstrap and validator/taskworker lifecycles,
without signing keys, database write credentials or authority to stop those
services. Give it its own bounded read budget, durable finalized-block cursor,
incident store and health signal. Replay from the last verified checkpoint after
restart; never replace missing observations with an assumed healthy interval.
Subscriptions wake readers but do not establish finality. Pin events, storage,
runtime interpretation and native/EVM mapping to the same authenticated block.

Compare the approved owned mainnet RPC against a separately operated,
independently authorized canonical source at the **same finalized block hash**.
Different latest heights alone are lag, not a reorganization. Validate both
identities, their available archive scope and the same transaction/event/storage
facts; preserve disagreements. The second source is read-only and cannot become
a submission fallback. Until it is provisioned, expose `independent_rpc=false`;
Snow and the LAN alias of one backend provide no independent confirmation.

Export SN metrics and structured incident events into the existing xops
Grafana/Mimir/Loki stack, using its Prometheus-compatible exporter and host
Fluent Bit paths. Reuse [deployment infrastructure](../../xops/main/ansible/playbook-dbs.yml)
and [telemetry isolation requirements](../../xops/VULNSCAN2.md): restricted
telemetry identity, scoped credentials, bounded journald retention and durable
log cursors. Do not mount signer material, a full vault or Docker control into
the observer or dashboard. Keep bounded metric labels to deployment, component,
role and error class; put transaction hashes, client-level detail and exact
evidence references in the incident store. Monitor telemetry delivery itself
through a separately hosted dead-man alert and named escalation route.

The [October 2 offline deployment increment](evidence/monitor-deployment-offline-20261002.md)
implements the gap from the earlier source audit at exact xops `b98f8769`:
explicit disjoint monitor/observer hosts, a release- and config-pinned monitor
unit, a dedicated textfile-only Fluent Bit instance, and an independently owned
expected-host/role roster and alert evaluator. The shared fleet collector census
is unchanged. New production policies require the exact native deadline schedule
profile and 60-second GET retries. Staging starts nothing; separate approval
must bind the host/chain/release/config bundle before activation, with physical
custody and actual service-user credential checks repeated at restart.
Snow's testfinney/runtime-471 configuration remains untouched and refused.
Author and independent offline qualification pass on the exact source pins:
32 new and 19 adjacent xops tests, 13 SN monitor roots normal/race, vet and
controller/unit/credential controls. The separate independent receipt is
bound in the evidence above. Selected live hosts, actual collector ingestion
and delivered missing-host/recovery alerts remain
explicit launch gates. An unsent delivery-drill payload is not a delivery
receipt.

Every dashboard distinguishes unavailable, pending, healthy and failed facts:

| Domain | Evidence and progress to observe |
| --- | --- |
| Chain and authority | Genesis/EVM domain, approved runtime capabilities and code/metadata, finalized age/height, native/EVM mapping, node agreement, endpoint/config/release drift and archive availability. |
| Validators | Every UR validator's hotkey ownership, permits, non-self eligibility, fresh proof domains through each operator, native source/EMA continuity, durable intents and finalized revealed/applied weight rows; the SN25 launch has one UR validator. Root seat, stake/retention margin, child delegation and basket are a separate role. |
| Operators and providers | Current policy and evidence activations, migration version, processed client-key registrations and peer pins, ready provider count, fresh signed usage and bounded queues. HTTP 200 and process liveness do not establish registration or proof success. |
| Settlement and treasury | Exact source epoch/root/artifact, immutable usage snapshots and uncredited debt; required/observed deposits under the selected policy; pool capture, carry, commitments, finalization, claims and outstanding liabilities. Reconcile native units, collateral/principal and fee/lifetime allowances. Measure the 10% native target and approved tolerance independently of claimed payouts. |
| Transactions and deadlines | Every signed attempt, nonce owner, uncertain send, replacement/cancellation, canonical receipt and postcondition; blocks remaining to policy, commit, reveal, renewal, claim and evidence-retention deadlines. |
| Services and resources | Process generation and restart count, last useful checkpoint, database/artifact health, CPU/memory, RPC concurrency, queue age, log byte lag and disk bytes/inodes. No healthy status from a stale lock file. |

### Alert taxonomy and initial SLOs

The following are proposed starting targets. Freeze them in the operations
manifest after a representative load/recovery rehearsal and before activation.
They are not claims of measured availability. Protocol deadlines remain exact
block boundaries; human response targets cannot extend them. Use a lightweight
health loop while expensive replay proceeds under a separate finite budget.

| Alert class | Starting detection/SLO target | Response |
| --- | --- | --- |
| Integrity, authority or accounting conflict | Emit immediately on an authenticated wrong-chain/domain, finalized-hash conflict, invalid signature, custody/conservation mismatch or unauthorized spend. No averaging or transient-error allowance. | Critical page; suspend dependent new signing through its owner and preserve evidence. Continue independent observation and valid claim service where safe. Primary acknowledges within 5 minutes; backup escalation after 5 minutes without acknowledgement. |
| Monitor or alert path absent | Target a health/progress event at least every 30 seconds; warn after 90 seconds, page after 2 minutes without one. Test alert delivery before activation and after routing changes. | Independent dead-man page; restore observation first. Missing monitor samples remain a gap, not a healthy interval. |
| RPC/read availability or stalled finality | Record every error as unavailable; warn after 2 minutes of persistent read failure, page after 5. Warn at 3 minutes without finalized advance and page at 5, after calibrating to admitted chain cadence. | Bounded retries/reconnect on approved routes, inspect chain-wide versus node-local failure, and block new actions lacking required fresh evidence. Never compare an unread default value with an approved one. |
| Deadline or readiness risk | Recompute at least every 30 seconds and on each new finalized block. Warn when remaining blocks fall below the greater of 20% of the window and twice measured p95 completion/finality cost. Page when the admitted completion margin is no longer available, or a required role remains unavailable for 2 minutes. | Resume the exact pending action if authorized; otherwise escalate a concrete forward plan. Record a missed boundary as missed. Every UR validator and every required operator/domain must remain independently visible. |
| Settlement and reward deviation | Evaluate every due finalized event/epoch and native emission interval; immediate critical alert for conservation or authority failure, deadline alert for missing work, explicit alert for a 10% result outside approved `Q(k)`. | Trace source usage through liabilities and receipts. Do not fabricate usage, increase a governed deposit to a native minimum, or count a late root as timely. |
| Resource exhaustion or replay backlog | Warn below 20% free bytes/inodes or when forecast capacity is under 24 hours; page below 10% or a shorter time than safe intervention. Alert if log/queue lag exceeds its approved window or foreground work loses its completion margin. | Reduce bounded background admission or restore capacity within policy; never delete signed evidence or increase spend/capacity authority silently. |
| Storage read/media error or failed copy verification | Page on a kernel/device unrecovered read, failed backup verification or incomplete evidence migration even if free bytes and service health look normal. The [qualification-host incident](evidence/qualification-host-media-error-20261001.md) is a design lesson, not evidence about Snow. | Preserve the original source and failed-copy record; stop relying on the destination, assess device health, restore from verified independent backup and prove exact evidence/release bytes before resuming affected work. |
| Recovered incident / recurring degradation | Retain first failure, retry count, recovery evidence and recurrence by stable incident ID. Noncritical pages acknowledged within 15 minutes; unresolved incidents carry an owner and next action. | Review open incidents daily and recurrence/capacity/runtime-change trends weekly. Create a scoped fix with causal regression and affected-path qualification. Recovery does not erase the failure. |

Select recovery-time objectives from real replay and protocol windows before
launch. Durable intent and signed-transaction recovery has **zero tolerated loss
of acknowledged records**; a retry may repeat observation but may not repeat an
economic effect. Local crash recovery requires fsync and restore evidence. A
zero-loss host-failure objective additionally requires independent durable
replication before acknowledging/broadcasting signed work; periodic backups
alone cannot provide it. Bind that recovery design before enabling automated
spend. Publish actual recovery times and observation gaps alongside the target
after each exercise or incident.

### Repair authority and durable execution

Service stop/drain must use retained process ownership even when replacement
configuration is invalid or RPC is unavailable. Verify the exact process
generation and join its children; retain journals, uncertain signed work and
volumes. Campaign closure records which services remain necessary and proves
the others stopped. The [testnet shutdown observation](evidence/closed-testnet-service-stop-20260928.md)
shows why publishing a terminal report alone is insufficient. Qualify this
path before production service activation.

Provide a standing signed repair envelope for routine operations the operator
chooses to automate. It binds chain/deployment, immutable release, allowed
action kinds and exact targets, signers/nonce domains, prerequisites, expiry,
maximum attempts and action counts, per-action value/gas/fees, total lifetime
debits, price/minimum-output limits and permitted postconditions. Automation can
continue within that envelope without asking again for each identical retry.
Changing its scope or exceeding a bound requires a new exact reviewed plan.
Default monetary limits are zero until supplied; alert severity never grants
transaction authority.

| Action | Automation boundary |
| --- | --- |
| Reconnect/retry reads, replay authenticated immutable evidence | Allowed within the monitor/worker's finite budget and unchanged authority. Preserve successful checkpoints and typed failures. |
| Restart an approved service or resume a stopped worker | Allowed by its service manifest only after joining the old process/children and retaining signer, volume and configuration identity. Cap restarts; escalate exhaustion. |
| Reconcile a previously signed transaction | Read receipts, dispatch, postconditions and nonce state automatically. Rebroadcast the exact bytes or make a replacement only when the owning approved action explicitly permits it and its bounds still hold. |
| Scheduled renewals, routine claims or approved funding repairs | Automatic only under their own signed targets, amount/count/price/deadline caps and lifetime ledger. A schedule alone is not spending permission. |
| Policy/rate/source/native-history changes, new registration or stake, destructive reset, custody/contracts, runtime admission, release/schema or endpoint changes | Operator-gated exact plan with the actual required coldkey/Safe/governance authority. No self-approval, permission widening, backdated success, historical signature rewrite or guessed SQL credit. |

Use one durable action/nonce owner shared with the production submitter. Append
and fsync an incident-bound intent before signing, then exact signed bytes before
broadcast. Journal `planned → intent-recorded → signed → submitted → finalized
→ postcondition-verified` with explicit failed, canceled and unresolved branches.
Store replacement/cancellation attempts separately; retain all paid fees and
outstanding liabilities across releases and retries. Before any repeat, locate
the original exact hash and reconcile canonical inclusion, dispatch, finality,
nonce and economic postcondition. A timeout is an unknown outcome. A new nonce
or a local database status is not evidence that the old action failed.

The [configured native HTTP read adapter](evidence/native-http-read-causes-20260928.md)
preserves status and physical body failures within the existing finite read
budget. Production consumers can distinguish that typed unavailability from
complete malformed evidence and mixed integrity errors; writes retain their
separate original-byte reconciliation policy. The 30-root component normal/race
qualification passed. The [production integration](evidence/production-native-http-integration-20260928.md)
also passed its 23-root normal/race scope and three regression-control families.
It admits only pure connection failures from HTTP body
close after releasing the response and discarding idle connections; local file
close, cancellation and mixed integrity causes remain hard. Its finite wire cap
includes the existing 16 MiB event value as hex plus 64 KiB of JSON framing.

The [operator receipt-census correction](evidence/operator-recovery-census-20260927.md)
preserves this boundary in the production account reconciler: if a retained
candidate's receipt cannot be read and no other candidate is canonical, the
intent remains unresolved. An advanced nonce cannot erase that unknown outcome,
and elapsed replacement time cannot turn the failed read into new signing
authority. MG-03/PF-03 still require live recovery composition, canonical
historical-status and receipt reconciliation, and actual fee accounting.
The [status-independent census source](evidence/operator-signature-census-qualification-20260928.md)
now reads selected databases and retained RLP stores without status filtering,
preserves original/replacement/cancellation provenance, and supports private
byte-preserving archive restoration. Its normal/race qualification covers both
new roots and adjacent controller/model recovery. Production receipt/finality
joins, actual fees, distributed custody ownership and live restart remain open.
The [conditional offline receipt/fee join](evidence/operator-receipt-fee-qualification-20260928.md)
now reports missing and conflicting candidates and counts observed gas once
per resolved nonce; its recovery and adjacent controller/model roots passed
normal and race modes. A separately authenticated native-to-EVM mapping,
owned-node receipt capability and exact runtime fee evidence are still required
before it can establish canonical production fees or authorize recovery.

The [qualified receipt commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md)
is now integrated at server `fbe0c039`. Its offline `verify-receipts` command
authenticates exact archived signed bytes and receipt status at the same trie
index, derives gas from the committed receipt and its predecessor, and verifies
consecutive raw Frontier headers through the supplied EVM boundary. All 52
recovery/CLI roots pass normal and race, including 14 new roots and three
private-database roots; four causal controls pass. This closes source-level
verification of those commitments only. Actual fees remain null: the raw Frontier
header does not commit the RPC-rendered base fee, and runtime/native debits
remain unproven. Independent native finality/mapping, boundary account nonce
proofs, owned-node collector capability, service adoption, release
composition and live custody/restart qualification remain open MG-03/PF-03 work.

The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md)
is integrated at server `b7c8c743`. Its `collect-receipts` command reads every
archived signed candidate, authenticates complete raw block/receipt vectors,
builds the proofs and replays the verifier before publishing one private,
create-only evidence file. `verify-collection` replays it entirely offline.
Transient reads retain exact selectors within configurable 60–900 second retry
windows (default 300), a total deadline and shared request/byte budgets.
All 71 affected recovery/CLI roots pass normal and race, including 19 new roots
and three private-database roots; five causal controls pass. The original failed
publication-mode assertion and its test/documentation correction remain retained.
This qualifies collection source only: node capability and the supplied native/
EVM mapping remain explicitly unapproved, actual fees remain null, and no
finality, canonical-accounting or spending authority is created.

The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md)
is integrated at server `44636e5e`. Its offline `verify-finality` command checks
GRANDPA weighted certificates, native header ancestry and delayed scheduled
authority handoffs, then binds the exact native Frontier digest to the
collection's raw EVM header. All 89 affected recovery/CLI roots pass normal
and race, including 18 new roots and three private-database roots; five causal
controls pass. These proofs are relative to a separately pinned checkpoint.
No independent genesis/checkpoint approval or deployed-runtime provenance is
supplied, and all finality, canonical-accounting and spending authorization
flags remain false. Actual fees remain null: native denomination conversion,
debits and best-effort refunds require authenticated runtime evidence beyond
receipt gas or reported prices. Checkpoint admission, account nonce proofs,
service adoption, release composition and live
custody/restart remain open MG-03/PF-03 work.

The [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md)
is integrated at server `5ff7bf02`. `capture-finality` retains exact native
headers, stored certificates and durable request/byte reservations, then replays
the existing offline verifier before private proof publication. Proof v2 accepts
a bounded certified descendant while preserving the original collection boundary
and its exact Frontier mapping. Completed restart replays offline; partial
evidence and spent budgets survive failed reads. All 112 affected roots pass
normal/race, including 23 new roots and three private-database roots; seven causal
controls discriminate in both modes, and vet/source/module fences pass. Archive
capability and independent checkpoint/runtime authority remain unapproved;
native account and debit/refund proofs, composed release and live custody remain
open. Actual fees remain null, and no accounting or spending authority is added.

The [qualified bounded native StorageProof verifier](evidence/operator-native-storage-proof-qualification-20260929.md)
is integrated at server `6201504e`. Its offline library replays receipt/finality
proofs before checking raw storage bytes at the original collection boundary; a
later certificate cannot move that state root. All 16 focused and six adjacent
roots pass normal/race with package exit zero, and all 21 causal controls
discriminate in both modes. The independent pinned-SDK oracle supplies 18 exact
vectors. Both earlier fixture/control anomalies remain separately preserved.
Its original API covers the collection boundary; the qualified historical
interface below supplies selected receipt/parent reads. Runtime decoding, proof
capture, account/fee authority and live custody remain open; actual fees stay null.
This scoped
qualification creates no owner-window, global-custody or spending authority.

The [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md)
is integrated at server `cfcbfcba` as documentation only. Generic balance
events share an extrinsic phase with native precompile effects; block balance
deltas also include non-fee effects, and a failed best-effort refund lacks a
fee-specific record. Neither proves general operator-call gas debits. Actual
fees remain null. The next dependency is bounded capture of historical raw
proofs and execution-runtime/source/metadata admission, followed by
runtime-qualified debit/refund
attribution through historical execution replay or an admitted fee-specific
runtime event. A future event cannot reconstruct historical fees. Independent
genesis/checkpoint and source-to-deployed-runtime admission remain separate;
an approved live checkpoint is not required to implement or qualify offline
proof machinery. This source review supplies no new behavioral qualification,
accounting or spending authority and does not close MG-03/PF-03.

The [qualified historical receipt/native fee-context source](evidence/operator-native-fee-context-qualification-20260929.md)
at server `41527380` passes all ten new and 122 affected roots in normal/race,
including three disposable-database roots; six causal controls discriminate in
both modes. It is integrated after storage as server `1bccc3cd`; a separate
composed smoke passes 30 of 138 available roots per normal/race mode. It derives
each receipt block's exact native commitment candidates and linked parent root
while keeping absent/ambiguous mappings unresolved.
Its source-profile account mapping supplies no runtime or payer admission, and
all actual fee amounts remain null. The original native StorageProof interface
covers collection-boundary reads; the separately qualified historical interface
below joins selected child/parent proofs. Runtime/debit attribution remains
separate. These prerequisite slices do not close MG-03/PF-03.

The [qualified historical native StorageProof API](evidence/operator-historical-native-storage-qualification-20260929.md)
is integrated at server `80c0e1b7`. It freshly replays receipt/native fee contexts
and derives one exact
receipt block's linked parent execution root or child post-state root internally.
A valid checkpoint child remains provable while its missing parent and incomplete
fee context stay explicit; parent fallback, ambiguous mappings and descendant
substitution are refused. All 20 new plus 30 adjacent roots pass normal/race with
package exit zero; all 21 causal controls discriminate in both modes. This is
50 selected roots per mode, not whole-package coverage. Runtime/payer/fee
interpretation, live
authority and custody remain absent, and actual fees stay null.

The [bounded historical native capture increment](evidence/operator-historical-native-capture-20261001.md)
at server `8a47dfe3` now produces those witnesses from exact-hash read-only RPC,
reusing durable partial evidence and lifetime request/byte budgets. Parent/child
selection comes from freshly replayed original proofs; ambiguous mappings,
wrong proof blocks and unproven values refuse private witness publication.
Completed capture and the new verification command replay offline. Author
qualification passes all 173 non-database recovery/CLI roots per normal/race
mode, vet, and seven causal controls in both modes; the three database census
roots remain unrun. Actual fees and every authority/spending flag remain absent.
Independent 173-root normal/race/vet review and three causal controls pass;
runtime interpretation/debit-refund attribution, production
archive capability, service adoption, successor release and live custody remain
open. The selected SN233/server942 release predates this source.

The independent monitor confirms the repair's postcondition at finalized state.
Only then close the incident, retaining its history and action receipts. A local
repair success with missing chain evidence stays pending. Recovery cannot erase
failed acceptance assertions or turn an observation gap into a completed epoch.

### On-call, incident evidence and rollout

Before activation, name a primary and backup operator, establish the alert route
and access to read-only diagnostics and the appropriate signing process, and
rehearse this runbook:

1. Acknowledge the incident and pin its deployment/release, actual process owners,
   last good checkpoint and current native/EVM blocks. Distinguish an unavailable
   read from an authenticated mismatch before deciding containment.
2. Stop only dependent new signing or unsafe work through its existing owner.
   Keep monitor, immutable history and valid earned claims available. Preserve
   signed and in-flight transactions for reconciliation; stopping a process does
   not stop native emission or remove custody obligations.
3. Seal an incident bundle: exact config/plan/runtime hashes, raw RPC responses
   and receipts, relevant signed artifacts, log byte ranges/cursors, queue state,
   liabilities, alert timeline and attempted repairs. Restrict signed transaction
   bytes and redact credentials; hash public evidence separately.
4. Reproduce the actual failure and inspect adjacent callers. Use the standing
   repair only if every precondition still holds; otherwise prepare the concrete
   bounded forward plan or code fix, qualify it, and obtain its required authority.
5. Reconcile transactions and databases before retry or service replacement.
   Verify the result independently, observe sustained proof/settlement progress,
   and retain unresolved consequences as open incidents. Record detection,
   response and recovery times and the follow-up owner.

Roll out immutable images with the qualified source/dependency/config manifest.
Run read-only shadow checks, then a canary with no duplicate signer and enough
capacity to preserve the required validator/operator quorum. At the SN25 launch
that quorum is one validator and one operator with no spare, so replacing either
cannot preserve it. Stage additive
database migrations before compatible consumers; specifically rehearse populated
client-key policy rollover and immutable usage writer/reader compatibility.
Advance only after current-domain readiness, resource bounds, fresh proofs and
finalized chain postconditions are observed. Thresholds and canary duration are
approved in the rollout manifest before execution.

Keep the previous image and an explicit state-format compatibility matrix.
Rollback is allowed only if the old reader/writer can safely interpret the
current schema, policies and signed state. An irreversible migration, finalized
registration, immutable deployment or economic transfer requires forward
recovery; restoring an old filesystem cannot undo it. Always reconcile in-flight
transactions and join old owners before changing images. Restore tests must
prove journals, keys and artifacts remain usable with no duplicate spend. End a
rollout with an independent state comparison and updated incident/capacity
records, not just a green process list.

## Integration with this repository

Reuse importable production packages: [crv4](../crv4) for authenticated runtime/native reads and transaction evidence; [stabi](../stabi) for the release ABI surface; [protocol](../protocol) for policy and domain encoding; [validator](../validator) for UR validation and evidence; and existing cryptographic/address/Merkle primitives where their units and domains match.

Do not import `sim-testnet` as a production dependency: it is a `package main` campaign with fixture, finance-repair, historical migration and adversarial machinery. Extract only a required, qualified generic facility into a neutral internal package when implementation begins. Keep mainnet capability adapters explicit, and keep the new root-validator implementation separate from UR scoring. The old [stctl configuration](../stctl/config.go) identifies itself as legacy pre-1.0 and rejects the current deployment domain; it is not the release-1.0 mainnet control plane.

Suggested future package boundary:

```text
mainnet/main.go                  argument parsing and command dispatch
internal/mainnetbootstrap/       config, snapshots, capabilities, plans, executor, reports
internal/rootvalidator/          root membership/basket observer and approved action loop
internal/chainactions/           only extracted, proven transaction/journal primitives
validator/                      existing production UR validator
```

Use interfaces for `FinalizedReader`, `NativeSigner`, `EvmSigner`, `SafeSigner`, `Submitter`, `Journal` and `ServiceSupervisor`. Follow the [UR Go style guide](../../connect/CODESTYLE.md): owned Go identifiers use `Evm`, `Rpc`, `Uid` and `Id` casing, `self` receivers and the prescribed field naming. Preserve externally required, generated and wire-format names. Packages may import a parent or peer, never their own child; the proposed internal packages are peers of the command package, not a bypass for that rule. A signer returns an identity-bound signed payload; it does not decide policy or fall back to another account. Mainnet configuration decoding must finish before any durable worker or RPC connection starts. Keep testnet provisional admission flags, deterministic fixture keys, fabricated identities, accelerated epochs, simulator routes and faucet/funding assumptions outside this interface.

## Acceptance evidence and implementation qualification

The closed testnet campaign requested 33 roles, 1,000 providers, 202 candidates,
200 head positions and two UR validators, five accelerated epochs and a later
production-policy observation. Those requirements were not completed by R48:
its original result is failed with zero complete acceptance epochs, and later
retained recovery remains non-accepting. Preserve that report and its explicit
exceptions as inputs. Mainnet is not blocked on reopening that campaign; it is
blocked on the [production gates](PRELAUNCH-FIXES.md#production-gates-in-execution-order)
and the actual capabilities, accounting and operating evidence required here.

Qualify the composed production release on a controlled integration deployment
with the relevant failure/recovery cases. Reuse historical tests only with exact
source/dependency and requirement mapping. A simulator-only patch, canceled
setup, signed plan, read-only monitor or partially observed epoch cannot replace
the required evidence. Keep one manifest of implemented, qualified, deployed
and operationally observed states, with every remaining exception explicit.
Mainnet has its own release identity, budgets and 50,400-block policy; testnet
provisional authority and accelerated timing do not carry over.

Current component evidence includes the
[complete server model census and qualified fixture corrections](evidence/server-model-completion-20260928.md)
and [shared receipt/miner recovery qualification](evidence/receipt-recovery-qualification-20260928.md).
The [September 29 server source composition](evidence/operator-mg03-r48-composition-20260929.md)
retains those historical approvals and records the new scoped normal/race
qualification of its full merge and test successor. It does not claim a new
full-model run or a composed production release.
The [registration transaction qualification](evidence/registration-server-model-qualification-20260928.md)
passes all 29 affected roots normally and under race detection. Its full model
body completed all 1,125 roots with 1,118 passes, seven explicit fixture-input
skips and no failures. Retain the original successful package exit separately
from its failed legacy-subtest metadata check; do not claim zero-skip coverage.
The [combined SN registration and diagnostics check](evidence/registration-diagnostics-composed-qualification-20260928.md)
passes all seven selected consumer roots normally and under race detection on
the sealed source graph; all 18 stages and independent after-fences passed.
The later [integrated mainnet source check](evidence/final-composed-source-qualification-20260928.md)
combines that graph with the native deadline observer and offline bootstrap:
379/379 full normal roots and 122/122 selected race roots passed with all 158
declared descendant executions, zero skips, and unchanged source/module seals.
It does not replace release-artifact, deployment, custody or live acceptance
gates.
The subsequent [two-UR-config admission qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
uses the same physical dependency refs with the v2 bootstrap source. Its full
normal mainnet census and affected mainnet/validator normal/race suites pass
all 588 root and 183 descendant executions. Six original control captures
retain their twelve intended failures; strict maintained resume accepts all
24 stages without rerunning bodies. Earlier metadata and mode refusals remain
recorded. This does not reuse validator package qualification wholesale or
supply live identity, producer eligibility, signing or service activation.
The frozen model invocation ended with three now-corrected fixture failures and
seven optional-configuration skips; it is not recorded as a passing full
invocation. Preserve these results when assembling the final source composition
and qualify its affected changes without restarting unchanged completed scopes.

The future mainnet acceptance bundle contains:

| Outcome | Evidence required |
| --- | --- |
| Reset | No owner trim (October 7): the census and protection set, the owner's recorded no-trim decision, the actual retained registrations and unchanged required custody/validator ownership; no removal is claimed. |
| Contracts | Source/toolchain/artifact hashes, predicted/actual addresses and nonces, creation receipts, runtime bytecode and immutable getters, Safe authority, one-shot links and evidence anchor, preserved custody invariants. |
| 10% miner rewards | Explicit denominator and activation boundary; exact native interval accounting; finalized incentive outcomes including collateral; direct-head and tail entitlement reconciliation; verified 90% ordinary native treasury credit to the `ur-reserve` recipients (the whole tranche in a reserve-only epoch), with no deliberate owner recycle; runtime-derived quantization tolerance and actual target result; proof of a stronger hard cap only if that assurance was selected. |
| Root validator | Real netuid-0 membership and owner mapping, bounded admission receipt, stake/retention observation, child policy, actual basket strategy, live owned service and runtime identity. |
| UR validator | Real UR eligibility and live applied/revealed CRv4 rows, authenticated evidence/usage, service signer, the policy's validator and operator safety minima (one each at the SN25 launch) and more than kappa (50%) of validator stake. |
| Financial/finality closure | Actual spend including failures, remaining allowance, all transaction owners joined, native/EVM finality mapping, open liabilities and future operations clearly reported. |

Measure activation across at least three complete native emission intervals, and observe a full mainnet UR settlement/claim cycle before declaring settlement acceptance. The 50,400-block cycle cannot be replaced by accelerated testnet timing. Bootstrap may report `DEPLOYED_AWAITING_SETTLEMENT` while that observation is pending; it must not call the whole requested program accepted early.

Implementation qualification is owned by **Sol medium (`gpt-6-sol`, effort `medium`) for test and gate execution; Astra max (`gpt-6-astra`, effort `max`) for all implementation, debugging and fixes**, following the [Go style guide's bug-fix and testing policy](../../connect/CODESTYLE.md). This assignment was confirmed on 2026-09-28; retain earlier Terra evidence under its original attribution and do not rerun completed work because the executing model changed. Every actual fix needs a regression that deterministically reproduces the pre-fix failure and verifies corrected behavior at the observable failing layer. Use explicit barriers, hooks or state transitions for ordering; sleeps, negative timeouts, queue polling and scheduler luck are not the primary proof. Review surrounding code, sibling call sites and similar patterns before declaring the root cause fixed, and record any affected adjacent paths.

Keep regression data visibly synthetic: generated test-only identities, `.example` hosts and reserved documentation addresses; sanitize captures before turning them into fixtures and retain necessary raw evidence outside source. Tests are top-level `func TestXxx(t *testing.T)` declarations. Use separate top-level tests or plain table loops for ordinary cases; use `t.Run` only when isolating and asserting a deliberately failing subtest is itself the subject. This document does not execute or request execution of new tests. When implementation is authorized, cover meaningful boundaries and recovery:

1. Command-level read-only preview cannot acquire a signer or submit, including malformed/duplicate config, wrong genesis/chain, testnet identity, route substitution, stale snapshots and runtime upgrades. Also cover a separately attested mainnet identity subsequently hosted at a previously testnet address.
2. Real pinned metadata/call encoding proves owner versus Root origins and both root/UR call domains. Negative cases include unsupported trim, wrong signer, root `register_limit`, missing price protection and native/EVM rollback failure for any new registration wrapper.
3. Reset census and actual pruning/trim replay cover dual-role neurons, validator-without-permit preservation, custody identities, immunity boundary, tie ordering, concurrent registration, minimum capacity, UID renumbering and surviving collateral. A smaller UID count alone cannot pass.
4. Full emission-path tests demonstrate why scaling weights or theta does not impose a cap; then prove the selected 10% target and runtime-derived tolerance through quantization, consensus, independent-validator rows, no-incentive fallback, multiple mechanisms, denominator-boundary accrual, locked rewards and cumulative dust. Distinguish actual target observation from any selected hard-cap guarantee. Cover burn and recycle accounting separately and refuse an unsupported reserve transfer.
5. Deployment tests use real creation payloads and contract execution for nonce `n+3` escrow registration, atomic proxy initialization, mapped coldkeys, Safe checks, refund/fee bounds, one-shot conflicts and evidence anchoring. A changed custody contract requires its own conservation/claim and adversarial review.
6. Crash/restart tests inject failure before and after signing, submission, finality and fsync; prove no duplicate burn/deposit/stake and no lost finalized success. Cover nonce collision, reorg before finality, failed batch children, canceled owners and authenticated successor history.
7. Process tests prove both distinct validator roles, no silent endpoint fallback, single signer ownership, complete child joining, current UR permit/reveal checks, root custom/default strategy handling and no unbounded restaking/re-registration loop.

Run bounded normal tests on the frozen implementation, then appropriate race tests for shared state, journal ownership and supervisors. Retain exact source, binary, selector, package working directory and terminal evidence. Diagnose any actual failure on that capture before retry; preserve failed evidence and use the established confirmation protocol. Reuse unaffected qualification only with an explicit source/dependency mapping; changed custody/runtime economics require their relevant full tests and owned-node rehearsal. There are no mainnet tests against public RPC and no broadcast hidden in a test command.

Use the maintained [qualification owner](../scripts/qualification/main.go) for
new captures. A retained test binary must run from its authenticated package
directory, just as `go test` would; the module root is not equivalent. Verify
the binary's actual test list and complete terminal events, including explicit
package success. A wrapper's zero exit after interrupted execution is not a
pass. Preserve valid compiled artifacts and completed work when correcting a
runner, with the failed invocation and the correction recorded separately.
Compare the enumerated roots with the independently supplied expected set before
launching bodies. A nonempty subset is insufficient. Parse declared table schemas
explicitly; never discard a first row merely because another file had a header.

The [literal slash checker correction](evidence/qualification-slash-parent-qualification-20260928.md)
now preserves source-declared Go child names without inventing intermediate
test events. Its 31-root normal/race qualification and two causal controls
passed before it replayed retained Connect evidence. Both original 33-root,
32-descendant captures now match; no test body was repeated. A corrected
checker or declaration must retain the original failed invocation, body exit
and source fences instead of turning metadata repair into a complete rerun.

Bind qualification to the roles and platforms selected for this release.
Unselected services and architectures remain explicitly unqualified and cannot
be deployed from its approval; they do not require speculative build or test
work before the selected release can proceed. Reuse successful unchanged
scopes by their actual consumed source and dependency identities. A new binary
hash alone is not a reason to repeat all historical qualification.

Set the qualification environment explicitly before compiling: `GOWORK=off`,
`GOMAXPROCS=2`, `GOCACHE=/mnt/data/sn-testnet/gocache`, and a capture-specific
`TMPDIR` under `/mnt/data/sn-testnet/evidence`. Create the temporary directory
first. Record the actual Go executable, resolved tool paths and module graph;
the host's default cache symlink is not a physical tool path. A preflight
refusal before any test body executes is a runner failure, not a product test
failure. Correct that environment and retain the refused invocation without
restarting unrelated bodies already in progress.

## Open inputs before an executable mainnet plan

Use `https://archive.chain.opentensor.ai` as the interim mainnet RPC for
read-only discovery and unsigned plans. The [October 1 observation](evidence/public-archive-switch-20261001.md)
reports mainnet genesis/EVM ID 964 and runtime spec 470, but the strict native/EVM
mapping initially lacked a public raw-header method. The
[qualified public fallback](evidence/public-header-fallback-20261001.md) now
passes a [live read-only exact-hash finalized snapshot](evidence/public-finalized-snapshot-20261001.md)
on that archive. The [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
rechecks one combined observation at block 9,186,298 and retains the exact
runtime470 hashes, source lock, release input and ten-action blocked plan for
SN `de0823ce` / server `0b8e758d`. Its separately declared network target is a
review input, with independent network/runtime authority still missing. Snow VPN
`172.28.208.185:9944` is a
future failover; it still returned HTTP 502 at the latest retained check.
The [later dual-route recheck](evidence/public-dual-route-recheck-20261001.md)
at block 9,190,703 confirms both public routes still report runtime 470, the
same mainnet genesis/EVM ID and `:code` hash while v471 remains proposed.
The [23:05 UTC pinned readback](evidence/public-dual-route-recheck-20261001-2305.md)
again matches both routes at block 9,191,112, while their separately sampled
current heads differ. The shared exact-block observation does not supply
independent chain or runtime authority.
This is read-only RPC corroboration, not independent finality or activation
authority.
Do not retarget signed action bytes. The
[earlier frozen offline composition](evidence/release-current-source-20261001.md)
passes its 17-binary, five-contract and 170-hash audit; eight fresh OCI image
readbacks pass as separate supplements for SN `2d53e6f2` / server `ecbf3aad`.
Those images do not establish provenance for the successor sources. No
deployment approval is implied. The later
[pre-Safe baseline](evidence/release-pre-safe-baseline-and-modes-20261001.md)
does establish local 17-binary/five-contract/eight-image coverage for SN
`1806b3b3` / server `0b8e758d`; it does not cover the later public Safe source.
The separate [Safe-source release](evidence/release-safe-source-20261001.md)
builds and verifies its own complete local composition at SN `095a2208` /
server `0b8e758d`, preserving the same remaining independent and production
approval gates.
Obtain
an independently approved mainnet genesis/runtime identity and complete
SN25 census. Qualify the production source/dependency release with
the retained R48/R46 lessons, then qualify the implemented separate bootstrap, owner-local signing/host submission, passive-root and UR activation paths. The generic combined plan remains blocked. Supply actual reset capability, independent custody, registration protection, identities and budgets, and observe the selected 10% native allocation/90% recycle outcome under the approved runtime tolerance. Existing passive root participation has no periodic root-weight signing requirement; any required registration/stake/basket mutation needs its separate current authority. Install independent
monitoring, existing-stack alerts, bounded repair authority and the on-call
runbook before activation. Each unresolved item remains visible in the plan and
report; the closed testnet effort is not relabeled as a pass.

[subtensor-commit]: https://github.com/RaoFoundation/subtensor/commit/67dcf7f791dc495064c293f080a0702cb433e51e
[subtensor-admin]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs
[subtensor-storage]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/lib.rs#L1661
[subtensor-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171
[subtensor-registration]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs
[subtensor-root]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/root.rs#L88
[subtensor-weights]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs#L875
[subtensor-dispatches]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/macros/dispatches.rs
[subtensor-coinbase]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs
[subtensor-alpha-accounting]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/alpha.rs#L38
[subtensor-shares]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/subnet_emissions.rs#L354
[subtensor-alpha-precompile]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/alpha.rs#L297
[subtensor-epoch]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/run_epoch.rs
[neuron-interface]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/solidity/neuron.sol#L206
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/root-reborn.mdx
[root-reborn-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx
[root-removal-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/migrations/migrate_remove_root_weights.rs
[root-basket-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/staking/basket_trade.rs
[subtensor-root-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/root.rs
[subtensor-dispatches-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/macros/dispatches.rs
[collateral-guide]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/mining/collateral.mdx
[max-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/hyperparameters/max-allowed-uids.mdx


### October 2 runtime source intake follow-up

The [bounded official tag/release census](evidence/runtime-472-upstream-intake-20261002.json)
returned v470 and v471 for `refs/tags/v47*`; the first ten public releases likewise
contained no v472 entry. [Official release source](https://github.com/RaoFoundation/subtensor/releases).
This is not proof that v472 source or a proposal is globally absent. Keep exact
observed v472 code/metadata as unapproved intake until source attribution and
consumed-purpose review are complete. Fleet catalog implementation and offline
native/snapshot preparation continue independently of that live authority input.


### Guarded spool reader qualification — 2026-10-02

Main now includes independently qualified guarded spool reader recovery
(merge `4cda804c5e23d3ae6f48971534108a51ffed52c0`). Normal EOF stays intact; failed post-read custody checks
admit zero bytes even when the descriptor advanced. Fifteen affected roots pass
normally and with race detection, with vet passing and causal old-body controls
retained. See [integration evidence](evidence/current-graph-reader-progress-20261002.md).
The adjacent Server and fresh-preparation reader scopes remain separate gates.
This integration does not authorize signing, deployment or activation.


### Fresh native and snapshot preparation — author qualification

Candidate `e097cff896313ac346d7165cc8c030a34abb6ffb` passed fourteen public
preparation command tests and two native checkpoint tests normally and with
race detection; four package vets passed. Four controls on the previous source
fail for the expected unsupported-owner boundaries in each mode. Root verified
the [author receipt](evidence/fixed-owner-preparation-author-20261002.json),
its 34 manifest members, raw archive and source bundle. Receipt SHA-256:
`8ef17e85905a18d80248d3d5438b8299e05ec893e6af418c1d002ecf21657ed6`.

This covers fresh native journals and fixed snapshot heads in precreated private
roots, including pending-attribute recovery and refusal to recreate lost completed
members. It does not grant runtime authority or restart authorization. Independent
qualification and the current published Connect/main composition remain pending;
private-root creation, retained/restore validation, remaining owners and joined
capacity revisions are still open. This candidate is not merged into main.


### Payment monitoring must retain accepted-credit semantics

The remaining proof/settlement monitoring implementation must distinguish
finalized claim acceptance, unpaid vault credit and actual aggregate payment.
A successful claim may defer transfer; a later payment may settle several
epochs/operators for one coldkey. See the [producer mapping](evidence/provider-proof-settlement-hook-map-20261002.md)
and [source-bound contract semantics](evidence/claim-payment-semantics-20261002.json).
No complete per-pool paid projection is qualified yet. This is part of MG-06/07
and PH-12/15/28, with deterministic contract and monitor controls still required.


### Fleet runtime continuation — integrated

Main merge `9671f4568b92e168d1db86d539d52b00f361a309` includes the independently qualified purpose-scoped
fleet runtime changes. Author and independent23 tests pass in each mode;
three package vets pass and six causal controls discriminate each mode. Root
verified all55 independent receipt bindings and exact merged file/module bytes.
[Qualification and integration evidence](evidence/fleet-runtime-catalog-progress-20261002.md)
keep current reads, historical decoding and present signing authority separate.
Original signatures and retained recovery authority remain intact. Runtime472
approval and final current Server/module/release composition remain open.


### Preparation source composition — bounded author result

SN `1f66a2bd` joins fixed-owner preparation with retained-member and spool
recovery. Connect `ba74f897` joins the unchanged preparation core onto the
published runtime/callback parent; its durablevolume subtree and module bytes
remain identical to the original qualified core. The exact eight-pin workspace
uses Server `2c4e5dca`. Fourteen public preparation commands, two native adapters
and one public successor-execution test pass normally and with race detection;
four package vets pass. Root verified all49 [author receipt](evidence/preparation-composition-author-20261002.json)
bindings and raw archive. Receipt SHA-256:
`dadf721ef6c38a1e1d808ad79dfa73481f9ffe702b2225330c5d7d25c7d1b854`.

This is a source/workspace result. It does not qualify published module consumption
or later Server main `1d72f577`. Directory-only owners, private-root creation,
retained/restore semantics and capacity revisions remain in implementation.
No production preparation or restoration is authorized by this result.


### Qualification throughput follow-up

The [compiler path-cache lead](evidence/compiler-cache-path-lead-20261002.json)
identifies a future harness optimization to qualify: stable path trimming can
avoid distinct cache identities for unchanged local packages in private checkouts.
No live gate changes flags or restarts for this lead. Its source observation does
not prove a benchmark improvement or close compiler provenance, source authority
or release qualification. Preserve fresh test execution and exact flag bindings.


### Claim projection qualification requirements

The new producer must use exact ABI event identities and preserve aggregate
coldkey-payment semantics. Tests must distinguish accepted liability from paid
credit, degrade malformed/impossible payment observations without stopping claim
recovery, and retain explicit configured-RPC evidence strength. Independent
finality and genesis admission are separate from matching a chain ID or Merkle
root. These requirements were refined during candidate review; the producer
and consumer are still being implemented and have no qualification receipt.


### Actionable claim backlog monitoring

The claim projection must retain bounded oldest/omitted unresolved summaries in
addition to recent entries. Monitoring separates publication heartbeat from
actual settlement progress and rejects future-dated observations. An omitted
entry is unobserved history, not a settled liability. These remain required
producer/consumer tests before unattended operation and MG-07 acceptance.


### Directory-owner preparation qualification

Frozen SN `682d568c` / Connect `05e39766` passed 77 core, 21 public
preparation-command and five miner-constructor tests in each of normal and race
modes, plus four package vet scopes. Root verified all 49 manifest bindings,
the raw archive and source bundles in the [author receipt](evidence/directory-owner-preparation-author-20261002.json).
Receipt SHA-256: `16c755189250f04d75f29c9d745016ae38a6c89420451223f255b2f80ce19d84`.
The old code reproduced two core and three public-command failures in each
mode while its negative authority control passed.

Exact attribute-only and fixed-head owner profiles now enroll through the actual
plan/apply command and reopen through real miner constructors. Interrupted
publication resumes the original inode and checkpoint; missing completed heads
and replacement markers remain refused. Existing capacity dimensions remain
unchanged, including the claim queue's 16 MiB retained-byte bound.

This author qualification uses Server `2c4e5dca` and fresh precreated private
roots. Independent qualification, private-root creation, retained/restore semantic
rebind, capacity revisions and the composition with current main remain separate
requirements. The candidate is not merged or approved for live preparation.


### Optional telemetry cannot reserve operational claim capacity

Claim-queue review found a second pressure case: retaining every previously
acknowledged optional observation can still exceed the 16 MiB queue limit when
new signed or operational fields grow. Dropping only newly added observations
is insufficient. Add a second bounded fallback that omits prior optional
observations and reports that degradation, while preserving every entry,
signature, attempt counter and operational outcome. If those required bytes
alone exceed capacity, retain the actual capacity refusal.

Qualification must exercise the real durable save with prior observations near
the limit, subsequent signed-field growth, failed-save publication isolation
and reopen of the exact acknowledged fallback. An omitted observation remains
unobserved history; no economic outcome or signing authority follows from it.
This review finding is assigned to the claim-producer successor, not qualified
or merged yet.


### Independent preparation composition qualification

The exact SN `1f66a2bd` / Connect `ba74f897` / Server `2c4e5dca`
workspace now passes independent qualification: 101 selected tests normally
and with race detection (15 mainnet, 11 validator, 73 storage core and two
chain tests), plus five package vet scopes. All tests completed without
failures or skips. Root verified all 48 [receipt manifest](evidence/preparation-composition-independent-20261002.json)
bindings. Receipt SHA-256:
`b5f4725895eadc33d398b8329bea98671b39f1845db0203a49ae9a7b3f4cd6c4`.

The receipt binds the 648-module local workspace graph and exact source
readback (20,080 SN and 3,715 Connect tracked blobs), including the unchanged
core, reader and adapter joins. Earlier old-source causal failures remain
separate author evidence. This qualifies the frozen source composition; it
does not qualify tracked published-module consumption, current main with later
fleet/server changes, directory-owner/private-root successors or a production
release. Integrate those source/dependency changes without replacing completed
recovery history, and qualify their actual final graph before launch.


### Explicit private-root creation is source-qualified

Frozen SN `c8998b31` / Connect `7600ea5c` passes 83 core and 26 public
preparation-command tests in each of normal/race modes, plus four package
vets. Root verified all 43 [author receipt](evidence/private-root-preparation-author-20261002.json)
manifest bindings, raw archive and source bundles. Receipt SHA-256:
`693444206629643f2100b2e30c05700241769c1d325aa108fe07c41afaf512ac`.
The old directory-owner source fails the two new public creation controls in
each mode. First-checkpoint positive results retain their separate source scope.

The explicit fresh mode stages a private leaf inode during planning, binds its
parent and zero-history fence, reserves the original inode/control before an
atomic no-replace move, and syncs both parents. Child crashes, lost sync
acknowledgements, competing targets and changed or missing completed namespaces
resume only through original retained custody; runtime absence never triggers
implicit mkdir. Target parent, staging and metadata roots remain precreated.

This is Linux amd64 source/workspace qualification using Server `2c4e5dca`;
arm64 execution, independent current-main tracked-module qualification,
retained/restore semantic rebinding and capacity revisions remain open. The
qualified candidates are being composed with provider/fleet fixes and current
server before merge. No live preparation or deployment authority is supplied.


### Durable claim projection is integrated

Main merge `bd5e7e72` includes exactly the 11 qualified producer files from
SN `347605fd`, preserving provider/fleet changes and original module files.
Root verified all 127 author bindings and 76 independent bindings. The
[independent receipt](evidence/claim-projection-independent-20261002.json), SHA-256
`1eda6ff63df36aa0b4260573f31076676badac83224fead862eda57ce4e86a9e`,
passes 29 tests per mode and two package vets; three old-body controls fail at
the intended assertions per mode. Author 37-test coverage remains separate.
The [main integration manifest](evidence/claim-projection-main-integration-20261002.json)
binds exact source bytes. This graph uses Server10a; current-server composition
and the independent claim consumer remain open. No finality, payment allocation
or live launch authority follows from the queue projection.


### October 3: native raw restore diagnostic correction

The [original native raw restore batch](evidence/restore-native-raw-original-20261003.json)
at frozen SN `00a30655` completed normally and under race detection. Each mode
passed ten mainnet roots and one chain root, with one mainnet assertion failure:
a duplicate JSON nomination was correctly refused by the earlier strict reader,
while the test expected a later raw-adapter diagnostic. Both package vets pass.
Preserve this failed batch; a separate test-only correction must reach that
assertion without weakening duplicate-input refusal. Broader member/multi-owner
restore and current published composition remain open.


### October 3: restored registry usability remains a launch requirement

The physical restore adapter must also satisfy the real successor execution
consumer's retained registry and local-preparation root authority. Original
approvals bind device/inode, so unsigned metadata rebinding alone does not
authorize execution at a new physical generation. Implement and qualify an
explicit reviewed lineage-bound rebind while retaining every original signed
approval and attempt. A successful storage copy is not proof of usable restart.
This requirement remains open alongside member/multi-owner restore; it does
not invalidate the earlier scoped ledger/native test results.


### October 3: durable earning boundary normal qualification

Frozen Server `cdcb61fa` now passes the [independent 28-root normal
batch](evidence/payout-boundary-normal-20261003.json): all 13 new boundary
controls and 15 neighbors pass, with no failures or skips and owned
PostgreSQL/Redis cleanup complete. The exact source tests concurrent explicit
preparation, declaration drift at actual planning/send paths, retained accepted
attempt reconciliation, process restart and a database timeout's bounded
continuation. The original container setup failure stays separately retained.
The [same 28-root race scope](evidence/payout-boundary-race-20261003.json) now
passes without failures, skips or race reports, with owned cleanup complete.
The [normal regression controls](evidence/payout-boundary-causal-normal-20261003.json)
also reproduce all eight intended failures while accepted reconciliation
remains a positive pass; owned cleanup completed. The five groups include
explicit omission/misclassification controls, not all unmodified historical
source. Race controls and vet/build remain queued. This does not relabel the earlier
full model source, prove a composed release or make the October 6 schedule
operational. Merge, production preparation and running worker evidence remain
required.


### October 3: wallet GET protocol qualification

The broader wallet GET successor must follow returned endpoint pagination
links, with confined destinations and a shared retry budget, rather than
deriving cursors from wallet/token IDs. Retained endpoint OpenAPI inspection
establishes this implementation requirement; deterministic public-client tests
and the frozen release join remain pending. The earlier qualified transaction
GET/rate scope does not establish wallet-census correctness.


### October 3: retained native correction and restore-core race scope

The [single native diagnostic correction](evidence/restore-native-diagnostic-independent-20261003.json)
at test-only `3ca923a9` passes normally and under race detection, without skips
or race reports. Root verified its 11 bindings, actual test events and exact
corrected test bytes. Production/module bytes remain `00a30655`; its eleven
passing roots per mode and two vets remain separately retained, and the
original diagnostic assertion failure stays failed.

Connect `53bc92fa` now also passes the [same 32 restore-core roots under race
detection and package vet](evidence/restore-core-race-vet-20261003.json). Root
verified eight addendum bindings and all 32 tracked package source files against
the exact Git commit. The prior independent normal scope remains retained.
This advances the shared dependency qualification; public member/multi-owner
restore, execution rebind, current published composition and live restoration
remain separate requirements.

The [ninth bounded inactive-cache cleanup](evidence/cache-reclaim-9-20261003.json)
reclaimed 16,330,846,208 bytes (15.21 GiB) from 450 old compiler archives.
Privileged process checks and bounded metadata reference checks found no
references; exact physical identities were rechecked before deletion. Active
test caches, source, evidence, modules and executables were retained. The data
volume subsequently had about 146 GiB free; admission must still remeasure
headroom before each costly phase. This is qualification-host evidence, not
production capacity qualification.


### October 3: shared recovery dependency published

Connect main now publishes `9e7ec0afa426472c083c750196155f5708e7d23b`,
tree `b1919e473daab790c5cf7a6fb6fc56e74831d636`. The
[sealed source join](evidence/connect53-source-join-20261003.json) preserves
all durable-volume and module bytes from qualified `53bc92fa`, and all paths
outside that package from upstream `69a006b3`. Root verified all 37 bindings.
Pull/rebase changed the integration commit identity from `da2385aa`, but root
verified the entire published tree remained identical and independently read
back the remote main ref. The [publication checkpoint](evidence/connect53-publication-20261003.json)
retains that distinction.

The 32 independent normal/race roots and package vet retain their original
source scopes; earlier preparation receipts remain separate. This is shared
source publication, not proof of current SN/server module consumption, a
qualified production release or usable live restore. Pin this exact published
revision in the next consumer composition without retargeting active tests.
Member `332b1f43`, complete co-owner restoration, explicit execution rebind
and capacity revisions remain subsequent requirements.


### October 3: returned I/O errors must preserve their causes

Composition review found that accepted-payment transaction readback wrapped
its error with `%s`, discarding typed cancellation and timeout causes before
retained continuation handled it. Candidate `bfb224c8` changes the actual
returned wrapper to `%w` and adds end-to-end controls; it is not yet qualified.
Audit adjacent wallet, cancellation, submit/reset and record-save return paths
for the same defect, distinguishing returned errors from log formatting.
Preserve joined hard causes and caller cancellation through public boundaries.
A canceled owner must not schedule more work, and an ephemeral physical read
may retain the same attempt for bounded continuation. Existing passing scopes
do not qualify this later composition correction.


The wallet read candidate's unchanged-POST fence preserves historical behavior;
it does not prove write recovery. A separately reviewed correction must retain
one transfer-challenge request/idempotency key across ambiguous responses and
caller retries, preserve cancellation causes and format amounts exactly.
Source review found fresh keys within the existing retry loop; no actual
duplicate transfer payment is established. Qualify this customer write path
separately from provider payout migration.


### October 3: composed payout/read continuation source

Frozen Server `2a453df9` joins the durable boundary `cdcb61fa` and wallet
GET successor `25f617dc`. The [source join](evidence/payout-get-composition-source-join-20261003.json)
retains all matched component bytes, with four deliberate composition changes:
public payment error/continuation handling, its new tests, the boundary drift
control and transition status. New legacy submission admission is distinct
from permission to reconcile existing attempts. Returned I/O errors retain
their causes, and canceled owners cannot schedule continuation tasks.

Root verified all 58 [intake bindings](evidence/payout-get-composition-intake-20261003.json),
46 candidate source/module files against Git, and every declared component
match. Ten controller roots and four operative regression controls are queued;
no composition execution, full-suite pass or deployment is claimed. Retain
the unchanged component and full-model scopes.

The source census also confirms a separate MG-07 implementation gap: current
`monitorServicesPolicy` has provider/validator/operator roles but no claim
consumer roles. Resume and integrate the retained `e7965b4` claim consumer
against the actual producer and current policy rather than declaring producer
publication alone to be monitoring closure. Sequential proof/receipt progress
requires qualification; the earlier review lead does not establish a hidden
leaf-progress defect in the producer's actual signed-receipt path.


## October 3 completed cutoff-boundary qualification

Frozen Server `cdcb61fa` completes its [independent scoped qualification](evidence/payout-boundary-qualification-20261003.json): 28 normal and 28 race roots pass, five package vets and the offline CLI build pass, and every owned fixture cleanup completes. The [race causal controls](evidence/payout-boundary-causal-race-20261003.json), like their normal counterparts, produce eight intended assertion failures and one accepted-payment reconciliation positive pass without race reports. The original container-bind setup failure remains retained. This qualifies the durable earning-policy boundary scope; wallet `25f617dc`, combined Server `2a453df9`, final release integration and production deployment remain separate gates.


## October 3 full hardening source reconciliation

The [38-requirement source review](evidence/current-requirements-20261003.md) covers every PH-01–PH-28 lesson and MG-01–MG-10 gate, with [68 exact Git-blob bindings](evidence/current-requirements-20261003.json). It identifies integrated fleet capability selection, finite GET recovery, provider monitoring, callback isolation and retained-member/reader recovery; these require composition and production rehearsal rather than duplicate implementations. Confirmed remaining implementation work includes the current claim consumer, durable customer transfer-out challenge recovery, independent continuous credit/reserve/capture/native economic observers, transaction-attributed fee verification, and joined capacity/restore behavior. Existing validator caches need causal work-count and invalidation qualification before additional caching is justified. This is a source census, not test or launch acceptance. Its pinned `4e413cb0`/Server `2a453df9` scopes remain historical; the separately recorded completed financial and cutoff-boundary tests retain their own exact sources.


## Continuous economic monitoring acceptance

Current `mainnet/economic_emission_observe.go` reads a bounded native range and explicitly labels finality as an owned-RPC assertion. It leaves the pre-withholding denominator, recipient generations, recycling and independent outcome proof unresolved. `mainnet/evm_reserve_link.go` verifies the original reserve recorder installation; it does not observe operating reserve health. Neither scope establishes continuous settlement monitoring.

The production observer must retain a contiguous finalized cursor and original event identities across restart. A partial page or timed-out block must remain pending, without advancing beyond unread evidence; repeated reads must not count capture, carry, credit or payment twice. Persist completed bounded batches and retry only the unfinished batch. Validate chain, contract, canonical boundary and event ordering before applying an observation. Preserve the RootMissed carry step explicitly between capture and later entitlement, as the testnet peer review required.

Expose observed contract conservation separately from independently verified native emissions and the 10% provider/90% recycle target. Missing denominator, fee attribution or finality authority is unknown, never zero or a successful economic check. Each domain needs distinct heartbeat, successful observation, durable progress and oldest unresolved obligation metrics. A reserve or claim read timeout must retry within the shared bounded budget and leave healthy validator/provider/operator observers running. Observers do not acquire repair or signing authority.

Qualification must exercise the actual public command and checkpoint owners: timeout midway through a batch, restart after persistence before acknowledgment, duplicate/overlapping pages, changed canonical boundary, capture carried into a later epoch, aggregate ClaimPaid without invented per-pool attribution, and unavailable native denominator. Prove exact-once observations, retained original evidence, bounded work, joined cancellation and domain isolation. These are required implementation and qualification items under PH-12, PH-15, PH-28 and MG-07; the source review and finite reader do not close them.


## October 3 wallet GET normal qualification

Frozen Server `25f617dc` passes its [independent 26-root normal scope](evidence/wallet-get-normal-20261003.json): fourteen new wallet-read controls and twelve prior payment-read controls, zero failures/skips and completed fixture cleanup. The sealed receipt binds twenty files; coordinator verification also reads the actual terminal JSON events and exact expected root names. Race execution, omission controls, vet and combined Server `2a453df9` qualification remain pending. This scope covers reads and the unchanged POST source join, not behavioral qualification of customer challenge writes or a deployed cutoff.


## October 3 wallet race and claim-consumer source checkpoint

The [same 26 wallet GET roots pass under race detection](evidence/wallet-get-race-20261003.json) at frozen Server `25f617dc`, with zero failures/skips/race reports and completed fixture cleanup. Omission controls, vet and combined-source qualification remain separate.

The new claim-consumer candidate `80030acb` has a [coordinator source review](evidence/claim-consumer-source-review-20261003.json): exactly nine changed files over the pinned current base; miner, validator, operator, protocol, chain and module bytes are unchanged. Qualification remains pending. Unsigned claimed-leaf progress and signed-receipt progress are distinct actual producer paths; later credit payments belong in the economic observer rather than rewriting original receipt evidence. An adjacent review identifies HTTP 408/429 classification and pacing for follow-up; these current statuses are nonterminal, so this is not evidence of a stopped observer.


## October 3 current claim-consumer qualification handoff

The [final frozen intake](evidence/claim-consumer-intake-20261003.json) for SN `80030acb` binds 31 source/control files and selects 23 roots: sixteen claim controls, two public worker neighbors and five actual producer controls. Six explicitly classified overlays expect eight behavioral assertion failures per mode. The final intake supersedes the retained preliminary control recipe; compiler failures cannot count as causal success. No execution result is claimed yet. This uses the unchanged producer347 graph (Server10a/Connect0a5), so published Connect9e/current Server2a consumption still requires its own release composition. The observer supplies no repair, signing, independent finality or per-epoch payment authority.


## October 3 runtime source refresh

A [read-only official source refresh](evidence/runtime-source-refresh-20261003.json) finds returned v470/v471 tags and releases; the main ref is `c004cebf`, whose runtime source and v471 manifest/digest explicitly describe spec version 471. These sources do not authorize the earlier observed v472 artifact. This does not establish absence of another branch or unpublished proposal, and does not claim a fresh on-chain version. Exact artifact/source attribution and consumed-purpose admission remain open launch inputs, independent of payout and monitoring qualification.


## October 3 wallet operative controls

The [normal wallet causal controls](evidence/wallet-get-causal-normal-20261003.json) at frozen Server `25f617dc` produce all nine intended assertion failures across five omission groups, with five positive neighbors passing and owned fixture cleanup complete. Coordinator verification covers all 59 bound files and the actual terminal event census. These discriminate retries, Link pagination, shared budgets, Retry-After and redirect credential protection; they are explicitly labelled operative omissions, not historical source replays. Race controls, vet and final combined-source qualification remain pending.


## October 3 original-authority registry recovery handoff

The [frozen registry-rebind intake](evidence/registry-rebind-intake-20261003.json) at SN `94e087da` binds fifteen files and proposes four public/neighbor normal/race roots, vet and an old-dispatcher causal check. Source review confirms physical adoption requires completed original claim and nonce custody, preserves exact nonce bytes and the original approver, and refuses an unrelated pending local publication. Compilation/preflight are complete; behavioral qualification is pending. This candidate uses Connect332/Serverc256 and does not establish published Connect9e/current Server2a consumption.

The first registry-only profile is an incremental implementation, not the completed restore requirement. Complete co-owner union restore, local-root rebind, original pending-outcome adoption, pending outer-head recovery, capacity revisions and the final published-module composition remain required. Rebinding must preserve original signing payloads, lifetime counters and histories and must not recreate acknowledged claims or missing receipts.


## October 3 current Server migration composition blocker

A [fresh upstream merge review](evidence/server-current-migration-join-20261003.json) found Server main `18ec7c05` has 87 changed files over the tested base `c2563f9a`; payout candidate `2a453df9` changes 44. Their sole file intersection is `db_migrations.go`. Upstream appends the Redis admission schema where the payout candidate appends bonus provenance and the earning-policy anchor. Existing production migration ordering must remain intact, with payout migrations appended afterward; an automatic conflict resolution or reordered list could make an already-applied version falsely represent another schema.

A separate current-main successor and deterministic upgrade controls are required before merging the payout release. Exercise the real existing-current-schema migration, repeated migration, missing-feature refusal and exact-policy initialization. Retain the completed financial630 and boundary/wallet component scopes; those tests did not use this newer upstream schema. Upstream deployment notes are not an independent production schema query. No migration or deployment was performed during this review.


## October 3 October 6 configuration publication

Reviewed config `93dc65fd` is now [published on config main](evidence/payout-config-publication-20261003.json), with exact `main/sn.yml` bytes preserved through fast-forward merge, pull/rebase and terminal push; remote main and clean worktree were verified. The cutoff remains `2026-10-06T00:00:00Z`, pre-cutoff USDC obligations may finish afterward, and mainnet activation remains explicitly blocked with readiness fields empty. This is configuration publication, not a deployed earning schedule or worker-adoption receipt. Corrected current Server migration composition and rollout verification remain required.


## October 3 completed wallet gate and combined payout normal result

Frozen Server `25f617dc` completes its [independent wallet GET gate](evidence/wallet-get-qualification-20261003.json): 26 normal and 26 race roots pass, controller vet passes, all fixture cleanup completes, and both causal modes discriminate nine intended assertion failures with five positive neighbors. The [race causal receipt](evidence/wallet-get-causal-race-20261003.json) retains explicit omission classifications; coordinator verification reads actual terminal events and all component bindings.

Frozen combined Server `2a453df9` separately passes its [ten-root normal composition scope](evidence/payout-get-composition-normal-20261003.json), covering retained read recovery during boundary hold, final-send boundary admission, public ST writer hold, cancellation causes, bounded continuation and atomic attempt-reset rollback. Race/causal/vet/build qualification remains pending. The newer current Server migration-order successor remains separate and must be qualified before production merge.


## October 3 combined payout static qualification

Frozen Server `2a453df9` passes [five-package vet and the offline CLI build](evidence/payout-get-composition-vet-build-20261003.json). Coordinator verification checks the seven bound files, terminal exit records and exact binary hash. This is static/compile qualification; it does not invoke the CLI or prove a deployment. Normal composition tests are separately recorded; race and operative controls remain pending. The newer migration-order successor must retain the complete upstream migration prefix and qualify an actual existing-current-schema upgrade, not infer safety from a fresh fixture.


## October 3 combined payout race and normal causal results

Frozen Server `2a453df9` passes the [same ten composition roots under race detection](evidence/payout-get-composition-race-20261003.json), with zero failures/skips/race reports and completed fixture cleanup. Its [four normal operative controls](evidence/payout-get-composition-causal-normal-20261003.json) all reach intended behavioral failures: lost cancellation cause, canceled continuation falsely acknowledged, missing retained GET recovery, and new-send hold conflated with accepted-attempt reconciliation. The exact prior error-format body and explicit omissions remain separately labelled. Race causal controls remain pending; this source does not include upstream18ec or establish the production migration upgrade.

The current-main successor also needs semantic integration coverage beyond conflict resolution: upstream18ec changes Redis escrow admission and settlement callbacks in otherwise disjoint files. Exercise actual contract close through that path into retained provider usage and cutoff accounting; a file-disjoint merge alone cannot prove unchanged financial behavior.


## October 3 completed frozen payout composition qualification

Frozen Server `2a453df9` completes its [independent combined gate](evidence/payout-get-composition-qualification-20261003.json): ten normal and ten race roots pass, four operative causal controls per mode reach intended assertions, five package vets and the offline CLI build pass, and all owned fixture cleanup completes. The [race causal receipt](evidence/payout-get-composition-causal-race-20261003.json) retains each control classification. The [independent source join](evidence/payout-get-composition-independent-join-20261003.json) is confirmed against all 46 physical files from the frozen components plus four explicit composition changes. Financial630 full-model results and wallet25f/boundarycdcb component receipts retain their original scopes.

The final current-main migration successor remains unqualified and unmerged. Preserve upstream18ec migration order and exercise the actual Redis-backed settlement integration before publishing that code; the completed frozen gate does not authorize a production upgrade or mainnet activation.


## October 3 claim normal execution and SQL-plan follow-up

Frozen SN `80030acb` passes [all 23 selected normal roots](evidence/claim-consumer-normal-20261003.json): sixteen consumer controls, two public worker neighbors and five actual producer controls, with zero failures/skips. Coordinator verification checks twenty bound files and actual terminal root names. Race/causal/vet and final module composition remain pending; the frozen producer347 graph is retained.

The unchanged financial630 [nested SQL-plan root passes separately](evidence/payout-retention-auto-explain-20261003.json). Its disposable PG18 fixture has the library; the original test role receives SQLSTATE42501 on LOAD. Capability is granted only to that synthetic role, then the exact root passes and fixture cleanup completes. All 27 receipt bindings and the actual terminal test event were verified. The original full-suite skip remains unchanged; no production database permission or newer Server source is qualified by this follow-up.


## Continuous claim expectation renewal

The frozen claim consumer requires one to sixteen independently declared epochs and binds its checkpoint to the exact policy hash, including that epoch list. This qualifies a finite reviewed expectation window. Adding a new epoch changes the policy identity and cannot be treated as a compatible reopen of the old checkpoint. It does not by itself implement continuous mainnet epoch monitoring.

Implement explicit bounded expectation renewal under the same reviewed pool/member/network domain. Preserve original policy references and completed observations; retain unresolved obligations across window changes and disclose archived/omitted coverage. Admit new expectations independently of producer assertions, reject altered original shares/roots/deadlines and foreign identities, and refuse capacity shrink that drops retained obligations. Do not delete checkpoints or silently relax their hash checks to admit a new window. Qualify actual public continuation through repeated renewals, overlap, unresolved old epochs, restart, history loss and exhausted capacity. This remains required under PH-17/PH-23/PH-28 and MG-07, separate from candidate800's finite-window gate.

## October 3 claim consumer race result

Frozen SN `80030acb` passes [all 23 selected roots under race detection](evidence/claim-consumer-race-20261003.json), with zero failures/skips/race reports. Coordinator verification checks all twenty bound files and actual terminal names across consumer, public worker and producer groups. Normal23 results remain separate. Causal and terminal qualification remain pending; published current-module adoption and continuous expectation renewal are separate requirements.


## October 3 claim normal causal qualification

Frozen SN `80030acb` reaches all [eight intended normal causal assertions](evidence/claim-consumer-causal-normal-20261003.json) across six groups: old public-policy admission, heartbeat/progress conflation, proof loss, receipt contradiction, healthy-peer cancellation and uncertain-publication continuation. Coordinator verification checks 39 bound files and actual failed-root events. One control uses the exact earlier operative policy body; five are explicitly labelled invariant omissions. Compiler/setup failures are not counted as causal evidence. Race causal execution remains active; final qualification and merge are pending.


## October 3 claim consumer merged on main

The independently configured claim consumer is integrated through SN `66f92220` after pull/rebase. Its [completed independent gate](evidence/claim-consumer-qualification-20261003.json) passes 23 normal and 23 race roots, mainnet/miner vet, and eight intended causal assertions in each mode. The [race causal receipt](evidence/claim-consumer-causal-race-20261003.json) preserves exact-old-body and explicit-omission classifications. The [merge source join](evidence/claim-consumer-merge-20261003.json) proves all nine changed code/test blobs are identical to qualified candidate800; other differences are documentation/evidence only. No duplicate execution is needed for unchanged tested bodies.

Current monitor policy and dispatch now include Claims alongside validator, operator and provider roles. This closes the missing finite-window claim-consumer code gap. Continuous expectation renewal, independent economic monitoring, current published-module release composition, real roster/alert delivery and production deployment remain open; MG-07 is not closed.


## October 3 current migration upgrade qualification

Current Server candidate `19952183` preserves the incoming `18ec7c05` 755-entry migration history exactly and appends payout provenance at version 756 and the immutable earning anchor at 757. The [source intake](evidence/payout-current-migration-intake-20261003.json) and [134-file composition join](evidence/payout-current-migration-source-join-20261003.json) were independently rehashed, including 143 intake bindings. Its [selected normal execution](evidence/payout-current-migration-normal-20261003.json) passes all 14 roots with no skips or failures and owned-fixture cleanup success. The scope includes actual 755→756→757 database upgrade, repeat idempotence, old conflicting payout history refusal, Redis settlement interaction and retained reconciliation. The separate [race execution](evidence/payout-current-migration-race-20261003.json) also passes all 14 roots with successful owned-fixture cleanup. Causal and static qualification remain separate; this candidate is not yet published or deployed.

The production lesson is to preserve already-published migration identities, not merely resolve overlapping source files. An appended migration must be checked against the actual incoming schema prefix and adjacent settlement callbacks before adoption. Do not replay an earlier alternative history at the same schema version, or relabel older-source full-suite results as current-composition acceptance.


## October 3 completed current migration candidate gate

Server `19952183` has completed its [independent candidate qualification](evidence/payout-current-migration-qualification-20261003.json): 14 normal and 14 race roots pass with no skips, failures or race reports; all three exact-old-migration controls fail as intended in each mode; five package vets and the offline CLI build pass. The [causal receipt](evidence/payout-current-migration-causal-20261003.json) and [independent source join](evidence/payout-current-migration-source-join-readback-20261003.json) were rehashed with their bound artifacts. This completes that immutable source scope, not production deployment.

A fresh publication fetch found Server main advanced to `ec6a038a` with hosted local-authority/proxy changes and updated modules after the candidate's `18ec7c05` base. Preserve both the qualified payout bytes and the new upstream work in a separate current publication composition. Reuse unchanged source-qualified outcomes with explicit joins; verify the changed dependency graph and its real consumers rather than assuming either that every test must repeat or that dependency changes cannot matter. The current publication join and release/recovery rehearsal remain outstanding. No migration or live payment was executed.


## October 3 combined-owner restore handoff

The [complete-union restore intake](evidence/restore-complete-union-intake-20261003.json) freezes SN `148ab9b4` and Connect `9fb897f8`. All 27 artifact bindings, both source heads/trees and 13 Git-backed source bindings were independently verified. Its proposed scope is six core and eight public roots covering complete disjoint owner coverage before staging, original heads/censuses, six-owner restore, real readers and cancellation after a partially published head. Compilation/preflight succeeded; behavioral normal/race qualification is still outstanding. This profile does not authorize restart or cover exclusive native/nonce/ledger/fleet/claim owners. Local execution-root rebinding, retained capacity revisions and final current dependency composition remain separate required work.

Current Server `ec6a038a` selects Connect `b8bd3c994855` and SDK `95ccd57da971`. The final composition must preserve that newer dependency floor and explicitly join its recovery code to qualified sources. Do not force the earlier published Connect `9e7ec0af` merely because it has an existing receipt: a qualified historical source is not authority to discard newer upstream behavior. Preserve historical receipts and qualify actual changed consumers on the selected current graph.


### Current Connect dependency join

Published Connect `5def2fa4` merges both `9e7ec0af` recovery APIs and `b8bd3c99` local discovery. The [independent Git source join](evidence/connect-current-recovery-source-join-20261003.json) verifies the complete durable-volume tree is identical to qualified `9e7ec0af`; only two local-discovery paths differ. The server's selected `b8bd3c99` branch alone lacks restore APIs, so the current consumer composition needs this combined published floor, with SDK `95ccd57d` retained. Source identity preserves earlier recovery qualifications within their original scope; it does not replace actual consumer compilation, changed-path tests or release rehearsal.


### Current Server publication source join

The [independent source join](evidence/server-publication-source-join-20261003.json) verifies candidate `84007fcb` retains all 46 financial paths exactly from qualified `19952183` and all ten newer upstream paths from `ec6a038a`. The path sets do not overlap and the join introduces no other changes relative to their common base. Preserve these qualified bodies while checking the changed dependency graph; this source receipt does not establish publication, deployment or final release acceptance.


## October 3 member restore test-only correction

The [independent affected-root gate](evidence/restore-member-correction-qualification-20261003.json) for `6b23b772` passes normal/race and mainnet vet. Its sole change from `f4e42066` is the test file; production and modules remain identical. The revised root retains the signed physical-directory refusal and proves closed custody, refusal to publish and no new nonce member. The original six-root public scope remains five passes/one failed assertion in each mode; no corrected-source full-suite pass is claimed. The [diagnosis erratum](evidence/restore-member-diagnosis-erratum-20261003.json) corrects a reversed predicate in receipt prose without changing original evidence.

The [original registry gate](evidence/restore-registry-original-failure-20261003.json) remains three passes/one diagnostic assertion failure per mode, with vet passing. Its actual exact-lineage refusal is preserved. The first separate diagnostic correction also encountered another mismatched expectation, so all fault cases are being traced together before further qualification. Constructor error outcomes must be judged by refusal and closed authority, not by an unrelated diagnostic substring or pointer presence alone. Local execution-root rebind and operational restore remain unqualified.


### Customer transfer continuation acceptance

The durable customer challenge successor must preserve caller intent/key/body across process and client restart, reconcile known challenges with GET, and keep exact amounts through the client wire boundary. Repeated equivalent provider observations must not exhaust retained capacity before terminal evidence arrives. Qualify more than 64 metadata-varying reads of one semantic state, later completion and contradiction, lost-response reconciliation, journal failure and cancellation. Missing caller IDs must refuse before submission; deployment must account for external client adoption, which is not proved by regenerated SDK bindings. This remains implementation/qualification work and grants no live transfer authority.


## October 3 complete-union gate and current module preflight

The [independent complete-union gate](evidence/restore-complete-union-qualification-20261003.json) now passes six core and eight public roots in both normal and race modes, with durable-volume/mainnet/durablehead vet success. Exact old-core and old-public test-only overlays produce all three intended missing-feature failures per mode. All 39 receipt bindings and actual terminal test outcomes were rechecked. This qualifies frozen SN `148ab9b4` / Connect `9fb897f8` on its inherited Server `c2563f9a` workspace; it is not a current-module, signed local-root rebind or production restore verdict.

Separately, [current-module preflight](evidence/current-module-5def-preflight-20261003.json) for SN `8cb08c95` / Server `0e04f038` passes real GOWORK=off graph/package resolution with published Connect `5def2fa4`, SDK `95ccd57d` and SCTP `6443417`. All bound artifacts were rehashed, including 32 downloaded durable-volume files joined to the qualified source. The two candidates change only module files from their immediate parents. Compilation, selected behavior, current-union integration and deployment remain separate. Qualified union/member fixes must be composed with current code and dependencies rather than left solely on historical branches.


## October 3 current compatibility and customer recovery handoffs

The [current public-module compatibility intake](evidence/current-module-compatibility-intake-20261003.json) fixes SN `8cb08c95` / Server `0e04f038` and 25 exact roots per mode across local authority/proxy/tunnel, payout/Redis/reconciliation and actual SN startup/restore consumers. All 71 bindings were rehashed. Sol has started the selected normal groups and passed the six Server/four SN package vets; terminal behavior and race receipts are still outstanding. This scope does not include unpublished union/local-root rebind code or establish a final production release.

The [customer-transfer source intake](evidence/customer-transfer-intake-20261003.json) freezes Server `530726fc`, SDK `9ae95704` and API-only Connect `56545699`. All 56 bindings and source heads/trees were rechecked; the [source join](evidence/customer-transfer-source-join-20261003.json) remains source-only. The successor retains request/key/body, GET-only known-challenge recovery, integer amount formatting and bounded semantic observation deduplication. Generated Go/JavaScript/C/C++ interfaces carry caller IDs; JavaScript rejects unsafe amount conversion before HTTP. Proposed controls cover 67 metadata-varying equivalent reads followed by completion, disabled custody guards, interrupted journals, contradictions, concurrent retries and client serialization. Behavior qualification and external application persistence/adoption remain open.

The [guarded cache reclaim](evidence/cache-reclaim-10-20261003.json) recovered 13.75 GiB from 450 inactive compiler archives at least 48 hours old. Process and metadata references, archive type/link count and exact physical identity were checked; all selected paths are absent. Source, evidence, modules, executable files and active caches were preserved. An initial missing sudo PATH entry for rg was retained separately; the successful absolute-path scan had no matches or diagnostics. The data volume had approximately 131 GiB available afterward, restoring qualification headroom without stopping a run.


## October 3 combined recovery dependency published

Connect main now publishes `08d48400`, verified against remote main with a clean local worktree. The [publication proof](evidence/connect-union-publication-20261003.json) joins the entire durable-volume subtree and module bytes to independently qualified `9fb897f8`, and every other path to newer upstream `ed82d358`. The [40-binding source join](evidence/connect-union-publication-source-join-20261003.json) preserves the earlier candidate. Rewritten ancestry caused pull/rebase add/add refusal before push; an ordinary merge preserved both sides, followed by successful pull and push. These recovery APIs are now published, but running compatibility tests remain pinned to `5def2fa4`; do not retarget them or infer adoption. SN union integration, local execution rebind, capacity revision and actual recovery remain separate.

The registry fault-case batch reached a genuine valid-resume defect after correcting diagnostic assertions: the storage reader admits the original-authorized rebind receipt, while the execution checkpoint inventory rejects that same retained file as unexpected. Unify authenticated receipt admission across construction, checkpoint, derivation and reopen paths. Test valid resume/restart/lost acknowledgment and reject forged, foreign or incorrectly staged receipts; do not fix this with a blanket filename-prefix exemption. Original failed scopes remain retained and no successful registry execution is claimed.


## October 3 current SN consumer qualification

The [independent SN consumer receipt](evidence/current-module-sn-qualification-20261003.json) now passes ten selected roots in normal and race modes, four-package vet and all source/dependency checks on frozen SN `8cb08c95` with Connect `5def2fa4`, SDK `95ccd57d` and SCTP `6443417`. All 38 bindings, actual test outcomes and vet exit were independently verified. This covers selected public startup/native/ledger/miner recovery consumers, not unpublished union/rebind/capacity code, the newer `08d48400` dependency or the full production release. Server `0e04f038` race qualification remains separately active; a later publication join must retain newer upstream Server `49450197`.

The [registry valid-resume failure](evidence/restore-registry-valid-resume-failure-20261003.json) records the genuine one-root failure on test-only batch `7a4123fe`, after invalid approvals correctly refused. Its metadata and actual terminal failure were rehashed. Static tracing identifies inconsistent authenticated receipt inventories between storage construction and checkpoint/replay. The production successor must qualify actual valid continuation, lost acknowledgment/reopen and forged/foreign-record refusal before this recovery gate can close. Earlier diagnostic-only failures remain distinct; no registry pass is inferred.

### October 3 completed compatibility and customer recovery scopes

The [current compatibility receipt](evidence/current-module-compatibility-qualification-20261003.json)
qualifies frozen SN `8cb08c95` / Server `0e04f038`: 25 selected roots
pass normal/race, ten package vets pass, and fixture cleanup exits zero.
All 122 receipt bindings were independently rehashed. This uses Connect
`5def2fa4`; it does not qualify the later `08d48400` composition.

Customer Server `530726fc` passes [14 selected roots in both modes](evidence/customer-server530-scoped-qualification-20261003.json),
but its original controller vet fails because a test does not release its
context on every path. The [test-only `a85c3e3f` correction](evidence/customer-context-correction-qualification-20261003.json)
adds the missing deferred cancellation; the affected root passes normal/race
and controller vet passes. Production and module bytes are unchanged. The
original vet failure remains a failure; no fresh full fourteen-root rerun is
claimed for the corrected source.

The [separate SDK scope](evidence/customer-sdk-qualification-20261003.json)
passes two Go roots normal/race, vet, six JavaScript checks and an actual C++
generated-interface roundtrip at SDK `9ae95704` / API `56545699`. An overbroad
offline module preflight appended unused checksums to the independent copy;
that failed attempt is retained. The tracked file was restored exactly before
the qualified Go reruns, whose before/after checksum guards match. Qualification
covers the imported package graph, not all unused module metadata.

Coordinator verification rehashed all 47 SDK, 49 original Server and 38 corrected
Server bindings and counted terminal Go root events in the qualified raw logs.
Customer causal controls, current-source joins, final model qualification and
publication remain separate work. Client adoption and deployed behavior are
unobserved. Latest Server `a244ae7c` on published Connect `08d48400` passes [sixteen roots in normal and race modes plus seven vets](evidence/server-a244-compatibility-qualification-20261003.json). Its scope remains selected compatibility, not the full model suite.

Continuous economic monitoring must catch up through authenticated bounded
historical pages rather than reject a cursor more than 4,096 blocks behind.
When an evidence-size limit is reached, commit only a complete verified subpage
and retain the original requested high-water; never skip history or reset the
cursor to latest. Native candidate `00d7efcd` implements this behavior and ten
new deterministic roots, including long outage, mid-page timeout, canonical
change, lost write acknowledgment and large census pressure. It is frozen for
independent qualification, not yet accepted. Continuous EVM reserve/credit/
capture monitoring, rolling claim expectations and composed recovery remain
required.

### October 3 Server publication

Server main now publishes `29ce22d6`; the [publication proof](evidence/server-publication-20261003.json) verifies remote main, clean custody and the whole source join. The financial/cutoff/retry source from qualified `a244ae7c` is preserved alongside upstream `87e7e8a6`. Coordinator verification independently rehashed all 88 qualification bindings and counted exactly sixteen passing roots per mode. No failed or skipped selected roots occurred; seven vets and both fixture cleanups exited zero.

The final pull observed a new upstream Redis accounting/test repair, and safely refused a divergent fast-forward. An ordinary merge retained its eleven changed paths plus the earlier documentation update; these do not overlap the financial change paths. The public escrow wrappers continue to apply Redis admission before entering their extracted shared bodies. The a244 receipt does not qualify the newly added upstream tests. Full current model qualification and customer recovery composition remain required. Published source is not deployed code or observed worker adoption.

### October 3 causal customer gate and cache review

Customer recovery now has independently verified [normal](evidence/customer-server-causal-normal-20261003.json) and [race](evidence/customer-server-causal-race-20261003.json) operative controls: six omitted/reintroduced mechanisms produce seven intended assertion failures per mode, with no compile/setup failures in those qualified runs. Coordinator verification rehashed 43 normal and 41 race bindings and matched the raw terminal roots and expected causes. This complements the successful original fourteen-root scope and one-root test-only cleanup correction; it does not rewrite their provenance. The separate initial runner path error is retained. Current Server/SDK/API publication composition is the next step.

The [eight-file cache source census](evidence/cache-source-census-20261003.json) finds three existing bounded mechanisms, avoiding duplicate implementation: authenticated original-intent receipt checkpoints, decision-local client-key authority reads, and cut-local exact assignment-signature reuse. Existing tests count actual body/HTTP/signature work and cover restart, eviction, changed runtime/route/boundary, independent client signatures and failed reads. Receipt scope explicitly excludes the executable hash; changed binaries alone need not discard that work. Reuse these controls against the final source/dependency graph and add a new cache only when a demonstrated uncovered repeat remains. This is source review, not a newly executed test gate or complete production lineage/capacity acceptance.

### October 3 customer libraries published

The [customer library publication proof](evidence/customer-library-publication-20261003.json) verifies SDK `9ae95704` and Connect `a53ed36a` on their remote main branches after clean fast-forward, pull and push. The SDK retains caller-owned request identity and exact amount handling; its [operative missing-intent control](evidence/customer-sdk-causal-20261003.json) fails at the intended assertion. API YAML exactly matches qualified `56545699`, while the rest of Connect remains exact `08d48400`.

The [complete customer source-join review](evidence/customer-current-source-join-20261003.json) independently rehashes all 5,380 Server, 3,729 Connect and 680 SDK tracked files, checks every Git blob/source correspondence and six original qualification/fence receipts. The Server candidate changes seven customer paths against published `29ce22d6`; final public dependency pins and current model qualification are being prepared. Earlier compatibility scopes stay unchanged. No deployed client adoption, live transfer or activation is claimed.

Qualification headroom: [reclaim11](evidence/cache-reclaim-11-20261003.json) removed 450 old, regular, single-link compiler archives (10.51 GiB) from the inactive cache after privileged process/reference checks and repeated physical identity checks. All deleted paths were verified absent; the active Sol cache, source and evidence remain intact. The volume returned to roughly 126 GiB free and existing test handles continued. Production sizing/restore gates remain open.

### October 3 native monitoring and final customer graph

Native `00d7efcd` passes [fifty selected roots in both normal and race modes plus mainnet vet](evidence/native-monitor-product-qualification-20261003.json). Coordinator verification rehashed all 44 bindings and counted exactly fifty passing terminal root events per mode. This is product qualification under the frozen `5def2fa4`/Server `0e04f038` graph; operative causal groups and final composition remain separate. The initial modfile-remapping setup failures remain recorded and are excluded from the behavioral census.

The [adjacent runtime renewal review](evidence/native-runtime-renewal-review-20261003.json) identifies a fixed-profile continuation gap: an approved new runtime can soft-hold the observer, while changing its policy prevents reopening the original checkpoint. A successor must admit a bounded monotonic reviewed runtime/read-purpose catalog, distinguish parent execution from post-state context, retain original historical profiles and preserve the same cursor, amounts and generation across renewal. Test a real upgrade boundary, approved renewal/restart and unknown-profile peer isolation. Preserve the completed00d gate; do not relabel it as qualification of the successor.

The [final customer intake](evidence/customer-final-intake-20261003.json) verifies Server `3f32d730`, all 5,381 tracked source entries, 26 intake bindings and all 3,581 Connect/523 SDK files in the public module archives against exact Git blobs. Its tracked graph now selects public API `a53ed36a` and SDK `9ae95704`. Twenty targeted roots, SDK compatibility and the required single full model run are underway or queued. Collect every targeted group and run the full model once source/fixture readiness is valid, even if an ordinary assertion fails; retain and repair all failures in a batch. Only structural corruption that invalidates the test run blocks that collection. Deployment and worker/client adoption remain unobserved.

### October 3 checkpoint recovery and full model execution

The [registry checkpoint fix](evidence/registry-checkpoint-fixed-qualification-20261003.json) at `08f32b7f` passes six selected roots normally and under race detection, plus mainnet vet. Coordinator verification rehashes all twelve bound files and counts six passing raw root events per mode. Shared authenticated receipt inventory now permits valid original-authority public resume and lost-ack recovery without admitting foreign receipts. The original `7a4123fe` failure stays recorded; historical/test-only causal comparison remains separate. This does not close local-root adoption with unfinished member outcomes, capacity revisions or full restored-service acceptance.

The native monitor's [eight operative normal controls](evidence/native-monitor-causal-normal-20261003.json) produce nine exact intended behavioral failures. Coordinator verification rehashes 43 files and matches the raw failed roots/assertions; setup and compile failures are absent from the qualified controls. Race controls remain pending. The fixed fifty-root normal/race product result retains its own scope.

Final customer Server `3f32d730` passes [all twenty targeted normal roots](evidence/customer-final-normal-qualification-20261003.json) and private fixture cleanup, verified against 28 raw/source/module hashes. The required full `./model` run has started on that exact source and published API `a53ed36a` / SDK `9ae95704` graph, alongside targeted race qualification. Preserve every assertion failure and optional skip, and feed actual failures into the next fix batch. No completed full-model result is claimed before the terminal receipt.


### October 3 current qualification and Redis refusal hardening

Final customer Server `3f32d730` passes [twenty targeted race roots](evidence/customer-final-race-qualification-20261003.json), complementing its twenty normal roots. [Five package vets and the CLI build](evidence/customer-final-vets-build-20261003.json) pass. The [current public API/SDK Go scope](evidence/customer-current-sdk-qualification-20261003.json) passes two roots in each mode and vet with unchanged module checksums. The [migration omission control](evidence/customer-current-migration-causal-20261003.json) compiles and fails at the intended missing-append assertion in both modes, with successful private-fixture cleanup. These selected results do not qualify the full model suite.

The required full model run remains active on the unchanged `3f32d730` source. It has exposed a genuine `TestCreateTransferEscrowSerializesWithDestinationDeactivation` failure: an inactive destination retains 1,024 escrow bytes rather than zero. The Redis admission path reserves request tokens before its later SQL eligibility checks; a known pre-SQL refusal can leave those tokens reserved. Preserve the failed run and continue collecting all failures while a separate successor fixes the class. No green full-model result or final publication qualification is claimed.

**Required adjacent fix.** Compensate only this request's exact attempted reservation tokens on every known pre-SQL exit: partial balance failure, inactive or mismatched identities, expired admission, caller cancellation and panic. Preserve unrelated live reservations and the original error/panic. Cleanup must survive caller cancellation; if transient Redis failure prevents completion, retain a retryable compensation obligation rather than silently leaking the reservation. Separate this known-no-SQL case from an uncertain SQL publication: reconcile persistence before releasing tokens that may back a committed escrow. Use deterministic barriers and actual public admission paths to verify both refused and successful/ambiguous outcomes, following `connect/CODESTYLE.md`. A short best-effort cleanup alone does not close this requirement. This lesson belongs to PH-03/PH-05/PH-11 and MG-06 recovery/accounting acceptance.

The [native monitoring race controls](evidence/native-monitor-causal-race-20261003.json) complete eight overlays with nine intended assertion failures and no setup/compiler failures. Coordinator verification rehashed all 43 bindings and counted the raw failed roots. The qualified fifty-root normal/race product gate retains its original source and dependency scope; reviewed runtime catalog renewal and continuous EVM/claim behavior remain open.

The [local execution-root intake](evidence/local-root-rebind-intake-20261003.json) at `2689fced` and [pending original-outcome successor intake](evidence/local-pending-outcome-intake-20261003.json) at `fcf41030` have independently rehashed 27 and 18 bindings respectively. Both remain compile-only, awaiting behavioral qualification. Their separate approval preserves original terminal payload, nonce and attempt authority; intake is not proof of successful restored-service continuation. Interrupted outer census-head restore, joined capacity revisions and the final current-module role composition remain required before closing PH-20/PH-23 and the corresponding launch gates.


The [registry old-source causal comparison](evidence/registry-checkpoint-causal-20261003.json) completes normally and under race detection: each mode exposes two intended public custody failures while the unapproved-receipt refusal remains passing. All nine bound files were rehashed and raw terminal events checked. Receipt v2 names the actual `source-preflight.json` directly instead of the original logical alias and retains the v1 digest; the other logical binding `old7a-source-fence.json` resolves to `/mnt/data/sn-testnet/sol-registry-rebind-7a-independent-20261003/evidence/source-fence.json`, as recorded by the sealer. No historical source or raw result was rewritten. This complements the fixed six-root gate; interrupted restore and capacity adoption remain open.


### October 3 incoming local gVisor dependency

An ordinary merge preserves incoming main commit `7debae6d`, which adds `replace gvisor.dev/gvisor => ../gvisor`. At this publication check `/home/by/urnetwork/gvisor/go.mod` is absent. This does not invalidate frozen qualification graphs, whose exact module declarations remain retained; it does mean current-main compilation and final release composition require the actual fork, its source identity and a reproducible dependency arrangement. Locate and bind the intended fork before testing the composed release. Do not remove the upstream replacement merely to reuse earlier green results, and do not claim those results qualify this new dependency.


### Restore fixture admission must exercise the complete owner plan

The first independent `2689fced` local-root control failed while constructing its complete owner-union preparation plan: `storage preparation capacities are absent or exceed the finite profile`. This occurs before the public local-rebind preview, so it is not yet evidence of a local-rebind production defect. Retain and finish the original test batch; diagnose whether the complete fixture omitted required profile values or exposed an actual bounded-profile mismatch. Do not widen production acceptance just to admit a test fixture. Before the next behavioral qualification, preflight the actual complete owner census, explicit capacity declarations, source graph and physical scratch ancestry. An empty/compile-only preflight cannot prove public restore admission. Any correction must retain the original failure and bind the corrected source/fixture separately.


### October 3 local gVisor fork materialized

The missing local dependency is now available through `/home/by/urnetwork/gvisor`, a symlink to the data-volume checkout `/mnt/data/sn-testnet/mainnet-gvisor-fork-20261003/gvisor`. The [source proof](evidence/local-gvisor-source-20261003.json) records repository `urnetwork/gvisor`, default branch `go`, commit `21c2a5da`, tree `e8600f8a`, and independent verification of all 2,440 tracked entries against their Git blobs and SHA-256 hashes. Its module declares Go 1.26.3. The checkout is clean; no frozen qualification graph was changed. This resolves the absent-directory obstacle, not compilation or behavioral acceptance of the new dependency. The final release must bind and qualify this exact source rather than depend on an unversioned sibling path.

The local-restore fixture mismatch has also been diagnosed: `localRebindTestRestore` requested a 16 MiB plan while the inherited production profile permits at most 8 MiB. Preserve original four-root normal/race outcomes and correct only the fixture request in separate successors for `2689fced` and `fcf41030`. This does not justify raising the production profile. The corrected fixture must exercise public planning and restore; compile success alone is insufficient.


The [original local-root normal failure receipt](evidence/local-root-original-normal-failure-20261003.json) preserves the exact `2689fced` source and raw outcomes: two fixture-planning failures and two passing authority-refusal controls. Coordinator verification rehashed every binding and matched the raw two failed/two passed root events. The failed public roots supply no restore acceptance; race/static results and the corrected fixture successor remain separate.


### October 3 qualification headroom restored

[Reclaim12](evidence/cache-reclaim-12-20261003.json) removes 450 old, inactive compiler archives totaling 7,633,731,584 bytes (7.11 GiB). The audited helper differs from reclaim11 only in its task directory. Privileged process-reference checks pass before planning and applying; the declared bounded metadata scan returns no references or diagnostics, and repeated physical identity checks plus final absence checks cover every selected file. The initial sudo PATH lookup failure is retained separately; the completed scan uses the absolute `rg` executable. Active test caches, source, evidence and executables remain intact. The volume returns to roughly 124 GiB free, allowing queued bounded qualification to continue without restarting existing handles. This is test-resource maintenance, not production capacity acceptance.


### October 3 corrected local-restore source join

The [fixture correction intake](evidence/local-fixture-correction-intake-20261003.json) binds `a07918db` over `2689fced` and `bdf63dea` over `fcf41030`. Coordinator verification rehashed all seven bindings and checked both exact Git parent diffs: each changes only `MaxPlanBytes` from 16 MiB to the existing 8 MiB limit in one test helper. Production and module changes are absent from those deltas. Independent qualification now runs seven roots on final `bdf63dea`: the two affected settled public paths, three pending-outcome paths and two authority-refusal controls. Results must name the actual tested final source; no separate acceptance of `a07918db` is inferred. Original failed `2689fced` runs remain retained.

**Optional recovery must not control foreground admission.** The Redis recovery draft needs an actual context budget for the whole page, not just a clock check between candidates. SQL and Redis waits must obey that budget. A timed-out optional SQL observation can abort a transaction even if its caller catches the error; never swallow that error and continue using an aborted transaction. Isolate optional recovery from healthy new admission where required, retain unknown tokens for later reconciliation, and verify slow/canceled recovery against a healthy foreground path with deterministic barriers. This is a review requirement for the separate successor, not a newly qualified behavior or authorization to release ambiguous SQL custody.


### October 3 full-model batch: companion query guard

The unchanged `3f32d730` full-model run found `TestOpenContractRuntimeQueriesUseStructuralPredicate` in addition to the real Redis refusal leak. The [exact-source review](evidence/companion-query-guard-review-20261003.json) verifies both relevant files against their Git blobs: the exported companion entry now delegates to the private shared implementation, whose SQL retains the structural outcome/dispute predicates. The test scans only the exported wrapper's string literals, so its zero count is a stale source-guard target rather than evidence that the runtime predicates disappeared. Preserve the failed run and correct the guard without weakening its required predicate counts. Explicitly protect public-wrapper linkage to the checked implementation; independently written EXPLAIN fixtures cannot alone prove the actual runtime query. Audit adjacent extracted-wrapper guards in the same batch, and qualify the final combined candidate. This source review supplies no newly passing test or completed model-suite acceptance.

The final `bdf63dea` restore batch has also exposed two pre-continuation pending-plan refusals: original members or owner checkpoints are omitted from its restore coverage. Keep collecting all seven roots, diagnose exact source/fixture coverage, and preflight reserved, acknowledged and first-stage variants before another freeze. Neither fixture setup refusal is silently converted into pending-continuation acceptance.


### October 3 completed restore failure collection

The [original `2689fced` race receipt](evidence/local-root-original-race-failure-20261003.json) preserves the same two capacity-fixture failures and two passing authority controls as its normal run. The [final `bdf63dea` normal receipt](evidence/local-pending-original-normal-failure-20261003.json) then collects all seven roots: four public paths fail at complete restore coverage admission, while first-stage generation and two authority controls pass; vet passes separately. Coordinator verification rehashed seven and nine bindings respectively and matched every raw top-level terminal outcome. None of the four public failures reaches successful local continuation. The shared inventory/checkpoint seam needs diagnosis across both settled and pending fixtures before another freeze; pending outcomes alone do not explain the whole failure class. Race collection continues on unchanged `bdf63dea`. Preserve these results and distinguish any fixture-only correction from a production coverage fix.


### October 3 paired-metadata core and signed-input placement

The [paired-metadata intake](evidence/paired-metadata-restore-intake-20261003.json) freezes Connect `5c48c3e5` and SN `11e282dc`. Coordinator verification rehashed all 25 intake bindings and checked clean commits, trees and all seven changed Git blobs. The [Connect primitive gate](evidence/paired-metadata-core-qualification-20261003.json) passes seven roots normally and under race detection, plus vet; all sixteen artifact/source/module/intake bindings and both raw seven-root censuses were independently checked. SN consumer qualification, old-source controls, restored preview/exclusive reconciliation and final source composition remain separate.

The [coverage diagnostic](evidence/local-coverage-diagnostic-20261003.json) reproduces one expected refusal after all fixed-owner planners and identifies eight unassigned external `.signed.bin` fixture inputs, with no omitted attributes. Its nine bound files and raw failed root are verified; the first missing-sibling workspace attempt remains recorded separately. The author traced these files to the test signer, while production retains imported bytes in `evmActionRecord.Signed`. The [separate `b35ca9c9` correction intake](evidence/local-signed-input-correction-intake-20261003.json) binds twelve files and changes only tests/documentation over `bdf63dea`: inputs stay beside the original config, journal/signature bytes are checked before restore, and all variants preflight complete owner coverage. Coordinator verification confirms the exact four-file delta and unchanged production/modules. This corrects fixture ownership; it does not exempt unknown runtime-root files or prove successful continuation before the actual public gate completes.

### October 3 Redis recovery source and failed fixture retained

The [frozen Redis intake](evidence/redis-recovery-source-intake-20261003.json) selects `f30583a5`, with production `164e42a6` and a test-only AST linkage correction. Coordinator verification rehashes all 23 intake bindings and all 5,384 tracked physical files against exact Git blobs; module bytes match `3f32d730`. Its recovery markers retain exact attempted tokens after exhausted/canceled cleanup, reconcile SQL custody under the original request fence, and isolate optional observation from the foreground transaction. These are source claims awaiting complete execution, not full release acceptance.

The [first seventeen-root normal receipt](evidence/redis-recovery-original-normal-failure-20261003.json) records sixteen passes and one fixture failure with successful owned cleanup. All 29 source/module/artifact bindings and the raw terminal counts are verified. The failing live-publication test holds a source-client share lock while synchronously awaiting a second caller's post-commit update on that same client. A separate test-only correction uses another client on the same payer network/balance; retain the original 30-second failure and verify the original request fence and 52-byte custody assertions. Extending the timeout alone would not repair that lock ordering.

The original full-model run also found public prober distribution and negative-reservation eligibility failures. They remain an open allocator review, not automatically harmless legacy assertions. Preserve public payer-hash spreading where required, retain exact allocation/priority and companion affinity, bound grant discovery/fallback work, and distinguish selection-cap holds from proven insufficient funding. Unknown/corrupt counters must not be silently repaired to zero; an independently valid neighbor may continue. The approved equal paid/free SN usage weighting does not authorize a change to Server funding eligibility. A separate allocator successor and final model qualification are still required.

### October 3 completed recovery qualification and remaining integration

The [retained historical EVM read scope](evidence/retained-evm-read-qualification-20261003.json) passes all nine selected roots normally and under race detection, with package vet successful. Root independently rehashed seventeen bindings and reproduced both raw positive censuses. Delayed expected observations retain their signed retry budget and custody; returned contradictions still refuse. Adjacent Safe reads and final composed startup remain separate requirements.

The [allocator causal controls](evidence/redis-allocation-causal-qualification-20261003.json) reproduce seven intended failures per mode across five groups, complementing the twenty-five normal/race positives. The [reservation old-oracle control](evidence/reservation-old-oracle-control-20261003.json) reproduces two intended failures per mode by restoring only an older test-state reader. It does not establish an old-production defect. Preserve these distinctions when reviewing the corrected full Server suite, which is still running.

The [latest inactive-cache reclaim](evidence/cache-reclaim-15-17-20261003.json) restores qualification headroom without deleting active caches, source or evidence. Continue the existing full suite rather than restarting it. Launch still requires continuous EVM and independent claim monitoring, fee attribution, restore/capacity continuation and one qualified composed release; selected green scopes cannot substitute for these requirements.

### Actual public paired-restore candidate

The [verified `39e2744a` source intake](evidence/paired-restore-public-source-intake-20261003.json) connects the paired census to actual local and registry recovery consumers. Root verified twenty bindings, the clean source identity and ten changed Git blobs. Passive preview retains the exact restored metadata pair and permits no repair; approved exclusive continuation reconciles separately while preserving original outcomes, signed bytes and attempt allowance. The [seven-root normal result](evidence/paired-restore-public-normal-failure-20261003.json) has five passes and two failures; original-outcome recovery and changed-member read behavior require a separate correction. Preserve the failed source and qualify its successor rather than claiming complete public recovery. Behavioral acceptance, joined capacity/retention changes and final current-module recovery remain required before this can support launch.

### Required Safe observations preserve original recovery authority

The [verified adjacent-read candidate](evidence/safe-state-read-source-intake-20261003.json), SN `296980a0`, distinguishes required code/storage/getter absence from intentionally nullable pending lookups. Original signed retry budgets remain unchanged. Transport failure or cancellation returns before authority comparison; a returned conflicting owner, nonce, guard, code or digest still refuses. Ten selected roots include actual public submission custody and unavailable archive state after a retained receipt. The [original normal scope](evidence/safe-state-read-normal-failure-20261003.json) has nine passes and one returned-conflict assertion failure; root verified ten bindings and the exact raw result. A separate correction, race qualification, five old-source controls and final composition remain required.

### Preserve monitoring progress through operational revisions

The [monitor policy renewal review](evidence/monitor-policy-renewal-review-20261003.json) identifies complete-policy hash comparisons in current provider and claim checkpoint readers. Before production adoption, implement predecessor-linked review of evolving expectations and acknowledged operational settings. A larger approved retry budget or retained-history capacity must preserve the original cursor, incidents and unresolved obligations; it must not require starting observation again. Immutable chain/contract/member/pool/source identity stays authenticated. Test actual reopen, old checkpoint compatibility, durable acknowledgement and refusal of resource shrink or unreviewed identity replacement. Apply the same rule to the new continuous EVM worker before its source is frozen.

### Native runtime and retry-budget renewal qualification

The [native renewal receipt](evidence/native-runtime-renewal-qualification-20261003.json) qualifies ten new roots and sixteen affected neighbors normally and under race detection, plus package vet and effective-graph preflight. Root verified twenty-eight bound artifacts, all four raw batches and twenty-six distinct passing names per mode. Purpose-scoped runtime review and acknowledged operational budget/capacity increases preserve the original cursor and history; they confer no signing authority. Causal and actual old-grammar controls remain separate, and this historical dependency graph still requires final current-release integration.

For general historical EVM gas debit/refund verification, the implementation route is authenticated native execution replay with the exact parent runtime and proved state, complete block body and deterministic host inputs. Payer balance deltas or generic same-phase balance events cannot replace attribution to the original fee hook. The fee-hook observation/equivalence mechanism remains an unresolved implementation dependency. A future dedicated runtime event cannot repair historical evidence. Keep actual native gas amounts unresolved until that witness is implemented and qualified; the continuous monitor must preserve unknown observations while independent healthy roles continue.

### Retained historical reads discriminate the original failure

The [old-source retained-read controls](evidence/retained-evm-read-causal-qualification-20261003.json) reproduce four intended failures normally and under race detection using the exact old production source plus one added test file. Root verified twelve artifact bindings, the exact overlay and each root’s failure assertion. The nine positive roots on the fixed source remain a separate result. Include this qualified correction in the current composed recovery path.

The bound [monitor read review](evidence/monitor-policy-renewal-review-20261003.json) also identifies single five-second GET samples and HTTP 408/429 classified as invalid. Provider and claim monitoring require at least sixty seconds total expected-read retry authority, normally three hundred, within one owned deadline. Retain immediate cancellation and permanent authentication/identity refusal, and never turn a failed observation into a current successful sample.

### Production capacity revisions retain original authority

The [production continuity source review](evidence/production-capacity-continuity-review-20261003.json) binds the actual config/history checks and distinguishes them from the simulator’s private V6 doubling helper. Implement an explicitly signed, predecessor-linked mainnet revision with retained and future resource accounting across ledger/history, upload, archive and resident limits. Preserve the original economic policy, activation, approvals and signed prefix. Exercise public preview and actual populated config reload; increasing resource bounds confers no new membership or spending permission.

### First continuous EVM monitor candidate

The [verified `0fb5db88` source intake](evidence/continuous-evm-monitor-source-intake-20261003.json) binds thirty-two artifacts and ten changed Git blobs. The candidate adds receipt/transaction checks, capture/carry/credit/payment accounting and a public durable observation worker; The [first seventeen-root attempt](evidence/continuous-evm-monitor-build-failure-20261003.json) failed to compile on a nonexistent peer service-event field; no root executed. Root verified twelve bindings and the compiler diagnostic. Qualify the corrected source separately; nine neighbors and seven operative controls remain required. Original cursor and identity survive monotonic resource growth, but the first candidate retains only the latest resource review. A separate successor must preserve bounded acknowledged revision history. Digest-format checks do not establish independently signed approval. Actual native gas debit/refund proof, independent entitlement and finality, archive lifecycle and final current-release integration remain open.

### Qualification headroom preserved without restarting work

The [guarded reclaim18–19 receipt](evidence/cache-reclaim-18-19-20261003.json) verifies removal of twenty-six unreferenced old compiler archives and recovery of 0.96 GiB. Active caches, module inputs, source and evidence remain retained. Keep existing full-suite and recovery processes; admit corrected sources only after checking current headroom. This is qualification-host maintenance, not production resource acceptance.

### EVM fixture successor and early composition

The [verified `a9eff813` test-only intake](evidence/continuous-evm-monitor-fixture-source-intake-20261003.json) corrects the peer event assertion using actual public fields, a fresh heartbeat and the resume barrier. Root verified five bindings and one changed test Git blob; production and module bytes match the failed `0fb` candidate. The [first seventeen normal roots](evidence/continuous-evm-monitor-original-normal-failure-20261003.json) completed with fifteen passes and two restart/cancellation exit assertions. Root verified twelve bindings and both raw failures. Resource review history remains a separate successor, and neither source-only intake supplies behavior acceptance.

Begin a separate current-main composition while capacity and scoped qualification finish. Keep original candidates immutable and distinguish qualified source scopes from pending recovery corrections, rolling monitors and capacity changes. Bind the intended Server `1d3c275b` (full suite still running), published Connect `a53ed36a`, SDK `9ae95704`, SCTP `6443417d` and gVisor `21c2a5da`, plus native renewal and historical-read corrections. Exposing source conflicts early does not close current-role startup or final release qualification.

### Exact fixture corrections retain original failures

The [Safe `9223d8d3` intake](evidence/safe-state-read-fixture-source-intake-20261003.json) and [paired-restore `3af77073` intake](evidence/paired-restore-fixture-source-intake-20261003.json) are verified test/document-only changes, preserving production and module bytes. Independent qualification remains pending. Account-specific retry counts must distinguish legitimate Safe proxy/singleton reads, and paired recovery must bind the exact reserved intent instead of exempting a filename prefix. Keep original normal/race failures and each corrected source distinct.

The [reclaim20 receipt](evidence/cache-reclaim-20-20261003.json) restores 5.56 GiB of local headroom under guarded inactive-archive deletion. Its documented four-MiB threshold does not weaken archive type, age, process/reference or identity checks. Do not stop an active qualification just because it crosses the admission floor for new heavy phases.

### EVM batch failures and independent neighboring positives

The [original seventeen-root normal batch](evidence/continuous-evm-monitor-original-normal-failure-20261003.json) preserves fifteen passes and two cancellation/restart assertions. A diagnostic-only narrower rerun passed both unchanged roots; that outcome is not a fix or replacement qualification. Add deterministic real-owner barriers to establish the cause before correcting cancellation versus integrity classification. The [separate nine affected roots](evidence/continuous-evm-monitor-neighbor-normal-qualification-20261003.json) pass normal execution, with twelve bindings and every passed name independently checked. Keep these scopes distinct.


## October 3 current recovery dependency published and EVM successor advancing

Use published Connect `631bcb282d392a24df6b509b89a0337c2fd4d7e7` for the next current-production composition. The [publication receipt](evidence/current-connect-paired-core-publication-20261003.json) verifies an exact mode/blob union of the 37 qualified paired-metadata core files and all outside files from newer upstream `a8a71432`, with terminal push/pull and clean matching local/remote main. The [seven-root normal/race core gate](evidence/current-connect-paired-core-qualification-20261003.json) retains its exact `1b1624f0` source scope. The published merge preserves those package bytes, but its current SN/Server/SDK/SCTP/gVisor consumer graph and final all-role behavior still need qualification. Preserve original frozen candidates and completed test bodies; do not rerun their censuses merely because this composition uses a newer source.

The [original paired-restore normal/race failures](evidence/paired-restore-public-original-normal-race-failure-20261003.json) remain five passes/two failures per mode. Corrected `3af77073` fixture qualification is separate. The [Safe `9223d8d3` affected-root gate](evidence/safe-state-read-fixture-affected-qualification-20261003.json) passes one normal and one race root plus vet; nine disjoint remaining race roots are still required. Both corrections preserve production/module bytes and do not silently erase failed original sources.

The [EVM `81de314b` intake](evidence/continuous-evm-review-history-source-intake-20261003.json) and [25-root normal result](evidence/continuous-evm-review-history-new25-normal-20261003.json) verify append-only reviewed resource history and actual checkpoint-load cancellation boundaries. Acknowledged history retains its original authority; hard identity/integrity/cleanup causes remain hard. This first positive scope leaves thirteen neighbors, race, omission controls, vet and final source composition open. The earlier `a9eff813` natural failures and diagnostic-only positives remain separate evidence.

The corrected Server `1d3c275b` full model suite continues on its original live handle; partial successful counts are not acceptance. Capacity revision controls, rolling claim expectation renewal and true transaction-attributed native payer debit/refund proof remain substantive work. Complete their qualification and final role/recovery composition before declaring mainnet behavior finalized. The published October 6 new-earnings boundary remains distinct from production deployment and worker adoption; pre-cutoff USDC obligations may finish afterward.


### October 3 completed disjoint Safe scope and adjacent resource review

The [Safe joined scope](evidence/safe-state-read-joined-qualification-20261003.json) verifies all ten selected roots through disjoint evidence: nine historical normal positives from `296980a0`, the corrected affected normal/race root on test-only `9223d8d3`, and nine newly passing race roots on that successor. Vet passes. The original one-root normal failure remains failed and explicitly retained; production/module bytes are unchanged by the fixture correction. Old-source causal controls, current graph and final release qualification remain separate requirements.

The [EVM neighboring normal scope](evidence/continuous-evm-review-history-neighbors13-normal-20261003.json) adds thirteen passing roots to the separately verified first 25 normal roots on `81de314b`; race and operative controls remain pending. The [adjacent resource authority review](evidence/monitor-review-authority-adjacent-review-20261003.json) verifies two exact Git blobs and identifies non-adjacent A/B/A review-digest reuse as requiring a separate deterministic correction. Retained review references are labelled local configuration references, not verified signatures. Final configuration admission must bind actual approval authority; no scoped result grants signing or deployment authority. The replay backend prototype is proceeding separately, with historical native withdrawal/refund attribution still absent until proved.


### October 3 consolidated current readiness checkpoint

Use the [current 38-requirement checkpoint](evidence/current-readiness-checkpoint-20261003.md) to distinguish completed component implementation from remaining composition and live gates. It supersedes old absent-claim/customer/native-monitor labels without closing any whole PH/MG requirement or inheriting qualification across sources. The [current remote config inspection](evidence/current-cutoff-readiness-config-20261003.json) confirms the same October6 cutoff and pre-cutoff USDC obligation policy at config main `ba97927f`; `main/sn.yml` remains the exact published blob `05b56036`, activation is blocked, and deployment/readiness fields are empty. No local branch switch, production adoption or launch authority is inferred.


### October 3 corrected outer recovery and EVM race results

The [corrected outer fixture gate](evidence/paired-restore-fixture-affected-qualification-20261003.json) verifies three affected public roots passing normally and under race detection, plus vet, on exact test-only `3af77073`. All seventeen receipt bindings and raw per-root terminal results were independently rehashed/read. The original `39e2744a` two failures per mode remain failed historical evidence; the other five positive roots retain their original scope. Production bytes are unchanged by this correction. Current published consumer composition remains a separate requirement.

The [EVM review/history 25-root gate](evidence/continuous-evm-review-history-new25-normal-race-20261003.json) now passes all selected roots normally and under race detection at `81de314b`; sixteen bindings and both exact root censuses were independently verified. Thirteen affected race neighbors and ten operative control groups remain pending. Non-adjacent review-digest reuse still requires its separate successor. No native fee witness, whole-role acceptance or live economic approval is supplied by this receipt.

The [guarded cache reclaim](evidence/cache-reclaim-21-20261003.json) removed 450 inactive compiler archives, recovering 2.37 GiB. The helper changes only its receipt directory relative to the previously verified helper. Both privileged process censuses, no-match metadata reference scan, exact identity/age/archive guards, plan hash and allocated-byte sum were checked; all selected paths are absent. Active caches, modules, sources, evidence and executable files were preserved. Available data-volume space was approximately 114.66 GiB afterward. The 110 GiB admission floor does not stop or restart existing work.


## October 3 signed resource-capacity continuation candidate

The [production capacity intake](evidence/production-capacity-revision-source-intake-20261003.json) verifies `b1c5fb23`/tree `0409413c`: 29 artifact bindings, fourteen exact changed Git blobs, unchanged module bytes, and clean validator/mainnet preflight and compile-only results. Independent behavior is queued for eleven new roots, four authority-history neighbors, old-source controls, race and vet. No behavioral pass or complete all-role capacity closure is inferred.

The candidate adds `sn-mainnet validator-capacity-preview --request FILE --request-sha256 sha256:HASH`, using the ordinary explicit durable-volume reference. Its request pins the original production configuration, every selected original ledger head, physical roots/former-writer fences and finite retained/future forecasts. The response contains an unsigned proposed config, signing bytes, original authority bundle and physical/ledger census; it grants no restart, signing, publication or economic-window authority. The existing independent approver must separately sign the full-config commitment. Only enumerated aggregate limits grow, covering retained plus forecast use with the selected factor of two. Economic policy, identities, approvals, per-object/transport limits and active membership remain fixed; actual retained-prefix admission remains required when producers open.

Preview holds exclusive custody leases and refuses active writers. Plan affected-owner quiescence and explicit successor adoption while healthy peers continue, before resource exhaustion; do not describe this slice as an automatic online upgrade or a reason to restart the entire fleet. Other role retention profiles, warning/adoption behavior, the fixed bootstrap member census and final current-module composition remain required. Accounting headroom of 2,048 slots does not authorize 2,048 active/funded members.


### October 3 current production module join and EVM causal controls

The [current module source join](evidence/current-release-module-source-join-20261003.json) verifies clean SN `b18e76dc` and Server `5089a9dd`: 20,377 and 5,387 physical Git blobs respectively, exact production mode/blob equality to their parents outside `go.mod`/`go.sum`, and unchanged final heads/trees. Both select published Connect `631bcb28`, SDK `9ae95704`, SCTP `6443417d` and the explicit local gVisor fork. The [compile-only receipt](evidence/current-release-module-compile-20261003.json) verifies all 28 bindings and terminal clean-source exits for both actual GOWORK=off graph/dependency preflights and five package test binaries. This candidate excludes pending capacity/EVM successors; the live Server `1d3c275b` full model stays on its original graph. Independent current-role behavior, final source/dependency provenance and release qualification remain required.

The [EVM normal causal receipt](evidence/continuous-evm-review-history-causal-normal-20261003.json) verifies ten operative omission groups and twelve exact intended failed roots. Coordinator checks cover 59 receipt artifacts, forty author/independent overlay/source/replacement bindings, exact remapped overlay tables, and actual terminal roots and named behavioral assertions. These are expected control failures on frozen `81de314b`, not new product regressions or old-whole-source causal claims. Affected-neighbor race and race controls remain pending.

Qualification fixture admission must protect its owned durable root/ancestor modes before opening storage owners. A fixture setup refusal does not justify weakening production custody checks. Preserve failed setup evidence, correct only the owned environment, and rerun only affected bodies when the earlier positives are independent of the corrected metadata. Capacity `b1c5fb23` initially passed five validator roots and refused two at the owned unprotected ancestor; independent corrected execution and remaining scopes are pending, not inferred from the compile pass.


### October 3 corrected full model completion and current publication seam

The [corrected Server full-model receipt](evidence/customer-corrected-full-model-terminal-20261003.json) is terminal PASS at exact `1d3c275b`: 1,484 top-level roots, 1,473 passes, zero failures and eleven individually documented optional/input-dependent skips. Coordinator verification rehashes all 23 artifacts, reads every raw root event and skip reason, checks the single passing package terminal at 6,572.307 seconds, all service/runner cleanup exits and the exact owned network cleanup ID. The original `3f32d730` nine-failure result stays retained. This completes that model scope on its own graph; current module and deployment acceptance remain separate.

Publication must also preserve newer Server main `f024b11a`. The [readonly integration review](evidence/server-current-publication-integration-review-20261003.json) finds three actual conflicts against `5089a9dd`: migrations, module declarations and Redis admission. A separate Astra successor must preserve the published migration prefix, bounded grant selection, shrink-to-fit, and exact asynchronous debit-journal authority alongside owned-token recovery. Already reduced amounts and pending debit debt cannot be interpreted as absent custody or prematurely released. Review clean textual merges at the same semantic boundary and add deterministic affected/adjacent controls; the completed old-source model run is not evidence that this new join works.

The [EVM 38-root product gate](evidence/continuous-evm-review-history-product-qualification-20261003.json) passes every selected root normally and under race detection, plus vet, on `81de314b`. The [ten-group causal gate](evidence/continuous-evm-review-history-causal-qualification-20261003.json) discriminates twelve intended failed roots in each mode; 99 artifacts, exact selectors and named assertions are verified. These scopes do not supply the separate digest-reuse successor, true historical fee debit/refund witness, current-source composition or live observer acceptance.

The [guarded cache reclaim22](evidence/cache-reclaim-22-20261003.json) recovers 14.37 GiB from 4,500 old inactive compiler archives, preserving active/warm caches, source, evidence, modules and executables. Minimum archive size is now1MiB and the administrative batch cap4,500, while the16GiB ceiling,48-hour age and all identity/process/reference/archive guards remain unchanged. An overbroad helper text replacement was caught and retained before deletion; the final helper uses four exact context replacements and an independently verified reverse comparison. Available data volume space was approximately125GiB afterward, admitting queued qualification without restarting any work.


### October 3 historical proof completeness and retry-attempt admission

The [historical proof-backend qualification](evidence/historical-proof-backend-qualification-20261003.json) verifies twelve synthetic replay roots on fixture-corrected `93907cd3` and two isolated operative guard omissions. All 21 receipt artifacts and fourteen physical crate sources match their recorded hashes and exact Git content. Removing either top-level or child write-path precheck compiles but incorrectly accepts the unchanged declared root; the corresponding test fails at its intended assertion. The pinned SDK can fall back to the original/default root after an internal proof error, so an apparently matching post-state root alone is insufficient. Preserve fallible proof-completeness checks before root calculation; incomplete reads or writes must publish no authenticated fact.

The original `89e4c8c` fixture failures remain retained. Runtime-version fixtures must advertise the Core API layout that their SCALE encoding uses and assert strict encode/decode round trips before exercising execution. The correction changes only the test fixture; it does not authorize a runtime. This qualification uses caller-supplied anchors and synthetic runtime bodies. Actual mainnet runtime host admission, independent finality, native fee withdrawal/refund attribution and the production caller remain required for MG-03/MG-06.

HTTP resilience must admit a successful slow read within its total retry budget. A 300-second budget with five-second header/body attempts still rejects reads that need longer than five seconds. The separate `ff42a433` successor raises attempts to sixty seconds clipped by the remaining owned budget, adds typed transient route/DNS errors, and supplies the missing shared `rpcWait` hook declaration. Sol qualification is pending; source compilation does not establish behavior. Retain permanent identity/authentication errors, NXDOMAIN and mixed hard causes as failures, and keep this GET policy separate from transaction broadcast reconciliation.

Signed configuration previews must commit to the exact strict-decoded document that will be loaded. Nil versus empty collection round trips must not invalidate a retained approval. Test actual public preview→independent signing→document export→production reload without an in-memory rehash shortcut; preserve economic authority and original signed sidecars. The capacity candidate's remaining canonical-hash failure is being corrected in a distinct successor and is not a reason to bypass production approval checks.


The [capacity affected-scope failure receipt](evidence/capacity-affected-canonical-failure-20261003.json) independently verifies 27 artifacts and all four raw selected roots in each mode: three pass and the retained original-sidecar root fails at the same complete-configuration approval hash normally and under race detection; vet passes. These are terminal results, not ongoing tests or a capacity acceptance. Keep the failure visible while qualifying the separate fixture and public pre-sign document round-trip successors. Preserve the passing independent scopes rather than restarting unrelated work.


### October 3 body errors must retain identity and hard-cause precedence

The [HTTP attempt source review](evidence/monitor-progress-attempt-source-review-20261003.json) verifies all 33 nested intake bindings, four changed Git blobs and eight clean peer heads/trees for `ff42a433`. Qualification remains separate. The shared transport classifier handles mixed causes, but both actual provider and claim body paths still promote any read/close error to retryable and check complete identity only when the read error is nil. This adjacent gap requires a distinct successor: explicitly classify transient body/close I/O failures, preserve permanent causes inside joined errors, reject a completely decoded foreign identity even when the final read also reports a transport error, and continue retrying genuinely incomplete bodies. Add deterministic positive partial-body recovery and negative mixed-hard/foreign-identity controls for both consumers. Do not replace final integrity checks with a blanket retry, or classify an incomplete JSON sample as authoritative malformed evidence.


The [HTTP attempt selected qualification](evidence/monitor-progress-attempt-selected-qualification-20261003.json) now verifies 29 selected roots normally and with race detection, with zero failures or skips, plus vet and effective dependency preflight. All 31 artifacts, four independent overlay Git blobs and exact raw root/package terminals are checked. This proves the selected timeout/status/network/peer-continuation slice at `ff42a433`; seven operative controls and the separately discovered body/close hard-cause successor remain pending. Retain the original `a5d2c5b4` compile failure and do not infer rolling policy, composed-release or deployment acceptance from this slice.


### October 3 current Server financial integration preserves published history

The [current financial source join](evidence/server-current-financial-source-join-20261003.json) verifies clean `5f2edd47` against both merge parents: all 5,439 physical Git blobs and modes, 27 artifact bindings and four clean graph/compile-only jobs. Exactly 5,326 blobs are retained from the qualified parent, 104 match incoming published main and nine differ from both. The author reviews ten files because the retained module file is also in its explicit review scope; review categories must not be mistaken for changed-blob counts. Published `f024b11a` migration source is byte-for-byte preserved with only the exact final customer migration append before the closing brace.

Current settlement uses the incoming durable asynchronous debit journal. Recovery must retain pending payer consumption until committed debit application, recognize already reduced token amounts and preserve debt for archived contracts; provider payout rows are not payer-debit authority. Bounded grant selection may shrink only after a complete census, and public consumers must use the actual granted amount. The new merge has its own 22-root normal/race and five-package vet scope with operative controls. Source/compile verification and the completed older full model are insufficient to accept the merged behavior, database rollout or final release.


The separate [HTTP body-error source successor](evidence/monitor-progress-body-source-intake-20261003.json) is frozen at `1a4524f2`: complete decoded identity now precedes a terminal read fault, and observed body/close causes override status-only retry permission through all-causes transient classification. Explicit EIO remains recoverable; mixed permanent causes refuse. Four new tests cover both consumers, with seven neighbors and separate prior-reader controls. Source and artifact checks are complete; Sol behavior qualification remains pending, so the earlier discovered gap is implemented but not yet accepted.


The [HTTP retry normal controls](evidence/monitor-progress-attempt-controls-normal-20261003.json) verify seven isolated groups and fourteen intended failed roots at `ff42a433`. All 52 artifacts, exact remapped replacement tables, source hashes and raw named assertions are checked; every group compiled and reached its behavioral assertion. This complements the separate 29-root normal/race product gate. Race controls and the body-error successor remain distinct scopes.

The [guarded cache reclaim23](evidence/cache-reclaim-23-20261003.json) removes 2,568 old inactive compiler archives, recovering 3.30 GiB. The helper changes only its receipt directory; both privileged process censuses and all reference, identity, age, size and archive checks remain unchanged. An unprivileged metadata scan encountered protected directories and was retained; deletion occurred only after a privileged clean scan. All selected paths are absent. Approximately117.35GiB remains available, admitting queued qualification while preserving active caches, modules, sources, evidence, executables and every running job. The110GiB floor governs admission of new heavy work, not termination of existing work.


### October 3 qualified current Server published; preserve explicit remaining gates

The [current Server product qualification](evidence/server-current-financial-product-qualification-20261003.json) verifies all 22 selected roots normally and under race detection, five-package vet, owned fixture start/cleanup and graph preflights. All 84 unique artifacts, 5,439 independently archived Git blobs/modes, the actual 647-module graph and seven clean pinned peers are checked. The [isolated prior-file controls](evidence/server-current-financial-controls-qualification-20261003.json) verify three exact one-file overlays and five intended failed roots in each mode, with 55 artifact bindings and clean fixture lifetimes. These tests cover the merge's bounded shrink, pending/applied/archived debit custody, public consumers and lost-ack hook; the older `1d3c` full-model result retains its separate source.

The [publication receipt](evidence/server-current-financial-main-publication-20261003.json) verifies remote main and the ready release branch at exact `5f2edd47` / tree `4728109a`, plus both clean local checkouts. Published `f024b11a` is retained as a merge parent; remote main advanced without force. The local main was pulled only after a privileged 385-process ownership census found no active references. Frozen test sources and earlier module peers were preserved. Publication does not apply database migrations, deploy services, sign transactions or close the remaining composed-release/mainnet gates.

The [HTTP retry causal qualification](evidence/monitor-progress-attempt-controls-qualification-20261003.json) now verifies seven groups/fourteen intended behavioral failures in each mode, 89 unique artifacts and exact remapped replacement tables. This completes that causal scope alongside its 29-root product scope; body-error and rolling-policy successors remain distinct.

The [capacity wire failure review](evidence/capacity-wire-producer-failure-review-20261003.json) separates two failures at frozen `e606947d`: the original sidecar fixture mixes envelope allowances, while the actual preview serializes untagged nested bounds with JSON Go field names that the strict YAML loader rejects. Both recur normally and under race detection, with vet passing. The successor must emit a loader-compatible exact ConfigDocument before signing, then complete only the established approval descriptor after independent signature and verify the exact emitted document through the public loader. Validate the whole original profile after all fixture helper overrides; preserve approvals and envelope checks. Neither failure is waived or counted as a capacity pass.


### October 3 body-error retry qualification

The [body-error qualification](evidence/monitor-progress-body-qualification-20261003.json) verifies the exact `1a4524f2` successor: four new and seven adjacent roots pass normally and with race detection, with vet and dependency preflight passing. Two exact prior reader bodies produce two intended failures and two positive controls in each mode; 54 artifact bindings and six independent source overlays were checked. Complete decoded foreign identity must remain a permanent refusal even when the final read or close times out. Partial transient body failures and explicit EIO close failures remain retryable; a mixed permanent cause cannot borrow retry permission from HTTP status or another transient cause.

This qualifies provider/claim body handling on its recorded Connect631/SDK9ae/Server5089 graph. It does not qualify the final combined release, native fee authority, policy renewal, archive rollover or live activation. Preserve successful checkpoints and retry only affected reads. Capacity CLI tests currently demonstrate correct strict rejection but still require correction of negative-test serialization and diagnostic fixtures; no failed test is waived or counted as passing.


### October 3 retained monitoring policy renewal qualified

The [policy renewal qualification](evidence/monitor-policy-renewal-qualification-20261003.json) verifies `1a98ef89`: nine new and ten adjacent roots pass normally and with race detection, plus vet and graph preflight. Three isolated compiling controls produce six intended failures per mode. Ninety unique artifact/source bindings, eleven exact Git overlays and the original four-field JSON grammar are verified. The grammar fixture is synthetic, bound to prior source bytes; it is not claimed as output from an old executable.

Budget, review-history and claim-epoch capacity may grow without dropping the original roster, expected debts, policy origin or acknowledged history. Record a policy acknowledgment only after durable publication and owner verification succeed. Refuse predecessor mismatch, resource shrink, immutable expectation changes and nonadjacent review-reference reuse. A local review reference is not a cryptographic signature or payment/finality authority. Finite growth to128 entries does not close indefinite archival rollover: archive provisioning, capacity forecasts with the approved2x margin and actual host adoption remain launch work. Qualification uses the explicit Connect631/SDK9ae/Server5089 graph; final release composition remains separate.


### October 3 EVM retained review history qualified

The [six-root product qualification](evidence/evm-review-reuse-selected-qualification-20261003.json) verifies `8a2693c2` normally and with race detection, plus vet. The [exact prior-file control](evidence/evm-review-reuse-causal-qualification-20261003.json) restores only the old81de resource-history validator and reproduces both intended failures in both modes. Thirty-two product/source bindings and seventeen causal bindings are checked; a first control overlay with duplicate declarations is retained as a harness failure and excluded from causal evidence.

Every nonempty review reference must be unique across the entire retained history. Comparing only adjacent revisions permits an internally hashedA-B-A chain to reuse already acknowledged authority. Preserve all resource-growth, predecessor, content-hash and durable acknowledgment checks. This is local review provenance, not a cryptographic signature or payment authority. The qualified graph remains historical5def/SDK95/Server0e; the final current-source release must incorporate this exact correction and qualify its changed composition.

The [capacity fixture review](evidence/capacity-test-successor-source-review-20261003.json) verifies two successors change only one test file. A canonical public approval and full config-loader baseline must succeed before negative faults can be meaningful. Each refusal must reach its expected boundary, return the exact refusal code, emit no config and preserve custody; no production guard is relaxed to accommodate fixture errors.


### October 3 public capacity completion qualified

The [public completion qualification](evidence/capacity-public-completion-qualification-20261003.json) verifies the `214a6b3d` test-only successor over unchanged42ef production. Three unique selected roots pass normally and with race detection; mainnet and validator vets pass. Thirty-four artifact/source bindings and seven exact Git overlays are checked. The mainnet roots execute directly on the successor test source, while the byte-identical validator sidecar results retain their original42ef source.

Use the actual emitted strict-YAML configuration document and exact preview/signing message through the public approval/completion/config-loader path. Complete readonly output only after all independent authority, signature, hash and semantic checks succeed; report short writes without claiming delivery. Tests must admit the valid baseline before injecting negative faults and verify each fault reaches its intended boundary. Earlier canonical-envelope, diagnostic and private-directory setup failures remain recorded. This closes the selected public CLI slice only: final current-source composition, aggregate sizing, signed archive-capacity adoption, backup/restore and deployed host adoption remain required.


### October 3 subprocess bounds must cover fast-copy dispatch

The [source review](evidence/subprocess-fast-copy-bypass-source-review-20261003.json) identifies a common output-control bypass in the replay caller, owner signing adapter and service storage inspector: anonymously embedding `bytes.Buffer` promotes `ReadFrom`, which permits `io.Copy` to bypass the wrapper's guarded `Write`. This skips output limits and can skip the callback that cancels an owned process. Use a named private buffer and test the real pipe-copy and subprocess paths, including both stdout and stderr, overflow, cancellation and descendant cleanup. Audit related `ReaderFrom`/`WriterTo` fast paths and post-allocation output checks. Preserve original source/test failures and signing authorization semantics; the source finding alone does not qualify the fix.


### October 3 native history archive and current release source

The [native archive source review](evidence/native-archive-source-review-20261003.json) checks22 bindings, nine exact changed paths and unchanged modules at `3bc2f9a5`. Public `monitor-native-archive plan` and `apply` retain original checkpoints, cursor, pending high-water, cumulative amounts and runtime reviews. Publish and authenticate the archive before compacting active history. Require provisioned owners, exact file references and a joined affected-writer fence; healthy roles continue. The initial128-segment profile requires explicit forecasts and2x segment/byte/inode margins. Signed catalog growth, claim/EVM adapters, complete archive backup/restore and behavioral qualification remain required. This is source review, not deployment or a tested rollover claim.

The [frozen866 release source review](evidence/ready-866-source-join-review-20261003.json) independently authenticates20,453 SN and5,439 Server physical Git blobs/modes plus six merge parent/base joins. Its [556 publication successor review](evidence/ready-publication-source-successor-review-20261003.json) adds only two exact qualified8a Go blobs plus published docs/evidence, retains083main history and leaves modules unchanged. The16-root current-source composition is still pending; native archive, replay and native-fee observer are separate increments. Preserve those explicit exclusions until their qualified production changes are joined.

The [guarded cache reclaim25](evidence/cache-reclaim-25-20261003.json) recovered1.53GiB from3,877 old inactive compiler archives; process, reference, age, identity and archive guards remained unchanged. Source, evidence, modules, executable artifacts and active caches are retained. Resource admission can defer new heavy jobs but never invalidates completed work or stops active jobs.


### October 3 assembled release source and compile checks

The [candidate source/compile review](evidence/current-release-556-source-compile-review-20261003.json) independently verifies20,461 physical tracked Git blobs, modes and SHA256 values at `556e1476`, plus33 artifact bindings. The candidate joins the qualified recovery, EVM review history, GET retries, retained policy renewal and public capacity completion with published Server5f. Clean unchanged-source module/dependency preflights and affected role compiles pass. A prior Server package-list command named nonexistent `./storage`; its failure is retained, and the corrected command passes on the same source.

The candidate still requires16 targeted public roots normally and under race detection plus five-package vet before publication. Earlier component receipts keep their distinct sources and graphs. Native archive rollover, historical replay and native-fee observer are excluded from this candidate until their independent fixes and qualification are joined. Source/compile success does not establish deployed identity, runtime approval, owner-device custody, complete conservation or settlement behavior.


### October 3 assembled production increment qualified

The [composed qualification](evidence/publication556-composed-qualification-20261003.json) verifies68 bindings and all16 targeted public roots normally and with race detection at `556e1476`, plus vet in mainnet, validator, miner, chain and crv4. The actual current graph uses published Connect631/SDK9ae/SCTP644, Server5f and gvisor21c2. The qualified increment joins retained recovery, EVM history/review reuse, provider/claim retries and policy renewal, and public capacity completion. Merge and publication must preserve these exact production inputs while retaining newer documentation. Full reproducible images, actual host/device/runtime admission and observed settlement remain separate requirements.

The [original replay failure review](evidence/replay-caller454-original-failure-review-20261003.json) preserves all ten original public roots: seven pass and three fail around300 seconds at partial-output, output-bound and blocked-input cancellation. A separate later-root invocation reproduces the same blocked-input failure. These failures are not waived. The output successor is independently tested while original evidence stays immutable.

The [original-Wasm selected qualification](evidence/original-wasm-observer-selected-qualification-20261003.json) verifies24 bindings, fifteen exact Rust Git files and nineteen passing library roots on the locked/offline graph. Original-code host frames, rollback and post-state logic remain synthetic scope; binary and causal receipts are separate. Native fee withdrawal/refund amounts and finality authority remain unproved until the actual-runtime witness/profile is admitted.

Test harness isolation must cover compiler caches and child environments. Distinct mutated Rust crates with the same package identity can reuse a stale artifact when copied source mtimes precede the shared target; require verified mutated source and actual recompilation before accepting causal results. Pass parent Go modfile/overlay flags directly to the test command rather than leaking them through GOFLAGS into synthetic child modules. Preserve invalid setup attempts separately; they are not product behavior or causal evidence.

The [cache26](evidence/cache-reclaim-26-20261003.json) and [cache27](evidence/cache-reclaim-27-20261003.json) receipts verify removal of inactive regenerable compiler archives only. Cache27 extends selection to archives older24 hours, preserves active-cache/process/reference/identity guards, and applies a strict383-file ordered subset below16GiB; an original trailing archive that exceeded the generator budget remains preserved. It recovered15.99GiB, restoring qualification headroom without stopping active work. Resource admission must use the remaining job increment and2x margin above the retained floor, then recheck between new phases.


### October 3 qualified production composition merged into main

The [merge review](evidence/qualified-composition-main-merge-20261003.json) records `e34c683e`, joining qualified556 into main while preserving newer plans and evidence. Every tracked input outside the two launch plans and mainnet/evidence is byte-exact to the independently tested candidate, including Go, modules, configurations, contracts, scripts and test fixtures. Documentation-only differences do not require repeating the16-root gate. This increment includes recovery/preparation/restore, EVM observation and review reuse, provider/claim retries and retained renewal, and public capacity completion. Native archive, replay/output and actual native-fee increments remain separately qualified work; final images, authority, custody, deployed adoption and live settlement remain open.


### October 3 replay cancellation qualified; native fee decoder and restore limits

The [publication readback](evidence/qualified-composition-main-publication-20261003.json) confirms qualified production inputs on remote main at `ac8a09ae`, following successful pull and push. Subsequent documentation updates do not change that code scope.

The [replay output qualification](evidence/replay-output-caller1b3f-qualification-20261003.json) independently verifies103 artifact bindings, six changed Git blobs and their actual execution overlays. All23 selected roots pass normally and under race detection; mainnet vet succeeds. Five omission groups produce six intended behavioral failures in each mode. Named buffers prevent promoted ReaderFrom from bypassing output guards, active overflow cancels children, and bounded collection applies to adjacent owner signing, volume inspection, repair and source-lock commands. Preserve the original454 genuine timeout failures and the three separate child-GOFLAGS harness failures; only the latter were corrected by invocation isolation. This qualification uses the historical Server5089 graph. The current5f Go/Rust caller composition remains a separate required gate, and no actual owner device or deployed host is covered.

The [original-Wasm fee decoder qualification](evidence/original-wasm-fee-decoder-a7ed-qualification-20261003.json) verifies71 bindings, all16 actual crate Git files,27 passing library roots and five recompiled omission controls. Metadata, payer, amount layout and extrinsic placement must agree before an event becomes an attributed candidate. Missing refund evidence stays unknown rather than becoming zero or an inferred debit; the omission control demonstrates the false1000-rao attribution. These synthetic fixtures do not establish actual-chain fee authority. Require the original runtime profile, supported host surface, complete parent witness, failed-refund branch coverage and finality before accepting native conservation evidence.

The [restore source-limit review](evidence/native-archive-restore-owner-capacity-source-gap-20261003.json) confirms that published Connect631 admits at most32 preparation owners and128 owner attributes. A per-head restore profile cannot represent every head of the accepted long-history archive/catalog namespace. The successor must export, restore and restart the entire accepted segment union plus active/chain heads, preserving signed capacity authority and the2x forecast margin. Qualifying only small history or silently reducing accepted history does not resolve this seam. Archive/catalog tests already running remain independent and must retain their successful work while the restore increment is implemented.


The [native history archive qualification](evidence/native-history-archive3bc-qualification-20261003.json) verifies27 artifact bindings and all20,382 physical tracked source blobs and executable modes at `3bc2f9a5`. Nine public archive roots pass normally and under race detection, with mainnet vet successful. Restoring the exact old dispatcher makes the public rollover test fail at the missing archive command, demonstrating actual command admission. Rollover, lost acknowledgments, missing/forged history, affected-role isolation and unchanged-payload work counts are covered. The signed catalog, full accepted namespace restore, claim/EVM adapters and final release composition remain separate required work.


### October 3 original-Wasm binary/control review and existing-cache qualification

The [observer binary/control review](evidence/original-wasm-observer-binary-causal-qualification-20261003.json) verifies45 artifact bindings, fifteen exact original crate files and the locked/offline historical-proof binary. Three actual recompiled omission controls fail at body identity, storage rollback and changed post-state assertions. Each control changes exactly one production source file. Preserve the two stale Cargo artifact passes as invalid harness results; they do not establish behavior. Binary construction and synthetic omission controls still do not admit an actual-chain runtime, transaction fee authority or finality.

The [existing-cache source selection](evidence/existing-validator-cache-selection-source-review-20261003.json) pins ten actual published-source blobs and41 distinct validator roots for current-graph qualification. Boundary, client-key authority and assignment verification already include real work counters, cancellation, changed-domain rejection and invalidation checks. Reuse those mechanisms and prove their behavior on the current release rather than adding parallel caches from an inventory alone. Durable receipt checkpoint tests cover misses, custody and scanner continuity; they do not prove full archive/predecessor work reuse. Add a separate actual full-lineage archive admission/use work-count probe, preserving complete predecessor and HeadEMA verification and final canonical checks. Source selection is not a passing test result or closure of PH-05/PH-17/PH-24.


The [native signed-catalog qualification](evidence/native-signed-catalog1b5-qualification-20261003.json) verifies40 artifact bindings and20,386 physical source blobs/modes in each of the current and old08f control trees. Twelve public catalog/archive roots pass normally and under race detection, with mainnet vet successful. The old dispatcher refuses the missing catalog command; the old importer passes signed growth but rejects the independently signed complete frame in both modes. Bound the full schema/signature envelope separately from the revision payload: a valid16KiB revision can produce a larger signed frame. Preserve explicit approver enrollment, signed monotonic revisions, original pending ranges and forecast checks. This qualifies catalog adoption, not full accepted namespace restore, cross-consumer archive adapters or production deployment.


### October 3 cache fixture corrected; real caller protocol verified; complete restore scope retained

The [original cache census](evidence/validator-cache41-original-failure-review-20261003.json) preserves40 passes and one permission-fixture failure in both normal and race modes. Required umask077 masks requested0644 creation to0600, so a negative permission fixture must explicitly set and verify the fault mode. The [Astra correction](evidence/validator-cache-permission0b58-qualification-20261003.json) changes only that test function, checks serialization errors and actual regular-file permissions, and passes all three same-file affected roots in both modes plus validator vet. Thirty-eight unchanged roots retain their exact original passing evidence. The [merge](evidence/validator-cache-permission-main-merge-20261003.json) preserves all production and module inputs. This is joined scoped qualification, not a claimed single41-root successful invocation or complete historical lineage work-count proof.

The [actual Go/Rust caller commands](evidence/current-fee-caller-e274-cross-engine-qualification-20261003.json) verify36 bindings, protected single-link executable custody, exact job hashes and typed outputs for paired, missing and explicit-zero refunds. Paired1000/250 yields candidate750; missing refund stays unknown; explicit zero yields candidate1000. Runtime admission stays false and top-level native expenditure null in all three synthetic fixtures. The [original selected caller gate](evidence/current-fee-caller-e274-original-gate-review-20261003.json) separately verifies all20,333 physical source blobs,16 normal passes and15 race passes with one deadline-fixture failure. A five-second prelaunch deadline cannot guarantee a child starts under race instrumentation. The reported whole-test duration includes fixture preparation and does not prove a missed production deadline. Astra's deterministic actual-output deadline control and preexpired-owner refusal are independently qualified before replacing that fixture; no original failure is relabeled.

The [cross-root archive source review](evidence/native-archive-cross-root-restore-scope-review-20261003.json) shows that current archive admission permits original and archived heads under different declared roots. Complete restore therefore requires a reviewed cohort, retaining every original signed reference, complete co-owner/capacity coverage and incremental recovery after partial restoration. The514-head same-root increment remains distinct; it cannot close this already accepted cross-root scope.

The [retained original observer binary](evidence/original-wasm-observer-binary-retention-20261003.json) preserves exact d022 bytes in an owned single-link artifact after the shared compiler output path was reused for a newer decoder build. Original receipts remain unchanged. Compiler target names are mutable build locations; copy byte-exact executables into stable retained evidence before target reuse, and never relabel a newer binary as an earlier source.


### October 3 deterministic deadlines and current release integration

The [deadline correction qualification](evidence/fee-caller-deadline4c96-qualification-20261003.json) verifies21 artifact bindings and20,333 physical Git blobs/modes. Four affected roots pass normally and under race detection, with mainnet vet successful. The test expires the owner deadline at actual partial output, verifies the inherited deadline and joins the real child. A preexpired owner refuses before opening an absent engine. This corrects a timing-dependent fixture without changing production timeouts; thirteen unchanged original race passes remain separate evidence, not a claimed single17-root invocation. Keep the original failure intact.

The [current release source join](evidence/current-caller-cb796-source-join-review-20261003.json) verifies27 artifact bindings,20,503 physical Git blobs/modes and23 changed paths over published3dc main. Twenty-two paths retain the qualified historical caller bytes; the remaining path adds only the public dispatcher to current main. Qualified556 production, current modules, cache-fixture correction and existing plans remain retained. Current-graph behavioral qualification is pending. A root-only storage inspector fixture must be reported as skipped in an ordinary user run and qualified separately with its explicit synthetic fixture authorization; a skip cannot establish deployed storage behavior.

For restore tests, isolate archive and preparation request namespaces. The original same-root fixture wrote both requests to the same metadata/request.json, overwriting the daemon preparation request before restore. Preserve the resulting original failures; qualify the test-only correction and the complete512-segment/514-head path independently. Do not weaken request admission to accommodate an overwritten fixture. Cross-root restore remains required because existing archive admission already permits heads on different declared roots. Hold all affected root leases, authenticate the complete cohort before any mutation, and retain each root's incremental journal and original signed references.


### October 3 EVM archival increment and large-restore race follow-up

The [EVM adapter source/compile review](evidence/evm-history8d-source-compile-review-20261003.json) verifies42 artifact bindings and all eleven changed physical Git blobs/modes at8d9904a5. Archive the exact acknowledged head before compacting; recover lost acknowledgments from exact original/next bytes, preserve credit/carry snapshots, cumulative event/fee counts, receipt-derived fee costs and original resource-review/catalog prefixes. Hold shared readers after admission and check custody without repeating payload reads. Signed capacity and2x physical forecasts remain independent of economic authority. Nineteen new public roots, eight neighbors and five omission controls await behavioral qualification; compile success is not a passing run or actual native-fee/finality authority.

The [original restore and scoped fixture review](evidence/native-restore-original-and-fixture259-review-20261003.json) verifies83 artifact bindings and the actual test-only delta. The original512-segment restore passes normally but its race run times out waiting60 seconds for the next sample. This is distinct from the request namespace collision, whose three affected roots now pass normally and under race detection with vet successful on test-only259. Preserve both original failure classes. Investigate actual worker progress and errors before attributing a sample observation timeout to permanent failure. Complete large-history race qualification and the already accepted cross-root cohort are still required; do not replace them with only small same-root tests.


### October 3 actual fee-proof acquisition and qualification headroom

Actual runtime fee authority requires a complete execution witness. An arbitrary-key state_getReadProof response does not establish that every execution read, write or prefix path was covered. The remaining collector must use the pinned SDK execution-proof mechanism against the canonical parent state, capture the exact child body/header and original runtime code/heap configuration, bound work and honor cancellation without committing state. Admit the job only after original-runtime replay reproduces the child root. Independently review the fee callsite/profile, payer mapping, failed/partial/zero refund branches and finality; incomplete evidence remains unknown. This is required implementation work, not a waived deployment condition or a confirmed RPC capability blocker.

The [guarded cache reclaim28](evidence/cache-reclaim-28-20261003.json) recovered5.41GiB from1,722 inactive unreferenced compiler archives, preserving source, evidence, modules, executable artifacts and active caches. Qualification admissions must recheck their own2x incremental forecast above the retained floor; a saved free-space snapshot does not authorize later work. Existing successful results and live processes remain retained.


The [large-history diagnostic source review](evidence/native-admission-ceae-source-review-20261003.json) verifies a two-test-file change over259 with all production/module tree entries retained. Observe positive reads of every exact retained inode under a300-second liveness budget, then require the unchanged public sample; access events alone do not prove admission. Preserve early worker errors, cancellation and original timeout evidence separately. Check observer cleanup errors and join its reader. Behavioral qualification and the cleanup successor remain separate from this source review.


### October 3 current replay/caller increment qualified and merged

The [current caller qualification](evidence/current-caller-cb796-joined-qualification-20261003.json) verifies59 artifact bindings. Normal and race ordinary-user gates each pass11 roots with one intentional root-only skip; the skipped storage inspector passes separately in both modes using protected pinned binaries and the observed filesystem UUID. Mainnet vet passes. The [merge review](evidence/current-caller-cb796-main-merge-20261003.json) verifies all23 actual changed files at778fca83 against the independently qualified candidate; every other code/module/config input matches that candidate while newer plans and evidence are retained.

Main now includes bounded subprocess collection and cancellation for replay, signing adapters, repair and storage inspection, plus the public original-Wasm historical verifier and typed fee-observation caller. Missing refund evidence remains unknown; synthetic candidates remain unapproved. Complete actual-runtime proof acquisition, source/profile/finality authority, archive adapters, full cross-root restore, deployment and observed settlement are still required. A published verifier alone cannot authorize production native fee accounting.


### October 3 reusable compiler-control guard review

The [Cargo guard source review](evidence/cargo-control-f0-source-review-20261003.json) verifies three workflow files at f0efe0f6 without a production change. Require the exact baseline and declared mutation, a fresh compiler artifact from that physical crate, an immutable retained executable and the intended single-test assertion. Preserve stale reused-binary passes as invalid harness evidence. Package-only cleanup and a shared target lease preserve warm dependencies and refuse active owners.

The same review identifies adjacent work before the guard is complete: join surviving descendants even when the leader exits normally or exits after TERM; check a 2x incremental compile, retention and log forecast above the disk floor; bound recipe and compiler-output reads before allocation. Add deterministic controls for these cases. Qualification of the existing six freshness tests and one real omission remains useful within its scope, but cannot establish those missing lifecycle and resource guarantees. Join only the qualified workflow delta onto current main; its historical host parent must not introduce unqualified production changes.


### October 3 compiler guard integrated and bounded host scope qualified

The [guard qualification](evidence/cargo-control-e68-qualification-20261003.json) verifies eleven harness roots and one actual freshly compiled omission, including the exact source assertion site and SDK error. The [isolated integration](evidence/cargo-control-e68-main-integration-20261003.json) places three byte-exact workflow files on main at62aaf598, retaining every other source entry. Bounded regular reads, streamed log limits, descendant cleanup and 2x incremental disk forecasts now accompany source/artifact freshness checks. Host production from the guard's parent is excluded from this integration.

The [bounded host qualification](evidence/runtime-hosts9cf-scoped-qualification-20261003.json) verifies39 Rust roots, including the twelve new roots, five Go roots normally and under race detection, five freshly compiled Rust omission groups with six intended assertion failures, two Go omissions in both modes and three actual Go-to-Rust typed reports. A dynamic error assertion may print the unexpected SDK error rather than its expected phrase: bind the exact source assertion site and actual payload instead of accepting a generic failing exit. Synthetic host support still does not admit actual runtime fee authority or finality.

Current-main host integration remains a separate gate. Four initial Go-run fixture failures rejected the temporary test image at protected-executable admission; the same seven roots pass from an owned single-link pinned image. Preserve the original result and qualify normal/race continuations from the immutable executable, with mode and ancestor custody recorded. Do not relax production executable admission to accommodate a compiler-generated fixture.


### October 3 current runtime-host integration completed

The [joined qualification](evidence/current-hosts-c59-joined-qualification-20261003.json) verifies seven selected roots in each normal and race mode, vet, and three exact Go-to-Rust executions. The [source join](evidence/current-hosts-c59-source-join-review-20261003.json) retains qualified Rust component results through nine byte-exact files. The [actual merge](evidence/current-hosts-c59-main-merge-20261003.json) records main0a9aeb34 and preserves the three compiler-guard workflow files.

Preserve the original temporary-image failures separately from the successful owned-image runs. The original failed executable-admission predicate was not captured; do not infer that its link count alone caused the rejection. Capture mode, links, ownership, ancestors and executable digest before running a protected test image. A corrected harness result does not change production admission policy.

This integration implements bounded historical host operations and separates job-read failures from evidence contradictions. It does not approve the actual mainnet runtime, establish a complete execution witness or authorize native fee accounting. Complete collector qualification, runtime/profile/finality review, economic conservation, cross-root recovery and actual host adoption remain required.

Operational headroom: the guarded reclaim29 plan found no eligible inactive archives older than12hours and removed nothing. Preserve this failed admission; select other disposable build data or derive a measured incremental forecast before admitting larger work. The full512 fixture contains encoded checkpoints, not an established512MiB payload; size forecasts must use measured or bounded actual data.


### October 3 EVM archival qualification completed

The [independent EVM archive review](evidence/evm-history8d-qualification-review-20261003.json) binds66 artifacts,27 normal roots from disjoint19+8 runs, the same27 under race detection, vet and five operative omissions in both modes. Controls require exact retained archive custody, fee-prefix accounting, archive-before-compaction publication, original legacy review provenance and retained observation census. A generic failing exit is insufficient: preserve the selected root and intended source assertion.

Qualified source8d9904a5 retains the original checkpoint before compaction and preserves credit, carry, payments and EVM cost history. Its current-main source join remains separate; this component result does not establish actual native fee authority, Claim/native conservation, deployment or observed settlement. Complete the Claim archive adapter and cross-domain economic rehearsal before accepting the continuous monitor as production-ready.

The [guarded cache reclaim30](evidence/cache-reclaim-30-20261003.json) recovered11.07GiB from1,139 inactive compiler archives in the superseded durable-volume cache. Exact-path reference scanning, active-process checks and physical identity checks passed; source, modules, evidence and ELF executables remain retained. Recheck each workload’s current2x incremental forecast before admission; this free-space snapshot is not a lasting capacity guarantee.


### October 3 retained Go qualification image safeguard

The [test-image review](evidence/go-test-image-173f-review-20261003.json) verifies six actual-file stdlib controls and three byte-exact workflow files integrated at6f6169c6. Capture the original image’s mode, links, ownership, ancestors and hash, then retain its exact pinned bytes in an owned mode0500 single-link image. Verify physical custody and hash before and after execution, retain bounded logs, join descendant processes and remove inherited GOFLAGS that would retarget fixture subprocesses.

This workflow addresses protected executable fixture admission without weakening production executable checks. Its receipt means pinned execution only: selected application roots, compiler/source/module provenance and actual outcomes require their own evidence. Do not rerun previously qualified application scopes merely to introduce this helper, or infer an original failed admission predicate that was not observed.


### October 3 archive continuation source review and observation causes

The [native/EVM integration source review](evidence/native-evm-current292-source-review-20261003.json) verifies22 changed physical files at29283743:21 match the independently qualified archive component and one joins the retained historical verifier dispatch. Current bounded-host and Cargo guard bytes are retained. The later Go workflow additions require their own exact preservation at final integration. Source review alone does not establish composed behavior.

An archive observation failure must retain its original cause before checking returned presence, digest or bytes. Read timeout or cancellation does not establish owner disappearance or contradictory evidence. Review native/EVM archive, catalog and Claim branches for combined error-and-comparison conditions; fix confirmed misclassification in a separate successor with deterministic interrupted-read controls. Preserve frozen component outcomes and durable original/next checkpoints while qualifying the affected continuation path.


### October 3 complete-proof collector cancellation finding

The [original collector result](evidence/proof-capture7b81-original-partial-review-20261003.json) retains seven passing Rust roots and one failing cancellation root at7b81da23. A cancellation after a real parent-trie read was translated by the SDK into `Invalid state root`; the caller returned that wrapper before checking its retained accessor cause. No complete collector qualification is claimed.

After each fallible backend code/heap, execution, write-root and strict-replay phase, inspect the retained accessor outcome before treating a wrapper as contradictory chain evidence. Preserve a concrete integrity error already observed before a later cancellation. Add deterministic before-read, after-read and adjacent phase controls in a distinct successor; retain original failed results and immutable input/output bounds. A failed observation must not become a false permanent rejection of authenticated progress.


### October 3 current native/EVM archive integration gate

The [current source review](evidence/native-evm-current0f-source-review-20261003.json) verifies39 artifacts and22 actual changed files at0f888694. The qualified archive components are joined with current bounded-host code and both executable qualification guards; all current module inputs are retained. The author’s full20,544-file inventory is bound separately from root’s changed-file verification.

Compilation and dependency preflight passed; the eight public archive/catalog, pending/admission and historical/capacity dispatch roots still require independent normal/race qualification and vet. Do not merge incomplete behavior evidence into launch acceptance. The distinct observation-error successor must preserve this frozen scope and its eventual results.


### October 3 cross-root restore core qualification

The [cross-root core review](evidence/native-cohort8fe-core16-review-20261003.json) verifies16 roots in each normal and race mode, with core/public vet successful, on frozen SN8fe01722/Connect1610f7a5. The checks cover exact per-mount and total declaration limits, refusal before the first mutation, healthy untouched owners, completed/pending peers, retained membership, sibling-root staged inode custody and aggregate capacity. Public10 and causal controls remain separate; no full public restore or actual host rehearsal is inferred.

A restore cohort must authenticate every original root and combined capacity before its first effect, then journal exact progress so interrupted or lost-ack continuation preserves completed peers. Keep root count, owner count, serialized declaration bytes, descriptors, retained segment bytes and disk/inode forecasts as distinct limits. Current accepted root ceilings remain64 per mount and256 total; explicit many-owner capacity is not permission to discard untouched roots or weaken identity checks.


### October 3 full512 recovery and collector Go scopes verified

The [full512 recovery review](evidence/native-ceae-full512-review-20261003.json) verifies the same full-history public restore root normally and under race detection, three adjacent negative roots per mode and vet. The primary test takes310.58seconds normally and731.17seconds under race detection. Positive reads of every retained inode precede the unchanged public sample assertion; the original d3d timeout remains failed historical evidence. Preserve the original progress and distinguish observation liveness from permanent worker failure. Cross-root public/causal qualification and actual host rehearsal still remain.

The [collector Go scope](evidence/proof-capture7b81-go10-review-20261003.json) verifies ten roots in each normal/race mode and both owned, pinned, single-link executable images. This preserves public request/job identity, bounded cancellation and missing-versus-zero observations. It does not erase the original Rust7PASS/1FAIL cancellation defect or qualify the complete collector, exporter, actual runtime profile/finality or native fee authority. Carry unchanged Go results into the distinct Rust fix only through exact source/module joins.


### October 3 source-custody admission during cross-root restore

The [original public cohort result](evidence/native-cohort8fe-public9-original-review-20261003.json) retains eight passing roots and one genuine late-source-custody failure. After valid planning, removal of an inventoried copied source-archive lock was missed by public apply because only its staged counterpart was admitted. The failing test stops before its subsequent no-mutation assertions; those assertions are not claimed as verified.

Check the required original source archive and owner attributes before the first effect, alongside staged inventory admission, for both single-root and cohort restore. Preserve completed target journals and permit exact interrupted resume; do not restart completed peers. Distinguish source custody from intentionally regenerated target inode identity. Qualify missing/replaced late members, pending/lost-ack continuation and unaffected peers in a distinct successor while preserving original failed evidence.


### October 3 collector cancellation successor source review

The [collector fix review](evidence/proof-capture550-source-review-20261003.json) verifies exactly two Rust files over7b81 at5501f9ac; Go and Cargo inputs are unchanged. Inspect retained accessor failure before SDK translation, preserve a returned node contradiction ahead of cancellation, and check completed code/heap/execution/root/replay/report boundaries with instance-local deterministic controls. Prior Go10 results retain their actual execution context and need no repeat without a concrete changed dependency.

Independent Rust positives, compiled omission controls, export and actual Go-to-Rust capture are separate gates. No complete runtime authority or finality admission follows from this source review, even when synthetic parent replay succeeds.


### October 3 qualified native/EVM archive continuation integrated

The [joined-source qualification](evidence/native-evm-current0f-qualification-20261003.json) verifies eight public roots normally and under race detection, vet and current dependency inputs. The initial four-pass/four-failure runner attempt used module-root cwd and could not find package-relative metadata fixtures; the exact unchanged source and owned binary passed from the correct package cwd. Preserve both outcomes and record execution context explicitly.

The [actual merge](evidence/native-evm-current0f-main-merge-20261003.json) integrates22 exact candidate files at1a7a123d; every non-documentation Git entry matches the qualified candidate and newer plans/evidence survive. Main now includes native/EVM immutable archive rollover, reviewed signed capacity catalogs and retained checkpoint continuation. Observation-error refinements, Claim archive lineage, source-custody restore, actual runtime fee/finality authority, cross-domain conservation and production rehearsal remain distinct open work.


### October 3 actual runtime472 structural review

The [retained runtime structure review](evidence/runtime472-structure-review-20261003.json) independently verifies compressed code and metadata against the saved snapshot, expanded Wasm bytes and five original function-body hashes. There are51 imports total, including50 functions and one memory; function-body indices must count only function imports. Observed fee-handler indices3053/3058 differ from runtime470’s3052/3057. Names and indices are source-review leads, not admitted economic semantics.

Bind a callsite profile to the exact original parent code and metadata; never carry an old runtime’s indices forward merely because function names resemble one another. Source/build correspondence, original branch/return evidence, complete execution witness and independent finality remain required. A missing refund event remains unknown; the failed-deposit branch cannot be inferred to return zero merely from absent events. Reuse existing receipt/finality verifiers and retained code snapshots while keeping their authority scopes explicit.

The owned archive’s configuration supplies a location lead, not live custody: Snow’s staged mainnet service uses `/data/subtensor-mainnet-archive-v1` and loopback RPC19945. Verify actual startup, sync and backend/export access before borrowing a parent trie. Ordinary state read proofs and a runtime-code snapshot do not constitute a complete parent execution witness.


### October 3 complete execution-proof collector component qualified

The [collector qualification review](evidence/proof-capture550-qualification-20261003.json) verifies192 artifact bindings: twelve Rust roots, four freshly compiled omission controls with one intended assertion failure each, and actual owned Go-to-Rust capture over complete synthetic parent nodes for pair, missing and zero-refund cases. Exported reports match exactly as typed JSON. Missing refund remains null, explicit zero remains0, and all cases retain unapproved runtime/native-fee authority. Original7b81 failure remains historical evidence; unchanged Go10 normal/race scope is retained through the exact source/module join.

Use immutable executable images and the qualified compiler freshness guard before target reuse. A Cargo `fresh:false` artifact means a new compile, not stale reuse. Bind the actual selected failing root and expected assertion rather than accepting a generic exit101. Public RPC request971/current-main composition and actual archive-parent, runtime/profile/source and independently admitted finality remain separate required gates.


### October 3 archive observation failures separated from conflicts

The [independent qualification](evidence/native-evm-read-causes9d-qualification-20261003.json) verifies seven deterministic roots normally and with race detection, vet, and two operative old-branch overlays. The overlays produce six and four intended behavioral failures per mode while unaffected controls pass. All seven changed paths are included in the [actual main merge](evidence/native-evm-read-causes9d-main-merge-20261003.json); every non-documentation Git entry matches the qualified candidate.

Preserve read failures as their original wrapped causes before considering absence, hash mismatch, changed bytes or reconstructed-plan differences. A read that returns no observation cannot establish an integrity contradiction. Exact same-plan continuation retains published archive/catalog work, while confirmed replacement remains rejected. This correction covers native and EVM archive/catalog paths and public reviewed-input reads; controller retry policy and final all-role composition remain separate work.

A read-only SSH attempt to Snow was rejected with public-key authentication before any remote command ran. That establishes missing access from this session, not an absent archive. Obtain a consistent separately owned parent-trie export or an in-node read-only backend route; do not open the running ParityDB with its exclusive-lock read-only API. Actual runtime/source correspondence, complete parent witness and independently admitted finality remain required before native fee accounting is approved.


### October 3 complete-history restore and evidence retention

The [original cohort terminal review](evidence/native-cohort8fe-original-terminal-review-20261003.json) verifies the exact 512-segment/two-head public restore root normally and with race detection. The separate nine-root batch still fails its late-source-custody root in both modes; the source-custody successor must pass before broad restore qualification. An initial misspelled race selector ran zero tests despite exit0. Require the declared root census, not process success alone, and retain the invalid run alongside its exact-selector correction.

The [compiler-image retention supplement](evidence/duplicate-compiler-image-retention-20261003.json) records524,730,368 allocated bytes reclaimed from six inactive compiler outputs after exact hash, physical identity and privileged process checks. Each exact executable remains at its original protected owned path; all source, logs and original receipt bytes remain. Bound duplicate paths can be retired only with an explicit byte-preserving mapping. Failed or insufficient cache-reclaim plans do not justify lowering the resource floor or deleting unique evidence.

The same retention supplement records a further74,248,192 bytes from the completed historical-request normal compiler duplicate, for598,978,560 total allocated bytes reclaimed. Future internally owned compilation can use a unique private output, then verify its identity/hash and set mode0500 before execution, rather than retaining a second identical ELF. Never reuse that output path for a subsequent compile. External or untrusted executable inputs still require the existing pinned-copy custody checks. Race and causal request qualification remain required; this operational change does not count them as passed.

Four further inactive0f/7b81 compiler duplicates were retired after the same independent hash, physical-identity and privileged process audit, reclaiming348,274,688 allocated bytes. All four qualified owned images and both original qualification documents remain unchanged. Total duplicate-only reclaim is947,253,248 bytes across eleven images; no source, active cache or unique evidence was deleted.


### October 3 workload-specific admission and historical request qualification

The [request qualification](evidence/capture-request971-final-review-20261003.json) verifies ten positive roots normally and under race detection, vet, and six intended behavioral failures from three operative controls in both modes. The actual failure sites113/209/219 establish the omitted parent-runtime, complete-body and closing-canonical checks; descriptive labels are not substituted for observed assertions. The [current-source union](evidence/capture-currentad0-source-review-20261003.json) preserves eighteen exact collector/request files and every other current-main entry. Its eight-root composed qualification remains required before merge.

The resource audit traced110GiB to an earlier serial OCI release preparation reserve, subsequently reused for unrelated Go tests without a new workload derivation. Preserve original policies and receipts, but size each admitted class from explicit production/emergency reserves, outstanding concurrent reservations and twice its incremental forecast. Separate already allocated retained inputs, cumulative growth and transient peaks. Warm Go, cold Go, Cargo, OCI, databases and full-history restores require distinct profiles and one host admission ledger.

A finite reviewed four-GiB grant admitted only the three staged request race controls, serially, from one pinned baseline. All attempts joined; timestamped0.2-second byte/inode samples observed749,080,576 bytes of cumulative consumption, below the cap. Sampling demonstrates an observed low-water, not an instantaneous maximum. Neither cleanup nor new free space recharges that grant, and it does not authorize other workloads. Crossing an old admission floor during an admitted finite test blocks inappropriate new work rather than canceling completed or active work. Other scopes retain their policies until separately reviewed.


### October 3 finite integration-test admission

The [mixed qualification admission review](evidence/mixed-qualification-resource-adoption-20261003.json) binds52 input artifacts, the independent V2 review and root adoption. The finite24GiB grant covers only34 enumerated serial archive, graph, compile, selected-test and vet phases: current collector integration, copied-source custody restore, a synthetic fee-context exporter and EVM restore compilation. Conservative retained compiler/cache growth is512MiB per compile; twice the aggregate forecast is21.5GiB, rounded24GiB. The original16GiB proposal remains historical evidence.

Keep one fixed baseline, one host lease and persistent byte/inode samples. A completed phase or cleanup does not refill attempts or budget. Full512 normal execution must measure its declared workset before race admission. Stop new admissions on a forecast overrun and retain/join admitted children; crossing an unrelated old floor alone must not cancel them. This grant excludes Rust, databases, full-model runs, live recovery and deployment, and does not change production reserves or global policy. Adoption establishes test resources, not passing behavior or final mainnet readiness.


### October 3 Claim archive source and foreground-work lesson

The [Claim archive source review](evidence/claim-archivee594-source-review-20261003.json) independently verifies26 bindings, nine exact changed Git blobs and unchanged module inputs at e5947c6f. Eleven new tests and seven neighbors are declared, with five operative controls; no compilation or behavior result is claimed. The candidate retains original checkpoint evidence, unresolved obligations and independently renewed policy history under signed archive capacity. Whole-Claim restore and continued renewal beyond finite epoch/review bounds remain required.

A source review found that `externalizeMonitorClaimRecord` compares every current epoch with every archived epoch during each save. Avoid work proportional to the product of those censuses: use an admitted commitment lookup while preserving ordered census checks and exact original evidence hashes. Keep the frozen candidate and qualify a separate successor with deterministic growing-census work counts. Measure hydration, compaction, state validation and replay/upload contention too; avoiding payload rereads alone does not establish bounded foreground work. This is a confirmed source finding, not a measured production stall.


### October 3 synthetic fee-context inputs verified

The [synthetic exporter review](evidence/synthetic-fee-exporter-review-20261003.json) independently verifies24 artifact bindings, the exact one-root pass, three successful graph/compile/test phases and two complete0600 fixtures totaling171,137bytes. Server5f uses its actual selected dependency graph and declared SN866 peer; unused peer packages gain no qualification. The immutable ledger snapshot matches the original output manifest. The public fee-context verifier can now qualify against these exact inputs; actual runtime/source, complete parent execution evidence and finality admission remain required.

The exporter omitted paired before/after executable custody during its test. Compiler post-mode identity, test input hash and later physical readback match, but they do not prove a stronger execution-boundary claim. Preserve that evidence limit, reuse the generated inputs through the verifier's own checks and record complete paired custody for subsequent tests. Do not repeat useful work merely to improve receipt wording or silently rewrite original evidence.


### October 3 current collector integration and scoped graph recovery

The [current collector qualification](evidence/capture-currentad0-qualification-20261003.json) verifies56 bindings, eight passing roots in each normal/race mode, vet and matched paired executable custody. The [actual main merge](evidence/capture-currentad0-main-merge-20261003.json) integrates all eighteen exact qualified paths at9576811d; every non-plan/evidence Git entry matches the candidate. The earlier Rust550 and Go971 component results retain their exact scopes. Actual parent export, runtime/source/callsite admission, independent checkpoint/finality and native fee authority remain open.

The [current fee-context source join](evidence/fee-context-current1b3-source-review-20261003.json) verifies27 bindings and seven exact feature paths over the current collector composition, retaining module inputs and all other source entries. Qualify this combined candidate once rather than first testing an older graph and rebuilding merely to compose it. Its ten-root/four-control gate is pending; synthetic export does not approve actual fees.

The [restore graph preflight review](evidence/selected-graph63-retry-review-20261003.json) retains an ordinary runner failure: an eager all-module query followed an unused replacement and failed before selected dependency inspection. Scope graph admission to imported production/test packages and record their resolved modules and physical inputs. Keep the failed attempt charged and use one explicitly bounded metadata retry after the corrected reader; preserve all completed tests and the original fixed resource baseline. An unused metadata failure must not trigger a full restart or invalidate authenticated components.


### October 3 full-scope economic and repair implementation gaps

The [full hardening source review](evidence/full-hardening-source-gap-review-20261003.json) independently verifies31 immutable Git bindings and29 current product readbacks. Native denominator evidence still always leaves allocation and quantization dust unknown, and the native summary does not derive provider entitlement or owner recycling. This is missing consumer code as well as missing real-chain authority. Implement the bounded runtime-specific execution-witness arithmetic and UID/hotkey generation consumer, then compose native, vault and independent Claim conservation. Cover intra-block accrual/drain, zero-incentive fallback, registration takeover, parent-runtime changes, omitted evidence and rounding; incomplete or unapproved evidence remains unknown.

Validator repair stores and manager rereads also conflate some unavailable observations with changed state. Return the original I/O/cancellation cause before comparing returned values, preserving confirmed contradictions and completed repairs. Add deterministic no-new-effect and healthy-peer continuation controls. Existing Solidity reserve, credit, carry and exact-payment guards remain reusable; no new contract defect is established by this source review. Runtime/source/finality approval, installed contracts, funded hosts and live role activation remain separate requirements.

The corrected selected-import graph retry passed with132 core packages/one module and848 SN packages/131 modules;3,895,108 raw metadata bytes and all source controls were verified. The original broad query failure remains charged. A narrow preflight repair preserves completed work and does not authorize source or resource-policy drift.


### October 3 retained model preparation and sampler continuation

The [continuation adoption](evidence/model-continuation-adoption-20261003.json) preserves the completed source archive and the dependency preflight failure caused by an uncreated private temporary directory. Provision that directory and retry only the affected graph inspection; do not restart source capture or discard the failed attempt. One corrected graph retry and one full current Server5f model invocation are admitted at the same resource baseline. Admission is not a passing model result.

The distinct sampler removes the undefined cache reference and records unavailable observations, final CSV readback and an explicit terminal outcome. Four independent lightweight controls distinguish complete telemetry, memory/container observation gaps and existing-output refusal. Keep test execution and telemetry qualification separate: a sampler failure must not terminate admitted work or fabricate zero resource usage. Current emissions, Claim rollover/restore, composed economic conservation and actual mainnet authority remain required.


### Native allocation conservation and launch split conformance

The [native accounting source finding](evidence/native-split-conformance-source-finding-20261003.json) distinguishes two requirements: authenticating observed emissions and enforcing the approved 10% provider / 90% owner recycle policy. Conserving the total alone cannot establish that split. Expose actual deviations and unknown authority explicitly; compare cumulative amounts against exact references with independently reviewed rounding, collateral capture and Claim reconciliation. Test a conserving wrong split and partitioned continuation so a new observation page cannot reset rounding carry or make incorrect economics appear ready. This finding concerns unsealed implementation and remains unqualified.


### Requested and granted transfer capacity

The [original full-model subsidy failure](evidence/server-model-subsidy-original-failure-20261003.json) remains retained while the suite continues. Initial source triage found a consumption loop counting requested capacity although successful escrow can now grant fewer bytes. Confirm the cause before changing assertions. Consumers and fixtures must use the returned, signed grant for capacity and completed-byte accounting; test partial grants and repeated exhaustion with exact revenue conservation. Check adjacent loops and production callers, preserving legitimate balance/debt retention. No post-fix result is established yet.


The [source review of the subsidy correction](evidence/subsidy-grant804-source-review-20261003.json) verifies the exact test-only successor: returned grant consumption, checked settlement errors, original exhaustion/payment assertions and unchanged production/modules. Qualify the affected root and two declared neighbors after the ongoing full suite joins; retain unaffected results through exact source scope rather than restarting the entire suite. The original failure remains evidence, and this source review supplies no behavioral pass.


### Role isolation must include startup admission

The [startup source finding](evidence/monitor-startup-isolation-source-finding-20261003.json) shows validator/operator/provider/Claim owner-admission failures can stop the monitor before healthy workers start. Runtime isolation alone does not cover this path. Separate invalid shared configuration and network/declaration admission from role-local observation or custody errors. Start healthy domains, retry recoverable local admission and quarantine confirmed local integrity failures, preserving exclusive ownership and checkpoints. Exercise actual multi-role startup, cancellation and duplicate-writer controls before closing this requirement.


### Preserve debit lifecycle and meaningful writer guards in tests

The [current full-model failure batch](evidence/server-model-clock-and-debit-original-failures-20261003.json) retains two writer-guard failures and six participant payout failures. Source triage separates a new early refusal branch from the later checked endpoint lock, and terminal settlement from asynchronous journal application. Review actual control flow and keep missing/late-lock mutants effective. For asynchronous debit, assert retained pending consumption and unavailable spendable credit, then exercise the real replay-safe flusher before checking raw balance and released reservations. Do not sleep, ignore close/read errors or weaken payout conservation to make fixtures pass. Continue the full run and qualify one combined affected-scope correction afterward.


### Preserve current wire changes in recovery dependency composition

The [Connect integration preview](evidence/current-connect772-integration-preview-20261003.json) retains all fifteen qualified recovery files alongside newer upstream optional close-report identity fields. The [composed source review](evidence/current-connect-composed-source-review-20261003.json) now binds commit `2ea8d82ea5cbc8bdebdee05f5b90a9f21b64128f`: all preview paths remain exact except the two namespace replacements from `e7f24717`. This is a Git-only composition, not a published dependency or test result. Qualify the final common dependency graph, including namespace controls and optional identity wire compatibility, once; then publish and pin it in SN. Optional report identity must remain non-emitting until the backend provides durable deduplication of the exact party, amount and checkpoint; a compatible wire field alone cannot authorize that behavior.


### Continuous native evidence requires an admitted producer

The [native admission gap](evidence/native-continuous-admission-source-gap-20261003.json) separates a signed per-block evidence consumer from an operational producer. Reuse the reviewed runtime/layout/engine authority, but automatically collect and independently verify fresh finalized boundaries, jobs and provider generations under explicit bounded producer authority. Owners must not manually sign every block, and an artifact signed by its own unverified collector is not independent proof. Qualify the actual producer→Rust→public consumer path, restart continuity and incomplete/canonical-change controls. Actual network and runtime approval remain required.

The [combined fixture source review](evidence/combined-model4754-source-review-20261003.json) supersedes the804-only testing plan with ten affected roots and six neighbors. Pending debt, public debit/replay and unchanged provider/account allocations are asserted before the original exhaustion checks. Source review establishes no behavioral pass; the unchanged full suite continues before correction tests are admitted. The [correction proposal review](evidence/model4754-proposal-source-review-20261003.json) verifies all four overlay files, sixteen selected test declarations and the resource arithmetic. The newly added helper file must be discovered by the actual Go overlay graph and compiled before the affected tests run. Retain original configuration-dependent skips; qualify referral and onboarding behavior with explicitly pinned configuration in a separate follow-up instead of treating skipped functional coverage as passed.


The [frozen native source review](evidence/native-ad1-source-review-20261003.json) verifies all sixteen changed files and twenty-four intake bindings at `ad1e5b19`. Qualification must exercise the actual Rust-exported original-Wasm job through the owned Go replay process and accounting consumer; its environment-dependent skip is not a pass. Normal/race, Rust controls and the continuous independent producer remain outstanding. Missing admission input must preserve the cursor and unknown amounts while healthy domains continue; confirmed identity conflicts retain their integrity handling.


The [closed-observation successor review](evidence/repair-closed6d-source-review-20261003.json) verifies `6d0612b8`, four changed files and eleven intake bindings. A closed handle or `EBADF` now remains unavailable evidence at stopped-generation and active pre/post-join checks; it cannot establish generation change or replenish the original join window. Positive integrity loss and uncertain durable publication keep precedence. Four new deterministic public controls join the existing twenty-root repair scope; qualification and current-role composition are pending.


The [common hardening source preview](evidence/hardening-composition-source-preview-20261003.json) joins Claim/fee/restore, native execution, repair closed-observation and lineage controls at `b704fcde`, retaining the exact Core `2ea8d82e` pin. Integration exposed overlapping replay-fixture dispatch changes; Astra resolved them at `5029c6c2` by retaining the fee marker and both native/other-profile paths. Qualify all retained scopes on this common graph instead of rebuilding separate precursors. This is a Git-only preview: final intake/census, compilation, normal/race and actual cross-engine qualification remain pending; no publication or activation is implied.


The [StatsProvider original failure](evidence/server-stats-original-failure-20261003.json) is the tenth current full-model failure and the seventh shared-balance assertion affected by asynchronous journal application. Its complete excerpt remains separate from the earlier six-test bundle. It was already included in the combined4754 ten-affected/six-neighbor scope; retain that batch and require actual public debit flushing/replay before raw-balance assertions. No correction pass is established yet.


The [final Claim source review](evidence/claim-final-source-review-20261003.json) verifies fifty-two changed Git bindings, fifty-one physical intake bindings and sixty-five exact test declarations (forty-four primary plus twenty-one compatibility roots). The common source preserves all reviewed Claim files except the explicitly resolved replay-test fixture. Keep the fourteen operative control groups and the native/repair/lineage scopes in the combined qualification. This source remains uncompiled and unqualified; official dependency packaging, behavioral execution, actual upload contention and production authority are still required.


The [startup/foreground source review](evidence/startup-foreground-source-review-20261003.json) binds independently reviewed six-role startup isolation and four actual replay/upload/foreground controls on the common preview. Startup emits bounded pending/quarantined/re-admitted events even when metrics cannot open, without claiming fresh checkpoint or metrics evidence. Cleanup errors must remain typed through nested archive load/open failure paths; a clean outer close cannot turn an unjoined nested owner into retry permission. That adjacent successor and all behavioral qualification remain pending. Performance observations from synthetic signed histories prove only their fixture scope, not production host or fleet sizing.


The [five additional original financial failures](evidence/server-additional-financial-original-failures-20261003.json) extend the live model failure set beyond the ten-root4754 correction. Retention must keep unapplied debt and suppress premature tombstones. Force-close tests must retain terminal/escrow/provider assertions while distinguishing committed journal consumption from its later raw-balance application. Exhaustion fixtures must report the returned grant, check close and forced-close errors, and prove public available credit before flushing; a malformed over-grant close cannot be counted as paid usage. Keep the frozen4754 source and original failures separate from the expanded correction and its future qualification.

The [current migration-plan skip](evidence/server-migration-plan-original-skip-20261003.json) confirms that PostgreSQL `auto_explain` availability or LOAD permission prevented the actual nested-plan test from running. The shared skip message does not identify which prerequisite failed. After the current full suite terminates and its fixture is cleaned up, rerun this exact test with the extension and permission provided in an owned disposable fixture. Require actual custom and generic INSERT/UPDATE/DELETE plans across the migration on an existing connection; retain the original skip and completed model results.

The [nested cleanup source review](evidence/nested-cleanup-source-review-20261003.json) retains cleanup uncertainty through native, EVM and Claim archive admission and all seven role recovery callbacks. Parent cancellation must not erase a real close failure or authorize another owner. Ordinary read failures remain retryable after the exact reader closes. Seven new deterministic roots and nine neighbors cover actual descriptor release, lost-ack Claim recovery, healthy peer continuity and cancellation precedence; qualification remains pending on the common source.

The [common qualification scope review](evidence/common-qualification-scope-review-20261003.json) binds the combined cleanup successor and Core dependency to 205 distinct Go test declarations, with complete matching normal and race selectors. The proposed 32 GiB data allowance covers a 2× margin on the 14 GiB planning forecast; it is not a measured production bound. Retain separate Rust/export and operative-control qualification, original model cleanup and fresh resource admission before execution. Monitoring completion also requires the continuous independently verified native producer and actual launch authority.

The [additional request-lease and retention failures](evidence/server-lease-retention-original-failures-20261003.json) require explicit separation of terminal settlement, pending debit, applied balance and token retirement. Preserve the original request expiry and safe retention guards. Qualify pre-flush accounting and available credit, public flush and repeated replay before declaring these fixture assumptions corrected; the original failures remain retained.

The [producer and packaging prerequisite review](evidence/producer-packaging-source-prerequisites-20261003.json) preserves two continuation invariants. A native producer must pair advanced GRANDPA authority state with the actual certified header, separately from earlier parent/child storage-proof blocks; qualify delayed handoff and descendant restart together. Final dependency packaging must retain the exact current and historical floor commits used by its official module verifier, with owned Git objects and measured source bytes. Neither source review establishes runtime approval or completed qualification.

The [current Server full-model terminal result](evidence/server-full-model-terminal-20261003.json) retains 1,507 passes, seventeen failures and eleven skips. The suite completed before its timeout with no unfinished roots; its owned database, Redis and network were removed successfully. Retain these results and qualify the expanded correction plus exact skipped-input follow-ups. The four extra names in the declaration forecast are non-test `Testing_` helpers; all 1,535 actual test roots have terminal outcomes. Future source forecasts must recognize test naming and signatures, rather than treating every `Test` prefix as a runnable test.

The [expanded seventeen-failure correction review](evidence/server-retention-c2b-source-review-20261003.json) binds nine test-only changes, 45 model roots and one actual debit-worker continuation root. It preserves pre-flush available credit, original journal identity across metadata removal, returned-grant accounting, provider allocation and exact post-acknowledgment token release. Three operative omission controls must fail for the intended behavioral reasons. Qualification remains pending; the completed original model suite remains retained.

The [four Claim control rebase review](evidence/claim-b6-control-source-review-20261003.json) preserves the current typed cleanup fix while carrying only the intended omission effects. Test-control overlays must match the composed production source; replacing an entire older file can silently remove an adjacent fix. Qualification coordination must also use one canonical shared host lock, rather than separate per-job locks that permit simultaneous holders. Retain completed sources and evidence when correcting either preparation issue.

The [combined physical source stage](evidence/common-source-stage-readback-20261003.json) passes for the exact SN and Core commits with seven bound peers and the historical module floor. Preserve its receipt and charged disk usage while later packaging, graph and test phases continue under the same baseline. Successful source staging establishes provenance, not behavioral or launch acceptance.

The [official dependency package readback](evidence/common-module-package-readback-20261003.json) passes the exact Core ZIP and module hashes independently. Preserve the source stage, package and original fixed resource baseline across later qualification. This proves reproducible dependency packaging; it does not prove combined behavior or authorize deployment. Selected graph checks now proceed separately, retaining either outcome without discarding the other.

The [selected dependency graph readback](evidence/common-selected-graphs-readback-20261003.json) passes all six selected SN/Core package imports against the exact private dependency. Additional Git Origin metadata in the module cache is recorded separately from exact ZIP/MOD bytes and matching version/time; provenance checks must compare the relevant semantics rather than reject this expected cache enrichment. Compilation and behavioral qualification remain pending.

Unattended native accounting also needs an automatic historical trie source. A supplied content-addressed directory alone does not meet continuous-operation requirements. Implement a bounded archive read-through or an equivalent production feed: request proofs at the certified parent block, retain request identity and the original retry budget, and let the owned original runtime verify the accessed nodes and resulting state root. RPC-returned values are not accounting authority. Missing nodes must leave the cursor and amounts pending while healthy domains continue. Unsupported child-trie or range operations must remain explicitly unavailable until proof completeness is implemented. Qualify actual missing-node replenishment, restart and cumulative resource limits; this work remains pending.

The [populated native capacity source review](evidence/native-populated-capacity-source-review-20261003.json) replaces empty-trie sizing assumptions with four families of 4,096 present provider records, both pinned SDK layouts, and a large separately hashed value. Qualification must measure actual raw nodes and decoded/serialized bytes with a 2× margin, reject real missing-node proofs, and exercise the native admission path. This synthetic fixture is unexecuted and grants no actual runtime or provider authority; it does not bound the complete VM state footprint. Apply capacity profiles across the complete path: native storage verification, Rust historical capture, retained/combined proof limits and Go replay decoding. Enlarging only the storage verifier leaves an earlier VM proof cap unchanged. Preserve the independently bounded receipt profile; qualify native execution with the enlarged profile and actual populated proofs.

The [proof-feed operation review](evidence/native-proof-feed-operation-review-20261003.json) binds the pinned SDK surface beyond point reads: child storage, iterator progression and root computation also need authenticated trie access. Key-discovery RPC pagination is insufficient to prove complete ranges or end-of-range. Test unavailable sibling nodes during state-root updates, wrong-parent proofs, interrupted replenishment and restart through the actual execution path. This design review does not establish implemented or qualified read-through behavior.

The pinned SDK root calculation can log an error and return an old or default root; its ephemeral lookup can turn a storage error into missing data. The proof-feed owner must retain an independent error latch and check it before accepting execution output or advancing the accounting cursor. Qualify transient storage failures during parent and child root updates, including unchanged-root cases. Logging alone cannot establish successful execution. This newly identified gap is assigned to the native producer implementation and remains unqualified.

The [six ordinary package compiles](evidence/common-six-normal-compiles-readback-20261003.json) pass on the frozen combined source. Behavioral tests may use those exact retained images while independent race compilation continues under a combined resource forecast and the original baseline. Do not impose an all-compilers barrier on independent completed packages. Compilation alone does not close behavior, capacity or deployment requirements.

The [producer integration findings](evidence/native-producer-integration-findings-20261003.json) require separately approved capture and replay executable identities: the real Rust programs accept different fixed protocols, although a synthetic peer can hide that mismatch. Qualify actual capture, retained-job restart and replay with the real binaries, including wrong-mode and changed-image controls. Independently approved monotonic authority/capacity renewal must retain the original cursor, completions and predecessor approvals; a new runtime or larger allowance must not force a fresh accounting history. Both fixes remain pending in the producer lane.

The [complete twelve-image compilation readback](evidence/common-twelve-compiles-readback-20261003.json) passes all six packages in ordinary and race modes on the combined frozen source. Retain these exact images for selected behavioral checks. Compilation does not qualify runtime assertions, real cross-engine execution, omission controls or the newer producer implementation.

The [original combined normal behavioral result](evidence/common-normal-ordinary-original-failures-20261003.json) retains 137 passes and 66 failures across 203 completed roots. Fifty-nine failures are package-relative fixture errors from the direct runner's module-root working directory; seven require separate behavioral analysis. Direct compiled tests must use the package directory, and preflight must validate fixture resolution before costly execution. Do not count a harness correction as a production fix, dismiss the separate failures, or repeat successful source/module/compile preparation.

The [checkpoint readback finding](evidence/durable-checkpoint-readback-identity-source-finding-20261003.json) identifies a production failure class: a canceled or failed read was compared with expected bytes and labeled an identity change. Failed observations must retain their cause and pending reconciliation; only successfully returned differing bytes establish a mismatch. Add actual publication-boundary cancellation and true-mismatch controls, then inspect adjacent preparation and inventory readers for the same conflation. This source finding remains unqualified and does not establish the cause of every original test failure.

The [corrected normal assertion scope](evidence/common-normal-corrected-assertions-20261003.json) retains 190 passes and thirteen failures, with all 203 selected roots terminal and no skips. Independent role quarantine requires waiting for the affected worker and proving peer continuity, rather than expecting the whole process to exit. Restore fixtures must preserve the untouched metrics root's prepared reserve, and foreground fixtures must advertise headers within the actual public metadata allowance. Correct these assumptions while retaining true authority, changed-byte and over-limit refusal controls; the production cancellation/readback defects require their own deterministic tests.

The [full512 normal qualification](evidence/full512-normal-retained-qualification-20261003.json) passes the actual public restore of all 512 original retained EVM segments. Its 331 sampler rows record a 132,313,088-byte allocated peak, thirteen ordinary child-path removals and complete final cleanup. This is sampled synthetic-fixture evidence, not a production sizing bound. Preserve body, measurement and executable custody separately from a post-body summary-path collision: regenerate the checker summary under a unique job namespace without rerunning successful work. Race and final composition remain pending.

The [frozen correction source review](evidence/b6-correction-source-review-20261003.json) binds fourteen changed files and 52 selected roots, with seven operative controls. Production corrections preserve checkpoint uncertainty and plan-file read causes; fixture corrections retain independent role continuity, original reserve authority and public header bounds. This batch is uncompiled and unexecuted. Qualification must retain the original failures and reuse unchanged source/module preparation; separate Core read-cause and continuous native-producer work remains open.

The [mixed cancellation finding](evidence/mixed-cancellation-source-finding-20261003.json) extends the error-classification lesson: a joined parent cancellation and owned deadline expiration is still pure cancellation. Require the actual owner cause, inspect every joined leaf, and preserve independent I/O, identity and cleanup failures. Add deterministic both-order controls before qualifying the correction successor; do not rerun or alter currently active qualification jobs.

The [mixed cancellation successor review](evidence/mixed-cancellation-successor-source-review-20261003.json) verifies the three-file child of the frozen correction batch. Both observation and checkpoint classifiers now accept joined cancellation/deadline leaves while requiring the actual owner cause and retaining hard-error dominance. The 52-root union and eight operative controls still need normal/race qualification; qualify this final child once rather than compiling the precursor again.

The [pending-file sync finding](evidence/native-pending-file-sync-source-finding-20261003.json) separates visible byte equality from durable publication. If the original file sync failed, retry must sync the exact retained pending file before rename; syncing only the directory does not repair file-data durability. Cover complete-write/file-sync failure, partial pending bytes and independent directory-sync failure with deterministic controls, and inspect adjacent node/evidence publishers. This moving-source finding is not yet qualified.

The [finite correction resource review](evidence/0e5-finite-resource-review-20261003.json) retains one fixed consumption baseline while allowing a reviewed 48-GiB aggregate scratch budget. It accounts for existing live reservations and a 2x margin for the 33-phase correction scope. An old accounting cap should not create an artificial serial bottleneck when actual host capacity safely admits overlap; exact executable jobs, current resource checks and owned process cleanup remain required. This review authorizes resource capacity, not mainnet activation or a behavioral pass.

The [full512 race readback](evidence/full512-race-independent-readback-20261003.json) now independently verifies the public restore of 512 retained EVM segments under race detection: one complete passing root, 719 error-free sampler rows, a sampled 127,963,136-byte allocated peak and empty final fixture. Normal and race results are both retained. This does not establish production capacity, the native producer or full composed launch readiness; do not repeat the successful scope merely to regenerate summaries.

The [Core preparation read-cause review](evidence/core-preparation-read-cause-source-review-20261003.json) verifies the adjacent error-first correction for retained staging plans and moved-root reservations. Failed reads preserve their cause and original journal; only complete observed contradictions establish differing custody. Four new deterministic roots and twelve neighbors cover interrupted real root publication, healthy exact replay and true missing/changed reservation. This three-file successor is uncompiled and unexecuted; qualification and later caller adoption remain separate from the current SN0e5/Core2ea tests.

The [correction source-stage readback](evidence/0e5-source-stage-independent-readback-20261003.json) independently verifies the final cancellation/recovery successor as a complete clean archive: 20,650 tracked entries and 224,960,146 bytes, exact commit/tree and unchanged module inputs. Retain this preparation for subsequent graph, compile and behavioral phases; source provenance alone grants no behavioral or launch acceptance.

The [finite correction continuation authority](evidence/0e5-finite-continuation-authority-20261003.json) removes per-phase coordination waits after review of the complete source, scope and resource budget. The test owner may finish the remaining 31 specified phases with previously reviewed runners adapted only for paths and selectors, preserving exact input checks, bounded execution and independent result readback. Retain completed preparation; report substantive runner changes, custody failures and new scope rather than silently weakening them. This local test authority does not authorize signing or deployment.

The [correction durablehead graph readback](evidence/0e5-durablehead-graph-independent-readback-20261003.json) verifies readonly resolution of the newly selected package under the exact reused Core2ea module. Raw graph/error output, unchanged source modules and physical GoMod files match the recorded evidence. Compilation and behavioral qualification remain separate; successful source and graph preparation is retained.

The [correction peer-staging finding](evidence/0e5-peer-staging-compile-finding-20261003.json) retains two successful durablehead compiles and four compile failures caused by omitted local replacement peers. A selected package graph does not prove the complete workspace layout. Restore the seven exact already-qualified peer links, preflight every local module replacement and retry only the failed compiles; preserve tracked source/modules and passing images. This preparation defect is separate from product behavior.

The [peer-link repair readback](evidence/0e5-peer-link-independent-readback-20261003.json) independently verifies all seven restored links against their exact existing target commits, clean tracked trees and module hashes. No full peer restaging or source revision occurred. Preserve both durablehead images and original missing-peer failures; the four affected compile retries and subsequent behavioral tests remain separate evidence.

The [corrected durablehead positive readback](evidence/0e5-durablehead-positive-independent-readback-20261003.json) verifies all six selected roots normally and under race detection. Failed acknowledgment reads retain uncertainty and their cause, actual changed bytes still establish conflict, and exact lost-ack recovery remains intact. Raw top-level outcomes and protected image custody are independently checked. Vet and the old-body regression control remain separate; this component pass does not imply the remaining mainnet/validator scope or live deployment.

The [corrected mainnet normal readback](evidence/0e5-mainnet-normal-independent-readback-20261003.json) now verifies all 37 selected roots passing, including the original affected cancellation, plan-input, Claim restore and fee-input cases. Each root ran once with no skip; protected executable identity and raw results match. Retain the original failures as historical evidence. Race, validator, vet and operative controls remain required before this correction batch is accepted.

The [current foreground failures](evidence/0e5-validator-foreground-failure-source-review-20261003.json) retain six passes and three failures in each nine-root normal and race scope. A remote object write inside a ledger Walk retains the single-reader admission needed by completed RunTrail disk-proof projection; Stats catchup shares that gate too. Release read ownership before remote callbacks using signature-checked point reads over the fixed captured prefix, then prove the actual blocked mid-stream path. Continuous-work fixtures must budget their seeded records plus new foreground records, while retaining real capacity refusal. Require immutable prefix isolation, bounded memory/snapshots and owned cancellation without partial publication. Mainnet and durablehead successes remain retained.

The [three-package vet readback](evidence/0e5-three-vet-independent-readback-20261004.json) verifies successful mainnet, validator and durablehead vet commands with unchanged modules and matching raw output. Behavioral failures and operative regression controls remain separate.

The [continuous native producer source review](evidence/native-continuous-producer-source-review-20261004.json) records the frozen capture/replay, certified-descendant and retained-completion implementation. Source review is not qualification: require actual Rust engines through the public restart path and populated provider capacity. Continuous production must renew signed authority, runtime and resource scope without resetting completed accounting, rewriting historical evidence or recapturing acknowledged jobs. The current exact-authority admission and 4,096-job ceiling still need that successor. Native/vault/Claim conservation and the 10/90 economic policy remain end-to-end requirements.

The [native capture heap review](evidence/native-capture-heap-parity-source-review-20261004.json) identifies a capture/replay context mismatch against the actual pinned SDK. Capture now executes the original block onchain, preserving the proved `:heappages` setting used by strict replay; a complete parent with an explicit heap override must not remain unavailable because capture selected different memory semantics. Three deterministic regressions cover the override, default dynamic growth and the unchanged memory ceiling. They are authored and unexecuted. Existing complete-proof refill qualification still needs child, iterator and root-sibling requests that are not satisfied by returning the entire fixture proof on the first RPC response.

The [incremental native refill review](evidence/native-capture-incremental-refill-source-review-20261004.json) adds six unexecuted Rust roots through the public directory/feed capture entry. Every refill proves the requested hash using the pinned SDK's exact top or child prefix, then retains only that node. The cases include unread root siblings, odd-nibble ranges, missing iterator/root nodes and request-bound acknowledgements. Independent compilation and execution remain required; the Go broker, process deadline and durable cursor still need their own complete qualification.

The [ade5 failed-root census](evidence/native-adoptionade5-failed-root-census-source-review-20261004.json) independently reproduces all 43 failed names and seven passes per mode from hash-bound raw output. Every original failure stops at the unprotected scratch ancestor, before a restore product guard. One same-image protected-environment smoke root passes per mode. Continue the exact failed roots beneath protected ancestry while preserving original failures and successful evidence; these setup failures do not justify weakening durable custody or changing restore production behavior.

The [first four normal regression controls](evidence/0e5-four-normal-operative-controls-independent-review-20261004.json) independently reproduce the intended failures with old acknowledgment-read, named-cancellation and plan-input bodies, and with Claim compaction omitted. Each compiled and reached its named assertion with no skip; the changed test suite detects those production defects. Twelve remaining normal/race controls and their causes remain to be reviewed. Preserve the narrower early-cancellation scope of the plan-input control; it does not alone prove every read/close path.

The [mainnet correction race readback](evidence/0e5-mainnet-race-independent-readback-20261004.json) verifies all 37 selected roots passing once without skips and with unchanged executable custody. Together with normal results and vets, this retains the unaffected correction scope while the isolated validator successor is tested.

The [foreground successor review](evidence/foreground-successor-source-review-20261004.json) binds the three-file fix and its deterministic controls. Read ownership ends before remote callbacks while ledger lifetime remains owned and cancellable. Tests separately target original-prefix isolation, full foreground proof projection, joined owner close and unchanged real capacity refusal. The 20-root successor qualification remains pending; source review does not establish a passing release.

The [RPC retry traversal review](evidence/rpc-retry-cause-traversal-finding-20261004.json) identifies an adjacent hardening task: bound error-tree traversal and require a real transient cause. Cyclic, over-budget and empty/all-nil joined shapes must not acquire retry admission; genuine timeouts still retry and an independent permanent cause still wins. This is a source finding rather than a live incident, and correction must preserve the frozen producer qualification work.

The [foreground successor normal readback](evidence/foreground-successor-normal-independent-readback-20261004.json) independently verifies all 20 roots passing once with no skips, including blocked HTTP, cancellation and continuous replay. Validator vet passes with unchanged modules. Original failures remain retained; race and the two intended old-body controls still determine full correction acceptance.

The [native recipient reconciliation finding](evidence/native-recipient-cross-domain-finding-20261004.json) requires retaining the individual amounts already authenticated by execution, instead of only recipient identities and aggregate sums. Bind those amounts to the exact original earning interval and reconcile capture, carry, entitlement and payment without duplicate attribution. A RootMissed carry may legitimately pay in a later epoch with no new capture; preserve the earlier source obligation. Equal aggregate totals do not excuse changed recipients or boundaries. Amount authentication remains distinct from complete 10/90 conformance and actual runtime authority.

The [foreground successor race readback](evidence/foreground-successor-race-independent-readback-20261004.json) verifies the same 20 roots passing once under race detection with no skips and unchanged protected executable custody. Both positive modes and vet now pass; exact old-body controls still determine causal acceptance before integration.

The [complete correction controls readback](evidence/0e5-sixteen-operative-controls-independent-review-20261004.json) verifies eight intended old-body failures in each normal/race mode: 16 clean compiles, exact named assertions and no skips or unrelated race report. Acknowledgment uncertainty, cancellation taxonomy, Claim custody/receipt retention/compaction and indexed work are causally exercised. The original0e5 positive result remains 49 passes/3 validator failures per mode; the separate foreground successor owns their correction.

The [native finite resource admission](evidence/native-finite-resource-admission-20261004.json) admits the complete 66-phase producer, SDK-prefix and populated-capacity qualification scope under the existing cumulative budget and 2x forecast margin. Preserve completed source/dependency work and avoid per-phase coordination pauses for reviewed runners; new orchestration mechanisms still receive one bounded review. This is test-resource admission, not production capacity or launch approval.

The [foreground correction control review](evidence/foreground-successor-controls-independent-review-20261004.json) completes focused qualification. Restoring the old production iterator reproduces the exact gate assertion in both modes; restoring the old test fixture capacity reproduces the expected ceiling failure in both modes. These are distinct defect types. Combined with 20 positive roots per mode and vet, the correction is ready for source integration; actual host sizing and the full mainnet release remain separate requirements.

The [delayed renewal review](evidence/delayed-native-renewal-source-finding-20261004.json) applies the stale-plan lesson to continuous native accounting. Ordinary authorized progress must not invalidate a reviewed successor merely because its signed predecessor is no longer the latest cursor. Prove that predecessor as retained authenticated ancestry, preserve intervening completions and pending scope, then recheck cumulative capacity before adoption at an untouched boundary. Unrelated or rewritten ancestry remains a refusal. Deterministic delayed-review and pending-completion tests are required.

The [final miner selected qualification](evidence/final-miner-selected-scope-root-readback-20261004.json) now retains seventeen passing roots per mode through an explicitly checked unchanged import closure and one fresh corrected root. Broader runtime and operative controls remain separate. The final Server source has a narrow test-only compiler correction pending; continue independent package discovery while that successor is checked. Final publication must bind actual successful source inputs, not the earlier frozen candidate alone.

Repair dispatch must admit busy services as pending before they can consume the bounded worker pool. Qualify a deterministic unrelated-service continuation test and both service-lock paths before enabling the controller. Pending outcomes must retain the original signed envelope, intent and cumulative allowance; they do not authorize a fresh action.

Retain successful qualification phases across runner corrections. Bind the actual Go/tool identities, package working directory, full child environment and private UMask0077. Remaining full-history normal/race tests and final Server/model checks remain required. Temporary root compiler storage is proposed, not adopted, pending the user's exception to the data-volume-only direction. A resource increase never resets the original baseline or substitutes for actual physical capacity.

Keep root and operator recovery authority distinct from subnet-validator repair. The passive-root adapter source now preserves original activation/bootstrap custody, checkpoint and permanent generation ownership before issuing its one approved start. Independent tests and integration are pending; operator recovery still requires the actual layered WARP environment and database/signing-resource profile. Neither a generic restart policy nor a validator envelope authorizes those operations.

The [published monitoring 144-test readback](evidence/xops-final144-publication-root-readback-20261004.json) passes exact source/output and actual exit/join checks. SN comparator's fourteen ordinary positive roots also pass independently; role startup, repair, race and control scopes remain separate. The production policy-reader frame mismatch now has a coherent successor awaiting qualification, rather than an exception allowing rejected service authority to pass.

The [full129 Claim ordinary failure](evidence/full129-normal-claim-failure-root-readback-20261004.json) remains unresolved. Retain the independent passing restore scope and continue its race run. A diagnostic stage split supplies no acceptance by itself; the correction must satisfy the actual custody and bounded-work requirements. The [future compiler admission](evidence/future108-narrow-compiler-adoption-20261004.json) permits one compiler alongside that healthy history under fresh physical and memory guards; no database or second-history overlap is included.

The [provider whole-work launch source handoff](evidence/provider-whole-work-launch-source-20261004.json) freezes SN `7da3e844` over `b4662ed8`, requiring Core `7de1d3e8` and SDK `9ae95704` in the final composition. Standalone and swarm provider roles now configure their actual SDK manager from an exact reviewed profile binding the independent request key, complete original domain, retained client identity and private outbox. Complete-mode CLI and bootstrap exports require that profile; missing configuration refuses startup, while legacy omission remains unknown. No request key is inferred from SQL, artifact signing or prior bootstrap approval. Restart preserves original cut bytes and enrolls a fresh manager generation.

The eight new miner and three new mainnet roots are authored but unexecuted. Public CLI refusal, public bootstrap export and actual shared DeviceLocal constructor lifecycle are distinct fixture scopes. Preserve the historical miner17/mode qualification separately; it does not qualify this new feature. Signed enrollment proves key possession, not a complete work census. Independent complete owner/window authority, Server route/custody and pre-sign attachment composition, exact normal/race/causal qualification, actual host adoption and mandatory MG06 conformance remain launch gates.

The [provider prepared-custody successor](evidence/provider-whole-work-prepared-source-20261004.json) freezes SN `07d531ba` with Core `77069204`. Offline recovery now shares the exact approved-profile decoder without requiring a moved original directory to be live. The actual standalone and swarm SDK constructor checks explicitly prepared outbox custody and signed scope before creating the device; runtime settings pin the same independently approved provider key. Runtime cannot create a missing birth. Rotated historical keys need separate original-profile/key-history approval, and retained leaves cannot supply that authority. Read-only admission releases its temporary lease; the live worker reacquires and monitors custody.

This successor adds six distinct miner roots and revises the two existing lifecycle fixtures to prepare one synthetic birth before first startup. The prior six other miner and three bootstrap root sources are unchanged. All new and revised roots remain authored and unexecuted; historical miner17/mode evidence is separate. The corresponding fresh/restore preparation adapter, exact composed qualification, actual approved deployment inputs and MG06 remain required.


Qualification source identity is now local to each execution owner. During the internal-SSD fallback, an unrelated SN mainnet/Rust merge caused a whole-repository cutoff guard to refuse an unstarted Go owner. Its shared plan also rehashed the Go job from the healthy Rust sampler, so changing the Go preparation would have interrupted Rust. The original Go owner stayed unstarted and Rust kept running. The [owner-local source readback](evidence/qualification-owner-local-inputs-source-readback-20261005.json) records the correction and twelve actual deterministic passes.

New callers use `scripts/qualification/owner_input_admission.py`: the shared plan fixes USER units, command paths, cgroups and budgets, while each owner freezes its own adopted job, runner and complete selected-input scope. Unrelated files or a not-yet-started peer's preparation do not invalidate another owner's source authority. Selected physical Go, module, native and embedded files are compared to authenticated Git blobs, including ignored and index-hidden files; selected symlinks, moved inputs and nonblocking open replacement races refuse. Only selected subtrees and their ancestors are traversed. Selected inputs still refuse changes, and resolved dependency paths/device/inode checks detect broken or rebound local cache/source symlinks. Existing lease, fixed resource floors, process ancestry, OOM, child joins and result checks remain mandatory. This source helper does not discover a dependency graph or promote retained test results; runtime callers must explicitly adopt it.
