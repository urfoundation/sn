# Probe custody recovery after a retained interval

The reverse probe call returned its exact approved principal but left stake
credited between blocks on the second hotkey. The signed release interval may
finish with `precompile_conformance_complete` unresolved. Preserve that result,
its observations and every original transaction. The recovery command closes
only this custody scope; it does not launch, restart or accept a scenario.

Run the command after the interval owner exits. It acquires the same exclusive
journal before changing evidence or opening transaction managers. A live owner
causes refusal before any recovery write. A still-running topology retains its
supervised RPC route; a stopped topology uses its approved canonical route.
All transactions use the retained plan, probe, two hotkeys and recovery coldkey.

## Authority and cost

A separate authorization is signed by both the plan's budget owner and probe
owner. It binds the original conformance hash, immutable receipt basis, plan,
configuration, chain, netuid, probe runtime and custody roles. It authorizes at
most eight supplemental calls, each at most 500,000 gas and the smaller of the
plan's fee ceiling or 100 gwei. It cannot migrate implicitly to another plan.

The first operation attempts to transfer the full quoted move-hotkey position to
the original recovery coldkey. Only an actual preflight revert can select a
fixed 1,000,000,000 alpha-rao top-up from existing sample-hotkey custody, followed
by another sweep. Every quote is pinned and bound into the action intent.

After the move position is empty, the original planned sample transfer runs.
Its receipt remains durable if stake accrued after its quote. If the remaining
sample position cannot be transferred, the authority permits at most two reseeds,
each exactly the original approved probe seed's TAO input. Each reseed is followed
by a full quoted sample sweep. No new alpha allowance is implied. The proposal
exposes this additional TAO separately from gas and charges both to an explicit
suballocation of the retained campaign reserve. Current signed and queued
liabilities are rechecked before execution and between turns.

If that campaign suballocation is exhausted, the v2 proposal shows its exact
shortfall: existing signed liabilities plus the bounded repair ceiling, less the
original reserve. Both owners may allocate only that amount from the retained
plan's unused lifetime caps. The proposal pins active plus superseded spend and
the original approved limits; it charges the supplement against both EVM and
total TAO, rounding native rao upward. It cannot increase a cap, add an arbitrary
margin, or change any setup action. Earlier v1 approvals retain their exact
encoding and original reserve-only authority. Stale proposals still fail if
new liabilities consume the explicitly signed headroom before execution.

For the current 20,000,000 TAO-rao seed, the worst-case supplemental authority is
0.04 TAO of reseeds plus 0.4 TAO of gas. The original planned sample transfer keeps
its existing plan budget. Successful steps and unused reserve are distinct;
these are ceilings, not a claim about actual expenditure.

## Commands

Set `SIM_TESTNET_BINARY` to the qualified recovery build. Set `STATE`, `CONFIG`,
`SN_REPO`, `PLAN_HASH` and `RPC_AUTHORITY` to the exact retained deployment inputs.
Use a fresh directory below `/mnt/data/sn-testnet` for `RECOVERY_WORK_DIR`.
The original interval's terminal evidence must already be captured before the
authorization is published, so its original hash can be checked by the composed
handoff.

```bash
mkdir -p -- "$RECOVERY_WORK_DIR"
RECOVERY_ARGS=(--config "$CONFIG" --sn-repo "$SN_REPO" --state-dir "$STATE"
  --provisional-resume --plan-hash "$PLAN_HASH"
  --owned-rpc-authority "$RPC_AUTHORITY" --format json)

# Read-only proposal: includes current signed/queued exposure and reserve headroom.
"$SIM_TESTNET_BINARY" probe-recovery "${RECOVERY_ARGS[@]}" \
  > "$RECOVERY_WORK_DIR/budget.json"
BUDGET_SHA256=$(sha256sum "$RECOVERY_WORK_DIR/budget.json" | awk '{print $1}')

# Publish the reviewed, dual-signed authorization; this sends no transaction.
"$SIM_TESTNET_BINARY" probe-recovery "${RECOVERY_ARGS[@]}" \
  --probe-recovery-budget "$RECOVERY_WORK_DIR/budget.json" \
  --probe-recovery-budget-sha256 "$BUDGET_SHA256" --apply \
  > "$RECOVERY_WORK_DIR/authorization.json"

# Once the preceding interval's writer has exited, perform only the repair.
"$SIM_TESTNET_BINARY" probe-recovery "${RECOVERY_ARGS[@]}" \
  --execute-recovery --apply > "$RECOVERY_WORK_DIR/completion.json"
```

The same execution command resumes a partial recovery. Signed bytes and nonce
are retained before broadcast; receipt and postcondition checkpoints are retained
before a later operation. Timeouts retry through the ordinary sender, while an
unknown transaction outcome must reconcile before any new signature. Exhausted
bounds or an untransferable position remain explicit pending liabilities. A new
quote cannot replace an already retained operation.

Recovery admission authenticates the dual-signed authority, journal anchor,
native custody roles and immutable probe runtime, then opens only its required
reader and deployer. An unfinalized signed repair reaches exact reconciliation
before deployment-wide nonce auditing. Its partial payload cannot execute other
setup actions, and saved bytes retain the same gas/value limits before any
rebroadcast. Full original and repair replay remains mandatory for completion.

## Evidence and acceptance

- `public/precompile-recovery-authorization.json`: immutable dual-signed authority.
- `public/precompile-conformance.json`: original receipt fields plus the explicit
  `recovery` sequence, inter-block credits, original sample quote and all remaining
  custody. An inclusion credit is never reclassified as rounding.
- `transactions/*.rlp`, journal and ordinary action postconditions: exact signed
  bytes, nonce, finalization and successful step checkpoints.
- `public/precompile-recovery-complete.json`: dual-signed completion binding the
  original evidence hash, repaired evidence hash, authorization, journal end and
  one canonical finalized head with both source positions exactly zero.

Completion requires replaying original and supplemental calldata/receipts,
finite gas/value limits, every pinned quote, historical dividend evidence and
both final stake positions. The original sample transfer can retain a nonzero
intermediate balance only when later authenticated recovery proves it empty.
Old evidence without a recovery field keeps its canonical encoding and existing
acceptance behavior.

`verifyPrecompileRecoveryOriginalEvidence` rejects changes to already retained
phase evidence; `verifyPrecompileRecoveryCompletion` verifies the signed complete
record and all accounting offline. The issuer performs strict chain replay before
signing it. A composed release marker must bind this exact completion to the
original signed interval and preserve its failed check. This command does not
produce that marker or grant mainnet approval by itself.
