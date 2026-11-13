# Version 2 timeline update

Updated October 25, 2026, after the paired production configuration rollout. This document supplies the revised schedule for [the user-facing timeline](timeline.html); that HTML has not yet been updated.

## Current status

**Epoch 3 was missed. Epoch 4 is the proposed first v2 epoch, conditional on completing activation before it starts.** Registration cannot retroactively activate an epoch that has already begun.

A read-only check at finalized block **9,354,702** (block timestamp October 24, 16:40:12 UTC) found runtime **484** and operator 1's v2 pool **unregistered**, with `firstEpoch = 0`. Contracts being deployed and service images being published do not establish that activation is complete.

The runtime 484 service rollout and paired v2 server configuration have now been verified on all six production hosts. Datacenter maintenance is complete. Activation remains pending validator history recovery and finalized pool registration; a Ledger signature has not been requested or submitted.

The prepared registration selects epoch 4. It must finalize **before block 9,398,428**. Until registration finalizes and its state is verified, describe this date as a target, not a confirmed activation.

## Revised public schedule

All dates below are UTC estimates. They use the finalized block timestamp above and **12 seconds per block**, rounded to the minute. Actual block production and finalized contract state govern; refresh estimates before publishing and again before registration.

| Milestone | Previous public schedule | Revised schedule / condition |
| --- | --- | --- |
| Epoch 3 | First v2 epoch, October 23–30 | Remains on v1 for operator 1; no v2 registration occurred before its start at block 9,348,028. |
| Pool registration | October 21–22 | Pending. Complete the production prerequisites and finalize registration before block **9,398,428**; do not wait until the boundary. |
| First v2 epoch | Epoch 3, October 23 | **Target: epoch 4**, October **30, approximately 18:25 UTC**, block **9,398,428**. Conditional on timely finalized registration. |
| First v2 epoch ends | October 30 | November **6, approximately 18:25 UTC**, block **9,448,828**. |
| First v2 close and root commit | October 30, approximately 18:09–22:09 UTC | November **6, approximately 18:25–22:25 UTC**. Epoch 4 ends at block **9,448,828**; the 1,200-block commit window ends at **9,450,028**. |
| First v2 root finalization and claims | November 1, earliest | **November 8, approximately 18:25–22:25 UTC, earliest**, depending on the actual root commit block. The 14,400-block challenge period must elapse and finalization must succeed before claims. |

The first-claim range is an estimate for a root committed within its normal window, not a guaranteed payout window. Open demands, an unavailable or invalid root, delayed finalization, or unmet claim conditions can delay claims. The earliest theoretical finalization block is **9,463,228**, assuming commit at the epoch boundary; a commit at the end of the window shifts that to **9,464,428**.

If registration is not finalized before epoch 4 begins, **epoch 4 is no longer eligible**. Recheck the current epoch, prepare the next eligible first epoch, and revise the public schedule before announcing another date. Do not retain the epoch 4 promise after its deadline passes.

## Work required before confirming activation

- Datacenter maintenance and the paired runtime/configuration rollout are complete. All 20 API instances report ready with the new configuration. Preserve concurrent developer image updates.
- Restore the missing immutable public attempt metadata and verify validator recovery to current epoch processing. The validator is running but remains in history read-wait; activation is not yet ready.
- Refresh registration preflight against finalized state, then use the owner's Ledger through the governance Safe to finalize the prepared operator-pool registration. Read back the actual registered pool, assigned UID, and `firstEpoch`; no contract redeployment is needed.
- Verify watcher operation against the registered pool and retain the transaction and finalized-state evidence. After epoch 4, verify root commitment, challenge expiry, finalization, and claim availability separately.

Application publication remains with the builder. Do not describe all application releases as published based on completed packaging alone.

The provider-policy configuration has a separate modern-provider start date of **October 28** in config main. That setting is not the SN v2 activation transaction and must not be presented as the v2 launch date. Its production deployment also needs verification.

## Suggested user-facing wording

> Settlement v2 is targeting epoch 4, starting around October 30 at 18:25 UTC. Activation is pending final production checks and on-chain pool registration before that epoch starts. The first v2 epoch would finish around November 6, with first claims no earlier than November 8 after root submission and the challenge period. Dates are estimates based on block production; we will confirm activation once registration is finalized.

Update `timeline.html` consistently: its as-of status, ribbon and accessible label, key milestones, October 21–22 registration event, epoch 2/3 descriptions, first-v2-epoch band, and close/commit/claim events. Preserve historical completed events; replace expired planned dates with their actual missed/pending status. Add an epoch 4 band and use the revised block numbers above. Remove the unconditional October 23 activation and November 1 first-v2-claim claims.

## Timing basis

- Finalized-chain reads: coordinator `0xc18925925e2b7bb9059b7d696b8c92762ae86406`, `epochStartBlock(4)` and `policyAt(4)`; vault2 `0xe4865eb1bebdf5c6a10c9d37f3ccdd59aee8f20c`, `pools(1)`.
- Live policy: 50,400-block epochs, 1,200-block root commit window, 14,400-block finalization offset. See [VERSION2-LAUNCH.md](../VERSION2-LAUNCH.md) and [STCoordinator.sol](../evm/src/STCoordinator.sol).
- [STSettlement2.sol](../evm/src/STSettlement2.sol) requires `firstEpoch` to be strictly greater than the current epoch when registering. [STSettlementVault2.sol](../evm/src/STSettlementVault2.sol) enforces root challenge and finalization conditions.
