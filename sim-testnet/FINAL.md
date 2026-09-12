# SN sim-testnet final report

This report records the actual shortened testnet exercise and its later evidence corrections. The shortened [retained plan](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/sn/FINALIZE-ACTIVE.md) governed that exercise. The current [full finalization request](../FINALIZE-ACTIVE.md) supersedes its waivers for future completion. **final_acceptance=false**: full release qualification and the full campaign remain incomplete. Failed and partial attempts remain evidence for independent peer review.

**Preparation checkpoint, 2026-09-12 21:55 UTC.** The fleet and soak remain stopped. Actual read-only `doctor` on source `02dfe50` passed 63 of 64 checks; its sole failure was the user systemd manager's degraded state. All 43 failed units belonged to earlier stopped simulator jobs, with no main PID or cgroup. Their metadata and 621 available journal entries were preserved before clearing only those failure flags; systemd then reported `running`. No process was restarted. [Doctor result](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/doctor.stdout.json), [preserved unit evidence hashes](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/failed-unit-evidence.sha256), [manager state](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/systemd-after.txt).

Four actual read-only `setup` attempts exposed successive migration blockers. Source `02dfe50` refused the full retained audit budget's `observed_at` field; `d282aba` accepted that signed document but refused the already successful repair activation without a verified journal marker. Source `9c444e4` authenticated and carried the exact repair transactions, then stopped at `validator evidence plan has a different predecessor CREATE` at 21:24:41 UTC. Source `cf3ccd6` corrected that binding by preserving the original companion predecessor under the authenticated repair carry. It advanced to **reserve-majority capacity** and stopped at 21:48:11 UTC: restoring the target requires **2,855.249565922 alpha**, while cumulative signed transfers already consume the **31,250 alpha** lifetime limit. The cause and available remedy remain under review; this attempt does not authorize another transfer. Each original error is preserved: [budget document](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup.stderr), [transaction recovery](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v2.stderr), [combined binding](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v3.stderr), [reserve capacity](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v4.stderr). All four attempts emitted no revised plan; the original plan and journal still match their [retained hashes](../../temp/sn-full-finalization-20260912/readonly-admission-20260912/state-before.sha256). No new deployment, renewal, relay continuation, chain transaction or soak launch occurred during this preparation.

On candidate `9c444e4`, all five repair-carry roots, four relay-continuation roots, the corrected offline authority root and six archive roots completed three fresh normal passes and one race pass. Seven public-checkpoint roots, fifteen native-capture roots and two CRV4 checkpoint roots also passed normally and under race. These results retain the original fixture failures and the real archive-copy defect that dropped its required canonical newline. The [completed affected-qualification receipt](../../temp/sn-final-release-20260912/runtime-9c444e4-20260912T2118Z/RESULT.md) identifies the exact populations, source, binaries, logs and input fences. This candidate is pushed on `codex/sim-testnet-final-20260912`; it is not a full-release or live-acceptance pass.

Successor `eca9e19` passed six native-coverage roots, six companion-carry roots, six adjacent evidence roots, seven archive/payout roots and fourteen native-consumer roots normally and under race. Its historical reward-reader cancellation root failed to join an RPC batch, then exceeded the unchanged ten-minute package timeout during HTTP cleanup; the other reward root passed, and reward race was not run. Astra traced this to GSRPC's `CallContext` bypassing the cancellation wrapper's `Call` method. Its correction is integrated for fresh qualification. [Exact successor receipt, passing populations and preserved timeout](../../temp/sn-final-release-20260912/runtime-eca9e19-20260912T2138Z/RESULT.md).

**Reserve observation, 22:02 UTC.** At canonical finalized native block **7,992,355**, hash `0xbbc5a964c18850b0d6badded7ff0cef82d1eb46e7031b9be81bb6b37e3c640fc`, the complete 256-UID census contains **80,409.602927411 alpha**. The retained reserve hotkey at UID254 holds **49,410.992336897 alpha**, or **61.44911868486939%**: above the configured 60% minimum, below the 65% repair target. The prior 5,999.806443325-alpha repair is already credited and cannot be counted again. No further transfer is authorized by this observation. [Original pinned RPC responses, complete stake census and read proof](peerreview/evidence/reserve-alpha-20260912T220217Z.json) (`sha256:cf8963f7603cbb09909a36ea944004295dbe920dc36ec39030984642258ca061`). The returned read proof is retained but has not been independently replayed.

The corrected renewal evidence roots have three fresh normal and three fresh race passes on `73ad855`. The next composed source `bcb1ce0` passes the selected native/EVM checkpoint, capture, history-read and startup populations in both modes; three simulator fixture failures and two archive fixture failures remain preserved for qualification on corrected source. [Renewal confirmations](../../temp/sn-semantic-renewal-generation-validation-20260912/runtime/RESULT.md), [composed results and failures](../../temp/sn-finalization-integration-20260912/runtime/RESULT.md). The earlier aggregate simulator race timeout remains recorded in the [original matrix](../../temp/sn-strict-composed-fixes-validation-20260912/runtime/preflight-20260912T191953Z/RESULT.md). Contract outputs were regenerated at `5c4c546`; the revised full Forge build and 18 binding-policy tests pass, and the coordinator creation/runtime bytes [exactly match the deployed repair artifact](../../temp/sn-contract-generation-20260912T1841Z/runtime/generation-20260912T1845Z/full-build-revised-coordinator-compare.stdout). Full producer/aggregate qualification and both live acceptance phases remain outstanding.

**Payment recovery completed, 2026-09-12 17:12 UTC.** All **16 epoch309 claims** finalized on chain before their expiry block, paying **103.320655346 alpha** across both pools. At finalized block **7,990,906**, `totalCaptured=103320655354`, `totalPaid=103320655346`, `pendingFunding=0`, `outstandingLiability=8`, `escrowAccounted=8`, and `conservationHolds=true`. The remaining **8 alpha-rao** are integer rounding residue (5 / 3 per operator). Actual gas was **0.049882627183941739 TAO**, within the reviewed 0.4550417 TAO projection, using existing relayer funds. [Canonical receipts and pinned vault calls](peerreview/evidence/epoch309-paid-claims-20260912.json) (`sha256:2aa27bfb3efa15bb87f93fc9fbe6adbd8d64a1e57e5ff68fab6d5a1a2deb18ec`).

The payout failure was an identity configuration defect: every daemon used its operator’s shared network JWT, which resolved the last shared wallet. The committed leaves belonged to individual provider clients. Recovery used the existing single-epoch claim command with each entitled client’s credential; original queues and signed artifacts were retained. The renderer now selects each miner’s `.provider.jwt`. Only two operator APIs and two RPC proxies were started for recovery; all four stopped at **17:13:50 UTC** with no remaining processes. [Recovery shutdown](../../temp/sn-full-finalization-20260912/epoch309-claim-recovery/services/stop-result.json). The full fleet and soak remain stopped while the full-finalization repairs and gates proceed.

**Retained storage observation, 18:25 UTC.** Terra read four copies of the stopped validators’ source ledgers with the database opened read-only; their file metadata remained unchanged. The largest source contains **134,673 records**, **16,958 trails**, **698,568,804 raw record bytes**, **283,301,268 bounded storage bytes** and **139 files**. The original per-source limits are 655,360 records, 81,920 trails, 10 GiB raw records, 24 GiB storage and 16,384 files. These are retained counters and identities, not a replay of record signatures or certification of remaining-run capacity. [Exact observations](peerreview/evidence/retained-ledger-capacity-20260912.json) (`sha256:e61e6d64347f8dde98d08d52464440a1a03104c874526e59a3c6056a453f3ea2`).

**Earlier corrected evidence checkpoint, 2026-09-12 16:34 UTC.** Finalized chain state at block **7,990,697** records **103.320655354 alpha captured**, **0 paid**, **0 pending funding**, and **103.320655354 alpha outstanding liability and accounted escrow**. Epoch308 captured positive emission from both pools; epoch309 committed and finalized two funded entitlement roots. Each operator database contains eight epoch309 leaves. The previous report incorrectly generalized epoch310’s zero result to the whole run and missed these transactions. The [correction bundle](peerreview/evidence/short-run-corrections-20260912.json) (`sha256:e6372f5bd2bb5de16d4e911f8a1b2a254e8ac3c237c0ef3b7eb3c98f1f98e5f7`) contains the raw receipts, logs, finalized header and pinned state calls.

**Runtime checkpoint, 16:16 UTC.** Both validators had exhausted five restarts before shutdown. The retained fleet was stopped at 16:14 UTC for repairs and qualification; all 32 recorded kernel processes were absent at follow-up and the systemd cgroup was removed. Old executable-path identity checks prevented graceful child cleanup, so systemd used forced termination. This is a preserved cleanup failure, with [stop evidence](../../temp/sn-full-finalization-20260912/runtime-stop/result.json); it is not a graceful-shutdown pass. No fleet or soak is running at this checkpoint.

**Historical checkpoint, 10:17 UTC (superseded above).** Source309 traffic completed cleanly for both operators at the full **274,877,906,944 bytes (256 GiB) per operator** target, with zero pending messages and joined cleanup. Epoch310's staged operator deposits finalized at blocks `7,988,380` and `7,988,381` and were reused. Its signed artifact was created at `09:45:36 UTC` with zero leaves and zero root; the commit was skipped. Finalization was scheduled at block `7,988,824` and the finalization transaction reached finalized block `7,988,827` with hash `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487`. Validator2 is absent after five supervisor restart attempts and has the retained native intent-lineage gap; validator1 remains alive. A CLI51 candidate containing the provisional startup and observer gap handling is built, committed at `fcbf51ea2bea4dbea3a37a78e75fd3dc7018b1ec`, pushed, and atomically staged at the shared image path without signaling live processes. The run remains provisional with `final_acceptance=false`; strict certification is not established.

**Epoch310-only settlement outcome, 10:17 UTC.** The owned node finalized the epoch boundary at block `7,988,674`. The signed artifact `sha256:e628a82f39c94146b6c25245f58a262664c38e9b5c264d77653882c6c2cdb478` was created at `09:45:36 UTC` with `leaves=[]`, `total_usage_bytes=0`, `eligible_usage_bytes=0`, and an all-zero payout root. The commit worker recorded repeated skipped attempts with `no payout leaves for epoch (nothing to commit; pool total carries)`. The scheduled finalize block was `7,988,824`; the finalization transaction was included and finalized at block `7,988,827` with hash `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487`. [Finalization receipt](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-finalization-receipt.json). This zero result concerns epoch310. Source309 produced its own nonempty epoch309 roots and funded entitlements, documented below.

**Earlier scenario checkpoint, 07:05 UTC.** The retained epoch scenario had **15/17 assertions passing** with `final_acceptance=false`; its two failed assertions, validator receipts, source307 deposit failures, and zero captured/paid state remain preserved in the original result.

**Runtime correction deployed, 04:28 UTC.** Validator2 had rejected native 1403 with `compact head EMA epoch jumped` because its retained score store was at 1401. CLI48 now permits the validated provisional runtime to fold the next actual head measurement once and follow genuine signed settlement history across skipped rounds. Combined focused regressions passed in 2.246s, including the 1401/302 → 1404/306 gap, strict rejection and altered/missing history. The source was committed, pulled and pushed as `424712326ea430ff2fca4998f984880fd3fae2ea`. Only the two validators restarted; the supervisor, other 31 fleet processes and both source305 SDKs kept their generations. Both new validators resumed new proof trails and cleanly deferred closed-settlement native 1403; their generations remained healthy through 04:42 UTC. All three native 1403 signed inputs and the prior applied V2 intent remain byte-identical. At that checkpoint new native commitment/application remained pending; later native1404/1405 evidence is recorded below. [Focused result](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/lineage-provisional-gap-fix.json), [actual deployment](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/result.json), [retained bytes](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/retained-native1403-and-applied-intent-postcondition.json).

**Historical native1404 findings, 05:10 UTC.** Validator2 encountered stale measurement-cut boundaries at records 55056 and 56663; its existing retries then sealed both original settlement 306 inputs at cuts 7,987,248 / 7,987,268. At 05:10 UTC no native1404 commitment/application was established; later evidence supersedes that status. Validator1 had a fatal upload502 at 04:54 and a fatal initial terminal-publication timeout at 05:03; the supervisor automatically restarted it, now PID4120278 with 4 restarts. Those failures remain recorded. Focused runtime corrections are in progress; the 07:15 payout opportunity remains conditional. [Actual native 1404 events](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1404-watch/events.jsonl).

**Earlier evidence snapshot.** The controller observation at 2026-09-12T04:46:49.54055505Z recorded all 33 fleet processes healthy, matching contract code, conservation holding, captured emission **0 alpha-rao** and paid claims **0 alpha-rao**. Later validator2 restart exhaustion and the terminal epoch310 outcome supersede its process-health snapshot. [Earlier observation](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1404-watch/current-goal-observation-20260912T0448.json).

| Required outcome | Current result | Evidence |
| --- | --- | --- |
| Actual retained epoch scenario | Completed; 15/17 assertions passed; final acceptance false | [Terminal result](runs/ur-subnet-testnet-v1-attempt-4/runs/20260912T024847.322334653Z-epoch/result.json) |
| Both validators finalize and apply native weights | Not achieved for both validators; V2 has native1401/1404/1405 receipts, but V1 lacks a current applied receipt; both later exhausted restarts | [Terminal supervisor state](runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json), [CLI51 staging receipt](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli51-validator-recovery/result.json) |
| Real traffic and advancing validator/operator proofs | Source309 completed 256 GiB per operator and epoch309 has eight leaves per operator; the separate epoch310 artifact has zero usage | [Source309 launcher](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/launcher-result.json), [epoch310 artifact](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-artifact-response.json) |
| Nonempty payout roots and valid demand deposits | Source304/source305 roots and deposit305/deposit306 finalized; epoch309 also has two committed and finalized funded roots | [Six finalized transactions](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/close-root-deposit305-complete-observation.json) |
| Positive captured emission | Achieved later: epoch308 captured 51.653232130 / 51.667423224 alpha; total 103.320655354 | [Finalized correction evidence](peerreview/evidence/short-run-corrections-20260912.json) |
| Funded positive ClaimPaid | Achieved by recovery: all16 claims finalized; 103.320655346 alpha paid across both pools | [Canonical payment receipts and final vault state](peerreview/evidence/epoch309-paid-claims-20260912.json) |
| Final run accounting and final report | Terminal actual outcome recorded; final acceptance false | [Completion evidence map](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/actual-run-completion-map.json) |

**Deployment and limits.** This is actual testnet chain 945, netuid 521. EVM and native RPC use the owned LAN node `192.168.1.162:9944` with request quotas 0. Existing keys, accounts, plan, configuration, journals and fleet identities remain retained.

| Item | Retained value |
| --- | --- |
| Plan hash | `0x6f3d21b4a3881a23da05148a0bb4d21a24990f2955eb073544b9194b080b1d9c` |
| Coordinator proxy | `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8` |
| Settlement vault | `0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e` |
| Reserve sink | `0x376f98bd7c6b334f7f1cb2685E0970a18bfe7d28` |
| Current implementation | `0x40e5abde2bc4ba84d966842cbab98c0c88894aaf` |
| Alpha limits | 6000 repair allowance (5999.806443325 recorded spent);31250 lifetime |
| TAO limits | 180 EVM within200 total |
| Registration limits | 262 registrations;0 new subnets |
| Active software | The fleet and controller are stopped. CLI44/47/49 describe historical process images; CLI51 was staged. A corrected full-finalization candidate is under qualification. |
| Historical CLI51 source | SN `fcbf51ea2bea4dbea3a37a78e75fd3dc7018b1ec`; server `4ff89807638b5c2d3bcbe2ea0f82c77da788eeb7` |

The historical SN changes were committed, pulled/rebased and pushed. Before the 16:14 UTC shutdown, non-validator processes retained their mapped CLI44 image despite replacement of the shared executable path. The old supervisor’s executable-path comparison proved insufficient at shutdown; the forced cleanup and exact process follow-up are recorded above. [Process custody and cleanup requirement](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/mixed-image-custody.json). Final fee totals and any remaining liabilities will be reported from the actual terminal run; approved caps are not statements of zero subsequent cost.

**Recorded spending inputs, updated 2026-09-12.** The existing receipts show 259 finalized registrations against 262 approved. The lifetime signed external-alpha transfer requests total **31,250.000000000 alpha**, exactly the approved lifetime limit. The three previously missing amounts were recovered from their exact signed SCALE extrinsics and authenticated historical call metadata: **3.25**, **2.25**, and **245.000000025 alpha**. All three byte sequences and transaction hashes match their recorded finalized block inclusions; their sum, 250.500000025 alpha, completes the earlier 30,999.499999975 subtotal. This establishes requested transfer amounts; historical source/destination balance deltas have not been replayed. No further external alpha transfer is available within this lifetime allowance. [Exact decoded amounts, transaction hashes and block references](peerreview/evidence/alpha-transfer-exact-amounts-20260912.json).

Twelve source304/source305 close/root/deposit receipts record 0.040246902684860388 TAO in gas. The 305/306 deposits moved 0.921551375 alpha of retained principal. The CLI47 repair actually paid **0.109311663184972897 TAO** for deployment and activation, within its existing 0.8 TAO allocation. The two expired303 cancellation transactions paid **0.000845639910654000 TAO** together. These four exact fees were recovered from the retained receipts and checked against canonical blocks below finalized block 7,991,132; their earlier missing-fee entries are now resolved. [Fee calculations](peerreview/evidence/historical-repair-fees-20260912.json), [canonical block observations](peerreview/evidence/historical-repair-fee-blocks-20260912.json). Full lifetime EVM and native fee accounting remains incomplete. Historical signed fee ceilings, allocated reserves and unknown terminal costs remain distinct from paid totals and current available balances. [Earlier accounting inputs and limitations](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/current-accounting-20260912T0503/current-accounting.md).

**Completed real traffic and settlement.** The first source304 bulk attempt hit its fixed 03:35:36 stop with actual ACK totals248,327,462,912 /253,275,238,400 bytes. Operator1 reported `context canceled`; operator2 reported `context deadline exceeded`; both exited 1 and completed normal SDK cleanup with no pending messages. These results remain partial failures.

After both owners/SDKs joined, a sequential tail used existing credits and the same 16 providers. It acknowledged 26,550,444,032 / 21,602,668,544 additional bytes; both SDKs exited 0 and joined. The tail owner joined at 03:39:20.795073 UTC. Combined bulk traffic was **274,877,906,944 bytes (256GiB) per operator**, with no overlapping consumer ownership and no new tail credit grants. The separate earlier128MiB capture run is retained in its own receipt. Actual server settlement, rather than ACK totals alone, determined deposit amounts. [Exact handoff and original errors](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-consumer/actual-first-to-tail-handoff.json), [combined results](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-consumer/epoch-304-tail-once/launcher-result.json).

Both source304 payout artifacts contain 8 eligible leaves. Both closes landed before the close cutoff. Deposits for epoch305 were **205,051,017 /256,313,850 alpha-rao**, finalized at 7,986,883 / 7,986,884. All six associated transactions were canonically finalized by 03:48:53 UTC:

| Operator | Operation | Finalized block | Transaction |
| --- | --- | ---: | --- |
| 1 | close | 7,986,877 | `0x3515b8f158e877d0fdec85b2af16f0a3f66a42fc4ce606cff6eeb4779d791210` |
| 2 | close | 7,986,877 | `0x23d4e8e44bbb518ba4a9406b9d516f1812fa54969eecaf29b87dbd90c32f58d7` |
| 1 | root | 7,986,880 | `0x6fa42bc0966361dcf3339e66073fd987a82423a144db6183f1572c3b7ac57fbf` |
| 2 | root | 7,986,880 | `0xbbc659ea638688ee1d4a95f6fb9c28da22d87d26dc945a6d81ac02cefd392196` |
| 1 | deposit | 7,986,883 | `0x83de36b625f56938e870054ff42fe995b810ca0866505ea27d3db1eac8301629` |
| 2 | deposit | 7,986,884 | `0x4fd861a212206954d7653371bbcbdd423d884083fc2f8d83a251697b06709a03` |

The expired deposit303 reservations were canceled automatically with same-nonce transactions, finalized at 7,986,880 / 7,986,881. No manual database nonce edits or new funding were used. [Cancellation receipt](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/expired303-cancellations-20260912T034728Z.json).

**Native application.** Validator2's native 1401 transaction `0xd8e13d9efb88d5a7b82abf67bae05a0923803c19b3ce6fa4dea52c6645a0fe43` finalized at 7,986,181. Its local record reports application by 7,986,499, and a single direct finalized-state check at 7,986,519 confirmed UID255's row `[7:65534,8:65535]`. This completed receipt is reused. That vector contains head weights; it does not establish positive pool emission. [Actual row evidence](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli44-native-application-watch/direct-native-applied-row.json).

Both native 1403 attempts have signed inputs for settlement304, at cuts 7,986,856 / 7,986,866, before deposit305 finalized. Validator1 produced an actual compact-cut mismatch for record48613 and then canceled the drained unsigned second-input reservation. Source inspection confirms that advancing to 305 defers a signed 304 input before pool-deposit audits; same-settlement retries may refresh those audits, but cannot rebind this input across epochs. Signed input bytes and the actual error remain preserved. Both replacement validators resumed proof trails and naturally deferred native 1403 before the new gap admission path; native1404 was pending at that earlier checkpoint and subsequently finalized as documented below. [Exact current native events](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/events.jsonl).

**Historical execution and conditional timing.** Real source305 bulk completed **256GiB per operator**, with both targets reached, exits0, normal SDK cleanup, no pending messages and no errors. Its owner joined at 04:40:43.056600 UTC, before the 04:43:36 fixed stop and 04:45:36 close. It used the original private consumer seeds and providers. Actual unpaid fixture-credit shortfalls were274,729,005,747 /275,080,550,653 bytes, bringing available balances to 272 GiB each; these grants are test fixtures, not paid commercial demand. No new chain funding occurred. Capture306 automatically started 04:46:23 after the completed 305 ownership checks and completed 128 MiB per operator; both SDKs and its owner joined with exits0 and no errors by 04:46:38. [Completed source305 evidence](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-consumer/completed-traffic-receipt.json), [capture306 start](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-capture306307-consumer/epoch-306-after-bulk304-once/started.json).

All six source305 close/root/deposit306 transactions are now canonically finalized. The two deposits are 204,495,792 / 255,690,716 alpha-rao, at blocks 7,987,180 / 7,987,181, and each source305 artifact has 8 eligible leaves. [Completed source305 settlement and capture306 traffic](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/close-root-deposit306-and-capture-complete-observation.json). This funding is available before fresh native 1404's snapshot, projected around 04:53:36 UTC; actual native admission remains pending. A timely native 1404 commitment could apply around 06:05:36; positive emission captured in settlement 307 could become payable shortly after 07:15:36, followed by paid-claim finality. Missing that emission boundary could move the payout window to 08:15:36 or later. **These are conditional opportunities, not completion deadlines.** Capture306 traffic is complete;307 remains queued behind its completed owner. The local native 1404 watcher is queued for 04:50 through 06:20 UTC. [Outcome watcher](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/launch-receipt.json), [capture ownership dependency](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-capture306307-consumer/capture306-add-actual305-predecessor.json).

**Actual repairs and preserved failures.** The coordinator correction permits at most one alpha-rao of transfer residue at the intermediary reserve while retaining balance-decrease, principal and overspend checks. Its implementation deployment finalized at 7,986,577 and activation at 7,986,580; custody identity stayed equal. The later successful deposit305 transactions demonstrate the corrected path executing on chain. [Finalized repair](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/actual-finalized-repair-checkpoint.json), [bounded campaign budget](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/reviewed-campaign-budget.json).

The original deposit303 deadline was missed. The later small-traffic deposit304 failed the native transfer minimum before nonce reservation. Earlier CLI45/46 controller admission attempts failed before signing; canonical owner-role lookup and reuse of verified historical deployment completion corrected those failures. CLI47 repair and resume completed successfully. Earlier validator restart, admission, stream, replay and settlement failures remain in their original records and in the [preserved report history](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/report-history-through-20260912T0354.md). The user-reported transient internet outage has no established exact interval and is not used to explain unrelated errors.

**Coverage limits.** The shortened exercise did not complete the full producer/aggregate release gates, public replay, fresh admission/proof certification, pruning or fault campaign. Those obligations are active again under the full finalization request. The pruning mismatch, interrupted intervals, proof errors and partial traffic runs remain failures or incomplete evidence. Positive capture and funded payment are now established. Both-validator completion, complete lifetime accounting and strict campaign certification remain outstanding.

The earlier report closed its observation at 2026-09-12T10:17:00Z; that was not the fleet shutdown time. The corrected checkpoints above include later chain events and the actual 16:14 UTC stop. Strict certification and final acceptance remain unachieved.


## On-chain evidence and independent verification

The chain-facing facts below are included so a peer reviewer can distinguish an EVM receipt from an off-chain artifact. The run used chain ID **945** (`0x3b1`), netuid **521**, and the owned JSON-RPC endpoint `http://192.168.1.162:9944`. The coordinator proxy was `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8`; the settlement vault was `0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e`; and the reserve sink was `0x376f98bd7c6b334f7f1cb2685E0970a18bfe7d28`.

A read-only receipt and log capture was taken from that endpoint at `2026-09-12T13:01:23Z`, at node head `7,989,652`: [on-chain receipt/log snapshot](peerreview/evidence/onchain-receipts-20260912.json) (`sha256:4e491a2d543e13c17ba1fdf44fb766bc2b3b838e9f4b2ea650ec3e47a7da59de`). Every listed EVM receipt returned `status=0x1`. The local run observations additionally mark the source304/source305 receipts and the final epoch310 receipt as canonical/finalized. A reviewer can reproduce any receipt with:

```bash
curl -sS http://192.168.1.162:9944 \
  -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["<TX_HASH>"]}'
```

The receipt proves inclusion and successful execution. Canonical/finalized status is taken from the corresponding run receipt and finalized-state observation, rather than inferred only from the EVM receipt. The Solidity event definitions used to decode logs are in [`evm/abi/STCoordinator.abi.json`](../evm/abi/STCoordinator.abi.json), [`evm/abi/STSettlementVault.abi.json`](../evm/abi/STSettlementVault.abi.json), and [`evm/abi/STReserveSink.abi.json`](../evm/abi/STReserveSink.abi.json).

### Repair deployment and activation

The coordinator rounding repair was actually deployed and activated. Both transactions succeeded on the coordinator deployment path and are recorded in [actual-finalized-repair-checkpoint.json](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/actual-finalized-repair-checkpoint.json):

| Action | Transaction | Included block | Block hash |
| --- | --- | ---: | --- |
| `repair.coordinator-rounding.deploy` | `0x7459e328f865d14a7818757a57edd41655b35ba6a1fff0d06c0cfb0dd74c22ca` | 7,986,577 | `0x6a4a4dfafa4792e294c14f3b0545e8dc920917798be3291b574e1aa10ab6dda8` |
| `repair.coordinator-rounding.activate` | `0x4255f99d94abecfb2a48804090890059d070ca8e66821038667e873fb20fc7b2` | 7,986,580 | `0xeb101aeb317fee5b3f27c44540b63c4f4eeb46882ead59654e36b37dd57f22f9` |

The checkpoint records implementation `0x40e5abde2bc4ba84d966842cbab98c0c88894aaf` and runtime code hash `0xc8837dcf6ebb607277f140677c73ad91f3359ef25bfda17e578ad52e2d4f5179`. This repair enabled the later successful deposits. Positive payment was completed through the separate client-identity recovery recorded above.

### Epoch 304 traffic, roots, and epoch 305 deposits

Source304 produced two nonempty off-chain artifacts with eight eligible leaves each. The six corresponding EVM transactions all returned `status=0x1` and are marked canonical/finalized in [close-root-deposit305-complete-observation.json](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/close-root-deposit305-complete-observation.json):

| Operator | Operation | Transaction | Included block |
| --- | --- | --- | ---: |
| 1 | close | `0x3515b8f158e877d0fdec85b2af16f0a3f66a42fc4ce606cff6eeb4779d791210` | 7,986,877 |
| 2 | close | `0x23d4e8e44bbb518ba4a9406b9d516f1812fa54969eecaf29b87dbd90c32f58d7` | 7,986,877 |
| 1 | root | `0x6fa42bc0966361dcf3339e66073fd987a82423a144db6183f1572c3b7ac57fbf` | 7,986,880 |
| 2 | root | `0xbbc659ea638688ee1d4a95f6fb9c28da22d87d26dc945a6d81ac02cefd392196` | 7,986,880 |
| 1 | deposit for epoch 305 | `0x83de36b625f56938e870054ff42fe995b810ca0866505ea27d3db1eac8301629` | 7,986,883 |
| 2 | deposit for epoch 305 | `0x4fd861a212206954d7653371bbcbdd423d884083fc2f8d83a251697b06709a03` | 7,986,884 |

The decoded deposit amounts were **205,051,017** and **256,313,850 alpha-rao**, respectively. Expired epoch303 intents were canceled by successful same-nonce replacement transactions `0xfacbbdbafb2bbd42a22df6f6fd3eb800203b9218180688869c76c771fea1873d` at block 7,986,880 and `0xb525d616f4aff2108ef8669e1073cf1ada1c68991369b73293d38f0315f0c72a` at block 7,986,881; the cancellation observation is [here](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/expired303-cancellations-20260912T034728Z.json).

### Epoch 305 traffic, roots, and epoch 306 deposits

Source305 also produced two nonempty artifacts with eight eligible leaves each. The six canonical/finalized transactions are recorded in [close-root-deposit306-and-capture-complete-observation.json](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/close-root-deposit306-and-capture-complete-observation.json):

| Operator | Operation | Transaction | Included block |
| --- | --- | --- | ---: |
| 1 | close | `0x2beb0b5f6b2aa44c98f9c9b443af8dc7171e34e6906d7a7ae1770d285ca1d404` | 7,987,177 |
| 2 | close | `0x09a2907ecef69c779a3cb84fd71785488e6f80490c9a80fa162ce8b7269f3736` | 7,987,178 |
| 1 | deposit for epoch 306 | `0x90854876441e51523f1f7f68055e6d18a36e379e7296c6ec6f7f1e0569221137` | 7,987,180 |
| 1 | root | `0x1a0474c1f2a2ab2bde029663fdeeb346da7e1790346b62c693a761dfe645f4cd` | 7,987,180 |
| 2 | deposit for epoch 306 | `0xb299d8668610be14d44c4e2f8f461a3fc8fc68ee3ec35fabfe31e94ed32ad97f` | 7,987,181 |
| 2 | root | `0x970bae6a9c2562ce6b73707d762260043b6693e7778b984b16b63f5e8dcee132` | 7,987,181 |

The decoded epoch306 deposit amounts were **204,495,792** and **255,690,716 alpha-rao**. These receipts prove that the repaired close/root/deposit path executed successfully; they do not prove validator application or a positive emission.

### Positive epoch308 capture and funded epoch309 roots

The [correction bundle](peerreview/evidence/short-run-corrections-20260912.json) records six successful receipts and their logs. Both `EmissionCaptured` events identify epoch308. Both `EntitlementFinalized` events identify epoch309 and expire at block **7,991,073**.

| Operator | Event | Alpha-rao | Block | Transaction |
| --- | --- | ---: | ---: | --- |
| 1 | Epoch308 emission captured | 51,653,232,130 | 7,988,077 | `0x0407e92951701d5d37ca7fe8806405b059b134189631c31e5313f6a8f31da9be` |
| 2 | Epoch308 emission captured | 51,667,423,224 | 7,988,077 | `0x1dc4a426cd9c680bbee8c24667583be770368f233182d4ff9281dd57eea3a075` |
| 1 | Epoch309 root committed | — | 7,988,380 | `0x2c2aebec65a93ff31733d2379accc69325a05259fd433f3b7bd665ecdb47ed02` |
| 2 | Epoch309 root committed | — | 7,988,380 | `0x7c4238ca0f7066412842792db04ab1f0c2bb3a96529de6600ee7e6a2c3ffabf9` |
| 1 | Epoch309 entitlement finalized | 51,653,232,130 | 7,988,527 | `0x6255bbc039103fa0a23e9a6d52adf50097fd5d3cc44333389018e7b90204fde3` |
| 2 | Epoch309 entitlement finalized | 51,667,423,224 | 7,988,527 | `0x7347ec8d8a7cc81fc8bcb33f1447864c3cef75b33a2e2b8a403475ddd009479a` |

The corresponding EVM block hashes are `0x1dcd02a11b09acb82db3754bbce56f9f725ce51e6622b8f9cf45c01b3bc411b1` (7,988,077), `0x0c33d6772165ae5d43bf92be9271e803ed256def1fca4f2e5c051fb92a8beb04` (7,988,380), and `0xed1317804b8787d5d9f9bb27a0c54ea613de380c49bb5c5a27f602afe683b240` (7,988,527).

| Operator | Epoch309 artifact hash | Committed payout root | Database leaf count |
| --- | --- | --- | ---: |
| 1 | `sha256:c5fe8a8e28157016987ed64171d62a63f98bf4007a55065a9f0f6f02204fecef` | `0x9bbab5462796712a3af4dafc5b32086842d7a72eca97b263154c8dbc8f32262b` | 8 |
| 2 | `sha256:e55d709f29f57ef75e4880e3e358f180720f8f4f58706e8aa5f6de676170e707` | `0xdc36a9982c1d53e90fdf5bb431fe92610a505dc92da07096bd3126d3748a6376` | 8 |

The complete signed epoch309 artifacts were subsequently retrieved through each operator’s unauthenticated public API and are included as [operator1 artifact](peerreview/evidence/epoch309-operator-1-artifact.json) and [operator2 artifact](peerreview/evidence/epoch309-operator-2-artifact.json). Their signed usage totals are275,079,276,039 /275,079,276,042 bytes; each contains8 providers and8 leaves. Terra verified both exact responses with `payoutartifact.Decode`, including canonical bytes, signatures, content hashes, deployment identity, deterministic provider/leaf/usage reconstruction and the committed payout roots. The recovered signers are `0xd255a91442f8c4f72513bcf6a211dd7d4ae3bcdc` and `0xbe37b9d50617dd03082df18700b2d8ca7bed95a9`. This verifies the signatures against the declared signers; historical signer authorization remains part of the outstanding full evidence-graph replay. At finalized EVM block7,990,697, hash `0x8f6ad48eb003878747eea7fb4050070d20fb84ecfc304e7541282c41ceae5178`, the pinned vault calls return `totalCaptured=103320655354`, `totalPaid=0`, `pendingFunding=0`, `outstandingLiability=103320655354`, and `escrowAccounted=103320655354`. This earlier checkpoint proves positive capture and funded liability before the later payment recovery.

### Epoch 310 staged deposits and terminal finalization

The staged deposits reused for epoch310 are visible directly in coordinator logs and receipts. Operator 1’s deposit transaction `0x148d1227bc74f99e50113ea484b4624233f2452b9c6cf5fb3aed85d208c55319` succeeded in block **7,988,380**, and operator 2’s `0x3d9b916104fe217455a6aa629f50c680cd647d5fd3f22c6415f64f0eb9aa3f59` succeeded in block **7,988,381**. Each decoded `Deposit` event has epoch `310`, amount **204,950,031 alpha-rao**, policy hash `0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`, with authorization nonces 6 and 4. The raw coordinator query is included in the [on-chain snapshot](peerreview/evidence/onchain-receipts-20260912.json).

The authoritative epoch310 artifact was created with:

- artifact hash `sha256:e628a82f39c94146b6c25245f58a262664c38e9b5c264d77653882c6c2cdb478`;
- `leaves=[]`, `total_usage_bytes=0`, and `eligible_usage_bytes=0`;
- payout root `0x0000000000000000000000000000000000000000000000000000000000000000`; and
- commit status `skipped` because there were no payout leaves and the pool total carried.

Finalization was scheduled for block 7,988,824 and executed successfully by transaction `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487` in block **7,988,827**, block hash `0xda2e1edb9d55312bc1cc93e0334ff531a335bef4d098c1466511b9a3a19496ca`. The finalized database row and receipt are [epoch310-finalization-receipt.json](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-finalization-receipt.json).

A direct `eth_getLogs` query over the settlement vault from blocks 7,988,674 through 7,988,827 returned two `EmissionCaptured` logs for epoch310 with amount **0** and two `RootMissed` logs with carried amount **0**, all attached to the finalization range. It returned no `ClaimPaid` or `Claimed` event. This confirms zero capture for epoch310 and the zero-leaf artifact; it does not negate the earlier positive epoch308 capture or epoch309 entitlement; the raw result is in the snapshot under `epoch310_settlement_logs_query`.

### Native validator evidence

The native weight transaction is a Substrate extrinsic and therefore is not expected in `eth_getTransactionReceipt`. Validator2’s native 1401 extrinsic is `0xd8e13d9efb88d5a7b82abf67bae05a0923803c19b3ce6fa4dea52c6645a0fe43`; the retained evidence records finality at block **7,986,181**, application at block **7,986,499**, and a direct finalized-state check at block **7,986,519** showing UID255 row `[7:65534,8:65535]`: [direct-native-applied-row.json](../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli44-native-application-watch/direct-native-applied-row.json). That vector is head-weight evidence only and does not establish positive pool emission. Validator2 later exhausted its five restart attempts; the terminal supervisor state is [here](runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json).

Later V2 receipts were also omitted from the earlier report. The correction bundle independently matches both extrinsic hashes to their canonical native blocks using BLAKE2b-256 of the encoded extrinsic. Terra independently read the exact application-block headers and historical weights from the owned node; both rows match the retained validator records. [Historical storage verification](peerreview/evidence/native1404-1405-historical-weights.json) (`sha256:6373f2a7d8b2d6c8c1dcedcf28d0dba5ee6bb9d339d8fdcd9d66d7faaf102592`).

| Native epoch | Extrinsic | Finalized inclusion | Verified application block | Verified UID/value row |
| --- | --- | ---: | ---: | --- |
| 1404 | `0xe58e4563e7a965baae577136a39933dafc3b67e2d29d7cab81b724772977526f` | 7,987,311 | 7,987,601 | `[3:65517,4:65535,7:24071,8:32094]` |
| 1405 | `0x5e0da9793f3d430ab32d8e202ca0c3c0e1b6c10d82a81d716fd3728b384d66fb` | 7,987,774 | 7,988,052 | `[7:65534,8:65535]` |

Native inclusion hashes are `0xaecc6a2ca7c93415923622512d2407c11aedbc098fc3007c2b37551de3bda78f` and `0xb30e815261b9f719e299aaf2b9ec75b022730462ed33cf992f037dc0d0384fab`; recorded application hashes are `0xde5db784894d846de13bc8ada9fe76fc5e58cc31574e24fdd8adca2610e83f1a` and `0x188cb56a3ba368c81137941fb561c372b9f591b221e4dab3de76c39813b42de2`, respectively. Native and EVM block hashes are distinct and must be queried through the corresponding API.

### Epoch309 paid claims

All16 receipts in the [payment evidence bundle](peerreview/evidence/epoch309-paid-claims-20260912.json) have `status=0x1`, successful `Claimed` and positive `ClaimPaid` events, matching canonical EVM block hashes, and inclusion before expiry block7,991,073. Relayer nonces2 through9 were used sequentially for each operator. No new funding, wallet reassignment, queue reset or full-fleet restart was needed.

| Operator | Miner | Alpha-rao paid | Finalized block | Transaction |
| --- | --- | ---: | ---: | --- |
| 1 | miner-108 | 6,492,811,278 | 7,990,845 | `0xa4c7c71e07d342927e65415e4ee52ab87cd7860ebd685c617bb6456d300e1b01` |
| 2 | miner-15 | 6,463,594,645 | 7,990,855 | `0x7f1c3bc2b613a0f861a0b9d8501a862682470b6abc9e7288e405954189db4ea2` |
| 1 | miner-153 | 6,270,702,380 | 7,990,858 | `0xc3946bd6da75d006f9f14553dff8304568a470dd6279da56ea56ad8d609f9752` |
| 2 | miner-173 | 6,484,261,614 | 7,990,861 | `0xebdef59c41ff2d5bc0d966ffda8081d039fa23ff8c1f1a9c2e647ee5428f4c4e` |
| 1 | miner-242 | 6,544,464,510 | 7,990,864 | `0x50ae812cf312373f8da7a90da632371141cec38480477e183baf05db0556bcd3` |
| 2 | miner-262 | 6,396,426,995 | 7,990,867 | `0x8c7ca43fe4c8cd7b340eea16d7d7f6753286632d2f8979aa8ee97819924640ae` |
| 1 | miner-441 | 6,513,472,571 | 7,990,870 | `0x70b39656ef3e140ab3b9d659861df704a79cb5d542c2bfcb6f309ac2403a3c50` |
| 2 | miner-350 | 6,396,426,995 | 7,990,873 | `0xc4247c1d16c815549fc6656d89155503182a728a25c6fe3f2095d855045c3237` |
| 1 | miner-467 | 6,477,315,309 | 7,990,876 | `0x0adddac416007e09922d9ee7cc56583ce5c767a88554b55a25794050dba1b2f2` |
| 2 | miner-399 | 6,572,096,234 | 7,990,879 | `0xe1d5e0c564b37b649ca995ab9dd42222def0d7f295d045c84b6f0edbbdb6c9fe` |
| 1 | miner-489 | 6,487,645,955 | 7,990,882 | `0xe2076806e1a5fc997a136bbf475d4e7e2a1cb384bce59ef089864adcf70d8d22` |
| 2 | miner-432 | 6,546,262,522 | 7,990,885 | `0xe4beca7f56be327575410b49d14f32b5b3e55761435bbe5f911090a2274442c4` |
| 1 | miner-779 | 6,446,323,369 | 7,990,888 | `0x51cc2a5d20a71c16338611f242711287a05ddc7f597d60d4b02d62c749bfddfb` |
| 2 | miner-902 | 6,463,594,645 | 7,990,891 | `0xa4a0f09b0ec33ac349d715c84d030568f28a7371819b8ebbd17a43f31f1f1a9e` |
| 1 | miner-817 | 6,420,496,753 | 7,990,894 | `0xb2b30aa8e1cc97ffd00a0337b1702f9cb8d49ddd139d2db6dea8160ea468b173` |
| 2 | miner-930 | 6,344,759,571 | 7,990,897 | `0x078d894339066e170b8ed8d35b26be196e58bd126a4a4a9e72900842a3001fa7` |

The pinned post-payment checkpoint is block7,990,906, hash `0xf11d60fc8e9c13fc7b51f0b848e7e98df51bd492a6cb8ee2d53e1ee369f46607`. Its8 alpha-rao liability equals its accounted escrow; there is no pending funding, and the contract reports conservation holding. The 16 claim fees total49,882,627,183,941,739 wei, computed from each receipt’s `gasUsed * effectiveGasPrice`. These payment results do not convert the earlier15/17 scenario result or unrun full release gates into passes.

## Peer-review conclusions

The evidence supports the following conclusions:

1. Real traffic ran and produced valid nonempty source304/source305 artifacts, and their close/root/deposit transactions succeeded on chain.
2. The coordinator repair was deployed and activated on chain and the repaired deposit path was exercised.
3. The terminal epoch310 boundary was finalized on chain, but its authoritative artifact had no eligible usage, no leaves, an all-zero root, and no commit.
4. Epoch308 captured **103.320655354 alpha**. All16 funded epoch309 claims subsequently finalized and paid **103.320655346 alpha**;8 alpha-rao of rounding residue remains accounted for, and conservation holds.
5. Both-validator native completion and strict release certification remain incomplete. The correct peer-review verdict is **provisional/incomplete**, with `final_acceptance=false`.

The report does not treat successful transaction inclusion, off-chain artifact creation, or fixture-credit traffic as proof of a paid production settlement. The preserved run artifacts and the raw RPC snapshot are the review record.
