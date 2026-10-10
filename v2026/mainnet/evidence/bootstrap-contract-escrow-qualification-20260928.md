# Escrow registration qualification

Astra max implemented approved action three, `escrow-register`, in frozen SN
commit `7c9b2f5ae6e06159e89adf9a0121e43e0066999a`, integrated on the mainnet
hardening branch as `25befce9`. It validates the exact approved
`registerEscrow(uint64)` cap, rao-to-wei value, derived vault target, canonical
receipt/event and UID/hotkey/coldkey mapping. A new durable journal retains
the reserve, vault and coordinator predecessors and the one signed graph's
cumulative attempts and funding limits. Prior journals and approval bytes stay
unchanged.

Sol medium qualified the frozen source in a pinned physical module graph. All
**24** new roots passed normally and under race detection in disjoint 12+12
shards. All **71** adjacent reserve/vault/coordinator/preview roots passed
normally and under race detection. The first combined 20-root coordinator
race run reached its unchanged 15-minute package timeout; exact 10+10 rescue
shards then passed, with the timeout retained as failed evidence. A full
`./mainnet` normal run passed **470** roots. Vet, formatting and source/module
fences passed. Eight isolated causal controls exposed their intended value,
event, mapping, target, predecessor, checkpoint, attempt and funding gaps.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-escrow-20260928/RESULT.md)
has SHA-256
`8adb2e974e0c4d9441253a4e0168a732f94d1167b549a1c52a7d62e665b3c7fa`.
Integrated `mainnet/*.go` bytes match the frozen tested commit. No live RPC,
signing, transaction or deployment occurred.

This qualifies four source-level actions of the nine-action installation
graph. The local native-precompile fixture uses zero-burn behavior, so actual
mainnet burn, existential-deposit and refund semantics remain unmeasured.
Proxy initialization, contract links, evidence installation, Safe anchoring,
mainnet identity and live approval/custody are still open.
