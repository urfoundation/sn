# Notice: Top 200 bindings that land by November 29 count for epoch 8

November 27, 2026. All times are UTC. Times for future blocks are estimates at
12 seconds per block; the block numbers govern. This is a one-time exception to
the binding timing rule of the
[Top 200 binding notice](2026-11-17-top200-binding-open.md), for epoch 8 only.

## Why

A binding normally starts no earlier than the epoch after the one it lands in:
the coordinator only accepts a binding signed to start in a later epoch. So a
binding had to land before epoch 8's first block, 9,600,028 (November 27,
18:33), to count for epoch 8.

No fleet could do that. Finney moved to node-subtensor runtime 497 at block
9,587,045 (November 25), and `provider fleet register`, `publish` and `bind`
refuse to write under a runtime until that runtime's reviewed fleet runtime
authority document is published. The document for runtime 497,
[`mainnet/fleet-runtime-authority/runtime-497.json`](../fleet-runtime-authority/runtime-497.json),
was published on November 27 at about 20:40, after epoch 8 had begun. The delay
was on our side: the document should have been ready when the runtime changed.

## What changes

A binding signed to start at epoch 9 that lands in any block from 9,600,028
through 9,615,060 counts for epoch 8 as well, from the block it lands in.

- **The deadline is block 9,615,061,** expected on November 29 at about 20:40.
  Blocks never come faster than one every 12 seconds, so it lands no earlier
  than 20:40. A binding that lands in block 9,615,061 or later waits for epoch
  9, as the normal rule says.
- **Head score.** The validator counts the device in its fleet for the rest of
  epoch 8, from the block the binding lands in, and its fleet's head score
  includes the work the validator measures on it from then on.
- **Provider pool.** A device earns in one tier at a time. A device bound in
  the window is bound at epoch 8's last boundary, so it leaves the provider pool
  for all of epoch 8 and earns no epoch 8 pool share, as for any bound device.
  Weigh that before binding a device that earns well in the pool.
- **Revoking.** A revocation takes effect at the next epoch, as for every
  binding, so a window binding revoked during epoch 8 still counts for all of
  epoch 8.

## What stays the same

- The coordinator on chain 964 is unchanged. You still sign the binding to start
  at epoch 9 (`--valid_from_epoch 9`), the earliest epoch the coordinator
  accepts during epoch 8. Its validity runs from epoch 9 for up to 12 epochs, as
  signed; the window adds epoch 8 in front.
- Bindings that landed before epoch 8 are unchanged. One signed from epoch 9
  starts at epoch 9, and one in force for epoch 8 stays in force: a window
  binding of the same device never displaces it.
- A binding signed to start after epoch 9 starts when it says.
- Every other epoch keeps the normal rule. There is no window for epoch 9 or
  later.
- The rest of the binding rules stand: register a hotkey, publish the fleet's
  manifest and wait for the commitment oracle's mirror, then bind each device.
  At most 7 devices of each fleet draw ahead into the measured cohort.

## When it takes effect

The exception is in the SN25 validator and the network operator's server
release that carry it. The validator scores the work it measures once its
release is live; work measured before then was scored under the normal rule and
keeps that score. The epoch 8 pool, capture the flag and extender pool payouts
are computed after epoch 8 closes, at block 9,650,428 (around December 4,
18:35), with the exception applied. We will update this notice when the
releases are live.

## What to do

- To use the window, bind before block 9,615,061, with `--valid_from_epoch 9`.
- `provider fleet bind` prints the EVM block the binding landed in. With the
  provider release that carries this exception, it also prints the epoch the
  binding counts from, and `provider fleet status` prints a `counts_from_epoch`
  line.
- If you bind nothing, nothing changes for you.

The rule is written in
[`protocol/fleet_late_binding_epoch8.go`](../../protocol/fleet_late_binding_epoch8.go).
