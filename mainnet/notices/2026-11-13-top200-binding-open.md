# Notice: Top 200 fleet binding is open on SN25

November 13, 2026. All times are UTC. The network operator's release that opens
binding has rolled out.

## What opened

Top-level miners can claim a spot in SN25's head, its 200 top-level seats. The
network operator's `GET /sn/top` answers `binding_open: true`. So the Top 200
page on ur.io (ur.io/app/account/top200) and the current apps no longer say that
claiming isn't open: they prompt a network projected to rank inside the head to
claim its spot.

Claiming is optional. Nothing changes for a provider that doesn't claim: the
provider pool is still split by completed usage.

## How a claim works

1. **Register a hotkey on SN25.** Registering burns TAO from your coldkey, and the
   burn is not refunded. Each registration raises the burn, and it falls back
   toward its floor over time, so check it right before you pay:
   `btcli subnets burn-cost 25`.
2. **Publish the fleet's manifest from that hotkey.** The manifest names the
   fleet's devices, and publishing writes it as a tagged commitment on SN25.
   Once the commitment is final, the network operator's commitment oracle
   mirrors it to the coordinator on chain 964 by itself. A binding checks the
   mirrored commitment, and reverts until it is there.
3. **Bind each device to the fleet.** Each device signs a binding for a run of
   up to 12 epochs that starts after the current epoch.

The Top 200 page, the apps and the command-line guide
(ur.io/docs/top200-claim-btcli) walk through each step. Nothing in a claim asks
for a recovery phrase, and no step sends a key anywhere.

The command-line writes, `provider fleet register`, `publish`, `bind` and
`revoke`, need the provider release with `provider fleet storage`, the storage
declaration that command prepares
([`miner/FLEET-MAINNET-RUNTIME.md`](../../miner/FLEET-MAINNET-RUNTIME.md#preparing-storage-for-mainnet-writes)),
and the reviewed fleet runtime authority document, published in
[`mainnet/fleet-runtime-authority/`](../fleet-runtime-authority/) with its
SHA-256. A new chain runtime needs a new reviewed document before those writes
run again: no runtime is adopted automatically.

## How a spot pays

The SN25 validator weights the head's selected fleets in proportion to each
fleet's score, not its rank. A spot pays only while the validator steers its
weights.

A fleet is scored on the devices the validator measures. At most 7 devices of
each fleet draw ahead of other providers into the measured cohort: the cohort's
size divided by the 200 seats. So bind at most 7 devices, unless the others are
already measured.

From epoch 8, the head's share of the providers' emission follows capture the
flag's split, as [its notice](2026-11-11-capture-the-flag.md) says. Rewards are
paid in alpha, whose value changes and can fall.

## What a bound device gives up

A bound device leaves the provider pool. While its binding is active at either
boundary of an epoch, it earns no pool share for that epoch, whether or not its
fleet earns: a device earns in one tier at a time. A bound device the validator
doesn't measure earns no head score either, so it earns nothing.

## Before you register

- **Timing.** A UID with no emission can be replaced by a new registration once
  its immunity ends, 21,600 blocks (about 3 days) after it registers, and a
  binding starts no earlier than the next epoch. So register in the 3 days
  before the epoch your binding starts, and leave time to publish, wait for the
  mirror and bind before that epoch's first block. A binding that lands later
  waits for the next epoch.
- **Publishing again.** A commitment older than 100,800 blocks (14 days) can't
  bind new devices, and commitments in the older, untagged format are not
  mirrored. A fleet that published in the older format, or more than 14 days
  ago, publishes again, and so does a fleet that adds devices later.
