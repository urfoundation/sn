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

## Update, November 6

- **Step 1 is done.** The recovery root was committed at the end of epoch 4,
  at block 9,449,041
  ([transaction](https://evm.taostats.io/tx/0x1731a110aba17ae0827a502896f84ba82b0e2d9e45792e8a380dd3f4f838662a)).
  The funds reach the v2 vault after its 48-hour challenge period, around
  **November 8, 18:25**.
- **The signed upgrade of the v2 settlement contract is in place.**
- **Changes to the v2 settlement contract now wait 72 hours** behind a public
  timelock, which has owned the contract since November 6, 17:43
  ([transaction](https://evm.taostats.io/tx/0x832da70ea60d269ee5a8542c3d42e56378f665f045481aae818e8ea3e4ccebea)).
  Adding the recovered funds to epoch 5's payout is one such change. It will
  be scheduled by **November 10, 18:25**, so that it takes effect before epoch
  5 closes.
- **The schedule is unchanged:** the funds are paid out with epoch 5, with
  claims from around November 15. If the addition misses epoch 5, the payout
  moves to epoch 6 and we will update this notice.

## Update, November 11

- **Step 2 is done.** The recovery root finalized on November 8 at block
  9,463,231
  ([transaction](https://evm.taostats.io/tx/0xc9fa804023288aefdb381d0c19dd8f1ef8559530715b045f87475e695a51d6f9)),
  and the funds reached the v2 vault at 18:30
  ([claim](https://evm.taostats.io/tx/0x18c4e8d3f72ceaea1b6f275b54b27786967a834822e3ae1539d084bc06a7ea28),
  [escrow](https://evm.taostats.io/tx/0xaa66b41505da170231e52a36db68c12198990241a3b1977663bce76d90ce93dc)).
  They are epoch 1's 3,482.26 α and epoch 3's 4,132.72 α, with 499.31 α that
  the v1 vault captured at the end of epoch 4.
- **Adding the funds to epoch 5's payout was not scheduled** by November 10,
  18:25. Every change to the v2 settlement contract waits 72 hours, so it can
  no longer take effect before epoch 5 closes, and the recovered funds are not
  paid with epoch 5. Epoch 5's regular payout is unaffected.
- **The funds stay in the v2 vault** until they are added to a later epoch's
  payout. We will update this notice when that is scheduled.
- **Epoch 4's claims opened on November 8, at 19:10,** when the first v2 root
  finalized at block 9,463,438
  ([transaction](https://evm.taostats.io/tx/0x6001380acdf1d850e6dbfc1e4bf22bac9d1e59c9d1075d56b1697c797ef26147)).
