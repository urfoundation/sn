# sim-testnet finalization report

**Verdict: INCOMPLETE. `final_acceptance: false`.** This report follows [FINALIZE.md](FINALIZE.md) and [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md), incorporating completed artifacts through 2026-09-11 03:41 UTC. Complete strict producer/aggregate gates, accepted release-candidate and production campaigns, and independent public replay remain pending. The source lock has not been regenerated for the integrated composition.

A provisional workload soak is running with a retained 1,000-provider fleet and separately managed validators. **Six records have been locally signature-verified:** validator 1 has one per operator in each of epochs 278 and 280; validator 2 has one per operator in epoch 280. Validator 1's two R32 records completed with all background workers resumed. At 03:39:56 its operational counts were 27 + 25 = 52 records; counts beyond the six individually checked records are not additional signature qualification. Both validators now run the R32 image; validator 2 is warming after its roll. Sustained performance and complete epoch coverage remain unqualified. The earlier [02:24 workload review](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workload-blocker.md) records registration/boundary deadlines and native epoch 1381 deferred without submission; it is historical evidence, not a current zero-proof claim. The [recorder](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r30/observational-soak.jsonl) retains subsequent observations.

## Scope, source and approval

The user's provisional testnet waiver permits the ongoing workload and valid evidence reuse. Original used-plan lineage, transaction custody and approvals remain binding. Waived formal criteria are not reported as passed.

The integrated SN production revision is `bc7dd7e0c2261320d4ae20164c0e3917f247802a`, on `codex/finalize-sim-testnet-20260911`. At 03:03:30, the active workspace became a regular Git checkout with identical tracked bytes and retained dependency pins; its recorded upstream was `origin/main` at `913d4e92fe78efeebd82e9e117f6bf989671e0cf`. This addresses local Go 1.26.6 VCS discovery for the next release build; it does not retroactively add an embedded revision to the earlier R32 binary. See [checkout activation](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/sn-regular-checkout-activated.json).

Server `4b8c3303f723f4ccbb1562713c32b05c77eb0bf5` and Connect `eb163c0079006119e010cc74a8775919502318c4` were pushed to their respective `origin/main` refs with actual exit 0 and joined owners at 03:05. [Server receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/source-publication/server-result.json), [Connect receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/source-publication/connect-result.json). SN publication, final release-lock regeneration, complete source fences and release qualification remain pending in this report.

Local artifact links in this draft preserve provenance for review. They must be supplemented by secret-scanned, immutable public locators and hashes before final acceptance. A local file path is not public availability evidence.

## Phase and gate status

| Phase | Observed evidence | Acceptance status |
|---|---|---|
| Source integration and freeze | Regular SN checkout at `bc7dd7e0…`; server `4b8c3303…` and Connect `eb163c00…` published. Final SN publication, lock and fences pending. | PARTIAL |
| Complete strict producer | Latest located v4 run: native exit 1, owner exit 1, all 27 phases joined; `server-db`, `solidity` and final source fence failed. Source was SN `7818a20…`, not the current composition. | No current PASS |
| Complete aggregate | R12 native/outer exits 1; 19/21 phases passed; `sn-simulator-race`, `server-db` and final fence failed. Its candidate-fence wrapper was explicitly not a strict certificate. | No current PASS |
| Focused component integration | Connect 26 roots and server five roots passed normally and with race detection, with actual 0 exits and joined ownership. These supplement retained component receipts. | PASS, bounded scope |
| Private DB upgrade rehearsal | Both isolated operator copies bridged 638→653→656; protected rows/history preserved and private containers removed. No live DB migration occurred. | PASS, rehearsal only |
| Formal launch admission | Original paired-plan/custody evidence retained. Current workload uses provisional external services; current strict preflight and formal generation criteria remain open. | PENDING |
| Provisional workload observation | Retained 1,000-provider fleet; both R32 images observed; six locally verified records, including validator-1 progress with background workers running. Validator 2 is warming. | RUNNING, provisional only |
| `release-1.0` | No accepted five-epoch capture/semantic closure supplied for this report. | INCOMPLETE |
| `production-soak` | No accepted three-epoch production-policy capture/semantic closure supplied for this report. | INCOMPLETE |
| Public replay and peer review | No clean-checkout full-graph replay agreement supplied for this report. | PENDING |
| Delivery and shutdown | Final report/source/lock publication and no-autostart/actual-join closure remain outstanding. | PENDING |

The [gate evidence review](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/gate-evidence.md) records exact revisions, receipts and failure paths. R12's [cleanup result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/candidate-aggregate-r12-terminal-closure/CLEANUP-RESULT.json) reports successful ownership cleanup while explicitly recording `native_full_gate_pass:false` and `source_fences_passed:false`; its cleanup status must not be interpreted as qualification success.

Connect's 26 selected roots passed in 1.384s normally and 2.638s with race, with zero failures/skips and all sessions joined; the original metadata-prerequisite failures remain retained. [Connect integration](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/connect-integration/integration-result.json). The five affected server model roots passed in 23.461s normally and 29.519s with race, using the checked-in private fixture; both PostgreSQL/Redis IDs were verified absent after cleanup. All resolved module versions stayed identical despite checksum metadata additions. [Server integration](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/server-fixture-integration/RESULT.json).

The producer scheduling change partitions all 178 selected simulator roots into 176 ordinary + two slow roots without omissions or overlap, in both normal and race modes with `-count=1`. Each partition has its own 10-minute package clock; this is not a pass of the old combined selection under one clock. Seven existing test bodies add only `t.Parallel()`, retaining private mutable fixtures, assertions and workload sizes. Fourteen focused guards plus compiled inventory checks passed with native/owner exits 0 and joined ownership; original slow bodies and full gates were not rerun for this scheduling review. Forge/generation checks, bounded phase admission, child joins and final source fences remain required. [Scheduling review](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/semantic-profile/scheduling/REVIEW.md), [inventory and actual exits](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/semantic-profile/scheduling/producer-partition/inventory-proof.json).

The private DB rehearsal preserved all 638 original catalog entries/timestamps and protected 29 tables per operator (6,865 and 11,375 rows), including signed history/staging and prior archive. Fifteen gap applications and three native tail migrations reached 656; mismatch/repeat refusals preserved state. The rehearsal owner and cleanup exited 0. [Rehearsal](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/database-bridge/RESULT.json), [cleanup](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/database-bridge/CLEANUP.json). Live DB migration remains separate work.

Full campaign acceptance requires five complete accelerated 300-block epochs plus a 150-block terminal window, followed by three complete production-policy `360/60/180/6` epochs plus their 180-block terminal window. The first partial observed epoch and terminal tails do not count as complete epochs. Preserve the native/EVM deadlines and finalized checkpoints, all 61 adversarial vectors and seven attributed concurrent actors, required sample/latency/error bounds, per-validator/per-operator proof growth, accounting invariants, intentional recovery evidence and owner-signed closure for both phases. None of those aggregate criteria is inferred from the provisional recorder's elapsed time.

## Retained setup, custody and budget

Retained plan `0x76d0386164e7019aa0c5a9ea2b9540705a5a3e492ea7b55e06856a399b1d9f8d` has config hash `0xddacd7f502ca598ffb7cd73b1ac0e4ee9e10a5476b2e3c29da16c4881751cc4c`, deployment `ur-subnet-testnet-v1`, chain 945/netuid 521. The setup journal ends at sequence 10154 on 2026-09-10 18:47:28; 2,229 of 2,307 current action intents match retained verified receipts. This indexes original evidence without replaying setup. The [setup report](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/report-setup-evidence.md) and [JSON index](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/report-setup-evidence.json) contain genesis, policy/release identities, original plan/intent ownership, code hashes and exact transaction/block/artifact hashes.

| Identity | Retained address |
|---|---|
| Coordinator proxy | `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8` |
| Immutable settlement vault | `0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e` |
| Immutable reserve sink | `0x376f98bd7c6b334f7f1cb2685e0970a18bfe7d28` |
| Current implementation | `0x7a169b125f01183957f7d34d17cfcfd9178b6796` |
| Current conformance probe | `0x3d7c317d7aa730341b2b41d8cdc66c48ae7a0f48` |
| Evidence companion | `0x79a597b4441c8b1b2fb8bfd93c1cd428c856ad9f` |

Original reserve creation transaction `0xd1dbc5e76809e436e8e8a8fc04bd67ce2e1f6c4895b5558432da0c941d27540d` finalized at EVM block 7894952 / `0xa031d2856d6616aa04ee0af9fd66b91772cee1cd10d838192ead9ff38575ee76`. Current implementation activation `0x3a8d29a7a73829c68ca40bc1680eda9329ad07092013f86396f93ebbce8c15a7` finalized at 7973227 / `0x63d83dc33d0fa4ffe1fbd2b640b9607f7ddbeee8586c00dc15e7c043c10ac387`. Companion anchor `0x759e7e477775a4f943e26f0c1a1bce27fd9707bed0a18f599f61ca4e0f7d349b` finalized at 7973242 / `0x1c7a6fee5056d552f1eee65c3d1555ef44f6fb862281d98cd02be1e9c6a43a9f`.

All four original validator/operator activations and their transaction hashes are indexed. Their used prepared hash is `0xd3b5fe069b9789b8549f99ced57d461c4c5fe84380cc1945b43f9b9421d3d9d0`, completed at boundary 7975774 / `0x7addfed5eb2cb18c3b20bca16c9ddc61a10eb4e890c48995078586c49dccc248`. Custody wiring, both operator registrations and validator UIDs 254/255 retain their original receipts. No activation was regenerated or rebound. Companion setup is not proof of accepted-epoch evidence publication or a completed governance drill.

| Dimension | Approved ceiling | Combined current + superseded planned maximum |
|---|---:|---:|
| Total TAO | 200 | 185.748236 |
| Lifetime alpha | 31,250 | 31,250 |
| EVM gas, TAO-equivalent | 180 | 180 |
| Registrations | 262 | 262 |
| Subnet creations | 0 | 0 |

Preserve the separate **6,000-alpha repair tranche**. These are planning/approval envelopes, not paid totals; the 180 EVM ceiling is not added to 200 total TAO. Current budget reservations include 3.471 TAO and 42.8135 EVM TAO-equivalent, which are not paid fees. Historical registration-bearing transactions total 259, distinct from planned or currently active registrations.

Observed exact-transfer receipts establish **30,999.499999975 alpha across 12 transactions**. Three older finalized alpha transactions lack sufficient exact-amount receipt fields for a complete lifetime paid total. The last reserve repair transferred **5,999.806443325 alpha**, transaction `0xdf681a0964bd9609434ff9884c57406ee8b8ec07b308275eb34b4aa9ffa383a6`, Substrate block 7973247 / `0x25681a4af6055c34ee57b355e84fe0bda0994f6a6575defd3f757edccc5b73a7`; its historical receipt satisfies the reserve share checks. Actual cumulative TAO transfers and native/EVM fees remain unresolved. Balance/minimum observations and fee ceilings must not be relabeled payments.

## Verified provisional proof and rollout evidence

The existing `validator.VerifyProofRecord` function checked six records against configured public keys, locally without RPC or live-file writes. All three verification runs joined with actual exit 0. All six passed server FINAL signature, validator FINAL co-signature, validator EXTEND signature and digest/path/identity/hop checks; all are depth 8, coverage 7.

| Validator/operator | Epoch | Complete timestamp UTC | Snapshot SHA-256 |
|---|---:|---|---|
| Validator 1 / operator 1 | 278 | 2026-09-11 01:31:27.801 | `c8ee74775a17ea882fd0907760980a7edcac38ca6be10fe5b5d13f8bccf5e2a3` |
| Validator 1 / operator 2 | 278 | 2026-09-11 01:31:24.799 | `ba652903f8a2f429ac4d23b8b8b32256f8340c04ee9af3a2edfb77f2bfc262d0` |
| Validator 2 / operator 1 | 280 | 2026-09-11 02:54:43.247 | `2e58bf41290c0171e5e4f54bfe9246096a7496ba8e417916f45a2ef2ff4b1368` |
| Validator 2 / operator 2 | 280 | 2026-09-11 02:55:06.791 | `6f9b080e0249ca4fb8d68311d317b1bd7c9483505b0d41582fff0cb4e2b8f666` |
| Validator 1 / operator 1, record 2 | 280 | 2026-09-11 03:15:46.731 | `13e980d92ea7aa3f3b62ce96958544dab997d38417141b6089d7fa30a29ac4a7` |
| Validator 1 / operator 2, record 2 | 280 | 2026-09-11 03:16:32.750 | `38da2ec6544416e20dfce17abade152e2dea056a244b664e15f25a684aea82e7` |

Validator-1's original [result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r31/first-two-proof-signatures/result.json) SHA-256 is `1a0ada991e77b2b8e25447a8cd955b5ccb31df35ab93a5f38e4ace296d6705fd`, with [actual exit](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r31/first-two-proof-signatures/native-exit.json). Validator-2's [result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-2-first-proofs/result.json) SHA-256 is `9a264523a0de10d2163bbfcc60a46ec17327db96d23044db61ea0ea8ff71784d`, with [actual exit](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-2-first-proofs/native-exit.json). Validator-1's new [result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-1-new-proofs/result.json) SHA-256 is `f73a1c08b43d08f7344469f13667493443204b9abe1bc1f82abfdfaae278d5a1`; its [native exit](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-1-new-proofs/native-exit.json) is 0, joined at 03:40:10. Counts beyond these six records are not signature-qualified by these helpers. Complete accepted-epoch census, anchors and RC acceptance remain open.

Validator 1 restarted at 02:54:35 with actual restart exit 0, unchanged protected plan/journal/config inputs, PID 1196139 and invocation `9cb4ab8a7722486094c40471a02a8fa3`. The later [actual-image observation](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-1-active-image.json) at 02:56:39 confirms SHA-256 `6b75d31d7c43c647fae4dacf013ffea1ff1533ef6105544eb64ececd5c23c76d`, matching the R32 build from `bc7dd7e0…`. Use that observation rather than the immediate restart receipt's transient Python launcher image. [Rollout receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-1-roll-result.json). Its two new verified proofs establish application workload progress at 03:15–03:16, after all background workers resumed at 02:57:19; full campaign acceptance remains pending.

Validator 2 rolled at 03:41:29 with restart actual exit 0 and joined ownership; old PID 1031640 exited. PID 1268183, start ticks 138442835, invocation `0d71ca523a904c7f98f6415765256825` and zero restarts were observed. The 03:41:31.870 [actual-image receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-2-active-image.json) confirms the same R32 SHA-256 above. Plan, journal, supervisor and both validator YAML inputs were unchanged. [Rollout receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/validator-2-roll-result.json). This new invocation is warming; its earlier verified proofs do not claim fresh R32 validator-2 completion.

The R32 production `NewTunnelTransport/PostVerify` probe reached two exact live hops with intentionally invalid `{}` bodies: the first returned expected HTTP 400 after 14.210s and the second after 60ms, within unchanged 30s step deadlines. It used one derived identity, claimed no proof, and joined transport/identity cleanup; native exit was 0. [Probe](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/live-two-hop-capacity-probe/result.json), [exit](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/probe-capacity-exit.json). Four background workers were paused under a 300-second owner from 02:52:19 to 02:57:19; all four exact process identities resumed, and the owner was inactive/successful. [Resumption](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/provisional-r32/capacity-20260911T0252/resume-observed.json). These capacity-controlled results do not establish sustained performance under the full workload.

## Final evidence matrix

The matrix follows FINALIZE-COMPLETE §10. **PENDING** means complete acceptance evidence has not been verified for this report; it does not mean the underlying deployment or action is absent. Each final row needs exact artifact URLs/hashes and replay commands, plus transaction/extrinsic hashes, addresses/IDs and finalized block numbers/hashes for chain claims.

| # | Requirement | Current evidence and remaining acceptance work | Status |
|---:|---|---|---|
| 1 | Chain/deployment | Exact plan/genesis, deployment addresses/code hashes and later upgrade/probe receipts indexed above. Current runtime attestation, regenerated source lock and independent replay pending. | PARTIAL |
| 2 | Governance/custody | Original immutable vault/reserve, wiring, approval and activation custody retained. Full governance drill and mainnet governance delta remain open. | PARTIAL |
| 3 | Operator pools | Both original pool registrations and role/UID receipts indexed; six records verified across four validator/operator identities. Accepted-epoch assignment/traffic/quality and public replay pending. | PARTIAL |
| 4 | Demand deposits | Supply required-versus-observed tier/rate deposits for both pools in every accepted epoch, underpayment effect and funded recovery receipts. | PENDING |
| 5 | 1,000-miner fleet | Provisional recorder reports 1,000 ready/20 swarms. Supply unique client manifest, full allocation, 202 candidate commitments/bindings/lineage, ownership/pruning and cleanup evidence. | PARTIAL |
| 6 | Top 200 | Both signed rankings, 200 positive/two zero outcomes, applied native vectors and promotion/demotion/fallback evidence pending; the earlier insufficient-input failure remains retained. | INCOMPLETE |
| 7 | Path proofs | Six records locally verified, plus original companion/anchor/four activation setup receipts. Need complete per-validator/operator/epoch census, proof-hash publications and semantic replay. | PARTIAL |
| 8 | CRv4 | Epoch 1381 deferral/no-submission is retained historical evidence. Accepted campaign commit/reveal/application extrinsics, finalized blocks and consensus reconstruction pending. | INCOMPLETE |
| 9 | Head/tail accounting | Supply theta and realized sums, head native rewards/exclusion from tail pools, pruned-return and reconciliation evidence. | PENDING |
| 10 | Payout roots | Supply canonical artifacts for every operator/epoch, on-chain roots/events/state and exact leaf proofs; demonstrate wrong-leaf/wrong-operator `InvalidProof` with unchanged finalized state. | PENDING |
| 11 | Contract claims | Supply funded/claimed/liability totals, real `ClaimPaid` in both pools, recipient deltas, replay rejection and pending/outstanding amounts. | PENDING |
| 12 | Validator rewards | Supply native dividend/emission/stake deltas for both validators; do not substitute bounty evidence. | PENDING |
| 13 | Miner rewards | Supply selected native-head rewards, rejected zero allocations and actual tail-pool payments. | PENDING |
| 14 | Reserve | Exact last repair and historical 65% target/60% minimum receipt indexed; current compounding, complete paid totals and accepted-epoch conservation pending. | PARTIAL |
| 15 | Settlement conservation | Reconcile captured = paid + escrow and escrow = pending funding + outstanding liability for all accepted epochs. | PENDING |
| 16 | Adversaries | Supply all 61 vectors/seven actors, attributable seeds/samples/latency/errors, applied/restored faults and a clean attempt ledger. | PENDING |
| 17 | Runtime | Both R32 images and unchanged protected inputs recorded; validator-1 proofs completed after background resumption. Validator 2 is warming. Formal generation, full error/restart accounting and no-autostart closure pending. | PARTIAL |
| 18 | Public history | Supply both archive locators, complete graph sizes/hashes, replica fetch/rehash and secret scan; owner completion/supplement signatures and immutable manifests. | PENDING |
| 19 | Final epochs | Supply five clean accelerated and three complete production-policy epochs with correct terminal windows and signed `capture_closed`/`semantic_verified` for both phases. | INCOMPLETE |
| 20 | Public replay | Supply both authorized signed discovery paths, pinned native/EVM transcript, clean-checkout replay commands and matching verdict/artifact hashes from independent review. | PENDING |

The later explicit on-chain-hash decision in FINALIZE-COMPLETE §10.1 controls the older matrix's “pending decision” wording. Full proof bytes remain signed off chain and publicly available; commitment hashes alone establish neither availability nor signature truth. Required signed settlement-tail closure (§10.2) and bounded chunk/census storage and replay (§10.3) must also be demonstrated, including complete accepted projections and legacy-byte/signature preservation. Their final implementation and evidence status are **pending review**.

## Anomalies, exclusions and recovery

| Finding | Retained evidence and consequence | Closure status |
|---|---|---|
| Strict producer failed | Server DB fixture failures; simulator race package deadline; final source fence failure. All actual phase/owner results remain preserved in the gate review. | Not a passing gate |
| Aggregate R12 failed | Full simulator race package timed out at 90 minutes; server-proxy MMDB fixtures were missing; final remote-ref source fence failed. Candidate wrapper is explicitly not a strict certificate. | Not a passing gate |
| Specific proxy fixture recovery | Same captured proxy binary passed all 84 normal roots and two exact 20-root confirmations with actual exits 0 and joined cleanup. Original wrapper failure is retained. | Bounded component recovery only |
| Tunnel setup race | Prior per-request lazy setup could not accommodate processed key registration within its expansion budget. Explicit pinned-client change reached real `/verify`; the first two signed production workload proofs then completed. | Causal repair has partial live evidence; sustained performance remains unqualified |
| Repeated registration and reserve-boundary deadlines | The 02:24 review records these failures. R32 reused-client probing, validator-2 first proofs and new validator-1 proofs with background workers running show later progress; sustained full-load closure remains unproved. | OPEN |
| Insufficient native inputs / stale settlement | Epoch-1381 local inputs contain no scored providers for validator 1 and only a zero-confirmation provider for validator 2. Signed inputs referred to settlement 278 after active settlement reached 279; both validators explicitly deferred with no submission. | OPEN; not native completion |
| Provisional topology exception | Validators run in separate managed services after the retained supervisor exhausted their restart allowance. This is authorized provisional operation but does not silently satisfy the formal single-generation criteria. | Formal admission unresolved |

The first two validator-1 proof records were produced during a bounded pause of four background roles, 01:29:12.385272–01:33:18.486146 UTC. This supports shared-capacity relief as a causal test; it does not establish that capacity is the sole remaining cause. Background roles were resumed. Preserve the raw logs, counters, original failures, pause owner exit and subsequent results; do not discard failed attempts or label a frozen historical prefix as a current error.

The user reported a brief internet outage. Its exact interval is unknown, and individual recorded failures have not all been attributed to it. Preserve both that report and the separately evidenced software, quota and deadline failures. The two original validator-1 records above predate R32; validator-2's first two records arrived during the later bounded pause. The two new R32 validator-1 records completed with background workers running, providing live progress without qualifying full campaign performance.

The full excluded-attempt inventory, causal fixes, regression commits and current-composition rerun evidence remain to be assembled. A formal passing run requires a clean attributable anomaly ledger; this draft does not certify one.

## Public replay, completion and delivery

Required final artifacts include `public.json`, `plan.json`, `journal.jsonl`, `config.redacted.yml`, process logs, native/EVM receipts, operator/validator artifacts, `assertions.json`, `adversaries.json`, `anomalies.json`, `analysis.json`, self-contained `analysis.html`, `junit.xml`, `result.json` and `complete.json`. Completion requires authentic owner-signed capture and semantic verification, the complete immutable graph, secret scanning, both archive replica readbacks and exact hashes. Missing/truncated logs, unresolved transactions, missing artifacts or violated invariants preclude a successful result.

Independent review must use a clean compatible checkout, no simulator DB or wallet secrets, either operator's latest authorized signed manifest and exact run ID, and all pinned native/EVM reads needed to reproduce the evidence matrix. Both analyses must match this report's final verdict and hashes. Commands/results remain **pending**. Retained `STATE/public.json` is historical: generated 2026-09-01, old plan `0x4ea536…`, runtime 452 and old config/policy/probe. It is not the current completed manifest. Loopback origins `127.0.0.1:18081` and `127.0.0.1:18082` establish neither off-host availability nor independent public replay.

After evidence and peer review finish, retain public history, chain deployments and claim paths. Stop local processes no longer needed through the retained harness state, join actual owners/children, record terminal results and prove user units are not enabled and dependency restart policies are `no`. Do not retire a deployment without a separate explicit retirement request. Final source/report/release-lock commits and push receipts are **pending**.

## Mainnet deltas

Testnet RC completion would not authorize mainnet transactions. Record and close the applicable separate requirements: independent private archival RPC and sustained-load capacity; DNS/TLS and authorized public history; Loki/Grafana/general statistics and alerts; 2-of-3 Safe/timelock and recovery drills; distinct mainnet keys/caps and an approval-identical mainnet plan; secret rotation and least-privilege storage; backup/restore and RPC/archive outage exercises; runtime, fee/burn/liquidity and economic review; unresolved anomaly/security closure; and the prescribed mainnet rehearsal and capacity evidence. Do not reuse testnet approval or credentials as mainnet authority.

The operational [requirements checklist](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/requirements.md) tracks the remaining acceptance obligations. This report must be updated from actual frozen evidence as they close; elapsed provisional run time and successful process starts cannot replace them.
