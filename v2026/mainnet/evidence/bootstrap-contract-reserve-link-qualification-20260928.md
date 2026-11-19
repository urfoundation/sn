# Reserve recorder binding source qualification

The one-shot `STReserveSink.setRecorderOnce(proxy)` executor in SN commit
`14afe75d951b1ecc464f84d6ebc2ecb3fb86cfc6`, with the fixture read-gas
correction `e27addf6d6057751eec01387676b5947f141d9c2`, passed offline
source qualification. The root branch cherry-picks retain byte-identical
candidate files. The same signed nine-action graph, five authenticated
predecessors, original transaction bytes, bounded attempts and canonical
recorder postconditions remain in force.

Sol medium ran all **27** reserve-link roots normally and under race detection
using exact disjoint shards. The full `./mainnet` normal suite passed **523**
roots in 2093.357 seconds. Two targeted getter-budget roots, ten adjacent
low-gas/refusal roots, vet, source/module fences and four causal checks also
passed. The causal checks remove only the independent getter gas budget and
make both targeted roots fail at the intended read boundary in normal and race.

The original candidate's one assertion failure was a test fixture `eth_call`
with the selected transaction's 21,000-gas limit. Its full test attempt later
hit a 35-minute package timer without a second assertion. Two first-pass
nine-root race shards also hit 15-minute package timers without assertions;
only their unfinished coverage was split and rerun. All **27** race roots then
passed. These timer artifacts are retained, not counted as passes.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-reserve-read-gas-20260928/RESULT.md)
has SHA-256
`67dc9ed38e74e893a2c00d459bc8e0de1df3a657d37be91863ab1fa0e0625aa6`.
Its source and resolved-module before/after fences match byte-for-byte. This
qualifies local source behavior under those pinned dependencies, not live
mainnet identity, custody, finality or deployed state. No live signing, RPC
write or contract action was performed.
