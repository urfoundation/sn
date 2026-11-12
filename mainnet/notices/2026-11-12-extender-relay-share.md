# Notice: extenders earn their share only on contracts they can relay

November 12, 2026. All times are UTC. The network operator's server release
of November 12 has rolled out.

## The rule

The subnet owner's rule of November 11: a contract that comes through an
extender counts the extender as one of the contract's n hops, beside the
provider and any other hop, and each hop is credited 1/n of the contract's
usage. Only contracts actually relayed through an extender earn that share.

## What changes

The network operator counts a connection as relayed by an extender when it
comes from the extender's address: a client that reaches the platform through
an extender arrives from that address. Some connections come from an
extender's address without being relayed. From the network operator's server
release of November 12, these no longer make the extender a hop:

- **The extender host's own connection.** An extender host is usually a
  provider too, and its provider connection comes from its own address.
- **Other devices of the extender's own network that share its address**,
  such as a fleet's providers on one host.
- **Providers of other networks that share its address while providing
  publicly.** A provider that provides publicly always connects to the
  platform directly, never through an extender, so one that connects from an
  extender's address shares the address rather than being relayed.

A client of another network that connects through the extender is relayed,
and the extender keeps its 1/n share of its contracts, as before. So does a
provider of another network that provides only to its own network, which may
connect through an extender.

**One kind of relay counts that did not.** An extender sends a client on over
the same address family the client arrived on, but within that family the
host picks the address it sends from, and an IPv6 host commonly sends from a
temporary address of its /64 rather than the one it activated from. From the
release, a connection from any address in the /64 of an extender's active
IPv6 address counts as relayed by that extender, unless the /64 holds the
addresses of more than one extender. IPv4 addresses are matched exactly, as
before.

## What this changes in the payout

SN25 pool rewards are still split by completed usage. What changes is which
hops a contract's usage is split between:

- **A provider of another network on an extender's address** is credited its
  contracts' whole usage. Before, the extender's network was credited a share
  of it, usually half.
- **A provider serving a device that connects from an extender of the
  device's own network** is credited the whole usage too. Before, the
  device's own network was credited a share of it in the SN25 usage.
- **Within one network**, a fleet's providers on its own extender's address
  are credited the usage they serve, instead of sharing it with the
  extender's provider client. The network's total is unchanged; its split
  between payout wallets can change where those clients pay out to different
  ones.
- **An extender host's own contracts are unchanged**: its provider and
  extender roles were already counted once.
- **A contract relayed from another address of an extender's IPv6 /64** now
  counts the extender as one of its hops, so the provider is credited its
  share beside the extender's, as on every relayed contract.

The network operator's provider subsidy, which for traffic since the October 6
cutoff pays no USDC and only counts toward the weekly points the network
operator awards, follows the same hops.

From epoch 8, the extender pool (WHITEPAPER.md §8.6) counts what it always
would: the shares of a contract that no provider earns. A connection that
relays nothing is no longer one of a contract's hops, so it adds nothing to the
pool.

The network operator's statistics of contracts with an extender count only
these hops from the release, so they drop: most of what they counted was
extender hosts' own connections and their own networks' devices.

## When

- **Contracts created since the network operator's server release of
  November 12.** The rule is applied when a contract is created, so contracts
  created before the release keep the hops they were created with until they
  close.

## What you need to do

Nothing. Providers and extenders do not need to change anything.

The change is on the [mainnet timeline](https://ur.xyz/mainnet#extender-relay-share).
It is the network operator's server change
[a83f0883c](https://github.com/urnetwork/server/commit/a83f0883cb11aa6d21b275540192072b8fa2d668).
