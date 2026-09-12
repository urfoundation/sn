# Testnet execution plan

Updated 2026-09-12. The user has requested full finalization and fixes for
previously ignored failures, flakiness and issues exposed by the shortened run.
The full requirements in [FINALIZE.md](FINALIZE.md) govern completion again.
The user explicitly confirmed SN testnet finalization under `sn/FINALIZE.md`;
the absent `server/connect/FINALIZE.md` reference does not change that scope.
The earlier shortened-run instructions below are retained as historical scope
for those attempts, whose `final_acceptance=false` results remain unchanged.

Current work:

1. Compose the retained campaign fixes and current source in one isolated
   candidate. Preserve the existing deployment, wallets, approvals and journals.
2. Use Terra (`gpt-5.6-terra`, reasoning effort `max`) for all tests and gate
   execution. Use Astra (`gpt-6-astra`, reasoning effort `max`) to diagnose and
   fix failures and flakiness, then return corrected source to Terra for reruns.
3. Correct the actual validator, native-receipt and scenario-anomaly failures.
   Complete both full gates on the final candidate with private test services;
   keep their original failed and interrupted results visible.
4. Complete the required real release campaign and production soak, then
   reconcile on-chain outcomes, public replay, the final report and shutdown.
   Reuse valid completed evidence; unrun, failed and waived checks are not passes.

Retain the approved 6,000-alpha repair allowance, 31,250-alpha lifetime limit,
180 EVM within 200 total TAO, 262 registrations and zero new subnets. Use
`192.168.1.162:9944` without RPC rate limits. Full acceptance remains pending.

Current checkpoint, 2026-09-12 21:55 UTC: the full fleet is stopped. Both
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
Astra is checking the concrete cause and remedy. No revised plan or transaction
was emitted; the original plan and journal remain byte-identical.
[Latest actual admission error](../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v4.stderr).

The physical final workspace and pinned Solidity libraries are prepared at
`temp/sn-final-release-20260912/workspace`; the existing unlocked vault is reused.
Candidate `9c444e4` is committed and pushed on its review branch, with a reviewed
source lock and an independently built read-only CLI. Five repair-carry and four
relay-continuation roots, the offline authority root and six archive roots each
completed three fresh normal passes and a race pass. Public checkpoint, native
capture and CRV4 checkpoint checks also passed in both modes.
[Completed affected qualification](../temp/sn-final-release-20260912/runtime-9c444e4-20260912T2118Z/RESULT.md).
Successor `eca9e19` is under affected qualification. Its native-coverage and
companion-carry checks pass in both modes; a historical reward-reader cancellation
test failed and is with Astra. Strict observation must use the adopted V2
namespace, and the acceptance baseline must follow fresh native applications
and payout observations from both validators. The automatic warm-up and its
complete remaining capacity forecast are being completed in parallel.

Terra has passed the strict V2 history-adoption core and corrected EMA bridge
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
Authenticated native interval and reward/application coverage remain in progress.
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
members; the existing full-work forecast requires another 184. Locator counts
still require complete signature and immutable-slot authentication. Astra has
frozen an explicit plan-bound continuation at `a52758b` with up to 512 relay slots at
50 gwei per 1,000,000-gas action, within the existing 25.6-TAO relay reserve.
It must preserve original activation and liabilities, bind the actual pending
census and finite remaining run window, and retain every original source-storage
and lifetime monetary limit. This candidate change is not yet qualified or applied.

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
