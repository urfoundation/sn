# Notice: an extender reached only over WebRTC earns its share only on contracts it relays

November 16, 2026. All times are UTC. The change ships with the network
operator's first server release after November 16. The release of November 16
does not carry it.

## The rule

The subnet owner's rule of November 11: a contract that comes through an
extender counts the extender as one of the contract's n hops, beside the
provider and any other hop, and each hop is credited 1/n of the contract's
usage. Only contracts actually relayed through an extender earn that share.

## What changes

The network operator counts a connection as relayed by an extender when it
comes from the extender's address. Since the server release of November 12,
three kinds of connection from that address no longer make the extender a hop:
the extender host's own connection, other devices of the extender's own network
that share its address, and providers that share its address while providing
publicly.

Some extenders can be reached only over WebRTC, the peer-to-peer carrier that
lets a host behind a home router or a carrier-grade NAT relay. A device reaches
such an extender only by signaling it through the device's own connection to
the network operator's platform, so a device relayed by it always holds a
connection that does not come through the extender. A device behind the same
router or NAT as the extender holds none: it connects to the platform directly,
from the address it shares with the extender, and nothing relays it.

From the network operator's first server release after November 16, an
extender that can be reached only over WebRTC is one of a contract's hops only
when the contract's device also holds a connection to the platform that does
not come through that extender. A device's connections include those of its
own client and of the clients it opens for its connections to providers.

On November 16, one such extender was a hop on every contract of 84 devices of
two other networks behind its address, on an app release that cannot reach an
extender over WebRTC, and none of those devices held a connection anywhere
else.

## What this changes in the payout

SN25 pool rewards are still split by completed usage. What changes is which
hops a contract's usage is split between:

- **A device behind the same address as an extender reached only over
  WebRTC**, with no connection elsewhere, no longer makes the extender a hop,
  so the provider is credited the whole of the contract's usage, where the
  extender was credited a share of it, usually half.
- **A device relayed by such an extender** keeps the extender as one of its
  contracts' hops, as before.
- **Extenders that can be reached over TCP, QUIC or DNS** are judged as before.

The network operator's provider subsidy, which for traffic since the October 6
cutoff pays no USDC and only counts toward the weekly points the network
operator awards, follows the same hops. From epoch 8, the extender pool
(WHITEPAPER.md §8.6) counts only the shares of a contract that no provider
earns, so a share that was never a hop adds nothing to it.

## When

- **Contracts created since the network operator's first server release after
  November 16.** The rule is applied when a contract is created, so contracts
  created before the release keep the hops they were created with until they
  close.

## What you need to do

Nothing. Providers and extenders do not need to change anything.

The change is on the [mainnet timeline](https://ur.xyz/mainnet#extender-webrtc-relay-share).
It is the network operator's server change
[ff4b49d56](https://github.com/urnetwork/server/commit/ff4b49d56e1bacd127978caebe35bb38d93f8015).
