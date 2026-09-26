# R47 launch readiness — read-only receipt

Reviewed runnable R46 source **cf0a86befbefa76597c0f6dd2dc4641400b29275**, its `FINALIZE.md`, R46 owner result/report and harness. No transactions, CLI mutation, service changes or live-state edits were performed. Exact observations, failure IDs and evidence hashes are in [receipt.json](receipt.json). Future commands are in [commands.md](commands.md); none were executed.

## Priority 1: choose the intended next interval

**The existing supervisor can support another provisional release interval without stopping.** Missing V1 intent, stale V2 intent, missing governance drill, incomplete native rewards/decision evidence, inherited lifecycle exception and failing actor/sample assertions do not independently prohibit this workload path. Run the admitted R46 driver with `scenario --name release-1.0 --provisional-resume --apply` and the exact retained plan. The CLI authenticates R46's sealed failure/invalidation and contiguous ancestry, opens generation47 automatically, reuses eligible preparation, and signs a new five-complete-epoch boundary. Do not manually reset campaign or journal files.

**This is not an acceptance candidate yet.** The retained supervisor PID3780072 is active with 33 healthy reported children; R46 owner is failed/MainPID0. V1 has no generation1 intent. V2 remains applied native1661/settlement614/finalized8079823, with no history. Both live validator executable build records identify **6100394b04fc3386b03870b8bdcc9a6abb5614a7**. Merely replacing the scenario driver does not deploy cf0's validator cache fix or fill native history.

Provisional native warmup is explicitly skipped (`scenario_native_warmup_v2.go:88`). Native absence is retained as observation/failing evidence. Classified steering-attempt/continuity, TLS/packet/close/exit-gap timeouts and stale-contract findings may continue; unknown classes, unauthenticated warnings and I/O failures cannot borrow that deferral (`scenario_process_log_continuation.go:23`). Governance startup is waived only for provisional release. This does not turn any R46 failure into PASS.

The five-epoch window is 5×300 blocks plus the 150-block terminal offset and initial alignment. The owner derives fresh epochs; do not reuse631–635. cf0 can seal an immutable failed result after complete interval/fault/lifecycle cleanup when an accepted RootMissed/invalid tier or retained blocking TLS/exit-gap makes a pass impossible. **Stale intents alone do not trigger this early sealing**; mutable/missing prerequisites can keep polling to the ordinary timeout. Hard faults or lost authority can still stop earlier.

## Priority 2: fresh validator generation is currently source-blocked

Current `policy-rollover/handoff.json` is activated generation1, cutoff604/first-full605. cf0 can plan a separate generation and publish four bounded activation actions, provision separate client identities and stage configs while the old topology runs. Planning itself writes immutable local approval files; apply broadcasts transactions. Neither was performed here.

However, final activation writes generation2 to the **same fixed `handoff.json`** (`policy_rollover_command_v2.go:428`). `writeRuntimeEvidenceSetupV2` calls the no-overwrite `validator.WriteReleaseEvidenceV2File`. Existing generation1 has different bytes. Thus **a second activation cannot complete through this code path, even after stopping**. Do not publish/spend first: qualify a narrow authenticated successor handoff before using that route. Preserve the first handoff and its signed journal custody; do not unlink it manually. The `--rollover-source-role` route preserves the active generation and is not a native-history reset.

After that code is qualified, the minimal route is: choose a fresh generation and current/future activation epoch from actual LAN schedule; capture/review the exact source-plan/journal/four-member consent/gas-and-lifetime-cap plan; apply the same plan to publish and stage while supervisor stays live; then officially stop, apply that **same exact approved plan** to activate, and resume retained topology with the new binary. The irreversible stop requirement begins immediately before final activation: source requires a stopped supervisor and both validator states stopped (`policy_rollover_command_v2.go:404–413`). Source selection, relay cursor and runtime-manifest readers must all authenticate the successor. A missed epoch is not repaired by changing an existing immutable plan.

Worker-only deployment can already use official `stop` then provisional retained `resume`; it preserves setup/state and copies the admitted binary. It does **not** repair V1/V2 native-history gaps. Strict history-adoption is an alternative only if authentic original history meets its checks; it also requires stopped topology/both validators and an exact first-native-epoch request. No supported hot repair was found that manufactures missing intent/history while retaining the live supervisor.

## Priority 3: immediate launch checks versus deferred acceptance

The next driver must reauthenticate unchanged config/policy/plan, sealed R46 result and invalidation, journal prefix and writer ownership, process manifest/PIDs/executable, approved spend and any signed/pending transaction reconciliation. The original R46 preflight hardcodes R45→R46: do not reuse it unchanged. A new receipt must checkpoint R46→47. CLI admission remains authoritative; this review is not a launch PASS.

All RPC stays on `192.168.1.162:9944`, `independent_rpc=false`. At08:55:40UTC the finalized native head was8089222. Fleet renewal7 covers settlement628–659; recompute whether the newly signed five-epoch window/tail fits before requesting more renewal. Relay v6's old forecast ends8070107; elapsed time is advisory only for exact provisional continuation, while finite funded slots/source bounds remain strict. Retain2048 funded slots (798 retained+1250 new) and existing ceilings. A capacity/validity failure needs an exact bounded repair, not a blanket budget increase.

Full historical audit, final packaging and diagnostic completion are not extra provisional-start prerequisites under `FINALIZE.md:20–29`. Diagnostic success is not acceptance. The blocked SN25 merge **b1d316d9** is outside this run source.

## R46 failures: code is landed; proof is still required

All **86/182** failures and **182 open anomalies** remain historical failures; exact IDs are retained in the JSON.

| R46 group | Count | Landed change / required fresh evidence |
|---|---:|---|
| Native decisions/rewards |14|Source selection is fixed; fresh authenticated V1/V2 applied decisions, common-epoch opposing/restored head choices, rewards/deposit/vector replay are still missing. Cache speedup is not history repair.|
| Actor/vector evidence |59|Bounded current operator surfaces16747c3c, approved validator source43861c23, RPC same-height8c1522cc, error chronologyc8932972/API separation8cb399e2/transient readsba3d6f02 are landed. Need new actor samples and exact source/replay outcomes; derived vector failures are not59 independent causes.|
| Incomplete epoch634 payout source |1|Restart readiness96d463da/3147116c and validator ASSIGN cacheab37201b are landed. Need fresh signed proof progress through all four streams after restart and nonempty finalized payout roots. R46 NO2 RootMissed cannot recover; Server zero-leaf regression is not a production behavior fix.|
| Cohort quality separation |1|Current bounded surfaces are fixed; new control/fault cohorts must actually differ.|
| Claim finality/tier exclusivity |2|R46 miner881 claim finalized after its signed terminal; retain that result. Fresh interval needs claims finalized inside its own terminal/source scope.|
| Process-log completion/publication |2|Narrow provisional continuation6411b0d9 is landed; new clean scoped process evidence is still required. Connect prefetch fix is adjacent and not proved to fix R46 exit gaps; pinned R45 dependency graph does not include it.|
| Cancellation/publication |2|Terminal-failure sealing3e8edc52 and chronology/diagnostic fixes are landed; require owner-managed terminal cleanup/result/publication, preserving failed verdicts.|
| Governance |1|Still unexecuted. Provisional startup waiver is not completed drill evidence.|
| Lifecycle exception |3|Retain exact R44-LC-1 scope; no widened waiver or claimed prune/reregister. Fresh causal coverage remains missing for strict closure.|
| Aggregate anomaly ledger |1|Closes only from underlying justified findings/evidence, never a rewritten diagnostic summary.|

R46 observed epochs631–635 and crossed terminal8086324, then sealed failed after graceful cancellation. Later diagnostics, old-object readback, cache tests and claims outside that window cannot retroactively make it pass. Production soak remains a separate uncompleted phase: three complete360-block epochs plus180-block terminal offset after the approved transition.
