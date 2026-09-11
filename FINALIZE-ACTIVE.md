# Testnet execution plan

Updated 2026-09-11 13:43 UTC. This is the active plan and supersedes conflicting
preparation requirements in FINALIZE.md, FINALIZE-COMPLETE.md and older handoffs.

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
2. Run resume and then release-candidate with explicit --provisional-resume,
   using the retained configuration and corrected driver. Keep native custody,
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

Prepared worker correction: after the hash-pinned testnet handoff validates,
enable existing closed-native-input deferral in memory. Preserve signed subnet
1391 inputs from settlement 290; report deferral without native submission and
continue at the next native epoch. This is not deployed to fleet 21 yet and
does not establish successful trail proofs.

Also prepared: validated provisional shared boundary preparation receives a
120-second canonical-read budget within its existing producer deadline (240
seconds in this run). Trail/packet deadlines and ordinary reads remain 30
seconds. RPC quota and canonical checks are unchanged; live success is pending.

Historical evidence remains in [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md),
[FINAL.md](FINAL.md), and the external finalization directory. The native
campaign's full epoch windows remain real elapsed time; there is no renewed
14-hour completion promise before an actual campaign start.
