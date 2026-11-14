# Notice: capture the flag starts with epoch 8, beside the provider pool

November 11, 2026. All times are UTC estimates from block numbers at 12
seconds per block, as on the mainnet timeline, and can move by a few minutes.
Finalized chain state governs, and we will update this notice as each step
completes.

## What capture the flag is

Capture the flag is a second SN25 pool for each network operator, beside its
provider pool. The provider pool pays for usage, which gathers where users
are. Capture the flag pays for being present in a country: a national
firewall works country by country, and providers and extenders inside a
country are the way around it.

- **Ten slots in every country.** Each of the 249 ISO 3166-1 countries has
  ten slots each epoch (one week): five for providers and five for
  extenders. Every slot is worth the same amount, the pool's new funding for
  the epoch divided by 2,490.
- **The operator lists who is eligible, and a public draw picks the
  winners.** At the end of each epoch the network operator lists the
  providers and extenders that its own data shows were eligible (see below)
  and records that list's hash on chain. A drand round published at least a
  minute after that record then draws up to five winners per country and
  role. The list is fixed on chain before the random value exists, so nobody
  can steer the draw.
- **One entry per subnet.** Miners in the same /29 IPv4 or /48 IPv6 block
  share one entry, so packing hosts into one block adds no chances.
- **A slot with no winner carries forward.** Its amount goes into its
  country's pot for that role. Each winner takes its own slot and a fifth of
  that pot, and the rest keeps carrying.
  - A miner that is alone in a country takes a fifth of what is left each
    epoch it stays: about two thirds of the pot over five epochs.
  - Every other miner that becomes eligible there takes its own fifth.
- **The pots stay with the pool.** An immutable contract with no owner moves
  each epoch's pots back to the capture the flag pool, so they wait there
  for winners.
- **It settles like the provider pool.** Each epoch's result is a payout
  root. The validator approves it after recomputing the draw and its
  amounts, claims open after the 14,400-block challenge period, and they
  never expire. The list, the draw, the winners and the pots are public.

## What this changes in the payout

From epoch 8, the providers' share of SN25 emission is split three ways. The
providers' share itself does not change: it is a tenth of the miners'
allocation, as before.

| Channel | Share | At epoch 4's scale, before any fleet is weighted | Once fleets are weighted |
| --- | --- | ---: | ---: |
| Provider pools | 4/10, plus the top-level miners' 5/10 while no top-level miner is weighted | 1,076.26 α | 478.34 α |
| Capture the flag | 1/10 | 119.58 α | 119.58 α |
| Top-level miners | 5/10 | none | 597.92 α |

- **The provider pool gets 9/10 of the providers' share instead of all of
  it.** In epoch 4 that share was 1,195.84 α, all of it to the provider
  pool. From epoch 8 the same share would give the provider pool about
  1,076 α, and 4/10, about 478 α, once top-level fleets are weighted.
- **A slot is worth about 0.048 α at epoch 4's scale,** and a country with
  no eligible miners adds about 0.48 α to its two pots each epoch.
- These are the pools' funding at epoch 4's scale, not a forecast. Emission
  changes what each epoch funds, and the number of eligible miners changes
  what each of them receives.

Validators do not measure or probe capture the flag miners. Each validator
chooses whether to weight the capture the flag pool, and the pool is paid
while validators holding more than half of the stake do. UR's validator
weights it from epoch 8, and holds that majority today. Partner validators
need nothing for epoch 8: until a release of theirs supports capture the
flag, consensus clips their weights by about a hundredth of their row.

## Who can win

A provider or extender enters its country's draw for an epoch when:

- **it was online, passing the network operator's checks and in that
  country for at least half of the epoch.** The operator samples every
  provider and extender each quarter hour, and a miner needs at least half
  of the epoch's samples. Quarter hours of a platform-wide outage that the
  operator declares are left out, and listed;
- **it is active at the end of the epoch, still in that country.** A
  provider is providing to the public with a valid location; an extender
  has an active address;
- **it has a payout wallet,** and it is not bound to a top-level miner's
  fleet, which is paid in that tier instead;
- **it is not on one of the network operator's own networks,** and each
  IPv4 address or IPv6 /64 counts once.

A miner can be eligible as a provider and as an extender, and can win both.

**The chance to win.** Up to five entries win in each country and role. With
five entries or fewer, each takes a slot. With more, each entry's chance is
5 in the number of entries: 83% at six entries, 50% at ten and 5% at a
hundred.

Rewards are paid in alpha, the token of SN25, whose value changes and can
fall. What a miner receives depends on its uptime, on how many others are
eligible in its country and on the network's emission, and miners pay their
own costs, such as power and data.

## Same wallet, same claims

Winners are paid to the payout wallet that the provider pool already uses
for them. There is nothing new to sign or register.

A winner's capture the flag account is claimed beside its pool account, in
the same claim flow, and like a pool account it never expires. Claims for
epoch 8 open around **December 7**, at about the same time as epoch 8's pool
claims. Until the app releases that name it, an app shows a capture the flag
account as "Pool 4294967297" and claims it like any pool.

## When

It starts with epoch 8, the same epoch as the extender pool, so extenders can
win from the first epoch.

| Epoch | Blocks | Ends about | Root committed by |
| --- | --- | --- | --- |
| 6 | 9,499,228 to 9,549,627 | November 20, 18:25 | block 9,550,828, about 22:25 |
| 7 | 9,549,628 to 9,600,027 | November 27, 18:25 | block 9,601,228, about 22:25 |
| 8 | 9,600,028 to 9,650,427 | December 4, 18:25 | block 9,651,628, about 22:25 |
| 9 | 9,650,428 to 9,700,827 | December 11, 18:25 | block 9,702,028, about 22:25 |

1. **Before November 20, about 18:25** (the end of epoch 6): the network
   operator's servers start sampling every provider and extender each
   quarter hour, so that epoch 7 is a full shadow epoch.
2. **By about November 22:** the governance Safe schedules one operation
   through the 72-hour public timelock. It upgrades the v2 settlement
   contract and registers the network operator's capture the flag pool,
   pool 4,294,967,297. The pool's hotkey is derived only when the
   operation executes, so it is not public during the 72-hour wait. If an
   execution fails, it runs again in a later block; if it cannot run before
   epoch 8 starts, capture the flag starts with epoch 9 instead. The vault
   that holds the funds does not change.
3. **Around November 25, two days before epoch 8:** anyone executes the
   operation once its 72 hours have passed. The pool registers on SN25 and
   takes one UID, early enough to earn weight before its immunity ends. The
   carry contract that keeps the pots with the pool is created after it.
4. **During epoch 7:** the subnet owner signs and publishes the amendment
   entry that fixes the split and the pool from epoch 8, beside the
   extender pool's.
5. **November 27, about 18:25:** epoch 8 starts at block 9,600,028, and
   presence counts from there. UR's validator weights the capture the flag
   pool from epoch 8's first tempo. Its new weights are revealed after a
   delay, so the first tempos of epoch 8 still pay as before.
6. **After epoch 7 ends:** a shadow run. The operator builds epoch 7's list,
   draw and payouts from the real samples without committing them, publishes
   an unsigned report, and the validator's checks run on it.
7. **By about December 1:** the validator release that checks and approves
   capture the flag roots.
8. **December 4, about 18:25:** epoch 8 ends at block 9,650,428. The
   operator commits its list by block 9,651,028 (about 20:25), the draw
   follows, and its root is committed with the provider pool's by block
   9,651,628 (about 22:25).
9. **Around December 7:** the first capture the flag claims, once the
   validator has approved the root and its 14,400-block challenge period has
   passed. The approval is due by the end of epoch 9, about December 11,
   18:25.

## What would move the start to epoch 9

Capture the flag starts with epoch 9 instead if, when epoch 8 starts:

- the timelock operation has not executed. It has to be scheduled at least
  72 hours before epoch 8 starts, so by about November 24, 18:25;
- the amendment entry is not signed, since the signing tool then requires
  epoch 9;
- the presence sampling is not running.

If the operator's draw is not ready when epoch 8 ends, it commits no list for
epoch 8. No winners are drawn for it, every slot carries forward into its
country's pot, and the first winners are drawn in epoch 9. A late validator
release does not move the start as long as it comes before the pool's UID
loses its immunity; it only lowers epoch 8's funding for the pool.

We will update this notice if the start moves.

## What you need to do

Nothing. Keep your miner online in its country. Eligible providers and
extenders are entered with the payout wallet they already use.

The milestones are on the
[mainnet timeline](https://ur.xyz/mainnet#capture-the-flag-epoch-8). The
design is [CAPTURETHEFLAG.md](../../CAPTURETHEFLAG.md).
