# Epoch state machine v1

Application epoch `e` begins at the `effective_block` of the latest policy at
or before `e`, plus `(e - effective_epoch) * epoch_blocks` from that immutable
snapshot. The first snapshot's `effective_block` is the deployment block; a
future snapshot's block is computed from the preceding cadence, so scheduling
cannot shorten or extend the active epoch. All boundary decisions use the
finalized block hash. Policy, operator conviction tier,
provider membership, payout wallet, fleet binding, and head/tail eligibility are
snapshotted at that boundary. Changes scheduled during `e` apply no earlier than
`e+1`.

Each `(epoch,no_id)` advances exactly once through:

`OPEN -> CLOSED -> ROOT_COMMITTED|ROOT_MISSED -> FUNDED -> FINALIZED -> CLAIMABLE -> EXPIRED -> CARRIED`.

A root commit is append-only. Finalization requires exact backing in the
non-upgradeable vault. Claims are permissionless and cannot be paused. After
`claim_ttl_epochs` plus grace, only the unclaimed remainder becomes carry for the
same operator. A missed root follows the same-operator carry path. A keeper
advances at most one epoch/page per call, so late maintenance cannot attribute a
multi-epoch delta to the first epoch.
