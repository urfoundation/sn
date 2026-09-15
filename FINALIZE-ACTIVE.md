# Testnet execution plan

Updated 2026-09-15. The user has requested full finalization and fixes for
previously ignored failures, flakiness and issues exposed by the shortened run.
The full functional requirements in [FINALIZE.md](FINALIZE.md) govern completion.
The user's 2026-09-15 instruction requires recovery by patching and retaining
incremental progress. The [harness recovery policy](sim-testnet/README.md#incremental-recovery-and-acceptance)
supersedes historical full-restart and blanket three-confirmation requirements.
The user explicitly confirmed SN testnet finalization under `sn/FINALIZE.md`;
all qualification and the current report at `sn/sim-testnet/FINAL-2.md` concern
this simulator and its runtime dependencies. Other simulation references were
mistaken and do not add work to this goal.
The earlier shortened-run instructions below are retained as historical scope
for those attempts, whose `final_acceptance=false` results remain unchanged.

Reports are numbered at the user's request: `sim-testnet/FINAL.md` remains
report 1, `sim-testnet/FINAL-2.md` covers this full finalization, and later
finalizations use `FINAL-3.md`, `FINAL-4.md`, and so on. Preserve each earlier
report and its underlying evidence. The [report 2 closure table](sim-testnet/FINAL-2.md)
tracks the first report's peer-review findings. A compatibility exception,
historical replay, pending check or artifact-only assertion is not proof that
the next run met an on-chain target.

The user explicitly directed execution against the real chain limits after
the peer review: retain the root-controlled
`max_allowed_validators=64`. Lowering it to 56 is not a prerequisite for this
testnet run. The existing compatibility policy already requires exactly 64;
prove the 200-head topology against actual UID occupancy, permits and native
selection under that value. Report the difference from the whitepaper's ≤56
target explicitly, without treating a permit limit as a fixed UID partition.

Current work:

1. The retained campaign, signer-authority and gate corrections are composed
   and published; their focused qualification and required failure confirmations
   are complete. Preserve the existing deployment, wallets, approvals and journals.
2. Use Terra (`gpt-5.6-terra`, reasoning effort `max`) for all tests and gate
   execution. Use Astra (`gpt-6-astra`, reasoning effort `max`) to diagnose and
   fix failures and flakiness, then return corrected source to Terra for reruns.
3. Complete producer and aggregate coverage using valid retained phase results
   plus failed, missing or patch-affected checks. Collect independent failures
   in a batch; preserve completed phases when a gate is interrupted.
   Retain earlier confirmations and every failed or interrupted result. Record
   accepted composition separately from each gate invocation's actual exit.
4. Complete the required real release campaign and production soak, then
   reconcile on-chain outcomes, LAN-node replay, the final report and shutdown.
   Reuse valid completed evidence; unrun, failed and waived checks are not passes.

Retain the approved 6,000-alpha repair allowance, 37,250-alpha lifetime limit,
205 EVM within 225 total TAO, 262 registrations and zero new subnets. Use
`192.168.1.162:9944` without RPC rate limits. Full acceptance remains pending.

Latest checkpoint, 2026-09-15 20:19 UTC: the renewal completed at 19:09:41,
with all **1,212 actions** verified across **202 fleets**. The corrected relay
continuation was adopted at 20:04:13 with zero chain transactions, retaining
all required work through block **8,021,242**. Final restart history selects
native epoch **1,477** and is saved with its exact hash. All four temporary
services stopped and joined cleanly. Actual strict resume is running from
the retained state, using the admitted c5 executable. Its first attempt
refused a missing private temporary directory; creating the required
directories also fixed the adjacent campaign command's preparation. The retry
started at **20:14:29 UTC**, with no source, approval or plan change.
Continue directly to `release-candidate` after successful startup; no new
test suite, preparation gate, renewal or repair is queued. The soak has not
started and final acceptance remains false.
[Completed renewal and continuation evidence](sim-testnet/FINAL-2.md).

Storage relief completed at **20:44:30 UTC**: the existing Go build cache was
copied to the user-mounted `/mnt/data` volume and verified with no checksum
or metadata differences before removing the original copy. Its old path is
preserved by a link. This frees about **56 GiB** on `/`, which now has about
**78 GiB** available. Active simulator state, binaries and command captures
remain at their existing paths; startup continues under the same owner.

Previous checkpoint, 2026-09-15 17:48 UTC: producer and aggregate coverage are
**accepted by composition**. The succession fixture correction at `93f6d35`
passes its nine affected normal tests and 29 children. The complete capture
race passes all 378 tests and 140 children in 479.526 seconds, within the
unchanged 600-second limit. The old-fixture control reproduces the expected
work-bound failure. All actual exits, thirteen-repository source comparisons,
binary comparisons and cleanup checks pass. These are local test results;
the original failed and interrupted gates retain their recorded outcomes.
The full cohort ran with four processors on a lighter host load than the
previous simultaneous full gates; that execution condition is retained.
[Replacement qualification receipts](sim-testnet/peerreview/evidence/FINAL-2-incremental-capture-20260915/README.md).

The unchanged 39 producer phases and unchanged aggregate scopes retain their
complete earlier receipts. No full gate restart or extra confirmation is
required. The approved exact Q09ac683b fleet renewal apply started at
17:27:28 UTC using the existing canonical c5 executable and LAN RPC node.
It stopped at 17:33:09 on the host disk check, before any transaction; campaign
state, executable and release lock comparisons were unchanged. Removing two
generated fixture trees from a terminal test owner recovered 3,681,648,640
bytes and restored free space to 22.28 GiB. All 61 parent evidence artifacts
remained unchanged. The same exact plan resumed in a fresh capture at
17:46:51 UTC. One journal writer owns this operation; retain the exact saved
plan and any persisted transactions on interruption. Completion and new
finalized actions are not yet claimed. The soak remains stopped, with strict continuation and
the actual release campaign next. Keep canonical/source c5 frozen while this
native owner runs; local test and report publication can be prepared separately.

Previous checkpoint, 2026-09-15 17:12 UTC: recover the interrupted c5 gate
attempts through composed coverage. Root verified both outer processes and
their test descendants absent; the retained logs record stop143, with no
final outer exit or final source receipt. Producer retained 38 passing joins,
the capture failure and one interrupted database phase. Aggregate retained
five passing joins and two interrupted race bodies. Reuse unchanged phases
from the earlier completed 330ba512 gates. No full gate restart is planned.
The test-only succession fixture repair is frozen at `93f6d35`; Terra is
building the corrected normal/race and old-fixture normal executables for
the three admitted bodies. Qualification remains pending.

Actual read-only renewal planning passed at 17:07:03 UTC for epochs 393–424:
plan `0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`,
all 202 fleets, 1,212 new actions, bounded at 9.09 EVM TAO plus 0.606 native TAO.
Expired predecessor bindings require no revocations. State, executable and
lock comparisons passed unchanged; no chain transaction was submitted.
The exact saved plan and apply command are ready. Complete producer coverage
precedes apply; reuse the existing canonical c5 runtime while its source and
approved production inputs remain unchanged. The 18 native preparation passes,
approved allowance and finalized reserve repair remain retained.

Previous checkpoint, 2026-09-15 16:15 UTC: the published `c5db71a` producer's
capture phase passed normally in 120.920 seconds and timed out under race at
600.377 seconds. The active succession test repeatedly builds and authenticates
a full 1,000-provider plan; Astra is correcting that test fixture while Terra
keeps both full gates collecting their remaining results. The fix is not yet
qualified. Retain unaffected completed phases, the 18 passing native preparation
checks, adopted plan and finalized reserve repair. The next qualification is
the corrected capture scope and affected consumers, not two restarted full
gates. Renewal is unsubmitted; the soak remains stopped. Publish patches after
live source owners release their checkouts, and reuse the admitted runtime
build if its production inputs remain unchanged.

Previous checkpoint, 2026-09-15 15:05 UTC: the user approved the lifetime
increase to 205 EVM within 225 total TAO for one fleet renewal capped at
13.13 EVM plus 0.606 native TAO. Alpha limits remain unchanged. The vault
setting is published at `9651a13062af2fd25dcd9e98db8c8871d148d114`.
Native plan `0x922e280318f33cb20f5b15082bb6329890e9d8778f1effabf8baae521a57f4ea`
is adopted: setup preparation passed all nine hard checks and authenticated
all 3,449 carried actions, without dispatching an action. The complete renewal
preview passed for 202 fleets and 2,020 new actions within those ceilings.
Existing keeper and oracle balances cover that maximum; an extra funding
transfer is not a prerequisite. Its F388/T419 window is diagnostic and must
not be adopted as the final window.

The approved 6,000-alpha repair finalized in native block 8,009,634. The later
complete registered-stake census at block 8,010,632 shows a 65.5997247163%
reserve share, above the 65% target. Do not repeat the repair. Both temporary
artifact APIs and both RPC proxies stopped and joined cleanly. Full strict
resume preparation passed all 18 hard checks at 13:58:20 UTC, including both
validator namespaces, host readiness and runtime inputs. All 1,000 fleet
records and 3,449 carried actions were authenticated, with unchanged recorded
campaign state. The soak remains stopped.

On source `330ba512`, the complete producer closed with 39 passing phases and
one capture-race timeout; its final source checks passed. The complete aggregate
closed at 15:05 UTC with all 25 phases, cleanup and final source checks passing.
The two-test-file correction `03d529b` removes repeated whole-script scans.
Its 30 affected tests pass normally and under race, and the two defect controls
each complete three fresh passing processes in both modes. The actual 376-test
capture race, including 140 declared subtests, passes in 567.382 seconds within
its original 600-second limit. Both causal controls reproduce the old scan
failure. Existing Go verification reuses the successful raw results; earlier
compiler-owner and missing-metadata refusals remain recorded. The original
failed producer remains failed.

Production code, scripts, the release lock and approved native plan identity
are unchanged by this correction. Publish it with these report updates, build
one matching CLI and complete the final candidate gates. The producer receives
16 processors and the aggregate eight, retaining four processors per job and
all test limits. Start the CLI alongside the producer and admit the aggregate
when the CLI joins, keeping the combined reservation at or below 24. Reuse the
completed native preparation. Choose the actual renewal window, final relay
end and fresh native history epoch immediately before their native apply/resume
steps. The canonical executable and producer pass still precede chain apply.
[Approval, adopted allowance, renewal bounds and on-chain repair evidence](sim-testnet/peerreview/evidence/FINAL-2-approved-renewal-and-reserve-20260915/README.md).

Historical checkpoint, 2026-09-15 02:38 UTC: the explicit runtime configuration
identity correction is frozen at85c0958. Its normal/race70 matrices each have
69PASS/oneFAIL; both adjacent50 bodies pass. The sole failed root already
authenticated the original repair but then planned with a temporary mock RPC
route, correctly changing resolved inputs and config.render intent. The one-file
fixture correction5a411d2 preserves the approved route and adds a deterministic
changed-route control. Its three fresh normal passes and three fresh race passes
are complete with exact one-test lists, seven events each and all outer/body/
converter/verifier exits0. All13 repository/source observations and per-mode
binaries remain unchanged; the final owner closed02:38:10UTC. The680-entry
portable bundle is sealed with manifest SHA-256
`09713bbd8a396831b926cda1fe1ed758d7fc2b384fdedea1dfade5e186361fe9`.
Retain the69
unchanged passing roots per mode and earlier181/36 scopes. All
four original matrix bodies and their capture-only verifier refusals are saved;
corrected replay of their retained events passes without body reruns.

The original-hashing control closed3 expectedFAIL/2PASS with body1 and
converter/checker/outer0; all source/dependency/binary/diff observations match.
Its failing authentication occurs before the fixture's changed planning section,
so no new causal compile is needed. The updated read-only parser and lock render
also closed0; lock SHA-256 is
`c437900d8cb2d29ff0d363ef629b88ceb8ace35976bd28bfd194a2d8dc6eb639`.
Only SN's production Go hash changes. Idle integrationf9970d6 includes that lock
and the qualified fixture correction; it is not published. Seal retained
evidence and publish. Reuse the accepted
parser for actual read-only setup while the canonical stamped CLI and both full
gates run. The canonical executable and producer PASS still gate approved apply.
Soak stopped; repair unsubmitted; approvals and retained custody unchanged.

Historical checkpoint, 2026-09-15 01:39 UTC: the historical455 lock correction is
qualified and published at eccfae8a5f4176ccaa6099ce5ce555924057f3d3. Its final
CLI built cleanly and passed, SHA-256
`73264ab8365c6cf538390c53ed0a6a71a39d3972e405b916088559b486218e99`.
Actual read-only setup advanced past that check, then exited1 at01:26:11 with
`coordinator repair configured strict domain differs`. All six state files,
binary and lock remained unchanged; no successor plan or transaction resulted.

The public expected runtime455→458 change also changed ConfigHash, while
original signed repair/probe/activation history retains the previous identity.
Astra is implementing an explicit reviewed configuration-identity pin, bound
into the new setup plan while current runtime authority remains exact458.
The shared hash/admission correction and deterministic transition/adjacent
controls are not qualified yet. Preserve action intents, ResolvedInputsHash,
signed originals,1212 finalized renewals,3521 actions, generation3 and nonce34.

Both eccfae8 full gates were intentionally canceled once the native failure
proved a production successor was required. Producer closed143 at01:32:11;
aggregate closed143 at01:33:33. Each passed its first four preflights, was
interrupted during runtime metadata, and admitted zero test phases. All owners
and private process records are gone. The123-entry portable capture manifest
is `b6b1dfc4fa43778945fe3c4d1910d5c69bf2ee96654cc2d6c74ad761ab13ce35`.
These attempts are cancellations, not full-gate passes or executed-test failures.
An added public manifest field requires an updated parser for the next lock
render; overlap that one read-only build with the frozen-source qualification.
Then publish the successor, build the canonical CLI, retry actual setup and run
both full gates. Producer PASS still gates approved apply; soak remains stopped.

Historical checkpoint, 2026-09-15 01:15 UTC: the canonical c572 build passed.
The actual read-only setup then exited1 at00:24:56 because the historical
validator-evidence reader applied current458 lock validation to an authentic
original455 archive. All six campaign-state hashes, binary and lock were
unchanged; no plan or transaction was produced. Astra's narrow historical
admission correction is frozen at3ffc1277d1acd1e21b908c3b2cfd37612602e07b.
Terra's36-root normal and race matrices pass. Both reader/restart roots have
three consecutive normal passes on the same binary and source. Restoring only
the old historical dispatch reproduces both failures while both controls pass;
all source, dependency and binary fences close unchanged. Reuse the
completed181-root runtime qualification within its recorded scope.

Both c572 full gates closed with transport preflight exit28 before any phase or
service owner. Their first three preflights passed; GitHub source downloads
timed out. The exact source fetch recovered at00:31UTC and matched its pinned
hash. TF has clean exact v454/v455/v458 source overrides for the next primary
source loops. No source-fetch feature change or extra transport probes are
needed.

Root reused the c572 executable for the successor lock render, closed0 at
00:54:37 with19 equal before/after observations. Exact YAML SHA-256 is
`5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab`,
included in the qualified successor after integration628f29a. Only SN's production
Go hash changes; all other fields are equal. Publish this coherent successor
and retained evidence, build one matching stamped CLI and launch the full
producer38/aggregate concurrently with16+4CPU and the prepared source overrides.
Review one actual retained-state setup preview; producer PASS gates apply.
Soak stopped, repair unsubmitted, all1212 renewals/3521 action identities,
generation3, original signed probe anchor/nonce34 and approvals preserved.

Historical checkpoint, 2026-09-15 00:09 UTC: the soak remains stopped and the
6,000-alpha repair remains unsubmitted. Fresh LAN observations at 00:03:15 UTC
show a synced node, 16 peers, runtime 458/1/1 and finalized block8,007,358.
Xops main includes both the runtime-pin correction and qualified quota-test
correction at `42bfe0b`; the corrected deployment assertion has not been rerun
on the node. No further rollout confirmation is needed for simulator work.

The composed runtime correction passes 181 selected top-level tests normally
and under race, with required fresh-process confirmations complete. The old
capacity, stale gate inventory and public/paced route causal checks closed with
their expected failures and passing controls. The 13-test production-policy
causal also closed with exactly seven expected failures and six passing
controls. The original simulator41 failure remains recorded
as39PASS/2FAIL; the repaired simulator47 passes both modes. All unchanged client
and typed-prior qualification is reused within its recorded scope.

Both FC908 gates are closed failed: producer36/37 and aggregate24/25. Their
known failures are corrected and qualified; replacement complete gates are
still required. The final exact release-lock SHA-256 is
`d11b2a41ca6e836f9267088f8899c4fb0faf53b78b3b9ca8804bab589cd63e7b`,
committed only in the idle integration at `2b907a4`. Publish the coherent
candidate and report, build one canonical stamped
CLI, and run producer38 with16CPU alongside the aggregate with4CPU. Producer
success gates campaign writes. Preserve all3,521 action identities,1,212
renewals, generation3, original probe anchor and nonce34 in the single successor
setup preview. The repair and real RC/production soak remain outstanding.

Historical checkpoint, 2026-09-14 23:01 UTC: the soak remains stopped and the approved
6,000-alpha repair remains unsubmitted. The operator verified removal of the
nginx limits at 21:01:55 UTC. Fresh direct-LAN observations found a synced node
with 16 peers, runtime 458/1/1 and advancing finalized blocks; the separate
deployment failure was its stale expected-runtime 455 pin. Correction
`446cbdb` passed its complete 30-test module, required confirmations and the
old-pin causal control, and is now pushed to xops main. The corrected deployment
check has not been rerun on the node. No SSH credential or rollout confirmation
is needed to continue simulator preparation.

The FC908 producer closed with 36 passes and one cumulative capture race
timeout. Its typed-prior process correction `907d186` is fully qualified:
26 affected roots in both modes, three fresh race confirmations of the active
boundary and the expected old-script causal failure with passing controls.
Reuse those results. The original FC908 aggregate remains live with 21 phases
passed and no reported failure; its physical source remains fixed.

Runtime-458 source `df98472` includes exact artifact admission, historical455
retention, unpaced LAN artifact checks and a six-identity cache/allowlist bound.
Its 175-root affected qualification is in progress. The readonly CLI build and
combined release-lock render passed. The exact YAML SHA-256 is
`e72b2146a1cbbd59a54b0424a30478aa9a475f8d82369c028c4fe22e8f2d71c2`,
committed only in the idle integration checkout as `151b515`; all contract
hashes remain unchanged. Finish affected qualification, publish the coherent
source, then use a genuine canonical stamped CLI and both complete gates.
Preserve all 3,521 action identities, 1,212 renewals, generation3 custody,
original signed probe anchor and nonce34 during the one successor setup
preview. The earlier runtime455 preview was never executed and is obsolete.
No new plan hash or chain transaction has been produced by this preparation.

Historical checkpoint, 2026-09-14 18:50 UTC: the soak remained stopped and the approved
6,000-alpha reserve repair remains unsubmitted. All 1,212 renewal transactions
across 202 fleets have finalized and passed their native postconditions. Two
setup attempts on the retained approved plan stopped on nginx HTTP 429 before
any new transaction. The qualified unpaced gateway correction is published in
xops main at `d33d417`; deployment is awaiting an SSH identity accepted by the
RPC node. Do not retry setup until the deployed gateway has been corrected.

The `7eab049` producer closed with 34 passing and two failed phases. The probe
test correction passed a strict build, all 215 contract tests and three strict
lint processes. The combined scheduling/source-census successor `713eae3`
passed all 81 affected tests normally and under race, three confirmations of
both failed guards in each mode, and three race confirmations of all four
timeout roots. Both original-defect causal controls reproduced their expected
failures with passing adjacent controls. These completed scopes are retained.
The obsolete aggregate was stopped and joined at 18:49:42 UTC: 14 phases passed,
the same strict Solidity lint failed, and two Connect phases were canceled.
Its outer exit was 143; cancellation did not reach the final source fence.

The successor lock was rendered locally with unchanged production Go and
contract bytes. Its only changes bind the producer scheduling script and both
gateway/node configuration groups. The reviewed YAML SHA-256 is
`76cd7fa7031ee5566301173a94e1a4369ccbc871542de05119b46bbcb1461f35`.
Publish the coherent candidate, build one stamped final CLI, and run the full
37-phase producer and complete aggregate with private services. Producer PASS
precedes traffic; both gates and the full live campaign/production soak remain
required for acceptance. Use one subsequent setup preview and reviewed successor
hash; preserve every completed renewal, receipt, custody record and spend limit.
See [report 2](sim-testnet/FINAL-2.md) and its linked evidence bundles.

Previous checkpoint, 2026-09-13 23:48 UTC: the soak remains stopped. The full
producer on `e3d3539` passed with 36/36 phase joins at 21:44 UTC. Its native
setup apply adopted plan `0xdbeb584008bbdbc6607a49a5118c1c82fdfa18a15ca8fe5b5c8cea9d2775c37a`
but failed at 22:09 UTC during carried-history verification, before action
execution. The journal and both supervisor files are unchanged; the approved
6,000-alpha repair is still unsubmitted. Do not retry the old executable.

The user now requires every actual testnet RPC call to use the LAN node,
including historical and final verification, with no RPC request pacing.
The `7de62c7` correction introduces the explicit owned-node observation profile
and reuses already authenticated historical inputs within an invocation.
Historical receipts retain their original labels. New evidence identifies
`independent_rpc=false`; public-node verification is no longer required for
this run. Read-only LAN probes have returned the tested historical EVM and
native storage successfully. The seven source/routing guards pass normally
and under race; the 33 historical-input checks pass normally. All four roots
active at the original race timeout have completed their three sequential
race confirmations on the same binary. Other affected checks remain pending.

The original aggregate closed with outer exit 1 at 23:14:29 UTC: all 23 phases
joined, with 20 passes and three failures. These were the cumulative simulator
race-package timeout, missing server migration-monitor entries and a context
deadline in the full 1,000-client registration cohort. Its final source check
also refused because canonical SN main advanced during execution. The
[complete original capture](sim-testnet/peerreview/evidence/FINAL-2-aggregate-e3d3539-20260913/README.md)
is retained; do not restart the old gate.
The corrected aggregate partitions all 2,198 simulator roots among five
disjoint owners. Server candidate `0f095a6` includes the published monitor
correction, a real PostgreSQL regression and one synced publication source
shared by both immutable writes. It keeps the original 1,000-client population
and 30-second operation deadline. Terra's qualification remains pending;
the diagnostic passed without reproducing the original deadline, so it is
not evidence that the correction closes that failure.
SN main also advanced independently to `928b7d5`; its provider memory-budget
fix is included in the next integrated candidate. Complete focused checks,
publish one coherent release and start both final gates. Then use one native
setup preview/review and exact hash-bound apply, followed by renewal and launch.
No duplicate preview or standalone full partition diagnostic is required.

Previous checkpoint, 2026-09-13 19:32 UTC: the soak remains stopped. The user
approved one 6,000-alpha replacement for the unsubmitted 3,750-alpha repair,
within 37,250 alpha lifetime. The single-setting vault change is published at
`8b2f481dbe87092d0c1742274712a6f805c1c375` in the independent final candidate
checkout. The final race owner has closed and released its primary-vault
source hold. The primary vault has also adopted that exact approved commit;
no additional repair transaction has been submitted.

The reserve-succession correction's causal check and 30-root normal/race
qualification are complete. The private-fixture correction passed all seven
selected roots normally and under race; its three timeout roots also completed
all three required normal confirmations on unchanged binary bytes. All three
e999 race confirmations have passed; the last closed at 19:05:50 UTC with all
four roots and actual outer/body/replay exits 0. Reuse those closed scopes.

The matching current upstream dependencies require Warp in source admission.
The reviewed `02ba4c7` integration preserves archived lock formats and extends
the current graph to 13 repositories and 16 live modules. Formatting and module
metadata are clean after the three-line Proxy reconciliation published at
`6204ae7df2a9868bbb3a7b61231917a36e4f5c9f`. The exact 24-root normal/race
integration passed at 18:40 and 18:41 UTC. Published candidate `e0a4542` has a
successful stamped bootstrap driver and native release-lock preview. Native
apply refused detached SN tracking metadata without changing the lock.
Corrected clone tracking exposed newer upstream dependency commits; all tested
dependency heads remain clean published ancestors. Astra's two-file correction
retains those exact commits as upstream advances, keeping SN's exact current-main
check and all before/after source checks. Its 12 affected checks passed normally
at 19:24:06 and under race at 19:25:14 UTC, with actual outer/native exits 0 and
unchanged 13-repository observations. Candidate `29be68f` is published. Its
stamped bootstrap driver built at 19:30:10 UTC, and native lock apply succeeded
at 19:32:03 UTC with exact reviewed SHA-256
`ddb22d0e1e525affac5b87cbba29cc70cb8d3e4afb9668507033d6fee907b51c`.
Publish the lock and start Terra's prepared full producer, aggregate and final
driver build concurrently. Preserve the final source snapshot throughout.
The older `cd036cee` executable and unapplied plan are
historical; fresh native planning must use the qualified current candidate and
approved replacement. Producer success can admit that repair while a clean
aggregate continues; both complete gates remain required for acceptance.
The [report and portable evidence](sim-testnet/FINAL-2.md) distinguish actual
passes, historical failures and remaining work.

Previous checkpoint, 2026-09-13 17:32 UTC: the soak remains stopped. The complete
`cd036cee` producer timed out in capture-private normal after 300.104 seconds.
Both superseded gates were stopped and joined: producer outer 143 at 17:19:25
with 23 passed phases, one failed and three interrupted; aggregate outer 143
at 17:19:27 with five passed and two interrupted, without an observed aggregate
product failure. The one-file fixture correction `f6cfd797` preserves all 1,000
miners and actual publication checks. Terra is preparing its exact seven-root
normal/race qualification and three sequential normal confirmations for the
three actual timeout roots. The separate e999 race confirmations continue on
unchanged source and binary; their first pass is complete.

The finalized 17:10 census at block 7,998,093 found 60.4855132935% reserve;
the approved 3,750-alpha repair now projects only 64.9713567471%. Native
read-only setup refused it with exit 1 at 17:14:23 UTC. The approved lifetime
cap remains 35,000 alpha. Correction `6311cb8` permits a provably unsubmitted,
insufficient terminal repair to be replaced through a new plan while preserving
the original history and all started or credited liabilities. Its focused
qualification is being launched independently. Neither correction is deployed,
the original journal is unchanged and no repair transaction has been submitted.

Previous checkpoint, 2026-09-13 16:25 UTC: clean publication `cd036cee` contains
the qualified scheduling correction and reviewed release lock. Its complete
producer and aggregate gates started at 15:57:41 and 16:01:22 UTC with private
test services; both are still running with no observed phase failure. Keep
their complete source and dependency snapshots unchanged. The final native
CLI built with exit 0 and unchanged observations of all 12 repositories;
two read-only setup reconstructions completed with exit 0 and agree on plan
`0x814d362c650dcdb86f1a57e4f266acd789e6be5703c9e65ad753576199bc3358`.
All actions and approved spending ceilings are unchanged. The plan remains
unapplied; the fleet and soak are stopped. The four interrupted roots'
required confirmations continue on their isolated source.

The 16:09:29 UTC complete reserve census at block 7,997,791 found
60.5324340452%; the approved 3,750-alpha repair projected 65.0285723985%.
Fresh native admission must still establish that the repair reaches 65%.
The 60% operating floor does not replace that target. If the approved amount
becomes insufficient, preserve the native refusal and do not exceed the
35,000-alpha lifetime cap. No new transfer has been submitted.

Previous checkpoint, 2026-09-13 15:45 UTC: the producer gate on published
`90f67b1` passed at 14:30:28 UTC with all 36 native phase joins exiting 0.
The aggregate's simulator race complement subsequently timed out after
90 minutes; its separate population race phase passed. The aggregate finished
with exit 1 at 15:40:56 UTC: 21 phases passed, one timed out, and its final source
check passed. All owned processes and private services are joined.
Correction `e99954a`, now pulled and pushed, separates the three full supplement publication roots
from the complement, preserving all 2,186 race roots and existing limits.
The ten affected guards now pass normally and under race; the compiled inventory
confirms all 2,186 roots. Terra continues three sequential race confirmations
for each of the four active timeout roots on unchanged isolated source. Prepare
the refreshed lock, final native CLI and corrected full gates concurrently with
those confirmations. The next full aggregate supplies broad
integration; an extra full development rerun is omitted.

The published `e99954a` bootstrap CLI applied the reviewed release lock with
exit 0 at 15:49:37 UTC. YAML SHA-256 is
`bd5e492077edc01acfa452d67ce1e437deec6d5e1add7ed8eab41dfd722b254f`;
only the protocol-script digest changed. The final CLI and corrected complete
gates follow publication of that lock and the updated evidence. Spending
limits and runtime code are unchanged; this update sent no chain transaction.

Setup began after the producer pass and adopted plan
`0xd4525b8da2da4f786f4990beb2285ac09b42e033c3c7fdd3c45473a7c9336507`
locally. After the aggregate failure, setup was interrupted and joined at
15:00:54 UTC with exit 1 and explicit context cancellation. The post-stop
journal contains no new-plan or reserve-repair entries. Preserve the adopted
plan and prior history for supported recovery; no additional reserve transfer,
renewal, continuation, adoption or fleet launch has occurred.
At the then-current complete census (15:10:42 UTC, block 7,997,497), the reserve was
60.5794990444%; the unapplied approved repair projected 65.0859768022% at that
snapshot. Actual finalized credit and a fresh target census remain required.
See the exact native results and public evidence locators in
[report 2](sim-testnet/FINAL-2.md).

Previous checkpoint, 2026-09-13 12:25 UTC: correction `b78b672` is published.
Its affected 443 capture roots and twelve coverage guards pass normally and
under race. The sole root active at the prior capture timeout, including all
four subtests, completed three fresh sequential race confirmations on identical
binary bytes and unchanged source. The eleven unchanged separately owned
capture roots retain their prior scoped qualification. Both corrected complete
gates still include the full selections.

The `0dcb5c8` producer remains a failed gate (21 passed phases, one failure,
three interrupted); its superseded aggregate was stopped with ten phases passed
and two interrupted, without an observed test failure. Their actual outer exits
are 143 following owned cleanup. All owners are joined and source is released.
Neither incomplete gate is reported as passing. See the portable raw outcomes
and qualification evidence in [report 2](sim-testnet/FINAL-2.md).

The clean, pushed `b78b672` native executable applied exactly the reviewed lock
with exit 0. YAML SHA-256 is
`776f6cf9d57d1c8427ac981f3cf2222ddc1441371c90cbded2789d8ea1299767`;
only the protocol-script digest changed. Final publication is followed by both
complete gates and the final stamped CLI build in parallel, then two fresh
matching setup plans. Producer success can admit the approved reserve repair
while a clean aggregate continues; both gates remain necessary for acceptance.
The historical 11:25 UTC census at block 7,996,371 found 60.7215670220% reserve
share and projected 65.2593333302% after the approved repair. The later
observation above supersedes that moving projection.

Earlier approval, 2026-09-13: the user explicitly approved raising the
lifetime cap from 31,250 to 35,000 alpha for one additional 3,750-alpha reserve
repair. The vault change is committed and pushed at
`d4ea0cbdf49630d8e1afc3e2184858cb58940fd3`. This approval remains valid but
the pending amount is insufficient at the later snapshot above. It does not
authorize exceeding 35,000 alpha lifetime. The per-repair maximum remains
6,000 alpha.

Two actual read-only setup builds on SN `3af4251` completed successfully at
01:48:04 and 01:58:00 UTC, both with plan hash
`0x06116ddc5cdc6945c7d96c8920f2b4cfbaa9a6f04bfd211503cd51803286182a`.
The reviewed revision adds the exact 3,750-alpha repair and authenticated
zero-spend carry of the existing coordinator repair. It retains all prior
positive-alpha actions. Neither plan was applied; the original plan, journal
and signed transaction bytes remain retained. New production corrections
require a refreshed release lock, stamped CLI and fresh bound plans before
application. [Read-only plan review](../temp/sn-approved-alpha-repair-review-20260913/setup-v5-review.json).

The original full producer gate ended with exit 1 at 02:50:47 UTC; the
aggregate ended with exit 1 at 03:23:25 UTC. Their source snapshots remained
clean. Both failed private-service startup, and their actual completed test
failures are being corrected. Jobs joined with exit 143 during cleanup are
recorded as interrupted, not completed test verdicts. [Producer receipt](../temp/sn-final-execution-20260912/runtime/producer-gate/capture/RESULT.json),
[aggregate receipt](../temp/sn-final-execution-20260912/aggregate-gate-prepared-20260912T2254Z/capture/RESULT.json).

The previously pending focused qualifications are now complete on their
recorded immutable sources. The Go 1.26 qualification launcher correction passed its four
regression roots normally and under race. Corrected CRV4 and server artifact
test binaries subsequently exited 0 in both modes, but their captures failed
because expected-outcome files omitted legitimate subtests. Existing offline
replay has now checked both retained streams against corrected exact declarations;
the original failed captures remain unchanged. The isolated PostgreSQL control
reproduced `Permission denied` on the copied mode-0700 initialization directory.
After restoring public fixture permissions, the actual PostgreSQL 18, Redis
and fixture preflight passed with successful owned cleanup at 03:43:02 UTC.
[Corrected service preflight exit](../temp/sn-private-services-qualification-20260913/runtime/preflight-0755/capture/outer.exit).
Service18, monitor14 and cache/provisional23 integration checks passed normally
and under race on SN `ebe70a3` and server `e2358826`. Their previously failed
normal roots have three fresh passing confirmations on the recorded immutable
binaries. [Sealed scoped qualification](../temp/sn-private-services-qualification-20260913/runtime/RESULT-service-monitor-cache-corrected.json)
(`sha256:c98dbe1e98943cf2eb7087e362b4c0b17e08ec08006007d87c21ef2b39d493cf`).
Connect's actual plain-WebSocket resolver bypass is corrected at `3d29e1f`.
Its 15-root integration passed normally and under race; all eight previously
failed roots completed three sequential passes in each mode on the same
recorded binaries. The remaining 18 simulator normal confirmations, both
CRV4 roots in both modes, three private-capture normal confirmations and the
seven-root private-capture race integration also passed. Earlier simulator,
validator, stabi, service, monitor and cache results retain their original
source scope. See the [current qualification evidence](sim-testnet/FINAL.md).

The integrated source includes server `bbfe4296`, Connect `3d29e1f`, SDK
`169d4c2c`, operator-proxy `714f10f0`, proxy `c11c7eb4` and xops `ec84346`.
All 15 live modules passed dependency validation. The xops update leaves all
15 Subtensor infrastructure lock inputs unchanged; the normal infrastructure
gate still covers its affected inputs. The new release-lock YAML hash is
`sha256:ddcd0ec9f44f11c86c22b6b9ff73a31a09c0d0b910e07a1d8b966a8c7af40923`.
The native stamped `5a79b62` renderer applied exactly the reviewed bytes with
exit 0. Runtime, EVM, interface and infrastructure lock fields are unchanged.
The final stamped CLI build and both full gates follow this publication in
parallel, using the existing physical workspace and separate private services.
Completed historical qualification remains reusable within its recorded scope;
the complete current candidate still needs both full-gate results.

The fleet and soak remain stopped. There has been no new reserve transfer,
renewal, relay continuation or live campaign during this preparation. The
critical path is to close the actual failures, publish the composed source and
lock, obtain launch admission, apply the approved reserve repair, then perform
renewal, relay continuation and retained-history adoption before the full run.
Producer success can admit the live campaign while a clean aggregate is still
running, as specified by the complete plan; any actual aggregate failure stops
new mutations. Both full gates are required for final acceptance.

Historical checkpoint, 2026-09-12 23:13 UTC: the full fleet is stopped. Both
operator APIs and temporary payout-recovery proxies were also stopped after
all 16 funded epoch309 claims finalized, paying 103.320655346 alpha with eight
alpha-rao of accounted rounding residue. The [final peer-review report](sim-testnet/FINAL.md)
contains the receipts and pinned state; the historical 15/17 scenario remains
`final_acceptance=false`.

Actual read-only doctor on local source `02dfe50` passed 63 of 64 checks. Its
sole failure was systemd's degraded state from 43 stopped simulator units.
Their metadata and all 621 available journal entries were preserved before
resetting only those historical failure flags; the manager now reports running,
without restarting any process. Actual read-only setup first refused the full
retained repair audit budget's `observed_at` field. Its corrected reader preserves
the signed projection and complete document hash. The next attempt exposed the
missing recovery case for the original finalized repair transactions. Candidate
`9c444e4` passed that gate, then refused the original companion's predecessor
CREATE while binding the later repaired coordinator. Source `cf3ccd6` corrected
the combined carry path and reached reserve-majority planning. Its fourth actual
attempt stopped at 21:48:11 UTC: the target needs another 2,855.249565922 alpha,
but signed transfers already consume the 31,250-alpha lifetime allowance.
The prior 5,999.806443325-alpha repair is already credited. The configured
6,000-alpha allowance applies per repair and does not replenish the lifetime
budget. User approval is pending to raise that lifetime cap to 35,000 alpha for
one additional 3,750-alpha tranche. At pinned native block 7,992,355, that would
raise the observed 61.449% reserve share to at least 66.113%; fresh planning
must recheck stake and transferable source capacity. No revised plan or
transaction was emitted; the original plan and journal remain byte-identical.
[Latest actual admission error](../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v4.stderr).

The final execution workspace and pinned Solidity libraries are prepared at
`temp/sn-final-execution-20260912/workspace`, with real Git directories for
authentic executable VCS stamping. Every tracked source file was compared to
the prior workspace for byte and mode equivalence. The existing unlocked vault
is reused. No key or plaintext secret was copied.
Candidate `9c444e4` is committed and pushed on its review branch, with a reviewed
source lock and an independently built read-only CLI. Five repair-carry and four
relay-continuation roots, the offline authority root and six archive roots each
completed three fresh normal passes and a race pass. Public checkpoint, native
capture and CRV4 checkpoint checks also passed in both modes.
[Completed affected qualification](../temp/sn-final-release-20260912/runtime-9c444e4-20260912T2118Z/RESULT.md).
Successor `eca9e19` passed native coverage, companion carry, adjacent evidence,
archive/payout and native consumers in both modes. Its reward-reader cancellation
failure is corrected: all three reward roots passed three fresh normal runs and
one race run on `94cb3dd`. Simulator and validator V2 observation also pass both
modes. Automatic native readiness and preparation share one absolute deadline;
the full remaining-work bound is 7,570 blocks, preserving both acceptance phases.
The retained-ledger capacity checks pass, including the exact 8,065-block limit
and refusal at 8,066. The 43-root matrix's outdated horizon fixture was its sole
normal failure, with that three-root race partition initially unrun.
[Exact completed matrix and retained failure](../temp/sn-final-release-20260912/runtime-94cb3dd-20260912T221700Z/RESULT-94-GO-MATRIX.md).

On `da27b85`, the corrected horizon root has three fresh normal passes, and its
original three-root partition passes under race. The exact final semantic census
contains 310 roots. Two of 27 gate guards failed in both modes: a stale direct-call
assumption across the real startup delegation chain and three renewal consumers
without the required parallel marker. Astra corrected those three test files;
on `e8bceaaa62e6d1c3ad2f5a30535f7a7a3806661d`, Terra completed three fresh normal
and three fresh race passes of both failed guards. The three affected renewal
consumers also pass together normally and under race. Production source and
the release lock remain unchanged. These confirmations are complete; historical
streaks and horizon checks will not restart.
[Exact partial qualification and stamped CLI](../temp/sn-final-execution-20260912/runtime-da27b85-20260912T2245Z/RESULT-PARTIAL.md).
[Completed guard corrections and adjacent integration](../temp/sn-final-execution-20260912/runtime-e8bceaaa-20260912T230158Z/RESULT-E8-GUARD-CORRECTION-CORRECTED.md).

Both full-gate launch commands are prepared with separate private mutable
resources. The twelve-repository snapshot includes the vault budget. Therefore
the pending spending decision and any approved vault edit, commit and push must
precede the final source freeze and concurrent producer/aggregate launch. A
mid-gate budget change would invalidate the final snapshot. Focused correction
and qualification are complete; final publication and CLI preparation proceed
while approval is pending. After both gates,
proceed through fresh setup-plan admission, renewed fleet authorizations, bounded
relay continuation, retained-history adoption and the actual full campaign.

Earlier component checkpoints, superseded by the completed results above:

Terra passed the strict V2 history-adoption core and corrected EMA bridge
normally and under race, including three fresh confirmations in each failed
mode. Those fixes are integrated. The ten focused renewal roots also passed
normally and under race on source `006c0c0`, including three fresh confirmations
in each failed mode for the two repaired roots. [Renewal validation](../temp/sn-renewal-006c-validation-20260912T1820Z/runtime/renewal-006c-20260912T1828Z/RESULT.md).
The combined strict CLI, owned-LAN routing, repair carry, plan revision and
renewed lifecycle code is assembled. On source `2984c9b`, all 71 selected
validator roots pass normally and under race; the corrected cadence root also
has three fresh passes per failed mode. The 98-root simulator matrix exposed
renewal-evidence, lifecycle and history-adoption fixture failures. Its race
process exhausted the shared ten-minute budget; the terminal lifecycle root
had run for 53 seconds. Preserve that timeout and qualify the complete selected
population in bounded partitions with unchanged deadlines. A mode-775 TMPDIR
also caused one invalid launcher refusal, which is not a product diagnosis.
[Combined validation and original failures](../temp/sn-strict-composed-fixes-validation-20260912/runtime/preflight-20260912T191953Z/RESULT.md).

Contract generation is complete at `5c4c546`. The revised full Forge build,
18 binding-policy tests, generator consistency checks, and generator/stabi
tests normally and under race pass. The coordinator creation and runtime bytes
exactly match the retained deployed repair; runtime size is 24,564 bytes.
The original oversized test harness and first overflow-fixture failure remain
retained. [Exact artifact comparison](../temp/sn-contract-generation-20260912T1841Z/runtime/generation-20260912T1845Z/full-build-revised-coordinator-compare.stdout).
The generated payload is integrated. Both corrected renewal-evidence roots
have three fresh normal and three fresh race passes on `73ad855`.
[Renewal evidence confirmations](../temp/sn-semantic-renewal-generation-validation-20260912/runtime/RESULT.md).
Composed `bcb1ce0` passes the selected native/EVM checkpoint, capture, history-read
and startup populations in both modes. Its three simulator fixture failures and
two archive fixture failures are preserved; their corrections and the frozen
public publication/consumer/recorder code are composed in successor `e06f055`
for Terra qualification. [Composed results](../temp/sn-finalization-integration-20260912/runtime/RESULT.md).
The later native interval and reward/application coverage results are above.
Neither full gate nor either complete live acceptance phase has passed.
The unrelated calibration prerequisites were removed from SN qualification;
Terra passed all 12 affected guard roots normally and under race. The existing
runtime dependency census and full SN gates remain required. [Scope validation](../temp/sn-scope-validation-20260912T1830Z/runtime/scope-guard-20260912T1831Z/RESULT.md).

Read-only copies of all four retained source ledgers were inspected without
changing their file metadata. The largest source contains 134,673 records,
16,958 trails and 698,568,804 raw record bytes, within the original limits of
655,360 records, 81,920 trails and 10 GiB. This observation does not replay record
signatures or certify the remaining campaign. All four activation contexts bind
block 7,975,563; the existing 10,080-block relay allowance formula therefore ends
at 7,985,643, before the observed finalized block 7,991,348. No `evidence.relay.*`
entry exists in the retained journal. The retained locators contain 182 pending
members; the corrected 7,570-block full-work forecast requires another 200.
Locator counts still require complete signature and immutable-slot authentication. Astra has
frozen an explicit plan-bound continuation at `a52758b` with up to 512 relay slots at
50 gwei per 1,000,000-gas action, within the existing 25.6-TAO relay reserve.
It must preserve original activation and liabilities, bind the actual pending
census and finite remaining run window, and retain every original source-storage
and lifetime monetary limit. The continuation and capacity code has passed its
affected checks above; no continuation plan has been applied. Choose its fixed
end only after gates and renewal so preparation does not consume the remaining
source-capacity margin.

## Historical shortened execution — 2026-09-11 17:33 UTC

The user directed us to stop preparation tests, run the actual simulation on the
real testnet, and fix issues found by that run. The former producer and aggregate
gate prerequisites, repeated failed-test confirmation sequences, duplicate plan
comparisons, separate pre-launch smoke rehearsals, and repeated audit work are
removed from the launch path. Do not start another preparation test cycle.
Previously completed evidence remains reusable within its recorded scope;
waived or unrun checks must never be described as passing. Full preparation
gates are no longer conditions of completing this testnet exercise.

## Execute now

1. Reuse the existing attempt-4 plan and completed receipts. The carry repair,
   database migration and configuration rendering are complete. Do not repeat
   them or regenerate the plan for a provisional driver correction.
2. Keep the existing `epoch` scenario running with explicit --provisional-resume,
   using the retained configuration and corrected driver. The first timed run
   started at 15:56:51 UTC. Current ownership and the latest run ID are recorded
   in the external finalization directory's CURRENT.json. This scenario observes
   an epoch transition on the working fleet; it does not
   certify the full release or production acceptance window. Fix failures
   observed by this run and report its actual outcome.
   The full release attempt completed all 16 lifecycle preparation actions,
   then stopped before acceptance because its planned pruning target was UID 7
   while the computed and recorded target was UID 1. Preserve that attempt and
   failure; defer the pruning/fault campaign while the epoch scenario runs.
   When an actual runtime correction requires a new worker image, preserve the
   interrupted result and completed state, join its owner and fleet, deploy the
   corrected image to both simulator and dedicated Connect, and resume the
   existing epoch scenario. Never describe an interrupted interval as passing.
   Keep native custody,
   spending limits, journal serialization, process ownership and live health.
   Process log classifications are observations in this mode: preserve every
   finding and its original classification without stopping the fleet for it.
   Provisional startup may use ready providers while other live swarms catch up;
   retain actual health values. A new controller may adopt the existing fleet
   without replacing its binary or manifest, or restarting its processes.
   Bound actual process readiness to 30 seconds; retain exact generation and
   process identities, live PIDs, and non-provider health probes.
   Provisional scenario startup uses the same authenticated live-fleet
   preparation as resume to omit the full doctor when no spend is pending.
   It reuses the completed topology handoff through the existing process log
   gate; it must not manufacture completion or new verified setup receipts.
   The nested provisional campaign executor reuses its exact parent's already
   authenticated deployment payloads. Omit the duplicate historical deployment
   preflight while retaining current scenario actions and their postconditions.
   Provisional relay startup and preparation require only the next block to
   fit the original paid horizon; log the full requested forecast as waived.
   Skip the duplicate pending-public-census preview, while authenticating each
   actual publication before its relay admission and send. Keep the original
   activation/native anchors, 256-slot ceiling, debits and all spending caps.
   Full phase coverage is not established by this provisional admission.
   Provisional launch omits precompile conformance and the pre-launch
   governance drill. Preserve their actual failed/unrun evidence and report
   both prerequisites as waived. Do not repeat probe funding or commitments
   to enter the traffic run; keep actual takeover binding actions and their
   spending/transaction postconditions. Full conformance remains unproven.
   The owned LAN RPC at 192.168.1.162:9944 has been verified against testnet
   chain 945 and the original native genesis. Native and EVM traffic now use
   that route through an invocation-only provisional transport override and
   the existing workload fault proxies, with all RPC rate limits removed.
   Record the actual endpoints and zero RPC rate limits. Retain the approved
   plan, signed inputs, receipts and spending limits; omit independent public
   RPC comparison in this mode and keep final_acceptance=false.
   Fresh signed proof coverage is an observation during the run, not a
   provisional campaign startup prerequisite. Record
   fresh_proof_startup_waived=true, the original proof baseline, and observed
   counts with observed_proof_counts_verified=false. Do not fill verified proof
   counts or describe old coverage as a fresh-proof pass. Continue actual proof
   validation during scenario observations and completion reporting.
   Provisional adoption also waives strict public deployment evidence
   publication, which revalidates superseded historical manifests. Record
   deployment_evidence_publication_waived=true and final_acceptance=false in the
   provisional handoff. Preserve existing public files and publication errors;
   do not create a substitute published manifest or describe publication as
   passing. Keep approved topology actions, journal entries and spending caps.
   The four exact private activation contexts may grant testnet staging directly under
   the explicit retained-context allowance. Upload signatures, session/object
   binding, finite intent expiry and quotas remain enforced. Historical and
   current-chain admission checks are waived/unrun, never reported as passing.
3. Observe real transactions, provider traffic, validator proofs and accepted
   epochs. Fix concrete runtime failures and resume supported completed work.
   Keep original errors and actual completion markers; never invent a pass.
4. Report the achieved coverage, transactions, epochs, failures and remaining
   gaps from the actual run. Public replay and release certification work must
   not delay launch; describe any omitted validation honestly.

## Limits and current state

Use public Bittensor testnet, netuid 521, with the existing 1,000 providers,
20 swarms, two operators and two validators. Retain attempt-4, its keys,
used activations, signed setup, journal, approvals and deployed contracts.
Caps remain: 6,000 alpha reserve-repair allowance, 31,250 alpha lifetime,
180 EVM within 200 total TAO, 262 registrations, and no new subnets.
Do not reset state or repeat funding/registration transactions.

The provisional fleet has run real testnet work. All twenty swarms passed live
startup on September 11 before a process-log gate stopped that generation for
onboarding-metric SQL warnings. The provisional driver now records those
classifications without making them launch conditions. The SQL correction is
prepared separately and must not delay launch. No preparation tests are running.
This mode records final_acceptance=false; do not claim strict certification.

Deployed in CLI25: after the hash-pinned testnet handoff validates,
enable existing closed-native-input deferral in memory. Preserve signed subnet
1391 inputs from settlement 290; report deferral without native submission and
continue at the next native epoch. This does not establish successful trail
proofs.

Also deployed in CLI25: validated provisional shared boundary preparation receives a
120-second canonical-read budget within its existing producer deadline (240
seconds in this run). Trail/packet deadlines and ordinary reads remain 30
seconds. The owned LAN route now has no RPC request quota; canonical checks
remain in place. CLI27 also corrected the stale dedicated Connect binary,
which had disabled subnet egress attribution. This produced 660 additional
proof rows across all four validator/operator paths before the epoch scenario.
That scenario exposed a settlement rollover failure. CLI29 excluded local
signature verification and scratch writes from the HTTP I/O deadline; both
settlement 292 closures completed. Public evidence replay then encountered a
truncated stream; its interaction with server deadlines is the inferred cause.
CLI32 gives that bounded route a ten-minute
request/write allowance and records abort causes. It also uses existing
same-nonce cancellation for an expired close intent, and schedules ST sync and
close retries every five seconds to reach the five-block close window. The
owned-LAN deployment restores the original taskworker count 8 / batch size 4.
Both operators finalized their epoch293 closes before the cutoff; captured
emission and payout remained zero. Both validators subsequently published
epoch293 evidence. CLI33 normalized validated in-memory contract address text,
allowing native1394 measurements to seal without changing retained files.
CLI34 added a bounded observation of the actual retained V2 intent records;
it leaves strict authenticated counters and acceptance claims unchanged.
The next actual native submission failure exposed GSRPC's handling of JSON
null storage results. Decode those results as nullable strings so an absent
slot remains distinguishable from malformed responses. Deploy this correction
through the same retained-state resume procedure; keep all original failures.

The bounded consumer run completed once and joined successfully. All eight
escrow contracts settled, producing 1,052,426 and 1,052,424 provider usage bytes
for the two operators from existing credits. No new account, credit or chain
funding was created. These byte sweeps establish actual usage, not a chain
payout; their fiat revenue is zero. Do not repeat this traffic as preparation.
Observe epoch294 provider eligibility and a nonempty payout commitment, then
the existing epoch295 deposit and subsequent pool scoring. A nonempty usage
root can be committed even when captured emission is zero. The sealed294
measurements correctly gave zero pool weight because source293 had no root;
preserve those measurements. Native application, positive capture and payout
remain actual-run outcomes to establish. Record their results in CURRENT.json
and the report while the fleet continues working.

Historical evidence remains in [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md),
[FINAL.md](FINAL.md), and the external finalization directory. The native
campaign's full epoch windows remain real elapsed time; there is no renewed
14-hour completion promise before an actual campaign start.
