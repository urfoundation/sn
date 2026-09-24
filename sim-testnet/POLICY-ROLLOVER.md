# Fresh policy evidence generation

`policy-rollover` is an independent, reviewable subplan for the existing two
validators and two operators. It keeps the existing deployment, native hotkeys,
permissionless activation contract, keeper, campaign plan and gas reserve.
Each generation derives four new client keys and uses independent ledger,
statistics, coordinator and evidence directories. The plan records the four
original activations as lineage and explicitly makes no ledger continuity claim.

Prepare against the current approved setup and an explicit future epoch:

```sh
sim-testnet policy-rollover --config CONFIG --state-dir STATE \
  --plan-hash SOURCE_PLAN_HASH --provisional-resume \
  --owned-rpc-authority LAN_HOST_PORT \
  --rollover-generation 1 --rollover-epoch FUTURE_EPOCH --format json
```

This retains one common finalized native/EVM snapshot, all four original
source identities and both signatures for each new activation before any
transaction. Inspect the printed `plan_hash`, snapshots, epoch, members and
maximum gas allocation. The exact plan is saved under
`STATE/policy-rollover/plans/generation-00000000000000000001-epoch-<20 digit epoch>/plan.json`.
Preparation may run while the old supervisor remains live.

Apply or resume the same exact subplan:

```sh
sim-testnet policy-rollover --config CONFIG --state-dir STATE \
  --plan-hash SOURCE_PLAN_HASH --provisional-resume \
  --owned-rpc-authority LAN_HOST_PORT --apply \
  --rollover-plan ABSOLUTE_SAVED_PLAN --rollover-plan-hash ROLLOVER_PLAN_HASH \
  --format json
```

The command acquires the existing deployment writer lock and reauthenticates
the snapshots. It reconciles finalized activations before sending. Transport
errors get four independently bounded read attempts; they do not spend the
durable send allowance. Each member has at most three keeper send attempts
across restarts. The keeper retains the same signed transaction and nonce.
Exact third-party publication and a late receipt both satisfy the public slot.
All maximum fees fit inside the existing campaign reserve after retained and
external transaction liabilities.

After all four activations, resume when the selected epoch's common initial
boundary is finalized. Client provisioning and immutable config staging then
complete while the old supervisor may remain live. A live supervisor produces
`staged-awaiting-stopped-topology`; this command never stops it. After the
controlled stop, repeat the same apply command. It writes the verified handoff
receipt before the immutable `STATE/policy-rollover/handoff.json` pointer.

The normal approved launch/resume re-renders both APIs from the four new
contexts and selects the new validator configs. Generation inputs and the
pointer enter the normal runtime file inventory. The relay preserves old
sources, records bounded old-policy gaps, and routes future evidence through
the new sources. `first_full_epoch` is activation epoch plus one; qualification
must start no earlier than this value. Publication alone is not interval proof.

If a selected epoch becomes unusable, prepare a later epoch with a new explicit
generation. Every prior plan, consent and transaction receipt remains immutable;
older signed transactions still count against the reserve. A generation cannot
be rebound to another epoch. The first completed handoff owns the active
pointer; replacing an already active generation needs a separately reviewed
successor handoff.
