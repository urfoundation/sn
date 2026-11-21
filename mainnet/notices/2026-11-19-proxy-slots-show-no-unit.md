# Notice: a proxy slot shows no head, capture the flag or extender unit

> **Withdrawn on November 21, 2026.** A proxy slot counts like any other
> provider again. See the
> [notice of November 21](2026-11-21-proxy-slots-count-again.md). The text
> below is kept as it was published.

November 19, 2026. All times are UTC. This takes effect with the network
operator's server release of November 19, 2026. It applies to proxy slots of the
provider release that sends every egress of a proxy slot through its proxy.

## What changes

A provider can run proxy slots: `provider proxy add` puts proxies in its proxy
file, and each one runs as a slot of its own. With the provider release that
follows this server release, a proxy slot's every connection goes through its
proxy: its platform connections, its exit traffic and its network requests. So
the network sees the slot at its proxy's address, not at its host's.

A proxy is not a host you run. So from this server release, a provider whose
platform connections say they come through a proxy counts at no address of its
own:

- **Head (Top 200).** Its trail hops record no prefix, so it adds no head-breadth
  unit to its fleet. It is measured like any other provider.
- **Capture the flag.** It is not a candidate. The census counts it with the
  miners that have no valid location (`no_location`).
- **Extender pool.** Its extender records count as not active, so it has no
  extender unit.

The provider software marks every connection it makes through a proxy, so a
proxy slot needs no setting.

## What stays the same

- A proxy slot carries traffic and earns its share of the provider pool by its
  completed usage, as before, unless it is bound to a fleet.
- Direct hosts are unchanged. A host shows its own address block, its /29 or
  /48, through a direct slot: a provider run without a proxy file.
- Proxy slots of earlier provider releases connect from their host, and nothing
  changes for them until they update.

## What to do

- **Bind direct devices only.** A bound proxy slot earns no head score and no
  pool share. `provider fleet bind` and `provider fleet sign` warn when the
  client is one of this host's proxy slots, and the Top 200 page doesn't offer
  one.
- To keep your host's unit, run a direct slot beside your proxy slots.

## Why

A head unit is the address block of a host a fleet runs. Without this rule, each
proxy's /29 or /48 would count as a unit of its own, and buying proxies would
buy head breadth.

No signed amendment is needed. This changes no settlement term, the validator
already gives a hop without a prefix no unit, and capture the flag's and the
extender pool's units are still egress prefixes. The rules are in
[`WHITEPAPER.md`](../../WHITEPAPER.md) §8.4 and §8.6.2 (E1),
[`CAPTURETHEFLAG.md`](../../CAPTURETHEFLAG.md) §4.2 and
[`VALIDATOR.md`](../../VALIDATOR.md) §8.1, §11.1 and §11.4.
