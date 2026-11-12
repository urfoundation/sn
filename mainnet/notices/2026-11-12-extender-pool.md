# Notice: the extender pool starts with epoch 8

November 12, 2026. All times are UTC estimates from block numbers at 12
seconds per block, as on the mainnet timeline, and can move by a few minutes.
Finalized chain state governs, and we will update this notice as each step
completes.

## What changes

From block 9,600,028, the first block of epoch 8, the part of each contract's
usage that no provider earns counts for extenders instead. The subnet owner
approved this amendment on November 10 ([WHITEPAPER.md](../../WHITEPAPER.md)
§8.6).

- **Providers are no longer credited for bytes they serve to devices of their
  own network.** A contract's usage is still split evenly between its hops:
  the provider, any intermediary and any extender that relays it. From epoch
  8, a hop on the network of the device that opened the contract earns no
  share of it, on the free plan or a paid one. Its share goes to the extender
  pool instead. The contract's other hops keep their shares, and no share
  passes from one hop to another.
- **Traffic to the network operator's platform counts too.** The share of a
  platform endpoint that the network operator registers, such as sidecar's
  edges, goes to the pool. Sidecar has not shipped, so until it does, the
  pool's bytes come from same-network traffic alone.
- **Only funded traffic counts.** A contract counts when a data balance funded
  it, on the free plan or a paid one. Since November 11, same-network contracts
  draw on the sending network's data balance like any other traffic. A
  contract that no balance funded adds nothing, and neither does the network
  operator's own measurement and test traffic: validators' trail clients, the
  qualityprobe, its probes and the proxy-test account. The providers that carry
  that traffic keep earning from it.
- **An extender on a contract keeps its own share**, as one of the contract's
  n hops (the subnet owner's rule of November 11), unless it is on the network
  of the device that opened the contract. A connection that an extender does
  not relay does not make it a hop, as
  [the notice of November 12](2026-11-12-extender-relay-share.md) on the
  extenders' share explains.
- **Friends-and-family providers** on another network are credited as before.
- **Earlier epochs keep their credit.** Same-network usage before epoch 8 keeps
  the credit it earned, and nothing before epoch 8 is rebuilt or reopened. The
  rule applies to every contract that closes in epoch 8 or later.

## How the pool is paid

The epoch's pool bytes are spread over the extenders that are eligible at the
end of the epoch:

- **Evenly over blocks, then within each block.** Each /29 IPv4 or /48 IPv6
  block that holds at least one eligible extender takes an equal part, and its
  eligible extenders split that part evenly. Packing hosts into one block adds
  nothing. Remainder bytes go to the lowest block hashes, then the lowest
  client ids.
- **At most half of a pool.** The pool's bytes are weighted beside the
  provider rows, up to the epoch's eligible provider usage, so extenders take
  at most half of the pool and providers at least half. Bytes above that cap
  enter no weight. Were the pool's bytes 5% of the eligible provider usage,
  extenders would take about 1/21 of the pool, or 4.76%.
- **No eligible extender, no pool.** With no eligible extender, the pool's
  bytes are dropped. The epoch pays its provider rows alone, still without the
  same-network shares.
- **Paid like provider usage.** An extender's share is paid in the network
  operator's provider pool, to its payout wallet. A wallet that also runs
  providers adds the two.

## Who counts as an extender

An extender of the network operator counts for an epoch when, at the epoch's
end block:

- **it is in the active pool:** its record is active and not revoked, and at
  least one of its addresses is active, not deactivated by the operator's
  five-minute challenge (six failures in a row). There is no minimum age and
  no uptime threshold;
- **it is counted once:** records that share a provider client, an IPv4
  address or an IPv6 /64 count as one;
- **it has a payout wallet,** chosen as for provider rows, and its provider
  client is not bound to a top-level miner's fleet at either end of the epoch;
- **it is not the network operator's own:** not in its gated fleet, not a
  canary, and not on any other network the operator declares it controls.

Validators do not probe or check extenders: the directory is built so that no
one learns every extender's address at once. The network operator attests
which extenders are in the active pool and which traffic is same-network or
platform traffic, and validators recompute the arithmetic.

## What is published

- **The ledger.** From epoch 8, each epoch's ledger header (schema
  urnetwork-payout-ledger-v3) lists its extender component: the pool's bytes,
  the census and the share rows. The census has one row per extender the
  operator knew in the epoch, eligible or not: its id, its provider client,
  its payout wallet, its block as a keyed /29 or /48 prefix hash, never an
  address, and its flags: active at the end, the operator's own (gated,
  canary or declared), revoked, a duplicate, or bound to a fleet. The share
  rows give each eligible extender's bytes. The operator serves all of them at
  `GET /sn/ledger/object`, by hash.
- **The usage statement.** From epoch 8, each epoch's signed usage statement
  is version 2 (kind epoch-usage-v2 on the operator's `/sn/evidence` routes).
  It adds the pool's bytes and their two parts, platform and same-network, the
  pool's weight after the cap, the number of contracts and of payer networks
  behind it, the number of blocks and of eligible extenders, and the census and
  share-row chunks. Its total usage includes all of the pool's bytes, so the
  operator's demand deposit prices them like any other usage.
- **Shadow reports for epochs 6 and 7.** After each of these epochs ends, the
  operator builds what the extender pool would have paid, from the real census
  and contracts, without committing it, and publishes an unsigned report
  (schema urnetwork-extender-shadow-v1) with its objects. The validator's
  checks run on it. Nothing in it is paid.
- **The signed amendment entry.** The subnet owner signs the line that starts
  the pool during epoch 7, beside capture the flag's entry. It is published at
  <https://ur.xyz/amendments.yml> and in [amendments.yml](../../amendments.yml)
  in this repository:

  ```text
  UR settlement amendment v1: chain 964, sn 25, no 1, from epoch 8, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1
  ```

## When

| Epoch | Blocks | Ends about | Root committed by |
| --- | --- | --- | --- |
| 6 | 9,499,228 to 9,549,627 | November 20, 18:25 | block 9,550,828, about 22:25 |
| 7 | 9,549,628 to 9,600,027 | November 27, 18:25 | block 9,601,228, about 22:25 |
| 8 | 9,600,028 to 9,650,427 | December 4, 18:25 | block 9,651,628, about 22:25 |
| 9 | 9,650,428 to 9,700,827 | December 11, 18:25 | block 9,702,028, about 22:25 |

1. **Since November 11:** same-network and friends-and-family contracts draw
   on the sending network's data balance, so the contracts that close in epoch
   8 are funded like any other.
2. **Before November 20, about 18:25** (the end of epoch 6): the network
   operator's servers start capturing the extender census at each epoch's end
   and building the pool in shadow, so that epoch 6 is the first shadow epoch.
3. **After epochs 6 and 7 end:** the shadow runs. For each, the operator
   publishes its unsigned report, and the validator's checks run on it. An
   offline replay of epoch 4, whose contracts are still kept, checks that the
   new rule changes its rows by exactly the same-network shares.
4. **During epoch 7:** the subnet owner signs and publishes the amendment
   entry, beside capture the flag's.
5. **November 27, about 18:25:** epoch 8 starts at block 9,600,028.
6. **By about December 1:** the validator release that checks the extender
   component and reads the amendment entry, deployed while the validator is
   idle.
7. **December 4, about 18:25:** epoch 8 ends at block 9,650,428, and its
   census is captured at that boundary. Its root, with the extender component,
   is committed by block 9,651,628 (about 22:25).
8. **About two days after the root:** epoch 8's claims, extenders' shares
   included, once the validator has approved the root and its 14,400-block
   challenge period has passed. The approval is due by the end of epoch 9,
   about December 11, 18:25.

## What would move the start to epoch 9

The extender pool starts with epoch 9 instead if:

- the amendment entry is not signed before epoch 8 starts, since the signing
  tool then requires epoch 9;
- the validator release is not deployed before epoch 8's root is committed;
- the census capture is not running at the end of epoch 8;
- a mismatch found in the shadow runs is not resolved;
- this notice is not out before epoch 8 starts.

If the signing cannot happen before epoch 8 starts, configuration is the
fallback: the network operator's server and every validator configure the same
line, unsigned, and the entry is signed from a later epoch
([SETTLEMENT-AMENDMENTS.md](../SETTLEMENT-AMENDMENTS.md)). We will update this
notice if the start moves or the fallback is used.

## What you need to do

Nothing. Keep your extender running with a payout wallet set: an extender
without one is not counted. Eligible extenders are paid to the payout wallet
they already use, and claim their shares with their pool accounts, in the same
claim flow.

Rewards are paid in alpha, the token of SN25, whose value changes and can
fall. What an extender receives depends on the pool's bytes in the epoch, on
how many blocks hold eligible extenders and on the network's emission, and
miners pay their own costs, such as power and data.

The milestones are on the
[mainnet timeline](https://ur.xyz/mainnet#extender-pool-epoch-8). The design is
[WHITEPAPER.md](../../WHITEPAPER.md) §8.6.
