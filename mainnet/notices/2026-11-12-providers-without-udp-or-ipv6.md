# Notice: providers without UDP or IPv6 are offered for the traffic they carry

November 12, 2026. The URnetwork apps released on November 12 carry the change.

## Before

The network operator's quality probe loads test sites through every provider,
about every 24 minutes, and judges each provider on its checks of the last 8
hours. Where a site serves HTTP/3, the probe loads it over HTTPS (TCP) and over
HTTP/3 (QUIC, which runs over UDP). Where a provider's connections prove IPv6,
it loads the site over IPv6 as well. A check passes only when every load
passes, and at least 4 in 5 checks must pass.

So a provider that carries TCP and IPv4 traffic well but carries no UDP, or
whose IPv6 fails, failed its checks, and clients were offered it rarely, if at
all, even for the traffic it carries. On November 12, about 20,700 providers
passed every check but their HTTP/3 loads.

## What changes

From the apps' release of November 12, clients are offered these providers for
the traffic they carry:

- **No UDP.** A provider whose checks failed only over HTTP/3 (UDP) is offered
  for TCP traffic. Clients never send it UDP, so QUIC and other UDP traffic
  stays on providers that carry UDP.
- **No IPv6.** A provider whose connections prove IPv6, but whose checks failed
  only over IPv6, is offered for IPv4 traffic.
- **How the apps use them.** Each app keeps a few of these providers beside its
  usual ones. They join the race for every TCP connection they can carry, and
  the quickest to answer takes the connection. The apps also turn to them where
  a location has too few providers that carry UDP.
- **Only failed checks count against a provider.** A provider with no failed
  check over UDP or IPv6 is offered for both, as before. A provider that
  regains UDP or IPv6 is offered for it again within 8 hours, as its failed
  checks leave the window.

The network operator's hosted proxies carry the same change. Apps from before
the release keep the old selection, so these providers gain traffic as the
apps update.

## What this changes in the payout

Nothing in SN25 scoring, weights or payouts reads the quality probe's checks,
and none of it changes. SN25 pool rewards are still split by completed usage. A
provider offered for the traffic it carries serves more of it, so it earns more
through that traffic alone, as from any other traffic.

## The provider status

A provider's status in the apps says what its checks found. For a provider
offered only for TCP or only for IPv4 traffic, the network checks row now says
how many of its checks failed only over HTTP/3 (UDP), or only over IPv6, and
what it is offered for.

## What you need to do

Nothing. A provider that carries no UDP can carry it again: update the app or
miner on it to the latest release, since some older builds do not route UDP,
and check that its network does not block UDP.

The change is on the
[mainnet timeline](https://ur.xyz/mainnet#providers-without-udp-or-ipv6). It is
the network operator's design
[FP2-PROTOCOL.md](https://github.com/urnetwork/server/blob/main/FP2-PROTOCOL.md).
