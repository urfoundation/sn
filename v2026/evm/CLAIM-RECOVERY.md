# Claim acceptance and failed runtime payment

A valid Merkle claim must keep its provider credit when a runtime payment
cannot be admitted. Previously, `RuntimeAccountingMismatch` thrown inside the
success arm of `try transferStake` escaped that catch. The whole claim reverted,
including its leaf marker and credit. A provider submitting on the last eligible
block could lose acceptance: expiry would return that amount to operator carry.

`STSettlementVault` now encloses payment accounting, the runtime transfer, and
both exact balance-delta checks in `transferClaimCredit`. Only a vault self-call
while the outer claim/withdrawal guard is held can enter this frame. A failed
frame rolls back all of its state, including runtime movement; `claim` retains
the accepted credit and emits the existing `RuntimeFailure` deferral. A direct
withdrawal still reverts on failure. No rounding tolerance, redirectable payout,
coordinator authority, or additional reserve credit is introduced.

The acceptance path retains its existing backing check. This change does not
promise successful acceptance if escrow backing cannot be established, nor
does it make a persistently incompatible runtime able to pay. Exact capture
checks are unchanged: a rejected capture creates no funded entitlement.

The receipt verifier also follows the actual zero-based Solidity enum:
`BelowMinimum = 0`, `PriceUnavailable = 1`, `RuntimeFailure = 2`. It previously
rejected `0` and accepted undefined `3`.

The regression suite exercises source/destination discrepancies at expiry,
unchanged escrow and preexisting recipient balances after rejected payments,
cross-operator credit aggregation, permissionless exact retry, leaf replay,
carry isolation, and denial of external/reentrant access to the payment frame.
The existing suite covers missing roots, zero capture with carry, price outages,
subminimum accumulation, capture failure, reserve rounding, caps, and finalized
claims across coordinator pause/upgrade.

## Deployment and runtime gates

- This changes a **non-upgradeable vault**. It is a new-deployment candidate;
  upgrading an existing coordinator cannot repair an existing vault. Existing
  roots, unpaid credits and custody require their own audited lifecycle and
  must remain accessible.
- The generated deployment payload and bindings must match the new artifact.
  The historical testnet `deploy/testnet/release.lock.yml` is not a mainnet
  authorization and is not silently refreshed by this correction. Approve a
  new release manifest after the composed source/toolchain/runtime review.
- Pinned reference source `67dcf7f791dc495064c293f080a0702cb433e51e` uses
  [nested Substrate storage transactions for EVM frames](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/vendor/frontier/frame/evm/src/runner/stack.rs#L801).
  Its [same-subnet stake transfer](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/staking/stake_utils.rs#L1062)
  updates share pools and enforces a live-price transfer minimum. This source
  inspection and EVM mock tests do not authenticate the actual mainnet runtime.
  Before installation, bind exact finalized code/metadata to reviewed source
  and rehearse nested-frame rollback, caller coldkeys, transfer units, exact
  deltas, price scale, transfer floor, registration and code-size rules.
- `STCoordinator` source, policy/cap semantics and storage are unchanged. Its
  existing 12-byte runtime size margin requires a fresh build-size check for
  every candidate and exact-artifact creation/readback on the selected runtime.
- Priced versus explicit zero-price demand remains an authenticated policy and
  usage-audit gate. These contracts alone do not validate the off-chain pricing
  schedule or prove eligible usage. Zero-priced demand is distinct from a zero
  or unavailable native alpha spot price, which cannot authorize a transfer
  below the runtime floor. Principal caps also do not replace the
  production lifetime ledger for gas, staging allowance and outstanding intents.
- The selected 90% owner-recycle policy does not fund this vault or the reserve
  sink. Live recognized owner identities, Recycle mode, validator caps,
  independent-validator behavior and actual final native payout remain separate
  activation gates. The remaining 10% proposal is not a Yuma payout guarantee.

No chain transaction, live deployment, runtime approval or mainnet settlement
acceptance is represented by these source changes.
