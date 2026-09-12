# sim-latency peer-review final report

This is the terminal report for the actual testnet exercise. The shortened [active plan](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/sn/FINALIZE-ACTIVE.md) governs the run scope. Full release certification and deferred preparation gates were not run; waived or unrun gates are not passes. **final_acceptance=false**. This report preserves failed and partial attempts for independent peer review.

**Terminal checkpoint, 10:17 UTC.** Source309 traffic completed cleanly for both operators at the full **274,877,906,944 bytes (256 GiB) per operator** target, with zero pending messages and joined cleanup. Epoch310's staged operator deposits finalized at blocks `7,988,380` and `7,988,381` and were reused. Its signed artifact was created at `09:45:36 UTC` with zero leaves and zero root; the commit was skipped. Finalization was scheduled at block `7,988,824` and the finalization transaction reached finalized block `7,988,827` with hash `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487`. Validator2 is absent after five supervisor restart attempts and has the retained native intent-lineage gap; validator1 remains alive. A CLI51 candidate containing the provisional startup and observer gap handling is built, committed at `fcbf51ea2bea4dbea3a37a78e75fd3dc7018b1ec`, pushed, and atomically staged at the shared image path without signaling live processes. The run remains provisional with `final_acceptance=false`; strict certification is not established.

**Epoch310 settlement outcome, 10:17 UTC.** The owned node finalized the epoch boundary at block `7,988,674`. The signed artifact `sha256:e628a82f39c94146b6c25245f58a262664c38e9b5c264d77653882c6c2cdb478` was created at `09:45:36 UTC` with `leaves=[]`, `total_usage_bytes=0`, `eligible_usage_bytes=0`, and an all-zero payout root. The commit worker recorded repeated skipped attempts with `no payout leaves for epoch (nothing to commit; pool total carries)`. The scheduled finalize block was `7,988,824`; the finalization transaction was included and finalized at block `7,988,827` with hash `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487`. [Finalization receipt](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-finalization-receipt.json). The artifact and its signed response are actual evidence; the source309 ACK receipt did not create a payable settlement.

**Earlier checkpoint, 07:05 UTC (superseded by the terminal checkpoint above).** The retained epoch scenario had **15/17 assertions passing** with `final_acceptance=false`; its two failed assertions, validator receipts, source307 deposit failures, and zero captured/paid state remain preserved in the original result.

**Runtime correction deployed, 04:28 UTC.** Validator2 had rejected native 1403 with `compact head EMA epoch jumped` because its retained score store was at 1401. CLI48 now permits the validated provisional runtime to fold the next actual head measurement once and follow genuine signed settlement history across skipped rounds. Combined focused regressions passed in 2.246s, including the 1401/302 → 1404/306 gap, strict rejection and altered/missing history. The source was committed, pulled and pushed as `424712326ea430ff2fca4998f984880fd3fae2ea`. Only the two validators restarted; the supervisor, other 31 fleet processes and both source305 SDKs kept their generations. Both new validators resumed new proof trails and cleanly deferred closed-settlement native 1403; their generations remained healthy through 04:42 UTC. All three native 1403 signed inputs and the prior applied V2 intent remain byte-identical. New native commitment/application remains pending. [Focused result](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/lineage-provisional-gap-fix.json), [actual deployment](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/result.json), [retained bytes](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/retained-native1403-and-applied-intent-postcondition.json).

**Live native 1404 findings, 05:10 UTC.** Validator2 encountered stale measurement-cut boundaries at records 55056 and 56663; its existing retries then sealed both original settlement 306 inputs at cuts 7,987,248 / 7,987,268. No native 1404 commitment/application is established yet. Validator1 had a fatal upload502 at 04:54 and a fatal initial terminal-publication timeout at 05:03; the supervisor automatically restarted it, now PID4120278 with 4 restarts. Those failures remain recorded. Focused runtime corrections are in progress; the 07:15 payout opportunity remains conditional. [Actual native 1404 events](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1404-watch/events.jsonl).

**Earlier evidence snapshot.** The controller observation at 2026-09-12T04:46:49.54055505Z recorded all 33 fleet processes healthy, matching contract code, conservation holding, captured emission **0 alpha-rao** and paid claims **0 alpha-rao**. Later validator2 restart exhaustion and the terminal epoch310 outcome supersede its process-health snapshot. [Earlier observation](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1404-watch/current-goal-observation-20260912T0448.json).

| Required outcome | Current result | Evidence |
| --- | --- | --- |
| Actual retained epoch scenario | Completed; 15/17 assertions passed; final acceptance false | [Terminal result](../../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260912T024847.322334653Z-epoch/result.json) |
| Both validators finalize and apply native weights | Not achieved; validator2 is absent after restart budget5/5 and validator1 has no current local applied receipt | [Terminal supervisor state](../../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json), [CLI51 staging receipt](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli51-validator-recovery/result.json) |
| Real traffic and advancing validator/operator proofs | Source309 bulk completed 256 GiB per operator cleanly; proof rows continued, but the epoch310 settlement artifact had zero usage | [Source309 launcher](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/launcher-result.json), [epoch310 artifact](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-artifact-response.json) |
| Nonempty payout roots and valid demand deposits | Both source304/source305 roots and deposit305/deposit306 transactions finalized | [Six finalized transactions](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/close-root-deposit305-complete-observation.json) |
| Positive captured emission | Not achieved; observed total 0 | [Terminal scenario result](../../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260912T024847.322334653Z-epoch/result.json) |
| Funded positive ClaimPaid | Not achieved; observed paid 0 | Same terminal result; successful deferred-credit claims alone will not count |
| Final run accounting and final report | Terminal actual outcome recorded; final acceptance false | [Completion evidence map](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/actual-run-completion-map.json) |

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
| Active software | Validator1 and the last validator2 generation remain mapped to the retained CLI49 image; CLI47 controller/scenario and CLI44 fleet processes remain live. CLI51 is staged at the shared path but inactive. |
| Committed source | SN `fcbf51ea2bea4dbea3a37a78e75fd3dc7018b1ec`; server `4ff89807638b5c2d3bcbe2ea0f82c77da788eeb7` |

The SN changes were committed, pulled/rebased and pushed. The CLI48 shared executable applies to any later incidental worker restart; the currently retained non-validator processes keep their mapped CLI44 image. At eventual shutdown, those old mappings require the recorded external cleanup using exact process identities; the old supervisor's executable-path comparison alone is insufficient. [Process custody and cleanup requirement](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli48-validator-only-recovery/mixed-image-custody.json). Final fee totals and any remaining liabilities will be reported from the actual terminal run; approved caps are not statements of zero subsequent cost.

**Recorded spending inputs.** The existing receipts show 259 finalized registrations against 262 approved. The known lifetime external-alpha transfer subtotal is 30,999.499999975 alpha; three original exact amounts remain unknown, so this is not a complete lifetime total. Twelve source304/source305 close/root/deposit receipts record 0.040246902684860388 TAO in gas. The 305/306 deposits moved 0.921551375 alpha of retained principal. The CLI47 repair used a 0.8 TAO allocation inside the existing campaign reserve, but its exact paid gas was not retained in the inspected result. Historical signed fee ceilings, allocated reserves and unknown terminal/native/cancellation costs are kept distinct from paid totals and current available balances. [Accounting inputs and limitations](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/current-accounting-20260912T0503/current-accounting.md).

**Completed real traffic and settlement.** The first source304 bulk attempt hit its fixed 03:35:36 stop with actual ACK totals248,327,462,912 /253,275,238,400 bytes. Operator1 reported `context canceled`; operator2 reported `context deadline exceeded`; both exited 1 and completed normal SDK cleanup with no pending messages. These results remain partial failures.

After both owners/SDKs joined, a sequential tail used existing credits and the same 16 providers. It acknowledged 26,550,444,032 / 21,602,668,544 additional bytes; both SDKs exited 0 and joined. The tail owner joined at 03:39:20.795073 UTC. Combined bulk traffic was **274,877,906,944 bytes (256GiB) per operator**, with no overlapping consumer ownership and no new tail credit grants. The separate earlier128MiB capture run is retained in its own receipt. Actual server settlement, rather than ACK totals alone, determined deposit amounts. [Exact handoff and original errors](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-consumer/actual-first-to-tail-handoff.json), [combined results](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-consumer/epoch-304-tail-once/launcher-result.json).

Both source304 payout artifacts contain 8 eligible leaves. Both closes landed before the close cutoff. Deposits for epoch305 were **205,051,017 /256,313,850 alpha-rao**, finalized at 7,986,883 / 7,986,884. All six associated transactions were canonically finalized by 03:48:53 UTC:

| Operator | Operation | Finalized block | Transaction |
| --- | --- | ---: | --- |
| 1 | close | 7,986,877 | `0x3515b8f158e877d0fdec85b2af16f0a3f66a42fc4ce606cff6eeb4779d791210` |
| 2 | close | 7,986,877 | `0x23d4e8e44bbb518ba4a9406b9d516f1812fa54969eecaf29b87dbd90c32f58d7` |
| 1 | root | 7,986,880 | `0x6fa42bc0966361dcf3339e66073fd987a82423a144db6183f1572c3b7ac57fbf` |
| 2 | root | 7,986,880 | `0xbbc659ea638688ee1d4a95f6fb9c28da22d87d26dc945a6d81ac02cefd392196` |
| 1 | deposit | 7,986,883 | `0x83de36b625f56938e870054ff42fe995b810ca0866505ea27d3db1eac8301629` |
| 2 | deposit | 7,986,884 | `0x4fd861a212206954d7653371bbcbdd423d884083fc2f8d83a251697b06709a03` |

The expired deposit303 reservations were canceled automatically with same-nonce transactions, finalized at 7,986,880 / 7,986,881. No manual database nonce edits or new funding were used. [Cancellation receipt](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/expired303-cancellations-20260912T034728Z.json).

**Native application.** Validator2's native 1401 transaction `0xd8e13d9efb88d5a7b82abf67bae05a0923803c19b3ce6fa4dea52c6645a0fe43` finalized at 7,986,181. Its local record reports application by 7,986,499, and a single direct finalized-state check at 7,986,519 confirmed UID255's row `[7:65534,8:65535]`. This completed receipt is reused. That vector contains head weights; it does not establish positive pool emission. [Actual row evidence](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli44-native-application-watch/direct-native-applied-row.json).

Both native 1403 attempts have signed inputs for settlement304, at cuts 7,986,856 / 7,986,866, before deposit305 finalized. Validator1 produced an actual compact-cut mismatch for record48613 and then canceled the drained unsigned second-input reservation. Source inspection confirms that advancing to 305 defers a signed 304 input before pool-deposit audits; same-settlement retries may refresh those audits, but cannot rebind this input across epochs. Signed input bytes and the actual error remain preserved. Both replacement validators resumed proof trails and naturally deferred native 1403 before the new gap admission path; live native 1404 execution remains pending. [Exact current native events](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/events.jsonl).

**Current execution and conditional timing.** Real source305 bulk completed **256GiB per operator**, with both targets reached, exits0, normal SDK cleanup, no pending messages and no errors. Its owner joined at 04:40:43.056600 UTC, before the 04:43:36 fixed stop and 04:45:36 close. It used the original private consumer seeds and providers. Actual unpaid fixture-credit shortfalls were274,729,005,747 /275,080,550,653 bytes, bringing available balances to 272 GiB each; these grants are test fixtures, not paid commercial demand. No new chain funding occurred. Capture306 automatically started 04:46:23 after the completed 305 ownership checks and completed 128 MiB per operator; both SDKs and its owner joined with exits0 and no errors by 04:46:38. [Completed source305 evidence](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-consumer/completed-traffic-receipt.json), [capture306 start](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-capture306307-consumer/epoch-306-after-bulk304-once/started.json).

All six source305 close/root/deposit306 transactions are now canonically finalized. The two deposits are 204,495,792 / 255,690,716 alpha-rao, at blocks 7,987,180 / 7,987,181, and each source305 artifact has 8 eligible leaves. [Completed source305 settlement and capture306 traffic](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/close-root-deposit306-and-capture-complete-observation.json). This funding is available before fresh native 1404's snapshot, projected around 04:53:36 UTC; actual native admission remains pending. A timely native 1404 commitment could apply around 06:05:36; positive emission captured in settlement 307 could become payable shortly after 07:15:36, followed by paid-claim finality. Missing that emission boundary could move the payout window to 08:15:36 or later. **These are conditional opportunities, not completion deadlines.** Capture306 traffic is complete;307 remains queued behind its completed owner. The local native 1404 watcher is queued for 04:50 through 06:20 UTC. [Outcome watcher](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/launch-receipt.json), [capture ownership dependency](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-capture306307-consumer/capture306-add-actual305-predecessor.json).

**Actual repairs and preserved failures.** The coordinator correction permits at most one alpha-rao of transfer residue at the intermediary reserve while retaining balance-decrease, principal and overspend checks. Its implementation deployment finalized at 7,986,577 and activation at 7,986,580; custody identity stayed equal. The later successful deposit305 transactions demonstrate the corrected path executing on chain. [Finalized repair](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/actual-finalized-repair-checkpoint.json), [bounded campaign budget](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/reviewed-campaign-budget.json).

The original deposit303 deadline was missed. The later small-traffic deposit304 failed the native transfer minimum before nonce reservation. Earlier CLI45/46 controller admission attempts failed before signing; canonical owner-role lookup and reuse of verified historical deployment completion corrected those failures. CLI47 repair and resume completed successfully. Earlier validator restart, admission, stream, replay and settlement failures remain in their original records and in the [preserved report history](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-native1403-watch/report-history-through-20260912T0354.md). The user-reported transient internet outage has no established exact interval and is not used to explain unrelated errors.

**Coverage limits.** Full producer/aggregate release gates, public replay, fresh admission/proof certification, pruning and fault campaign coverage are deferred by the active plan. The original pruning mismatch, failed/interrupted observation intervals, individual proof errors and partial traffic runs are not converted into passes. Full relayed coverage remains subject to the original relay horizon; ordinary production validation and payouts continue. Final completion still requires the missing actual native, capture, payment and scenario outcomes, plus this report's final accounting and delivery.

Terminal run closed 2026-09-12T10:17:00+00:00. Strict certification and final acceptance were not achieved.


## On-chain evidence and independent verification

The chain-facing facts below are included so a peer reviewer can distinguish an EVM receipt from an off-chain artifact. The run used chain ID **945** (`0x3b1`), netuid **521**, and the owned JSON-RPC endpoint `http://192.168.1.162:9944`. The coordinator proxy was `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8`; the settlement vault was `0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e`; and the reserve sink was `0x376f98bd7c6b334f7f1cb2685E0970a18bfe7d28`.

A read-only receipt and log capture was taken from that endpoint at `2026-09-12T13:01:23Z`, at node head `7,989,652`: [on-chain receipt/log snapshot](evidence/onchain-receipts-20260912.json) (`sha256:379718184bdc5f9f0121f16642e854930cac5b2a271208877e054fba56d6266b`). Every listed EVM receipt returned `status=0x1`. The local run observations additionally mark the source304/source305 receipts and the final epoch310 receipt as canonical/finalized. A reviewer can reproduce any receipt with:

```bash
curl -sS http://192.168.1.162:9944 \
  -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["<TX_HASH>"]}'
```

The receipt proves inclusion and successful execution. Canonical/finalized status is taken from the corresponding run receipt and finalized-state observation, rather than inferred only from the EVM receipt. The Solidity event definitions used to decode logs are in [`evm/abi/STCoordinator.abi.json`](../../evm/abi/STCoordinator.abi.json), [`evm/abi/STSettlementVault.abi.json`](../../evm/abi/STSettlementVault.abi.json), and [`evm/abi/STReserveSink.abi.json`](../../evm/abi/STReserveSink.abi.json).

### Repair deployment and activation

The coordinator rounding repair was actually deployed and activated. Both transactions succeeded on the coordinator deployment path and are recorded in [actual-finalized-repair-checkpoint.json](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source302-outcome/deposit303-runtime-rounding-fix/actual-finalized-repair-checkpoint.json):

| Action | Transaction | Included block | Block hash |
| --- | --- | ---: | --- |
| `repair.coordinator-rounding.deploy` | `0x7459e328f865d14a7818757a57edd41655b35ba6a1fff0d06c0cfb0dd74c22ca` | 7,986,577 | `0x6a4a4dfafa4792e294c14f3b0545e8dc920917798be3291b574e1aa10ab6dda8` |
| `repair.coordinator-rounding.activate` | `0x4255f99d94abecfb2a48804090890059d070ca8e66821038667e873fb20fc7b2` | 7,986,580 | `0xeb101aeb317fee5b3f27c44540b63c4f4eeb46882ead59654e36b37dd57f22f9` |

The checkpoint records implementation `0x40e5abde2bc4ba84d966842cbab98c0c88894aaf` and runtime code hash `0xc8837dcf6ebb607277f140677c73ad91f3359ef25bfda17e578ad52e2d4f5179`. This is the repair that made the later successful deposits possible; it did not make the final run a positive-payout run.

### Epoch 304 traffic, roots, and epoch 305 deposits

Source304 produced two nonempty off-chain artifacts with eight eligible leaves each. The six corresponding EVM transactions all returned `status=0x1` and are marked canonical/finalized in [close-root-deposit305-complete-observation.json](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/close-root-deposit305-complete-observation.json):

| Operator | Operation | Transaction | Included block |
| --- | --- | --- | ---: |
| 1 | close | `0x3515b8f158e877d0fdec85b2af16f0a3f66a42fc4ce606cff6eeb4779d791210` | 7,986,877 |
| 2 | close | `0x23d4e8e44bbb518ba4a9406b9d516f1812fa54969eecaf29b87dbd90c32f58d7` | 7,986,877 |
| 1 | root | `0x6fa42bc0966361dcf3339e66073fd987a82423a144db6183f1572c3b7ac57fbf` | 7,986,880 |
| 2 | root | `0xbbc659ea638688ee1d4a95f6fb9c28da22d87d26dc945a6d81ac02cefd392196` | 7,986,880 |
| 1 | deposit for epoch 305 | `0x83de36b625f56938e870054ff42fe995b810ca0866505ea27d3db1eac8301629` | 7,986,883 |
| 2 | deposit for epoch 305 | `0x4fd861a212206954d7653371bbcbdd423d884083fc2f8d83a251697b06709a03` | 7,986,884 |

The decoded deposit amounts were **205,051,017** and **256,313,850 alpha-rao**, respectively. Expired epoch303 intents were canceled by successful same-nonce replacement transactions `0xfacbbdbafb2bbd42a22df6f6fd3eb800203b9218180688869c76c771fea1873d` at block 7,986,880 and `0xb525d616f4aff2108ef8669e1073cf1ada1c68991369b73293d38f0315f0c72a` at block 7,986,881; the cancellation observation is [here](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source304-outcome/expired303-cancellations-20260912T034728Z.json).

### Epoch 305 traffic, roots, and epoch 306 deposits

Source305 also produced two nonempty artifacts with eight eligible leaves each. The six canonical/finalized transactions are recorded in [close-root-deposit306-and-capture-complete-observation.json](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source305-outcome/close-root-deposit306-and-capture-complete-observation.json):

| Operator | Operation | Transaction | Included block |
| --- | --- | --- | ---: |
| 1 | close | `0x2beb0b5f6b2aa44c98f9c9b443af8dc7171e34e6906d7a7ae1770d285ca1d404` | 7,987,177 |
| 2 | close | `0x09a2907ecef69c779a3cb84fd71785488e6f80490c9a80fa162ce8b7269f3736` | 7,987,178 |
| 1 | deposit for epoch 306 | `0x90854876441e51523f1f7f68055e6d18a36e379e7296c6ec6f7f1e0569221137` | 7,987,180 |
| 1 | root | `0x1a0474c1f2a2ab2bde029663fdeeb346da7e1790346b62c693a761dfe645f4cd` | 7,987,180 |
| 2 | deposit for epoch 306 | `0xb299d8668610be14d44c4e2f8f461a3fc8fc68ee3ec35fabfe31e94ed32ad97f` | 7,987,181 |
| 2 | root | `0x970bae6a9c2562ce6b73707d762260043b6693e7778b984b16b63f5e8dcee132` | 7,987,181 |

The decoded epoch306 deposit amounts were **204,495,792** and **255,690,716 alpha-rao**. These receipts prove that the repaired close/root/deposit path executed successfully; they do not prove validator application or a positive emission.

### Epoch 310 staged deposits and terminal finalization

The staged deposits reused for epoch310 are visible directly in coordinator logs and receipts. Operator 1’s deposit transaction `0x148d1227bc74f99e50113ea484b4624233f2452b9c6cf5fb3aed85d208c55319` succeeded in block **7,988,380**, and operator 2’s `0x3d9b916104fe217455a6aa629f50c680cd647d5fd3f22c6415f64f0eb9aa3f59` succeeded in block **7,988,381**. Each decoded `Deposit` event has epoch `310`, amount **204,950,031 alpha-rao**, policy hash `0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`, with authorization nonces 6 and 4. The raw coordinator query is included in the [on-chain snapshot](evidence/onchain-receipts-20260912.json).

The authoritative epoch310 artifact was created with:

- artifact hash `sha256:e628a82f39c94146b6c25245f58a262664c38e9b5c264d77653882c6c2cdb478`;
- `leaves=[]`, `total_usage_bytes=0`, and `eligible_usage_bytes=0`;
- payout root `0x0000000000000000000000000000000000000000000000000000000000000000`; and
- commit status `skipped` because there were no payout leaves and the pool total carried.

Finalization was scheduled for block 7,988,824 and executed successfully by transaction `0xbe6ff669419f7896bbe5285c7e927eaef9c3a1533529c2d9294e69aa46b73487` in block **7,988,827**, block hash `0xda2e1edb9d55312bc1cc93e0334ff531a335bef4d098c1466511b9a3a19496ca`. The finalized database row and receipt are [epoch310-finalization-receipt.json](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-source309-consumer/epoch-309-once/epoch310-finalization-receipt.json).

A direct `eth_getLogs` query over the settlement vault from blocks 7,988,674 through 7,988,827 returned two `EmissionCaptured` logs for epoch310 with amount **0** and two `RootMissed` logs with carried amount **0**, all attached to the finalization range. It returned no `ClaimPaid` or `Claimed` event. This is the on-chain counterpart to `captured_rao=0`, `paid_rao=0`, and the zero-leaf artifact; the raw result is in the snapshot under `epoch310_settlement_logs_query`.

### Native validator evidence

The native weight transaction is a Substrate extrinsic and therefore is not expected in `eth_getTransactionReceipt`. Validator2’s native 1401 extrinsic is `0xd8e13d9efb88d5a7b82abf67bae05a0923803c19b3ce6fa4dea52c6645a0fe43`; the retained evidence records finality at block **7,986,181**, application at block **7,986,499**, and a direct finalized-state check at block **7,986,519** showing UID255 row `[7:65534,8:65535]`: [direct-native-applied-row.json](../../../temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/run-first-20260911T0743/actual-cli44-native-application-watch/direct-native-applied-row.json). That vector is head-weight evidence only and does not establish positive pool emission. Validator2 later exhausted its five restart attempts; the terminal supervisor state is [here](../../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json).

## Peer-review conclusions

The evidence supports the following conclusions:

1. Real traffic ran and produced valid nonempty source304/source305 artifacts, and their close/root/deposit transactions succeeded on chain.
2. The coordinator repair was deployed and activated on chain and the repaired deposit path was exercised.
3. The terminal epoch310 boundary was finalized on chain, but its authoritative artifact had no eligible usage, no leaves, an all-zero root, and no commit.
4. The final run captured and paid zero alpha-rao. No positive `ClaimPaid` was observed.
5. Both-validator native completion, positive emission, funded claim, and strict release certification were not achieved. The correct peer-review verdict is **provisional/incomplete**, with `final_acceptance=false`.

The report does not treat successful transaction inclusion, off-chain artifact creation, or fixture-credit traffic as proof of a paid production settlement. The preserved run artifacts and the raw RPC snapshot are the review record.
