# Testnet execution plan

Updated 2026-09-11 12:52 UTC. This is the active plan and supersedes conflicting
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
   retain actual health values and fresh signed proof requirements. The four
   exact private activation contexts may grant testnet staging directly under
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

Historical evidence remains in [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md),
[FINAL.md](FINAL.md), and the external finalization directory. The native
campaign's full epoch windows remain real elapsed time; there is no renewed
14-hour completion promise before an actual campaign start.
