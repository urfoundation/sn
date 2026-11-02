# Notice: epoch 3 payout delayed, funds safe, recovery planned

October 31, 2026. All times are UTC estimates at 12 seconds per block.
Finalized chain state governs, and we will update this notice as each step
completes.

## What happened

- **Epoch 3** (October 23–30): no payout root was committed within its commit
  window, which closed around October 30, 22:25.
  - The cause was on our side. The server step that reads the epoch's
    completed work timed out twice at mainnet scale.
  - That step has been fixed and deployed (October 31) ahead of the next
    close.
- **Epoch 1** (October 9–16): it had no eligible work to pay out, so under the
  protocol's missed-commit rule its funding carried forward instead of being
  paid.

## Your funds are safe

Both epochs' funding sits in the v1 settlement vault. Nothing has been lost,
and carried funding does not expire. No payout from the v1 vault has been made
yet.

| Epoch | Amount | State |
| --- | --- | --- |
| 1 | 3,482.26 α | In carry |
| 3 | 4,132.72 α | Covers about two weeks of emission. It becomes carry when epoch 3 finalizes as missed, around November 1, 18:25. |

## What happens next

We will move these funds from the v1 vault into the v2 settlement vault
through the recovery step planned for the move to v2:
1. **At the next epoch boundary** (epoch 4 ends around **November 6, 18:25**),
   a single recovery root sends the v1 funds to the v2 vault.
2. **After the 48-hour challenge period** (around **November 8**), the funds
   arrive in the v2 vault.
3. **The funds are paid out in the next v2 payout,** for epoch 5:
   - **Epoch 3's funds go to the providers who did epoch 3's work,** in
     proportion to their epoch 3 usage.
   - **Epoch 1's funds** are added to that epoch's regular payout. Epoch 1 had
     no eligible work to allocate them by.

Epoch 5 ends around **November 13, 18:25**. Claims open after its own
challenge period, around **November 15**. That date depends on two things:
- a review and signed upgrade of the v2 settlement contract;
- the validator release that checks the recovery allocation.

If either is not ready in time, the payout moves to epoch 6 (ending around
November 20) and we will update this notice.

Epoch 4 is unaffected. It is the first v2 epoch, and it closes and pays on its
own schedule (close around November 6, 18:25–22:25; claims from around November
8).

## What you need to do

Nothing. Recovered payouts are claimed the same way as regular v2 payouts once
their epoch's claims open.
