# Testnet execution plan

Updated 2026-09-11 07:48 UTC. This is the active plan and supersedes conflicting
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

1. Finish the known carry fix for obsolete, closed, never-broadcast activation
   attempts. Publish it, update the existing release lock and build the CLI once.
   Validate the repair through the actual plan/resume operation.
2. In parallel, stop and join existing writers, back up and migrate both existing
   databases using the already rehearsed bridge, and use the qualified server
   at the original path. Preserve the current database volumes and all history.
3. Generate one actual plan, check its spending and retained transaction state,
   then resume attempt-4 and start the real release-candidate campaign. Perform
   only prerequisites enforced by the native command or needed to avoid duplicate
   transactions, lost state or conflicting writers.
4. Observe real transactions, provider traffic, validator proofs and accepted
   epochs. Fix concrete runtime failures and resume supported completed work.
   Keep original errors and actual completion markers; never invent a pass.
5. Report the achieved coverage, transactions, epochs, failures and remaining
   gaps from the actual run. Public replay and release certification work must
   not delay launch; describe any omitted validation honestly.

## Limits and current state

Use public Bittensor testnet, netuid 521, with the existing 1,000 providers,
20 swarms, two operators and two validators. Retain attempt-4, its keys,
used activations, signed setup, journal, approvals and deployed contracts.
Caps remain: 6,000 alpha reserve-repair allowance, 31,250 alpha lifetime,
180 EVM within 200 total TAO, 262 registrations, and no new subnets.
Do not reset state or repeat funding/registration transactions.

At this update, the provisional fleet has run real testnet work but the
release-candidate campaign has not started. Both second preparation gates
were cancelled and joined; neither passed. No preparation tests are running.
The actual carry failure and migration are the current launch work.

Historical evidence remains in [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md),
[FINAL.md](FINAL.md), and the external finalization directory. The native
campaign's full epoch windows remain real elapsed time; there is no renewed
14-hour completion promise before an actual campaign start.
