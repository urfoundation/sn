# Notice: Pro data moves to each network's own cycle, SN25 rewards counted as before

November 11, 2026. All times are UTC. We will update this notice when the
release has rolled out.

## What changes

- **Pro data follows each network's own cycle.** From the network operator's
  server release of November 11, a Pro network gets its full monthly data on
  its own monthly cycle, counted from the day its subscription started or
  last renewed, instead of on the 1st of the month.
  - A monthly plan's cycle is its billing or renewal period: the Stripe
    billing period, or the App Store or Google Play renewal period.
  - A yearly plan, or a term paid once in crypto, has a cycle each month from
    its start.
- **Each cycle's grant carries that cycle's share of the subscription's
  revenue:** the paid period's revenue in proportion to the part of the
  period the cycle covers. The cycles of a period carry its revenue once
  between them.
- **A payment no longer adds a separate 600 GiB credit.** The payment's own
  balance stays as the record of the paid period, with no data in it.

## What this changes in the payout

**SN25 pool rewards are counted as before.** The pool is split by completed
usage, and subscription payments are not an input to it (see
[how rewards are counted](https://ur.xyz/docs/miner/rewards#how-rewards-are-counted)).

**What changes is the subscription revenue that the network operator's
provider subsidy counts.** Subscription revenue is one of the subsidy's
inputs, and the subsidy counts it through the balances that fund settled
traffic. For traffic since the October 6 cutoff, the subsidy pays no USDC. It
only counts toward the weekly points the network operator awards.

- **Before**, a paid period's revenue could count twice: in full through its
  600 GiB credit, and again as a share of the monthly grant on the 1st. A
  Google Play subscription's revenue was spread over the whole time since the
  subscription first started, so a long-running one counted only a small
  share.
- **From the release**, each period's revenue counts once, through its cycle
  grants, as the traffic they fund is settled. A Google Play period counts
  from where the one before it ended.
- **So the subsidy counts less** where revenue counted twice, as in a new
  subscription's first month, **and more** for long-running Google Play
  subscriptions. Its timing follows each network's own cycle rather than the
  1st.
- **A payment credited during a running cycle**, such as a second
  subscription, adds its revenue to the cycles after it. The part that
  overlaps the running cycle is not counted.

## When

- **New payments:** from the server release of November 11.
- **Pro networks that hold the November 1 grant:** their first cycle grant
  starts on **December 1 at 00:00**, when that grant's month ends, and runs to
  the end of the network's own current cycle. After that, grants come at the
  start of each of the network's own cycles.
- **Pro networks that subscribed after November 1, before the release:**
  their current cycle's grant comes soon after the release, beside the
  600 GiB credit they already have, so that period's revenue can still count
  twice. Their next payment starts a cycle like any other.
- **Nothing granted before the release changes.** No balance is shortened,
  moved or removed, and a credit or grant made before the release counts as
  before until it is used up or ends.

## What you need to do

Nothing. Customers pay the same, and SN25 claims are unchanged.

The change is on the [mainnet timeline](https://ur.xyz/mainnet#pro-grant-per-cycle).
It is the network operator's server change
[d96add0d6](https://github.com/urnetwork/server/commit/d96add0d67a23abd0b5df3b68ef1967ae2691e85).
