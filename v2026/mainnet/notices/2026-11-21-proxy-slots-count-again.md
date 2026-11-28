# Notice: proxy slots count like any other provider again

November 21, 2026. All times are UTC. This takes effect with the network
operator's server release of November 21, 2026. It withdraws the
[notice of November 19](2026-11-19-proxy-slots-show-no-unit.md).

## What changes

A provider counts at the address the network sees it at, whether the miner owns
that address or brings it in through a proxy. So from this server release, a
proxy slot counts like any other provider:

- **Head (Top 200).** Its trail hops record the prefix of its address, its /29
  or /48, as every provider's do. So a proxy slot adds a head-breadth unit to
  its fleet like a direct device.
- **Capture the flag.** It is a candidate like any other miner, with the unit
  of its address.
- **Extender pool.** Its extender records count as active like any other.

## What stays the same

- Clients and the network operators choose providers by latency and speed. A
  direct provider still wins where it is faster.
- Pool shares by completed usage are unchanged, for proxy slots and direct
  devices alike.
- No signed amendment is needed. This changes no settlement term, and the
  validator scores every hop that records a prefix, as before.
- Hops a proxy slot confirmed between the November 19 release and this one
  recorded no prefix, and they keep what they recorded. From this release on, a
  proxy slot's hops record its prefix.

## What to do

Nothing. You can bind a proxy slot like any other device. Until a provider
release removes it, `provider fleet bind` and `provider fleet sign` still warn
that a proxy slot shows no head-breadth unit; that warning is out of date.

## Why

Proxies add addresses to the network quickly and cheaply, by bringing in
capacity from other networks. The network operators choose providers by
latency and speed, so they already prefer providers that are not behind a
proxy where those are faster. From the network's point of view, an address a
miner owns and an address it bridges into the network through a proxy are
alike.

The rules in [`WHITEPAPER.md`](../../WHITEPAPER.md) §8.4 and §8.6.2 (E1),
[`CAPTURETHEFLAG.md`](../../CAPTURETHEFLAG.md) §4.2 and
[`VALIDATOR.md`](../../VALIDATOR.md) §8.1, §11.1 and §11.4 read as they did
before November 19.
