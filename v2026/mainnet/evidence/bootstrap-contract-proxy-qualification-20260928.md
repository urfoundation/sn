# Atomic coordinator proxy CREATE qualification

Astra max implemented approved action four, `proxy-create`, in frozen SN commit
`a42a2ff351abc0ce9ba263be2393c5fb90297822`, integrated on the mainnet
hardening branch as `b09b646a`. It builds the exact ERC1967Proxy creation and
atomic coordinator initializer from approved bytes, preserving the original
policy while checking the inclusion-height normalization. Canonical completion
checks the implementation, proxy runtime, 24 getters and five distinct storage
words. A new durable journal binds all four completed predecessors and the one
signed graph's cumulative attempts and funding limits.

Sol medium qualified the frozen source in a pinned physical module graph. All
**25** new roots passed normally and under race detection in exact 8+8+9
shards. All **95** adjacent prior-action roots passed normally and under race
detection in exact disjoint shards. A full `./mainnet` normal run passed
**495** roots. Vet, formatting and before/after source/module fences passed.
Ten isolated causal controls exposed the intended encoding, policy, getter,
storage, inclusion-height, implementation, predecessor, checkpoint, attempt
and funding guards. A first encoding mutant failed compilation and was retained
separately; its corrected control produced the intended test failure.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-proxy-20260928/RESULT.md)
has SHA-256
`ba655552282171ba6537e263f944e3836825ede91a7186690db047535a4b719c`.
Integrated `mainnet/*.go` bytes match the frozen tested commit. No live RPC,
signing, transaction or deployment occurred.

This qualifies five source-level actions of the nine-action installation
graph. Reserve/vault links, evidence installation and Safe anchoring remain
open. The synthetic native-precompile fixture does not establish actual
mainnet burn/refund, independent finality, runtime trust or Safe authority.
